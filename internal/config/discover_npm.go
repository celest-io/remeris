package config

import (
	"io/fs"
	"path"

	"github.com/celest-io/remeris/internal/glob"
)

// npmWorkspaces is the npm detector (spec F20.3, Knope's npm_workspaces). Milestone M10 PR 64 replaces its body; it
// edits this file, discover_npm_test.go and testdata/script/discovery/npm_*.txtar only.
//
// Until then it returns the M10 error for a workspace root and no packages otherwise.
func npmWorkspaces(root fs.StatFS) ([]PackageConfig, error) {
	if isNPMWorkspace(root) {
		return nil, errDiscoveryNotImplemented("npm")
	}
	return nil, nil
}

// isNPMWorkspace: package.json has a `workspaces` array of which at least one string pattern matches a directory
// with a readable package.json (Knope's npm_workspaces). Knope ignores the object form, and with no such directory
// it falls back to the default package. A pattern that does not compile is an error in Knope, so it counts here.
func isNPMWorkspace(root fs.StatFS) bool {
	doc := readJSON(root, "package.json")
	workspaces, _ := doc["workspaces"].([]any)
	for _, p := range workspaces {
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
