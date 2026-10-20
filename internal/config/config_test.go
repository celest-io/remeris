package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/steps"
	"github.com/celest-io/remeris/internal/variables"
	"github.com/celest-io/remeris/internal/workflow"
)

const (
	wfRelease          = "release"
	wfDocumentChange   = "document-change"
	stepRelease        = "Release"
	stepCommand        = "Command"
	stepPrepareRelease = "PrepareRelease"
)

func load(t *testing.T, content string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "knope.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return LoadFS(path, anyFS{})
}

// anyFS says every file exists, so tests of other features can name versioned files freely.
type anyFS struct{}

func (anyFS) Open(string) (fs.File, error)     { return nil, fs.ErrNotExist }
func (anyFS) Stat(string) (fs.FileInfo, error) { return nil, nil }

func mustLoad(t *testing.T, content string) *Config {
	t.Helper()
	cfg, err := load(t, content)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	return cfg
}

func stepsOf(t *testing.T, cfg *Config, name string) []workflow.Step {
	t.Helper()
	for _, w := range cfg.Workflows {
		if w.Name == name {
			return w.Steps
		}
	}
	t.Fatalf("no workflow %q", name)
	return nil
}

func TestLoadMissingFileGivesDefault(t *testing.T) {
	cfg, err := LoadFS(filepath.Join(t.TempDir(), "knope.toml"), fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source != SourceDefault || cfg.PackageCount() != 0 || cfg.HasForge() {
		t.Errorf("unexpected default config: %+v", cfg)
	}
	var names []string
	for _, w := range cfg.Workflows {
		names = append(names, w.Name)
	}
	if want := []string{wfRelease, wfDocumentChange}; !slices.Equal(names, want) {
		t.Errorf("default workflows = %v, want %v", names, want)
	}
}

func TestLoadFillsInWorkflows(t *testing.T) {
	cases := map[string]struct {
		toml      string
		files     []string
		wantNames []string
		wantPush  string // the step after PrepareRelease and the commit
	}{
		"no workflows, no packages": {
			toml:      "",
			wantNames: []string{wfRelease, wfDocumentChange},
			wantPush:  stepRelease,
		},
		"forge changes the release workflow": {
			toml:      "[github]\nowner = \"o\"\nrepo = \"r\"\n",
			wantNames: []string{wfRelease, wfDocumentChange},
			wantPush:  stepCommand,
		},
		"one declared package adds get-version": {
			toml:      "[package]\nversioned_files = [\"Cargo.toml\"]\n",
			files:     []string{"Cargo.toml"},
			wantNames: []string{wfRelease, wfDocumentChange, "get-version"},
			wantPush:  stepRelease,
		},
		"two packages do not": {
			toml:      "[packages.a]\n[packages.b]\n",
			wantNames: []string{wfRelease, wfDocumentChange},
			wantPush:  stepRelease,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := loadIn(t, tc.toml, tc.files...)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Source != SourceHybrid {
				t.Errorf("Source = %v, want hybrid", cfg.Source)
			}
			var names []string
			for _, w := range cfg.Workflows {
				names = append(names, w.Name)
			}
			if !slices.Equal(names, tc.wantNames) {
				t.Errorf("workflows = %v, want %v", names, tc.wantNames)
			}
			if got := cfg.Workflows[0].Steps[2].Type(); got != tc.wantPush {
				t.Errorf("third release step = %s, want %s", got, tc.wantPush)
			}
		})
	}
}

func TestLoadKeepsExplicitWorkflowsAsFile(t *testing.T) {
	cfg := mustLoad(t, "[[workflows]]\nname = \"a\"\n")
	if cfg.Source != SourceFile || len(cfg.Workflows) != 1 || cfg.Workflows[0].Name != "a" {
		t.Errorf("config = %+v", cfg)
	}
}

