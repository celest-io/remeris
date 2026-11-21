package github

import (
	"context"
	"encoding/base64"
	"os"
	"strings"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/diag"
)

const (
	codeCommit = "remeris::github::commit"

	// createCommitMutation is GitHub's createCommitOnBranch. The commit is made by GitHub, which signs it (it shows as
	// Verified) when the caller is a GitHub App or a user, and it is refused when the branch is not at expectedHeadOid,
	// so the branch cannot move between the check and the commit.
	createCommitMutation = `mutation($input: CreateCommitOnBranchInput!) {
  createCommitOnBranch(input: $input) { commit { oid } }
}`
)

// CommitFile is a file added or changed by a commit.
type CommitFile struct {
	// Path is slash-separated and relative to the root of the repository.
	Path    string
	Content []byte
}

// CommitInput describes one commit made through the API.
type CommitInput struct {
	// Branch is the branch to commit on. It must exist on GitHub.
	Branch string
	// ExpectedHead is the commit the branch must point to, so that nothing newer is overwritten.
	ExpectedHead string
	// Message is the full commit message: the first line is the headline, the rest the body.
	Message   string
	Additions []CommitFile
	// Deletions are the paths to remove.
	Deletions []string
}

// HasCredentials reports whether the forge was given credentials without looking at any secret: a GitHub App, or the
// token variable set. It reads neither the key nor the stored token and makes no request, so it is safe in a dry run.
// A stored token or a prompt is not counted: they are for the commands that need GitHub, not a reason to pick it.
func (f *Forge) HasCredentials() bool {
	if f.app != nil && f.app.ID != 0 {
		return true
	}
	getenv := f.store.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	return getenv(credentials.GitHubToken.Env()) != ""
}

// CreateCommitOnBranch makes a commit on in.Branch with the GraphQL mutation createCommitOnBranch and returns its id.
// GitHub signs the commit. Executable bits and symbolic links are not carried: a file is added as a regular file,
// or keeps the mode it has when it already exists.
func (c *Client) CreateCommitOnBranch(ctx context.Context, in CommitInput) (string, error) {
	headline, body, _ := strings.Cut(strings.TrimSpace(in.Message), "\n")
	additions := make([]map[string]string, 0, len(in.Additions))
	for _, f := range in.Additions {
		additions = append(additions, map[string]string{
			"path": f.Path, "contents": base64.StdEncoding.EncodeToString(f.Content),
		})
	}
	deletions := make([]map[string]string, 0, len(in.Deletions))
	for _, p := range in.Deletions {
		deletions = append(deletions, map[string]string{"path": p})
	}
	input := map[string]any{
		"branch":          map[string]string{"repositoryNameWithOwner": c.Owner + "/" + c.Repo, "branchName": in.Branch},
		"message":         map[string]string{"headline": headline, "body": strings.TrimSpace(body)},
		"expectedHeadOid": in.ExpectedHead,
		"fileChanges":     map[string]any{"additions": additions, "deletions": deletions},
	}

	req, err := c.NewRequest(ctx, "POST", c.graphQLURL(), map[string]any{
		"query": createCommitMutation, "variables": map[string]any{"input": input},
	})
	if err != nil {
		return "", WrapError("creating a commit", err)
	}
	var out struct {
		Data struct {
			Create struct {
				Commit struct {
					OID string `json:"oid"`
				} `json:"commit"`
			} `json:"createCommitOnBranch"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if _, err := c.Do(req, &out); err != nil {
		return "", WrapError("creating a commit", err)
	}
	if len(out.Errors) > 0 {
		msgs := make([]string, len(out.Errors))
		for i, e := range out.Errors {
			msgs[i] = e.Message
		}
		return "", diag.New("GitHub refused the commit: " + strings.Join(msgs, "; ")).
			WithCode(codeCommit).
			WithHelp("The branch must exist on GitHub and point to the commit you are on: push or pull first. " +
				"The token or App needs the Contents write permission, and a protected branch may refuse the commit.")
	}
	if out.Data.Create.Commit.OID == "" {
		return "", diag.New("GitHub did not return the id of the commit").WithCode(codeCommit)
	}
	return out.Data.Create.Commit.OID, nil
}

// graphQLURL is the GraphQL endpoint of the API the client talks to: "graphql" next to the REST API on github.com and
// on hosts whose API has its own domain, "api/graphql" for GitHub Enterprise Server, whose REST API is below
// "api/v3/". go-github refuses a path with "..", so the URL is built here and given as an absolute one; it has the
// host of the API, so the credentials guard lets the token through.
func (c *Client) graphQLURL() string {
	base := strings.TrimSuffix(c.BaseURL(), "/") + "/"
	if root, ok := strings.CutSuffix(base, "api/v3/"); ok {
		return root + "api/graphql"
	}
	return base + "graphql"
}
