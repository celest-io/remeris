package cli

import (
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

// applyOverrides stores the --override-version values on the packages (spec F3.1).
//
// With one package the only value is a bare version. With several, each value is name=version and a package takes
// the first value that names it; names left over are an error, listed in the order given. Packages without a
// value keep a nil override.
func applyOverrides(pkgs []*versioning.Package, values []string) error {
	if len(values) == 0 || len(pkgs) == 0 {
		return nil
	}
	if len(pkgs) == 1 {
		if len(values) > 1 {
			return diag.New("the argument '--override-version <OVERRIDE_ONE_VERSION>' cannot be used multiple times")
		}
		v, err := semver.Parse(values[0])
		if err != nil {
			return err
		}
		pkgs[0].Override = &v
		return nil
	}

	type override struct {
		name    string
		version semver.Version
	}
	overrides := make([]override, 0, len(values))
	for _, s := range values {
		pkgName, versionString, ok := strings.Cut(s, "=")
		if !ok {
			return diag.New("package override should be formatted like package_name=version")
		}
		v, err := semver.Parse(versionString)
		if err != nil {
			return err
		}
		overrides = append(overrides, override{name: pkgName, version: v})
	}
	for _, pkg := range pkgs {
		pkg.Override = nil
		for i, o := range overrides {
			if pkg.Name.Equal(versioning.CustomName(o.name)) {
				v := o.version
				pkg.Override = &v
				overrides = append(overrides[:i], overrides[i+1:]...)
				break
			}
		}
	}
	if len(overrides) == 0 {
		return nil
	}
	names := make([]string, len(overrides))
	for i, o := range overrides {
		names[i] = o.name
	}
	return diag.New("Unknown package(s) to override: " + strings.Join(names, ", "))
}
