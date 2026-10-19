package config

import (
	"bytes"
	"encoding/json"
	"io/fs"

	gotoml "github.com/pelletier/go-toml/v2"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	codeDiscoveryNotImplemented = "remeris::discovery_not_implemented"
	defaultChangelog            = "CHANGELOG.md"
)

// discoverPackages finds the packages of the project at the root of root when knope.toml declares none (spec F20.1,
// Knope's Package::find_in_working_dir).
//
// Cargo, npm and Deno workspaces are milestone M10. Releasing a workspace root as one package would be wrong (Knope
// releases the members), so a root that looks like a workspace is an error. Otherwise the single default package
// holds every default file name that exists, in table order, and CHANGELOG.md when it exists. No such file means
// no packages.
func discoverPackages(root fs.StatFS) ([]PackageConfig, error) {
	if kind := workspaceKind(root); kind != "" {
		return nil, diag.Newf("Package discovery for %s workspaces is not implemented yet (milestone M10)", kind).
			WithCode(codeDiscoveryNotImplemented).
			WithHelp("Declare the packages in " + KnopeFile + ", or wait for milestone M10.")
	}

	var files []versionedfile.Config
	for _, cfg := range versionedfile.Defaults() {
		if _, err := root.Stat(cfg.Path); err == nil {
			files = append(files, cfg)
		}
	}
	if len(files) == 0 {
		return nil, nil
	}
	pkg := PackageConfig{Name: versioning.DefaultName(), VersionedFiles: files}
	if _, err := root.Stat(defaultChangelog); err == nil {
		pkg.Changelog = defaultChangelog
	}
	return []PackageConfig{pkg}, nil
}

// workspaceKind names the kind of workspace the root of root declares, or returns "" for none. The detectors run in
// Knope's order: Cargo, npm, Deno. Files that cannot be read or parsed are not workspaces here; loading them as
// versioned files reports the problem.
func workspaceKind(root fs.StatFS) string {
	switch {
	case isCargoWorkspace(root):
		return "Cargo"
	case isNPMWorkspace(root):
		return "npm"
	case isDenoWorkspace(root):
		return "Deno"
	}
	return ""
}

// isCargoWorkspace: Cargo.toml has a `workspace.members` array. An empty array has no members, so Knope finds
// no packages and falls back to the default package.
func isCargoWorkspace(root fs.StatFS) bool {
	data, err := fs.ReadFile(root, "Cargo.toml")
	if err != nil {
		return false
	}
	var doc struct {
		Workspace struct {
			Members []any `toml:"members"`
		} `toml:"workspace"`
	}
	return gotoml.Unmarshal(data, &doc) == nil && len(doc.Workspace.Members) > 0
}

// isNPMWorkspace: package.json has a `workspaces` array. Knope ignores the object form.
func isNPMWorkspace(root fs.StatFS) bool {
	data, err := fs.ReadFile(root, "package.json")
	if err != nil {
		return false
	}
	var doc struct {
		Workspaces []any `json:"workspaces"`
	}
	return json.Unmarshal(data, &doc) == nil && len(doc.Workspaces) > 0
}

// isDenoWorkspace: deno.json or deno.jsonc has a `workspace` array or object with members.
func isDenoWorkspace(root fs.StatFS) bool {
	for _, name := range []string{"deno.json", "deno.jsonc"} {
		data, err := fs.ReadFile(root, name)
		if err != nil {
			continue
		}
		var doc struct {
			Workspace json.RawMessage `json:"workspace"`
		}
		if json.Unmarshal(stripJSONC(data), &doc) != nil || len(doc.Workspace) == 0 {
			continue
		}
		var list []any
		var obj map[string]any
		if json.Unmarshal(doc.Workspace, &list) == nil && len(list) > 0 ||
			json.Unmarshal(doc.Workspace, &obj) == nil && len(obj) > 0 {
			return true
		}
	}
	return false
}

// stripJSONC removes comments and trailing commas, which deno.jsonc allows, so encoding/json can read it.
func stripJSONC(in []byte) []byte {
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); i++ {
		c := in[i]
		switch {
		case c == '"':
			start := i
			for i++; i < len(in) && in[i] != '"'; i++ {
				if in[i] == '\\' {
					i++
				}
			}
			end := min(i+1, len(in))
			out = append(out, in[start:end]...)
		case c == '/' && i+1 < len(in) && in[i+1] == '/':
			for i < len(in) && in[i] != '\n' {
				i++
			}
			out = append(out, '\n')
		case c == '/' && i+1 < len(in) && in[i+1] == '*':
			end := bytes.Index(in[i+2:], []byte("*/"))
			if end < 0 {
				return out
			}
			i += end + 3
			out = append(out, ' ')
		case c == '}' || c == ']':
			trimmed := bytes.TrimRight(out, " \t\r\n")
			if n := len(trimmed); n > 0 && trimmed[n-1] == ',' {
				out = trimmed[:n-1]
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}
