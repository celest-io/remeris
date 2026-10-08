package config

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

// Error codes of package validation.
const (
	codeConflictingPackages = "remeris::config::conflicting_packages"
	codeEmptyPackages       = "remeris::config::empty_packages"
	codeGiteaAssets         = "remeris::config::gitea_asset_uploads"
	codeMissingFile         = "remeris::config::missing_versioned_file"
)

// PackageConfig is the validated configuration of one package, ready for the versioning core to load.
type PackageConfig struct {
	// Name is versioning.DefaultName() for `[package]` and versioning.CustomName(x) for `[packages.x]`.
	Name versioning.PackageName
	// VersionedFiles are the validated entries of `versioned_files`, in declaration order. Every file exists.
	VersionedFiles []versionedfile.Config
	// Changelog is the relative path of the changelog, or "" if there is none.
	Changelog string
	// Scopes limits the conventional commit scopes of the package. nil means all scopes; an empty non-nil slice
	// means `scopes = []`.
	Scopes []string
	// ExtraChangelogSections are the `extra_changelog_sections` entries (F12.1).
	ExtraChangelogSections []ExtraSection
	// Assets are the release assets, decoded only (used by the forge steps in M7). nil means none.
	Assets []Asset
	// IgnoreGoMajorVersioning is the `ignore_go_major_versioning` flag.
	IgnoreGoMajorVersioning bool
}

// PackageConfigs returns the packages of the file in declaration order: `[package]` alone, or the entries of
// `[packages]`. It is empty when the file declares none (package discovery is a later milestone).
func (c *Config) PackageConfigs() []PackageConfig { return c.packages }

// osRoot is the working directory. Unlike os.DirFS it accepts `..`, which Knope allows in `versioned_files`.
type osRoot struct{}

func (osRoot) Open(name string) (fs.File, error)     { return os.Open(filepath.FromSlash(name)) }
func (osRoot) Stat(name string) (fs.FileInfo, error) { return os.Stat(filepath.FromSlash(name)) }

// validatePackages checks `package` and `packages` in the order of spec F2.2 and builds the PackageConfigs.
func validatePackages(
	r *raw, index *tomlIndex, src string, root fs.StatFS, ordered []NamedPackage,
) ([]PackageConfig, error) {
	hasPackages := index.root.at("packages") != nil
	if r.Package != nil && hasPackages {
		first := index.Keys("packages")
		if len(first) == 0 {
			return nil, emptyPackages()
		}
		var labels []diag.Label
		if s, ok := index.Span("package"); ok {
			labels = append(labels, diag.Label{Start: s.Start, End: s.End, Text: "`package` defined here"})
		}
		if s, ok := index.Span("packages", first[0]); ok {
			labels = append(labels, diag.Label{Start: s.Start, End: s.End, Text: "`packages` defined here"})
		}
		return nil, diag.New("You cannot define both `packages` and `package`").
			WithCode(codeConflictingPackages).
			WithHelp("Add the `package` as a key under `packages` instead.").
			WithSource(src, labels...)
	}
	if hasPackages && len(ordered) == 0 {
		return nil, emptyPackages()
	}

	var out []PackageConfig
	if r.Package != nil {
		cfg, err := packageConfig(versioning.DefaultName(), *r.Package, root, src, index, "package")
		if err != nil {
			return nil, err
		}
		out = append(out, cfg)
	}
	for _, np := range ordered {
		cfg, err := packageConfig(versioning.CustomName(np.Name), np.Package, root, src, index, "packages", np.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, cfg)
	}

	if r.Gitea != nil {
		for _, p := range out {
			if p.Assets != nil {
				return nil, diag.New("Asset uploads for Gitea are not supported").
					WithCode(codeGiteaAssets).
					WithHelp("Remove the `[[package.assets]]` key from your config.")
			}
		}
	}
	return out, nil
}

func emptyPackages() error {
	return diag.New("The `packages` key is defined but does not contain any packages").
		WithCode(codeEmptyPackages).
		WithHelp("Add at least one package to the `packages` key.")
}

// packageConfig validates the versioned files of one package. where is the index path of the package table.
func packageConfig(
	name versioning.PackageName, p Package, root fs.StatFS, src string, index *tomlIndex, where ...any,
) (PackageConfig, error) {
	files := make([]versionedfile.Config, 0, len(p.VersionedFiles))
	for i, vf := range p.VersionedFiles {
		declared := func(err *diag.Error) error {
			s, ok := index.Span(append(append([]any(nil), where...), "versioned_files", i)...)
			if !ok {
				return err
			}
			return err.WithSource(src, diag.Label{Start: s.Start, End: s.End, Text: "Declared here"})
		}
		cfg, err := versionedfile.NewConfig(vf.Path, vf.Dependency, vf.Regex)
		if err != nil {
			return PackageConfig{}, declared(diag.New("Problem with versioned file").WithRelated(err))
		}
		if _, statErr := root.Stat(vf.Path); statErr != nil {
			return PackageConfig{}, declared(diag.New("File " + vf.Path + " does not exist").
				WithCode(codeMissingFile).
				WithHelp("Make sure the file exists and is accessible."))
		}
		files = append(files, cfg)
	}
	return PackageConfig{
		Name:                    name,
		VersionedFiles:          files,
		Changelog:               p.Changelog,
		Scopes:                  p.Scopes,
		ExtraChangelogSections:  p.ExtraChangelogSections,
		Assets:                  p.Assets,
		IgnoreGoMajorVersioning: p.IgnoreGoMajorVersioning,
	}, nil
}
