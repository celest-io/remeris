package config

import (
	"io/fs"
	"path"
	"slices"

	"github.com/celest-io/remeris/internal/glob"
)

// denoWorkspace is the Deno detector (spec F20.4, Knope's deno_workspaces). Milestone M10 PR 65a replaces its body;
// it edits this file, discover_deno_test.go and testdata/script/discovery/deno_*.txtar only.
//
// Until then it returns the M10 error for a workspace root and no packages otherwise.
func denoWorkspace(root fs.StatFS) ([]PackageConfig, error) {
	if isDenoWorkspace(root) {
		return nil, errDiscoveryNotImplemented("Deno")
	}
	return nil, nil
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
