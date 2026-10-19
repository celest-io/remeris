package credentials_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/prompt"
)

const (
	fakeEnv    = "fake-env-value"
	fakeFile   = "fake-file-value"
	fakeLegacy = "fake-knope-value"
	fakeAnswer = "fake-typed-value"
	failHelp   = "This step requires user input, but no user input was provided. Try running the step again."

	dirRemeris = "remeris"
	dirKnope   = "knope"
	fileGitHub = "github_token"
	fileEmail  = "email"
	envGitHub  = "GITHUB_TOKEN"
)

// failingPrompter fails the test when asked anything.
type failingPrompter struct {
	prompt.Prompter
	t *testing.T
}

func (f failingPrompter) Password(context.Context, string) (string, error) {
	f.t.Helper()
	f.t.Error("the user must not be asked")
	return "", errors.New("asked")
}

func put(t *testing.T, base, dir, name, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(base, dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, dir, name), []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, base, dir, name string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(base, dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b), true
}

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLookupChain(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		env        map[string]string
		remeris    string
		knope      string
		want       string
		found      bool
		wantCopied bool
	}{
		"nothing":  {},
		"env only": {env: map[string]string{envGitHub: fakeEnv}, want: fakeEnv, found: true},
		"env beats files": {
			env:     map[string]string{envGitHub: fakeEnv},
			remeris: fakeFile,
			knope:   fakeLegacy,
			want:    fakeEnv,
			found:   true,
		},
		"remeris file":        {remeris: fakeFile, want: fakeFile, found: true},
		"remeris beats knope": {remeris: fakeFile, knope: fakeLegacy, want: fakeFile, found: true},
		"migration":           {knope: fakeLegacy, want: fakeLegacy, found: true, wantCopied: true},
		"empty env ignored":   {env: map[string]string{envGitHub: ""}, remeris: fakeFile, want: fakeFile, found: true},
		"raw value kept":      {remeris: "  " + fakeFile + "\n", want: "  " + fakeFile + "\n", found: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			base := t.TempDir()
			if tc.remeris != "" {
				put(t, base, dirRemeris, fileGitHub, tc.remeris)
			}
			if tc.knope != "" {
				put(t, base, dirKnope, fileGitHub, tc.knope)
			}
			s := &credentials.Store{Dir: base, Getenv: env(tc.env), Prompter: failingPrompter{t: t}}
			got, found, err := s.Lookup(t.Context(), credentials.GitHubToken)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want || found != tc.found {
				t.Errorf("Lookup = %q, %v; want %q, %v", got, found, tc.want, tc.found)
			}
			if tc.wantCopied {
				if v, ok := read(t, base, dirRemeris, fileGitHub); !ok || v != fakeLegacy {
					t.Errorf("remeris file = %q, %v; want the copied value", v, ok)
				}
				if v, ok := read(t, base, dirKnope, fileGitHub); !ok || v != fakeLegacy {
					t.Errorf("knope file = %q, %v; want it left alone", v, ok)
				}
			} else if tc.remeris == "" {
				if _, ok := read(t, base, dirRemeris, fileGitHub); ok {
					t.Error("Lookup must not create the remeris file")
				}
			}
		})
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		key  credentials.Key
		vars map[string]string
		want string
	}{
		"gitea": {
			key:  credentials.GiteaToken("https://gitea.example"),
			vars: map[string]string{"GITEA_TOKEN": fakeEnv},
			want: fakeEnv,
		},
		"jira has no override": {key: credentials.JiraToken, vars: map[string]string{"JIRA_TOKEN": fakeEnv}},
		"email has none":       {key: credentials.Email, vars: map[string]string{"EMAIL": fakeEnv}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := &credentials.Store{Dir: t.TempDir(), Getenv: env(tc.vars)}
			got, _, err := s.Lookup(t.Context(), tc.key)
			if err != nil || got != tc.want {
				t.Errorf("Lookup = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestGetPromptsAndSaves(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		key     credentials.Key
		file    string
		message string
	}{
		"github": {
			key:  credentials.GitHubToken,
			file: fileGitHub,
			message: "No GitHub token found, generate one from https://github.com/settings/tokens " +
				"with `repo` permissions and input here",
		},
		"gitea includes the host": {
			key:  credentials.GiteaToken("https://gitea.example"),
			file: "gitea_token",
			message: "No Gitea token found, generate one from https://gitea.example/user/settings/applications with\n" +
				"`repository` permissions set to `Read and Write` and `issue` permissions set to `Read` and input here",
		},
		"jira": {
			key:  credentials.JiraToken,
			file: "jira_token",
			message: "No Jira token found, generate one from https://id.atlassian.com/manage-profile/security/api-tokens " +
				"and input here",
		},
		"email": {key: credentials.Email, file: fileEmail, message: "Input your email address"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			base := t.TempDir()
			p := prompt.NewScripted(prompt.Answer{Message: tc.message, Text: fakeAnswer})
			s := &credentials.Store{Dir: base, Getenv: env(nil), Prompter: p}
			got, err := s.Get(t.Context(), tc.key)
			if err != nil || got != fakeAnswer {
				t.Fatalf("Get = %q, %v", got, err)
			}
			asked := p.Asked()
			if len(asked) != 1 || asked[0].Kind != prompt.KindPassword || asked[0].Message != tc.message {
				t.Errorf("asked = %+v", asked)
			}
			if v, ok := read(t, base, dirRemeris, tc.file); !ok || v != fakeAnswer {
				t.Errorf("saved = %q, %v", v, ok)
			}
			// The second call finds the saved value and does not ask again.
			s.Prompter = failingPrompter{t: t}
			if got, err := s.Get(t.Context(), tc.key); err != nil || got != fakeAnswer {
				t.Errorf("second Get = %q, %v", got, err)
			}
		})
	}
}

func TestGetSkipsPromptWhenFound(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	put(t, base, dirKnope, fileEmail, fakeLegacy)
	s := &credentials.Store{Dir: base, Getenv: env(nil), Prompter: failingPrompter{t: t}}
	got, err := s.Get(t.Context(), credentials.Email)
	if err != nil || got != fakeLegacy {
		t.Errorf("Get = %q, %v", got, err)
	}
}

func TestGetPromptFailure(t *testing.T) {
	t.Parallel()
	cause := errors.New("not a terminal")
	tests := map[string]struct {
		prompter prompt.Prompter
	}{
		"plain error is wrapped": {prompter: prompt.NewScripted(prompt.Answer{Err: cause})},
		"diagnostic is kept":     {prompter: prompt.NewScripted(prompt.Answer{Err: prompt.Error(cause)})},
		"no answer left":         {prompter: prompt.NewScripted()},
		"no prompter":            {},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			base := t.TempDir()
			s := &credentials.Store{Dir: base, Getenv: env(nil), Prompter: tc.prompter}
			_, err := s.Get(t.Context(), credentials.JiraToken)
			var d diag.Diagnostic
			if !errors.As(err, &d) {
				t.Fatalf("err = %v, want a diagnostic", err)
			}
			if d.Error() != "Failed to get user input" || d.Help() != failHelp {
				t.Errorf("got %q / %q", d.Error(), d.Help())
			}
			if _, ok := read(t, base, dirRemeris, "jira_token"); ok {
				t.Error("nothing must be saved on failure")
			}
		})
	}
}

