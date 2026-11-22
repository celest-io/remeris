package signing

import (
	"context"
	"os"
	"path/filepath"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/git"
)

const codeAPI = "remeris::commit::api"

// apiSigner makes the commit through a forge API, which signs it. The commit is made on the remote branch, so the
// local branch must be at the commit the remote branch points to; the forge refuses the commit otherwise. Afterwards
// the commit is fetched and the local branch moves to it, so that the local repository is where the remote one is.
type apiSigner struct {
	git       git.Git
	committer git.Committer
	api       APICommitter
}

func (s *apiSigner) Name() string { return "the " + s.api.Name() + " API" }

func (s *apiSigner) Signed() bool { return true }

func (s *apiSigner) Commit(ctx context.Context, req Request) (string, error) {
	branch, err := s.git.CurrentBranch(ctx)
	if err != nil {
		return "", err
	}
	head, err := s.git.HeadCommitSHA(ctx)
	if err != nil {
		return "", err
	}
	root, err := s.committer.Root(ctx)
	if err != nil {
		return "", err
	}

	in := APICommit{Branch: branch, ExpectedHead: head, Message: req.Message}
	paths := make([]string, 0, len(req.Changes))
	for _, ch := range req.Changes {
		paths = append(paths, ch.Path)
		if ch.Deleted {
			in.Deletions = append(in.Deletions, ch.Path)
			continue
		}
		file, err := readFile(root, ch.Path)
		if err != nil {
			return "", err
		}
		in.Additions = append(in.Additions, file)
	}

	sha, err := s.api.CommitFiles(ctx, in)
	if err != nil {
		return "", err
	}
	// The files are staged as they were committed, so that moving the branch leaves nothing of them to commit.
	if err := s.git.AddFiles(ctx, paths); err != nil {
		return "", adoptError(sha, err)
	}
	if err := s.committer.AdoptCommit(ctx, branch, sha); err != nil {
		return "", adoptError(sha, err)
	}
	return sha, nil
}

// adoptError says that the commit exists on the forge but the local repository is not at it.
func adoptError(sha string, cause error) error {
	return diag.Newf("The commit %s was made, but the local branch could not be updated: %w", sha, cause).
		WithCode(codeAPI).
		WithHelp("Run \"git fetch\" and then \"git reset --soft " + sha + "\" on the branch to catch up.")
}

// readFile reads a file of the working tree for the API. A symbolic link cannot be sent as one, so it is refused.
func readFile(root, path string) (File, error) {
	full := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(full)
	if err != nil {
		return File{}, diag.Newf("Could not read %s: %w", path, err).WithCode(codeAPI)
	}
	if !info.Mode().IsRegular() {
		return File{}, diag.Newf("%s is not a regular file", path).
			WithCode(codeAPI).
			WithHelp("The forge API commits regular files only. Exclude it, or sign with your own key (signer = \"git\").")
	}
	content, err := os.ReadFile(full) //nolint:gosec // the path is a change of the working tree, under root
	if err != nil {
		return File{}, diag.Newf("Could not read %s: %w", path, err).WithCode(codeAPI)
	}
	return File{Path: path, Content: content}, nil
}
