package signing

import (
	"strings"

	"github.com/celest-io/remeris/internal/diag"
)

// CodeNotConfigured is the code of the error that comes with the setup guide.
const CodeNotConfigured = "remeris::commit::signing_not_configured"

// NotConfigured is the error for a signed commit that no signer can make. Its help is a step-by-step guide to set up
// the way that was asked for (git or api), or both ways for "auto". The commit is not made unsigned: the user turns
// signing off with sign = false.
func NotConfigured(m Method, forges []APICommitter) error {
	var b strings.Builder
	switch m {
	case MethodGit:
		b.WriteString("Git is not set up to sign commits, and the signer is \"git\".\n\n")
		b.WriteString(gitGuide("Set up Git to sign with your own key"))
	case MethodAPI:
		b.WriteString("No forge is set up to make signed commits, and the signer is \"api\".\n\n")
		b.WriteString(apiGuide("Set up the forge API", forges))
	default:
		b.WriteString("Commit signing is on, but there is no way to sign: Git has no signing key and no forge is set up.\n")
		b.WriteString("Set up one of these two ways. Git is used when it is set up, else the forge.\n\n")
		b.WriteString(gitGuide("1. Sign with your own key (on your machine)"))
		b.WriteString("\n")
		b.WriteString(apiGuide("2. Sign through the forge API (for CI and bots)", forges))
	}
	b.WriteString("\nThen run the workflow again. To commit without signing, set sign = false.")
	return diag.New("Cannot sign the commit: signing is not set up").
		WithCode(CodeNotConfigured).
		WithHelp(b.String())
}

func gitGuide(title string) string {
	return title + `
   a. Create a signing key. An SSH key is the shortest way:
        ssh-keygen -t ed25519 -C "you@example.com" -f ~/.ssh/signing_key
      (To use GPG instead, run "gpg --full-generate-key" and note the key id.)
   b. Tell Git to sign with it:
        git config --global gpg.format ssh
        git config --global user.signingkey ~/.ssh/signing_key.pub
        git config --global commit.gpgsign true
      (For GPG: git config --global user.signingkey <KEY ID> and git config --global commit.gpgsign true.)
   c. Add the public key to your account as a signing key, so that the commits show as Verified:
        GitHub: Settings > SSH and GPG keys > New SSH key, with the key type "Signing Key".
   d. Check it works: git commit --allow-empty -S -m test, then git log --show-signature -1.
`
}

func apiGuide(title string, forges []APICommitter) string {
	if len(forges) == 0 {
		return title + `
   No forge that can sign is set up for this repository. Add a [github] table to knope.toml
   (owner and repo), then follow these steps.
` + githubSteps()
	}
	return title + "\n" + githubSteps()
}

func githubSteps() string {
	return `   a. Create a GitHub App (GitHub: Settings > Developer settings > GitHub Apps > New) with the
      "Contents: Read and write" repository permission, and install it on this repository.
   b. Give remeris the App:
        REMERIS_GITHUB_APP_ID, REMERIS_GITHUB_INSTALLATION_ID and REMERIS_GITHUB_APP_PRIVATE_KEY
        (or REMERIS_GITHUB_APP_PRIVATE_KEY_PATH), or --github-app-id, --github-installation-id and
        --github-app-private-key-path.
      To use another App just for commits, set REMERIS_COMMIT_GITHUB_APP_ID,
      REMERIS_COMMIT_GITHUB_INSTALLATION_ID and REMERIS_COMMIT_GITHUB_APP_PRIVATE_KEY (or ..._PATH):
      without them, the App above is used. A GITHUB_TOKEN with Contents write also works.
   c. Make sure the branch is pushed and up to date: GitHub makes the commit on the remote branch.
      Executable bits and symbolic links are not carried by the API; use your own key for those.
`
}
