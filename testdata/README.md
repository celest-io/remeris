# Test data

## `script/`

Golden tests for the `remeris` binary, run with
[`testscript`](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript) from
`cmd/remeris/main_test.go`. Each directory under `script/` is a suite; each `.txtar` file is one case,
holding the input files, the commands and the expected output.

```sh
go test ./cmd/remeris                       # all scripts
go test ./cmd/remeris -run TestScripts/cli  # one suite
UPDATE_SCRIPTS=1 go test ./cmd/remeris      # rewrite the files compared with cmp (review the diff!)
```

| Suite | Content |
| --- | --- |
| `cli` | Remeris' own scripts: flags, help, errors, dry run, `--validate`, default workflows. |
| `bump_version` | The `BumpVersion` step, ported from Knope's `bump_version` suite (`<dir>_<case>.txtar`). |
| `command` | The `Command` step. `shell` and `replace_version` are ported from Knope; the rest are ours. |
| `git_release` | The `Release` step in a repository with no forge, ported from Knope's `git_release` suite (`<dir>_<case>.txtar`). `split_workflow` is skipped until remeris prints all of Knope's `--verbose` debug lines; `split_workflow_quiet` (ours) runs the same case without `--verbose`. |
| `github_release` | The `Release` step with `[github]`, ported from Knope's `github_release` suite (`<dir>_<case>.txtar`). All are dry runs, as in Knope: a real release needs the GitHub API, which remeris tests with a fake server in Go (`internal/steps/release_github_test.go`). |
| `gitea_release` | The `Release` step with `[gitea]`, ported from Knope's `gitea_release` suite (`<dir>_<case>.txtar`). All are dry runs, as in Knope, except `release_assets`, which runs `--validate` and expects the error for asset uploads on Gitea. The real run is tested with a fake server in Go (`internal/steps/release_gitea_test.go`). |
| `multi_forge_release` | The `Release` step with `[github]` and `[gitea]`, ported from Knope's `multi_forge_release` suite (`<dir>_<case>.txtar`): the GitHub line, then the Gitea line, for each package in turn. `release_assets` has only `[gitea]` in Knope too. |
| `create_pull_requests` | The `CreatePullRequest` step with `[github]`, ported from Knope's `create_pull_requests` suite (`<case>.txtar`), and our own `gitea_*` and `github_and_gitea` scripts (`[gitea]`; Knope has no Gitea fixture). All are dry runs, as in Knope; the real run is tested with a fake server in Go (`internal/steps/create_pull_request_test.go`). The branch is printed as `main`, not `refs/heads/main` (Q3, see `deviations.md`). |
| `default_workflows` | Knope's `default_workflows` suite (`<dir>_<case>.txtar`, where `<dir>` is `release` or `get_version`). Help is compared by content, as in `cli`. `release_cargo_workspace` and `release_help_multi_package` use Cargo workspace discovery (F20.2). |
| `generate` | Knope's `generate` suite, one script per directory (`github` and `gitea` hold all their cases, run with each form of the remote): `cargo_workspace`, `github`, `gitea`, `no_remote`, `packages`, `packages_with_changelog`. Each compares stdout, the file and, by loading it again (`--validate` when there are packages), what was written. Remeris' own: `refuses_to_overwrite` (a directory or a symbolic link in the way), `dry_run`, `discovery_error` and `outside_repository`. |
| `discovery` | Remeris' own scripts for what is found when `knope.toml` declares nothing. Package discovery at the root (spec F20.1): the default package, Cargo workspaces (F20.2: `cargo_workspace*`, `cargo_glob_member`, `cargo_parse_error`), the M10 error for the other workspace roots, and no packages at all. Forge detection from the first remote (F20.5, F20.6): `github_remote_ssh` and `github_remote_https` run `git push` before the GitHub release, `codeberg_remote_ssh` and `codeberg_remote_https` do the same before the Gitea release, while `other_remote`, `no_remote`, `knope_toml_no_detection`, `codeberg_remote_evil` and `codeberg_remote_knope_toml` keep the no-forge order. All are dry runs. |
| `upgrade` | `--upgrade`. `ignore_conventional_commits` and `nothing` are ported from Knope's `upgrade` suite (the former also checks the warning line remeris adds); `idempotent`, `dry_run`, `prerelease_label{,_workflow}`, `comments_lost`, `jira_release_notes_kept`, `symlink`, `hybrid` and `discovery_error` are ours. |
| `prepare_release` | The `PrepareRelease` step. `changesets_*`, `enable_prerelease_*`, `second_prerelease`, `release_after_prerelease`, `override_*`, `scopes_*`, `multiple_packages` (skipped: needs M9), `allow_empty`, `no_version_change`, `branching_history_*`, `cargo_*`, `changelog_*`, `custom_change_entries_all_custom`, `deno_*`, `gleam_toml`, `go_modules_*`, `handle_pre_versions_that_are_too_new`, `ignore_conventional_commits{,_toplevel}`, `inconsistent_versions`, `no_versioned_files`, `npm_*`, `override_version_multiple_packages`, `package_selection`, `prerelease_after_release`, `pubspec_yaml`, `pyproject_toml_*`, `readme_version`, `tauri_conf_json` and `verbose` are ported from Knope's `prepare_release` suite (skipped ones are listed below) (`<dir>_<case>.txtar`); `no_packages_ready`, `stages_files`, `forge_variable` (it also checks the dry-run pull request data of F16.4, which no Knope script covers: Knope's only test of it, `integration_github`, needs real credentials) and `go_modules_major_version_directories_quiet` are ours. Scripts set `REMERIS_FAKE_DATE=2026-01-01` and write that date where Knope has `[DATE]`; `rstrip` (defined in `cmd/remeris/main_test.go`) removes trailing spaces from a dry run's output before it is compared. |

