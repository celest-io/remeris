package steps_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/steps"
	"github.com/celest-io/remeris/internal/workflow"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/releasenotes"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

const (
	fakeDate      = "2026-01-01"
	changelogFile = "CHANGELOG.md"
	featCommit    = "feat: Something new"
	fixCommit     = "fix: Something fixed"
	changeFile    = ".changeset/note.md"
	prNumber      = "$pr_number"
	summaryLine   = "- $summary"
	patchNote     = "---\ndefault: patch\n---\n\n# A note\n"
	v200          = "2.0.0"
	v130          = "1.3.0"
	v124          = "1.2.4"
	authorAnn     = "Ann"
	hashAbc       = "abc1234"
)

// recordingGit serves canned commits and records what steps ask of it.
type recordingGit struct {
	commits []git.Commit
	tagArg  *string
	called  bool
	staged  []string
	// tagged are the names given to CreateTag, in order.
	tagged []string
	// tagErr is returned by CreateTag.
	tagErr error
	// forbidTag, when set, makes every CreateTag call fail this test.
	forbidTag *testing.T
	// head and headErr are what HeadCommitSHA returns.
	head    string
	headErr error
}

func (*recordingGit) AllTagsOnBranch(context.Context) ([]string, error) { return nil, nil }

func (g *recordingGit) CommitMessagesAfterTag(_ context.Context, tag *string) ([]git.Commit, error) {
	g.called, g.tagArg = true, tag
	return g.commits, nil
}

func (*recordingGit) FirstCommitsForFiles(context.Context, []string) map[string]*git.CommitInfo {
	return nil
}

func (g *recordingGit) AddFiles(_ context.Context, paths []string) error {
	g.staged = append(g.staged, paths...)
	return nil
}

func (g *recordingGit) CreateTag(_ context.Context, name string, _ git.TagOptions) error {
	if g.forbidTag != nil {
		g.forbidTag.Errorf("CreateTag(%q) must not be called", name)
	}
	g.tagged = append(g.tagged, name)
	return g.tagErr
}

func (g *recordingGit) HeadCommitSHA(context.Context) (string, error) { return g.head, g.headErr }

func (*recordingGit) CurrentBranch(context.Context) (string, error) { return "", nil }

func (*recordingGit) FirstRemoteURL(context.Context) (string, bool) { return "", false }

func commits(messages ...string) []git.Commit {
	out := make([]git.Commit, len(messages))
	for i, m := range messages {
		out[i] = git.Commit{Message: m, Info: &git.CommitInfo{Hash: hashAbc, AuthorName: authorAnn}}
	}
	return out
}

