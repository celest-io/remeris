package project

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/config"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	versionTxt  = "VERSION.txt"
	versionBody = "v1.2.3"
	goModPath   = "go.mod"
	tagV1       = "v1.0.0"
)

type fakeGit struct {
	tags  []string
	err   error
	calls *int
}

func (f fakeGit) AllTagsOnBranch(context.Context) ([]string, error) {
	*f.calls++
	return f.tags, f.err
}

// CommitMessagesAfterTag is not used by package loading.
func (fakeGit) CommitMessagesAfterTag(context.Context, *string) ([]git.Commit, error) {
	return nil, nil
}

// FirstCommitsForFiles is not used by package loading.
func (fakeGit) FirstCommitsForFiles(context.Context, []string) map[string]*git.CommitInfo { return nil }

func regexCfg(t *testing.T, path string) versionedfile.Config {
	t.Helper()
	cfg, err := versionedfile.NewConfig(path, nil, []string{`v(?<version>\d+\.\d+\.\d+)`})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func pkgCfg(name versioning.PackageName, files ...versionedfile.Config) config.PackageConfig {
	return config.PackageConfig{Name: name, VersionedFiles: files}
}

func TestState(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		files       map[string]string
		pkgs        func(t *testing.T) []config.PackageConfig
		openErr     error
		tagsErr     error
		tags        []string
		wantErr     string
		wantCode    string
		wantPkgs    int
		wantFiles   int
		wantTags    []string
		wantGitCall int
		wantGit     bool
	}{
		"no packages makes no git call": {
			pkgs: func(*testing.T) []config.PackageConfig { return nil },
			tags: []string{tagV1},
		},
		"one package": {
			files: map[string]string{versionTxt: versionBody},
			pkgs: func(t *testing.T) []config.PackageConfig {
				return []config.PackageConfig{pkgCfg(versioning.DefaultName(), regexCfg(t, versionTxt))}
			},
			tags:        []string{tagV1},
			wantPkgs:    1,
			wantFiles:   1,
			wantTags:    []string{tagV1},
			wantGitCall: 1,
			wantGit:     true,
		},
		"two packages sharing a file parse it twice": {
			files: map[string]string{versionTxt: versionBody},
			pkgs: func(t *testing.T) []config.PackageConfig {
				return []config.PackageConfig{
					pkgCfg(versioning.CustomName("a"), regexCfg(t, versionTxt)),
					pkgCfg(versioning.CustomName("b"), regexCfg(t, versionTxt)),
				}
			},
			wantPkgs:    2,
			wantFiles:   2,
			wantGitCall: 1,
			wantGit:     true,
		},
		"missing file": {
			pkgs: func(t *testing.T) []config.PackageConfig {
				return []config.PackageConfig{pkgCfg(versioning.DefaultName(), regexCfg(t, versionTxt))}
			},
			wantErr:     "Error reading from VERSION.txt: file does not exist",
			wantCode:    codeRead,
			wantGitCall: 1,
		},
		"unparsable file": {
			files: map[string]string{versionTxt: "no version here"},
			pkgs: func(t *testing.T) []config.PackageConfig {
				return []config.PackageConfig{pkgCfg(versioning.DefaultName(), regexCfg(t, versionTxt))}
			},
			wantErr:     "VERSION.txt",
			wantGitCall: 1,
		},
		"inconsistent versions": {
			files: map[string]string{versionTxt: versionBody, "OTHER.txt": "v2.0.0"},
			pkgs: func(t *testing.T) []config.PackageConfig {
				return []config.PackageConfig{
					pkgCfg(versioning.DefaultName(), regexCfg(t, versionTxt), regexCfg(t, "OTHER.txt")),
				}
			},
			wantErr:     "Found inconsistent versions in package",
			wantGitCall: 1,
		},
		"tag read failure gives empty tags": {
			files: map[string]string{versionTxt: versionBody},
			pkgs: func(t *testing.T) []config.PackageConfig {
				return []config.PackageConfig{pkgCfg(versioning.DefaultName(), regexCfg(t, versionTxt))}
			},
			tags:        []string{"v9.9.9"},
			tagsErr:     errors.New("boom"),
			wantPkgs:    1,
			wantFiles:   1,
			wantGitCall: 1,
			wantGit:     true,
		},
		"open failure gives empty tags and no git": {
			files: map[string]string{versionTxt: versionBody},
			pkgs: func(t *testing.T) []config.PackageConfig {
				return []config.PackageConfig{pkgCfg(versioning.DefaultName(), regexCfg(t, versionTxt))}
			},
			openErr:   errors.New("no git"),
			wantPkgs:  1,
			wantFiles: 1,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var calls int
			l := Loader{
				ReadFile: func(path string) (string, error) {
					if c, ok := tt.files[path]; ok {
						return c, nil
					}
					return "", &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
				},
				OpenGit: func(context.Context) (git.Git, error) {
					if tt.openErr != nil {
						return nil, tt.openErr
					}
					return fakeGit{tags: tt.tags, err: tt.tagsErr, calls: &calls}, nil
				},
			}

			state, err := l.State(context.Background(), tt.pkgs(t), true)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				var d diag.Diagnostic
				if tt.wantCode != "" && (!errors.As(err, &d) || d.Code() != tt.wantCode) {
					t.Errorf("code = %v, want %s", err, tt.wantCode)
				}
			} else if err != nil {
				t.Fatalf("err = %v", err)
			}
			if calls != tt.wantGitCall {
				t.Errorf("tag reads = %d, want %d", calls, tt.wantGitCall)
			}
			if tt.wantErr != "" {
				return
			}
			if len(state.Packages) != tt.wantPkgs || len(state.AllVersionedFiles) != tt.wantFiles {
				t.Errorf("packages = %d, files = %d; want %d, %d",
					len(state.Packages), len(state.AllVersionedFiles), tt.wantPkgs, tt.wantFiles)
			}
			if !slices.Equal(state.AllGitTags, tt.wantTags) {
				t.Errorf("tags = %v, want %v", state.AllGitTags, tt.wantTags)
			}
			if (state.Git != nil) != tt.wantGit {
				t.Errorf("Git set = %t, want %t", state.Git != nil, tt.wantGit)
			}
			if !state.IgnoreConventionalCommits {
				t.Error("IgnoreConventionalCommits was not carried over")
			}
		})
	}
}

