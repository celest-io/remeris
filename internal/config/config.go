// Package config loads knope.toml (spec F2).
//
// The whole top-level schema is decoded; workflows and packages are validated.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"regexp"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	gotoml "github.com/pelletier/go-toml/v2"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/workflow"
)

// KnopeFile is the file name of Knope's release configuration, the first configuration remeris supports.
const KnopeFile = "knope.toml"

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
	// packages are the validated packages, in declaration order.
	packages []PackageConfig
	// discoveryErr is why package discovery failed. It is reported when the project is loaded, so that --help and
	// the other commands that read no packages keep working.
	discoveryErr error
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
	Assets                  *Assets         `toml:"assets"`
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

// Assets is the `assets` setting of a package: a glob pattern (`assets = "dist/*"`) or a list of files
// (`assets = [{ path = "dist/app", name = "app" }]`). The two forms stay apart because Knope expands a pattern
// and takes a list entry as a literal path, even when it holds a `*`.
type Assets struct {
	// IsGlob is true for the string form. Then Glob is the pattern and List is nil.
	IsGlob bool
	Glob   string
	// List holds the entries of the list form; an empty non-nil list is `assets = []`.
	List []Asset
}

// Asset is an entry of the list form of `assets`.
type Asset struct {
	Path string `toml:"path"`
	Name string `toml:"name"`
}

// GitHub is the [github] table. APIURL and UploadURL are remeris' own keys (Knope ignores them), for GitHub
// Enterprise Server and for test servers.
type GitHub struct {
	Owner string `toml:"owner"`
	Repo  string `toml:"repo"`
	// APIURL is the base URL of the GitHub API, an absolute http or https URL. go-github's WithEnterpriseURLs
	// appends "api/v3/" to its path unless the path already ends in "/api/v3/" or the host starts with "api."
	// or contains ".api.", and adds a trailing slash when the path has none; the value here is kept as written.
	//
	// The value is not trusted on its own: a repository's configuration cannot choose where the GitHub token
	// goes. It is used only when --github-api-url or REMERIS_GITHUB_API_URL gives the same URL; otherwise
	// building the GitHub client fails.
	APIURL string `toml:"api_url"`
	// UploadURL is the base URL for release asset uploads, with the same shape as APIURL. It needs APIURL. When
	// only APIURL is set, uploads use APIURL too. go-github appends "api/uploads/" to its path unless
	// the path already ends in "/api/uploads/" or the host starts with "api." or "uploads." or contains ".api.".
	// Like APIURL, it must be confirmed by --github-upload-url or REMERIS_GITHUB_UPLOAD_URL.
	UploadURL string `toml:"upload_url"`
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

// PackageCount is the number of packages: those declared in the file, or the one discovered at the root.
func (c *Config) PackageCount() int { return len(c.packages) }

// DiscoveryError is the error of package discovery (spec F20.1), nil when it succeeded or did not run.
func (c *Config) DiscoveryError() error { return c.discoveryErr }

// Span returns where the table or array element at path (as in tomlIndex.Span) sits in knope.toml.
func (c *Config) Span(path ...any) (Span, bool) {
	if c.index == nil {
		return Span{}, false
	}
	return c.index.Span(path...)
}

// tagName is the struct tag mapstructure reads field names from.
const tagName = "toml"

var workflowName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Load reads the configuration at path, which is normally KnopeFile in the working
// directory. A file that cannot be read means there is none, and the default configuration is
// returned.
func Load(path string) (*Config, error) { return LoadFS(path, osRoot{}) }

// LoadFS is Load with the file system that the existence checks of `versioned_files` use. Paths are
// slash-separated and relative to its root, which is the working directory for Load.
func LoadFS(path string, root fs.StatFS) (*Config, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		cfg := Default()
		cfg.packages, cfg.discoveryErr = discoverPackages(root)
		cfg.Workflows = DefaultWorkflows(false, len(cfg.packages))
		return cfg, nil
	}
	cfg, err := decode(path, src, root)
	if err != nil {
		return nil, err
	}
	// Like Knope's fill_in_gaps: a file without packages gets the discovered ones, whatever its workflows.
	if len(cfg.packages) == 0 {
		cfg.packages, cfg.discoveryErr = discoverPackages(root)
	}
	if len(cfg.Workflows) == 0 {
		cfg.Workflows = DefaultWorkflows(cfg.HasForge(), cfg.PackageCount())
		cfg.Source = SourceHybrid
	}
	return cfg, nil
}

// Default returns the bare configuration used when there is no knope.toml: no packages, no forge and
// the default workflows. LoadFS adds the package discovered at the root (spec F20.1); forge discovery is not
// implemented yet.
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

func decode(path string, src []byte, root fs.StatFS) (*Config, error) {
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
		Tag: tagName,
		DecoderConfig: &mapstructure.DecoderConfig{
			DecodeHook: decodeHook,
			Result:     &r,
			TagName:    tagName,
			// Unknown keys are ignored, as in Knope (Quirk Q1): ErrorUnused stays off.
		},
	})
	if err != nil {
		return nil, diag.Newf("Invalid configuration in %s: %w", KnopeFile, err).
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
	if cfg.packages, err = validatePackages(&r, index, string(src), root, cfg.Packages); err != nil {
		return nil, err
	}

	if err := validateGitHub(r.GitHub, index, string(src)); err != nil {
		return nil, err
	}

	for i, rw := range r.Workflows {
		if !workflowName.MatchString(rw.Name) {
			return nil, diag.Newf("Workflow names may only contain letters, digits, '-' and '_': %s", rw.Name).
				WithCode("config::workflow_name").
				WithHelp("Rename the workflow in " + KnopeFile + ".")
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

const (
	codeGitHubURL         = "remeris::config::github_url"
	codeUploadURLNoAPIURL = "remeris::config::github_upload_url_without_api_url"
)

// validateGitHub checks the optional api_url and upload_url keys of [github].
func validateGitHub(g *GitHub, index *tomlIndex, src string) error {
	if g == nil {
		return nil
	}
	for _, k := range []struct{ key, value string }{{"api_url", g.APIURL}, {"upload_url", g.UploadURL}} {
		if k.value == "" {
			continue
		}
		if !isHTTPURL(k.value) {
			d := diag.Newf("`%s` in [github] must be an absolute http or https URL with a host: %s", k.key, redacted(k.value)).
				WithCode(codeGitHubURL).
				WithHelp("Use a value like https://github.example.com/ for `" + k.key + "`.")
			if s, ok := index.Span("github", k.key); ok {
				d = d.WithSource(src, diag.Label{Start: s.Start, End: s.End, Text: "`" + k.key + "` defined here"})
			}
			return d
		}
	}
	if g.UploadURL != "" && g.APIURL == "" {
		d := diag.New("`upload_url` in [github] needs `api_url`").
			WithCode(codeUploadURLNoAPIURL).
			WithHelp("Set `api_url` in [github] too, or remove `upload_url`.")
		if s, ok := index.Span("github", "upload_url"); ok {
			d = d.WithSource(src, diag.Label{Start: s.Start, End: s.End, Text: "`upload_url` defined here"})
		}
		return d
	}
	return nil
}

// redacted hides the password of a URL, so an error does not print a credential.
func redacted(s string) string {
	if u, err := url.Parse(s); err == nil {
		return u.Redacted()
	}
	return "(not a URL)"
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}
