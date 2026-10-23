package config

import (
	"io/fs"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/steps"
	"github.com/celest-io/remeris/internal/tomlwrite"
	"github.com/celest-io/remeris/internal/variables"
	"github.com/celest-io/remeris/internal/workflow"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	documentTestdata = "testdata/document/"
	stepsHeader      = "[[workflows]]\nname = \"w\"\n\n[[workflows.steps]]\n"
	cargoManifest    = "first/Cargo.toml"
	echoHi           = "echo hi"
	preLabel         = "rc"
	mainBranch       = "main"
	changelogKey     = "$changelog"
	statusDone       = "Done"
	emptyWorkflows   = "workflows = []\n"
)

// vfile builds a validated versioned file.
func vfile(t *testing.T, path string, dependency *string, regex []string) versionedfile.Config {
	t.Helper()
	cfg, err := versionedfile.NewConfig(path, dependency, regex)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func ptr[T any](v T) *T { return &v }

// encodeConfig maps cfg to a document and prints it.
func encodeConfig(t *testing.T, cfg *Config) string {
	t.Helper()
	doc, err := cfg.Document()
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	b, err := doc.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return string(b)
}

// withSteps is a configuration with one workflow, "w", that holds the steps.
func withSteps(list ...workflow.Step) *Config {
	return &Config{Workflows: []workflow.Workflow{{Name: "w", Steps: list}}}
}

func TestDocumentGoldens(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		file  string
		build func(t *testing.T) *Config
	}{
		"generate no remote": {
			file:  "generate_no_remote.toml",
			build: func(*testing.T) *Config { return Default() },
		},
		"generate github": {
			file: "generate_github.toml",
			build: func(*testing.T) *Config {
				cfg := &Config{GitHub: &GitHub{Owner: "knope-dev", Repo: "knope"}}
				cfg.Workflows = DefaultWorkflows(true, 0)
				return cfg
			},
		},
		"generate gitea": {
			file: "generate_gitea.toml",
			build: func(*testing.T) *Config {
				cfg := &Config{Gitea: &Gitea{Owner: "knope-dev", Repo: "knope", Host: codebergHost}}
				cfg.Workflows = DefaultWorkflows(true, 0)
				return cfg
			},
		},
		"generate packages": {
			file: "generate_packages.toml",
			build: func(t *testing.T) *Config {
				cfg := &Config{packages: []PackageConfig{{
					Name: versioning.DefaultName(),
					VersionedFiles: []versionedfile.Config{
						vfile(
							t,
							cargoTomlName,
							nil,
							nil,
						),
						vfile(t, "package.json", nil, nil),
						vfile(t, "pyproject.toml", nil, nil),
					},
				}}}
				cfg.Workflows = DefaultWorkflows(false, 1)
				return cfg
			},
		},
		"generate packages with changelog": {
			file: "generate_packages_with_changelog.toml",
			build: func(t *testing.T) *Config {
				cfg := &Config{packages: []PackageConfig{{
					Name: versioning.DefaultName(),
					VersionedFiles: []versionedfile.Config{
						vfile(
							t,
							cargoTomlName,
							nil,
							nil,
						),
						vfile(t, "package.json", nil, nil),
						vfile(t, "pyproject.toml", nil, nil),
					},
					Changelog: defaultChangelog,
				}}}
				cfg.Workflows = DefaultWorkflows(false, 1)
				return cfg
			},
		},
		"generate cargo workspace": {
			file: "generate_cargo_workspace.toml",
			build: func(t *testing.T) *Config {
				cfg := &Config{packages: []PackageConfig{
					{
						Name:           versioning.CustomName("first-package"),
						VersionedFiles: []versionedfile.Config{vfile(t, cargoManifest, nil, nil)},
						Scopes:         []string{"first-package"},
					},
					{
						Name: versioning.CustomName("second-package"),
						VersionedFiles: []versionedfile.Config{
							vfile(t, cargoManifest, ptr("second-package"), nil), vfile(t, "second/Cargo.toml", nil, nil),
						},
						Scopes: []string{"second-package"},
					},
				}}
				cfg.Workflows = DefaultWorkflows(false, 2)
				return cfg
			},
		},
		"upgrade ignore conventional commits": {
			file: "upgrade_ignore_conventional_commits.toml",
			build: func(t *testing.T) *Config {
				// Knope loads the input, moves the flag of PrepareRelease to [changes] and writes the result.
				in, err := os.ReadFile(documentTestdata + "upgrade_ignore_conventional_commits.in.toml")
				if err != nil {
					t.Fatal(err)
				}
				cfg, err := loadIn(t, string(in), cargoTomlName)
				if err != nil {
					t.Fatal(err)
				}
				pr, ok := cfg.Workflows[0].Steps[0].(*steps.PrepareRelease)
				if !ok || !pr.IgnoreConventionalCommits {
					t.Fatalf("the input does not set the flag: %#v", cfg.Workflows[0].Steps[0])
				}
				pr.IgnoreConventionalCommits = false
				cfg.Changes = &Changes{IgnoreConventionalCommits: true}
				return cfg
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want, err := os.ReadFile(documentTestdata + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			if got := encodeConfig(t, tc.build(t)); got != string(want) {
				t.Errorf("document differs from Knope's output\n got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestDocumentNilConfig(t *testing.T) {
	t.Parallel()
	var cfg *Config
	if doc, err := cfg.Document(); err == nil || doc != nil {
		t.Errorf("Document() = %v, %v, want an error and no document", doc, err)
	}
}

func TestDocumentPackageNaming(t *testing.T) {
	t.Parallel()
	pkg := func(name versioning.PackageName) PackageConfig {
		return PackageConfig{Name: name, Changelog: defaultChangelog}
	}
	cases := map[string]struct {
		packages []PackageConfig
		want     string
	}{
		"no package": {want: emptyWorkflows},
		"one default package": {
			packages: []PackageConfig{pkg(versioning.DefaultName())},
			want:     emptyWorkflows + "\n[package]\nchangelog = \"CHANGELOG.md\"\n",
		},
		"one custom package is still [package]": {
			packages: []PackageConfig{pkg(versioning.CustomName("solo"))},
			want:     emptyWorkflows + "\n[package]\nchangelog = \"CHANGELOG.md\"\n",
		},
		"two packages": {
			packages: []PackageConfig{pkg(versioning.CustomName("a")), pkg(versioning.CustomName("b"))},
			want: emptyWorkflows + "\n[packages.a]\nchangelog = \"CHANGELOG.md\"\n\n" +
				"[packages.b]\nchangelog = \"CHANGELOG.md\"\n",
		},
		"names that need quotes": {
			packages: []PackageConfig{
				pkg(
					versioning.CustomName("@scope/name"),
				),
				pkg(versioning.CustomName("a.b")),
				pkg(versioning.CustomName("ok_name-1")),
			},
			want: emptyWorkflows + "\n[packages.\"@scope/name\"]\nchangelog = \"CHANGELOG.md\"\n\n" +
				"[packages.\"a.b\"]\nchangelog = \"CHANGELOG.md\"\n\n[packages.ok_name-1]\nchangelog = \"CHANGELOG.md\"\n",
		},
		"default package among several is called default": {
			packages: []PackageConfig{pkg(versioning.DefaultName()), pkg(versioning.CustomName("b"))},
			want: emptyWorkflows + "\n[packages.default]\nchangelog = \"CHANGELOG.md\"\n\n" +
				"[packages.b]\nchangelog = \"CHANGELOG.md\"\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{packages: tc.packages}
			if got := encodeConfig(t, cfg); got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestDocumentDuplicatePackageNames(t *testing.T) {
	t.Parallel()
	cfg := &Config{packages: []PackageConfig{{Name: versioning.CustomName("a")}, {Name: versioning.CustomName("a")}}}
	if doc, err := cfg.Document(); err == nil || doc != nil {
		t.Errorf("Document() = %v, %v, want an error for two packages with one name", doc, err)
	}
}

func TestDocumentPackageFields(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		pkg  func(t *testing.T) PackageConfig
		want string
	}{
		"nothing set is an empty table": {
			pkg:  func(*testing.T) PackageConfig { return PackageConfig{} },
			want: "[package]\n",
		},
		"empty versioned files, extra sections and false flag are left out": {
			pkg: func(*testing.T) PackageConfig {
				return PackageConfig{VersionedFiles: []versionedfile.Config{}, ExtraChangelogSections: []ExtraSection{}}
			},
			want: "[package]\n",
		},
		"empty scopes are kept": {
			pkg:  func(*testing.T) PackageConfig { return PackageConfig{Scopes: []string{}} },
			want: "[package]\nscopes = []\n",
		},
		"ignore_go_major_versioning true is written": {
			pkg:  func(*testing.T) PackageConfig { return PackageConfig{IgnoreGoMajorVersioning: true} },
			want: "[package]\nignore_go_major_versioning = true\n",
		},
		"bare paths": {
			pkg: func(t *testing.T) PackageConfig {
				return PackageConfig{VersionedFiles: []versionedfile.Config{vfile(t, cargoTomlName, nil, nil)}}
			},
			want: "[package]\nversioned_files = [\"Cargo.toml\"]\n",
		},
		"only dependencies are an array of tables": {
			pkg: func(t *testing.T) PackageConfig {
				return PackageConfig{VersionedFiles: []versionedfile.Config{
					vfile(t, "a/Cargo.toml", ptr("x"), nil), vfile(t, "b/Cargo.toml", ptr("y"), nil),
				}}
			},
			want: "[[package.versioned_files]]\npath = \"a/Cargo.toml\"\ndependency = \"x\"\n\n" +
				"[[package.versioned_files]]\npath = \"b/Cargo.toml\"\ndependency = \"y\"\n",
		},
		"a dependency and a path are inline": {
			pkg: func(t *testing.T) PackageConfig {
				return PackageConfig{VersionedFiles: []versionedfile.Config{
					vfile(t, cargoTomlName, nil, nil), vfile(t, "a/Cargo.toml", ptr("x"), nil),
				}}
			},
			want: "[package]\nversioned_files = [\"Cargo.toml\", { path = \"a/Cargo.toml\", dependency = \"x\" }]\n",
		},
		"a single regex is a string": {
			pkg: func(t *testing.T) PackageConfig {
				return PackageConfig{
					VersionedFiles: []versionedfile.Config{vfile(t, "README.md", nil, []string{`(?<version>\d+)`})},
				}
			},
			want: "[[package.versioned_files]]\npath = \"README.md\"\nregex = '(?<version>\\d+)'\n",
		},
		"several regexes and no regex are lists": {
			pkg: func(t *testing.T) PackageConfig {
				return PackageConfig{VersionedFiles: []versionedfile.Config{
					vfile(t, "a.md", nil, []string{"x", "y"}), vfile(t, "b.md", nil, []string{}),
				}}
			},
			want: "[[package.versioned_files]]\npath = \"a.md\"\nregex = [\"x\", \"y\"]\n\n" +
				"[[package.versioned_files]]\npath = \"b.md\"\nregex = []\n",
		},
		"extra sections keep empty lists": {
			pkg: func(*testing.T) PackageConfig {
				return PackageConfig{ExtraChangelogSections: []ExtraSection{
					{Name: "Notes"}, {Name: "Guide", Footers: []string{"Doc"}, Types: []string{"documentation", "x"}},
				}}
			},
			want: "[[package.extra_changelog_sections]]\nname = \"Notes\"\nfooters = []\ntypes = []\n\n" +
				"[[package.extra_changelog_sections]]\nname = \"Guide\"\nfooters = [\"Doc\"]\ntypes = [\"documentation\", \"x\"]\n",
		},
		"assets glob": {
			pkg:  func(*testing.T) PackageConfig { return PackageConfig{Assets: &Assets{IsGlob: true, Glob: "dist/*"}} },
			want: "[package]\nassets = \"dist/*\"\n",
		},
		"assets list, name only when set": {
			pkg: func(*testing.T) PackageConfig {
				return PackageConfig{Assets: &Assets{List: []Asset{{Path: "a"}, {Path: "b", Name: "bee"}}}}
			},
			want: "[[package.assets]]\npath = \"a\"\n\n[[package.assets]]\npath = \"b\"\nname = \"bee\"\n",
		},
		"assets empty list": {
			pkg:  func(*testing.T) PackageConfig { return PackageConfig{Assets: &Assets{List: []Asset{}}} },
			want: "[package]\nassets = []\n",
		},
		"keys of a table come before its sections": {
			pkg: func(*testing.T) PackageConfig {
				return PackageConfig{
					IgnoreGoMajorVersioning: true,
					Assets:                  &Assets{List: []Asset{{Path: "a"}}},
					Changelog:               "C.md",
				}
			},
			want: "[package]\nchangelog = \"C.md\"\nignore_go_major_versioning = true\n\n[[package.assets]]\npath = \"a\"\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{packages: []PackageConfig{tc.pkg(t)}}
			want := emptyWorkflows + "\n" + tc.want
			if got := encodeConfig(t, cfg); got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestDocumentSteps(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		step workflow.Step
		want string
	}{
		"Command": {
			step: &steps.Command{Command: echoHi},
			want: "type = \"Command\"\ncommand = \"echo hi\"\n",
		},
		"Command shell true": {
			step: &steps.Command{Command: echoHi, Shell: true},
			want: "type = \"Command\"\ncommand = \"echo hi\"\nshell = true\n",
		},
		"Command shell false is left out": {
			step: &steps.Command{Command: "a", Shell: false},
			want: "type = \"Command\"\ncommand = \"a\"\n",
		},
		"Command with variables": {
			step: &steps.Command{Command: "$v $c", Variables: []variables.Entry{
				{Key: "$v", Variable: variables.Version}, {Key: "$c", Variable: variables.ChangelogEntry},
				{Key: "a.b", Variable: variables.IssueBranch},
			}},
			want: "type = \"Command\"\ncommand = \"$v $c\"\n\n[workflows.steps.variables]\n" +
				"\"$v\" = \"Version\"\n\"$c\" = \"ChangelogEntry\"\n\"a.b\" = \"IssueBranch\"\n",
		},
		"Command with empty variables turns substitution off": {
			step: &steps.Command{Command: "a", Variables: []variables.Entry{}},
			want: "type = \"Command\"\ncommand = \"a\"\n\n[workflows.steps.variables]\n",
		},
		"BumpVersion Major": {
			step: &steps.BumpVersion{Rule: steps.RuleMajor},
			want: "type = \"BumpVersion\"\nrule = \"Major\"\n",
		},
		"BumpVersion Pre has a label": {
			step: &steps.BumpVersion{Rule: steps.RulePre, Label: preLabel},
			want: "type = \"BumpVersion\"\nrule = \"Pre\"\nlabel = \"rc\"\n",
		},
		"BumpVersion Release ignores a stray label": {
			step: &steps.BumpVersion{Rule: steps.RuleRelease, Label: preLabel},
			want: "type = \"BumpVersion\"\nrule = \"Release\"\n",
		},
		"CreateChangeFile": {step: &steps.CreateChangeFile{}, want: "type = \"CreateChangeFile\"\n"},
		"PrepareRelease with nothing set": {
			step: &steps.PrepareRelease{},
			want: "type = \"PrepareRelease\"\n",
		},
		"PrepareRelease with everything set": {
			step: &steps.PrepareRelease{PrereleaseLabel: "beta", AllowEmpty: true, IgnoreConventionalCommits: true},
			want: "type = \"PrepareRelease\"\nprerelease_label = \"beta\"\nallow_empty = true\nignore_conventional_commits = true\n",
		},
		"PrepareRelease allow_empty only": {
			step: &steps.PrepareRelease{AllowEmpty: true},
			want: "type = \"PrepareRelease\"\nallow_empty = true\n",
		},
		"Release": {step: &steps.Release{}, want: "type = \"Release\"\n"},
		"CreatePullRequest with default variables": {
			step: &steps.CreatePullRequest{
				Base:  mainBranch,
				Title: variables.Template{Text: "Release $version"},
				Body:  variables.Template{Text: changelogKey},
			},
			want: "type = \"CreatePullRequest\"\nbase = \"main\"\n\n[workflows.steps.title]\ntemplate = \"Release $version\"\n\n" +
				"[workflows.steps.title.variables]\n\"$version\" = \"Version\"\n\"$changelog\" = \"ChangelogEntry\"\n\n" +
				"[workflows.steps.body]\ntemplate = \"$changelog\"\n\n[workflows.steps.body.variables]\n" +
				"\"$version\" = \"Version\"\n\"$changelog\" = \"ChangelogEntry\"\n",
		},
		"CreatePullRequest with its own variables": {
			step: &steps.CreatePullRequest{
				Base: mainBranch,
				Title: variables.Template{
					Text:      "t",
					Variables: []variables.Entry{{Key: "$b", Variable: variables.IssueBranch}},
				},
				Body: variables.Template{Text: "b", Variables: []variables.Entry{}},
			},
			want: "type = \"CreatePullRequest\"\nbase = \"main\"\n\n[workflows.steps.title]\ntemplate = \"t\"\n\n" +
				"[workflows.steps.title.variables]\n\"$b\" = \"IssueBranch\"\n\n[workflows.steps.body]\ntemplate = \"b\"\n\n" +
				"[workflows.steps.body.variables]\n",
		},
		"SelectJiraIssue": {
			step: &steps.SelectJiraIssue{Status: "Backlog"},
			want: "type = \"SelectJiraIssue\"\nstatus = \"Backlog\"\n",
		},
		"TransitionJiraIssue": {
			step: &steps.TransitionJiraIssue{Status: statusDone},
			want: "type = \"TransitionJiraIssue\"\nstatus = \"Done\"\n",
		},
		"SelectGitHubIssue without labels": {
			step: &steps.SelectGitHubIssue{},
			want: "type = \"SelectGitHubIssue\"\n",
		},
		"SelectGitHubIssue with labels": {
			step: &steps.SelectGitHubIssue{Labels: []string{"bug", "help wanted"}},
			want: "type = \"SelectGitHubIssue\"\nlabels = [\"bug\", \"help wanted\"]\n",
		},
		"SelectGitHubIssue with empty labels": {
			step: &steps.SelectGitHubIssue{Labels: []string{}},
			want: "type = \"SelectGitHubIssue\"\nlabels = []\n",
		},
		"SelectGiteaIssue with labels": {
			step: &steps.SelectGiteaIssue{Labels: []string{"x"}},
			want: "type = \"SelectGiteaIssue\"\nlabels = [\"x\"]\n",
		},
		"SelectGiteaIssue without labels": {step: &steps.SelectGiteaIssue{}, want: "type = \"SelectGiteaIssue\"\n"},
		"SelectIssueFromBranch":           {step: &steps.SelectIssueFromBranch{}, want: "type = \"SelectIssueFromBranch\"\n"},
		"SwitchBranches":                  {step: &steps.SwitchBranches{}, want: "type = \"SwitchBranches\"\n"},
		"RebaseBranch": {
			step: &steps.RebaseBranch{To: mainBranch},
			want: "type = \"RebaseBranch\"\nto = \"main\"\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got, want := encodeConfig(t, withSteps(tc.step)), stepsHeader+tc.want; got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestDocumentWorkflows(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		workflows []workflow.Workflow
		want      string
	}{
		"no workflows": {want: emptyWorkflows},
		"no help text and no steps": {
			workflows: []workflow.Workflow{{Name: "No-op"}},
			want:      "[[workflows]]\nname = \"No-op\"\nsteps = []\n",
		},
		"help text": {
			workflows: []workflow.Workflow{{Name: "a_b", HelpText: "Do \"it\"", Steps: []workflow.Step{&steps.Release{}}}},
			want:      "[[workflows]]\nname = \"a_b\"\nhelp_text = 'Do \"it\"'\n\n[[workflows.steps]]\ntype = \"Release\"\n",
		},
		"two workflows": {
			workflows: []workflow.Workflow{
				{
					Name:  "a",
					Steps: []workflow.Step{&steps.Release{}},
				},
				{Name: "b", Steps: []workflow.Step{&steps.CreateChangeFile{}}},
			},
			want: "[[workflows]]\nname = \"a\"\n\n[[workflows.steps]]\ntype = \"Release\"\n\n" +
				"[[workflows]]\nname = \"b\"\n\n[[workflows.steps]]\ntype = \"CreateChangeFile\"\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := encodeConfig(t, &Config{Workflows: tc.workflows}); got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestDocumentSectionsAndForges(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		cfg  *Config
		want string
	}{
		"changes false is left out": {
			cfg:  &Config{Changes: &Changes{}},
			want: emptyWorkflows,
		},
		"changes true comes first": {
			cfg:  &Config{Changes: &Changes{IgnoreConventionalCommits: true}, Packages: nil, packages: []PackageConfig{{}}},
			want: emptyWorkflows + "\n[changes]\nignore_conventional_commits = true\n\n[package]\n",
		},
		"github only": {
			cfg:  &Config{GitHub: &GitHub{Owner: "o", Repo: "r"}},
			want: emptyWorkflows + "\n[github]\nowner = \"o\"\nrepo = \"r\"\n",
		},
		"github with api_url and upload_url is kept": {
			cfg: &Config{GitHub: &GitHub{
				Owner: "o", Repo: "r", APIURL: "https://ghe.example/api/v3/", UploadURL: "https://ghe.example/api/uploads/",
			}},
			want: emptyWorkflows + "\n[github]\nowner = \"o\"\nrepo = \"r\"\n" +
				"api_url = \"https://ghe.example/api/v3/\"\nupload_url = \"https://ghe.example/api/uploads/\"\n",
		},
		"github with api_url only": {
			cfg:  &Config{GitHub: &GitHub{Owner: "o", Repo: "r", APIURL: "https://ghe.example/"}},
			want: emptyWorkflows + "\n[github]\nowner = \"o\"\nrepo = \"r\"\napi_url = \"https://ghe.example/\"\n",
		},
		"gitea only": {
			cfg:  &Config{Gitea: &Gitea{Owner: "o", Repo: "r", Host: codebergHost}},
			want: emptyWorkflows + "\n[gitea]\nowner = \"o\"\nrepo = \"r\"\nhost = \"https://codeberg.org\"\n",
		},
		"github and gitea": {
			cfg: &Config{GitHub: &GitHub{Owner: "o", Repo: "r"}, Gitea: &Gitea{Owner: "p", Repo: "s", Host: "h"}},
			want: emptyWorkflows + "\n[github]\nowner = \"o\"\nrepo = \"r\"\n\n" +
				"[gitea]\nowner = \"p\"\nrepo = \"s\"\nhost = \"h\"\n",
		},
		"jira is kept": {
			cfg:  &Config{Jira: &Jira{URL: "https://j.example", Project: "KEY"}},
			want: emptyWorkflows + "\n[jira]\nurl = \"https://j.example\"\nproject = \"KEY\"\n",
		},
		"release_notes is kept": {
			cfg:  &Config{ReleaseNotes: &ReleaseNotes{ChangeTemplates: []string{"### $summary\n\n$details", "x"}}},
			want: emptyWorkflows + "\n[release_notes]\nchange_templates = [\"\"\"\n### $summary\n\n$details\"\"\", \"x\"]\n",
		},
		"empty release_notes": {
			cfg:  &Config{ReleaseNotes: &ReleaseNotes{}},
			want: emptyWorkflows + "\n[release_notes]\nchange_templates = []\n",
		},
		"all sections in Knope's order, then ours": {
			cfg: &Config{
				Changes: &Changes{
					IgnoreConventionalCommits: true,
				},
				ReleaseNotes: &ReleaseNotes{},
				Jira:         &Jira{URL: "u", Project: "p"},
				Gitea:        &Gitea{Owner: "g", Repo: "h", Host: "i"},
				GitHub:       &GitHub{Owner: "a", Repo: "b"},
				packages:     []PackageConfig{{}},
			},
			want: emptyWorkflows + "\n[changes]\nignore_conventional_commits = true\n\n[package]\n\n" +
				"[github]\nowner = \"a\"\nrepo = \"b\"\n\n[gitea]\nowner = \"g\"\nrepo = \"h\"\nhost = \"i\"\n\n" +
				"[jira]\nurl = \"u\"\nproject = \"p\"\n\n[release_notes]\nchange_templates = []\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := encodeConfig(t, tc.cfg)
			if got != tc.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

func TestDocumentErrors(t *testing.T) {
	t.Parallel()
	cases := map[string]*Config{
		"nil step":                  withSteps(nil),
		"unknown step type":         withSteps(unknownStep{}),
		"bump version without rule": withSteps(&steps.BumpVersion{}),
		"Pre without label":         withSteps(&steps.BumpVersion{Rule: steps.RulePre}),
		"unknown variable": withSteps(&steps.Command{
			Command: "a", Variables: []variables.Entry{{Key: "k", Variable: "Nope"}},
		}),
		"unknown variable in a template": withSteps(&steps.CreatePullRequest{
			Title: variables.Template{Variables: []variables.Entry{{Key: "k", Variable: "Nope"}}},
		}),
		"invalid UTF-8": withSteps(&steps.Command{Command: "a\xffb"}),
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, err := cfg.Document()
			if err == nil {
				// The writer reports a bad string when the document is printed.
				var b []byte
				if b, err = doc.Encode(); err == nil || b != nil {
					t.Errorf("Document and Encode returned %q, %v, want an error", b, err)
				}
			} else if doc != nil {
				t.Errorf("Document returned both a document and %v", err)
			}
		})
	}
}

// unknownStep is a step type the writer does not know.
type unknownStep struct{ workflow.Step }

func (unknownStep) Type() string { return "Unknown" }

// TestEveryStepTypeHasASerializer fails when a type that the loader accepts cannot be written, or leaves out a field
// it reads: adding a step type or a `toml:` field without a writer breaks this test.
func TestEveryStepTypeHasASerializer(t *testing.T) {
	t.Parallel()
	for name, newStep := range stepConstructors {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			step := newStep()
			if step.Type() != name {
				t.Fatalf("constructor of %q builds a %q", name, step.Type())
			}
			fillStep(t, step)
			table, err := stepTable(step)
			if err != nil {
				t.Fatalf("stepTable: %v", err)
			}
			entries := tableEntries(t, table)
			if len(entries) == 0 || entries[0] != "type" {
				t.Fatalf("keys = %v, want type first", entries)
			}
			for _, tag := range tomlTags(step) {
				if !contains(entries, tag) {
					t.Errorf("the field %q of %s is not written (keys: %v)", tag, name, entries)
				}
			}
		})
	}
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func tableEntries(t *testing.T, table *tomlwrite.Table) []string {
	t.Helper()
	var keys []string
	for _, e := range table.Entries() {
		keys = append(keys, e.Key)
	}
	return keys
}

func tomlTags(step workflow.Step) []string {
	var tags []string
	typ := reflect.TypeOf(step).Elem()
	for i := range typ.NumField() {
		if tag := typ.Field(i).Tag.Get(tagName); tag != "" {
			tags = append(tags, tag)
		}
	}
	sort.Strings(tags)
	return tags
}

// fillStep sets every field of the step to a non-zero value.
func fillStep(t *testing.T, step workflow.Step) {
	t.Helper()
	v := reflect.ValueOf(step).Elem()
	for i := range v.NumField() {
		f := v.Field(i)
		switch f.Type() {
		case reflect.TypeFor[string]():
			f.SetString("x")
		case reflect.TypeFor[bool]():
			f.SetBool(true)
		case reflect.TypeFor[[]string]():
			f.Set(reflect.ValueOf([]string{"x"}))
		case reflect.TypeFor[steps.BumpVersionRule]():
			f.Set(reflect.ValueOf(steps.RulePre))
		case reflect.TypeFor[variables.Template]():
			f.Set(
				reflect.ValueOf(
					variables.Template{Text: "x", Variables: []variables.Entry{{Key: "k", Variable: variables.Version}}},
				),
			)
		case reflect.TypeFor[[]variables.Entry]():
			f.Set(reflect.ValueOf([]variables.Entry{{Key: "k", Variable: variables.Version}}))
		default:
			t.Fatalf("fillStep: no value for field %s of %s", v.Type().Field(i).Name, v.Type())
		}
	}
}

// fullConfig covers every step type and every field, with strings that would break text built by concatenation.
func fullConfig(t *testing.T, hostile string) (*Config, []string) {
	t.Helper()
	dir := "dir " + hostile
	files := []string{
		cargoTomlName, dir + "/package.json", dir + "/Cargo.toml", dir + "/README.md", dir + "/OTHER.md", defaultChangelog,
	}
	vars := []variables.Entry{
		{Key: "$v " + hostile, Variable: variables.Version}, {Key: "$c", Variable: variables.ChangelogEntry},
		{Key: "bare", Variable: variables.IssueBranch},
	}
	cfg := &Config{
		Changes:      &Changes{IgnoreConventionalCommits: true},
		ReleaseNotes: &ReleaseNotes{ChangeTemplates: []string{"### $summary " + hostile + "\n\n$details", "$summary"}},
		GitHub: &GitHub{
			Owner:     "o",
			Repo:      "r",
			APIURL:    "https://ghe.example/api/v3/",
			UploadURL: "https://ghe.example/up/",
		},
		Jira: &Jira{URL: "https://jira.example", Project: "KEY"},
		packages: []PackageConfig{
			{
				Name: versioning.CustomName("@scope/" + hostile),
				VersionedFiles: []versionedfile.Config{
					vfile(t, cargoTomlName, nil, nil),
					vfile(t, dir+"/package.json", ptr("dep "+hostile), nil),
					vfile(t, dir+"/README.md", nil, []string{`(?<version>\d+) ` + hostile}),
					vfile(t, dir+"/OTHER.md", nil, []string{"(?<version>a)", "(?<version>b)"}),
				},
				Changelog: defaultChangelog,
				Scopes:    []string{"a", hostile},
				ExtraChangelogSections: []ExtraSection{
					{Name: "Notes " + hostile, Footers: []string{"Note"}, Types: []string{}},
				},
				Assets: &Assets{List: []Asset{{Path: "dist/a " + hostile, Name: "A"}, {Path: "dist/b"}}},
			},
			{
				Name:                    versioning.CustomName("second"),
				VersionedFiles:          []versionedfile.Config{vfile(t, dir+"/Cargo.toml", ptr("x"), nil)},
				Scopes:                  []string{},
				Assets:                  &Assets{IsGlob: true, Glob: "dist/*"},
				IgnoreGoMajorVersioning: true,
			},
		},
		Workflows: []workflow.Workflow{
			{Name: "everything", HelpText: "Help " + hostile, Steps: []workflow.Step{
				&steps.Command{Command: "echo " + hostile},
				&steps.Command{Command: "echo $v " + hostile, Variables: vars, Shell: true},
				&steps.Command{Command: "no variables", Variables: []variables.Entry{}},
				&steps.BumpVersion{Rule: steps.RuleMajor}, &steps.BumpVersion{Rule: steps.RuleMinor},
				&steps.BumpVersion{Rule: steps.RulePatch}, &steps.BumpVersion{Rule: steps.RuleRelease},
				&steps.BumpVersion{Rule: steps.RulePre, Label: "rc " + hostile},
				&steps.CreateChangeFile{},
				&steps.PrepareRelease{},
				&steps.PrepareRelease{PrereleaseLabel: "beta " + hostile, AllowEmpty: true, IgnoreConventionalCommits: true},
				&steps.Release{},
				&steps.CreatePullRequest{
					Base:  "main " + hostile,
					Title: variables.Template{Text: "Release $version " + hostile, Variables: vars[:1]},
					Body:  variables.Template{Text: changelogKey, Variables: []variables.Entry{}},
				},
				&steps.SelectJiraIssue{Status: "Backlog " + hostile}, &steps.TransitionJiraIssue{Status: statusDone},
				&steps.SelectGitHubIssue{Labels: []string{"bug", hostile}}, &steps.SelectGitHubIssue{Labels: []string{}},
				&steps.SelectGiteaIssue{Labels: []string{"x"}}, &steps.SelectGiteaIssue{},
				&steps.SelectIssueFromBranch{}, &steps.SwitchBranches{}, &steps.RebaseBranch{To: mainBranch},
			}},
			{Name: "no-steps"},
		},
	}
	return cfg, files
}

// normalized makes the configuration comparable: templates with no `variables` key get the defaults, which is what
// the document spells out (as Knope does).
func normalized(cfg *Config) *Config {
	out := *cfg
	out.packages = append([]PackageConfig(nil), cfg.packages...)
	for i := range out.packages {
		if out.packages[i].VersionedFiles == nil { // the loader returns an empty list, not nil
			out.packages[i].VersionedFiles = []versionedfile.Config{}
		}
		if len(out.packages) == 1 { // `[package]` is the default package, whatever the name was
			out.packages[i].Name = versioning.DefaultName()
		}
	}
	out.Workflows = nil
	for _, wf := range cfg.Workflows {
		wf.Steps = append([]workflow.Step{}, wf.Steps...) // the loader returns an empty list, not nil
		for i, step := range wf.Steps {
			if pr, ok := step.(*steps.CreatePullRequest); ok {
				c := *pr
				if c.Title.Variables == nil {
					c.Title.Variables = variables.DefaultEntries()
				}
				if c.Body.Variables == nil {
					c.Body.Variables = variables.DefaultEntries()
				}
				wf.Steps[i] = &c
			}
		}
		out.Workflows = append(out.Workflows, wf)
	}
	return &out
}

// sameConfig compares what the document carries.
func sameConfig(t *testing.T, got, want *Config) {
	t.Helper()
	want = normalized(want)
	for i := 0; i < len(got.Workflows) && i < len(want.Workflows); i++ {
		g, w := got.Workflows[i], want.Workflows[i]
		for j := 0; j < len(g.Steps) && j < len(w.Steps); j++ {
			if !reflect.DeepEqual(g.Steps[j], w.Steps[j]) {
				t.Errorf("workflow %d step %d differs\n got: %+v\nwant: %+v", i, j, g.Steps[j], w.Steps[j])
			}
		}
	}
	checks := []struct {
		name      string
		got, want any
	}{
		{"changes", got.Changes, want.Changes}, {"release_notes", got.ReleaseNotes, want.ReleaseNotes},
		{"github", got.GitHub, want.GitHub}, {"gitea", got.Gitea, want.Gitea}, {"jira", got.Jira, want.Jira},
		{"packages", got.PackageConfigs(), want.PackageConfigs()}, {"workflows", got.Workflows, want.Workflows},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s differ after the round trip\n got: %#v\nwant: %#v", c.name, c.got, c.want)
		}
	}
}

// roundTrip prints cfg, loads the text and prints that configuration again.
func roundTrip(t *testing.T, cfg *Config, files []string) (first, second string, loaded *Config) {
	t.Helper()
	first = encodeConfig(t, cfg)
	loaded, err := loadIn(t, first, files...)
	if err != nil {
		t.Fatalf("the document does not load: %s\n%s", rendered(err), first)
	}
	return first, encodeConfig(t, loaded), loaded
}

func TestDocumentRoundTrip(t *testing.T) {
	t.Parallel()
	hostiles := map[string]string{
		"plaintext":  "plaintext",
		"quotes":     `"double" and 'single' and ''' and """`,
		"brackets":   "] [[x]] [y] }{ = # not a comment",
		"newlines":   "line one\nline two\r\nthree",
		"backslash":  `back\slash \n A \`,
		"tab":        "tab\there",
		"unicode":    "héllo wörld   日本語 \U0001F600",
		"control":    "bell\x07 escape\x1b",
		"table-ish":  "\n[workflows.steps]\ntype = \"Release\"\n",
		"dollar-key": "$version",
	}
	for name, hostile := range hostiles {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg, files := fullConfig(t, hostile)
			first, second, loaded := roundTrip(t, cfg, files)
			sameConfig(t, loaded, cfg)
			if first != second {
				t.Errorf("the document is not stable\nfirst:\n%s\nsecond:\n%s", first, second)
			}
		})
	}
}

// TestDocumentRoundTripGitea covers [gitea], which cannot be loaded together with package assets.
func TestDocumentRoundTripGitea(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		GitHub:    &GitHub{Owner: "o", Repo: "r"},
		Gitea:     &Gitea{Owner: "o2 \"x\"", Repo: "r2", Host: codebergHost},
		Workflows: DefaultWorkflows(true, 0),
	}
	_, _, loaded := roundTrip(t, cfg, nil)
	sameConfig(t, loaded, cfg)
}

func TestDocumentRoundTripDefaults(t *testing.T) {
	t.Parallel()
	for name, hasForge := range map[string]bool{"no forge": false, "forge": true} {
		for count := range 3 {
			t.Run(name+" "+string(rune('0'+count)), func(t *testing.T) {
				t.Parallel()
				cfg := &Config{Workflows: DefaultWorkflows(hasForge, count)}
				var files []string
				for i := range count {
					dir := string(rune('a' + i))
					files = append(files, dir+"/Cargo.toml")
					cfg.packages = append(cfg.packages, PackageConfig{
						Name:           versioning.CustomName("pkg-" + dir),
						VersionedFiles: []versionedfile.Config{vfile(t, dir+"/Cargo.toml", nil, nil)},
					})
				}
				if hasForge {
					cfg.GitHub = &GitHub{Owner: "o", Repo: "r"}
				}
				_, _, loaded := roundTrip(t, cfg, files)
				sameConfig(t, loaded, cfg)
			})
		}
	}
}

// FuzzDocumentRoundTrip checks that no string in a name, path, command or template can change the structure of the
// document: what is loaded is what was printed.
func FuzzDocumentRoundTrip(f *testing.F) {
	f.Add("name", "dir", echoHi, "Release $version", `(?<version>\d+)`)
	f.Add("@scope/a.b", "d]#", "a\nb", "\"\"\"\n[x]", "'''")
	f.Add("quo\"te", "sp ace", "\\", "\x00\t", "# c")
	f.Fuzz(func(t *testing.T, name, dir, command, template, regex string) {
		// koanf splits keys at U+001F (decode.go), so no key can hold it; a path must be valid for the file system.
		if strings.ContainsRune(name, '\x1f') || strings.ContainsRune(template, '\x1f') ||
			strings.ContainsRune(command, '\x1f') {
			t.Skip()
		}
		manifest := "d/" + dir + "/Cargo.toml"
		if dir == "" || !fs.ValidPath(manifest) || path.Clean(manifest) != manifest {
			t.Skip()
		}
		cfg := &Config{
			packages: []PackageConfig{
				{
					Name: versioning.CustomName(name),
					VersionedFiles: []versionedfile.Config{
						vfile(t, manifest, nil, nil), vfile(t, "d/R.md", nil, []string{regex}),
					},
					Scopes: []string{template},
				},
				{Name: versioning.CustomName(name + "2")},
			},
			Workflows: []workflow.Workflow{{Name: "w", HelpText: template, Steps: []workflow.Step{
				&steps.Command{Command: command, Variables: []variables.Entry{{Key: template, Variable: variables.Version}}},
				&steps.CreatePullRequest{
					Base:  name,
					Title: variables.Template{Text: template},
					Body:  variables.Template{Text: command},
				},
			}}},
		}
		doc, err := cfg.Document()
		if err != nil {
			t.Fatal(err)
		}
		b, err := doc.Encode()
		if err != nil {
			t.Skip() // not valid UTF-8, which the writer refuses
		}
		loaded, err := loadIn(t, string(b), manifest, "d/R.md")
		if err != nil {
			t.Fatalf("the document does not load: %s\n%s", rendered(err), b)
		}
		sameConfig(t, loaded, cfg)
		again, err := loaded.Document()
		if err != nil {
			t.Fatal(err)
		}
		if b2, err := again.Encode(); err != nil || string(b2) != string(b) {
			t.Errorf("not stable: %v\nfirst:\n%s\nsecond:\n%s", err, b, b2)
		}
	})
}