`--help` is compared by content, not byte for byte: its layout comes from urfave/cli and differs from
clap's (spec F3.1).

## Fixtures from Knope

The fixtures ported from [knope-dev/knope](https://github.com/knope-dev/knope) (`crates/knope/tests/**`)
are © Triax Technologies and Dylan Anthony, used under the MIT License:

> Copyright (c) 2021 Triax Technologies
> Copyright (c) 2022 Dylan Anthony
>
> Permission is hereby granted, free of charge, to any person obtaining a copy of this software and
> associated documentation files (the "Software"), to deal in the Software without restriction, including
> without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
> copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the
> following conditions:
>
> The above copyright notice and this permission notice shall be included in all copies or substantial
> portions of the Software.
>
> THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT
> LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO
> EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
> IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR
> THE USE OR OTHER DEALINGS IN THE SOFTWARE.

A ported script starts with a comment naming its Knope source directory. Unit tests under `pkg/` also reuse a few
Knope fixture files as golden data, such as `pkg/versioning/internal/jsonedit/testdata/` (from
`prepare_release/{npm,multiple_packages,tauri_conf_json}`) and `pkg/versioning/versionedfile/testdata/deno_json/` (from
`prepare_release/deno`) and `internal/tomlwrite/testdata/` (from `generate/*` and `upgrade/ignore_conventional_commits`); they
carry the same credit.

### Conversion rules

- The `in/` files become txtar files, `out/` files go under `out/`, and `stdout.log` / `stderr.log` become
  the files compared with `cmp`.
- `knope` is rewritten to `remeris` in expected output. The file name `knope.toml` is not (spec D1).
- miette's graphics in `stderr.log` are reduced to the message and help lines, as rendered by
  `internal/diag` (spec F3.3, D4).
- Git setup from the Rust `mod.rs` becomes `exec git ...` commands.
- A fixture that needs a feature that does not exist yet starts with `skip` and the milestones it needs.
  Remove the `skip` line when they are done.

### Status of `crates/knope/tests/command/*`

| Fixture | Status |
| --- | --- |
| `shell` | Ported: `script/command/shell.txtar`. |
| `replace_version` | Ported: `script/command/replace_version.txtar`. Its `knope.toml` declares no package, so it uses the root package discovery (F20.1). |

### Status of `crates/knope/tests/git_release/*`

| Fixture | Status |
| --- | --- |
| `single_package`, `multiple_packages` | Ported: `script/git_release/{single_package,multiple_packages}.txtar`. |
| `split_workflow` | Ported but skipped: it checks `--verbose` output, and remeris does not print all of Knope's debug lines yet (as `prepare_release/verbose`). `split_workflow_quiet.txtar` (ours) runs it without `--verbose`. |

### Status of `crates/knope/tests/github_release/*`

| Fixture | Status |
| --- | --- |
| `simple`, `auto_generate_release_notes`, `multiple_packages`, `no_previous_tag`, `version_go_mod` | Ported: `script/github_release/<name>.txtar`. |
| `release_assets/glob`, `release_assets/list` | Ported: `script/github_release/release_assets_{glob,list}.txtar`. |

### Status of `crates/knope/tests/{gitea_release,multi_forge_release}/*`

| Fixture | Status |
| --- | --- |
| `simple`, `auto_generate_release_notes`, `multiple_packages`, `no_previous_tag`, `version_go_mod` | Ported: `script/{gitea_release,multi_forge_release}/<name>.txtar`. |
| `release_assets` | Ported: `script/{gitea_release,multi_forge_release}/release_assets.txtar` (`--validate`, as in Knope). |

### Status of `crates/knope/tests/create_pull_requests/*`

| Fixture | Status |
| --- | --- |
| `default_variables`, `disable_default_variables`, `explicit_variables`, `unused_variables` | Ported: `script/create_pull_requests/<name>.txtar`, with `main` where Knope's `stdout.log` has `refs/heads/main` (Q3). |
| (no Knope fixture) | Ours: `script/create_pull_requests/{gitea_only,gitea_branch_with_slash,github_and_gitea}.txtar`. |

### Status of `crates/knope/tests/default_workflows/*`

| Fixture | Status |
| --- | --- |
| `document_change`, `release/no_forge` | Ported: `script/default_workflows/{document_change,release_no_forge}.txtar`. |
| `get_version/multi_package` | Ported: `script/default_workflows/get_version_multi_package.txtar`, with remeris' own unknown-workflow error (see `deviations.md`). |
| `get_version/single_package`, `release/help_single_package` | Ported: `script/default_workflows/{get_version,release_help}_single_package.txtar`. The help check leaves out the backticks of Knope's usage text (see `deviations.md`). |
| `release/help_multi_package` | Ported: `script/default_workflows/release_help_multi_package.txtar`. The help check leaves out the backticks, as for the single package. |
| `release/cargo_workspace` | Ported: `script/default_workflows/release_cargo_workspace.txtar`. |
| `release/github` | Ported: `script/default_workflows/release_github.txtar`. |
| `release/gitea` | Ported: `script/default_workflows/release_gitea.txtar`. |

### Status of `crates/knope/tests/bump_version/*`

| Fixture | Status |
| --- | --- |
| `single_package/{major,minor,patch,release}`, `single_package/pre/{from_full,from_pre,increment_pre}` | Ported: `script/bump_version/single_package_*.txtar`. |
| `multiple_packages/{major,minor,patch,release}`, `multiple_packages/pre/{from_full,from_pre}` | Ported: `script/bump_version/multiple_packages_*.txtar`. |
| `text_file/minor` | Ported: `script/bump_version/text_file_minor.txtar`. |
| `single_package/override`, `multiple_packages/override` | Ported: `script/bump_version/{single,multiple}_package*_override.txtar`. |
| `maven` | Ported: `script/bump_version/maven.txtar`. The output is the same as Knope's because the input is already in Knope's emitted form (see `deviations.md`, pom.xml). |

### Status of `crates/knope/tests/prepare_release/*` (skipped scripts)

Every case that Knope's `mod.rs` files declare is ported. These scripts are skipped:

| Fixture | Status |
| --- | --- |
| `go_modules/major_version_directories` | Ported but skipped: it checks `--verbose` output (as `verbose`). `go_modules_major_version_directories_quiet.txtar` (ours) checks the same files and module tags without `--verbose`. |
| `allow_empty` | Ported: `script/prepare_release/allow_empty.txtar`. Knope's `[..]` wildcard for the commit hash is a regular expression. |
| `cargo/workspace/{full,no_lock}` | Ported: `script/prepare_release/cargo_workspace_{full,no_lock}.txtar` (Cargo workspace discovery, F20.2). |
| `npm/workspaces` | Ported: `script/prepare_release/npm_workspaces.txtar`. |
| `deno/{workspaces,file_priority}` | Ported: `script/prepare_release/deno_workspaces.txtar` and `deno_file_priority.txtar` (Deno workspace discovery, M10). `script/discovery/deno_workspace.txtar` is ours. |
| `pubspec_yaml` | Ported: `script/prepare_release/pubspec_yaml.txtar`. |
| `verbose` | Skipped: remeris does not print five of Knope's `--verbose` debug lines yet (see `deviations.md`). |
| `custom_change_entries/with_fallback`, `go_modules/major_versions/failed_v2` | Not ported: Knope's `mod.rs` does not declare them. |

Other Knope suites (`validate`, ...) are ported
with the milestone that needs them. Deviations from Knope's behaviour are listed in
[`deviations.md`](deviations.md).
