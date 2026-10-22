package config

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"path"
	"slices"

	gotoml "github.com/pelletier/go-toml/v2"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/glob"
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

// isNPMWorkspace: package.json has a `workspaces` array of which at least one string pattern matches a directory
// with a readable package.json (Knope's npm_workspaces). Knope ignores the object form, and with no such directory
// it falls back to the default package. A pattern that does not compile is an error in Knope, so it counts here.
func isNPMWorkspace(root fs.StatFS) bool {
	data, err := fs.ReadFile(root, "package.json")
	if err != nil {
		return false
	}
	var doc struct {
		Workspaces []any `json:"workspaces"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return false
	}
	for _, p := range doc.Workspaces {
		pattern, ok := p.(string)
		if !ok {
			continue
		}
		matches, err := glob.Glob(root, pattern)
		if err != nil {
			return true
		}
		for _, m := range matches {
			if readJSON(root, path.Join(m, "package.json")) != nil {
				return true
			}
		}
	}
	return false
}

// isDenoWorkspace follows Knope's deno_workspaces: it applies when the root has a deno.json or deno.jsonc and at
// least one folder of the workspace (the root, then each member) has a usable config: a deno.json(c) with a name and
// a version, or else a package.json with a name and a version. Having a `workspace` key is not enough. The member
// list is the `workspace` array or the `members` array of the object form.
func isDenoWorkspace(root fs.StatFS) bool {
	var cfg map[string]any
	for _, name := range []string{"deno.json", "deno.jsonc"} {
		if cfg = readJSONC(root, name); cfg != nil {
			break
		}
	}
	if cfg == nil {
		return false
	}
	if denoFolderUsable(root, ".") {
		return true
	}
	var members []any
	switch w := cfg["workspace"].(type) {
	case []any:
		members = w
	case map[string]any:
		members, _ = w["members"].([]any)
	}
	for _, m := range members {
		pattern, ok := m.(string)
		if !ok {
			continue
		}
		matches, err := glob.Glob(root, pattern)
		if err != nil {
			continue
		}
		if slices.ContainsFunc(matches, func(dir string) bool { return denoFolderUsable(root, dir) }) {
			return true
		}
	}
	return false
}

// denoFolderUsable reports whether dir holds a deno.json(c) with a name and version, or a package.json with both.
func denoFolderUsable(root fs.StatFS, dir string) bool {
	for _, name := range []string{"deno.json", "deno.jsonc"} {
		if hasNameAndVersion(readJSONC(root, path.Join(dir, name))) {
			return true
		}
	}
	return hasNameAndVersion(readJSON(root, path.Join(dir, "package.json")))
}

func hasNameAndVersion(doc map[string]any) bool {
	name, _ := doc["name"].(string)
	version, _ := doc["version"].(string)
	return name != "" && version != ""
}

// readJSON returns the object at name, or nil if the file is missing, unreadable or not a JSON object.
func readJSON(root fs.StatFS, name string) map[string]any {
	data, err := fs.ReadFile(root, name)
	if err != nil {
		return nil
	}
	var doc map[string]any
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	return doc
}

// readJSONC is readJSON for files that may hold comments and trailing commas.
func readJSONC(root fs.StatFS, name string) map[string]any {
	data, err := fs.ReadFile(root, name)
	if err != nil {
		return nil
	}
	var doc map[string]any
	if json.Unmarshal(stripJSONC(data), &doc) != nil {
		return nil
	}
	return doc
}

// stripJSONC removes comments and trailing commas, which deno.jsonc allows, so encoding/json can read it.
func stripJSONC(in []byte) []byte {
	in = bytes.TrimPrefix(in, []byte("\xef\xbb\xbf"))
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
