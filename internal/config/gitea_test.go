package config

import "testing"

const (
	codebergHost = "https://codeberg.org"
	knopeOwner   = "knope-dev"
	knopeRepo    = "knope"
	hostH        = "https://h"
)

func TestGiteaFromRemote(t *testing.T) {
	tests := map[string]struct {
		url  string
		want *Gitea // nil: no Gitea
	}{
		"https with .git": {
			"https://codeberg.org/knope-dev/knope.git",
			&Gitea{knopeOwner, knopeRepo, codebergHost},
		},
		"https without .git": {"https://codeberg.org/knope-dev/knope", &Gitea{knopeOwner, knopeRepo, codebergHost}},
		"ssh with .git": {
			"git@codeberg.org:/knope-dev/knope.git",
			&Gitea{knopeOwner, knopeRepo, codebergHost},
		},
		"ssh without .git":              {"git@codeberg.org:/knope-dev/knope", &Gitea{knopeOwner, knopeRepo, codebergHost}},
		"ssh without leading slash":     {"git@codeberg.org:o/r.git", &Gitea{"o", "r", codebergHost}},
		"ssh url":                       {"ssh://git@codeberg.org/o/r", &Gitea{"o", "r", codebergHost}},
		"ssh url with other user":       {"ssh://user@codeberg.org/o/r.git", &Gitea{"o", "r", codebergHost}},
		"http is forced to https":       {"http://codeberg.org/o/r.git", &Gitea{"o", "r", codebergHost}},
		"userinfo with token":           {"https://user:tok@codeberg.org/o/r", &Gitea{"o", "r", codebergHost}},
		"userinfo with only a token":    {"https://tok@codeberg.org/o/r.git", &Gitea{"o", "r", codebergHost}},
		"host case":                     {"https://CodeBerg.org/o/r", &Gitea{"o", "r", codebergHost}},
		"extra path is part of repo":    {"https://codeberg.org/o/r/x", &Gitea{"o", "r/x", codebergHost}},
		"trailing slash is kept":        {"https://codeberg.org/o/r/", &Gitea{"o", "r/", codebergHost}},
		"single part":                   {"https://codeberg.org/o", nil},
		"no path":                       {"https://codeberg.org", nil},
		"ssh single part":               {"git@codeberg.org:r.git", nil},
		"no colon":                      {"codeberg.org/o/r", nil},
		"scp style without git user":    {"user@codeberg.org:o/r", nil},
		"local path":                    {"/srv/git/codeberg.org/r.git", nil},
		"empty url":                     {"", nil},
		"github":                        {"https://github.com/o/r.git", nil},
		"unknown host":                  {"https://gitea.example.com/o/r.git", nil},
		"known host only in the path":   {"https://evil.example/codeberg.org/r", nil},
		"known host as a subdomain":     {"https://codeberg.org.evil.example/o/r", nil},
		"known host only in scp path":   {"git@evil.example:codeberg.org/r", nil},
		"known host in userinfo":        {"https://codeberg.org@evil.example/o/r", nil},
		"known host with a port":        {"https://codeberg.org:8443/o/r", nil},
		"known host suffix of the host": {"https://notcodeberg.org/o/r", nil},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := GiteaFromRemote(tt.url)
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("GiteaFromRemote(%q) = %+v, want nil", tt.url, *got)
			case tt.want != nil && (got == nil || *got != *tt.want):
				t.Errorf("GiteaFromRemote(%q) = %+v, want %+v", tt.url, got, *tt.want)
			}
		})
	}
}

func TestParseGiteaRemote(t *testing.T) {
	tests := map[string]struct {
		url  string
		want *Gitea
	}{
		"extra path":       {"https://h/o/r/x", &Gitea{"o", "r/x", hostH}},
		"scp style":        {"git@h:/o/r.git", &Gitea{"o", "r", hostH}},
		"userinfo dropped": {"https://u:p@h/o/r", &Gitea{"o", "r", hostH}},
		"git@ in a scheme": {"x-git@h:o/r", nil},
		"two parts":        {"https://h/o", nil},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := parseGiteaRemote(tt.url)
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Errorf("parseGiteaRemote(%q) = %+v, want %+v", tt.url, got, tt.want)
			}
		})
	}
}
