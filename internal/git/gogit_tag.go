package git

import (
	"context"
	"time"

	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// CreateTag creates an annotated tag with an empty message on HEAD (spec F9.5). Sign is ignored.
//
// go-git's Repository.CreateTag validates its options and may refuse an empty message, so the tag object is
// written here. The tagger is user.name and user.email from the system, global and repository configuration.
func (g *GoGit) CreateTag(ctx context.Context, name string, _ TagOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	repo, err := g.open()
	if err != nil {
		return err
	}
	head, err := repo.Head()
	if err != nil {
		return unknownGitError(err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return unknownGitError(err)
	}
	cfg, err := repo.ConfigScoped(config.SystemScope)
	if err != nil {
		return unknownGitError(err)
	}
	userName, userEmail := cfg.User.Name, cfg.User.Email
	if userName == "" {
		return noCommitterError(missingConfigError(configUserName))
	}
	if userEmail == "" {
		return noCommitterError(missingConfigError(configUserEmail))
	}

	refName := plumbing.NewTagReferenceName(name)
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
	head, err := repo.Head()
	if err != nil {
		return "", unknownGitError(err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return "", unknownGitError(err)
	}
	return commit.Hash.String(), nil
}
