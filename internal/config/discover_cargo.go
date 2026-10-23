package config

import (
	"errors"
	"io"
	"io/fs"

	gotoml "github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

// Error codes of Cargo workspace discovery (Knope's CargoWorkspaceError).
const (
	codeCargoTOML          = "remeris::workspace::toml"
	codeCargoMembers       = "remeris::workspace::members"
	codeCargoNoPackageName = "remeris::workspace::no_package_name"
	codeCargoRead          = "remeris::fs::read"
)

const (
	cargoTomlFile = "Cargo.toml"
	cargoLockFile = "Cargo.lock"
	// maxManifestSize caps what is read from one manifest.
	maxManifestSize = 8 << 20
)

// cargoMember is a workspace member: its manifest path, package name and parsed document.
type cargoMember struct {
	file versionedfile.Config
	name string
	doc  map[string]any
}

// cargoWorkspace is the Cargo detector (spec F20.2, Knope's cargo_workspace_members).
//
// Like Knope it reads ./Cargo.toml only if the file can be read (a missing file is no error), and a file that does
// not parse is an error (see cargoRoot). A `[workspace]` standard table with a `members` array makes one package per
// member, named after its `package.name`, with the member's commit scope. Members are used literally (`<member>/Cargo.toml`,
// no globs, as in Knope). Its versioned files are, in member order, the member's own manifest and a dependency entry
// for each other member that depends on it, then the root manifest if it depends on it, then Cargo.lock if it exists.
func cargoWorkspace(root fs.StatFS) ([]PackageConfig, error) {
	doc, src, err := cargoRoot(root)
	if err != nil {
		return nil, err
	}
	rawMembers := cargoMemberList(doc, src)
	if len(rawMembers) == 0 {
		return nil, nil
	}
	var lock *versionedfile.Config
	if _, err := root.Stat(cargoLockFile); err == nil {
		if cfg, cfgErr := versionedfile.NewConfig(cargoLockFile, nil, nil); cfgErr == nil {
			lock = &cfg
		}
	}

	members := make([]cargoMember, 0, len(rawMembers))
	for _, raw := range rawMembers {
		dir, ok := raw.(string)
		if !ok {
			return nil, diag.New("The Cargo workspace members array should contain only strings").
				WithCode(codeCargoMembers)
		}
		member, err := readCargoMember(root, dir+"/"+cargoTomlFile)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}

	pkgs := make([]PackageConfig, 0, len(members))
	for _, member := range members {
		var files []versionedfile.Config
		for _, other := range members {
			switch {
			case other.name == member.name:
				files = append(files, other.file)
			case cargoContainsDependency(other.doc, member.name):
				files = append(files, withDependency(other.file, member.name))
			}
		}
		if cargoContainsDependency(doc, member.name) {
			if cfg, err := versionedfile.NewConfig(cargoTomlFile, &member.name, nil); err == nil {
				files = append(files, cfg)
			}
		}
		if lock != nil {
			files = append(files, *lock)
		}
		pkgs = append(pkgs, PackageConfig{
			Name:           versioning.CustomName(member.name),
			VersionedFiles: files,
			Scopes:         []string{member.name},
		})
	}
	return pkgs, nil
}

func withDependency(cfg versionedfile.Config, name string) versionedfile.Config {
	cfg.Dependency = &name
	return cfg
}

// readCargoMember reads and parses one member manifest.
func readCargoMember(root fs.StatFS, path string) (cargoMember, error) {
	data, err := readCapped(root, path)
	if err != nil {
		cause := err
		var pe *fs.PathError
		if errors.As(err, &pe) {
			cause = pe.Err
		}
		return cargoMember{}, diag.New("Error reading from " + path + ": " + cause.Error()).
			WithCode(codeCargoRead).
			WithHelp("Make sure you have permission to read this file.").
			WithCause(err)
	}
	var doc map[string]any
	if err := gotoml.Unmarshal(data, &doc); err != nil {
		return cargoMember{}, diag.New("Could not parse TOML in " + path + ": " + err.Error()).
			WithCode(codeCargoTOML).
			WithCause(err)
	}
	pkg, _ := doc["package"].(map[string]any)
	name, ok := pkg["name"].(string)
	if !ok {
		return cargoMember{}, diag.New("Could not find a package.name in " + path).WithCode(codeCargoNoPackageName)
	}
	cfg, err := versionedfile.NewConfig(path, nil, nil)
	if err != nil {
		return cargoMember{}, err
	}
	return cargoMember{file: cfg, name: name, doc: doc}, nil
}

// readCapped reads a file of at most maxManifestSize bytes; the rest of a larger file is ignored and the parse
// fails on the truncated text.
func readCapped(root fs.StatFS, path string) ([]byte, error) {
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, maxManifestSize))
}

// cargoRoot reads the root Cargo.toml. It returns nil, nil when the file is missing or unreadable, and the
// `Could not parse TOML` error when it is not valid TOML (the message carries the TOML library's text, which
// differs from Knope's). The document is a generic table: its shape is the caller's concern.
func cargoRoot(root fs.StatFS) (doc map[string]any, src []byte, err error) {
	src, err = readCapped(root, cargoTomlFile)
	if err != nil {
		return nil, nil, nil //nolint:nilerr // Knope: a Cargo.toml that cannot be read means no Cargo workspace
	}
	if err := gotoml.Unmarshal(src, &doc); err != nil {
		return nil, nil, diag.Newf("Could not parse TOML in Cargo.toml: %v", err).
			WithCode(codeCargoTOML).
			WithCause(err)
	}
	return doc, src, nil
}

// cargoMemberList is `workspace.members` when `workspace` is a standard table and `members` an array; nil
// otherwise. The generic decoder merges inline and standard tables, so the source text says which it is: Knope's
// `as_table` rejects `workspace = { ... }`, and an array of tables is not an array of strings either.
func cargoMemberList(doc map[string]any, src []byte) []any {
	if inlineWorkspace(src) {
		return nil
	}
	workspace, _ := doc["workspace"].(map[string]any)
	members, _ := workspace["members"].([]any)
	return members
}

// inlineWorkspace reports whether the top level of src defines `workspace` as an inline table, or `workspace.members`
// as an array of tables (`[[workspace.members]]`).
func inlineWorkspace(src []byte) bool {
	p := unstable.Parser{}
	p.Reset(src)
	for p.NextExpression() {
		expr := p.Expression()
		switch expr.Kind {
		case unstable.KeyValue:
			if keys := keyParts(expr.Key()); len(keys) == 1 && keys[0] == "workspace" &&
				expr.Value().Kind == unstable.InlineTable {
				return true
			}
		case unstable.ArrayTable:
			if keys := keyParts(expr.Key()); len(keys) == 2 && keys[0] == "workspace" && keys[1] == "members" {
				return true
			}
		}
	}
	return false
}

// cargoContainsDependency is Knope's contains_dependency: the name is a key of `dependencies`, `dev-dependencies`
// or `workspace.dependencies`. Build and target dependencies do not count, nor does a renamed `package = "..."`.
func cargoContainsDependency(doc map[string]any, name string) bool {
	for _, key := range []string{"dependencies", "dev-dependencies"} {
		if deps, ok := doc[key].(map[string]any); ok {
			if _, found := deps[name]; found {
				return true
			}
		}
	}
	workspace, _ := doc["workspace"].(map[string]any)
	deps, _ := workspace["dependencies"].(map[string]any)
	_, found := deps[name]
	return found
}
