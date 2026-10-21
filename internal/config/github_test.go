package config

import (
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
)

func TestGitHubURLs(t *testing.T) {
	cases := map[string]struct {
		toml     string
		wantCode string
		api, up  string
	}{
		"none": {"[github]\nowner = \"o\"\nrepo = \"r\"\n", "", "", ""},
		"api only": {
			"[github]\nowner = \"o\"\nrepo = \"r\"\napi_url = \"https://ghe.example.com/\"\n",
			"",
			"https://ghe.example.com/",
			"",
		},
		"both": {
			"[github]\nowner = \"o\"\nrepo = \"r\"\napi_url = \"http://127.0.0.1:8080\"\nupload_url = \"http://127.0.0.1:8081\"\n",
			"",
			"http://127.0.0.1:8080",
			"http://127.0.0.1:8081",
		},
		"upload only": {
			"[github]\nowner = \"o\"\nrepo = \"r\"\nupload_url = \"https://u.example.com/\"\n",
			codeUploadURLNoAPIURL,
			"",
			"",
		},
		"api no scheme":   {"[github]\napi_url = \"ghe.example.com\"\n", codeGitHubURL, "", ""},
		"api ftp":         {"[github]\napi_url = \"ftp://ghe.example.com\"\n", codeGitHubURL, "", ""},
		"api no host":     {"[github]\napi_url = \"https:///path\"\n", codeGitHubURL, "", ""},
		"api relative":    {"[github]\napi_url = \"/api/v3\"\n", codeGitHubURL, "", ""},
		"upload bad":      {"[github]\napi_url = \"https://a.example.com\"\nupload_url = \"nope\"\n", codeGitHubURL, "", ""},
		"upload bad only": {"[github]\nupload_url = \"nope\"\n", codeGitHubURL, "", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := load(t, tc.toml)
			if tc.wantCode != "" {
				var d diag.Diagnostic
				if !asDiag(err, &d) || d.Code() != tc.wantCode {
					t.Fatalf("error = %v, want code %s", err, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.GitHub.APIURL != tc.api || cfg.GitHub.UploadURL != tc.up {
				t.Errorf("got %q %q, want %q %q", cfg.GitHub.APIURL, cfg.GitHub.UploadURL, tc.api, tc.up)
			}
		})
	}
}

// A rejected URL is printed without its password, so a credential does not end up in CI logs.
func TestGitHubURLErrorHidesPassword(t *testing.T) {
	_, err := load(t, "[github]\napi_url = \"ftp://user:not-a-real-secret@ghe.example.com/\"\n")
	if err == nil || strings.Contains(err.Error(), "not-a-real-secret") || !strings.Contains(err.Error(), "ghe.example.com") {
		t.Fatalf("error = %v", err)
	}
}
