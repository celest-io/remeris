# Configuration

remeris reads Knope's `knope.toml` from the working directory. This page lists what remeris reads from it. For what each setting means in practice, see the Knope documentation at
<https://knope.tech>; remeris follows it, and [differences.md](differences.md) lists where it does not. For the command line, see [usage.md](usage.md). For the project overview,
see [../README.md](../README.md).

Unknown keys, tables and step fields are ignored without a warning (Knope's behaviour, spec quirk Q1). A typo such as `label` for `labels` therefore has no effect. The one
exception is `remeris --upgrade`, which refuses to rewrite a file that has keys it would drop.

A missing or unreadable `knope.toml` is not an error: remeris then uses the discovered packages and the default workflows described below.

## Top-level keys

| Key | Kind | Purpose |
| --- | --- | --- |
| `[package]` | table | The single, unnamed package (name `default`, tags `vX.Y.Z`). |
| `[packages.<name>]` | tables | Named packages, in file order (tags `<name>/vX.Y.Z`). |
| `[changes]` | table | Which commits are read. |
| `[release_notes]` | table | How changes are rendered in changelogs. |
| `[github]`, `[gitea]`, `[jira]` | tables | Forge and issue tracker settings. |
| `[[workflows]]` | array of tables | The workflows, each a list of steps. |

`[package]` and `[packages]` cannot both be defined, and an empty `[packages]` table is an error.

## Packages

`[package]` and each `[packages.<name>]` take the same fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `versioned_files` | array | Files that hold the version (see below). |
| `changelog` | string | Path of the changelog file to update. |
| `scopes` | array of strings | Only conventional commits with one of these scopes count for the package. `[]` is different from absent. |
| `extra_changelog_sections` | array of tables | Extra changelog sections: `name`, `footers` (array), `types` (array). |
| `assets` | string or array | Files to upload to the GitHub release: a glob (`"dist/*"`) or a list of `{ path, name }` tables. Not supported with `[gitea]`: such a configuration is refused. |
| `ignore_go_major_versioning` | bool | Skip Go's rule that a major version above 1 changes the module path in `go.mod`. |

Package names in `[packages.<name>]` may contain dots. Workflow names (below) may not.

```toml
[packages.first]
versioned_files = ["Cargo.toml"]
changelog = "FIRST_CHANGELOG.md"
scopes = ["first", "both"]

[[packages.first.extra_changelog_sections]]
name = "Notes"
footers = ["Changelog-First-Note"]
```

### Versioned files

An entry of `versioned_files` is a path, `{ path, dependency }` or `{ path, regex }`. The path is relative to the working directory and must exist. The format is chosen from the
last path component (case-sensitive). `dependency` says whether the entry may name a dependency:

| File name | `dependency` |
| --- | --- |
| `Cargo.toml` | yes |
| `Cargo.lock` | yes |
| `deno.json`, `deno.jsonc` | yes |
| `deno.lock` | yes |
| `gleam.toml` | no |
| `go.mod` | no |
| `package.json` | yes |
| `package-lock.json` | yes |
| `pom.xml` | no |
| `pubspec.yaml` | no |
| `pyproject.toml` | no |
| `tauri.conf.json`, `tauri.macos.conf.json`, `tauri.windows.conf.json`, `tauri.linux.conf.json` | no |

- `dependency = "name"` versions that dependency inside the file instead of the file's own package version. It is an error on a format marked "no".
- `regex` is a string or an array of strings. Each pattern finds the version in any text file, in a named group `version`. Patterns use Go's RE2 syntax. `regex = []` counts as
    given (a regex file with no pattern).
- `dependency` and `regex` cannot be used together.
- Any other file name is an error that lists the supported names.

```toml
[package]
versioned_files = [
  "Cargo.toml",
  { path = "README.md", regex = "version:\\s+(?<version>\\d+\\.\\d+\\.\\d+)" },
]
```

## `[changes]`

| Key | Type | Meaning |
| --- | --- | --- |
| `ignore_conventional_commits` | bool | Do not read conventional commits; use change files only. |
| `ignore_conventional_merge_commits` | bool | Skip commits with more than one parent when reading conventional commits. |

`PrepareRelease` still accepts `ignore_conventional_commits` as a deprecated step field; `remeris --upgrade` moves it to `[changes]`.

## `[release_notes]`

| Key | Type | Meaning |
| --- | --- | --- |
| `change_templates` | array of strings | Templates for one change entry in the changelog, with the placeholders `$summary`, `$details`, `$commit_author_name`, `$commit_hash`, `$pr_number` and `$pr_author_login`. A template that uses a placeholder the change cannot fill is not used for that change; other `$words` stay as written. |

```toml
[release_notes]
change_templates = ["- $summary (#$pr_number by @$pr_author_login)"]
```

`$pr_number` and `$pr_author_login` need `[github]`; in a dry run they get fixed values and no request is made.

## `[github]`, `[gitea]`, `[jira]`

| Table | Keys | Notes |
| --- | --- | --- |
| `[github]` | `owner`, `repo` | Used by `Release`, `CreatePullRequest`, `SelectGitHubIssue` and the `$pr_*` placeholders. |
| `[github]` | `api_url`, `upload_url` | remeris only (Knope ignores them), for GitHub Enterprise Server. `upload_url` needs `api_url`. Both must be absolute http(s) URLs, and a configuration file cannot choose where the token goes: the URL is used only when `--github-api-url` or `REMERIS_GITHUB_API_URL` (and `--github-upload-url` or `REMERIS_GITHUB_UPLOAD_URL`) gives the same value. See [usage.md](usage.md). |
| `[gitea]` | `owner`, `repo`, `host` | `host` needs a scheme and uses https (http only for loopback). |
| `[jira]` | `url`, `project` | `url` needs a scheme and uses https (http only for loopback). |

```toml
[github]
owner = "knope-dev"
repo = "knope"
```

Without a `knope.toml`, remeris reads the first Git remote: a `github.com` remote sets `[github]`, and a `codeberg.org` remote sets `[gitea]`. A `knope.toml` without `[github]`
stays without GitHub.

## Workflows

```toml
[[workflows]]
name = "release"
help_text = "Prepare and publish a release"   # optional, shown in --help

[[workflows.steps]]
type = "PrepareRelease"
```

`name` must match `^[A-Za-z0-9_-]+$`. Each `[[workflows.steps]]` table needs a `type` from the list below, and runs in order. Each workflow becomes a subcommand of `remeris`.

### Default workflows

When `knope.toml` has no `[[workflows]]`, remeris generates:

| Workflow | Steps | When |
| --- | --- | --- |
| `release` | `PrepareRelease`, `git commit -m "chore: prepare release $version"`, `Release`, `git push`, `git push --tags` | no forge |
| `release` (forge) | `PrepareRelease`, the commit, `git push`, `Release` | `[github]` or `[gitea]` is set or detected |
| `document-change` | `CreateChangeFile` | always |
| `get-version` | `Command` running `echo "$version"` | exactly one package |

With two or more packages the commit message is `chore: prepare releases`.

### Steps

Meaning of each step and field: <https://knope.tech>. "Variables" below means the `variables` table described in the next section.

| `type` | Fields | What it does |
| --- | --- | --- |
| `Command` | `command`, `variables`, `shell` | Runs a command after variable substitution. `shell = true` runs it with `sh -c` (`cmd /C` on Windows); otherwise the line is split into arguments and no `$VAR` expansion happens. |
| `BumpVersion` | `rule`, `label` | Bumps the version and edits versioned files. `rule` is `Major`, `Minor`, `Patch`, `Pre` or `Release`; `Pre` requires `label`, other rules ignore it. |
| `PrepareRelease` | `prerelease_label`, `allow_empty`, `ignore_conventional_commits` | Computes versions from commits and change files, edits versioned files and changelogs, removes change files, stages the result. `ignore_conventional_commits` is deprecated for `[changes]`. |
| `Release` | none | Creates the Git tags and the GitHub or Gitea releases of the prepared release. |
| `CreateChangeFile` | none | Asks which packages, what kind of change and a summary, then writes a file in `.changeset/`. |
| `CreatePullRequest` | `base`, `title`, `body` | Creates or updates a pull request on GitHub and/or Gitea. `title` and `body` are tables with `template` and `variables`. |
| `SelectGitHubIssue` | `labels` | Asks for one of the open issues of `[github]`; `labels` narrows the list. |
| `SelectGiteaIssue` | `labels` | The same for `[gitea]`. |
| `SelectJiraIssue` | `status` | Asks for one of the issues of the `[jira]` project in that status. |
| `TransitionJiraIssue` | `status` | Moves the selected Jira issue to that status. |
| `SelectIssueFromBranch` | none | Takes the issue from the current branch name (`123-summary` for GitHub or Gitea, `ABC-123-summary` for Jira). |
| `SwitchBranches` | none | Switches to the branch named after the selected issue, creating it if needed. |
| `RebaseBranch` | `to` | Fast-forwards the branch `to` to the current commit and switches to it. It fails if `to` has commits the current branch lacks. |

```toml
[[workflows]]
name = "bump"

[[workflows.steps]]
type = "BumpVersion"
rule = "Minor"
```

An unknown `type` is an error that lists the valid ones. In a dry run (`--dry-run`) steps log what they would do and change nothing; the issue steps use fake issues and read no
credentials.

## Variables

`Command`, and the `title` and `body` of `CreatePullRequest`, substitute variables into their text. A `variables` table maps the text to replace (any string, such as `"$version"`
or `"{version}"`) to a variable name:

| Variable | Value |
| --- | --- |
| `Version` | The version of the package. |
| `ChangelogEntry` | The changelog entry of the prepared release. |
| `IssueBranch` | The branch name of the selected issue (needs a `Select*Issue` step first). |

```toml
[[workflows.steps]]
type = "CreatePullRequest"
base = "main"

[workflows.steps.title]
template = "chore: Release {version}"
variables = { "{version}" = "Version" }
```

If the `variables` key is absent, `$version` means `Version` and `$changelog` means `ChangelogEntry`. If it is present, only the listed entries apply, and `variables = {}` turns
substitution off. An unknown variable name is an error.

`Version` and `ChangelogEntry` need the packages, so a workflow that uses them (or has a `PrepareRelease`, `BumpVersion`, `Release` or `CreateChangeFile` step) fails if package
discovery failed.

## Package discovery

If `knope.toml` declares no package, remeris looks for packages in the working directory, in this order:

1. A Cargo workspace: `./Cargo.toml` with a `[workspace]` table and a `members` list (paths, not globs). One package per member, named after its `package.name`, with its commit
    scope. `Cargo.lock` is added if it exists.
2. npm workspaces: the `workspaces` patterns of `./package.json`. One package per matching directory with a `package.json`, which must have a `name`.
3. A Deno workspace: a root `deno.json` or `deno.jsonc` is required, its `workspace` key is optional. The root folder comes first, then the members. A folder is a package when its
    `deno.json(c)` has a name and a version, or else its `package.json` has both. Only the working directory is read, never a parent workspace.
4. Otherwise, one package named `default` that holds every default file name that exists, in this order: `Cargo.toml`, `Cargo.lock`, `deno.json`, `deno.lock`, `gleam.toml`,
    `go.mod`, `package.json`, `package-lock.json`, `pom.xml`, `pubspec.yaml`, `pyproject.toml` and the four `tauri*.conf.json` names, plus `CHANGELOG.md` as the changelog if it
    exists. With none of these, there is no package.

The first three results are concatenated if several apply. A discovery error is raised only by a workflow that reads the packages, and by `--validate` and `--generate`; `--help`
and workflows that need no package keep working. `remeris --generate` writes the discovered configuration as a `knope.toml` (see [usage.md](usage.md)).
