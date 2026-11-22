package signing_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/signing"
)

const (
	branchMain = "main"
	fileOld    = "old.txt"
	nameGit    = "git"
	nameGitKey = "git and your own signing key"
	nameGitHub = "GitHub"
	apiGitHub  = "the GitHub API"
	guideApp   = "GitHub App"
	keygen     = "ssh-keygen"
	fileChange = "CHANGELOG.md"
	fileCS     = ".changeset/x.md"
	fileVer    = "VERSION"
	fileDocA   = "docs/a.md"
	fileDocB   = "docs/sub/b.md"
	headSHA    = "0123456789abcdef0123456789abcdef01234567"
	newSHA     = "89abcdef0123456789abcdef0123456789abcdef"
)

// fakeGit is a Git and Committer that records what it was asked.
type fakeGit struct {
	git.Git // only the methods below are used

	cfg      git.SigningConfig
	cfgErr   error
	root     string
	added    [][]string
	adopted  []string
	adoptErr error
	commits  []git.CommitOptions
	messages []string
	sha      string
}

func (f *fakeGit) Changes(context.Context) ([]git.Change, error) { return nil, nil }

func (f *fakeGit) CurrentBranch(context.Context) (string, error) { return branchMain, nil }
func (f *fakeGit) HeadCommitSHA(context.Context) (string, error) { return headSHA, nil }
func (f *fakeGit) AddFiles(_ context.Context, p []string) error {
	f.added = append(f.added, p)
	return nil
}
func (f *fakeGit) Root(context.Context) (string, error) { return f.root, nil }

func (f *fakeGit) AdoptCommit(_ context.Context, branch, sha string) error {
	f.adopted = append(f.adopted, branch+"@"+sha)
	return f.adoptErr
}

func (f *fakeGit) SigningConfig(context.Context) (git.SigningConfig, error) { return f.cfg, f.cfgErr }

func (f *fakeGit) Commit(_ context.Context, msg string, o git.CommitOptions) (string, error) {
	f.commits = append(f.commits, o)
	f.messages = append(f.messages, msg)
	return f.sha, nil
}

// fakeAPI is an APICommitter.
type fakeAPI struct {
	name       string
	configured bool
	got        []signing.APICommit
	err        error
}

func (a *fakeAPI) Name() string     { return a.name }
func (a *fakeAPI) Configured() bool { return a.configured }
func (a *fakeAPI) CommitFiles(_ context.Context, in signing.APICommit) (string, error) {
	a.got = append(a.got, in)
	return newSHA, a.err
}

