// Package project reads the project on disk into a workflow.State: the packages of the configuration, their
// versioned files and the Git tags (spec F7.1, F4.3).
//
// It sits between config and cli. workflow cannot do this itself: it must not import config, and the reading is
// I/O, which the pure versioning core does not do.
package project

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/celest-io/remeris/internal/config"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/workflow"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const codeRead = "remeris::fs::read"

// Loader reads the project. The zero value reads the working directory; tests replace the two seams.
type Loader struct {
	// ReadFile returns the content of the file at the slash-separated relative path. nil reads from disk.
	ReadFile func(path string) (string, error)
	// OpenGit opens the repository. nil uses git.Open on the working directory.
	OpenGit func(ctx context.Context) (git.Git, error)
}

// Load builds the State for cfg with the default Loader.
func Load(ctx context.Context, cfg *config.Config) (workflow.State, error) {
	ignore := cfg.Changes != nil && cfg.Changes.IgnoreConventionalCommits
	return Loader{}.State(ctx, cfg.PackageConfigs(), ignore)
}

// State loads the packages, their versioned files and the Git tags. Git is only opened, and tags only read,
// when there is at least one package; a failure to do either leaves the tag list empty (spec F4.3).
//
// Every versioned_files entry of every package is read and parsed on its own, so a path listed twice is parsed
// twice (F7.1). Files are read even in a dry run: only writes are forbidden there.
func (l Loader) State(
	ctx context.Context, pkgs []config.PackageConfig, ignoreConventionalCommits bool,
) (workflow.State, error) {
	state := workflow.State{IgnoreConventionalCommits: ignoreConventionalCommits}
	if len(pkgs) == 0 {
		return state, nil
	}
	state.Git, state.AllGitTags = l.readTags(ctx)

	for _, p := range pkgs {
		for _, cfg := range p.VersionedFiles {
			content, err := l.read(cfg.Path)
			if err != nil {
				return workflow.State{}, err
			}
			file, err := versionedfile.New(cfg, content, state.AllGitTags)
			if err != nil {
				return workflow.State{}, err
			}
			state.AllVersionedFiles = append(state.AllVersionedFiles, file)
		}
	}
	for _, p := range pkgs {
		if custom := p.Name.Custom(); custom != nil {
			slog.DebugContext(ctx, "Loading package "+*custom)
		} else {
			slog.DebugContext(ctx, "Loading package")
		}
		gv := versionedfile.Standard
		if p.IgnoreGoMajorVersioning {
			gv = versionedfile.IgnoreMajorRules
		}
		pkg, err := versioning.NewPackage(p.Name, p.VersionedFiles, state.AllVersionedFiles, state.AllGitTags, gv, p.Scopes)
		if err != nil {
			return workflow.State{}, err
		}
		state.Packages = append(state.Packages, pkg)
	}
	return state, nil
}

// readTags opens Git and lists its tags. Errors are logged and swallowed, as Knope does (unwrap_or_default).
func (l Loader) readTags(ctx context.Context) (git.Git, []string) {
	open := l.OpenGit
	if open == nil {
		open = func(ctx context.Context) (git.Git, error) { return git.Open(ctx, ".") }
	}
	g, err := open(ctx)
	if err != nil {
		slog.DebugContext(ctx, "Could not open the Git repository: "+err.Error())
		return nil, nil
	}
	tags, err := g.AllTagsOnBranch(ctx)
	if err != nil {
		slog.DebugContext(ctx, "Could not read the Git tags: "+err.Error())
		return g, nil
	}
	return g, tags
}

func (l Loader) read(path string) (string, error) {
	var (
		content string
		err     error
	)
	if l.ReadFile != nil {
		content, err = l.ReadFile(path)
	} else {
		var b []byte
		b, err = os.ReadFile(filepath.FromSlash(path))
		content = string(b)
	}
	if err != nil {
		cause := err
		var pe *fs.PathError
		if errors.As(err, &pe) {
			cause = pe.Err
		}
		return "", diag.New("Error reading from " + path + ": " + cause.Error()).
			WithCode(codeRead).
			WithHelp("Make sure you have permission to read this file.").
			WithCause(err)
	}
	return content, nil
}
