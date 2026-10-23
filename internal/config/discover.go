package config

import (
	"io/fs"

	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const defaultChangelog = "CHANGELOG.md"

// A detector finds the packages of one kind of workspace at the root of root (Knope's cargo_workspace_members,
// npm_workspaces and deno_workspaces). It returns no packages and no error when the root is not such a workspace, or
// has no member Knope can use; the framework then tries the next detector and finally the default package. A
// returned error stops the discovery, as in Knope.
//
// A detector reads the file system only through root, and does not touch the other detectors' files.
//
// Each detector lives in its own file (cargoWorkspace in discover_cargo.go, npmWorkspaces in discover_npm.go,
// denoWorkspace in discover_deno.go) with its tests in discover_<kind>_test.go and its golden scripts in
// testdata/script/discovery/<kind>_*.txtar. Shared helpers are in discover_json.go and discover_read.go.
type detector func(root fs.StatFS) ([]PackageConfig, error)

// detectors run in Knope's order: Cargo, npm, Deno. The results are concatenated.
var detectors = []detector{cargoWorkspace, npmWorkspaces, denoWorkspace}

// discoverPackages finds the packages of the project at the root of root when knope.toml declares none (spec F20.1,
// Knope's Package::find_in_working_dir).
//
// Every detector runs; if they found packages, those are returned. Otherwise the single default package holds every
// default file name that exists, in table order, and CHANGELOG.md when it exists. No such file means no packages.
func discoverPackages(root fs.StatFS) ([]PackageConfig, error) {
	return discoverWith(root, detectors)
}

func discoverWith(root fs.StatFS, list []detector) ([]PackageConfig, error) {
	var found []PackageConfig
	for _, detect := range list {
		pkgs, err := detect(root)
		if err != nil {
			return nil, err
		}
		found = append(found, pkgs...)
	}
	if len(found) > 0 {
		return found, nil
	}
	return defaultPackage(root), nil
}

// defaultPackage is the fallback of discoverWith: nil when no default file exists.
func defaultPackage(root fs.StatFS) []PackageConfig {
	var files []versionedfile.Config
	for _, cfg := range versionedfile.Defaults() {
		if _, err := root.Stat(cfg.Path); err == nil {
			files = append(files, cfg)
		}
	}
	if len(files) == 0 {
		return nil
	}
	pkg := PackageConfig{Name: versioning.DefaultName(), VersionedFiles: files}
	if _, err := root.Stat(defaultChangelog); err == nil {
		pkg.Changelog = defaultChangelog
	}
	return []PackageConfig{pkg}
}