func TestSelect(t *testing.T) {
	t.Parallel()
	yes, no := true, false
	signedKey := git.SigningConfig{Key: "KEY"}
	gh := func(configured bool) []signing.APICommitter {
		return []signing.APICommitter{&fakeAPI{name: nameGitHub, configured: configured}}
	}
	tests := map[string]struct {
		settings signing.Settings
		cfg      git.SigningConfig
		apis     []signing.APICommitter
		want     string // the signer name, or "" for the setup guide
		signed   bool
	}{
		"unset and nothing set up is a plain commit": {settings: signing.Settings{Signer: signing.MethodAuto}, want: nameGit},
		"unset ignores a configured forge": {
			settings: signing.Settings{Signer: signing.MethodAuto},
			apis:     gh(true),
			want:     nameGit,
		},
		"unset signs when git is set up to sign": {
			settings: signing.Settings{
				Signer: signing.MethodAuto,
			},
			cfg:    signedKey,
			apis:   gh(true),
			want:   nameGitKey,
			signed: true,
		},
		"gpgsign alone counts as set up": {
			settings: signing.Settings{
				Signer: signing.MethodAuto,
			},
			cfg:    git.SigningConfig{GPGSign: true},
			want:   nameGitKey,
			signed: true,
		},
		"false turns it off even when git is set up": {
			settings: signing.Settings{Sign: &no, Signer: signing.MethodAuto}, cfg: signedKey, apis: gh(true), want: nameGit,
		},
		"true uses git when it is set up": {
			settings: signing.Settings{
				Sign:   &yes,
				Signer: signing.MethodAuto,
			},
			cfg:    signedKey,
			apis:   gh(true),
			want:   nameGitKey,
			signed: true,
		},
		"true falls back to the forge": {
			settings: signing.Settings{Sign: &yes, Signer: signing.MethodAuto}, apis: gh(true), want: apiGitHub, signed: true,
		},
		"true skips a forge that is not configured": {
			settings: signing.Settings{Sign: &yes, Signer: signing.MethodAuto},
			apis:     []signing.APICommitter{&fakeAPI{name: "GitLab"}, &fakeAPI{name: nameGitHub, configured: true}},
			want:     apiGitHub, signed: true,
		},
		"true with nothing set up is the guide": {
			settings: signing.Settings{Sign: &yes, Signer: signing.MethodAuto},
			apis:     gh(false),
		},
		"git only with a key": {
			settings: signing.Settings{
				Sign:   &yes,
				Signer: signing.MethodGit,
			},
			cfg:    signedKey,
			apis:   gh(true),
			want:   nameGitKey,
			signed: true,
		},
		"git only never uses the forge": {settings: signing.Settings{Sign: &yes, Signer: signing.MethodGit}, apis: gh(true)},
		"api only never uses git":       {settings: signing.Settings{Sign: &yes, Signer: signing.MethodAPI}, cfg: signedKey},
		"api only with a forge": {
			settings: signing.Settings{
				Sign:   &yes,
				Signer: signing.MethodAPI,
			},
			cfg:    signedKey,
			apis:   gh(true),
			want:   apiGitHub,
			signed: true,
		},
		"api only and unset is a plain commit": {
			settings: signing.Settings{Signer: signing.MethodAPI}, cfg: signedKey, apis: gh(true), want: nameGit,
		},
		"git only and unset without a key is a plain commit": {
			settings: signing.Settings{Signer: signing.MethodGit},
			want:     nameGit,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := signing.Select(t.Context(), tc.settings, signing.Env{Git: &fakeGit{cfg: tc.cfg}, API: tc.apis})
			if tc.want == "" {
				var d diag.Diagnostic
				if !errors.As(err, &d) || d.Code() != signing.CodeNotConfigured || d.Help() == "" {
					t.Fatalf("error = %v, want the setup guide", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Name() != tc.want || got.Signed() != tc.signed {
				t.Fatalf("signer = %q (signed %v), want %q (signed %v)", got.Name(), got.Signed(), tc.want, tc.signed)
			}
		})
	}
}

func TestSelectNeedsACommitter(t *testing.T) {
	t.Parallel()
	_, err := signing.Select(t.Context(), signing.Settings{}, signing.Env{Git: struct{ git.Git }{}})
	if err == nil || !strings.Contains(err.Error(), "Git repository") {
		t.Fatalf("error = %v", err)
	}
}

func TestSelectDoesNotReadGitConfigForTheAPI(t *testing.T) {
	t.Parallel()
	yes := true
	g := &fakeGit{cfgErr: errors.New("must not be read")}
	if _, err := signing.Select(t.Context(), signing.Settings{Sign: &yes, Signer: signing.MethodAPI}, signing.Env{
		Git: g, API: []signing.APICommitter{&fakeAPI{name: nameGitHub, configured: true}},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSelectNoSignReachesGit(t *testing.T) {
	t.Parallel()
	no := false
	g := &fakeGit{sha: headSHA}
	s, err := signing.Select(t.Context(), signing.Settings{Sign: &no}, signing.Env{Git: g})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit(t.Context(), signing.Request{Message: "m", Changes: []git.Change{{Path: "a"}}}); err != nil {
		t.Fatal(err)
	}
	if want := []git.CommitOptions{{NoSign: true, Paths: []string{"a"}}}; !reflect.DeepEqual(g.commits, want) {
		t.Fatalf("commits %+v, want %+v", g.commits, want)
	}
}

func TestPlainCommit(t *testing.T) {
	t.Parallel()
	g := &fakeGit{sha: headSHA}
	s, err := signing.Select(t.Context(), signing.Settings{}, signing.Env{Git: g})
	if err != nil {
		t.Fatal(err)
	}
	sha, err := s.Commit(
		t.Context(),
		signing.Request{Message: "msg", Changes: []git.Change{{Path: "b"}, {Path: "a", Deleted: true}}},
	)
	if err != nil || sha != headSHA {
		t.Fatalf("sha %q, error %v", sha, err)
	}
	if want := []git.CommitOptions{{Paths: []string{"b", "a"}}}; !reflect.DeepEqual(g.commits, want) || g.messages[0] != "msg" {
		t.Fatalf("commits %+v, want %+v", g.commits, want)
	}
}

func TestGuide(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		method signing.Method
		want   []string
		not    []string
	}{
		"auto shows both ways": {
			method: signing.MethodAuto,
			want:   []string{keygen, "commit.gpgsign", guideApp, "REMERIS_COMMIT_GITHUB_APP_ID", "sign = false"},
		},
		"git shows the key setup": {
			method: signing.MethodGit, want: []string{keygen, "user.signingkey"}, not: []string{guideApp},
		},
		"api shows the App setup": {
			method: signing.MethodAPI, want: []string{guideApp, "[github]"}, not: []string{keygen},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var d diag.Diagnostic
			if !errors.As(signing.NotConfigured(tc.method, nil), &d) {
				t.Fatal("not a diagnostic")
			}
			for _, w := range tc.want {
				if !strings.Contains(d.Help(), w) {
					t.Errorf("the guide has no %q:\n%s", w, d.Help())
				}
			}
			for _, n := range tc.not {
				if strings.Contains(d.Help(), n) {
					t.Errorf("the guide has %q:\n%s", n, d.Help())
				}
			}
		})
	}
}

func TestParseMethod(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in      string
		want    signing.Method
		wantErr bool
	}{
		"unset": {in: "", want: ""},
		"auto":  {in: "auto", want: signing.MethodAuto},
		"git":   {in: " GIT ", want: signing.MethodGit},
		"api":   {in: "api", want: signing.MethodAPI},
		"bad":   {in: "gpg", wantErr: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := signing.ParseMethod(tc.in)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("ParseMethod(%q) = %q, %v", tc.in, got, err)
			}
		})
	}
}

