package cli

import (
	"testing"

	"github.com/celest-io/remeris/internal/app"
)

func TestProjectOptions(t *testing.T) {
	cases := map[string]struct {
		global  app.GlobalOptions
		wantApp bool
	}{
		"no app":   {global: app.GlobalOptions{}},
		"with app": {global: app.GlobalOptions{GitHubAppID: 12, GitHubInstallationID: 34}, wantApp: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			opts := projectOptions(&tc.global, nil)

			if opts.Credentials == nil {
				t.Error("no credential store")
			}
			if (opts.GitHubApp != nil) != tc.wantApp {
				t.Fatalf("GitHubApp = %+v, want app = %v", opts.GitHubApp, tc.wantApp)
			}
			if tc.wantApp && (opts.GitHubApp.ID != 12 || opts.GitHubApp.InstallationID != 34) {
				t.Errorf("GitHubApp = %+v", opts.GitHubApp)
			}
		})
	}
}

func TestProjectOptionsTrustedURLs(t *testing.T) {
	global := app.GlobalOptions{GitHubAPIURL: "https://ghe.example.com/api/v3/", GitHubUploadURL: "https://up.example.com/"}
	opts := projectOptions(&global, nil)
	if opts.TrustedGitHubAPIURL != global.GitHubAPIURL || opts.TrustedGitHubUploadURL != global.GitHubUploadURL {
		t.Errorf("trusted URLs = %q, %q", opts.TrustedGitHubAPIURL, opts.TrustedGitHubUploadURL)
	}
}