func TestStateInvalidUTF8(t *testing.T) {
	t.Parallel()
	l := Loader{
		ReadFile: func(string) (string, error) { return "v1.0.0\xff", nil },
		OpenGit:  func(context.Context) (git.Git, error) { return nil, errors.New("no git") },
	}
	_, err := l.State(context.Background(), []config.PackageConfig{
		pkgCfg(versioning.DefaultName(), regexCfg(t, versionTxt)),
	}, false)

	var d diag.Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("err = %v, want a diagnostic", err)
	}
	if want := "Error reading from VERSION.txt: stream did not contain valid UTF-8"; d.Error() != want {
		t.Errorf("message = %q, want %q", d.Error(), want)
	}
	if d.Code() != codeRead || d.Help() == "" {
		t.Errorf("code = %q, help = %q", d.Code(), d.Help())
	}
}

func TestStateUsesTagsAndGoVersioning(t *testing.T) {
	t.Parallel()
	cfg, err := versionedfile.NewConfig(goModPath, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := pkgCfg(versioning.DefaultName(), cfg)
	p.IgnoreGoMajorVersioning = true
	var calls int
	l := Loader{
		ReadFile: func(string) (string, error) { return "module example.com/m/v2\n\ngo 1.22\n", nil },
		OpenGit: func(context.Context) (git.Git, error) {
			return fakeGit{tags: []string{"v2.1.0", tagV1}, calls: &calls}, nil
		},
	}

	state, err := l.State(context.Background(), []config.PackageConfig{p}, false)

	if err != nil {
		t.Fatal(err)
	}
	if got := state.Packages[0].GoVersioning; got != versionedfile.IgnoreMajorRules {
		t.Errorf("GoVersioning = %v, want IgnoreMajorRules", got)
	}
	latest, ok := state.Packages[0].LatestVersion()
	if !ok || latest.String() != "2.1.0" {
		t.Errorf("latest = %v (%t), want 2.1.0 from the go.mod tags", latest, ok)
	}
}
