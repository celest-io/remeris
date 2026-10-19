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
| `default_workflows` | Knope's `default_workflows` suite (`<dir>_<case>.txtar`, where `<dir>` is `release` or `get_version`). Help is compared by content, as in `cli`. Ports that need a forge or package discovery are skipped until their milestones (see below). |
| `prepare_release` | The `PrepareRelease` step. `changesets_*`, `enable_prerelease_*`, `second_prerelease`, `release_after_prerelease`, `override_*`, `scopes_*`, `multiple_packages` (skipped: needs M9), `allow_empty` (skipped: needs M10), `no_version_change`, `branching_history_*`, `cargo_*`, `changelog_*`, `custom_change_entries_all_custom`, `deno_*`, `gleam_toml`, `go_modules_*`, `handle_pre_versions_that_are_too_new`, `ignore_conventional_commits{,_toplevel}`, `inconsistent_versions`, `no_versioned_files`, `npm_*`, `override_version_multiple_packages`, `package_selection`, `prerelease_after_release`, `pubspec_yaml`, `pyproject_toml_*`, `readme_version`, `tauri_conf_json` and `verbose` are ported from Knope's `prepare_release` suite (skipped ones are listed below) (`<dir>_<case>.txtar`); `no_packages_ready`, `stages_files`, `forge_variable` and `go_modules_major_version_directories_quiet` are ours. Scripts set `REMERIS_FAKE_DATE=2026-01-01` and write that date where Knope has `[DATE]`; `rstrip` (defined in `cmd/remeris/main_test.go`) removes trailing spaces from a dry run's output before it is compared. |

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
`prepare_release/{npm,multiple_packages,tauri_conf_json}`); they carry the same credit.

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
| `replace_version` | Ported but skipped: needs package discovery (M10), since its `knope.toml` declares no package. |

### Status of `crates/knope/tests/git_release/*`

| Fixture | Status |
| --- | --- |
| `single_package`, `multiple_packages` | Ported: `script/git_release/{single_package,multiple_packages}.txtar`. |
| `split_workflow` | Ported but skipped: it checks `--verbose` output, and remeris does not print all of Knope's debug lines yet (as `prepare_release/verbose`). `split_workflow_quiet.txtar` (ours) runs it without `--verbose`. |

### Status of `crates/knope/tests/default_workflows/*`

| Fixture | Status |
| --- | --- |
| `document_change`, `release/no_forge` | Ported: `script/default_workflows/{document_change,release_no_forge}.txtar`. |
| `get_version/multi_package` | Ported: `script/default_workflows/get_version_multi_package.txtar`, with remeris' own unknown-workflow error (see `deviations.md`). |
| `get_version/single_package`, `release/help_single_package`, `release/help_multi_package` | Ported but skipped: need package discovery (M10). |
| `release/cargo_workspace` | Ported but skipped: needs the GitHub forge (M7) and package discovery (M10). |
| `release/github` | Ported but skipped: needs the GitHub forge (M7) and package discovery (M10). |
| `release/gitea` | Ported but skipped: needs the Gitea forge (M8). |

### Status of `crates/knope/tests/bump_version/*`

| Fixture | Status |
| --- | --- |
| `single_package/{major,minor,patch,release}`, `single_package/pre/{from_full,from_pre,increment_pre}` | Ported: `script/bump_version/single_package_*.txtar`. |
| `multiple_packages/{major,minor,patch,release}`, `multiple_packages/pre/{from_full,from_pre}` | Ported: `script/bump_version/multiple_packages_*.txtar`. |
| `text_file/minor` | Ported: `script/bump_version/text_file_minor.txtar`. |
| `single_package/override`, `multiple_packages/override` | Ported: `script/bump_version/{single,multiple}_package*_override.txtar`. |
| `maven` | Ported but skipped: needs `pom.xml` (M9). |

### Status of `crates/knope/tests/prepare_release/*` (skipped scripts)

Every case that Knope's `mod.rs` files declare is ported. These scripts are skipped:

| Fixture | Status |
| --- | --- |
| `changelog/header_level_detection` | Skipped: needs the GitHub forge (M7). |
| `go_modules/major_version_directories` | Ported but skipped: it checks `--verbose` output (as `verbose`). `go_modules_major_version_directories_quiet.txtar` (ours) checks the same files and module tags without `--verbose`. |
| `cargo/workspace/{full,no_lock}`, `deno/*`, `npm/workspaces`, `allow_empty` | Skipped: need package discovery (M10), and for `full`, `deno` and `npm` also `Cargo.lock`, `deno.json` or `package-lock.json` (M9). |
| `cargo/workspace/shared_version`, `npm/{dependencies,package}`, `gleam_toml`, `pubspec_yaml`, `tauri_conf_json`, `multiple_packages` | Skipped: need M9 formats. |
| `verbose` | Skipped: remeris does not print five of Knope's `--verbose` debug lines yet (see `deviations.md`). |
| `custom_change_entries/with_fallback`, `go_modules/major_versions/failed_v2` | Not ported: Knope's `mod.rs` does not declare them. |

Other Knope suites (`validate`, ...) are ported
with the milestone that needs them. Deviations from Knope's behaviour are listed in
[`deviations.md`](deviations.md).