func TestDefaultWorkflows(t *testing.T) {
	commitOf := func(w workflow.Workflow) string { return w.Steps[1].(*steps.Command).Command }
	cases := map[string]struct {
		hasForge bool
		pkgs     int
		commit   string
		release  []string
		hasGet   bool
	}{
		"no forge, no package": {
			false,
			0,
			`git commit -m "chore: prepare release $version"`,
			[]string{stepPrepareRelease, stepCommand, stepRelease, stepCommand, stepCommand},
			false,
		},
		"forge, one package": {
			true,
			1,
			`git commit -m "chore: prepare release $version"`,
			[]string{stepPrepareRelease, stepCommand, stepCommand, stepRelease},
			true,
		},
		"two packages": {
			false,
			2,
			`git commit -m "chore: prepare releases"`,
			[]string{stepPrepareRelease, stepCommand, stepRelease, stepCommand, stepCommand},
			false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ws := DefaultWorkflows(tc.hasForge, tc.pkgs)
			var types []string
			for _, s := range ws[0].Steps {
				types = append(types, s.Type())
			}
			if !slices.Equal(types, tc.release) {
				t.Errorf("release steps = %v, want %v", types, tc.release)
			}
			if got := commitOf(ws[0]); got != tc.commit {
				t.Errorf("commit = %q, want %q", got, tc.commit)
			}
			if got := len(ws) == 3; got != tc.hasGet {
				t.Errorf("get-version present = %v, want %v", got, tc.hasGet)
			}
		})
	}

	noForge := DefaultWorkflows(false, 1)
	if cmds := []string{
		noForge[0].Steps[3].(*steps.Command).Command,
		noForge[0].Steps[4].(*steps.Command).Command,
	}; !slices.Equal(
		cmds,
		[]string{"git push", "git push --tags"},
	) {
		t.Errorf("no-forge push commands = %v", cmds)
	}
	if got := noForge[2].Steps[0].(*steps.Command).Command; got != `echo "$version"` {
		t.Errorf("get-version command = %q", got)
	}
	if noForge[0].HelpText != "Prepare and create a new release (update changelog, bump version, create release)" ||
		noForge[1].HelpText != "Create a new change file to be included in the next release" ||
		noForge[2].HelpText != "Get the current version of the project" {
		t.Errorf("help texts: %q / %q / %q", noForge[0].HelpText, noForge[1].HelpText, noForge[2].HelpText)
	}
}

const everyStep = `
[[workflows]]
name = "all"
help_text = "Every step"

[[workflows.steps]]
type = "Command"
command = "echo hi"
shell = true
variables = { "$v" = "Version" }

[[workflows.steps]]
type = "BumpVersion"
rule = "Major"

[[workflows.steps]]
type = "BumpVersion"
rule = "Pre"
label = "rc"

[[workflows.steps]]
type = "BumpVersion"
rule = "Release"

[[workflows.steps]]
type = "PrepareRelease"
prerelease_label = "beta"
allow_empty = true
ignore_conventional_commits = true

[[workflows.steps]]
type = "Release"

[[workflows.steps]]
type = "CreateChangeFile"

[[workflows.steps]]
type = "CreatePullRequest"
base = "main"
title = { template = "Release $version", variables = { "$version" = "Version" } }
body = { template = "$changelog" }

[[workflows.steps]]
type = "SelectJiraIssue"
status = "Ready"

[[workflows.steps]]
type = "TransitionJiraIssue"
status = "Done"

[[workflows.steps]]
type = "SelectGitHubIssue"
labels = ["a", "b"]

[[workflows.steps]]
type = "SelectGiteaIssue"
labels = ["c"]

[[workflows.steps]]
type = "SelectIssueFromBranch"

[[workflows.steps]]
type = "SwitchBranches"

[[workflows.steps]]
type = "RebaseBranch"
to = "main"
`

