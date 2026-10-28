# Differences from Knope

remeris reads Knope's `knope.toml`, `.changeset/` files and Git tags, writes the same file edits and changelogs, and aims to be a drop-in replacement for Knope 0.23.1. This page
lists the differences that can matter to you. The complete list, with the reasons, is
[testdata/deviations.md](https://github.com/celest-io/remeris/blob/main/testdata/deviations.md); quirk (Q) and decision (D) numbers refer to the register at the end of
[spec.md](spec.md).

See also [configuration.md](configuration.md) and [usage.md](usage.md).

## Versions and files

- **Full SemVer 2.0.0 (D8, Q2).** Tags and versions such as `1.2.3-rc4`, `1.2.3+build.7` or `1.2.3-alpha.beta.1` are valid, and pre-releases are ordered by SemVer precedence.
    Leading zeros (`01.2.3`) are rejected, where Knope read them as `1.2.3`. Build metadata is kept when a version is written. Bumps keep the `label.N` shape. Rows: "Version
    grammar", "Version ordering", "Version display and equality".
- **`deno.json` and `deno.jsonc` edits keep comments (Q6)**, and `deno.jsonc` is accepted in `versioned_files` (Q11). Section "M9 versioned files".
- **`pom.xml` edits are surgical (Q7).** Only the project version text changes; formatting is kept. Same section.
- **`Cargo.toml` edits keep the whitespace and trailing comment** around the changed value. Row: "`Cargo.toml` edits".
- **Regexes are RE2.** The `regex` of a versioned file uses Go's syntax: `\d`, `\w`, `\s` and `\b` are ASCII-only, and Rust-only syntax such as `(?x)` is an invalid pattern. Row:
    "Regex versioned files".
- **Kept as in Knope:** unknown keys are ignored (Q1), JSON files lose their trailing newline (Q10), change-file package names are not unquoted (Q4), a regex write replaces every
    occurrence of the version text in the match (Q8), tags are annotated with empty messages (Q12), and the npm workspace `changelog` is always set (Q13). `--upgrade` still
    regenerates the whole file (Q9), so remeris prints a warning after it.

## Git, branches and forges

- **Short branch names (Q3).** `CreatePullRequest` and `SelectIssueFromBranch` use `main`, not `refs/heads/main`. Rows: "`create_pull_requests/*` (Quirk Q3)",
    "`SelectIssueFromBranch` (Quirk Q3)".
- **`RebaseBranch` is fast-forward only (D6, Q14).** It fails with `remeris::git::not_fast_forward` when `to` has commits the current branch lacks, where Knope silently dropped
    them. Section "M11 Git branch operations".
- **Change-file front matter is in selection order (Q5)**, not random.
- **`SelectGitHubIssue` works with GitHub Enterprise.** The GraphQL endpoint comes from `api_url`. Section "M11 SelectGitHubIssue".
- **GitHub App authentication (D11).** The order is App credentials (from flags or `REMERIS_GITHUB_APP_*`, never from `knope.toml`), `GITHUB_TOKEN`, the stored token, then a
    prompt. See [usage.md](usage.md).
- **Hardened Gitea and Jira.** The token goes in the `Authorization` header (Knope puts the Gitea token in the URL), `host` and `url` need a scheme and https (http only for
    loopback), redirects to another origin are refused, and tokens are scrubbed from errors. Rows: "Gitea authentication", "Gitea `host`", "Jira authentication", "Jira `url`".
- **Jira queries are quoted.** `status` and `project` are quoted and escaped in the JQL, the issue key is escaped as a path segment, every HTTP status is checked, and the search
    uses `POST /rest/api/3/search/jql` (D5). A configuration written for Knope (`status = "'In Progress'"`) gives the same query. Rows: "Jira JQL and issue key", "Jira HTTP
    status".
- **Forge detection is stricter.** Gitea is detected only for a known host (`codeberg.org`), by its parsed host, and credentials in the remote URL are dropped. Rows: "Gitea
    detection".
- **Credentials.** An empty `GITHUB_TOKEN` or `GITEA_TOKEN` counts as unset. Credentials live in `<config dir>/remeris/`; a value found only in `<config dir>/knope/` is used and
    copied. Rows: "Credentials", "Credentials directory".

## Command line and output

- **Workflow names** must match `^[A-Za-z0-9_-]+$` (Knope accepts any string). Row: `validate/kitchen_sink`.
- **Exit code 1 for every error**, including bad flags and `--override-version` values (clap exits with 2). Related errors are printed as `╰─▶` lines without miette's graphics,
    codes and URLs (D4). Section "--validate".
- **`--help` and usage errors use urfave/cli's layout**, not clap's. Unknown flags and workflows are reported in clap's or remeris' own words, as the rows "`--validate` (or
    `--upgrade`) without a `knope.toml`" and "default_workflows/get_version/multi_package" describe.
- **`--verbose` prints `Running step <Name> {Field:value ...}`** for a step with fields, in Go's form, where Knope prints Rust's `Debug` form. A step without fields prints as in
    Knope (`Running step Release`). Rows: "Log line of a step without fields", "`prepare_release/verbose`".
- **`--prerelease-label=` (empty)** counts as no flag. `REMERIS_PRERELEASE_LABEL` is read before `KNOPE_PRERELEASE_LABEL`.
- **`--generate` and `--upgrade` never destroy a file.** `--generate` refuses to replace a path that exists, and `--upgrade` writes atomically, refuses symbolic links, read-only
    files and files with keys it would drop. Sections "--generate" and "--upgrade".
- **`--dry-run --generate` and `--dry-run --upgrade` write nothing** and print `Would generate a knope.toml file:` or `Would upgrade knope.toml:` followed by the content. Knope
    ignores `--dry-run` there and writes the file.
- **A package discovery error** stops only the workflows that read the packages, not `--help` or a plain `Command` workflow. Row: "Package discovery errors".
- **`[github]` `api_url` and `upload_url`** (GitHub Enterprise Server) are remeris-only keys. Knope ignores them.