func TestPermissions(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	tests := map[string]func(t *testing.T, s *credentials.Store){
		"after prompt": func(t *testing.T, s *credentials.Store) {
			t.Helper()
			s.Prompter = prompt.NewScripted(prompt.Answer{Text: fakeAnswer})
			if _, err := s.Get(t.Context(), credentials.Email); err != nil {
				t.Fatal(err)
			}
		},
		"after migration": func(t *testing.T, s *credentials.Store) {
			t.Helper()
			put(t, s.Dir, dirKnope, fileEmail, fakeLegacy)
			if _, _, err := s.Lookup(t.Context(), credentials.Email); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := &credentials.Store{Dir: t.TempDir(), Getenv: env(nil)}
			run(t, s)
			dir, err := os.Stat(filepath.Join(s.Dir, dirRemeris))
			if err != nil {
				t.Fatal(err)
			}
			file, err := os.Stat(filepath.Join(s.Dir, dirRemeris, fileEmail))
			if err != nil {
				t.Fatal(err)
			}
			if dir.Mode().Perm() != 0o700 || file.Mode().Perm() != 0o600 {
				t.Errorf("dir %v, file %v; want 0700, 0600", dir.Mode().Perm(), file.Mode().Perm())
			}
		})
	}
}

func TestFileErrors(t *testing.T) {
	t.Parallel()
	t.Run("directory cannot be created", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		// A regular file where the remeris directory should be.
		if err := os.WriteFile(filepath.Join(base, dirRemeris), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		s := &credentials.Store{Dir: base, Getenv: env(nil), Prompter: prompt.NewScripted(prompt.Answer{Text: fakeAnswer})}
		_, err := s.Get(t.Context(), credentials.Email)
		var d diag.Diagnostic
		if !errors.As(err, &d) {
			t.Fatalf("err = %v, want a diagnostic", err)
		}
	})
	t.Run("value path is a directory", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		if err := os.MkdirAll(filepath.Join(base, dirRemeris, fileEmail), 0o700); err != nil {
			t.Fatal(err)
		}
		s := &credentials.Store{Dir: base, Getenv: env(nil)}
		_, _, err := s.Lookup(t.Context(), credentials.Email)
		var d diag.Diagnostic
		if !errors.As(err, &d) || d.Error() != "Could not open configuration path" {
			t.Errorf("err = %v", err)
		}
	})
}