func TestDecodeEveryStepType(t *testing.T) {
	cfg := mustLoad(t, everyStep)
	got := stepsOf(t, cfg, "all")
	want := []workflow.Step{
		&steps.Command{Command: "echo hi", Shell: true, Variables: []variables.Entry{{Key: "$v", Variable: variables.Version}}},
		&steps.BumpVersion{Rule: steps.RuleMajor},
		&steps.BumpVersion{Rule: steps.RulePre, Label: "rc"},
		&steps.BumpVersion{Rule: steps.RuleRelease},
		&steps.PrepareRelease{PrereleaseLabel: "beta", AllowEmpty: true, IgnoreConventionalCommits: true},
		&steps.Release{},
		&steps.CreateChangeFile{},
		&steps.CreatePullRequest{
			Base: "main",
			Title: variables.Template{
				Text:      "Release $version",
				Variables: []variables.Entry{{Key: "$version", Variable: variables.Version}},
			},
			Body: variables.Template{Text: "$changelog"},
		},
		&steps.SelectJiraIssue{Status: "Ready"},
		&steps.TransitionJiraIssue{Status: "Done"},
		&steps.SelectGitHubIssue{Labels: []string{"a", "b"}},
		&steps.SelectGiteaIssue{Labels: []string{"c"}},
		&steps.SelectIssueFromBranch{},
		&steps.SwitchBranches{},
		&steps.RebaseBranch{To: "main"},
	}
	if len(got) != len(want) {
		t.Fatalf("decoded %d steps, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("step %d:\n got %#v\nwant %#v", i, got[i], want[i])
		}
	}
	if cfg.Workflows[0].HelpText != "Every step" {
		t.Errorf("help text = %q", cfg.Workflows[0].HelpText)
	}
}

func TestUnknownKeysAreIgnored(t *testing.T) {
	cfg := mustLoad(t, `
mystery = 1
[changes]
ignore_conventional_commits = true
future = "x"
[package]
versioned_files = ["Cargo.toml"]
surprise = [1, 2]
[[workflows]]
name = "w"
extra = true
[[workflows.steps]]
type = "Command"
command = "echo"
label = "not a Command field"
[[workflows.steps]]
type = "SelectGitHubIssue"
label = "selected"
`)
	if !cfg.Changes.IgnoreConventionalCommits || cfg.Package == nil {
		t.Errorf("known keys were lost: %+v", cfg)
	}
	if sel := stepsOf(t, cfg, "w")[1].(*steps.SelectGitHubIssue); sel.Labels != nil {
		t.Errorf("docs-style `label` must be ignored, got %v", sel.Labels)
	}
}

func TestVariablesKeepDeclarationOrder(t *testing.T) {
	cases := map[string]struct {
		toml string
		want []string
	}{
		"table, reverse alphabetical": {
			toml: `
[[workflows]]
name = "w"
[[workflows.steps]]
type = "Command"
command = "x"
[workflows.steps.variables]
"$z" = "Version"
"$m" = "ChangelogEntry"
"$a" = "IssueBranch"
`,
			want: []string{"$z", "$m", "$a"},
		},
		"inline table": {
			toml: `
[[workflows]]
name = "w"
steps = [{ type = "Command", command = "x", variables = { c = "Version", b = "Version", a = "Version" } }]
`,
			want: []string{"c", "b", "a"},
		},
		"dotted keys": {
			toml: `
[[workflows]]
name = "w"
[[workflows.steps]]
type = "Command"
command = "x"
variables.second = "Version"
variables.first = "Version"
`,
			want: []string{"second", "first"},
		},
		"keys with dots and quotes": {
			toml: `
[[workflows]]
name = "w"
[[workflows.steps]]
type = "Command"
command = "x"
[workflows.steps.variables]
"{version}.x" = "Version"
"$$$$" = "Version"
"a b" = "Version"
`,
			want: []string{"{version}.x", "$$$$", "a b"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cmd := stepsOf(t, mustLoad(t, tc.toml), "w")[0].(*steps.Command)
			var keys []string
			for _, e := range cmd.Variables {
				keys = append(keys, e.Key)
			}
			if !slices.Equal(keys, tc.want) {
				t.Errorf("variable order = %q, want %q", keys, tc.want)
			}
		})
	}
}

