package tomlwrite_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/celest-io/remeris/internal/tomlwrite"
)

// The documents below are the ones Knope serialised for its tests/generate and tests/upgrade fixtures. Each one is
// built by hand, the way the --generate code will build it, and must print as the fixture file does, byte for byte.

const (
	releaseHelp    = "Prepare and create a new release (update changelog, bump version, create release)"
	documentHelp   = "Create a new change file to be included in the next release"
	versionHelp    = "Get the current version of the project"
	commitRelease  = `git commit -m "chore: prepare release $version"`
	commitReleases = `git commit -m "chore: prepare releases"`
	gitPush        = "git push"
	gitPushTags    = "git push --tags"
	stepKey        = "type"
	commandStep    = "Command"
	releaseStep    = "Release"
)

func strs(items ...string) *tomlwrite.Array {
	a := tomlwrite.NewArray()
	for _, item := range items {
		a.Append(tomlwrite.String(item))
	}
	return a
}

// step is a workflow step: `type` first, then the other keys.
func step(typ string, kv ...string) *tomlwrite.Table {
	t := tomlwrite.NewTable()
	t.Set(stepKey, tomlwrite.String(typ))
	for i := 0; i+1 < len(kv); i += 2 {
		t.Set(kv[i], tomlwrite.String(kv[i+1]))
	}
	return t
}

func command(line string) *tomlwrite.Table { return step(commandStep, "command", line) }

func workflow(name, help string, steps ...*tomlwrite.Table) *tomlwrite.Table {
	w := tomlwrite.NewTable()
	w.Set("name", tomlwrite.String(name))
	if help != "" {
		w.Set("help_text", tomlwrite.String(help))
	}
	w.Set("steps", tomlwrite.NewArrayOfTables(steps...))
	return w
}

// release is the release workflow: PrepareRelease, the commit, then the steps in tail.
func release(commit string, tail ...*tomlwrite.Table) *tomlwrite.Table {
	steps := append([]*tomlwrite.Table{step("PrepareRelease"), command(commit)}, tail...)
	return workflow("release", releaseHelp, steps...)
}

func documentChange() *tomlwrite.Table {
	return workflow("document-change", documentHelp, step("CreateChangeFile"))
}

func getVersion() *tomlwrite.Table {
	return workflow("get-version", versionHelp, command(`echo "$version"`))
}

func workflows(ws ...*tomlwrite.Table) *tomlwrite.Array { return tomlwrite.NewArrayOfTables(ws...) }

func packageTable(files tomlwrite.Value, extra ...string) *tomlwrite.Table {
	t := tomlwrite.NewTable()
	t.Set("versioned_files", files)
	for i := 0; i+1 < len(extra); i += 2 {
		t.Set(extra[i], tomlwrite.String(extra[i+1]))
	}
	return t
}

func forge(kv ...string) *tomlwrite.Table {
	t := tomlwrite.NewTable()
	for i := 0; i+1 < len(kv); i += 2 {
		t.Set(kv[i], tomlwrite.String(kv[i+1]))
	}
	return t
}

func cargoWorkspace() *tomlwrite.Doc {
	first := packageTable(strs("first/Cargo.toml"))
	first.Set("scopes", strs("first-package"))
	dependency := tomlwrite.NewInlineTable()
	dependency.Set("path", tomlwrite.String("first/Cargo.toml"))
	dependency.Set("dependency", tomlwrite.String("second-package"))
	second := packageTable(tomlwrite.NewArray(dependency, tomlwrite.String("second/Cargo.toml")))
	second.Set("scopes", strs("second-package"))
	packages := tomlwrite.NewTable()
	packages.Set("first-package", first)
	packages.Set("second-package", second)

	d := tomlwrite.NewDoc()
	d.Set("packages", packages)
	d.Set("workflows", workflows(
		release(commitReleases, step(releaseStep), command(gitPush), command(gitPushTags)), documentChange()))
	return d
}

func defaultDoc(release *tomlwrite.Table) *tomlwrite.Doc {
	d := tomlwrite.NewDoc()
	d.Set("workflows", workflows(release, documentChange()))
	return d
}

func packageDoc(changelog bool) *tomlwrite.Doc {
	pkg := packageTable(strs("Cargo.toml", "package.json", "pyproject.toml"))
	if changelog {
		pkg.Set("changelog", tomlwrite.String("CHANGELOG.md"))
	}
	d := tomlwrite.NewDoc()
	d.Set("package", pkg)
	d.Set("workflows", workflows(
		release(commitRelease, step(releaseStep), command(gitPush), command(gitPushTags)), documentChange(), getVersion()))
	return d
}

func upgradeDoc() *tomlwrite.Doc {
	changes := tomlwrite.NewTable()
	changes.Set("ignore_conventional_commits", tomlwrite.Bool(true))
	d := tomlwrite.NewDoc()
	d.Set("changes", changes)
	d.Set("package", packageTable(strs("Cargo.toml"), "changelog", "CHANGELOG.md"))
	d.Set("workflows", workflows(workflow("prepare-release", "", step("PrepareRelease"))))
	return d
}

func TestGoldenFromKnope(t *testing.T) {
	t.Parallel()
	pushFirst := release(commitRelease, command(gitPush), step(releaseStep))
	gitea := defaultDoc(pushFirst)
	gitea.Set("gitea", forge("owner", "knope-dev", "repo", "knope", "host", "https://codeberg.org"))
	github := defaultDoc(pushFirst)
	github.Set("github", forge("owner", "knope-dev", "repo", "knope"))

	tests := map[string]struct {
		file string
		doc  *tomlwrite.Doc
	}{
		"generate cargo_workspace": {"generate_cargo_workspace.toml", cargoWorkspace()},
		"generate gitea":           {"generate_gitea.toml", gitea},
		"generate github":          {"generate_github.toml", github},
		"generate no_remote": {
			"generate_no_remote.toml",
			defaultDoc(release(commitRelease, step(releaseStep), command(gitPush), command(gitPushTags))),
		},
		"generate packages":                {"generate_packages.toml", packageDoc(false)},
		"generate packages_with_changelog": {"generate_packages_with_changelog.toml", packageDoc(true)},
		"upgrade ignore_conventional":      {"upgrade_ignore_conventional_commits.toml", upgradeDoc()},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			if got := tc.doc.String(); got != string(want) {
				t.Errorf("%s differs\n--- got ---\n%s\n--- want ---\n%s", tc.file, got, want)
			}
		})
	}
}
