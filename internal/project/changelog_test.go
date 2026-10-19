package project

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/celest-io/remeris/internal/config"
	"github.com/celest-io/remeris/internal/forge/github"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/releasenotes"
)

const changelogPath = "CHANGELOG.md"

func TestStateLoadsChangelogAndTemplates(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		changelog   string
		files       map[string]string
		wantContent string
		wantNil     bool
	}{
		"no changelog configured": {changelog: "", wantNil: true},
		"existing changelog": {
			changelog: changelogPath, files: map[string]string{changelogPath: "# Changelog\n"}, wantContent: "# Changelog\n",
		},
		"missing file is empty": {changelog: changelogPath, wantContent: ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := pkgCfg(versioning.DefaultName(), regexCfg(t, versionTxt))
			cfg.Changelog = tc.changelog
			l := Loader{
				ChangeTemplates: []releasenotes.ChangeTemplate{"- $summary"},
				GitHub:          github.New(github.Config{Owner: "o", Repo: "r"}, nil, nil),
				ReadFile: func(path string) (string, error) {
					if path == versionTxt {
						return versionBody, nil
					}
					if body, ok := tc.files[path]; ok {
						return body, nil
					}
					return "", &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
				},
				OpenGit: func(context.Context) (git.Git, error) { return nil, errors.New("no git") },
			}

			state, err := l.State(context.Background(), []config.PackageConfig{cfg}, false)

			if err != nil {
				t.Fatal(err)
			}
			notes := state.Packages[0].ReleaseNotes
			if (notes.Changelog == nil) != tc.wantNil || notes.Changelog != nil && notes.Changelog.Content != tc.wantContent {
				t.Errorf("changelog = %+v", notes.Changelog)
			}
			if len(notes.ChangeTemplates) != 1 || state.GitHub == nil {
				t.Errorf("templates = %v, github = %v", notes.ChangeTemplates, state.GitHub)
			}
		})
	}
}
