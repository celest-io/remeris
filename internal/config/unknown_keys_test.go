package config

import (
	"slices"
	"testing"
)

func TestUnknownKeys(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		toml string
		want []string
	}{
		"none": {
			toml: `[changes]
ignore_conventional_commits = true

[package]
versioned_files = ["Cargo.toml", { path = "package.json", dependency = "x" }]
changelog = "CHANGELOG.md"
scopes = ["a"]
extra_changelog_sections = [{ name = "N", footers = ["f"], types = ["t"] }]
assets = [{ path = "a", name = "b" }]

[[workflows]]
name = "w"
help_text = "h"

[[workflows.steps]]
type = "Command"
command = "echo"
shell = true
variables = { "$version" = "Version", "any.thing" = "Version" }

[[workflows.steps]]
type = "CreatePullRequest"
base = "main"
title = { template = "t", variables = { "x" = "Version" } }
body = { template = "b" }

[github]
owner = "o"
repo = "r"

[release_notes]
change_templates = ["x"]
`,
		},
		"merge commits key is known": {
			toml: "[changes]\nignore_conventional_commits = true\nignore_conventional_merge_commits = true\n",
		},
		"top level, declaration order": {
			toml: "zeta = 1\n[changes]\n[alpha]\nx = 1\n",
			want: []string{"zeta", "alpha"},
		},
		"quoted and dotted keys": {
			toml: "\"a.b\" = 1\n[package]\n\"odd key\" = 1\nsub.x = 2\n",
			want: []string{`"a.b"`, `package."odd key"`, "package.sub"},
		},
		"named packages": {
			toml: "[packages.foo]\nchangelog = \"C\"\nbogus = 1\n[packages.\"b.ar\"]\nscope = []\n",
			want: []string{"packages.foo.bogus", `packages."b.ar".scope`},
		},
		"array elements": {
			toml: "[package]\nversioned_files = [{ path = \"package.json\", glob = 1 }]\nassets = [{ path = \"a\", size = 1 }]\n",
			want: []string{"package.versioned_files[0].glob", "package.assets[0].size"},
		},
		"workflows and steps by type": {
			toml: `[[workflows]]
name = "a"
extra = 1

[[workflows.steps]]
type = "PrepareRelease"
command = "echo"
allow_empty = true

[[workflows.steps]]
type = "Release"
anything = 1

[[workflows]]
name = "b"
steps = [{ type = "Command", command = "x", shell = true, oops = 1 }]
`,
			want: []string{
				"workflows[0].extra",
				"workflows[0].steps[0].command",
				"workflows[0].steps[1].anything",
				"workflows[1].steps[0].oops",
			},
		},
		"template and forge tables": {
			toml: `[[workflows]]
name = "a"
steps = [{ type = "CreatePullRequest", base = "main", title = { template = "t", style = 1 } }]

[gitea]
owner = "o"
repo = "r"
host = "h"
token = "x"

[jira]
url = "u"
project = "p"
key = "k"
`,
			want: []string{"workflows[0].steps[0].title.style", "gitea.token", "jira.key"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := mustLoad(t, tc.toml).UnknownKeys()
			if !slices.Equal(got, tc.want) {
				t.Errorf("UnknownKeys() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUnknownKeysWithoutAFile(t *testing.T) {
	t.Parallel()
	if got := Default().UnknownKeys(); got != nil {
		t.Errorf("UnknownKeys() = %q", got)
	}
	var cfg *Config
	if got := cfg.UnknownKeys(); got != nil {
		t.Errorf("UnknownKeys() = %q", got)
	}
}
