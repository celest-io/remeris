---
description: "Commit and sign commits with remeris: the Commit step, the options that turn signing on, how it picks between your own key and a forge API, include and exclude patterns, and a setup guide for each way."
---

# Signed commits

The `Commit` step commits the files a workflow changed. It is remeris' own step: Knope has none, and its workflows run `git commit` through a `Command` step, which still works. The
step can sign the commit, and can limit which files go into it. For the command line, see [usage.md](usage.md); for every key, see [configuration.md](configuration.md).

```toml
[[workflows.steps]]
type = "Commit"
message = "chore: prepare release $version"
sign = true
exclude = ["mise*.lock"]
```

## Turning signing on

Signing is **off** unless you turn it on. Without it the step makes a plain commit that follows your own Git configuration, like `git commit -m` does.

| Where | Setting |
| --- | --- |
| the step | `sign = true` or `sign = false` |
| the command line | `--sign-commits` |
| the environment | `REMERIS_SIGN_COMMITS=true` |
| `knope.toml` | `[commit]` with `sign = true` |

When several are given, the step wins, then the command line, then the environment, then `[commit]`. `sign = false` in the step therefore turns signing off even when the
environment asks for it.

## Who signs

With signing on, `signer = "auto"` (the default) decides from what is set up, in this order:

1. **Git, with your own key**, when Git is set up to sign: `commit.gpgsign` is true or `user.signingkey` is set. The commit has your authorship and your signature. This is
    `git commit -S`, so it follows `gpg.format` (`openpgp`, `ssh` or `x509`), `user.signingkey` and the program of the format.
2. **The forge API**, when a forge is configured: `[github]` is in `knope.toml` and there is a GitHub App or a `GITHUB_TOKEN`. The commit is made by GitHub on the remote branch,
    and GitHub signs it, so it shows as Verified under the identity of the App.
3. Otherwise the step **fails** and prints a step-by-step guide to set one of them up (below). It never makes an unsigned commit when signing is on.

`signer = "git"` or `signer = "api"` (also `--signer` and `REMERIS_SIGNER`) allows only that way, so a bot never silently signs as you and you never silently sign as a bot.

The built-in Git support (used only when the `git` command is not installed) cannot sign and cannot leave other staged files out of a commit. It refuses and says so.

### The forge API

A commit made through the API has these properties:

- It is made on the **remote** branch, so the branch must be pushed and the local branch must be at the same commit. GitHub refuses the commit otherwise, and nothing is
  overwritten.
- Afterwards remeris fetches the commit and moves the local branch to it. The local files and index stay as they are.
- Regular files only. Executable bits and symbolic links are not carried, and a symbolic link in the selection is an error. Use your own key for those.
- It needs the **Contents: read and write** permission.

#### Which GitHub App

The commit uses the same authentication as every other GitHub call: the App of `--github-app-*`, else `GITHUB_TOKEN`. To use **another App just for commits**, for example one that
is allowed to push to a protected branch, give it its own options. They fall back on the default App when not given:

| Flag | Variable |
| --- | --- |
| `--commit-github-app-id` | `REMERIS_COMMIT_GITHUB_APP_ID` |
| `--commit-github-installation-id` | `REMERIS_COMMIT_GITHUB_INSTALLATION_ID` |
| `--commit-github-app-private-key-path` | `REMERIS_COMMIT_GITHUB_APP_PRIVATE_KEY_PATH` |
| none | `REMERIS_COMMIT_GITHUB_APP_PRIVATE_KEY` (the PEM key itself) |

Credentials come only from flags and variables, never from `knope.toml`.

## Choosing the files

By default the step commits every file that differs from `HEAD`: modified, untracked and deleted ones. Ignored files are never included. Narrow that with glob patterns:

| Where | Include | Exclude |
| --- | --- | --- |
| the step | `include = [...]` | `exclude = [...]` |
| the command line | `--commit-include` | `--commit-exclude` (repeat the flag) |
| the environment | `REMERIS_COMMIT_INCLUDE` | `REMERIS_COMMIT_EXCLUDE` (comma separated) |
| `[commit]` | `include = [...]` | `exclude = [...]` |

`include` narrows the set (everything when it is empty), then `exclude` removes files, so exclude wins. Patterns are the same globs as `assets`: `*` and `?` stay inside a
directory, `**` crosses directories, and a pattern such as `docs/**` or `docs` selects everything below it. Paths are relative to the root of the repository.

Only the selected files are committed. Anything else that is staged **stays staged**, and the other changed files stay changed. When nothing is selected the step logs
`Nothing to commit` and does nothing.

## Dry run

`--dry-run` makes no commit, reads no credential and uses no network. It reads the Git configuration and the changed files, then says what would happen, and fails with the setup
guide when signing is asked for and not possible, as a real run does:

```text
Would create a signed commit using git and your own signing key on main: chore: prepare release 1.2.0
Would commit these files:
  CHANGELOG.md
  VERSION
```

## Setting signing up

When signing is on and nothing can sign, the step stops with this guide.

### Sign with your own key

1. Create a key. An SSH key is the shortest way:

    ```sh
    ssh-keygen -t ed25519 -C "you@example.com" -f ~/.ssh/signing_key
    ```

    To use GPG instead, run `gpg --full-generate-key` and note the key id.
2. Tell Git to sign with it:

    ```sh
    git config --global gpg.format ssh
    git config --global user.signingkey ~/.ssh/signing_key.pub
    git config --global commit.gpgsign true
    ```

    For GPG, set `user.signingkey` to the key id and leave `gpg.format` alone.
3. Add the public key to your account as a **signing key** (GitHub: Settings, SSH and GPG keys, New SSH key, key type "Signing Key"), so that the commits show as Verified.
4. Check it: `git commit --allow-empty -S -m test`, then `git log --show-signature -1`.

### Sign through GitHub (CI and bots)

1. Create a GitHub App (Settings, Developer settings, GitHub Apps) with the **Contents: Read and write** repository permission, and install it on the repository.
2. Give remeris the App: `REMERIS_GITHUB_APP_ID`, `REMERIS_GITHUB_INSTALLATION_ID` and `REMERIS_GITHUB_APP_PRIVATE_KEY` (or `..._PATH`), or the flags. A `GITHUB_TOKEN` with
    Contents write also works. See [releasing.md](releasing.md#the-github-app) for the App this repository uses.
3. Add a `[github]` table to `knope.toml` and make sure the branch is pushed and up to date.

## Adding another forge

Signing through a forge API sits behind the `signing.APICommitter` interface (`internal/signing`): a forge says whether it is configured and how to make a commit from a branch, the
expected head, a message and the files. GitHub is the only one so far; GitLab's Commits API fits the same shape.
