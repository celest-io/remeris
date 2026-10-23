package config

import (
	"io/fs"

	gotoml "github.com/pelletier/go-toml/v2"
)

// cargoWorkspace is the Cargo detector (spec F20.2, Knope's cargo_workspace_members). Milestone M10 PR 63 replaces
// its body; it edits this file, discover_cargo_test.go and testdata/script/discovery/cargo_*.txtar only.
//
// Until then it returns the M10 error for a workspace root and no packages otherwise.
func cargoWorkspace(root fs.StatFS) ([]PackageConfig, error) {
	if isCargoWorkspace(root) {
		return nil, errDiscoveryNotImplemented("Cargo")
	}
	return nil, nil
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
