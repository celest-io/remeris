package versioning

import (
	"slices"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

// defaultName is how the default package prints.
const defaultName = "default"

// PackageName names a package: either the default package (a bare [package] table, no tag prefix) or a custom
// one ([packages.X], tag prefix "X/"). The zero value is the default package.
type PackageName struct{ custom *string }

// DefaultName returns the name of the default package.
func DefaultName() PackageName { return PackageName{} }

// CustomName returns the name of the custom package n.
func CustomName(n string) PackageName { return PackageName{custom: &n} }

// Custom returns the custom name, or nil for the default package. The result is a copy.
func (n PackageName) Custom() *string {
	if n.custom == nil {
		return nil
	}
	c := *n.custom
	return &c
}

// String returns the custom name, or "default".
func (n PackageName) String() string {
	if n.custom == nil {
		return defaultName
	}
	return *n.custom
}

// Equal reports whether two names are the same package name. A custom package called "default" is not the
// default package.
func (n PackageName) Equal(o PackageName) bool {
	if n.custom == nil || o.custom == nil {
		return n.custom == o.custom
	}
	return *n.custom == *o.custom
}

// Error codes of packages.
const (
	codeInconsistentVersions = "remeris::inconsistent_versions"
	codeFileNotFound         = "remeris::package::versioned_file_not_found"
	codeCargoLockNoDep       = "remeris::package::cargo_lock_no_dependency"
)

// ErrCargoLockNoDependency is returned when a Cargo.lock has no dependency and the package has no Cargo.toml with
// a package.name to default it to. Match it with errors.Is.
var ErrCargoLockNoDependency error = diag.New("Cargo.lock must specify a dependency").
	WithCode(codeCargoLockNoDep).
	WithHelp("To use `Cargo.lock` in `versioned_files`, you must either manually specify `dependency` or define a " +
		"`Cargo.toml` with a `package.name` in the same array.")

// Package is a logical package: the versioned files that hold its version, its version history and the settings
// that depend on its name. Release notes and the changelog are added by a later change; this is the seam.
type Package struct {
	// Name is the package name; it decides the tag prefix.
	Name PackageName
	// Files are the validated configs, with the Cargo.lock dependency filled in, in the configured order.
	Files []versionedfile.Config
	// Versions is the version history: tags, plus the version found in files.
	Versions semver.PackageVersions
	// GoVersioning is how go.mod files handle major versions.
	GoVersioning versionedfile.GoVersioning
	// Scopes limits the conventional commit scopes that belong to the package; nil means all scopes.
	Scopes []string
	// Override is the version given with --override-version, or nil. BumpVersion uses it as given; PrepareRelease
	// takes it once (F13).
	Override *semver.Version
}

// TakeOverride returns the override and clears it, so that it is used once.
func (p *Package) TakeOverride() *semver.Version {
	o := p.Override
	p.Override = nil
	return o
}

// Clone returns a deep copy of the package: changing the copy (SetVersion, the history, the slices) does not
// change p. Workflows run on clones so that one cannot see another's changes (--validate).
func (p *Package) Clone() *Package {
	c := *p
	c.Files = slices.Clone(p.Files)
	for i, f := range c.Files {
		if f.Dependency != nil {
			d := *f.Dependency
			c.Files[i].Dependency = &d
		}
		c.Files[i].Regex = slices.Clone(f.Regex)
	}
	c.Versions = p.Versions.Clone()
	c.Scopes = slices.Clone(p.Scopes)
	if p.Name.custom != nil {
		c.Name = CustomName(*p.Name.custom)
	}
	return &c
}

func findFile(all []versionedfile.File, path string) (versionedfile.File, bool) {
	idx := slices.IndexFunc(all, func(f versionedfile.File) bool { return f.Path() == path })
	if idx < 0 {
		return nil, false
	}
	return all[idx], true
}

// ValidateVersionedFiles checks the files of one package (spec F7.2) and returns the configs with defaults filled
// in, plus the package version according to the files, if any. configs are the package's entries in order and
// all holds every parsed file, looked up by path. Neither is modified.
//
// A Cargo.lock without dependency gets the package.name of the first Cargo.toml of the package that is not a
// dependency entry. Entries with a dependency do not count toward the package version; the others must all
// have the same version, build metadata included. A file that cannot hold a version (versionedfile.ErrNoVersion)
// and has no dependency is an error, as in Knope.
func ValidateVersionedFiles(
	configs []versionedfile.Config, all []versionedfile.File,
) ([]versionedfile.Config, *semver.Version, error) {
	files := make([]versionedfile.File, len(configs))
	for i, cfg := range configs {
		f, ok := findFile(all, cfg.Path)
		if !ok {
			return nil, nil, diag.New("Versioned file not found: " + cfg.Path).
				WithCode(codeFileNotFound).
				WithHelp("this is likely a bug, please report it")
		}
		files[i] = f
	}

	var (
		first     *semver.Version
		firstPath string
		validated = make([]versionedfile.Config, 0, len(configs))
	)
	for i, cfg := range configs {
		cfg, err := validateDependency(cfg, configs, all)
		if err != nil {
			return nil, nil, err
		}
		validated = append(validated, cfg)
		if cfg.Dependency != nil {
			continue
		}
		v, err := files[i].Version()
		if err != nil {
			return nil, nil, err
		}
		switch {
		case first == nil:
			first, firstPath = &v, cfg.Path
		case !first.Equal(v):
			return nil, nil, diag.Newf("Found inconsistent versions in package: %s had %s and %s had %s",
				firstPath, first, cfg.Path, v).
				WithCode(codeInconsistentVersions).
				WithHelp("All files in a package must have the same version")
		}
	}
	return validated, first, nil
}

// validateDependency applies the dependency rules of cfg's format (spec F7.2 step 2). It returns a copy of cfg.
func validateDependency(
	cfg versionedfile.Config, configs []versionedfile.Config, all []versionedfile.File,
) (versionedfile.Config, error) {
	if cfg.Dependency != nil {
		d := *cfg.Dependency
		cfg.Dependency = &d
	}
	switch {
	case cfg.Format == versionedfile.CargoLock && cfg.Dependency == nil:
		name, ok := cargoPackageName(configs, all)
		if !ok {
			return cfg, ErrCargoLockNoDependency
		}
		cfg.Dependency = &name
	case cfg.Dependency != nil && !cfg.Format.SupportsDependency():
		return cfg, versionedfile.UnsupportedDependency(cfg.Path)
	}
	return cfg, nil
}

// cargoPackageName is the package.name of the first Cargo.toml entry without dependency that has one.
func cargoPackageName(configs []versionedfile.Config, all []versionedfile.File) (string, bool) {
	for _, cfg := range configs {
		if cfg.Format != versionedfile.Cargo || cfg.Dependency != nil {
			continue
		}
		f, ok := findFile(all, cfg.Path)
		if !ok {
			continue
		}
		if n, ok := f.(versionedfile.PackageNamer); ok {
			if name, found := n.PackageName(); found {
				return name, true
			}
		}
	}
	return "", false
}

// NewPackage builds a package from its configs and the parsed files (the pure half of spec F7.1). gitTags must be
// newest first. The history starts from the tags of the package; a version found in the files is added to it
// (Update), so a file ahead of the latest tag wins. The inputs are not modified.
func NewPackage(
	name PackageName,
	configs []versionedfile.Config,
	all []versionedfile.File,
	gitTags []string,
	goVersioning versionedfile.GoVersioning,
	scopes []string,
) (*Package, error) {
	validated, fromFiles, err := ValidateVersionedFiles(configs, all)
	if err != nil {
		return nil, err
	}
	versions := semver.FromTags(name.custom, gitTags)
	if fromFiles != nil {
		versions.Update(*fromFiles)
	}
	return &Package{
		Name:         name,
		Files:        validated,
		Versions:     versions,
		GoVersioning: goVersioning,
		Scopes:       slices.Clone(scopes),
	}, nil
}

// LatestVersion returns the package's current version: the newest of the version in the files, the latest stable
// tag and the pre-release tags after it (spec F7.3). The bool is false when there is no version at all.
func (p *Package) LatestVersion() (semver.Version, bool) { return p.Versions.Latest() }

// SetVersion applies v to every file of all that this package lists, once for each matching config in config
// order (spec F7.4), and returns all files in their original order; files that no config matches are returned as
// they were. On success the history is reset to v alone. On error the package is unchanged. gv is passed to the
// files, so callers can force versionedfile.BumpMajor for a version override.
func (p *Package) SetVersion(
	v semver.Version, gv versionedfile.GoVersioning, all []versionedfile.File,
) ([]versionedfile.File, error) {
	out := make([]versionedfile.File, len(all))
	for i, file := range all {
		for _, cfg := range p.Files {
			if cfg.Path != file.Path() {
				continue
			}
			var err error
			if file, err = file.SetVersion(v, cfg.Dependency, gv); err != nil {
				return nil, err
			}
		}
		out[i] = file
	}
	p.Versions = semver.FromVersion(v)
	return out, nil
}

// ReleaseTag returns the Git tag of version v of the package: "v{v}", or "{name}/v{v}" for a custom name (F7.5).
func (p *Package) ReleaseTag(v semver.Version) string { return action.ReleaseTag(v, p.Name.custom) }

// IsReleaseTag reports whether tag is a release tag of this package (F7.5).
func (p *Package) IsReleaseTag(tag string) bool { return action.IsReleaseTag(tag, p.Name.custom) }
