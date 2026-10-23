package config

import (
	"io/fs"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	codeDiscoveryNotImplemented = "remeris::discovery_not_implemented"
	defaultChangelog            = "CHANGELOG.md"
)

// A detector finds the packages of one kind of workspace at the root of root (Knope's cargo_workspace_members,
// npm_workspaces and deno_workspaces). It returns no packages and no error when the root is not such a workspace, or
// has no member Knope can use; the framework then tries the next detector and finally the default package. A
// returned error stops the discovery, as in Knope.
//
// A detector reads the file system only through root, and does not touch the other detectors' files.
//
// To implement a detector, a milestone M10 PR replaces the body of its own function and edits nothing else in this
// package:
//   - cargoWorkspace in discover_cargo.go, npmWorkspaces in discover_npm.go, denoWorkspace in discover_deno.go;
//   - tests in discover_<kind>_test.go next to it (the framework tests of discover_test.go stay untouched), and the
//     golden scripts testdata/script/discovery/<kind>_*.txtar;
//   - shared helpers (readJSON, readJSONC) are in discover_json.go; a new helper used by one kind stays in its file.
//
// The stubs return the M10 error (errDiscoveryNotImplemented) for a root that looks like a workspace of their kind;
// a built detector drops that call, and the helper that decided it (isCargoWorkspace and so on) once unused.
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

// errDiscoveryNotImplemented is the error of the detectors that are not built yet (milestone M10): releasing a
// workspace root as one package would be wrong, since Knope releases the members.
func errDiscoveryNotImplemented(kind string) error {
	return diag.Newf("Package discovery for %s workspaces is not implemented yet (milestone M10)", kind).
		WithCode(codeDiscoveryNotImplemented).
		WithHelp("Declare the packages in " + KnopeFile + ", or wait for milestone M10.")
}
