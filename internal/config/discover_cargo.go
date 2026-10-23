package config

import (
	"io/fs"

	gotoml "github.com/pelletier/go-toml/v2"

	"github.com/celest-io/remeris/internal/diag"
)

// codeCargoTOML is Knope's workspace::toml.
const codeCargoTOML = "remeris::workspace::toml"

// cargoWorkspace is the Cargo detector (spec F20.2, Knope's cargo_workspace_members). Milestone M10 PR 63 replaces
// its body; it edits this file, discover_cargo_test.go and testdata/script/discovery/cargo_*.txtar only.
//
// Like Knope it reads ./Cargo.toml only if the file can be read (a missing file is no error), and a file that does
// not parse is an error (see cargoRoot). Until the workspace members are built it returns the M10 error for a
// workspace root and no packages otherwise.
func cargoWorkspace(root fs.StatFS) ([]PackageConfig, error) {
	doc, err := cargoRoot(root)
	if err != nil {
		return nil, err
	}
	if hasCargoMembers(doc) {
		return nil, errDiscoveryNotImplemented("Cargo")
	}
	return nil, nil
}

// cargoRoot reads the root Cargo.toml. It returns nil, nil when the file is missing or unreadable, and the
// `Could not parse TOML` error when it is not valid TOML (the message carries the TOML library's text, which
// differs from Knope's). The document is a generic table: its shape is the caller's concern.
func cargoRoot(root fs.StatFS) (map[string]any, error) {
	data, err := fs.ReadFile(root, "Cargo.toml")
	if err != nil {
		return nil, nil //nolint:nilerr // Knope: a Cargo.toml that cannot be read means no Cargo workspace
	}
	var doc map[string]any
	if err := gotoml.Unmarshal(data, &doc); err != nil {
		return nil, diag.Newf("Could not parse TOML in Cargo.toml: %v", err).
			WithCode(codeCargoTOML).
			WithCause(err)
	}
	return doc, nil
}

// hasCargoMembers: the table has a `workspace.members` array that is not empty. An empty array has no members, so
// Knope finds no packages and falls back to the default package.
func hasCargoMembers(doc map[string]any) bool {
	workspace, _ := doc["workspace"].(map[string]any)
	members, _ := workspace["members"].([]any)
	return len(members) > 0
}
