package config

import (
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"

	gotoml "github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

// Error codes of Cargo workspace discovery (Knope's CargoWorkspaceError, and the size cap).
const (
	codeCargoTOML          = "remeris::workspace::toml"
	codeCargoMembers       = "remeris::workspace::members"
	codeCargoNoPackageName = "remeris::workspace::no_package_name"
	codeCargoTooLarge      = "remeris::workspace::too_large"
	codeCargoRead          = "remeris::fs::read"
)

const (
	cargoTomlFile = "Cargo.toml"
	cargoLockFile = "Cargo.lock"
)

// cargoMember is a workspace member: its manifest file, package name and the names it depends on (the keys of
// `dependencies`, `dev-dependencies` and `workspace.dependencies`). The parsed document is not kept.
type cargoMember struct {
	file versionedfile.Config
	name string
	deps map[string]struct{}
}

// cargoWorkspace is the Cargo detector (spec F20.2, Knope's cargo_workspace_members).
//
// Like Knope it reads ./Cargo.toml only if the file can be read (a missing file is no error), and a file that does
// not parse is an error (see cargoRoot). A `[workspace]` standard table with a `members` array makes one package per
// member, named after its `package.name`, with the member's commit scope. Members are paths inside the workspace
// (`<member>/Cargo.toml`, no globs, as in Knope). Its versioned files are, in member order, the member's own manifest
// and a dependency entry for each other member that depends on it, then the root manifest if it depends on it, then
// Cargo.lock if it exists.
func cargoWorkspace(root fs.StatFS) ([]PackageConfig, error) {
	doc, src, err := cargoRoot(root)
	if err != nil {
		return nil, err
	}
	rawMembers := cargoMemberList(doc, src)
	if len(rawMembers) == 0 {
		return nil, nil
	}
	rootDeps := cargoDependencyNames(doc)
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
		manifestPath, ok := cargoMemberManifest(dir)
		if !ok {
			return nil, diag.New("The Cargo workspace member " + dir + " must be a path inside the workspace").
				WithCode(codeCargoMembers).
				WithHelp("Use a relative path without `..`; globs are not supported.")
		}
		member, err := readCargoMember(root, manifestPath)
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
			case hasKey(other.deps, member.name):
				files = append(files, withDependency(other.file, member.name))
			}
		}
		if hasKey(rootDeps, member.name) {
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

func hasKey(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}

func withDependency(cfg versionedfile.Config, name string) versionedfile.Config {
	cfg.Dependency = &name
	return cfg
}

// cargoMemberManifest is the manifest path of a member, cleaned with path.Clean (`./a` and `a/` are `a`, `.` is the
// root manifest). A member that is empty, absolute or has a `..` component is refused: osRoot would read outside the
// repository.
func cargoMemberManifest(member string) (string, bool) {
	if member == "" || strings.HasPrefix(member, "/") || filepath.IsAbs(member) || filepath.VolumeName(member) != "" {
		return "", false
	}
	if slices.Contains(strings.Split(member, "/"), "..") {
		return "", false
	}
	return path.Join(path.Clean(member), cargoTomlFile), true
}

// readCargoMember reads and parses one member manifest.
func readCargoMember(root fs.StatFS, manifestPath string) (cargoMember, error) {
	data, err := readManifestFile(root, manifestPath)
	if err != nil {
		return cargoMember{}, cargoReadError(manifestPath, err)
	}
	var doc map[string]any
	if err := gotoml.Unmarshal(data, &doc); err != nil {
		return cargoMember{}, diag.New("Could not parse TOML in " + manifestPath + ": " + err.Error()).
			WithCode(codeCargoTOML).
			WithCause(err)
	}
	pkg, _ := doc["package"].(map[string]any)
	name, ok := pkg["name"].(string)
	if !ok {
		return cargoMember{}, diag.New("Could not find a package.name in " + manifestPath).
			WithCode(codeCargoNoPackageName)
	}
	cfg, err := versionedfile.NewConfig(manifestPath, nil, nil)
	if err != nil {
		return cargoMember{}, err
	}
	return cargoMember{file: cfg, name: name, deps: cargoDependencyNames(doc)}, nil
}

// cargoReadError is the diagnostic of a manifest that cannot be read: the size cap or Knope's fs::read.
func cargoReadError(manifestPath string, err error) error {
	if errors.Is(err, errManifestTooLarge) {
		return diag.New("Cargo manifest " + manifestPath + " is larger than 8 MiB").
			WithCode(codeCargoTooLarge).
			WithCause(err)
	}
	cause := err
	var pe *fs.PathError
	if errors.As(err, &pe) {
		cause = pe.Err
	}
	return diag.New("Error reading from " + manifestPath + ": " + cause.Error()).
		WithCode(codeCargoRead).
		WithHelp("Make sure you have permission to read this file.").
		WithCause(err)
}

// cargoRoot reads the root Cargo.toml. It returns nil, nil when the file is missing or unreadable, and the
// `Could not parse TOML` error when it is not valid TOML (the message carries the TOML library's text, which
// differs from Knope's) or the file is too large. The document is a generic table: its shape is the caller's concern.
func cargoRoot(root fs.StatFS) (doc map[string]any, src []byte, err error) {
	src, err = readManifestFile(root, cargoTomlFile)
	if errors.Is(err, errManifestTooLarge) {
		return nil, nil, cargoReadError(cargoTomlFile, err)
	}
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

// inlineWorkspace reports whether src defines `workspace` as an inline table before its first table header, or
// `workspace.members` as an array of tables (`[[workspace.members]]`). Key-values after a header belong to that
// table (`[dependencies]` with a crate called workspace is no business of ours).
func inlineWorkspace(src []byte) bool {
	p := unstable.Parser{}
	p.Reset(src)
	topLevel := true
	for p.NextExpression() {
		expr := p.Expression()
		switch expr.Kind {
		case unstable.Table:
			topLevel = false
		case unstable.ArrayTable:
			topLevel = false
			if keys := keyParts(expr.Key()); len(keys) == 2 && keys[0] == "workspace" && keys[1] == "members" {
				return true
			}
		case unstable.KeyValue:
			if !topLevel {
				continue
			}
			if keys := keyParts(expr.Key()); len(keys) == 1 && keys[0] == "workspace" &&
				expr.Value().Kind == unstable.InlineTable {
				return true
			}
		}
	}
	return false
}

// cargoDependencyNames is the set of Knope's contains_dependency: the keys of `dependencies`, `dev-dependencies`
// and `workspace.dependencies`. Build and target dependencies do not count, nor does a renamed `package = "..."`.
func cargoDependencyNames(doc map[string]any) map[string]struct{} {
	names := map[string]struct{}{}
	workspace, _ := doc["workspace"].(map[string]any)
	for _, deps := range []any{doc["dependencies"], doc["dev-dependencies"], workspace["dependencies"]} {
		table, _ := deps.(map[string]any)
		for name := range table {
			names[name] = struct{}{}
		}
	}
	return names
}