func TestVariablesOrderOnSecondStepAndPullRequest(t *testing.T) {
	cfg := mustLoad(t, `
[[workflows]]
name = "w"
[[workflows.steps]]
type = "Command"
command = "a"
variables = { only = "Version" }
[[workflows.steps]]
type = "Command"
command = "b"
variables = { y = "Version", x = "Version" }
[[workflows.steps]]
type = "CreatePullRequest"
base = "main"
[workflows.steps.title]
template = "t"
[workflows.steps.title.variables]
"$b" = "Version"
"$a" = "Version"
[workflows.steps.body]
template = "b"
variables = { q = "Version", p = "Version" }
`)
	ss := stepsOf(t, cfg, "w")
	keys := func(es []variables.Entry) string {
		var out []string
		for _, e := range es {
			out = append(out, e.Key)
		}
		return strings.Join(out, ",")
	}
	if got := keys(ss[1].(*steps.Command).Variables); got != "y,x" {
		t.Errorf("second command variables = %s", got)
	}
	pr := ss[2].(*steps.CreatePullRequest)
	if got := keys(pr.Title.Variables); got != "$b,$a" {
		t.Errorf("title variables = %s", got)
	}
	if got := keys(pr.Body.Variables); got != "q,p" {
		t.Errorf("body variables = %s", got)
	}
}

func TestVariablesAbsentVersusEmpty(t *testing.T) {
	cfg := mustLoad(t, `
[[workflows]]
name = "w"
[[workflows.steps]]
type = "Command"
command = "absent"
[[workflows.steps]]
type = "Command"
command = "empty"
variables = {}
`)
	ss := stepsOf(t, cfg, "w")
	if v := ss[0].(*steps.Command).Variables; v != nil {
		t.Errorf("absent variables should be nil, got %#v", v)
	}
	if v := ss[1].(*steps.Command).Variables; v == nil || len(v) != 0 {
		t.Errorf("`variables = {}` should be empty and non-nil, got %#v", v)
	}
}

func TestPackagesKeepDeclarationOrderAndDottedNames(t *testing.T) {
	cfg := mustLoad(t, `
[packages.zebra]
versioned_files = ["z/Cargo.toml"]
[packages."foo.bar"]
changelog = "foo/CHANGELOG.md"
[packages."@scope/pkg.js"]
scopes = ["a", "b"]
[packages.apple]
ignore_go_major_versioning = true
`)
	var names []string
	for _, p := range cfg.Packages {
		names = append(names, p.Name)
	}
	if want := []string{"zebra", "foo.bar", "@scope/pkg.js", "apple"}; !slices.Equal(names, want) {
		t.Fatalf("package order = %q, want %q", names, want)
	}
	if cfg.Packages[1].Changelog != "foo/CHANGELOG.md" || !slices.Equal(cfg.Packages[2].Scopes, []string{"a", "b"}) ||
		!cfg.Packages[3].IgnoreGoMajorVersioning || cfg.Packages[0].VersionedFiles[0].Path != "z/Cargo.toml" {
		t.Errorf("package fields were not decoded: %+v", cfg.Packages)
	}
	if cfg.PackageCount() != 4 {
		t.Errorf("PackageCount() = %d", cfg.PackageCount())
	}
}

