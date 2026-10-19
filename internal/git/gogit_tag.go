package git

import (
	"context"
	"errors"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// CreateTag creates an annotated tag with an empty message on HEAD (spec F9.5). Sign is ignored.
//
// The spec (F9.0) names Repository.CreateTag, but its CreateTagOptions refuses an empty message, so the tag object
// is encoded and written by hand here. The tagger is user.name and user.email from the system, global and repository
// configuration, trimmed like git does. go-git also rejects some names that git accepts (for example "@"), and a
// name that starts with "-" is refused, as git does.
func (g *GoGit) CreateTag(ctx context.Context, name string, _ TagOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	repo, err := g.open()
	if err != nil {
		return err
	}
	head, err := g.head(repo)
	if err != nil {
		return err
	}
	commit, err := repo.CommitObject(head)
	if err != nil {
		return unknownGitError(err)
	}
	cfg, err := repo.ConfigScoped(config.SystemScope)
	if err != nil {
		return unknownGitError(err)
	}
	userName, userEmail := strings.TrimSpace(cfg.User.Name), strings.TrimSpace(cfg.User.Email)
	if userName == "" {
		return noCommitterError(missingConfigError(configUserName))
	}
	if userEmail == "" {
		return noCommitterError(missingConfigError(configUserEmail))
	}

	refName := plumbing.NewTagReferenceName(name)
	if strings.HasPrefix(name, "-") {
		return unknownGitError(invalidTagNameError(name))
	}
	if err := refName.Validate(); err != nil {
		return unknownGitError(err)
	}
	if _, err := repo.Storer.Reference(refName); err == nil {
		return unknownGitError(tagExistsError(name))
	}
	tag := &object.Tag{
		Name:       name,
		Tagger:     object.Signature{Name: userName, Email: userEmail, When: time.Now()},
		TargetType: plumbing.CommitObject,
		Target:     commit.Hash,
	}
	obj := repo.Storer.NewEncodedObject()
	if err := tag.Encode(obj); err != nil {
		return unknownGitError(err)
	}
	hash, err := repo.Storer.SetEncodedObject(obj)
	if err != nil {
		return unknownGitError(err)
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(refName, hash)); err != nil {
		return unknownGitError(err)
	}
	return nil
}

// HeadCommitSHA returns the full id of the commit HEAD points to. A repository without commits is an error.
func (g *GoGit) HeadCommitSHA(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	repo, err := g.open()
	if err != nil {
		return "", err
	}
	head, err := g.head(repo)
	if err != nil {
		return "", err
	}
	commit, err := repo.CommitObject(head)
	if err != nil {
		return "", unknownGitError(err)
	}
	return commit.Hash.String(), nil
}

// head resolves HEAD. A missing reference is reported as libgit2 does, naming the branch of an unborn HEAD.
func (g *GoGit) head(repo *gogit.Repository) (plumbing.Hash, error) {
	ref, err := repo.Head()
	if err == nil {
		return ref.Hash(), nil
	}
	if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return plumbing.ZeroHash, unknownGitError(err)
	}
	name := plumbing.HEAD
	if sym, serr := repo.Reference(plumbing.HEAD, false); serr == nil && sym.Type() == plumbing.SymbolicReference {
		name = sym.Target()
	}
	return plumbing.ZeroHash, unknownGitError(refNotFoundError(name.String()))
}
