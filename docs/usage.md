# Usage

```sh
remeris [global options] [workflow [workflow options]]
```

`remeris --help` lists the workflows and `remeris <workflow> --help` shows the options of one. `--version` (`-V`) prints the version. Remeris reads `knope.toml` in the working
directory; see [configuration.md](configuration.md).

## Install

With [mise](https://mise.jdx.dev), which verifies the signed release bundle of the GitHub release before it installs:

```sh
mise use -g packslip:github.com/celest-io/remeris        # or without -g, to record it in the mise.toml of the project
mise use -g packslip:github.com/celest-io/remeris@0.0.1  # an exact version also skips mise's minimum release age (24 hours by default)
```

TODO: the signer fingerprint to pin is filled in here after the first release with a `packslip.sigstore.json` exists. Until then do not pin one. The archives of every release can
also be downloaded and [verified by hand](releasing.md#verify-a-download), or built with `go install github.com/celest-io/remeris/cmd/remeris@latest`.

## Workflows

Each workflow in `knope.toml` is a subcommand with the same name. Without a `knope.toml`, the workflows `release`, `document-change` and `get-version` exist, and `--generate`
writes them to a file.

Global options can be given before or after the workflow name.

| Option | Meaning |
| --- | --- |
| `--dry-run` | Say what would happen and change nothing: no file, Git or network change, no prompt. |
| `--verbose`, `-v` | Also print the debug lines (see Output). |
| `--version`, `-V` | Print the version. |
| `--help`, `-h` | Show help. |
| `--github-app-id`, `--github-installation-id`, `--github-app-private-key-path` | GitHub App authentication (see Credentials). |
| `--github-api-url`, `--github-upload-url` | The GitHub Enterprise URLs that may receive credentials. |

Options of a workflow, present only when the workflow has the step that uses them:

| Option | Present when | Meaning |
| --- | --- | --- |
| `--override-version` | the workflow has `BumpVersion` or `PrepareRelease`, and there is at least one package | Set the new version instead of calculating it. With one package the value is a version. With several it is `package_name=version`, and the option can be repeated. A comma is not a separator. |
| `--prerelease-label` | the workflow has `PrepareRelease` | Sets `prerelease_label` of every `PrepareRelease` step. |

`--prerelease-label` also reads `REMERIS_PRERELEASE_LABEL`, then `KNOPE_PRERELEASE_LABEL`. The flag wins over both.

## Special modes

These options belong to the root command and replace the workflow. Naming a workflow with one of them is an error (`No workflow named ...`). Which of them exist depends on
`knope.toml`:

| Mode | Exists when | What it does |
| --- | --- | --- |
| `--generate` | there is no `knope.toml` | Writes the default configuration to `knope.toml`, including the packages found by discovery. It never overwrites: if the path exists in any form it fails with `remeris::generate::exists`. With `--dry-run` it prints the file. |
| `--upgrade` | there is a `knope.toml` | Moves `ignore_conventional_commits` from `PrepareRelease` steps to `[changes]`. Prints `Nothing to upgrade` if there is nothing to move. Otherwise it rewrites the whole file, so comments and formatting are lost, and it refuses (`remeris::upgrade::unknown_key`) when the file has keys remeris does not read. With `--dry-run` it prints the result. |
| `--validate` | there is a `knope.toml` | Loads the packages, then runs every workflow as a dry run and reports all problems together. Logs are silenced. `--override-version` of a workflow is applied first. |

Where a mode does not exist, the option is an unknown argument: `unexpected argument '--validate' found`. `--generate` and `--upgrade` ignore `--prerelease-label`, so the label
never reaches the file they write.

## Output

- Normal messages go to stdout, one per line, with no level or time. Warnings, such as a rate-limit hint, are printed the same way. Prompts are written to stdout too.
- A failure is rendered to stderr as `Error: <message>`, then `help: <text>` when there is one, then related errors. Messages and help text follow Knope's, with `knope` rewritten
    to `remeris`. Diagnostics carry a code such as `remeris::generate::exists` where one is defined. They carry no documentation URLs.
- `--verbose` adds the debug lines to stdout: `Running step ...`, the packages loaded, the Git tags and commits used, the conventional-commit and version-rule decisions, and pull
    request lookups. Knope prints most of these as `debug!` lines. Remeris also prints some that Knope does not, all at debug level:

| Line | When |
| --- | --- |
| `Using GITHUB_TOKEN` | The GitHub token comes from the environment. |
| `Using the stored GitHub token` | The token comes from the credential file, or was just entered. |
| `Using GitHub App installation <id>` | GitHub App authentication is used. |
| `Using GitHub without authentication` | A GitHub lookup that does not require a token ran without one. |
| `Using the Gitea token`, `Using the Jira token` | A Gitea or Jira credential was obtained. The value is never printed. |
| `Could not copy the Knope credential <name> to <dir>: <error>` | The migration from the `knope` directory failed. The Knope value is still used. |
| `git not found on PATH, using built-in Git support` | Remeris uses its built-in Git implementation instead of the `git` command. |
| `Could not open the Git repository: <error>`, `Could not read the Git tags: <error>`, `Could not read the HEAD commit: <error>` | A Git lookup that is optional failed and was skipped. |

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success, including `--help`, `--version` and `Nothing to upgrade`. |
| 1 | Any failure: configuration, package, step, forge or credential errors, and usage errors such as an unknown flag or workflow. |

Knope's argument parser exits with 2 on usage errors. Remeris exits with 1.

## Environment variables

| Variable | Used for |
| --- | --- |
| `REMERIS_PRERELEASE_LABEL` | `--prerelease-label`. Read before `KNOPE_PRERELEASE_LABEL`. |
| `KNOPE_PRERELEASE_LABEL` | `--prerelease-label`, under Knope's name. |
| `GITHUB_TOKEN` | GitHub token. Overrides the stored token. |
| `GITEA_TOKEN` | Gitea token. Overrides the stored token. |
| `REMERIS_GITHUB_APP_ID` | `--github-app-id`. |
| `REMERIS_GITHUB_INSTALLATION_ID` | `--github-installation-id`. Looked up from the repository when 0. |
| `REMERIS_GITHUB_APP_PRIVATE_KEY_PATH` | `--github-app-private-key-path`. |
| `REMERIS_GITHUB_APP_PRIVATE_KEY` | The PEM key itself. Has no flag. Used only when no path is given. |
| `REMERIS_GITHUB_API_URL` | `--github-api-url`. |
| `REMERIS_GITHUB_UPLOAD_URL` | `--github-upload-url`. |

## Credentials

A forge or Jira credential is looked up in this order, and the first non-blank value wins:

1. The environment variable, for the GitHub token (`GITHUB_TOKEN`) and the Gitea token (`GITEA_TOKEN`). An empty variable counts as unset. Jira has no variable.
2. The file `remeris/<name>` in the user configuration directory (Go's `os.UserConfigDir`, for example `~/.config` on Linux). The names are `github_token`, `gitea_token`,
    `jira_token` and `email`. A file holds the raw value.
3. The file `knope/<name>` in the same directory. Its value is copied to the `remeris` directory, so a Knope user is not asked again. Knope's files are left in place.
4. A prompt, which asks for the secret without echo and saves the answer to `remeris/<name>` (directory mode 0700, file mode 0600). A blank answer is refused and not saved.
    Without a terminal the prompt fails with `Failed to get user input`.

The Gitea token file is shared by all hosts. Jira needs both an email address and an API token, each stored in its own file. Values are never logged. In a dry run credentials are
not read, so nothing is copied or prompted.

### GitHub App

With `--github-app-id` set, remeris authenticates as a GitHub App installation instead of using a token. The private key comes from `--github-app-private-key-path`, or else from
`REMERIS_GITHUB_APP_PRIVATE_KEY`. These options are checked when `knope.toml` has a `[github]` section: an App ID needs a key, an installation ID needs an App ID, and IDs must not
be negative (`remeris::github::app_options`). A key without an App ID is ignored. They are never read from `knope.toml`.

### GitHub Enterprise

An `api_url` or `upload_url` in `[github]` receives credentials only if the same URL is given with `--github-api-url` or `--github-upload-url` (or the variables above), so a
repository's configuration cannot decide where a token is sent.