func TestOptionsOver(t *testing.T) {
	t.Parallel()
	yes, no := true, false
	tests := map[string]struct {
		high, low signing.Options
		want      signing.Settings
	}{
		"defaults": {want: signing.Settings{Signer: signing.MethodAuto}},
		"low is used when high is unset": {
			low:  signing.Options{Sign: &yes, Signer: signing.MethodGit, Include: []string{"a"}, Exclude: []string{"b"}},
			want: signing.Settings{Sign: &yes, Signer: signing.MethodGit, Include: []string{"a"}, Exclude: []string{"b"}},
		},
		"high wins, even with false": {
			high: signing.Options{Sign: &no, Signer: signing.MethodAPI, Include: []string{"x"}},
			low:  signing.Options{Sign: &yes, Signer: signing.MethodGit, Include: []string{"a"}, Exclude: []string{"b"}},
			want: signing.Settings{Sign: &no, Signer: signing.MethodAPI, Include: []string{"x"}, Exclude: []string{"b"}},
		},
		"an empty list replaces the lower one": {
			high: signing.Options{Exclude: []string{}},
			low:  signing.Options{Exclude: []string{"b"}},
			want: signing.Settings{Signer: signing.MethodAuto, Exclude: []string{}},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tc.high.Over(tc.low).Resolve(); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("resolved %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestFilter(t *testing.T) {
	t.Parallel()
	changes := []git.Change{
		{Path: fileChange}, {Path: fileVer}, {Path: "mise.lock"}, {Path: "mise.claude.lock"},
		{Path: fileDocA}, {Path: fileDocB}, {Path: fileCS, Deleted: true},
	}
	paths := func(cs []git.Change) []string {
		out := make([]string, len(cs))
		for i, c := range cs {
			out[i] = c.Path
		}
		return out
	}
	tests := map[string]struct {
		include, exclude []string
		want             []string
		wantErr          bool
	}{
		"everything": {
			want: []string{
				fileCS,
				fileChange,
				fileVer,
				fileDocA,
				fileDocB,
				"mise.claude.lock",
				"mise.lock",
			},
		},
		"include narrows": {
			include: []string{fileChange, fileVer}, want: []string{fileChange, fileVer},
		},
		"exclude removes": {
			exclude: []string{"mise*.lock", "docs/**"},
			want:    []string{fileCS, fileChange, "VERSION"},
		},
		"exclude wins over include": {
			include: []string{"docs/**"}, exclude: []string{"docs/sub"}, want: []string{fileDocA},
		},
		"a directory selects what is under it": {include: []string{"docs"}, want: []string{fileDocA, fileDocB}},
		"a pattern that matches nothing":       {include: []string{"nothing"}, want: nil},
		"deleted files can be selected":        {include: []string{".changeset/*.md"}, want: []string{fileCS}},
		"a bad pattern":                        {include: []string{"a["}, wantErr: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := signing.Filter(changes, tc.include, tc.exclude)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if p := paths(got); !reflect.DeepEqual(p, tc.want) && (len(p) != 0 || len(tc.want) != 0) {
				t.Fatalf("Filter = %v, want %v", p, tc.want)
			}
		})
	}
}

func TestAPISignerCommit(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (*fakeGit, *fakeAPI, signing.Signer, string) {
		t.Helper()
		root := t.TempDir()
		yes := true
		g := &fakeGit{root: root}
		api := &fakeAPI{name: nameGitHub, configured: true}
		s, err := signing.Select(t.Context(), signing.Settings{Sign: &yes, Signer: signing.MethodAPI},
			signing.Env{Git: g, API: []signing.APICommitter{api}})
		if err != nil {
			t.Fatal(err)
		}
		return g, api, s, root
	}

	t.Run("sends the files, stages them and adopts the commit", func(t *testing.T) {
		t.Parallel()
		g, api, s, root := setup(t)
		if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "docs", "a.md"), []byte("hello\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		sha, err := s.Commit(t.Context(), signing.Request{
			Message: "chore: release",
			Changes: []git.Change{{Path: fileDocA}, {Path: fileOld, Deleted: true}},
		})
		if err != nil {
			t.Fatal(err)
		}
		want := signing.APICommit{
			Branch: branchMain, ExpectedHead: headSHA, Message: "chore: release",
			Additions: []signing.File{{Path: fileDocA, Content: []byte("hello\n")}}, Deletions: []string{fileOld},
		}
		if sha != newSHA || len(api.got) != 1 || !reflect.DeepEqual(api.got[0], want) {
			t.Fatalf("sha %s, API calls %+v, want %+v", sha, api.got, want)
		}
		if !reflect.DeepEqual(g.added, [][]string{{fileDocA, fileOld}}) ||
			!reflect.DeepEqual(g.adopted, []string{branchMain + "@" + newSHA}) {
			t.Fatalf("staged %v, adopted %v", g.added, g.adopted)
		}
		if len(g.commits) != 0 {
			t.Fatal("git committed as well")
		}
	})

	t.Run("a symbolic link is refused before anything is sent", func(t *testing.T) {
		t.Parallel()
		_, api, s, root := setup(t)
		if err := os.Symlink("target", filepath.Join(root, "link")); err != nil {
			t.Skip("symbolic links are not available:", err)
		}
		_, err := s.Commit(t.Context(), signing.Request{Message: "m", Changes: []git.Change{{Path: "link"}}})
		if err == nil || !strings.Contains(err.Error(), "not a regular file") || len(api.got) != 0 {
			t.Fatalf("error = %v, API calls %d", err, len(api.got))
		}
	})

	t.Run("a failing API does not touch the repository", func(t *testing.T) {
		t.Parallel()
		g, api, s, root := setup(t)
		api.err = errors.New("refused")
		if err := os.WriteFile(filepath.Join(root, "f"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Commit(t.Context(), signing.Request{Message: "m", Changes: []git.Change{{Path: "f"}}}); err == nil {
			t.Fatal("no error")
		}
		if len(g.added) != 0 || len(g.adopted) != 0 {
			t.Fatalf("staged %v, adopted %v", g.added, g.adopted)
		}
	})

	t.Run("a commit that cannot be adopted says how to catch up", func(t *testing.T) {
		t.Parallel()
		g, _, s, root := setup(t)
		g.adoptErr = errors.New("no network")
		if err := os.WriteFile(filepath.Join(root, "f"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := s.Commit(t.Context(), signing.Request{Message: "m", Changes: []git.Change{{Path: "f"}}})
		var d diag.Diagnostic
		if !errors.As(err, &d) || !strings.Contains(d.Help(), "git reset --soft "+newSHA) {
			t.Fatalf("error = %v", err)
		}
	})
}
