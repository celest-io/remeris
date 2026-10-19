package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
)

const (
	fakeKey = "fake-key-for-tests"
	keyPath = "/k"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestGitHubAppPrivateKey(t *testing.T) {
	read := func(p string) ([]byte, error) {
		if p == "/missing" {
			return nil, errors.New("no such file")
		}
		return []byte("from-file:" + p), nil
	}
	cases := map[string]struct {
		opts      GlobalOptions
		env       map[string]string
		want      string
		found     bool
		wantError bool
	}{
		"none":      {},
		"env only":  {env: map[string]string{EnvGitHubAppKey: fakeKey}, want: fakeKey, found: true},
		"path only": {opts: GlobalOptions{GitHubAppKeyPath: keyPath}, want: "from-file:" + keyPath, found: true},
		"path wins": {
			opts:  GlobalOptions{GitHubAppKeyPath: keyPath},
			env:   map[string]string{EnvGitHubAppKey: fakeKey},
			want:  "from-file:" + keyPath,
			found: true,
		},
		"unreadable path": {
			opts:      GlobalOptions{GitHubAppKeyPath: "/missing"},
			env:       map[string]string{EnvGitHubAppKey: fakeKey},
			wantError: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			key, found, err := tc.opts.GitHubAppPrivateKey(envOf(tc.env), read)
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "/missing") || strings.Contains(err.Error(), fakeKey) {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil || found != tc.found || string(key) != tc.want {
				t.Errorf("got %q, %v, %v; want %q, %v", key, found, err, tc.want, tc.found)
			}
		})
	}
}

func TestValidateGitHubApp(t *testing.T) {
	keyEnv := map[string]string{EnvGitHubAppKey: fakeKey}
	cases := map[string]struct {
		opts GlobalOptions
		env  map[string]string
		want string
	}{
		"all unset":                 {},
		"id and path":               {opts: GlobalOptions{GitHubAppID: 1, GitHubAppKeyPath: keyPath}},
		"id and env key":            {opts: GlobalOptions{GitHubAppID: 1}, env: keyEnv},
		"id, installation and path": {opts: GlobalOptions{GitHubAppID: 1, GitHubInstallationID: 2, GitHubAppKeyPath: keyPath}},
		"id without key":            {opts: GlobalOptions{GitHubAppID: 1}, want: "without a private key"},
		"path without id":           {opts: GlobalOptions{GitHubAppKeyPath: keyPath}, want: "without an App ID"},
		"env key without id":        {env: keyEnv, want: "without an App ID"},
		"installation without id": {
			opts: GlobalOptions{GitHubInstallationID: 2},
			want: "installation ID is set without an App ID",
		},
		"installation, id, no key": {
			opts: GlobalOptions{GitHubAppID: 1, GitHubInstallationID: 2},
			want: "without a private key",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.opts.ValidateGitHubApp(envOf(tc.env))
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var d diag.Diagnostic
			if !errors.As(err, &d) || !strings.Contains(err.Error(), tc.want) || d.Code() != codeGitHubApp || d.Help() == "" {
				t.Fatalf("error = %v", err)
			}
			if strings.Contains(err.Error()+d.Help(), fakeKey) {
				t.Error("error leaks the key")
			}
		})
	}
}

func TestGitHubAppFlagsLoad(t *testing.T) {
	t.Setenv(EnvGitHubAppID, "7")
	global, _ := run(t, workflowName, "--github-installation-id", "9", "--github-app-private-key-path", keyPath)
	if global.GitHubAppID != 7 || global.GitHubInstallationID != 9 || global.GitHubAppKeyPath != keyPath {
		t.Errorf("got %+v", global)
	}
}