func TestDecodeTopLevelSchema(t *testing.T) {
	dep := "serde"
	cfg := mustLoad(t, `
[changes]
ignore_conventional_commits = true

[release_notes]
change_templates = ["#### $summary", "$details"]

[package]
changelog = "CHANGELOG.md"
scopes = ["core"]
versioned_files = [
  "Cargo.toml",
  { path = "Cargo.lock", dependency = "serde" },
  { path = "README.md", regex = "version (?<version>\\d+)" },
  { path = "docs.md", regex = ["a(?<version>1)", "b(?<version>2)"] },
]
extra_changelog_sections = [{ name = "Notes", footers = ["Changelog-Note"], types = ["docs"] }]
assets = [{ path = "dist/*.zip", name = "bundle" }]

[github]
owner = "celest-io"
repo = "remeris"

[jira]
url = "https://x.atlassian.net"
project = "PRJ"
`)
	if !cfg.Changes.IgnoreConventionalCommits {
		t.Error("changes not decoded")
	}
	if !slices.Equal(cfg.ReleaseNotes.ChangeTemplates, []string{"#### $summary", "$details"}) {
		t.Errorf("release notes = %+v", cfg.ReleaseNotes)
	}
	wantFiles := []VersionedFile{
		{Path: "Cargo.toml"},
		{Path: cargoLockName, Dependency: &dep},
		{Path: "README.md", Regex: []string{`version (?<version>\d+)`}},
		{Path: "docs.md", Regex: []string{"a(?<version>1)", "b(?<version>2)"}},
	}
	if !reflect.DeepEqual(cfg.Package.VersionedFiles, wantFiles) {
		t.Errorf("versioned files:\n got %+v\nwant %+v", cfg.Package.VersionedFiles, wantFiles)
	}
	if want := []ExtraSection{
		{Name: "Notes", Footers: []string{"Changelog-Note"}, Types: []string{"docs"}},
	}; !reflect.DeepEqual(
		cfg.Package.ExtraChangelogSections,
		want,
	) {
		t.Errorf("extra sections = %+v", cfg.Package.ExtraChangelogSections)
	}
	if want := (&Assets{List: []Asset{{Path: "dist/*.zip", Name: "bundle"}}}); !reflect.DeepEqual(cfg.Package.Assets, want) {
		t.Errorf("assets = %+v", cfg.Package.Assets)
	}
	if *cfg.GitHub != (GitHub{Owner: "celest-io", Repo: "remeris"}) || *cfg.Jira != (Jira{"https://x.atlassian.net", "PRJ"}) {
		t.Errorf("forge sections: %+v %+v %+v", cfg.GitHub, cfg.Gitea, cfg.Jira)
	}
}

func TestDecodeGitea(t *testing.T) {
	cfg := mustLoad(t, "[gitea]\nowner = \"o\"\nrepo = \"r\"\nhost = \"https://codeberg.org\"\n")
	if *cfg.Gitea != (Gitea{"o", "r", "https://codeberg.org"}) {
		t.Errorf("gitea = %+v", cfg.Gitea)
	}
}

const distGlob = "dist/*"

