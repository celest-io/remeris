package config

import (
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/tomlwrite"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

// Error codes of the configuration document.
const (
	codeDocumentStep     = "remeris::config::document_step"
	codeDocumentPackages = "remeris::config::document_packages"
)

// Document returns the configuration as the TOML document that `--generate` and `--upgrade` write (spec F20.7, F21).
// It maps the loaded configuration, with the packages that were discovered, the default workflows and the detected
// forges, to what Knope's `toml::to_string` prints for the same data, in the same order:
//
//	[changes]  [package] or [packages.<name>]  [[workflows]]  [github]  [gitea]  [jira]  [release_notes]
//
// Knope's writer leaves out `[jira]` and `[release_notes]` (its SimpleConfig has no such fields), which loses a
// user's settings on `--upgrade`; remeris keeps them (testdata/deviations.md). The remeris-only `api_url` and
// `upload_url` of `[github]` are kept for the same reason.
//
// Keys are left out as Knope's serde attributes leave them out: unset options, `false` flags and empty lists as
// listed with each writer below. The document is built only with the tomlwrite builders, so any string, hostile or
// not, ends up as data. A configuration that cannot be written (a step of a type the writer does not know, an
// invalid variable, two packages with one name) is an error and never an empty document.
func (c *Config) Document() (*tomlwrite.Doc, error) {
	if c == nil {
		return nil, diag.New("There is no configuration to write").WithCode(codeDocumentPackages)
	}
	doc := tomlwrite.NewDoc()

	if c.Changes != nil && c.Changes.IgnoreConventionalCommits {
		// Knope also writes ignore_conventional_merge_commits when it is set; the configuration model has no such
		// key yet, so there is nothing to write for it.
		changes := tomlwrite.NewTable()
		changes.Set("ignore_conventional_commits", tomlwrite.Bool(true))
		doc.Set("changes", changes)
	}

	if err := c.setPackages(doc); err != nil {
		return nil, err
	}

	workflows := make([]*tomlwrite.Table, 0, len(c.Workflows))
	for _, wf := range c.Workflows {
		t, err := workflowTable(wf.Name, wf.HelpText, wf.Steps)
		if err != nil {
			return nil, err
		}
		workflows = append(workflows, t)
	}
	// An empty array is written as `workflows = []`, as Knope does.
	doc.Set("workflows", tomlwrite.NewArrayOfTables(workflows...))

	if g := c.GitHub; g != nil {
		t := tomlwrite.NewTable()
		t.Set("owner", tomlwrite.String(g.Owner))
		t.Set("repo", tomlwrite.String(g.Repo))
		setNonEmpty(t, "api_url", g.APIURL)
		setNonEmpty(t, "upload_url", g.UploadURL)
		doc.Set("github", t)
	}
	if g := c.Gitea; g != nil {
		t := tomlwrite.NewTable()
		t.Set("owner", tomlwrite.String(g.Owner))
		t.Set("repo", tomlwrite.String(g.Repo))
		t.Set("host", tomlwrite.String(g.Host))
		doc.Set("gitea", t)
	}
	if j := c.Jira; j != nil {
		t := tomlwrite.NewTable()
		t.Set("url", tomlwrite.String(j.URL))
		t.Set("project", tomlwrite.String(j.Project))
		doc.Set("jira", t)
	}
	if r := c.ReleaseNotes; r != nil {
		t := tomlwrite.NewTable()
		t.Set("change_templates", stringArray(r.ChangeTemplates))
		doc.Set("release_notes", t)
	}
	return doc, nil
}

// setPackages writes `[package]` for the one unnamed default package and `[packages.<name>]` for every named package
// and for two or more packages. No package at all writes neither. Knope writes a lone named package as `[package]`,
// which loads back as the default package: its tags change from `name/vX` to `vX`. Remeris keeps the name.
func (c *Config) setPackages(doc *tomlwrite.Doc) error {
	switch pkgs := c.packages; {
	case len(pkgs) == 0:
		return nil
	case len(pkgs) == 1 && pkgs[0].Name.Custom() == nil:
		doc.Set("package", packageTable(pkgs[0]))
	default:
		table := tomlwrite.NewTable()
		for _, p := range pkgs {
			name := p.Name.String() // the default package is called "default", as in Knope
			if _, dup := table.Get(name); dup {
				return diag.Newf("Two packages are called %q", name).
					WithCode(codeDocumentPackages).
					WithHelp("Give each package its own name under `packages`.")
			}
			table.Set(name, packageTable(p))
		}
		doc.Set("packages", table)
	}
	return nil
}

// packageTable maps one package. Like Knope's serde attributes, it leaves out empty `versioned_files` and
// `extra_changelog_sections`, no `changelog`, no `scopes` (nil; an empty list is kept), no `assets` and a false
// `ignore_go_major_versioning`.
func packageTable(p PackageConfig) *tomlwrite.Table {
	t := tomlwrite.NewTable()
	if len(p.VersionedFiles) > 0 {
		files := make([]tomlwrite.Value, 0, len(p.VersionedFiles))
		for _, f := range p.VersionedFiles {
			files = append(files, versionedFileValue(f))
		}
		// An array of only tables is written as `[[package.versioned_files]]`, a mixed one inline.
		t.Set("versioned_files", tomlwrite.NewArray(files...))
	}
	setNonEmpty(t, "changelog", p.Changelog)
	if p.Scopes != nil {
		t.Set("scopes", stringArray(p.Scopes))
	}
	if len(p.ExtraChangelogSections) > 0 {
		sections := make([]*tomlwrite.Table, 0, len(p.ExtraChangelogSections))
		for _, s := range p.ExtraChangelogSections {
			st := tomlwrite.NewTable()
			st.Set("name", tomlwrite.String(s.Name))
			// Knope always writes both lists, empty or not.
			st.Set("footers", stringArray(s.Footers))
			st.Set("types", stringArray(s.Types))
			sections = append(sections, st)
		}
		t.Set("extra_changelog_sections", tomlwrite.NewArrayOfTables(sections...))
	}
	if p.Assets != nil {
		t.Set("assets", assetsValue(p.Assets))
	}
	if p.IgnoreGoMajorVersioning {
		t.Set("ignore_go_major_versioning", tomlwrite.Bool(true))
	}
	return t
}

// versionedFileValue is Knope's VersionedFile::from: a regex wins over a dependency, a lone regex is written as a
// string, and a file with neither is its bare path.
func versionedFileValue(f versionedfile.Config) tomlwrite.Value {
	switch {
	case f.Regex != nil:
		t := tomlwrite.NewTable()
		t.Set("path", tomlwrite.String(f.Path))
		if len(f.Regex) == 1 {
			t.Set("regex", tomlwrite.String(f.Regex[0]))
		} else {
			t.Set("regex", stringArray(f.Regex))
		}
		return t
	case f.Dependency != nil:
		t := tomlwrite.NewTable()
		t.Set("path", tomlwrite.String(f.Path))
		t.Set("dependency", tomlwrite.String(*f.Dependency))
		return t
	}
	return tomlwrite.String(f.Path)
}

// assetsValue writes a glob as a string and a list as an array of `{ path, name }` tables, with no `name` when
// there is none.
func assetsValue(a *Assets) tomlwrite.Value {
	if a.IsGlob {
		return tomlwrite.String(a.Glob)
	}
	assets := make([]*tomlwrite.Table, 0, len(a.List))
	for _, asset := range a.List {
		t := tomlwrite.NewTable()
		t.Set("path", tomlwrite.String(asset.Path))
		setNonEmpty(t, "name", asset.Name)
		assets = append(assets, t)
	}
	return tomlwrite.NewArrayOfTables(assets...)
}

// setNonEmpty stores s under key unless it is empty, which is how the configuration says "not set".
func setNonEmpty(t *tomlwrite.Table, key, s string) {
	if s != "" {
		t.Set(key, tomlwrite.String(s))
	}
}

// stringArray is an inline array of strings; a nil slice is the empty array.
func stringArray(items []string) *tomlwrite.Array {
	a := tomlwrite.NewArray()
	for _, s := range items {
		a.Append(tomlwrite.String(s))
	}
	return a
}