// prepareDir moves to a temporary directory holding Cargo.toml (version 1.2.3) and optional change files.
func prepareDir(t *testing.T, changeFiles map[string]string) {
	t.Helper()
	t.Chdir(t.TempDir())
	today := steps.Today
	steps.Today = func() string { return fakeDate }
	t.Cleanup(func() { steps.Today = today })
	if err := os.WriteFile(cargoFile, []byte(cargo(v123)), 0o600); err != nil {
		t.Fatal(err)
	}
	for path, body := range changeFiles {
		if err := os.MkdirAll(".changeset", 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func runPrepare(
	t *testing.T, step *steps.PrepareRelease, state workflow.State, dryRun bool,
) (workflow.RunType[workflow.State], error) {
	t.Helper()
	rt := workflow.Real(state)
	if dryRun {
		rt = workflow.DryRun(state)
	}
	return step.Run(context.Background(), rt)
}

func TestPrepareReleaseNoPackages(t *testing.T) {
	prepareDir(t, nil)
	_, err := runPrepare(t, &steps.PrepareRelease{}, workflow.State{}, false)
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Error() != "No packages to operate on" || d.Help() == "" {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareReleaseGatherChanges(t *testing.T) {
	cases := map[string]struct {
		step       steps.PrepareRelease
		global     bool
		tags       []string
		commits    []string
		wantCommit bool   // CommitMessagesAfterTag was called
		wantTag    string // tag it was called with, "" for none
		wantFile   string // version in Cargo.toml after the run
	}{
		"all commits without a tag": {
			commits: []string{featCommit}, wantCommit: true, wantFile: v130,
		},
		"commits since the last stable tag, not the pre-release one": {
			tags: []string{"v1.3.0-rc.1", tagV123}, commits: []string{fixCommit},
			wantCommit: true, wantTag: tagV123, wantFile: v124,
		},
		"step ignores commits": {
			step: steps.PrepareRelease{IgnoreConventionalCommits: true}, commits: []string{featCommit},
			wantFile: v123,
		},
		"global setting ignores commits": {
			global: true, commits: []string{featCommit}, wantFile: v123,
		},
		"commits that are not conventional are skipped": {
			commits: []string{"Update the readme"}, wantCommit: true, wantFile: v123,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			prepareDir(t, nil)
			g := &recordingGit{commits: commits(tc.commits...)}
			state := stateFor(t, tc.tags, cargo(v123), "")
			state.Git, state.IgnoreConventionalCommits = g, tc.global
			step := tc.step
			step.AllowEmpty = true

			if _, err := runPrepare(t, &step, state, false); err != nil {
				t.Fatal(err)
			}

			if g.called != tc.wantCommit {
				t.Errorf("commits read = %v, want %v", g.called, tc.wantCommit)
			}
			if tc.wantCommit && (g.tagArg == nil) != (tc.wantTag == "") || g.tagArg != nil && *g.tagArg != tc.wantTag {
				t.Errorf("tag = %v, want %q", g.tagArg, tc.wantTag)
			}
			got, err := os.ReadFile(cargoFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != cargo(tc.wantFile) {
				t.Errorf("Cargo.toml = %q, want version %s", got, tc.wantFile)
			}
		})
	}
}

func TestPrepareReleaseIgnoreMergeCommits(t *testing.T) {
	merge := func(msg string) git.Commit {
		return git.Commit{Message: msg, Info: &git.CommitInfo{Hash: hashAbc, AuthorName: authorAnn}, Parents: 2}
	}
	plain := func(msg string) git.Commit {
		return git.Commit{Message: msg, Info: &git.CommitInfo{Hash: hashAbc, AuthorName: authorAnn}, Parents: 1}
	}
	cases := map[string]struct {
		ignore   bool
		commits  []git.Commit
		wantFile string
	}{
		"conventional merge commit counts by default": {
			commits: []git.Commit{merge(featCommit)}, wantFile: v130,
		},
		"conventional merge commit is ignored when set": {
			ignore: true, commits: []git.Commit{merge(featCommit)}, wantFile: v123,
		},
		"commits merged in still count": {
			ignore: true, commits: []git.Commit{plain(fixCommit), merge(featCommit)}, wantFile: v124,
		},
		"non-merge commit that says Merge is not ignored": {
			ignore: true, commits: []git.Commit{plain("feat: Merge the two")}, wantFile: v130,
		},
		"non-merge conventional commit is kept when set": {
			ignore: true, commits: []git.Commit{plain(featCommit)}, wantFile: v130,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			prepareDir(t, nil)
			state := stateFor(t, nil, cargo(v123), "")
			state.Git = &recordingGit{commits: tc.commits}
			state.IgnoreConventionalMergeCommits = tc.ignore
			step := steps.PrepareRelease{AllowEmpty: true}

			if _, err := runPrepare(t, &step, state, false); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(cargoFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != cargo(tc.wantFile) {
				t.Errorf("Cargo.toml = %q, want version %s", got, tc.wantFile)
			}
		})
	}
}

func TestPrepareReleaseScopes(t *testing.T) {
	cases := map[string]struct {
		scopes []string
		want   string
	}{
		"no scope filter":         {nil, v200},
		"matching scope":          {[]string{"first"}, v200},
		"case-insensitive scope":  {[]string{"FIRST"}, v200},
		"scope of another record": {[]string{"second"}, "1.2.4"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			prepareDir(t, nil)
			state := stateFor(t, []string{tagV123}, cargo(v123), "")
			state.Packages[0].Scopes = tc.scopes
			state.Git = &recordingGit{commits: commits("feat(first)!: Break it", fixCommit)}

			if _, err := runPrepare(t, &steps.PrepareRelease{}, state, false); err != nil {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(cargoFile); string(got) != cargo(tc.want) {
				t.Errorf("Cargo.toml = %q, want version %s", got, tc.want)
			}
		})
	}
}

func TestPrepareReleaseNoPackagesReady(t *testing.T) {
	cases := map[string]struct {
		step    steps.PrepareRelease
		dryRun  bool
		wantErr bool
	}{
		"real run":                  {steps.PrepareRelease{}, false, true},
		"dry run never fails":       {steps.PrepareRelease{}, true, false},
		"allow_empty in a real run": {steps.PrepareRelease{AllowEmpty: true}, false, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			prepareDir(t, nil)
			state := stateFor(t, []string{tagV123}, cargo(v123), "")
			state.Git = &recordingGit{commits: commits("docs: Update the readme")}

			_, err := runPrepare(t, &tc.step, state, tc.dryRun)

			var d diag.Diagnostic
			switch {
			case !tc.wantErr && err != nil:
				t.Fatal(err)
			case tc.wantErr && (!errors.As(err, &d) || d.Error() != "No packages are ready to release" ||
				!strings.HasPrefix(d.Help(), "The `PrepareRelease` step will not complete")):
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestPrepareReleaseForgeVariable(t *testing.T) {
	cases := map[string]struct {
		template   string
		github     bool
		commits    []string
		wantErr    string
		wantStaged bool
	}{
		"no forge variable":               {summaryLine, false, []string{featCommit}, "", true},
		"variable but nothing to release": {prNumber, false, nil, "", false},
		"pr_author_login without [github]": {
			"$pr_author_login",
			false,
			[]string{featCommit},
			"You must configure [github] to use the variable $pr_author_login in a change template",
			false,
		},
		"pr_number without [github]": {
			prNumber,
			false,
			[]string{featCommit},
			"You must configure [github] to use the variable $pr_number in a change template",
			false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			prepareDir(t, nil)
			g := &recordingGit{commits: commits(tc.commits...)}
			state := stateFor(t, []string{tagV123}, cargo(v123), "")
			state.Git = g
			if tc.github {
				state.GitHub = newTestForge()
			}
			state.Packages[0].ReleaseNotes.ChangeTemplates = []releasenotes.ChangeTemplate{
				releasenotes.ChangeTemplate(tc.template),
			}

			_, err := runPrepare(t, &steps.PrepareRelease{AllowEmpty: true}, state, false)

			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			if (len(g.staged) > 0) != tc.wantStaged {
				t.Errorf("staged = %v", g.staged)
			}
		})
	}
}

func TestPrepareReleaseStagesInOrder(t *testing.T) {
	prepareDir(t, map[string]string{changeFile: patchNote})
	g := &recordingGit{commits: commits(featCommit)}
	state := stateFor(t, []string{tagV123}, cargo(v123), "")
	state.Git = g

	rt, err := runPrepare(t, &steps.PrepareRelease{}, state, false)
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{cargoFile, changeFile}; !equal(g.staged, want) {
		t.Errorf("staged = %v, want %v", g.staged, want)
	}
	if _, err := os.Stat(changeFile); !os.IsNotExist(err) {
		t.Error("the change file should be removed")
	}
	if len(rt.State.PendingActions) != 1 || rt.State.PendingActions[0].Kind != action.KindCreateRelease {
		t.Errorf("pending = %+v, want the CreateRelease", rt.State.PendingActions)
	}
	if rt.State.AllVersionedFiles != nil {
		t.Error("the versioned files should be drained")
	}
}

func TestPrepareReleaseDryRunStaging(t *testing.T) {
	prepareDir(t, map[string]string{changeFile: patchNote})
	logs := captureLogs(t)
	state := stateFor(t, []string{tagV123}, cargo(v123), "")
	state.Git = &recordingGit{commits: commits(featCommit)}

	if _, err := runPrepare(t, &steps.PrepareRelease{}, state, true); err != nil {
		t.Fatal(err)
	}

	want := "msg=\"Would add the following to Cargo.toml: version = 1.3.0\"\n" +
		"msg=\"Would delete .changeset/note.md\"\n" +
		"msg=\"Would add files to git:\"\n" +
		"msg=\"  Cargo.toml\"\n" +
		"msg=\"  .changeset/note.md\"\n"
	if logs.String() != want {
		t.Errorf("logs = %q, want %q", logs.String(), want)
	}
	if _, err := os.Stat(changeFile); err != nil {
		t.Error("a dry run must keep the change file")
	}
}

func TestPrepareReleasePrereleaseKeepsChangeFiles(t *testing.T) {
	prepareDir(t, map[string]string{changeFile: "---\ndefault: minor\n---\n\n# A note\n"})
	state := stateFor(t, []string{tagV123}, cargo(v123), "")
	state.Git = &recordingGit{}

	if _, err := runPrepare(t, &steps.PrepareRelease{PrereleaseLabel: "rc"}, state, false); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(changeFile); err != nil {
		t.Error("a pre-release must keep the change file")
	}
	if got, _ := os.ReadFile(cargoFile); string(got) != cargo("1.3.0-rc.0") {
		t.Errorf("Cargo.toml = %q", got)
	}
}

func TestPrepareReleaseSharedChangeFile(t *testing.T) {
	prepareDir(t, map[string]string{changeFile: "---\nfirst: patch\nsecond: patch\n---\n\n# Shared\n"})
	g := &recordingGit{}
	state := stateFor(t, []string{"first/" + tagV123, "second/" + tagV123}, cargo(v123), "first", "second")
	state.Git = g

	rt, err := runPrepare(t, &steps.PrepareRelease{}, state, false)
	if err != nil {
		t.Fatal(err)
	}

	// The same RemoveFile is made for both packages and the files are written once each.
	if want := []string{cargoFile, changeFile}; !equal(g.staged, want) {
		t.Errorf("staged = %v, want %v", g.staged, want)
	}
	if len(rt.State.PendingActions) != 2 {
		t.Errorf("pending = %d actions, want one release per package", len(rt.State.PendingActions))
	}
}

func TestPrepareReleaseOverrideIsConsumedOnce(t *testing.T) {
	prepareDir(t, nil)
	override, err := semver.Parse("5.0.0")
	if err != nil {
		t.Fatal(err)
	}
	state := stateFor(t, []string{tagV123}, cargo(v123), "")
	state.Git = &recordingGit{}
	state.Packages[0].Override = &override

	if _, err := runPrepare(t, &steps.PrepareRelease{}, state, false); err != nil {
		t.Fatal(err)
	}

	if state.Packages[0].Override != nil {
		t.Error("PrepareRelease should take the override")
	}
	if got, _ := os.ReadFile(cargoFile); string(got) != cargo("5.0.0") {
		t.Errorf("Cargo.toml = %q, want 5.0.0 with no changes at all", got)
	}
}

func TestPrepareReleaseChangeFileError(t *testing.T) {
	prepareDir(t, map[string]string{changeFile: "no front matter"})
	state := stateFor(t, nil, cargo(v123), "")
	state.Git = &recordingGit{}

	_, err := runPrepare(t, &steps.PrepareRelease{}, state, false)

	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Code() != "remeris::changesets::could_not_read_changeset" ||
		d.Error() != "missing front matter" || !strings.Contains(d.Help(), "formatting of a change file") {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareReleaseWritesChangelog(t *testing.T) {
	prepareDir(t, nil)
	if err := os.WriteFile(changelogFile, []byte("# Changelog\n\n## 1.2.3\n\nOld\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := stateFor(t, []string{tagV123}, cargo(v123), "")
	state.Git = &recordingGit{commits: commits(featCommit)}
	changelog := releasenotes.NewChangelog(changelogFile, "# Changelog\n\n## 1.2.3\n\nOld\n")
	state.Packages[0].ReleaseNotes.Changelog = &changelog

	if _, err := runPrepare(t, &steps.PrepareRelease{}, state, false); err != nil {
		t.Fatal(err)
	}

	want := "# Changelog\n\n## 1.3.0 (" + fakeDate + ")\n\n### Features\n\n- Something new\n\n## 1.2.3\n\nOld\n"
	if got, _ := os.ReadFile(changelogFile); string(got) != want {
		t.Errorf("changelog = %q, want %q", got, want)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
