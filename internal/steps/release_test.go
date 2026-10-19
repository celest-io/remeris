package steps_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/steps"
	"github.com/celest-io/remeris/internal/workflow"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	subGoMod    = "sub/go.mod"
	subTagV100  = "sub/v1.0.0"
	subTagV120  = "sub/v1.2.0"
	tagV100     = "v1.0.0"
	tagV120     = "v1.2.0"
	fooName     = "foo"
	fooTagV120  = "foo/v1.2.0"
	v120        = "1.2.0"
	lastTag100  = "Last tag is 1.0.0"
	searching   = "Searching for last package tag to determine if there's a release to release"
	noChangelog = "Release has previously been prepared without a changelog, will create a release with no notes."
)

// releaseState builds a state whose packages (named as in names, "" is the default one) list Cargo.toml at
// version 1.2.0, or, when withGoMod is set, list the Go module sub/go.mod at that version instead.
func releaseState(t *testing.T, tags []string, withGoMod bool, names ...string) workflow.State {
	t.Helper()
	cfg := versionedfile.Config{Path: cargoFile, Format: versionedfile.Cargo}
	content := cargo(v120)
	var goTags []string
	if withGoMod {
		// A go.mod has no version of its own: it takes the one of its tags, so this one is ahead of the package tags.
		cfg = versionedfile.Config{Path: subGoMod, Format: versionedfile.GoMod}
		content, goTags = "module example.com/m/sub\n\ngo 1.19\n", []string{subTagV120}
	}
	f, err := versionedfile.New(cfg, content, goTags)
	if err != nil {
		t.Fatal(err)
	}
	cfgs, all := []versionedfile.Config{cfg}, []versionedfile.File{f}
	state := workflow.State{AllVersionedFiles: all, AllGitTags: tags}
	for _, name := range names {
		pn := versioning.DefaultName()
		if name != "" {
			pn = versioning.CustomName(name)
		}
		pkg, err := versioning.NewPackage(pn, cfgs, all, tags, versionedfile.Standard, nil)
		if err != nil {
			t.Fatal(err)
		}
		state.Packages = append(state.Packages, pkg)
	}
	return state
}

func runRelease(state workflow.State, dryRun bool) (workflow.RunType[workflow.State], error) {
	rt := workflow.Real(state)
	if dryRun {
		rt = workflow.DryRun(state)
	}
	return (&steps.Release{}).Run(context.Background(), rt)
}

// captureDebugLogs is captureLogs at debug level, with one "msg" per line.
func captureDebugLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
				return slog.Attr{}
			}
			return a
		},
	})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func logLines(buf *bytes.Buffer) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			l = strings.TrimPrefix(l, "msg=")
			out = append(out, strings.Trim(l, `"`))
		}
	}
	return out
}

func releaseOf(version, name string) action.Action {
	var n *string
	if name != "" {
		n = &name
	}
	return action.CreateRelease(action.Release{Title: version, Version: semver.MustParse(version), PackageName: n})
}

func TestReleasePending(t *testing.T) {
	cases := map[string]struct {
		pending []action.Action
		want    []string
	}{
		"prepared pipeline": {
			pending: []action.Action{
				releaseOf(v120, ""), releaseOf(v120, fooName),
				action.AddTag(subTagV120),
				action.AddTag(tagV120), // the tag of the default package: its CreateRelease makes it
				action.WriteToFile("x", "y", "z"),
			},
			want: []string{tagV120, fooTagV120, subTagV120},
		},
		"add tag before the release of its package": {
			pending: []action.Action{action.AddTag(fooTagV120), releaseOf(v120, fooName)},
			want:    []string{fooTagV120},
		},
		"other kinds are skipped": {
			pending: []action.Action{action.RemoveFile("a"), action.WriteToFile("x", "y", "z")},
		},
	}
	for name, tc := range cases {
		for _, dry := range []bool{false, true} {
			t.Run(name, func(t *testing.T) {
				logs := captureLogs(t)
				g := &recordingGit{}
				state := releaseState(t, nil, false, "", fooName)
				state.Git, state.PendingActions = g, tc.pending
				if dry {
					g.forbidTag = t
				}

				rt, err := runRelease(state, dry)
				if err != nil {
					t.Fatal(err)
				}

				if len(rt.State.PendingActions) != 0 {
					t.Errorf("pending = %v, want none", rt.State.PendingActions)
				}
				if dry {
					var want []string
					for _, tag := range tc.want {
						want = append(want, "Would create Git tag "+tag)
					}
					if got := logLines(logs); !slices.Equal(got, want) {
						t.Errorf("log = %q, want %q", got, want)
					}
				} else if !slices.Equal(g.tagged, tc.want) {
					t.Errorf("tags = %q, want %q", g.tagged, tc.want)
				}
			})
		}
	}
}

