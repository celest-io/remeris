// Package config loads knope.toml (spec F2).
//
// Milestone M0 decodes the whole top-level schema but only validates and uses the workflows.
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/celest-io/remeris/internal/brand"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/workflow"
	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	gotoml "github.com/pelletier/go-toml/v2"
)

// Source says where a Config came from, which decides the CLI flags that exist (spec F2.3, F3.1).
type Source int

const (
	// SourceDefault: there is no knope.toml, everything is generated.
	SourceDefault Source = iota
	// SourceFile: knope.toml was read as it is.
	SourceFile
	// SourceHybrid: knope.toml was read and gaps were filled in from defaults.
	SourceHybrid
)

// Config is a loaded configuration.
type Config struct {
	Source       Source
	Changes      *Changes
	ReleaseNotes *ReleaseNotes
	// Package is the `[package]` table, which has the Default name and no tag prefix.
	Package *Package
	// Packages are the `[packages.<name>]` tables in declaration order.
	Packages  []NamedPackage
	Workflows []workflow.Workflow
	GitHub    *GitHub
	Gitea     *Gitea
	Jira      *Jira

	// index holds key order and spans of the source, for diagnostics.
	index *tomlIndex
}

type Changes struct {
	IgnoreConventionalCommits bool `toml:"ignore_conventional_commits"`
}

type ReleaseNotes struct {
	ChangeTemplates []string `toml:"change_templates"`
}

// Package is the schema of `[package]` and of each `[packages.<name>]` (spec F2.2).
type Package struct {
	VersionedFiles          []VersionedFile `toml:"versioned_files"`
	Changelog               string          `toml:"changelog"`
	Scopes                  []string        `toml:"scopes"`
	ExtraChangelogSections  []ExtraSection  `toml:"extra_changelog_sections"`
	Assets                  []Asset         `toml:"assets"`
	IgnoreGoMajorVersioning bool            `toml:"ignore_go_major_versioning"`
}

// NamedPackage is an entry of `[packages]`.
type NamedPackage struct {
	Name string
	Package
}

// VersionedFile is a `versioned_files` entry: a bare path, `{path, dependency}` or `{path, regex}`.
type VersionedFile struct {
	Path       string
	Dependency *string
	Regex      []string
}

type ExtraSection struct {
	Name    string   `toml:"name"`
	Footers []string `toml:"footers"`
	Types   []string `toml:"types"`
}

// Asset is a release asset. A bare `assets = "glob"` string becomes an Asset with only a Path.
type Asset struct {
	Path string `toml:"path"`
	Name string `toml:"name"`
}

type GitHub struct {
	Owner string `toml:"owner"`
	Repo  string `toml:"repo"`
}

type Gitea struct {
	Owner string `toml:"owner"`
	Repo  string `toml:"repo"`
	Host  string `toml:"host"`
}

type Jira struct {
	URL     string `toml:"url"`
	Project string `toml:"project"`
}

// HasForge reports whether a GitHub or Gitea integration is configured.
func (c *Config) HasForge() bool { return c.GitHub != nil || c.Gitea != nil }

// PackageCount is the number of packages declared in the file.
func (c *Config) PackageCount() int {
	n := len(c.Packages)
	if c.Package != nil {
		n++
	}
	return n
}

// Span returns where the table or array element at path (as in tomlIndex.Span) sits in knope.toml.
func (c *Config) Span(path ...any) (Span, bool) {
	if c.index == nil {
		return Span{}, false
	}
	return c.index.Span(path...)
}

var workflowName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Load reads the configuration at path, which is normally brand.ConfigFile in the working
// directory. A file that cannot be read means there is none, and the default configuration is
// returned.
func Load(path string) (*Config, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return Default(), nil
	}
	cfg, err := decode(path, src)
	if err != nil {
		return nil, err
	}
	if len(cfg.Workflows) == 0 {
		cfg.Workflows = DefaultWorkflows(cfg.HasForge(), cfg.PackageCount())
		cfg.Source = SourceHybrid
	}
	return cfg, nil
}

// Default returns the configuration used when there is no knope.toml: no packages, no forge and
// the default workflows. Package and forge discovery are not implemented yet.
func Default() *Config {
	return &Config{Source: SourceDefault, Workflows: DefaultWorkflows(false, 0)}
}

// raw mirrors the file. Workflow steps and ordered tables are completed after decoding.
type raw struct {
	Changes      *Changes           `toml:"changes"`
	ReleaseNotes *ReleaseNotes      `toml:"release_notes"`
	Package      *Package           `toml:"package"`
	Packages     map[string]Package `toml:"packages"`
	Workflows    []rawWorkflow      `toml:"workflows"`
	GitHub       *GitHub            `toml:"github"`
	Gitea        *Gitea             `toml:"gitea"`
	Jira         *Jira              `toml:"jira"`
}

type rawWorkflow struct {
	Name     string          `toml:"name"`
	HelpText string          `toml:"help_text"`
	Steps    []workflow.Step `toml:"steps"`
}

func decode(path string, src []byte) (*Config, error) {
	// A key delimiter that cannot occur in TOML keeps package names and variable keys that
	// contain dots intact (F2.4).
	k := koanf.NewWithConf(koanf.Conf{Delim: "\x1f", StrictMerge: true})
	if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
		return nil, tomlError(path, err)
	}
	index, err := buildIndex(src)
	if err != nil {
		return nil, tomlError(path, err)
	}

	var r raw
	err = k.UnmarshalWithConf("", &r, koanf.UnmarshalConf{
		Tag: "toml",
		DecoderConfig: &mapstructure.DecoderConfig{
			DecodeHook: decodeHook,
			Result:     &r,
			TagName:    "toml",
			// Unknown keys are ignored, as in Knope (Quirk Q1): ErrorUnused stays off.
		},
	})
	if err != nil {
		return nil, diag.Newf("Invalid configuration in %s: %w", brand.ConfigFile, err).
			WithCode("config::invalid").
			WithHelp("Check the file against the documented schema.")
	}

	cfg := &Config{
		Source:       SourceFile,
		Changes:      r.Changes,
		ReleaseNotes: r.ReleaseNotes,
		Package:      r.Package,
		GitHub:       r.GitHub,
		Gitea:        r.Gitea,
		Jira:         r.Jira,
		index:        index,
	}
	cfg.Packages = orderedPackages(r.Packages, index.Keys("packages"))

	for i, rw := range r.Workflows {
		if !workflowName.MatchString(rw.Name) {
			return nil, diag.Newf("Workflow names may only contain letters, digits, '-' and '_': %s", rw.Name).
				WithCode("config::workflow_name").
				WithHelp("Rename the workflow in " + brand.ConfigFile + ".")
		}
		for j, step := range rw.Steps {
			orderVariables(step, index, "workflows", i, "steps", j)
		}
		cfg.Workflows = append(cfg.Workflows, workflow.Workflow{Name: rw.Name, HelpText: rw.HelpText, Steps: rw.Steps})
	}
	return cfg, nil
}

// tomlError turns a parse failure into a diagnostic that names the position when it is known.
func tomlError(path string, err error) error {
	var de *gotoml.DecodeError
	var msg string
	if errors.As(err, &de) {
		line, col := de.Position()
		msg = fmt.Sprintf("TOML parse error at line %d, column %d: %s", line, col, de.Error())
	} else {
		msg = fmt.Sprintf("TOML parse error in %s: %s", path, err)
	}
	return diag.New(msg).WithCode("config::toml").WithHelp("Check the TOML is valid.").WithCause(err)
}
