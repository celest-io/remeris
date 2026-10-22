# Deviations from Knope in ported fixtures

Each entry names a Knope fixture that cannot be ported unchanged, and why (spec: Testing strategy).

| Fixture | Change | Reason |
| --- | --- | --- |
| `validate/kitchen_sink` | Workflow names such as `Cannot SelectJiraIssue without Jira config ` must be renamed (no spaces, no trailing space). | Remeris restricts workflow names to `^[A-Za-z0-9_-]+$` because a `.` would break the koanf key path (spec F3.1). Knope allows any string. |
| Version grammar (F1.1) | `1.2.3-rc4`, `1.2.3-rc.4.5`, `1.2.3+build.7` and `1.2.3-alpha.beta.1` are valid versions. Knope rejected them, so tags like these were skipped; they now count as releases. | Full SemVer 2.0.0 (D8). |
| Version grammar (F1.1) | Rejected now, accepted by Knope: leading zeros (`01.2.3`, which Knope read as `1.2.3`, and `1.2.3-rc.01`) and a leading `+` on a number (`+1.2.3`, accepted by Rust's integer parser). Build identifiers may keep leading zeros. Numeric identifiers, pre-release ones included, must fit in a uint64; SemVer itself sets no limit. | Full SemVer 2.0.0 (D8), with the uint64 limit of spec F1.1. |
| Version ordering (F1.1) | Pre-releases are ordered by SemVer 2.0.0 section 11 precedence, not by "label string, then number". Both give the same order for the `label.N` shape. | Quirk Q2, D8. |
| Version display and equality (F1.1) | `+build` metadata is kept when a version is printed or written. Versions that differ only in build metadata have the same precedence but are not equal, so `1.2.3` and `1.2.3+b1` in one package are reported as inconsistent (F7.2). | D8. |
| `prepare_release/unknown_versioned_file_format` | The `Unknown file` help is `remeris identifies the type of file based on its name. Supported file names: …` instead of Knope's `Knope identities the type of file based on its name.` | Spec F2.2 asks the help to list the supported file names. |
| `versioned_files` with `deno.jsonc` | `deno.jsonc` is accepted as an explicit versioned file (DenoJson). Default detection still has no `deno.jsonc`, as in Knope. | Quirk Q11. |
| Regex versioned files (`{ path, regex }`) | Patterns use Go's RE2 syntax, not Rust's `regex` crate: `\d`, `\w`, `\s` and `\b` are ASCII-only, and Rust-only syntax such as `(?x)`, class set operations and `\u{…}` is an invalid pattern. Ordinary patterns like `\d+\.\d+\.\d+` behave the same. The `Invalid regex pattern` message carries Go's error text. | No Rust regex engine in Go; spec 0.3 picks stdlib `regexp`. |
| `Cargo.toml` edits | The whitespace around an edited value and a trailing comment on its line are kept: `version="1"   # note` becomes `version="3.0.0"   # note`. Knope's toml_edit resets the value's decor, so it writes `version= "3.0.0"` and drops the comment. CRLF line endings are kept too (new lines use the document's line ending); Knope rewrites the file with LF. | Format-preserving edits (spec F6.2): change only the bytes of the value. |
| `Cargo.toml` dependency edits | When `[workspace]` has no `dependencies` table, editing a dependency does not add `dependencies = {}` to `[workspace]`. Knope does, as a side effect of `entry().or_insert`. | The side effect is an accident of toml_edit's API, not a feature. |
| `pyproject.toml` with both `tool.poetry.version` and `project.version`, `[tool.poetry]` first | When the new version has a different length, Knope reuses stale byte offsets for the second replacement: a longer version writes a broken file (`version 10.20.300.0"`), a shorter one panics (`range end index … out of range`). Remeris writes both versions correctly. | Knope bug; spans are recomputed after each edit. |
| `pyproject.toml` with a multi-line string version (`version = """1.0.0"""`) | The delimiters are kept. Knope's span arithmetic rewrites it as `"1.0.1"`. | Knope bug. |
| `pyproject.toml` invalid or ill-typed content | The message keeps Knope's form `Could not deserialize {path} as a pyproject.toml: {err}`, but `{err}` is Go's TOML parser text, not serde's. | Different TOML library. |
| `package.json` with a duplicated top-level `version` key | The last value wins. Knope rejects the file (`duplicate field \`version\``); other duplicated keys keep the last value in both. | The JSON tree keeps the last value for every duplicated key, like serde_json's map; only Knope's struct deserializer treats `version` specially. |
| `[packages]` with no packages | An empty `[packages]` table is always an error (`The \`packages\` key is defined but does not contain any packages`). Knope reports it only when `[package]` is also defined; otherwise it falls back to package discovery. | Spec F2.2. |
| Read and write errors (`Error reading from {path}`, `Error writing to {path}`) | The cause is Go's OS error text (`is a directory`, `no such file or directory`), not Rust's (`Is a directory (os error 21)`). | Different standard library. |
| `prepare_release/invalid_versioned_files_package_json` | Expects `Error deserializing package.json: EOF`, where Knope prints `EOF while parsing a value at line 1 column 0`. | Different JSON library. |
| `bump_version/*` with `package.json` | The real run checks `git diff` of `package.json` instead of `cmp` against `out/package.json`. | The output has no final newline (Q10) and a txtar file always ends with one. |
| `bump_version/multiple_packages/release` | The ported `dryrun_stdout.log` expects `Cargo.toml: version = 0.1.3` (and the same for `package.json`); Knope's file has no `version = ` prefix. | The fixture is stale upstream: `multiple_packages/mod.rs` has no `mod release;`, so Knope never runs it. Knope's code writes the prefix, as in every other fixture. |
| Usage text with backticks (`--override-version`: ``Override the version set by `BumpVersion` or `PrepareRelease` …``) | The backticks are left out: `Override the version set by BumpVersion or PrepareRelease for the package.` | urfave/cli turns a backticked word into the flag's placeholder. |
| Workspace roots with no `knope.toml` packages | Cargo, npm and Deno workspaces (and a root `deno.json(c)` with `name` and `version`) are an error (`Package discovery for … workspaces is not implemented yet (milestone M10)`). Knope releases the members. The error is raised only by a workflow that reads the packages: one with a `PrepareRelease`, `BumpVersion`, `Release` or `CreateChangeFile` step, or a `Command` or `CreatePullRequest` that uses the `Version` or `ChangelogEntry` variable; and by `--validate`. `--help` and the other workflows, such as one with only a plain `Command`, run as usual. | M10 is not built; releasing the root as one package would be wrong. |
| `prepare_release/allow_empty` | The real run checks git's output line with the regular expression `^\[main [0-9a-f]+\] chore: prepare release 1\.0\.0$` instead of `cmp` against a `stdout.log` that has Knope's `[..]` wildcard. | `cmp` has no wildcard, and the commit hash changes at every run. |
| `--override-version` with a bad value | The error is our semver parse error (`Found invalid semantic version …`), not clap's `invalid value '…' for '--override-version <…>': …` wrapper. | urfave/cli has no per-value parser error of clap's form. |
| `--override-version` repeated with one package | The error text is `the argument '--override-version <OVERRIDE_ONE_VERSION>' cannot be used multiple times`, written by hand, without clap's usage lines and `For more information, try '--help'`. | urfave/cli accepts a repeated slice flag; remeris rejects it after package loading. |
| `--override-version` bad values | They are reported after the packages are loaded, with exit code 1. clap reports them first, with exit code 2. | The values are parsed once the packages are known (spec F3.1). |
| `prepare_release/*`, `git_release/*`, `default_workflows/*` | `[DATE]` is the fixed date `2026-01-01`, set with `env REMERIS_FAKE_DATE=2026-01-01`, which only the test hook in `cmd/remeris/main_test.go` reads (the binary does not). A dry run's output is compared through `rstrip`, which removes the space that ends `Would add the following to {path}: `. | Scripts need a fixed date, and the repository's whitespace check rejects trailing spaces in a script. |
| `prepare_release/*` and `git_release/multiple_packages` with `package.json` | The real run checks the version with `grep` instead of `cmp` against `out/package.json`; input files that had no final newline get one. | Q10: the output has no final newline and a txtar file always ends with one. |
| `prepare_release/changesets/invalid_change_file` | The errors are the message and help lines as rendered by `internal/diag`, without miette's graphics and `changesets::could_not_read_changeset` code line. | Spec F3.3, D4. |
| `prepare_release/enable_prerelease`, `override_prerelease_label` | One script per case (`_with_option`, `_with_env`); two more cover `REMERIS_PRERELEASE_LABEL` and the flag beating the environment. The environment variable of the ported ones is still `KNOPE_PRERELEASE_LABEL`. | D1: `REMERIS_*` is read first. |
| `prepare_release/*`, `git_release/*`, `default_workflows/*` git setup | Commits are made with `exec git commit --allow-empty --author 'Knope <knope@example.com>'`, and no file is added to the index before the run, as in Knope's harness. | Same as Knope's `TestCase`; recorded because `git add` of untracked files is part of what the real run does. |
| `--prerelease-label=` (empty) | An empty value counts as no flag: the step's own `prerelease_label`, if any, applies, otherwise the release is stable. Knope uses the empty label and produces the invalid version `1.1.0-.0`. | An empty label cannot make a valid SemVer pre-release. |
| `--verbose` output of PrepareRelease | The debug lines are not matched: remeris logs `Determining new version for default` for an unnamed package and the rule implied by each change even with `--override-version`, and does not log `Only checking commits with scopes`, `Using {RULE} rule to bump from` or `Pre-release label … selected`. | Debug output is not part of the compatibility contract; no fixture checks it. |
| Git errors outside a repository or with no commits | The cause is the `git` command's message (`fatal: not a git repository …`, `fatal: bad revision 'HEAD'`), not libgit2's (`could not find repository at '.'`, `reference 'refs/heads/main' not found`). The message, help and exit code match. | remeris runs the `git` CLI (spec F9). |
| `prepare_release/inconsistent_versions` | The error is the message and help lines as rendered by `internal/diag`, without miette's graphics, code line and wrapping. The dry run and the real run share one `stderr.log`. | Spec F3.3, D4. |
| `prepare_release/verbose` | `Running step PrepareRelease {PrereleaseLabel: AllowEmpty:false IgnoreConventionalCommits:false}` replaces Rust's `Debug` form. The script is skipped until the missing debug lines (`{file} has version {v}`, `Looking for Git tags matching package name.`, `Only checking commits with scopes`, `Using {RULE} rule to bump from`, `Rule is MAJOR, but major component is 0`) are printed. | Spec F3.2 ("debug repr"). |
| `prepare_release/*` and `git_release/*` with `[..]` (commit hashes) | `cmp` is replaced by `rstrip` and an anchored `grep` regex, or by an anchored `stdout` regex (`(?m)`), in which `[..]` becomes `[0-9a-f]+`. | `cmp` has no wildcards. |
| `prepare_release/go_modules_major_versions_v2` | The input `CHANGELOG.md` keeps its literal `[DATE]` for the old `1.1.0` heading; only the new heading has `2026-01-01`. | Knope redacts only its own output. |
| `prepare_release/deno_workspaces` | The trailing space after `"name": "@scope/second-package",` in the input is removed. | The repository's whitespace check. |
| `prepare_release/*` with multi-paragraph commits and merges | `Commit("a\n\nb")` becomes `git commit -m a -m b`; branches and merges use `git switch -c` and `git merge --no-ff --no-edit`; Knope's `sleep(1)` becomes `exec sleep 1`. | A txtar command line cannot hold a newline. |
| `prepare_release/*` and `default_workflows/*` with files Knope lists unchanged in `out/` | `cp X X.orig` before the run and `cmp X X.orig` after it. | txtar has no separate expected directory. |
| `prepare_release/*` with JSON outputs | Where Knope's `out/*.json` has no final newline, the script compares against `printf %s "$(cat out/X)"`. All such files are in skipped scripts (tauri, deno, npm). | Q10: txtar cannot express a missing final newline. |
| `default_workflows/get_version/multi_package` | `stderr.log` is `Error: No workflow named get-version` instead of clap's `error: unrecognized subcommand 'get-version'` with its usage lines. | remeris reports an unknown workflow in its own words (`cli/no_workflow`); the CLI is urfave/cli, not clap, so clap's texts and layout are not reproduced (spec F3.1). |
| Ported scripts with `expected_tags` | Knope compares the sorted `git tag --contains HEAD`; the scripts run that command and `cmp` it with `tags.txt`. | Same check, written as a testscript command. |
| `prepare_release/no_version_change` | The error is the message and help lines as rendered by `internal/diag`, without miette's graphics, `releases::no_release` code line and wrapping. | Spec F3.3, D4. |
| `prepare_release/go_modules_*` | `go.mod` inputs and outputs end with a newline; Knope's files have none. | A txtar file always ends with a newline; remeris keeps the newline it read (as Q10 for JSON). |
| Log line of a step without fields (`--verbose`) | `Running step Release`, as Knope prints a unit variant. Steps with fields keep the Go form (`prepare_release/verbose`). | Spec F3.2. |
| Credentials (`GITHUB_TOKEN`, `GITEA_TOKEN`) | An empty environment variable counts as unset, so the stored token or the prompt is used. Knope uses the empty string as the token. | An empty token never authenticates; falling through is friendlier (spec F15.2). |
| Credentials directory | Credentials are read from `<config>/remeris/`. A value found only in `<config>/knope/` is used and copied into the remeris directory; if the copy fails, the value is still used and the failure is logged at debug level. Knope has one directory. | Rename to remeris (D1) without asking existing Knope users again. |
| `github_release/*` | `[COMMIT]` is matched as 40 hex digits by an anchored `grep` on the output after `rstrip`, as for the other commit hashes. `[DATE]` is `2026-01-01`, also in the input changelogs of `release_assets_*` and `no_previous_tag`, where Knope's input holds the literal `[DATE]`. A final `git tag --list` check shows that the dry run made no tag. | `cmp` has no wildcards; scripts need a fixed date. |
| `prepare_release/changelog_header_level_detection` | No longer skipped: the GitHub release line it expects is printed since M7. The commit is matched as 40 hex digits. | Same as above. |
| Pull request lookup (`$pr_number`, `$pr_author_login`) | The request goes through go-github, which sets its own `X-GitHub-Api-Version`; Knope sends `2026-03-10`. | The client library owns that header (spec F16.4). |
| `create_pull_requests/*` (Quirk Q3) | The dry run prints `Would create or update a pull request from main to main:`; Knope prints `refs/heads/main` because its `current_branch` returns the full ref. A real run sends `head=owner:main` and `"head": "main"` (Knope: `refs/heads/main`). Branches with a `/` keep it (`feature/x`). | Spec F18, Q3: Knope's value is a bug; the short name is what GitHub expects. |
| Gitea `CreatePullRequest` lookup | The open pull requests are listed with `state=open&base_branch={base}&limit=50&page={n}` (at most 20 pages) and filtered on the client: head branch, head repository owner (case-insensitive) and base branch must match. Knope sends `state=open&head=owner:branch&base=base` and updates the first result. | Gitea's list endpoint has no `head` filter, so Knope's request returns every open pull request and it may update an unrelated one (spec F17, F18). |
| Gitea `CreatePullRequest` branch (Quirk Q3) | The dry run, the lookup and the `head` of the new pull request use the full short branch name (`feature/x`). Knope uses the last path segment of the ref (`x`). | Spec F18, Q3: the last segment is wrong for branches with a `/`. |
| `create_pull_requests/gitea_*`, `github_and_gitea` | Our own dry-run scripts: Knope has no Gitea fixture. With both forges the three lines are printed once per forge, as in Knope. | Coverage for M8. |
| Forge detection (F20.6) | A remote with a single `/`-less part such as `git@github.com:r.git` gives no GitHub. Knope computes `parts.len() - 2` on a `usize` there: that panics in a debug build and wraps to "no owner" in a release build, so the release behaviour is kept. Other quirks stay: `https://github.com/r` gives owner `github.com`, and a trailing slash gives an empty repo. | Spec F20.6; the debug panic is not a behaviour to copy. |
| Gitea detection (F17, F20.6): known host | Gitea is detected only when the parsed host of the first remote equals a known public host (`codeberg.org`, case-insensitive, no port). Knope tests `remote.contains("codeberg.org")`, so `https://evil.example/codeberg.org/r` and `https://codeberg.org.evil.example/o/r` give Gitea there. GitHub is checked first and Gitea only when GitHub is not detected (Knope can set both). | A substring match would send the release request, and the token, to a host the remote only mentions. |
| Gitea detection (F17, F20.6): userinfo | The userinfo of the remote is dropped: `https://user:token@codeberg.org/o/r` and `ssh://git@codeberg.org/o/r` give host `https://codeberg.org`. Knope keeps it (`https://user:token@codeberg.org`). | A credential must never reach the configuration, a log line or a request. Other `try_from_remote` behaviour is kept: `http` is forced to `https`, `https://h/o/r/x` gives repo `r/x`, a trailing slash stays in the repo. |
| Forge detection from a subdirectory (no `knope.toml`) | The first remote is read from the repository that contains the working directory, as for every other Git access of remeris. Knope opens `.` only and finds no remote below the repository root. | The Git backends discover the repository upward (spec F9.0); package discovery from a subdirectory usually finds no package anyway. |
| Gitea authentication (`[gitea]` forge) | The token is sent in the header `Authorization: token {token}`. Knope sends it as the `access_token` query parameter. | A token in a URL ends up in logs, proxies and error messages (spec F17, decision). |
| Gitea `host` | The host must have a scheme (`remeris::gitea::invalid_host` on a real run; a dry run prints it as written), use https (http only for loopback hosts), have no userinfo, query, fragment, space or control character, and trailing slashes are trimmed when URLs are built (`https://x/gitea/` and `https://x/gitea` both reach `https://x/gitea/api/v1/…`). A redirect to another host or port, or from https to http, is refused, and `Authorization` is never sent to another origin. Knope accepts any string and follows redirects with the token in the URL. The token is also replaced by `[REDACTED]` in every Gitea error message. | The `host` of a repository's configuration must not make the token leave the instance (spec F17, decision). |
| Gitea release response | Any valid JSON is accepted as the answer to `POST .../releases`. Knope requires an object with `url` and `upload_url` and fails with `Trouble decoding the response from Gitea while creating a release` otherwise. Invalid JSON, or an empty body, is still that error. | Older Gitea versions send no `upload_url`, and remeris does not use the response (spec F16.2). |
| Gitea dry run (`gitea_release/*`, `multi_forge_release/*`, `default_workflows/release/gitea`) | The host in `Would create a release on Gitea [{host}]` is printed as written, except that a user name and password are removed (`https://user:pw@gitea.example.com` prints as `https://gitea.example.com`); a host with `@` that cannot be parsed prints `[REDACTED]`. The fixtures use `https://codeberg.org`, so their output is Knope's. | A dry run does not check the host, and a password in it must not reach a terminal or a log (spec F17). |

## M9 versioned files

Each format's pull request edits only its own section. Rows follow the table above: what differs from Knope and why.

### Cargo.lock

| Knope | remeris | Why |
| --- | --- | --- |
| Edits the `toml_edit` document and prints it back. | Splices the bytes of the `version` value (or appends `version = "..."` to the table), so everything else is untouched, as for Cargo.toml. | Same output as `toml_edit` for the files Cargo writes; byte-exact for hand-edited ones. |
| The unsupported-version warning prints the `toml_edit` item with its decor. | The same text is built from the file's bytes: the whitespace after `=`, the raw value, then the rest of the line (whitespace and comment), so `version = 5` warns `Unsupported version of Cargo.lock:  5. Outcome may be unexpected` (two spaces). | Not confirmed byte for byte against a Knope run (no fixture shows it); the trailing-comment case in particular follows my reading of `toml_edit` and is the one to check. A top-level `[version]` table prints an empty value. |
| Error help says "Knope may not yet support a newer format". | "remeris may not yet support a newer format". | Naming (D1). |

### gleam.toml

| Knope | remeris | Why |
| --- | --- | --- |
| Edits the `toml_edit` document and prints it back. | Splices the bytes of the `version` value, or inserts `version = "..."` after the last top-level pair when it is missing (unreachable after F7.2 validation). | Same as Cargo.toml. |

### package-lock.json

None yet.

### tauri.conf.json

None yet.

### pubspec.yaml

None yet.

### pom.xml

None yet.

### deno.json and deno.jsonc

None yet.

### deno.lock

None yet.
