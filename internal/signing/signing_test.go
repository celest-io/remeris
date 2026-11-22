package signing_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/signing"
)

const (
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
	commits  []git.CommitOptions
	messages []string
	sha      string
}

func (f *fakeGit) Changes(context.Context) ([]git.Change, error) { return nil, nil }

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
			want:   []string{keygen, "commit.gpgsign", guideApp, "sign = false"},
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