func TestReleaseFindsPreparedRelease(t *testing.T) {
	cases := map[string]struct {
		tags      []string
		goMod     bool
		names     []string
		wantTags  []string
		wantDebug []string
	}{
		"newer than the last tag": {
			tags: []string{tagV100}, names: []string{""}, wantTags: []string{tagV120},
			wantDebug: []string{searching, lastTag100, noChangelog},
		},
		"no tag at all": {
			names: []string{""}, wantTags: []string{tagV120}, wantDebug: []string{searching, noChangelog},
		},
		"equal to the last tag": {
			tags: []string{tagV120}, names: []string{""}, wantDebug: []string{searching, "Last tag is 1.2.0"},
		},
		"named package": {
			tags: []string{"foo/v1.0.0"}, names: []string{fooName}, wantTags: []string{fooTagV120},
			wantDebug: []string{searching, lastTag100, noChangelog},
		},
		"go module in a subdirectory": {
			tags: []string{tagV100, subTagV100}, goMod: true, names: []string{""}, wantTags: []string{tagV120, subTagV120},
			wantDebug: []string{searching, lastTag100, noChangelog},
		},
		"two packages, one released": {
			tags: []string{tagV120, "foo/v1.0.0"}, names: []string{"", fooName}, wantTags: []string{fooTagV120},
			wantDebug: []string{searching, "Last tag is 1.2.0", searching, lastTag100, noChangelog},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			logs := captureDebugLogs(t)
			g := &recordingGit{}
			state := releaseState(t, tc.tags, tc.goMod, tc.names...)
			state.Git = g

			rt, err := runRelease(state, false)
			if err != nil {
				t.Fatal(err)
			}

			if !slices.Equal(g.tagged, tc.wantTags) {
				t.Errorf("tags = %q, want %q", g.tagged, tc.wantTags)
			}
			if got := logLines(logs); !slices.Equal(got, tc.wantDebug) {
				t.Errorf("log = %q, want %q", got, tc.wantDebug)
			}
			if len(rt.State.PendingActions) != 0 {
				t.Errorf("pending = %v, want none", rt.State.PendingActions)
			}
		})
	}
}

func TestReleaseDryRunDoesNotTouchGit(t *testing.T) {
	logs := captureLogs(t)
	g := &recordingGit{forbidTag: t}
	state := releaseState(t, []string{tagV100, subTagV100}, true, "")
	state.Git = g

	if _, err := runRelease(state, true); err != nil {
		t.Fatal(err)
	}

	want := []string{"Would create Git tag " + tagV120, "Would create Git tag " + subTagV120}
	if got := logLines(logs); !slices.Equal(got, want) {
		t.Errorf("log = %q, want %q", got, want)
	}
	if len(g.tagged) != 0 || g.called {
		t.Errorf("git used in a dry run: %+v", g)
	}
}

func TestReleaseDryRunWithoutGit(t *testing.T) {
	t.Chdir(t.TempDir()) // a nil Git must not be opened: there is no repository here
	logs := captureLogs(t)
	state := releaseState(t, []string{tagV100}, false, "")

	if _, err := runRelease(state, true); err != nil {
		t.Fatal(err)
	}
	if got := logLines(logs); !slices.Equal(got, []string{"Would create Git tag " + tagV120}) {
		t.Errorf("log = %q", got)
	}
}

func TestReleaseForges(t *testing.T) {
	cases := map[string]struct {
		github, gitea bool
		want          string
	}{
		"github":       {github: true, want: "Step Release is not implemented yet (milestone M7)"},
		"gitea":        {gitea: true, want: "Step Release is not implemented yet (milestone M8)"},
		"both: github": {github: true, gitea: true, want: "Step Release is not implemented yet (milestone M7)"},
	}
	for name, tc := range cases {
		for _, dry := range []bool{false, true} {
			t.Run(name, func(t *testing.T) {
				g := &recordingGit{forbidTag: t}
				state := releaseState(t, []string{tagV100}, false, "")
				state.Git, state.GitHubConfigured, state.GiteaConfigured = g, tc.github, tc.gitea

				_, err := runRelease(state, dry)

				if err == nil || err.Error() != tc.want {
					t.Errorf("err = %v, want %q", err, tc.want)
				}
			})
		}
	}
}

func TestReleaseForgeStillCreatesAddTag(t *testing.T) {
	g := &recordingGit{}
	state := releaseState(t, nil, false, "")
	state.Git, state.GitHubConfigured = g, true
	state.PendingActions = []action.Action{action.AddTag(subTagV120), releaseOf(v120, "")}

	_, err := runRelease(state, false)

	if err == nil || !strings.Contains(err.Error(), "M7") {
		t.Errorf("err = %v", err)
	}
	if !slices.Equal(g.tagged, []string{subTagV120}) {
		t.Errorf("tags = %q", g.tagged)
	}
}

func TestReleaseNoPackages(t *testing.T) {
	logs := captureDebugLogs(t)
	rt, err := runRelease(workflow.State{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rt.State.PendingActions) != 0 || logs.Len() != 0 {
		t.Errorf("pending = %v, log = %q", rt.State.PendingActions, logs)
	}
}

func TestReleaseCreateTagError(t *testing.T) {
	boom := errors.New("boom")
	g := &recordingGit{tagErr: boom}
	state := releaseState(t, []string{tagV100}, true, "")
	state.Git = g

	_, err := runRelease(state, false)

	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
	if len(g.tagged) != 1 {
		t.Errorf("tags = %q, want a stop at the first failure", g.tagged)
	}
}

func TestReleaseOpensGitWhenStateHasNone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Chdir(t.TempDir())
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.CommandContext(t.Context(), "git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	state := releaseState(t, []string{tagV100}, false, "")

	rt, err := runRelease(state, false)
	if err != nil {
		t.Fatal(err)
	}

	if rt.State.Git == nil {
		t.Error("the opened repository should be kept in the state")
	}
	out, err := exec.CommandContext(t.Context(), "git", "tag", "--list").Output()
	if err != nil || strings.TrimSpace(string(out)) != tagV120 {
		t.Errorf("tags = %q, err = %v", out, err)
	}
	if _, err := os.Stat(".git"); err != nil {
		t.Fatal(err)
	}
}
