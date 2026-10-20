package config

import "strings"

// GitHubFromRemote ports the GitHub detection of Knope's config::generate (spec F20.6) on the URL of the first
// remote. The URL must contain "github.com"; it is split on "/", the owner is the second-to-last part without a
// "git@github.com:" prefix and the repo is the last part without ".git". The quirks stay: "https://github.com/r"
// gives the owner "github.com", and a trailing slash gives an empty repo.
//
// A URL with a single part, such as "git@github.com:r.git", has no second-to-last part. Knope computes
// parts.len() - 2 on a usize there: it panics in a debug build and wraps to "no owner" in a release build. The
// release behaviour is kept, so there is no detection and no panic.
//
// Gitea detection (the known public host codeberg.org) is milestone M8: such a remote gives no forge for now.
func GitHubFromRemote(url string) (*GitHub, bool) {
	if !strings.Contains(url, "github.com") {
		return nil, false
	}
	parts := strings.Split(url, "/")
	if len(parts) < 2 {
		return nil, false
	}
	owner := strings.TrimPrefix(parts[len(parts)-2], "git@github.com:")
	repo := strings.TrimSuffix(parts[len(parts)-1], ".git")
	return &GitHub{Owner: owner, Repo: repo}, true
}

// RemoteFunc returns the URL of the first Git remote, false when there is none or the repository cannot be read.
// It is a function so that config does not depend on the Git access and tests can use a fake.
type RemoteFunc func() (string, bool)