func TestAssetsFormsStayApart(t *testing.T) {
	cases := map[string]struct {
		toml string
		want *Assets
	}{
		"glob":             {"[package]\nassets = \"dist/*\"\n", &Assets{IsGlob: true, Glob: distGlob}},
		"list of one star": {"[package]\nassets = [{ path = \"dist/*\" }]\n", &Assets{List: []Asset{{Path: distGlob}}}},
		"empty list":       {"[package]\nassets = []\n", &Assets{List: []Asset{}}},
		"absent":           {packageHeader, nil},
		"array of tables": {
			"[[package.assets]]\npath = \"a\"\nname = \"b\"\n", &Assets{List: []Asset{{Path: "a", Name: "b"}}},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := mustLoad(t, tc.toml).Package.Assets
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("assets = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestInvalidWorkflowNames(t *testing.T) {
	for name, workflowName := range map[string]string{
		"dot":       "a.b",
		"space":     "has space",
		"trailing":  "release ",
		"empty":     "",
		"slash":     "a/b",
		"non-ascii": "relêase",
		"colon":     "a:b",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := load(t, "[[workflows]]\nname = \""+workflowName+"\"\n")
			want := "Workflow names may only contain letters, digits, '-' and '_': " + workflowName
			if err == nil || err.Error() != want {
				t.Errorf("err = %v, want %q", err, want)
			}
		})
	}
	for _, ok := range []string{wfRelease, wfDocumentChange, "get_version", "A1", "9"} {
		if _, err := load(t, "[[workflows]]\nname = \""+ok+"\"\n"); err != nil {
			t.Errorf("name %q rejected: %v", ok, err)
		}
	}
}

func TestMissingWorkflowNameIsInvalid(t *testing.T) {
	_, err := load(t, "[[workflows]]\nhelp_text = \"x\"\n")
	if err == nil || !strings.HasPrefix(err.Error(), "Workflow names may only contain") {
		t.Errorf("err = %v", err)
	}
}

func TestTomlSyntaxError(t *testing.T) {
	_, err := load(t, "[[workflows]]\nname = \n")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.HasPrefix(err.Error(), "TOML parse error at line 2, column ") {
		t.Errorf("message = %q", err.Error())
	}
	if d, ok := err.(diag.Diagnostic); !ok || d.Help() != "Check the TOML is valid." || d.Code() != "config::toml" {
		t.Errorf("diagnostic = %#v", err)
	}
}

func TestInvalidStepsAndVariables(t *testing.T) {
	cases := map[string]struct {
		toml    string
		wantErr string
	}{
		"unknown type": {
			"[[workflows]]\nname=\"w\"\n[[workflows.steps]]\ntype=\"Nope\"\n",
			`Unknown step type "Nope"`,
		},
		"missing type":     {"[[workflows]]\nname=\"w\"\n[[workflows.steps]]\ncommand=\"x\"\n", "missing its 'type' key"},
		"step not a table": {"[[workflows]]\nname=\"w\"\nsteps = [\"Release\"]\n", "must be a table with a 'type' key"},
		"bad bump rule": {
			"[[workflows]]\nname=\"w\"\n[[workflows.steps]]\ntype=\"BumpVersion\"\nrule=\"Huge\"\n",
			`Unknown BumpVersion rule "Huge"`,
		},
		"missing bump rule": {
			"[[workflows]]\nname=\"w\"\n[[workflows.steps]]\ntype=\"BumpVersion\"\n",
			`Unknown BumpVersion rule ""`,
		},
		"pre without label": {
			"[[workflows]]\nname=\"w\"\n[[workflows.steps]]\ntype=\"BumpVersion\"\nrule=\"Pre\"\n",
			"requires a 'label'",
		},
		"unknown variable": {
			"[[workflows]]\nname=\"w\"\n[[workflows.steps]]\ntype=\"Command\"\ncommand=\"x\"\nvariables={a=\"Nope\"}\n",
			"Unknown variable Nope",
		},
		"variable not string": {
			"[[workflows]]\nname=\"w\"\n[[workflows.steps]]\ntype=\"Command\"\ncommand=\"x\"\nvariables={a=1}\n",
			"Unknown variable 1",
		},
		"variables not table": {
			"[[workflows]]\nname=\"w\"\n[[workflows.steps]]\ntype=\"Command\"\ncommand=\"x\"\nvariables=\"x\"\n",
			"'variables' must be a table",
		},
		"wrong field type": {
			"[[workflows]]\nname=\"w\"\n[[workflows.steps]]\ntype=\"Command\"\ncommand=3\nshell=\"yes\"\n",
			"Invalid configuration in knope.toml",
		},
		"versioned file no path": {"[package]\nversioned_files=[{dependency=\"x\"}]\n", "needs a string 'path'"},
		"versioned file shape":   {"[package]\nversioned_files=[3]\n", "must be a path or a table"},
		"regex wrong type": {
			"[package]\nversioned_files=[{path=\"a\", regex=3}]\n",
			"'regex' of a must be a string or an array of strings",
		},
		"regex item wrong type": {
			"[package]\nversioned_files=[{path=\"a\", regex=[1]}]\n",
			"'regex' of a must be a string or an array of strings",
		},
		"dependency wrong type": {
			"[package]\nversioned_files=[{path=\"a\", dependency=1}]\n",
			"'dependency' of a must be a string",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := load(t, tc.toml)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestBumpVersionLabelOnlyForPre(t *testing.T) {
	cfg := mustLoad(
		t,
		"[[workflows]]\nname=\"w\"\n[[workflows.steps]]\ntype=\"BumpVersion\"\nrule=\"Minor\"\nlabel=\"ignored\"\n",
	)
	if got := stepsOf(t, cfg, "w")[0].(*steps.BumpVersion); got.Label != "" || got.Rule != steps.RuleMinor {
		t.Errorf("step = %+v", got)
	}
}

func TestWorkflowWithoutSteps(t *testing.T) {
	cfg := mustLoad(t, "[[workflows]]\nname = \"empty\"\nhelp_text = \"nothing\"\n")
	if len(cfg.Workflows) != 1 || len(cfg.Workflows[0].Steps) != 0 {
		t.Errorf("workflows = %+v", cfg.Workflows)
	}
}

func TestSpans(t *testing.T) {
	src := "[package]\nname = \"x\"\n\n[[workflows]]\nname = \"a\"\n\n[[workflows]]\nname = \"b\"\n"
	cfg := mustLoad(t, src)

	slice := func(path ...any) string {
		t.Helper()
		span, ok := cfg.Span(path...)
		if !ok {
			t.Fatalf("no span for %v", path)
		}
		return src[span.Start:span.End]
	}
	if got := slice("package"); got != "[package]\nname = \"x\"\n\n" {
		t.Errorf("package span = %q", got)
	}
	if got := slice("workflows", 0); got != "[[workflows]]\nname = \"a\"\n\n" {
		t.Errorf("workflows[0] span = %q", got)
	}
	if got := slice("workflows", 1); got != "[[workflows]]\nname = \"b\"\n" {
		t.Errorf("workflows[1] span = %q", got)
	}
	if _, ok := cfg.Span("workflows", 2); ok {
		t.Error("workflows[2] should not exist")
	}
	if _, ok := Default().Span("package"); ok {
		t.Error("a default config has no source to point into")
	}
}

func TestSpansOfPackagesAndVersionedFiles(t *testing.T) {
	src := "[packages.a]\nversioned_files = [\"Cargo.toml\", \"package.json\"]\n\n[packages.\"b.c\"]\nchangelog = \"X\"\n"
	cfg := mustLoad(t, src)

	span, ok := cfg.Span("packages", "b.c")
	if !ok || src[span.Start:span.End] != "[packages.\"b.c\"]\nchangelog = \"X\"\n" {
		t.Errorf("packages.b.c span = %v %q", ok, src[span.Start:span.End])
	}
	span, ok = cfg.Span("packages", "a", "versioned_files", 1)
	if !ok || src[span.Start:span.End] != `"package.json"` {
		t.Errorf("versioned_files[1] span = %v %q", ok, src[span.Start:span.End])
	}
}

func TestIndexKeys(t *testing.T) {
	idx, err := buildIndex([]byte(`
[packages.b]
[packages.a]
[packages.b.child]
x = { z = 1, y = 2 }
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := idx.Keys("packages"); !slices.Equal(got, []string{"b", "a"}) {
		t.Errorf("packages keys = %v", got)
	}
	if got := idx.Keys("packages", "b", "child", "x"); !slices.Equal(got, []string{"z", "y"}) {
		t.Errorf("inline table keys = %v", got)
	}
	if got := idx.Keys("nope"); got != nil {
		t.Errorf("missing table keys = %v", got)
	}
	if got := idx.Keys("packages", 0); got != nil {
		t.Errorf("wrong path shape = %v", got)
	}
}
