# Remeris: spec for a Go reimplementation of Knope

**Source analysed:** [knope-dev/knope](https://github.com/knope-dev/knope) at `c952077` (2026-09-14), Knope CLI `0.23.0`,
`knope-versioning` `0.8.x`, `knope-config` `0.4.x`, plus the [changesets](https://github.com/knope-dev/changesets) crate it
depends on.

**Scope:**
- **Phase 1 (parity):** rebuild Knope's features in Go, one at a time (milestones M0–M11). Two foundations go beyond
  Knope from the start because the rest builds on them: full SemVer 2.0.0 (F1) and extension hooks for Phase 2.
- **Phase 2 (extensions):** pre-release channels, signed commits (git or GitHub App), more changelog sources, and
  better monorepo support with per-package variables. See [Phase 2](#phase-2-extensions-beyond-knope).

**Goal:** Remeris supports multiple release configurations. The first one is Knope's, and Knope is a peer of the
configurations that follow, not a legacy format.

**Goal of Phase 1:** Rebuild Knope in Go, one feature at a time. On Knope's configuration the Go binary (`remeris`)
should be a **drop-in replacement**: it reads the same `knope.toml`, uses the same `.changeset/` files and Git tag
formats, writes the same file edits and changelog output, and prints the same messages. Each section below is one
unit of work. It lists the behaviour to reproduce, the algorithm (taken from the Rust source, not only the docs),
edge cases, and acceptance criteria.

> Conventions in this document
>
> - **MUST**: required for compatibility with Knope.
> - **Quirk**: surprising Knope behaviour found in the source. Each one says whether to keep it or change it.
> - `path/to/file.rs` refers to the Knope repo. `crates/knope/` is the CLI, `crates/knope-versioning/` is the core
>   versioning library, and `crates/knope-config/` holds the config types shared with Knope Bot.

---

## Table of contents

0. [Architecture and Go project layout](#0-architecture-and-go-project-layout)
1. [Milestone plan (feature order)](#1-milestone-plan-feature-order)
2. [F1: Semantic versions](#f1-semantic-versions)
3. [F2: Config file (`knope.toml`)](#f2-config-file-knopetoml)
4. [F3: CLI, run modes, logging, errors](#f3-cli-run-modes-logging-errors)
5. [F4: Workflows, state and variables](#f4-workflows-state-and-variables)
6. [F5: `Command` step](#f5-command-step)
7. [F6: Versioned files (per format)](#f6-versioned-files-per-format)
8. [F7: Packages and version resolution](#f7-packages-and-version-resolution)
9. [F8: `BumpVersion` step](#f8-bumpversion-step)
10. [F9: Git integration](#f9-git-integration)
11. [F10: Conventional commits](#f10-conventional-commits)
12. [F11: Changesets and the `CreateChangeFile` step](#f11-changesets-and-the-createchangefile-step)
13. [F12: Release notes and changelogs](#f12-release-notes-and-changelogs)
14. [F13: `PrepareRelease` step](#f13-preparerelease-step)
15. [F14: `Release` step (Git tags)](#f14-release-step-git-tags)
16. [F15: HTTP client and credentials](#f15-http-client-and-credentials)
17. [F16: GitHub integration](#f16-github-integration)
18. [F17: Gitea integration](#f17-gitea-integration)
19. [F18: `CreatePullRequest` step](#f18-createpullrequest-step)
20. [F19: Issue steps (GitHub / Gitea / Jira / branch)](#f19-issue-steps)
21. [F20: Default config, auto-detection and `--generate`](#f20-default-config-auto-detection-and---generate)
22. [F21: `--upgrade` and `--validate`](#f21---upgrade-and---validate)
23. [Phase 2: Extensions beyond Knope](#phase-2-extensions-beyond-knope)
24. [Testing strategy](#testing-strategy)
25. [Quirks register](#quirks-register)
26. [Decisions](#decisions)

---

## 0. Architecture and Go project layout

### 0.1 How Knope is built

```
main ─► config.Load() ─► ConfigSource{Default|File|Hybrid}
     ─► build CLI from config (workflows become subcommands)
     ─► create State (packages loaded, versioned files parsed, git tags read)
     ─► run Workflow: for each Step: state = step.Run(RunType[State])
```

- **Core library** (`knope-versioning`): semver, versioned files, packages, changes, release notes, and the
  `Action`s produced. It does **no I/O**: it takes file contents and tag lists as input and returns `Action`s
  (`WriteToFile`, `RemoveFile`, `AddTag`, `CreateRelease`).
- **CLI** (`knope`): config, steps, Git, HTTP forges, prompts. It runs the `Action`s, either for real or as a dry run.
- **RunType**: every step gets `DryRun(state)` or `Real(state)`. A dry run must do **no writes, no network calls
  and no external commands**. It logs what it would have done instead.

Keep this split in Go. It keeps the core easy to unit-test with no filesystem or network.

### 0.2 Proposed Go layout

```
cmd/remeris/main.go            # entry point
internal/app/                  # readykit pattern: GlobalOptions/WorkflowOptions, DefaultXxx(), LoadConfig[T] (F3.1)
internal/cli/                  # command tree built from config, flags, run/validate/generate/upgrade
internal/config/               # knope.toml loading, defaults, discovery, serialisation (generate/upgrade)
internal/workflow/             # Workflow, Step (tagged union), runner, State, RunType
internal/steps/                # one file per step
internal/variables/            # Template + Variable substitution
internal/git/                  # Git interface + implementation
internal/forge/github/         # go-github client, auth (token or GitHub App), releases, PRs, issues
internal/forge/gitea/          # Gitea client on net/http (leaf): header auth, hardened host, releases, PRs, issues
internal/issues/jira/
internal/httpx/                # client with retry/backoff/rate-limit logging, ApiRequestError
internal/credentials/          # token/email store in user config dir, prompting
internal/prompt/               # select, multiselect, text, password
internal/fsx/                  # dry-run aware write/remove/mkdir
internal/signing/              # Phase 2 (E2): signed commits/tags via git or the GitHub API
pkg/versioning/                # PURE core (the knope-versioning equivalent)
    semver/                    # full SemVer 2.0.0 Version, Rule, PackageVersions
    changes/                   # Change, ChangeType, Source interface (E3)
    changes/conventional/      # conventional commit parsing (a Source)
    changesets/                # .changeset file format (the changesets crate equivalent)
    releasenotes/              # Sections, ChangeTemplate, Changelog, Release
    versionedfile/             # one file per format + Config/Format
    package.go                 # Package, ChangeConfig, apply changes → []Action
    action.go                  # Action, ReleaseTag
```

### 0.3 Go version and dependencies

**Go version:** `go 1.27` in `go.mod` (the latest toolchain is 1.27.1). **Decided (2026-10-02):** Go 1.27 is the
minimum from day one. Go 1.27 is the first release where
`encoding/json/v2` and `encoding/json/jsontext` are generally available, and the spec relies on them. It also
covers `(?<name>…)` regex groups (Go ≥ 1.22) and `hujson`'s minimum of Go 1.26.

**How dependencies were chosen:** prefer the standard library. Add a third-party module only when it has had a
release in the last ~12 months and is not archived or marked unmaintained. CLI and config use the libraries chosen
by the project: **urfave/cli v3** and **koanf v2**. Versions were checked against `proxy.golang.org` on 2026-10-02.

| Need | Knope uses | Go choice | Latest release (checked 2026-10-02) | Notes |
| --- | --- | --- | --- | --- |
| CLI | clap | **`github.com/urfave/cli/v3`** | v3.14.0, 2026-10-02 | Workflows become `[]*cli.Command`, built at runtime from the config. See F3.1. |
| CLI flags → typed options | — | **`github.com/knadh/koanf/providers/cliflagv3`** | v1.1.1, 2026-03-20 | The readykit pattern (D9): flags are loaded into koanf and unmarshalled into option structs. See F3.1. |
| Config loading | `toml` (serde) | **`github.com/knadh/koanf/v2`** + `providers/file` + `parsers/toml/v2` | v2.3.7, 2026-09-24 (toml parser v2.2.2, 2026-08-04) | Decodes with `go-viper/mapstructure/v2`. koanf loses key order and byte positions, so it is paired with a TOML index. See F2.4. |
| TOML index (key order + spans) and **format-preserving edits** | `toml_edit`, serde `Spanned` | `github.com/pelletier/go-toml/v2/unstable` (already a dependency through koanf's TOML parser) | go-toml v2.4.3, 2026-07-05 | No maintained Go module offers a `toml_edit` equivalent. Write a small span-based editor on this parser. **MUST** keep comments and formatting in `Cargo.toml`, `Cargo.lock`, `gleam.toml`, `pyproject.toml`. See F6. |
| TOML encode (`--generate`, `--upgrade`) | `toml` | Hand-written emitter (the schema is small) | n/a | Golden output needs exact control over quoting and inline tables (F20.7). go-toml/v2 `Marshal` is only used in tests, to check the output round-trips. |
| JSON with key order kept | `serde_json` + `preserve_order` | **stdlib** `encoding/json/jsontext` (Go 1.27) | stdlib | Edit `package.json`, `package-lock.json`, `tauri*.conf.json` and `deno.lock` as a token stream, or as a small ordered tree built from `jsontext.Decoder`. Members keep their original order. Output: `jsontext.Value.Indent("", "  ")`, no trailing newline (matches serde `to_string_pretty`). Set `jsontext.AllowDuplicateNames(true)` to accept the same input serde does. v2 does not escape `<>&` by default, which matches serde. Replaces `iancoleman/orderedmap` (no release since 2023). |
| JSONC | `jsonc-parser` | `github.com/tailscale/hujson` | pseudo-version 2026-07-27 (no tags; actively committed, needs Go 1.26) | Parse `deno.json(c)`. `hujson` keeps comments, so the version can be edited in place by patching the value. This fixes Quirk Q6. |
| YAML | `serde_yaml` | **`go.yaml.in/yaml/v3`** | v3.0.5, 2026-07-26 | The YAML org's maintained continuation of `gopkg.in/yaml.v3`, whose README now says "THIS PROJECT IS UNMAINTAINED". Drop-in API. Only used to read `version` and for the rare whole-file fallback. Considered: `github.com/goccy/go-yaml` (v1.19.2, 2026-01). |
| XML | `xmltree` | **stdlib** `encoding/xml` | stdlib | Use `Decoder.Token` + `InputOffset()` to find the byte range of the direct-child `<version>` text, then patch that range in place (fixes Quirk Q7). |
| Regex | `regex` | **stdlib** `regexp` | stdlib | RE2 semantics in both, so behaviour matches. |
| Glob | `glob` | **stdlib** `io/fs` (`fs.WalkDir` + `path.Match` per segment, with `**` handled by the walker) | stdlib | About 60 lines. `filepath.Glob` alone has no `**`. Fallback if edge cases pile up: `github.com/bmatcuk/doublestar/v4` (v4.10.2, 2026-09-20). |
| Prompts | `inquire` | **`charm.land/huh/v2`** | v2.0.3, 2026-03-10 (needs Go 1.25.8, built on `charm.land/bubbletea/v2` v2.0.10, 2026-09-24) | Select, MultiSelect with a "select at least one" validator, Input, and Input in password mode. Replaces `AlecAivazis/survey` (no release since 2022). Put it behind `internal/prompt` so tests can script answers. |
| Git | `git2` (libgit2) | **stdlib** `os/exec` calling the `git` CLI, with **`github.com/go-git/go-git/v5`** as the fallback when `git` is not installed (D2, F9.0) | go-git v5.19.2, 2026-07-29 | The CLI gives exact behaviour, including rebase. go-git has no rebase, so the fallback supports only fast-forward for `RebaseBranch`. v6 is still alpha. |
| HTTP + retry | reqwest-middleware + reessaie | **stdlib** `net/http` with a retrying `RoundTripper` (also used as go-github's transport) | stdlib | About 80 lines: exponential backoff with jitter, 5 retries, honours `Retry-After` (seconds or HTTP date), logs rate-limit warnings (F15.1). Considered: `hashicorp/go-retryablehttp` (last release v0.7.8, 2025-06). |
| GitHub API | reqwest (hand-written) | **`github.com/google/go-github/v92`** | v92.0.0, 2026-09-14 (needs Go 1.26) | All GitHub calls (D10, F16.0). Also handles the asset upload URL, so no URI-template library is needed. |
| GitHub App auth | — | **`github.com/jferrl/go-githubauth`** | v1.9.1, 2026-09-19 (needs Go 1.26; uses `golang-jwt/jwt/v5`, `x/oauth2`) | App JWT → installation token as an `oauth2.TokenSource` (F16.1, E2). Rejected: `bradleyfalzon/ghinstallation/v2` (v2.19.0) because it pins `go-github/v88` and `jwt/v4`, which would add a second go-github major. |
| Commit signing (go-git fallback, E2) | — | go-git's signing support (OpenPGP via `github.com/ProtonMail/go-crypto` v1.5.2, 2026-09-25, already a go-git dependency) | — | Only for the go-git backend. The `git` CLI backend uses the user's own git signing config. |
| Command splitting | `execute` | Hand-written tokenizer in `internal/steps/command` | n/a | About 50 lines: whitespace separates words, `'…'` and `"…"` quote, backslash escapes, **no** `$VAR` expansion (matches Knope). Rejected: `google/shlex` (no change since 2019); `mvdan.cc/sh/v3/shell.Fields` (expands variables); `mattn/go-shellwords` (recent tag, but `go 1.13` module and adds little). |
| Base64 (Jira auth) | `base64` | **stdlib** `encoding/base64` | stdlib | |
| Logging | tracing | **stdlib** `log/slog` with a custom plain handler | stdlib | No level, time or target prefix. See F3. |
| Diagnostics | miette | Custom `Diagnostic` interface: `Code()`, `Help()`, `URL()`, optional source span | n/a | See F3. |
| User config dir | `dirs` | **stdlib** `os.UserConfigDir()` | stdlib | Same locations as Rust `dirs::config_dir()`. |
| Dates | `time` | **stdlib** `time` (`time.Now().UTC().Format(time.DateOnly)`) | stdlib | |
| Conventional commits | `git-conventional` | Hand-written parser in `pkg/versioning/changes` | n/a | Must match `git-conventional`'s grammar exactly (F10.1). Considered: `github.com/leodido/go-conventionalcommits` (v0.13.0, 2026-07). Its grammar and footer rules differ, so it would need adapting anyway. |
| Semver | own | Own type in `pkg/versioning/semver` | n/a | Full SemVer 2.0.0 (D8, F1). Own type because the bump logic needs Knope's `(label, counter)` view of pre-releases and exact control of precedence and errors; about 200 lines plus tests. |
| CLI golden tests | snapbox | **`github.com/rogpeppe/go-internal/testscript`** | v1.16.0, 2026-07-01 (needs Go 1.25) | Runs the binary against `.txtar` scripts. See Testing strategy. |
| Test diffs | pretty_assertions | `github.com/google/go-cmp` | v0.7.0, 2025-01 | Stable, maintained by Google. Optional: stdlib `testing` is enough for most tests. |

**Removed from the earlier draft:** `spf13/cobra` (replaced by urfave/cli), `iancoleman/orderedmap` (stdlib
jsontext), `gopkg.in/yaml.v3` (unmaintained), `bmatcuk/doublestar` (stdlib, kept as fallback), `google/shlex`
(dead), `hashicorp/go-retryablehttp` (stdlib), `charmbracelet/huh` v1 (moved to `charm.land/huh/v2`).

---

## 1. Milestone plan (feature order)

Each milestone ships something users can run, and its tests pass on its own. Later milestones only add to earlier ones.

| # | Milestone | Features | Knope user value unlocked |
| --- | --- | --- | --- |
| M0 | Skeleton | F3 (CLI, logging, errors, dry-run), F2 (parse config, workflows only), F4, F5 | `remeris <workflow>` runs `Command` steps |
| M1 | Semver core | F1 | Library only, fully unit-tested |
| M2 | Versioned files v1 | F6: `Cargo.toml`, `package.json`, `pyproject.toml`, `go.mod`, regex | Read and write versions |
| M3 | Packages + BumpVersion | F7, F8, `$version` variable, F9 (read tags) | `BumpVersion` with Major/Minor/Patch/Pre/Release, `--override-version` |
| M4 | Changes | F10, F11 (parse + `CreateChangeFile`), F9 (commit walk) | `document-change` workflow |
| M5 | PrepareRelease | F12, F13, F9 (stage files) | Changelog and version bump from commits and changesets |
| M6 | Release (git) | F14, F9 (create tag), default workflows (F20 partial) | Full offline release loop, zero-config mode |
| M7 | GitHub | F15, F16 (releases, assets), F18 (GitHub), PR-info enrichment | GitHub release flow |
| M8 | Gitea | F17, F18 (Gitea) | Codeberg/Gitea flow |
| M9 | Remaining formats | F6: `Cargo.lock`, `gleam.toml`, `package-lock.json`, `pubspec.yaml`, `pom.xml`, `tauri*.conf.json`, `deno.json(c)`, `deno.lock` | Full format parity |
| M10 | Discovery + tooling | F20 (Cargo/npm/Deno workspaces, `--generate`), F21 (`--upgrade`, `--validate`) | Zero-config monorepos |
| M11 | Issues | F19 (GitHub/Gitea/Jira issues, SwitchBranches, RebaseBranch, SelectIssueFromBranch, TransitionJiraIssue), `IssueBranch` variable | Issue-driven dev workflows |

Phase 2 milestones (P2.1–P2.6) are listed at the end of [Phase 2](#phase-2-extensions-beyond-knope).

---

## F1: Semantic versions

Source: `knope-versioning/src/semver/{mod,rule,package_versions,prerelease_map}.rs`

### F1.1 Version grammar: full SemVer 2.0.0 (decision D8)

Knope accepts only a subset of SemVer (`M.m.p` or `M.m.p-label.N`). Remeris implements the full
[SemVer 2.0.0](https://semver.org/spec/v2.0.0.html) grammar from the start, because it is the base for the
pre-release channel work (E1).

```
version     := core ["-" prerelease] ["+" build]
core        := numeric "." numeric "." numeric            ; uint64, no leading zeros ("0" is fine)
prerelease  := pre-id *("." pre-id)
pre-id      := numeric | alphanumeric                     ; numeric: no leading zeros
alphanumeric:= 1*[0-9A-Za-z-] containing at least one non-digit
build       := build-id *("." build-id)
build-id    := 1*[0-9A-Za-z-]                             ; leading zeros allowed
```

No `v` prefix in a Version (tag prefixes such as `v` / `name/v` are stripped before parsing, F7.5).
Error message stays Knope's: `Found invalid semantic version {detail}`, help `The version must be a valid Semantic Version`.

```go
type Version struct {
    Major, Minor, Patch uint64
    Pre   []PreID   // empty = stable
    Build []string  // kept for display and writing; ignored for precedence
}
type PreID struct { Str string; Num uint64; IsNum bool }

func Parse(s string) (Version, error)
func (v Version) String() string           // canonical form, including build metadata
func Compare(a, b Version) int             // SemVer §11 precedence, build ignored
func (v Version) Equal(o Version) bool     // identical, including build
func (v Version) IsPrerelease() bool
```

**Precedence (SemVer §11):** compare major, minor and patch numerically. A version with a pre-release is lower
than the same core without one. Pre-release identifiers are compared left to right: two numeric ids compare
numerically, two alphanumeric ids compare in ASCII order, numeric is lower than alphanumeric, and if all shared
ids are equal the longer list is higher. Build metadata is ignored.

Which comparison to use where:
- Ordering, `Latest()`, "is the tag already released" (F14), bump calculations → `Compare`.
- The "inconsistent versions" check across a package's files (F7.2) → `Equal`, so `1.2.3` vs `1.2.3+b1` is
  reported (Knope could not express build metadata at all).

**Knope's pre-release model on top of SemVer.** Knope's bumping logic needs a `(label, counter)` pair. Any
pre-release is mapped onto that pair like this:

| Pre-release | label | counter |
| --- | --- | --- |
| `rc.4` | `rc` | 4 |
| `alpha.beta.2` | `alpha.beta` | 2 |
| `rc4`, `beta` | `rc4`, `beta` | none |
| `1` (single numeric id) | `""` | 1 |

The rule: if there are at least 2 identifiers and the last one is numeric, it is the counter and the rest
(joined with `.`) is the label. Otherwise the whole pre-release is the label and there is no counter. Versions
produced by a bump are always `core-label.N` with no build metadata.

**Differences from Knope (record in `testdata/deviations.md`):**
- Accepted now, rejected by Knope: `1.2.3-rc4`, `1.2.3-rc.4.5`, `1.2.3+build.7`, `1.2.3-alpha.beta.1`. Tags like
  these were silently skipped by Knope and now count as releases.
- Rejected now, accepted by Knope: leading zeros (`01.2.3` → Knope read it as `1.2.3`; `1.2.3-rc.01`).
- Ordering between pre-releases follows SemVer precedence instead of Knope's "label string, then number" (Quirk
  Q2). For the `label.N` shape both give the same order, so only exotic versions are affected.

Display: `String()` prints the canonical form, so `1.2.3+build` keeps its build metadata when written to files.

### F1.2 Rules

```go
type StableRule int // Patch < Minor < Major (ordering used for "max")
type Rule struct {
    Kind       RuleKind   // Stable | Pre | Release
    Stable     StableRule // for Kind==Stable, and the base rule for Kind==Pre
    Label      string     // for Kind==Pre
}
```

`StableRule` from a list of changes = the **max** over all changes of: Breaking→Major, Feature→Minor,
Fix/Custom→Patch. It is `Patch` when there are no changes.

`bump_stable(v, rule)`:
| rule | major ≥ 1 | major == 0 |
| --- | --- | --- |
| Major | `M+1.0.0` | `0.m+1.0` |
| Minor | `M.m+1.0` | `0.m.p+1` |
| Patch | `M.m.p+1` | `0.m.p+1` |

### F1.3 `PackageVersions`: the version history of a package

State: `stable *Core` and `prereleases map[Core]map[Label]Version` (Core = major.minor.patch; Label as defined in F1.1). Keep both maps **ordered by
key** (Go: sort keys when needed). There is always at least one label per entry.

**`FromTags(prefix *string, tags []string)`**: tags must already be in **newest-first order** (see F9.3).
- The pattern is `"v"`, or `"{prefix}/v"` when a prefix is given. Only tags starting with the pattern are used.
- For each matching tag, remove the pattern (first occurrence only) and try to parse a Version (skip it on error):
  - Stable: set `stable` and **stop** (only pre-releases newer than the latest stable matter).
  - Pre: `update(version)`.

**`update(version)`**:
- Stable `new`: if `stable >= new` do nothing. Otherwise set `stable = new` and **clear all pre-releases**.
- Pre `(s, pre)`: if a pre with the same `s` and label already exists and is `>= pre` (by `Compare`), do nothing.
  Otherwise insert or overwrite `prereleases[s][label(pre)] = pre`.

**`Latest()`**: if there are pre-releases, take the **largest** stable key and return the highest-precedence
pre-release in its map (`Compare`; Knope used the largest label string). Otherwise return `stable`. Otherwise return nothing.

**`CalculateNewVersion(rule)`**:
| rule | stable exists | result |
| --- | --- | --- |
| Stable(r) | yes | `bump_stable(stable, r)` |
| Stable(r) | no | largest pre-release stable key **as-is**, else `0.0.0` |
| Pre{label, r} | any | `calculatePre` (below) |
| Release | any | largest pre-release stable key, else error `PreReleaseNotFound` (`No prerelease version found, but a Release rule was requested`) |

`calculatePre(label, r)`:
1. `base = bump_stable(stable, r)` if stable exists, else the largest pre-release stable key, else `0.0.0`.
2. `n = counter + 1` if `prereleases[base][label]` exists and has a counter; `0` if it exists without a counter
   (so `1.2.4-beta` is followed by `1.2.4-beta.0`, which is higher by SemVer precedence); else `0`.
3. Return `base-label.n`.

Examples to test (from the docs and code):
- `1.2.3`, Pre(rc, Patch) → `1.2.4-rc.0`. Then with `1.2.4-rc.0` tagged → `1.2.4-rc.1`.
- Stable `1.2.3`, tags `1.2.4-rc.0`, then a Minor change with Pre(alpha) → `1.3.0-alpha.0`.
- `0.1.2` + Major → `0.2.0`. `0.2.0` + Minor → `0.2.1`.
- BumpVersion `Release` on `1.2.3-rc.4` → `1.2.3`.

### Acceptance (F1)
- Port every unit test in `semver/*.rs` and `package_versions.rs` as a Go table test (adjusting the few that
  expect Knope's subset errors, see F1.1).
- Add SemVer 2.0.0 conformance tests: the valid/invalid examples and the precedence chain from the spec
  (`1.0.0-alpha < 1.0.0-alpha.1 < 1.0.0-alpha.beta < 1.0.0-beta < 1.0.0-beta.2 < 1.0.0-beta.11 < 1.0.0-rc.1 < 1.0.0`),
  plus a fuzz test that `Parse(v.String())` round-trips.

---

## F2: Config file (`knope.toml`)

Sources: `crates/knope/src/config/{mod,toml,package}.rs`, `crates/knope-config/src/*`

### F2.1 Top-level schema

```toml
[changes]                    # optional
ignore_conventional_commits = false

[release_notes]              # optional
change_templates = ["..."]   # array of strings (F12.3)

[package]                    # EITHER [package] ...
# or
[packages."<name>"]          # ... OR one or more [packages.<name>]. Both at once is an error.

[[workflows]]                # optional (defaults apply when absent, F20)
name = "release"             # required, unique name = CLI subcommand
help_text = "..."            # optional
[[workflows.steps]]
type = "<StepName>"          # tagged union, F4.2

[github]  owner = "", repo = ""
[gitea]   owner = "", repo = "", host = "https://codeberg.org"
[jira]    url = "https://x.atlassian.net", project = "PRJ"
```

Unknown keys: Knope's serde structs do **not** use `deny_unknown_fields`, so **unknown keys are ignored**. Copy this.

### F2.2 Package schema

| key | type | default | notes |
| --- | --- | --- | --- |
| `versioned_files` | array of `string` \| `{path, dependency}` \| `{path, regex}` | `[]` | `regex` may be a string or an array of strings. `dependency` and `regex` together is an error (`Cannot specify both 'dependency' and 'regex' for the same file: {path}`). |
| `changelog` | relative path | none | If the file is missing it is treated as empty and created on write. |
| `scopes` | `[]string` | none (= all scopes) | |
| `extra_changelog_sections` | `[{name, footers?, types?}]` | `[]` | F12.1 |
| `assets` | `string` (glob) \| `[{path, name?}]` | none | GitHub only |
| `ignore_go_major_versioning` | bool | false | |

Validation when loading (in this order):
1. `[package]` and `[packages]` both present → `You cannot define both 'packages' and 'package'`, with source-span
   labels on both definitions. `[packages]` present but empty → `The 'packages' key is defined but does not contain any packages`.
2. Each versioned file: `regex` present → Format = RegexFile. Otherwise the **file name** (last path component,
   case-sensitive) must be one of the supported names (F6.1), else `Unknown file: {path}` (help lists the supported
   formats). The file **must exist** on disk, else `File {path} does not exist` with the span labelled
   `Declared here`.
3. `[gitea]` present and any package has `assets` → `Asset uploads for Gitea are not supported`.

Package names: `[package]` gives the **Default** name (shown as `default`, no tag prefix).
`[packages.X]` gives `Custom(X)`. Keep the order packages are declared in (use an ordered map).

### F2.3 Config sources

```
Load():
  if knope.toml can't be read → ConfigSource.Default(generate())         # F20
  else parse → ConfigSource.File(cfg) → fillInGaps():
      if cfg.packages empty  → cfg.packages = findInWorkingDir()          # F20
      if cfg.workflows empty → cfg.workflows = defaultWorkflows(hasForge, packages)
      → ConfigSource.Hybrid(cfg)
```

Forges are **never** auto-detected when `knope.toml` exists. The source type decides which CLI flags exist (F3).

### F2.4 Loading with koanf (Go design)

koanf does the loading and decoding. Two of its defaults don't fit `knope.toml`, so a TOML index fills the gaps.

1. **Key delimiter.** koanf splits keys on its delimiter (`.` by default). Package names (`[packages."foo.bar"]`,
   `[packages."@scope/pkg.js"]`) and `variables` keys (such as `"{version}"`, or anything with a dot) can contain
   dots. Create the instance with a delimiter that can't appear in TOML keys:
   `koanf.NewWithConf(koanf.Conf{Delim: "\x1f", StrictMerge: true})`.
2. **Order.** `parsers/toml/v2` decodes into `map[string]any`, so the declaration order of `[packages.*]` and
   `variables` is lost. Both orders matter (package order drives action and tag order, and variable order drives
   replacement order, F4.4). Read the original bytes a second time with `go-toml/v2/unstable` and build a
   `tomlIndex` that holds:
   - the ordered keys of `packages` and of every `variables` table, keyed by their TOML path;
   - the byte span of every table and array element (`package`, `packages.X`, `versioned_files[i]`, `workflows[i]`),
     used for diagnostics (F2.2, F3.3).

   After decoding, re-order the `packages` and `variables` maps using the index.
3. **Decoding.** `k.UnmarshalWithConf("", &raw, koanf.UnmarshalConf{Tag: "toml", DecoderConfig: …})` with
   `mapstructure` decode hooks for the untagged and tagged unions:
   - `versioned_files[]`: string → `{path}`; table → `{path, dependency}` or `{path, regex}`, where `regex` is a
     string or `[]string`;
   - `assets`: string (glob) or a list of `{path, name?}`;
   - `workflows[].steps[]`: switch on `type` into the concrete step struct (F4.2), and `BumpVersion` on `rule`;
   - `Variable` values: exactly `Version` | `ChangelogEntry` | `IssueBranch`.

   Leave `ErrorUnused` off, so unknown keys are ignored as in Knope (Quirk Q1). Decode errors are wrapped as
   diagnostics, with spans looked up in the index.
4. **Layers.** Only one source, `file.Provider("knope.toml")`, is needed for parity. Defaults for Hybrid configs
   (F2.3) are applied to the decoded structs, not merged as koanf layers, because "missing" means "detect from the
   working directory", not a fixed value. Extra layers later (for example `remeris.toml` overrides or `REMERIS_*` env
   vars) are where koanf pays off (see D1: `knope.toml` is the only file for now).

If `knope.toml` is missing, skip koanf entirely and go to `ConfigSource.Default` (F20).

### Acceptance (F2)
- Fixtures: `tests/prepare_release/{invalid_versioned_files,missing_versioned_files,unknown_versioned_file_format}`, and the `package`/`packages` conflict.

---

## F3: CLI, run modes, logging, errors

Source: `crates/knope/src/main.rs`

### F3.1 Command line

```
remeris [--dry-run] [-v|--verbose] [--generate | --upgrade | --validate] [<workflow> [workflow flags]]
remeris --help | --version
```

| Flag | Available when | Behaviour |
| --- | --- | --- |
| `--dry-run` (global) | always | Run the workflow in DryRun mode. |
| `-v`, `--verbose` (global) | always | Log level DEBUG instead of INFO. |
| `--generate` | **no** `knope.toml` | Log `Generating a knope.toml file`, write the generated config (F20), exit. |
| `--upgrade` | `knope.toml` exists | F21 |
| `--validate` | `knope.toml` exists | F21. All log output is suppressed while validating. |
| `--override-version` (workflow flag) | workflow has `BumpVersion` or `PrepareRelease` **and** ≥1 package | With 1 package: single `<version>`, parsed with F1. With ≥2 packages: repeatable `name=version`. Unknown names → error `Unknown package(s) to override: a, b`. Bad format → `package override should be formatted like package_name=version`. |
| `--prerelease-label` (workflow flag, env `REMERIS_PRERELEASE_LABEL` or `KNOPE_PRERELEASE_LABEL`, the first one set wins) | workflow has `PrepareRelease` | Sets `prerelease_label` on **every** `PrepareRelease` step in **every** workflow. The flag wins over the env var. |

- Each workflow is a subcommand, and `help_text` is its description.
- No subcommand (and no `--validate`) → error `No workflow selected. Run 'remeris --help' for a list of options.`
- Unknown workflow names are rejected by the CLI parser. Knope also has a fallback `No workflow named {name}`.
- `--help` on the root lists workflows. `<workflow> --help` shows the flags for that workflow.

**urfave/cli v3 + koanf: the readykit pattern (decision D9).** Reuse the structure of
`celest-io/readykit` (`cmd/readykit/main.go`, `pkg/app/config.go`):

1. **Typed options struct with defaults.** Runtime options live in plain structs with `mapstructure` tags whose
   names match the flag names, plus a constructor for defaults:
   ```go
   // internal/app/options.go
   type GlobalOptions struct {
       DryRun   bool   `mapstructure:"dry-run"`
       Verbose  bool   `mapstructure:"verbose"`
       Generate bool   `mapstructure:"generate"`
       Upgrade  bool   `mapstructure:"upgrade"`
       Validate bool   `mapstructure:"validate"`
       // GitHub App auth (E2), never read from knope.toml:
       GitHubAppID          int64  `mapstructure:"github-app-id"`
       GitHubInstallationID int64  `mapstructure:"github-installation-id"`
       GitHubAppKeyPath     string `mapstructure:"github-app-private-key-path"`
   }
   func DefaultGlobalOptions() *GlobalOptions { return &GlobalOptions{} }

   type WorkflowOptions struct {
       OverrideVersion []string `mapstructure:"override-version"`  // "1.2.3" or "pkg=1.2.3"
       PrereleaseLabel string   `mapstructure:"prerelease-label"`
   }
   ```
2. **Flags declare their env vars** through `Sources: cli.EnvVars(...)`, as in readykit:
   `--prerelease-label` → `cli.EnvVars("REMERIS_PRERELEASE_LABEL", "KNOPE_PRERELEASE_LABEL")`,
   `--github-app-id` → `REMERIS_GITHUB_APP_ID`, `--github-installation-id` → `REMERIS_GITHUB_INSTALLATION_ID`,
   `--github-app-private-key-path` → `REMERIS_GITHUB_APP_PRIVATE_KEY_PATH`.
   The private key itself can also come from `REMERIS_GITHUB_APP_PRIVATE_KEY` (PEM content, for CI secrets); that one is
   read directly, never through a flag, so it doesn't show up in `--help` defaults or process listings.
   The flag wins over the env vars (urfave's precedence).
3. **One generic loader**, copied from readykit's `app.LoadConfig` with one fix:
   ```go
   // internal/app/config.go
   func LoadConfig[T any](cmd *cli.Command, cfg *T) error {
       k := koanf.New(".")
       if err := k.Load(cliflagv3.Provider(cmd, "."), nil); err != nil { return err }
       return k.UnmarshalWithConf(commandPath(cmd), cfg, koanf.UnmarshalConf{Tag: "mapstructure"})
   }
   // commandPath returns "remeris" or "remeris.release": cliflagv3 keys every flag by the
   // command lineage, so readykit's cmd.Name prefix only works for the root command.
   func commandPath(cmd *cli.Command) string { /* join Lineage() names root→cmd with "." */ }
   ```
   Only flags that were set (on the command line or through their env var) reach koanf, so values not set keep the
   defaults from the constructor. Workflow names are validated against `^[A-Za-z0-9_-]+$` at config load
   time, because a `.` in a workflow name would break the koanf key path. Knope allows any string, so this is a
   small deviation; the error is `Workflow names may only contain letters, digits, '-' and '_': {name}`.
4. **Command tree.** As in readykit, `main` builds a root `cli.Command` with `Name: "remeris"`,
   `Version: version.Full()`, `Suggest: true`, `EnableShellCompletion: true`, root `Flags`, and a `Before` hook that
   runs `LoadConfig(cmd, globalOpts)` and sets up `slog` (verbose → debug level). Command constructors take the
   shared state as an argument and close over it (`workflowCommand(cfg, w) *cli.Command`), like readykit's
   `daemonCommand(cfg)`. Each workflow `Action` runs `LoadConfig(cmd, &WorkflowOptions{})` and then the workflow.
5. **Project config is loaded before the tree is built**, because workflows become subcommands. `knope.toml` uses
   its own koanf instance (F2.4, file provider + TOML parser + TOML index). It is not merged with the CLI koanf
   instance: CLI options and project config have different shapes and lifetimes.
6. Flag details:
   - `--dry-run` and `--verbose` (alias `v`) are root `BoolFlag`s. v3 root flags are persistent by default, so
     `remeris release --dry-run` works, as in clap's `global(true)`.
   - `--generate`, `--upgrade` and `--validate` are root `BoolFlag`s with `Local: true`, added only for the matching
     ConfigSource. The root `Action` handles them, and errors with `No workflow selected…` when none is set.
   - `--override-version`: `StringSliceFlag` in both cases. With one package, exactly one bare `<version>` is allowed.
     With several, each value must be `name=version`.
   - `root.Version` gives `--version`. Help formatting differs from clap, so golden tests don't compare `--help`.
7. **Errors.** Readykit logs `slog.Error` and exits 1. Remeris instead renders the `Diagnostic` (F3.3) to stderr and
   exits 1, because diagnostics have help text and nested errors that a log line would lose. Steps never call
   `cli.Exit`.

> **Decision (D1):** the program is `remeris`: binary name, `--help`, log and error text (for example
> `Run 'remeris --help'`), diagnostic codes (`remeris::inconsistent_versions`), `User-Agent: Remeris` and the
> credential directory (F15.2). Knope's release configuration is the first of several supported ones, not a legacy
> format, so it keeps the names its own format defines: the file `knope.toml` (read and written by
> `--generate`/`--upgrade`) and `KNOPE_PRERELEASE_LABEL`. The code that implements a configuration owns its names
> (the file name lives in `internal/config`); there is no global name package. A setting that is also useful to other
> configurations gets `REMERIS_*` as its own name, which takes precedence. Golden logs vendored from Knope are
> rewritten `knope` → `remeris` where the text names the program; `knope.toml` itself is left alone.

### F3.2 Logging

- Plain lines with no timestamp, level or target. INFO by default, DEBUG with `--verbose`.
- Only messages from the app itself are shown (Knope filters to targets starting with `knope`).
- Knope's `tracing-subscriber` fmt layer writes to **stdout**. Warnings (`warn!`) also go to stdout, with no prefix.
- Diagnostics (errors) go to **stderr**. Exit code 1 on error.

### F3.3 Error rendering

Knope uses miette. Each error has a message, and optionally a code (such as `knope_versioning::inconsistent_versions`),
help text, a docs URL, a source snippet with labelled spans (config errors), and related/nested errors (workflow
errors wrap step errors). Go design:

```go
type Diagnostic interface {
    error
    Code() string
    Help() string
    URL() string
    Labels() []Label            // {Span [start,end), Text}, rendered against SourceCode()
    SourceCode() string
    Related() []error
}
```

Workflow failure wraps the step error as `Problem with workflow {name}`. Validation collects them as
`There are problems with the defined workflows`, listing all.

**Decision (D4):** v1 renders the **message** and **help text** only (plus nested related errors), matching
Knope's wording with `knope` → `remeris`. No source snippets, no docs URL: the Knope URLs point at knope.tech and
the remeris docs will be written later. Keep `URL()`, `Labels()` and `SourceCode()` on the interface (and keep the
spans in the TOML index, F2.4) so snippets and links can be turned on later without touching every error.

Rendering:
```
Error: {message}
  help: {help}
  ╰─▶ {related message}      # one line per nested error, indented per level
```
Golden `stderr.log` files from Knope contain miette's ASCII graphics. The fixture conversion script reduces them to
the message and help lines, and tests compare only those.

---

## F4: Workflows, state and variables

Sources: `workflow.rs`, `state.rs`, `step/mod.rs`, `variables.rs`, `knope-config/src/template.rs`

### F4.1 Runner

```go
func Run(w Workflow, s RunType[State]) error {
    for _, step := range w.Steps { s, err = step.Run(s); if err != nil { return WorkflowError{w.Name, err} } }
}
```
Steps run in order. Stop at the first error. State moves from step to step.

### F4.2 Steps (TOML `type = "..."`)

| type | fields | Feature |
| --- | --- | --- |
| `Command` | `command` (string), `variables` (ordered map string→Variable, optional), `shell` (bool, optional) | F5 |
| `BumpVersion` | `rule` = `Major` \| `Minor` \| `Patch` \| `Pre` \| `Release`, plus `label` for `Pre` | F8 |
| `PrepareRelease` | `prerelease_label?`, `allow_empty` (false), `ignore_conventional_commits` (false, deprecated) | F13 |
| `Release` | none | F14 |
| `CreateChangeFile` | none | F11 |
| `CreatePullRequest` | `base`, `title = {template, variables?}`, `body = {template, variables?}` | F18 |
| `SelectJiraIssue` | `status` | F19 |
| `TransitionJiraIssue` | `status` | F19 |
| `SelectGitHubIssue` | `labels?` ([]string) | F19 |
| `SelectGiteaIssue` | `labels?` ([]string) | F19 |
| `SelectIssueFromBranch` | none | F19 |
| `SwitchBranches` | none | F19 |
| `RebaseBranch` | `to` | F19 |

> The docs examples show `label = "selected"` for `SelectGitHubIssue`. The code field is `labels` (an array), and
> unknown keys are ignored, so the docs example is effectively "no filter". Copy the code.

In verbose mode, log `Running step {debug repr}` before each step.

### F4.3 State

```go
type State struct {
    JiraConfig   *JiraConfig
    GitHubConfig *GitHubConfig; GitHub GitHubAuthState   // New | Authenticated{token,client} | Unauthenticated{client}
    GiteaConfig  *GiteaConfig;  Gitea  GiteaAuthState    // New | Initialized{token,client}
    Issue        *Issue                                  // nil = Initial
    Packages     []*Package
    AllVersionedFiles []versionedfile.File                // parsed ONCE at startup, shared by packages
    PendingActions    []Action                            // produced by PrepareRelease, consumed by Release
    AllGitTags        []string                            // newest-first, current branch only (F9.3)
    IgnoreConventionalCommits bool                        // from [changes]
}
```

Git tags are read **only when there is at least one package**. If reading fails, the tag list is empty.

### F4.4 Templates and variables

A `Template` is `{template string, variables ordered-map[string]Variable}`. Default variables, used **only when the
`variables` key is absent**, are `{"$version": Version, "$changelog": ChangelogEntry}`. `variables = {}` turns
substitution off.

`replace(template)`: for each `(key, var)` **in declared order**, skip it if `key` does not occur in the current
string (so unused, invalid variables never error). Otherwise look up the value and do `strings.ReplaceAll`. Later
replacements can change text produced by earlier ones. Copy this.

Variables:
- **`Version`**: needs exactly one package (`Too many packages defined` if more, `No packages to operate on` if none).
  Value = `package.LatestVersion()`, else error `Could not determine version of package`.
- **`ChangelogEntry`**: same single-package rule. If `PendingActions` has a `CreateRelease`, use the first one's
  `notes`. Otherwise use `latestVersion` and find that release in the changelog (F12.5). If that fails:
  `Could not find a changelog entry for version {v}`.
- **`IssueBranch`**: needs a selected issue (`No issue selected`). Value = `branchNameFromIssue(issue)` (F19.6).

Phase 2 hook: implement lookup as `Resolve(v Variable, scope *PackageScope)`. Phase 1 always passes `nil`, which
gives the single-package rules above. E4 passes a package for qualified variables and `for_each` loops.

---

## F5: `Command` step

Source: `step/command.rs`

1. Substitute variables (F4.4) into `command`.
2. Dry run: log `Would run {command}` and return.
3. If `shell == true`: run through the platform shell (`sh -c "<cmd>"` on Unix, `cmd /C "<cmd>"` on Windows).
   Otherwise split the string into argv with the hand-written tokenizer (whitespace separates words, `'…'` and `"…"`
   quote, backslash escapes, no `$VAR` expansion; see 0.3) and run it directly (no shell).
4. Inherit stdin, stdout and stderr, and the working directory.
5. Non-zero exit → `Command returned non-zero exit code`. Spawn failure → `I/O error: {err}`.

Default `release` workflow command: `git commit -m "chore: prepare release $version"`. Argv splitting must give
`["git","commit","-m","chore: prepare release 1.2.3"]`.

### Acceptance (F5)
- Fixtures `tests/command/*` (variables, shell mode, default variables, disabled defaults).

---

## F6: Versioned files (per format)

Source: `knope-versioning/src/versioned_file/*.rs`

### F6.1 Common model

```go
type Format int // Cargo, CargoLock, DenoJson, DenoLock, Gleam, GoMod, PackageJson, PackageLockJson,
                // MavenPom, PubSpec, PyProject, TauriConf, RegexFile

// Supported file names, matched EXACTLY on the last path component. This order is also the order
// used by default detection (F20).
var FileNames = []struct{ Name string; Format Format }{
  {"Cargo.toml", Cargo}, {"Cargo.lock", CargoLock}, {"deno.json", DenoJson}, {"deno.lock", DenoLock},
  {"gleam.toml", Gleam}, {"go.mod", GoMod}, {"package.json", PackageJson},
  {"package-lock.json", PackageLockJson}, {"pom.xml", MavenPom}, {"pubspec.yaml", PubSpec},
  {"pyproject.toml", PyProject}, {"tauri.conf.json", TauriConf}, {"tauri.macos.conf.json", TauriConf},
  {"tauri.windows.conf.json", TauriConf}, {"tauri.linux.conf.json", TauriConf},
}

type Config struct { Path string; Format Format; Dependency *string; Regex []string }

type File interface {
    Path() string
    Version() (semver.Version, error)            // ErrNoVersion for lockfiles
    SetVersion(v semver.Version, dependency *string, gv GoVersioning) (File, error)
    Write() []Action                             // nil if nothing changed
}
```

> `deno.jsonc` is **not** in the name table, so it cannot be used as an explicit `versioned_files` entry. It is only
> reached through Deno workspace discovery, which keeps the real config path (F20.4). Treat this as a Knope bug:
> add `deno.jsonc` → DenoJson to the table.

**Parse once, edit many times.** Every file in every package is parsed once at startup into
`State.AllVersionedFiles`. `SetVersion` is applied once for **each** config entry that matches the path, in
package order and then config order. So one file can hold its own version and dependency versions for several
packages (see the docs example with `package.json` listed twice). The diff strings from each edit are joined with
`", "`.

`Action.WriteToFile{Path, Content, Diff}`: `Diff` is what dry run prints (`Would add the following to {path}: {diff}`).

### F6.2 Format details

For each format: **read** (where the version comes from), **write** (what is edited and how formatting is kept),
**diff** string, and **dependency** support.

#### `Cargo.toml` (TOML, keep formatting)
- Read: `package.version` (string). If absent, `workspace.package.version`. Else error
  `{path} was missing required property 'package.version' or 'workspace.package.version'`.
  Error `Invalid TOML in {path}` on parse failure.
- Write (no dependency): if `package.version` exists **and is a string**, set it. Otherwise (for example
  `version.workspace = true`), if `workspace.package.version` exists, set that. Diff `version = {v}`.
- Write (dependency `d`): in each of `dependencies.d`, `dev-dependencies.d` and `workspace.dependencies.d`: if the
  entry is a table (`[dependencies.d]`) → set/insert `version`. If it is an inline table → set/insert `version`. If
  it is a plain value (`d = "1.0"`) → replace it with the version string. Diff `{d}.version = {v}`.
- Helpers: `NameFromDocument` returns `package.name`. `ContainsDependency(doc, d)` checks any of the three tables.

#### `Cargo.lock` (TOML, keep formatting)
- Read: none (`This file can't contain a version`). It must have a dependency (F7.2 fills in a default).
- Warn if the top-level `version` is not 3 or 4 (`Unsupported version of Cargo.lock: …` / `Unknown version of Cargo.lock…`).
- Write: for each `[[package]]` with `name == d`, set/insert `version`. Diff `{name} = {v}`, joined with `", "`.
- Errors: missing `[[package]]` array, or a package without a string `name`.

#### `gleam.toml`
- Read top-level `version`. Write/insert top-level `version`. Diff `version = {v}`. No dependency support.

#### `pyproject.toml`
- Read `project.version` (PEP 621) and `tool.poetry.version`. If both exist they must be equal
  (`Found conflicting versions {project} and {poetry} in {path}`). If neither: `No versions were found in {path}`.
- Write: **replace the text between the quotes** at each version's byte span. Every other byte is kept.
  Diff = `{v}` (version only).

#### `go.mod`
- Parse the first line starting with `module `: `module <path>[/vN] [// v<full-version>]`. `major` = N when the
  path's last segment is `vN`.
- Read: if the module comment has a version, use it. Otherwise take it from **git tags**:
  - `parent` = the directory of go.mod. If `major` is set and parent ends in `v{major}`, drop that segment
    (major-version subdirectory).
  - Tag prefix = `"{parent}/"`, or `""` at the repo root. Candidates = tags with that prefix and then `v`, parsed
    as Version, with major ∈ `{major}` if set, else `{0,1}`. Take the **first** match in tag order (newest first).
    None → `No matching tag found for the go.mod file. Searched for a tag with the prefix {prefix} and a major version of {filter}`.
- Write with `GoVersioning`:
  - Set the comment version to the new version.
  - If `newMajor > 1 && newMajor != (major or 0) && mode != IgnoreMajorRules`:
    - If the module has no `/vN` and mode != BumpMajor → `Will not bump Go modules to 2.0.0`.
    - If the old major is set and parent ends with `v{oldMajor}` → `Cannot bump major versions of directory-based go modules`.
    - Otherwise set `/v{newMajor}` on the module path.
  - Rebuild the line: `module {path}` + (`/v{N}` if N>1) + ` // v{version}`. Replace the original line in the raw text.
- `Write()` returns **two** actions: `WriteToFile` (diff = version) **and** `AddTag`:
  `tag = "{prefix}/v{version}"`, where prefix = parent dir without a trailing `v{major}` and `/`. At the root it is
  `v{version}`.
- `GoVersioning`: `Standard` (default), `IgnoreMajorRules` (package `ignore_go_major_versioning = true`), and
  `BumpMajor` (used when the version is **overridden**).

#### `package.json`
- Read: top-level `version` (must parse, else a deserialise error). Help:
  `knope expects the package.json file to be an object with a top level 'version' property`.
- Write (no dependency): parse the raw text into an **ordered** map and set `version` (inserted at the end if
  missing). Re-serialise pretty with 2 spaces, **no trailing newline**. Diff `version = {v}`.
- Write (dependency `d`): in `dependencies` and then `devDependencies`, if `d` is a string value:
  - `workspace:<prefix><digits…>` → `workspace:<prefix>{v}`, where prefix is everything before the first ASCII digit
    (such as `^` or `~`). `workspace:*`, `workspace:^` and other values with no digit are left unchanged.
  - Any other string → `{v}`.
  - Only when the value changes: re-serialise and add diff `{d} = {newValue}`.
- Diffs are joined with `", "`.

#### `package-lock.json`
- Read top-level `version` (``{path} did not have a `version` field``, help ``Either add a `version` field or specify a `dependency` to update``).
- Warn if `lockfileVersion` is not 2 or 3.
- Write (no dependency): set top-level `version` and `packages[""].version`. Diff `version = {v}`.
- Write (dependency `d`): for every value in `packages`: if `name == d`, set `version`. If `dependencies[d]` or
  `devDependencies[d]` exists, set it to `{v}`. Diff `{d} = {v}` (always added).
- Serialise pretty with 2 spaces, no trailing newline.

#### `deno.json` / `deno.jsonc`
- Parse as JSONC. Read `version` (missing or invalid → no version).
- Write (no dependency): Knope sets `version` and re-serialises the whole file as plain pretty JSON. **Quirk Q6:**
  comments and formatting are lost, even though the docs say they are kept. **Decision:** with `hujson`, patch only
  the `version` value (or insert the member) and keep everything else, as the docs promise. Update the Deno fixtures
  and record the deviation. Diff `version = {v}`.
- Write with a dependency: **no-op** (deno.lock owns dependency versions).

#### `deno.lock`
- Only lockfile `"version": "5"` is supported. Versions 1–4 or missing → `Lockfile version {v} in {path} requires npm package metadata which is not supported yet`.
  Other values → `Unsupported lockfile version {v} in {path}`.
- Write needs dependency `d`. Algorithm:
  1. For each `specifiers` entry whose key parses as a JSR/npm package request (`jsr:@scope/name@<req>` or
     `npm:name@<req>`) with name `d`: record the old resolved value and the old req text, record the kind
     (jsr/npm), and set the value to `{v}`.
  2. None updated → no-op.
  3. For each kind seen, rename keys in the `jsr` / `npm` section: `name@old` → `name@{v}` when the old version is
     in the recorded set.
  4. In `workspace` (and each `workspace.members.*`, and each key of `workspace.links`, including renaming the link
     key): rewrite entries in `dependencies`, `packageJson.dependencies`, `optionalDependencies` and
     `peerDependencies` that match `d` with a recorded version, to `{scheme:}{d}@{v}`.
  5. Diff `{d} = {v}`. Serialise like deno_lockfile 0.49.0 `print_v5_content` (`printer.rs`, called from
     `Lockfile::as_json_string` in `lib.rs`): 2-space pretty with a trailing newline, the top-level keys in the fixed
     order `version`, `specifiers`, `jsr`, `npm`, `redirects`, `remote`, `workspace`, empty sections left out, and the
     maps sorted: `specifiers` and the dependency arrays by the normalised requirement text
     (`JsrDepPackageReq::to_string_normalized`: `^1.0.0` prints as `1`, see deno_semver 0.9.1 `range.rs`), `jsr` by
     `PackageNv` order (name, then version precedence), `npm`, `members` and `links` by key text. Two more printer rules (`printer.rs` 183-271): a `jsr` package's dependency
     prints as bare `scheme:name` when that name has a single specifier in `specifiers`, otherwise as the normalised
     requirement; an `npm` dependency prints as `name@npm:pkg@ver` for an alias and as the bare name when the package
     has a single version (the `npm` dependency arrays are in the order of the dependency names, not of the printed
     text; a name used in both registries counts as having several specifiers); an `npm` package's `os` and `cpu` arrays
     print on one line. `JsrDepPackageReq::cmp` is not the print order. The Go port of the
     requirement and version types is `pkg/versioning/internal/denosemver`, and the lockfile model, reader and printer
     are `pkg/versioning/internal/denolock`. Check against fixtures
     `tests/prepare_release/deno`.

#### `pubspec.yaml`
- Read top-level `version` (YAML).
- Write: replace only the bytes of the top-level `version` scalar (found from the position yaml.v3 reports for the
  first top-level `version` key), so comments, quote style, line endings and nested `version:` keys stay. Quoted values
  keep their quotes. A block scalar, anchor, tag, escaped or multi-line value fails with a diagnostic and is not
  written. The file is never re-serialised: a mapping with no `version` key (unreachable, since loading requires the
  key) gets a `version: {v}` line appended. Knope replaces the whole line, everywhere it occurs. Diff `{v}`,
  replacing any earlier diff.

#### `pom.xml`
- The root element must be `<project>` (`{path} was missing required property project`). Read `project > version` text.
- Write: set the text of `<version>`, or insert `<version>` right after `<artifactId>` (or at the end). Diff
  `project.version = {v}`.
- Knope re-emits the whole XML tree with indentation and no XML declaration (**Quirk Q7**, it loses the original
  formatting). **Decision:** do a *surgical* replacement of the direct-child `<version>` text in Go. This is better
  behaviour. Refresh the fixtures to match, and record it as an intended deviation.

#### `tauri.conf.json` (+ `.macos`, `.windows`, `.linux` variants)
- Read top-level `version`. Write: ordered-map set `version`, pretty, no trailing newline. Diff `{v}`.

#### Regex files (`{ path, regex }`)
- Each pattern must compile (`Invalid regex pattern '{re}' for {path}: {err}`) and must contain the named group
  `version` (`Regex pattern '{re}' must contain at least one named capture group called 'version'`).
- Read: **every** pattern must match (`Regex '{re}' did not match any content in {path}`). The captured `version` of
  each must parse as a Version (`Matched version '{v}' in {path} is not a valid semantic version`). The file's
  version is the **first** pattern's match.
- Write: apply each pattern in turn with replace-all. For each match, take the full match text and replace
  **every occurrence** of the captured version substring *inside the match* with `{v}` (Quirk Q8: copy this).
- Diff = the new lines that changed, compared line by line (zip old and new lines), trimmed and joined with `\n`.
  No changes → no write.

### F6.3 Dependency support matrix

Allowed with `dependency`: Cargo.toml, package.json, package-lock.json, deno.json (no-op), deno.lock, Cargo.lock
(required). All others → `Dependencies are not supported in {file_name} files`.

### Acceptance (F6)
- Port the unit tests in each `versioned_file/*.rs`.
- Fixtures: `tests/prepare_release/{cargo,npm,deno,gleam_toml,go_modules,pubspec_yaml,pyproject_toml,tauri_conf_json,readme_version}`, `tests/bump_version/*`.

---

## F7: Packages and version resolution

Sources: `knope-versioning/src/package.rs`, `crates/knope/src/step/releases/package.rs`

### F7.1 Loading

```
AllVersionedFiles = for each package, for each versioned_file config: read the file, then versionedfile.New(config, content, gitTags)
for each package:
    validated, versionFromFiles = validateVersionedFiles(pkg.configs, AllVersionedFiles)
    versions = PackageVersions.FromTags(pkg.name.customOrNil, gitTags)
    if versionFromFiles != nil { versions.update(versionFromFiles) }
    releaseNotes = {Sections: convertSections(pkg.extra_changelog_sections),
                    Changelog: loadChangelog(pkg.changelog), ChangeTemplates: cfg.release_notes.change_templates}
    goVersioning = IgnoreMajorRules if pkg.ignore_go_major_versioning else Standard
```

Note: the same path listed in two packages is parsed **twice** into `AllVersionedFiles` (once per entry). Edits
still apply by path through the `SetVersion` chaining.

### F7.2 `validateVersionedFiles`
For each config, in order:
1. Find the parsed file with the same path (the error `Versioned file not found` means a bug).
2. Dependency validation:
   - Cargo.lock with no dependency: default it to the `package.name` of the first non-dependency `Cargo.toml` in the
     same package. None → `Cargo.lock must specify a dependency`.
   - Dependency on an unsupported format → error (F6.3).
3. Dependency entries don't count toward the package version. For the rest, `file.Version()` must equal the
   first-seen version, else:
   `Found inconsistent versions in package: {p1} had {v1} and {p2} had {v2}`
   (code `remeris::inconsistent_versions`, help `All files in a package must have the same version`).
4. Return the validated configs and the first version (if any).

### F7.3 Package version
`LatestVersion()` = `versions.Latest()`. So a package's "current version" is the newest of: files version, latest
stable tag, and pre-release tags after it.

### F7.4 Setting a version
```
SetVersion(version, goVersioning, allFiles):
    for each file in allFiles:
        for each config in pkg.configs where config.Path == file.Path(): file = file.SetVersion(version, config.Dependency, goVersioning)
    pkg.versions = PackageVersions.From(version)   // resets history to just this version
```

### F7.5 Tag format (`ReleaseTag`)
- Default package: `v{version}`. Named package: `{name}/v{version}`.
- `IsReleaseTag(tag, pkg)` = the tag has the package prefix and the rest parses as a Version.

---

## F8: `BumpVersion` step

Source: `step/releases/semver.rs`

TOML: `rule = "Major"|"Minor"|"Patch"|"Release"` or `rule = "Pre"` with `label = "rc"`.
`Pre` maps to `Rule{Pre, label, stable: Patch}`.

```
for each package:
    if pkg.overrideVersion != nil { v, gv = *override, BumpMajor }
    else { v = pkg.versions.CalculateNewVersion(rule); gv = pkg.goVersioning }   // error PreReleaseNotFound for Release w/o pre
    AllVersionedFiles = pkg.SetVersion(v, gv, AllVersionedFiles)
actions = flatten(file.Write() for file in AllVersionedFiles)   // drained
execute(actions, stageToGit=false)   // F13.4, AddTag/CreateRelease are ignored here
```

No package → nothing happens (no error). `BumpVersion` does **not** stage files to git and does **not** write a
changelog.

### Acceptance (F8)
- Fixtures `tests/bump_version/*`.

---

## F9: Git integration

Source: `integrations/git.rs`. Define an interface so it can be faked in tests:

```go
type Git interface {
    AllTagsOnBranch() ([]string, error)
    CommitMessagesAfterTag(tag *string) ([]Commit, error)
    FirstCommitsForFiles(paths []string) map[string]*GitInfo
    AddFiles(paths []string) error
    CreateTag(name string) error         // lightweight? see F9.5
    HeadCommitSHA() (string, error)
    CurrentBranch() (string, error)
    FirstRemoteURL() (string, bool)
    // F19:
    LocalBranches() ([]string, error); BranchExists(name string) bool
    CreateBranch(name, from string) error; Checkout(name string) error
    HasUncommittedChanges() (bool, error); Rebase(current, to string) error   // F19.8
}
```

### F9.0 Backends (decision D2)

Two implementations of the `Git` interface, chosen once at startup:

1. **`git` CLI (primary):** used when `exec.LookPath("git")` succeeds. Every method runs `git` through `os/exec` with
   `-C <cwd>`, `GIT_TERMINAL_PROMPT=0` and `LC_ALL=C` (stable output to parse).
2. **go-git fallback:** `github.com/go-git/go-git/v5` (v5.19.2, 2026-07-29) when `git` is not on `PATH`. Log at debug:
   `git not found on PATH, using built-in Git support`.

| Method | go-git fallback |
| --- | --- |
| `AllTagsOnBranch`, `CommitMessagesAfterTag`, `FirstCommitsForFiles`, `HeadCommitSHA`, `CurrentBranch`, `FirstRemoteURL`, `LocalBranches`, `BranchExists` | Full support (`Repository.Tags`, `Log`, `Tree.File`, `Head`, `Remotes`, `Branches`) |
| `AddFiles` | `Worktree.Add` per path; `Worktree.Remove` for deleted paths |
| `CreateTag` | `Repository.CreateTag` with `CreateTagOptions{Message: "", Tagger: …}`. The tagger comes from git config (`user.name`/`user.email`, read with `config.LoadConfig(GlobalScope)` + repo config), else the same `Could not determine Git committer…` error |
| `CreateBranch`, `Checkout`, `HasUncommittedChanges` | `Repository.CreateBranch` + `Storer.SetReference`, `Worktree.Checkout{Force: true}`, `Worktree.Status` (respects `.gitignore`) |
| `Rebase` | **Fast-forward only** (go-git has no rebase). If `to` is an ancestor of HEAD, move `to` to HEAD and check it out. Otherwise fail with `RebaseBranch needs the git command when the branches have diverged` (help: `Install git, or rebase manually.`) |

Both backends must pass the same contract test suite (`internal/git/contract_test.go`), run against temporary
repositories. The go-git run sets `PATH` to an empty dir to force the fallback.

### F9.1 `AllTagsOnBranch`
1. Map commit OID → the tag names pointing at it (tags peeled to commits, both annotated and lightweight), in
   reference-iteration order.
2. Walk from HEAD using libgit2's default revwalk order (**reverse chronological by commit time**; Knope sets no
   sorting). For each commit, append its tags.
3. Tags not reachable from HEAD are dropped (logged at debug: `Skipping relevant tags that are not on the current branch: …`).
Result: tags **newest first**. `git log --format=%H` from HEAD, joined with `git for-each-ref refs/tags --format='%(objectname) %(*objectname) %(refname:strip=2)'`, gives the same result.

### F9.2 `CommitMessagesAfterTag(tag)`
- If the tag is given and exists: collect **all ancestors** of the tag's commit (including it) into an exclude set.
  If it is missing, log at debug and use all commits.
- Walk from HEAD. Keep the commits not in the exclude set. Record the full message and `GitInfo{hash: first 7 hex chars, author name}`.
- **Reverse** the list so it is oldest first.
- Equivalent to `git log --reverse <tag>..HEAD`.

Which tag is used: `PackageVersions.FromTags(pkg).stable` turned back into a tag (`v1.2.3` / `name/v1.2.3`). That is
the **last stable** release. Pre-releases don't reset the commit window, so `1.3.0-rc.1` → final `1.3.0` includes
every change since `1.2.x`.

### F9.3 `FirstCommitsForFiles(paths)`
Walk commits topologically **in reverse** (oldest first). For each commit, for each path not found yet: if the
commit's tree has the path, record `GitInfo` from that commit. Paths never found → nil. Equivalent to
`git log --diff-filter=A --format=... -- path` (take the oldest). Errors are swallowed and give an empty result.

### F9.4 `AddFiles` = `git add` of all paths (the index is written). No-op when empty. Paths that were removed must
be staged as deletions too (libgit2 `add_all` does this for tracked deleted files; with the CLI use `git add -A -- <paths>`).

### F9.5 `CreateTag(name)`
Knope calls `repo.tag(name, HEAD, signature, "", force=false)`. That makes an **annotated tag with an empty
message**, using the configured committer (`user.name` and `user.email` must be set, else
`Could not determine Git committer…`). Go: `git tag -a <name> -m ""` (use `--cleanup=verbatim` so an empty message is
allowed). It fails if the tag already exists.

### F9.6 Misc
- `HeadCommitSHA`: full 40-char SHA.
- `CurrentBranch`: fails with `Not on the tip of a Git branch.` when HEAD is detached. **Quirk Q3:** Knope returns
  the full ref name (`refs/heads/main`). See F18 and F19 for where this leaks.
- `FirstRemoteURL`: URL of the first remote (`git remote` order).

---

## F10: Conventional commits

Sources: `knope-versioning/src/changes/{mod,conventional_commit}.rs`. Knope uses the `git-conventional` crate. Copy
its grammar.

### F10.1 Parsing (on `strings.TrimSpace(message)`)
```
first line := type ["(" scope ")"] ["!"] ": " description
type       := 1+ chars, not whitespace, "(", ")", ":" or "!"   (git-conventional: a "noun")
body       := optional paragraphs after a blank line
footers    := the final paragraph, if EVERY line in it is a footer:
              token ": " value   |   token " #" value
              token := "BREAKING CHANGE" | "BREAKING-CHANGE" | [A-Za-z0-9-]+  (no spaces)
              a value may continue onto following lines that are not footers
```
- Messages that don't parse are **not changes at all**. Their footers are ignored as well.
- `breaking` = `!` present OR any footer token is `BREAKING CHANGE` / `BREAKING-CHANGE`.
- Type comparison (`feat`, `fix`) and footer-token comparison are **case-insensitive** (unicase).
- Pick a Go library carefully, or hand-write the parser. Test it against `git-conventional`'s own test cases.

### F10.2 From commit to changes
Inputs: commits (oldest first), package `scopes`, and `sections` (F12.1).
1. If the commit has a scope and `scopes` is set: skip the commit unless some scope matches, compared **ASCII
   case-insensitively**. Commits with no scope always count.
2. For each footer:
   - Breaking footer → `Change{Breaking, summary: footer.value}` and remember `hasBreakingFooter`.
   - Else if some section lists this footer token (case-insensitive) → `Change{Custom(Footer token), value}`.
   - Else skip it.
3. Description change: `Breaking` if `breaking && !hasBreakingFooter`. Else `Feature` if the type is `feat`. Else
   `Fix` if the type is `fix`. Else no description change. Summary = description.
4. The footer changes come **before** the description change in the output.
5. Every change gets `GitInfo` from the commit and `ChangeSource.ConventionalCommit{description}`. Here
   `description = "{type}{(scope)}{!}: {desc}"` (the `!` only if breaking came from `!` and not from a footer).
   Footer changes get `"{summary}\n\tContaining footer {token}{sep} {value}"`. These are used in verbose logs:
   `{source}\n\timplies rule {MAJOR|MINOR|PATCH}`.

Note: custom **types** (such as `docs:` listed in `extra_changelog_sections.types`) do **not** create changes from
commits. They only apply to change files. Only custom **footers** apply to commits.

### Acceptance (F10)
- Port the unit tests in `conventional_commit.rs`. Fixtures `tests/prepare_release/{scopes,verbose,...}`.

---

## F11: Changesets and the `CreateChangeFile` step

Sources: the changesets crate (`change.rs`, `changeset.rs`, `versioning.rs`), `step/create_change_file.rs`

### F11.1 Change file format (`.changeset/<id>.md`)
```markdown
---
<package-name>: <change-type>
<package-name>: <change-type>
---

<summary: markdown, usually starting with "# Title">
```
Parsing:
1. The first line trimmed must be `---` (`MissingFrontMatter`).
2. Then lines until one that trims to `---`: each is `name: type` (split on the **first** `:`, trim both sides).
   A line without `:` → `InvalidFrontMatter`. Names are used as written: **no quote stripping**. Duplicate names:
   the last one wins (it is a map). An empty map → `Versioning needs to contain at least one item.`
3. The closing `---` must be there.
4. Summary = the rest, with leading blank lines skipped, joined with `\n`.
5. Change type: `patch` | `minor` | `major`, anything else is `Custom(s)` (case-sensitive).
6. `unique_id` = the file name without `.md`. Only regular files ending in `.md` directly in `.changeset/` are read.

`ChangeSet.FromDirectory`: group by package name into `Release{package, changes}` (in the order first seen). Within a
package, **sort changes by unique_id** (plain string compare, so uppercase comes before lowercase).

Map to a package: find the release whose `package_name == pkg.name` (`default` for the single `[package]`).

### F11.2 Change file → `Change`
- `summary` = the first non-empty line of the trimmed summary, with leading `#` and space characters removed.
- `details` = the remaining lines, leading blank lines skipped, joined with `\n`. Empty → nil.
- Type: major → Breaking, minor → Feature, patch → Fix, custom → `Custom(ChangeType(s))`.
- GitInfo: from `FirstCommitsForFiles(".changeset/<id>.md")`. It can be nil when the file is uncommitted.
- Source: `ChangeFile{id}` (displayed as `changeset {id}.md`).

### F11.3 `UniqueId.normalize(summary)`
For each character: ASCII alphanumeric → lowercase it. `' '`, `'_'` or `'-'` → `_`, but only if the previous
output character was not `_` from this rule. Anything else is dropped (and does not reset the underscore flag).
Example: `` `[i carry your heart with me(i carry it in]` `` → `i_carry_your_heart_with_mei_carry_it_in`.

### F11.4 `CreateChangeFile` step
1. Dry run → log `Would create a new change file` and return.
2. Packages: if there is exactly 1, use it. Otherwise MultiSelect `Which packages does this change affect?`
   (at least one: `At least one package must be selected`).
3. For each chosen package, Select a change type from the package's sections, **in section order**, keeping only
   the sources that map to a changeset type (Breaking → `major`, Feature → `minor`, Fix → `patch`, custom type →
   the name; footers are left out). Prompt: `What type of change is this for {name}?`, or
   `What type of change is this?` for the default package.
4. Text prompt `What is a short summary of this change?`, help `This will be used as a header in the changelog`.
5. Create `.changeset/` if it is missing. Write `.changeset/{normalize(summary)}.md` with this content:
   ```
   ---
   {pkg}: {type}      # one line per package (Knope: HashMap order, i.e. random; Go: use selection order)
   ---

   # {summary}
   ```
   (the file ends with a newline). Overwrites a file with the same name if one exists.

---

## F12: Release notes and changelogs

Sources: `knope-versioning/src/release_notes/*`, `knope-config/src/changelog_section.rs`

### F12.1 Sections
Defaults, in order:
| name | source |
| --- | --- |
| `Breaking Changes` | Breaking |
| `Features` | Feature |
| `Fixes` | Fix |
| `Notes` | footer `Changelog-Note` |

`convertSections(extra)`:
```
defaults := the default list above
sections := []
for each {name, footers, types} in extra (in order):
    sources := footers (as Custom(Footer)) followed by types (as Custom(Type), where major/minor/patch map to the built-ins)
    remove from defaults any entry whose source is in sources          // override
    while a default with the same name exists: add its source to sources, remove it   // merge by name
    sections = append(sections, (name, sources))
return remaining defaults (in original order) + sections
```
So custom sections always come **after** the remaining defaults. Overriding a default source moves it to the new
section.

### F12.2 Rendering a release (`CreateRelease`)
```
notes := ""
for (name, sources) in sections:
    cs := changes whose type is in sources, STABLE-sorted so that no-details comes before has-details
    if len(cs)==0: continue
    if notes != "": notes += "\n\n"
    notes += "## " + name + "\n\n"
    for i, c in cs:
        writeChange(c)
        if next exists: notes += "\n" if next has no details else "\n\n"
title := "{version} ({YYYY-MM-DD, UTC today})"
```
The notes are at "H1 level": sections use `##` and complex changes use `###`. Input change order: commit changes
(oldest commit first), **then** changeset changes (sorted by id).

`writeChange`: try each template in order (F12.3). If none applies, use the default: complex →
`### {summary}\n\n{details}`, simple → `- {summary}`.

### F12.3 Change templates (`[release_notes].change_templates`)
Variables: `$summary`, `$details`, `$commit_author_name`, `$commit_hash`, `$pr_number`, `$pr_author_login`.
A template **applies** only if every variable it contains can be filled:
- `$commit_author_name` / `$commit_hash` need GitInfo.
- `$pr_author_login` / `$pr_number` need the PR fields (F16.4).
- `$details` needs details.
Substitution order: author/hash → pr login → pr number → details → summary (plain replace-all).
Templates that don't apply are skipped. The first one that applies wins.

Forge-data check: if any template contains `$pr_author_login` or `$pr_number` (checked in that order), then
PrepareRelease needs `[github]` (F13.2).

### F12.4 Changelog writing
`Changelog{path, content, releaseHeaderLevel}`:
- `releaseHeaderLevel`: take the **second** line starting with `#`. If it starts with `##` → H2, else if it starts
  with `#` → H1. If there is no second header → H2. (The first `#` line is assumed to be the title.)
- `WithRelease(release)`:
  ```
  body := release.notes, with one extra "#" added to each line starting with "#" when the level is H2
  new  := "{level} {title}\n\n{body}"
  insert `new + "\n\n"` right before the first line that parses as a release title (F12.5 parseTitle)
  (checks every line, header level ignored); if there is none, append `new` at the end
  keep the trailing newline: if the original ended with "\n" or was empty, make sure the output ends with "\n"
  ```
  Each original line is copied with `\n` added (so CRLF becomes LF). Returns `new` as the diff, and the action is
  `WriteToFile{diff: "\n" + new + "\n"}`.

### F12.5 Reading a release from a changelog (`GetRelease(version)`)
- `parseTitle(line)`: whitespace tokens. tok0 ∈ {`#`, `##`}, tok1 = a Version. Other tokens may contain a `(YYYY-MM-DD)` date.
- Scan for a line starting with `"{level} {version}"` that parses with the same level and an equal version.
  Title = the line with leading `#` removed, trimmed (for example `1.2.3 (2023-01-01)`).
- Notes = the following lines up to (not including) the next line starting with `"{level} "`. For H2 changelogs,
  remove one `#` from lines starting with `##`. Join with `\n`. Empty → not found.

### F12.6 Actions produced for a package release
`[WriteToFile(changelog) if changelog is configured, CreateRelease(Release{title, version, notes, package})]`.

---

## F13: `PrepareRelease` step

Source: `step/releases/mod.rs::prepare_release`, `package.rs`

```
if no packages → error "No packages to operate on"   (code package::no_defined_packages)
needsForge := first template variable needing forge data (F12.3)
changeset  := ChangeSet.FromDirectory(".changeset") if the dir exists, else empty   // parse errors → CouldNotReadChangeSet
for pkg in packages:
    changes := gatherChanges(pkg)
    if len(changes)>0 && needsForge != "":
        if no [github] → error "You must configure [github] to use the variable {var} in a change template"
        enrich via GitHub (F16.4); on error only warn "Failed to enrich git info from GitHub: {e}"
    if len(changes)==0 && pkg.override == nil: continue      // no actions for this package
    cfg := Force(override) (consumes it) or Calculate{prerelease_label, pkg.goVersioning}
    version := Force → override ; Calculate → versions.CalculateNewVersion(Pre{label, maxRule} or Stable(maxRule))
    AllVersionedFiles = pkg.SetVersion(version, gv, AllVersionedFiles)
    actions += RemoveFile(".changeset/{id}.md") for each change-file change, ONLY IF version is NOT a pre-release
    actions += releaseNotes.CreateRelease(version, changes, pkg.name)          // F12.6 (mutates changelog content)
    PendingActions += actions
all := dedupe(flatten(file.Write() for file in AllVersionedFiles) ++ PendingActions)   // exact-equality dedupe, keep first
PendingActions = execute(all, stageToGit=true)
if REAL run && !allow_empty && len(PendingActions)==0 → error "No packages are ready to release"
   (help: "The `PrepareRelease` step will not complete if no changes cause a package's version to be increased.")
```

### F13.1 `gatherChanges(pkg)`
- `ignore` = step `ignore_conventional_commits` (log the deprecation **warning** when it is set) OR
  `[changes].ignore_conventional_commits`.
- commits = ignore ? none : `CommitMessagesAfterTag(lastStableTag(pkg))` (F9.2).
- Change files for this package (sorted by id), each with GitInfo (F11.2).
- Result = commit changes (F10.2) followed by change-file changes (F11.2).
- Implement both as `changes.Source` values (Phase 2 hook, E3), run in that fixed order in Phase 1.

### F13.2 Pre-releases
- `prerelease_label` comes from the step, overridden at runtime by `--prerelease-label` / `REMERIS_PRERELEASE_LABEL` (or `KNOPE_PRERELEASE_LABEL`).
- Change files are **kept** for pre-releases so that the final release picks them up again.
- Commits are always counted from the last **stable** tag, so each pre-release changelog entry lists **all** changes
  since that stable release.

### F13.3 Changesets shared between packages
One change file can name several packages. The `RemoveFile` action is created for each package, and duplicates are
removed by the dedupe step. A remove error is **ignored**, because another package may have deleted the file already.

### F13.4 Executing actions (`execute(actions, stageToGit)`)
In order:
- `WriteToFile`: dry run → `Would add the following to {path}: {diff}`. Real → write the file (bytes as given, no
  newline added).
- `RemoveFile`: dry run → `Would delete {path}`. Real → remove the file, ignoring errors.
- The path of every write and remove goes onto the staging list, in order.
- `AddTag` and `CreateRelease` are **returned** (they become the new `PendingActions`).
- If `stageToGit`: dry run logs `Would add files to git:` and then `  {path}` for each path (only if there are any).
  Real → `git add` (F9.4).

The action order in the dry-run output is therefore: versioned files (in the order parsed) → go.mod `AddTag` is not
printed → change-file deletes → changelog writes → staging list. Check against `tests/prepare_release/changesets/happy/dryrun_stdout.log`.

### Acceptance (F13)
All of `tests/prepare_release/*` (more than 40 scenarios): allow_empty, branching_history, enable_prerelease,
second_prerelease, release_after_prerelease, prerelease_after_release, handle_pre_versions_that_are_too_new,
override_version(+multiple), override_prerelease_label, scopes, multiple_packages, package_selection,
no_version_change, custom_change_entries, ignore_conventional_commits(_toplevel), etc.

---

## F14: `Release` step (Git tags)

Source: `step/releases/mod.rs::release`

```
if len(PendingActions)==0:                                  // Release run WITHOUT PrepareRelease in this workflow
    for pkg in packages:
        rel := findPreparedRelease(pkg); if nil continue
        PendingActions += CreateRelease(rel)
        // Go-module tags: re-apply the version to get the go.mod AddTag actions
        files := pkg.SetVersion(rel.version, pkg.goVersioning, clone(AllVersionedFiles))  (errors → none)
        PendingActions += the first AddTag action from each file.Write()
for action in PendingActions (drain):
    AddTag{tag}: create the tag UNLESS it is a package release tag of any package (IsReleaseTag, F7.5)
    CreateRelease{rel}:
        tag := ReleaseTag(rel.version, rel.package)
        if [github]: github release (F16.2) with the assets of the matching package
        if [gitea]:  gitea release (F17.2)
        if neither:  CreateTag(tag)        // dry run: "Would create Git tag {tag}"
```

`findPreparedRelease(pkg)`:
1. `current := pkg.LatestVersion()`. None → skip.
2. `lastTag := PackageVersions.FromTags(pkg, AllGitTags).Latest()`. If `lastTag == current` → nothing to release.
3. Release = `changelog.GetRelease(current)` if found. Otherwise `Release{title: current.String(), notes: "", ...}`
   (the forge then auto-generates notes, F16.2).

Forge release name: `"{pkgName} {title}"` for named packages, else `title`. Body = `notes` trimmed. Pre-release
flag = `version.IsPrerelease()`. With forges configured, **no local tag is created**: the forge creates it at
`target_commitish` = HEAD SHA.

### Acceptance (F14)
- Fixtures `tests/git_release/*`, `tests/default_workflows/*`.

---

## F15: HTTP client and credentials

Sources: `integrations/http.rs`, `app_config.rs`, `prompt.rs`

### F15.1 Client
- `User-Agent: Remeris`.
- Retry: exponential backoff, **up to 5 retries** on transient errors (connect errors, 5xx, 408, 429). Honour
  `Retry-After` (seconds or HTTP date).
- On 429 with `Retry-After`: warn `API rate limited; retrying in {n} seconds` (or `retrying after {value}`). If
  unauthenticated, also warn `To increase your rate limit, set the GITHUB_TOKEN environment variable`.
- Status errors are wrapped as `ApiRequestError`:
  `Trouble communicating with {service} while {activity}: {status error} with body: {body}`.
  JSON decode errors: `Trouble decoding the response from {GitHub|Gitea} while {activity}: …`, whose help names `GitHub`
  or `the Gitea instance at {host}` (`httpx.NewDecodeErrorFor` takes the two names apart).
- `httpx.Guard` and `httpx.URLPolicy` hold the credential-host rules both forges share: credentials only go to the
  allowed origins, a redirect to another host or port or from https to http is refused, and plain http is accepted for
  loopback hosts only. The service name and the diagnostic code prefix (`remeris::github`, `remeris::gitea`) are
  parameters.

### F15.2 Credential store
Directory: `os.UserConfigDir()/remeris/` (Knope used `…/knope/`, the same base as Rust `dirs::config_dir()`). One file
per key, holding the raw value. **Migration:** when a key is missing from `remeris/`, read it from `knope/` and copy it
into `remeris/`, so existing Knope users are not prompted again.

| key | env override | prompt |
| --- | --- | --- |
| `github_token` | `GITHUB_TOKEN` | `No GitHub token found, generate one from https://github.com/settings/tokens with 'repo' permissions and input here` |
| `gitea_token` | `GITEA_TOKEN` | `No Gitea token found, generate one from {host}/user/settings/applications with\n'repository' permissions set to 'Read and Write' and 'issue' permissions set to 'Read' and input here` |
| `jira_token` | (none) | `No Jira token found, generate one from https://id.atlassian.com/manage-profile/security/api-tokens and input here` |
| `email` | (none) | `Input your email address` |

Order: env var → stored file → prompt (masked, with display toggle, no confirmation) → save to file (create the dir).
Note: the stored gitea token is **not** per host (binding it to a host is deferred). A Gitea token is trimmed of
surrounding white space before it goes into a header; an empty one is `No Gitea token was given`.
Prompt failure / non-interactive → `Failed to get user input` (help: `This step requires user input, but no user input was provided. Try running the step again.`).

---

## F16: GitHub integration

### F16.0 Client: `google/go-github` (decision D10)

Every GitHub call goes through `github.com/google/go-github/v92` (v92.0.0, 2026-09-14, needs Go 1.26). No
hand-written REST calls. The HTTP behaviour from F15.1 (User-Agent, retries, rate-limit warnings) lives in the
`http.Client` handed to go-github:

```go
httpClient := &http.Client{Transport: httpx.NewRetryTransport(authTransport)}   // F15.1
gh := github.NewClient(httpClient)
gh.UserAgent = "Remeris"
if cfg.GitHub.APIURL != "" { gh, _ = gh.WithEnterpriseURLs(cfg.GitHub.APIURL, cfg.GitHub.UploadURL) }
```

`APIURL`/`UploadURL` are new optional `[github]` keys (`api_url`, `upload_url`). They give GitHub Enterprise support
and let tests point the client at an `httptest.Server`. Unknown to Knope, but ignored by it, so the file stays
compatible.

The two keys are not trusted on their own, because a repository's configuration must not choose where the GitHub token
goes. The URLs that may receive credentials come from `--github-api-url` / `REMERIS_GITHUB_API_URL` and
`--github-upload-url` / `REMERIS_GITHUB_UPLOAD_URL`. With only the flag or variable, it is used as is. A configured
`api_url` or `upload_url` is used only when it equals the trusted value after normalisation (lowercase scheme and host,
default port removed, one trailing slash); otherwise building the client fails with `remeris::github::untrusted_api_url`
or `remeris::github::untrusted_upload_url`. Credentials are attached only to requests for the scheme, host and port of
the API and upload URLs, a redirect to another host or from https to http is refused, and plain `http` is accepted only
for loopback hosts (`remeris::github::insecure_url`).

| Knope call | go-github method |
| --- | --- |
| Create release | `Repositories.CreateRelease(ctx, o, r, &github.RepositoryRelease{TagName, Name, Body, Prerelease, Draft, GenerateReleaseNotes, TargetCommitish})` |
| Upload asset | `Repositories.UploadReleaseAsset(ctx, o, r, releaseID, &github.UploadOptions{Name: name}, file)`. go-github expands the upload URL, so no URI-template code is needed. |
| Publish draft | `Repositories.EditRelease(ctx, o, r, id, &github.RepositoryRelease{Draft: github.Ptr(false)})` |
| Find existing PR | `PullRequests.List(ctx, o, r, &github.PullRequestListOptions{Head: owner+":"+branch, Base: base})` |
| Update PR | `PullRequests.Edit(ctx, o, r, number, &github.PullRequest{Title, Body})` |
| Create PR | `PullRequests.Create(ctx, o, r, &github.NewPullRequest{Title, Body, Head: branch, Base: base})` |
| PRs for a commit (F16.4) | `PullRequests.ListPullRequestsWithCommit(ctx, o, r, sha, nil)` |
| List issues (F19.1) | `Issues.ListByRepo(ctx, o, r, &github.IssueListByRepoOptions{State: "open", Labels: labels})` |

Errors: wrap `*github.ErrorResponse` / `*github.RateLimitError` / `*github.AbuseRateLimitError` into the same
`ApiRequestError` message (`Trouble communicating with GitHub while {activity}: {status} with body: {message}`).

### F16.1 Auth
Token sources, in order:
1. **GitHub App** (E2, available from M7 because it is just another token source): when an App ID and a private key
   are configured (F3.1 flags/env vars), use `github.com/jferrl/go-githubauth` to mint an installation token.
   `githubauth.NewApplicationTokenSource(appID, pem)` → `githubauth.NewInstallationTokenSource(installationID, appTS)`
   → `oauth2.NewClient`. If no installation ID is given, look it up once with the app JWT
   (`Apps.FindRepositoryInstallation(ctx, owner, repo)`). Tokens refresh automatically.
2. `GITHUB_TOKEN` env var.
3. Stored token (F15.2).
4. Prompt (only for `RequireAuthentication`).

- `RequireAuthentication`: sources 1 → 4.
- `MaybeAuthenticated`: sources 1 → 3, **no prompt**. Anonymous is allowed (used for PR lookups on public repos).
- The client is built once and cached in State.
- Log at debug which source was used (`Using GitHub App installation {id}` / `Using GITHUB_TOKEN`), never the token.

### F16.2 Create release
The wire payload (built by go-github from `RepositoryRelease`) must be:
```json
{"tag_name": "...", "name": "...", "body": "... (omitted when empty)", "prerelease": false,
 "generate_release_notes": <body empty>, "draft": <assets configured>, "target_commitish": "<HEAD sha, omitted if unknown>"}
```
With assets:
1. Resolve assets: glob → every match (name = file name), or the explicit list (name = `name`, else the file name).
2. For each: read the file (`Could not read asset file {path}: … Release has been created but not published!`), then
   upload it with `UploadReleaseAsset` (content type `application/octet-stream`).
3. Publish: `EditRelease` with `draft: false`.

Dry run:
`Would create a {release|prerelease} on GitHub with name {name} and tag {tag}[ at commit {sha}] and {body:\n<notes> | autogenerated body}`,
then `Would upload assets to GitHub:` and `- {name} from {path}` for each asset.

### F16.3 Create or update PR, see F18.

### F16.4 PR info enrichment (for `$pr_number` / `$pr_author_login`)
Dry run: log `Would fetch Pull Request info from GitHub repo {owner}/{repo} for {n} changes`, and set each change
that has GitInfo to `pr_number=1234`, `pr_author_login="some-user"`.
Real: for each change with GitInfo, `ListPullRequestsWithCommit` with the short hash (Knope sends
`X-GitHub-Api-Version: 2026-03-10`; go-github sets its own API version header). Take the first PR: `number` and `user.login`. Errors for one commit → warn and continue.
Nothing found → debug log.

### F16.5 Issues, see F19.1.

---

## F17: Gitea integration

Config: `{owner, repo, host}`. Base = `{host}/api/v1`, with the trailing slashes of `host` removed (a path prefix such as
`https://example.com/gitea` is kept). The code is in `internal/forge/gitea`, a leaf package.

**Auth (decision, changes Knope):** the header `Authorization: token {token}`, not the `access_token` query parameter
Knope sends: a token in a URL ends up in logs, proxies and error messages. The token is never part of a URL. It comes from
`GITEA_TOKEN`, the stored token or a prompt (F15.2), is asked for at most once per run, and never in a dry run. It is
also replaced by `[REDACTED]` in every error message the package produces.

**The `host` is trusted, but hardened (decision):** it is a required key of `[gitea]`, so unlike GitHub's `api_url` it needs
no confirmation on the command line. When a client is built (a real run only; a dry run prints `host` as written):
- it must be a URL with a scheme: otherwise `remeris::gitea::invalid_host`, as for userinfo (`user:pw@`), a query or a
  fragment, spaces and control characters. The message never repeats a host that may hold a password;
- the scheme is `https`; `http` is accepted only for `localhost` and other loopback addresses
  (`remeris::gitea::insecure_url`, and `remeris::gitea::invalid_url` for any other scheme);
- a redirect that changes the host or the port, or goes from https to http, is refused
  (`remeris::gitea::redirect`), and `Authorization` is never sent to another origin. These rules are those of
  `httpx.Guard` and `httpx.URLPolicy`, shared with GitHub.

Requests send `Accept: application/json`, a JSON body when there is one, and `User-Agent: Remeris`. The retry policy is
F15.1's, without the `GITHUB_TOKEN` hint. Errors name the service as the host written in the configuration:
`Trouble communicating with {host} while {activity}: {status error} with body: {body}`. A response that does not decode
gives `Trouble decoding the response from Gitea while {activity}: {error}` (code `remeris::gitea::api_response_error`, help
`Failure to decode a response from the Gitea instance at {host} is probably a bug. Please report it at {issue tracker}`).
The activities are `creating a release`, `fetching existing pull requests`, `updating pull request` and
`creating pull request`.

- Releases: `POST {base}/repos/{o}/{r}/releases`. Same JSON as GitHub, with `draft=false` (assets are not supported).
  Dry run: `Would create a {kind} on Gitea [{host}] with name … and tag …[ at commit …] and …`.
- Pulls: Knope sends `GET {base}/repos/{o}/{r}/pulls?state=open&head={owner}:{branch}&base={base}` and takes the first
  result. **Deviation:** Gitea's list endpoint has no `head` filter (its query parameters are `state`, `sort`,
  `milestone`, `labels`, `poster`, `page`, `limit` and `base_branch`), so a server ignores `head` and Knope may update an
  unrelated open pull request. remeris sends `GET …/pulls?state=open&base_branch={base}&limit=50&page={n}`, reads pages
  until an empty one (a server may cap the page size below 50) (at most 20 pages, then it creates), and takes the first pull request whose `head.ref` is
  the branch, whose `head.repo.owner.login` is the configured owner (case-insensitive; a null `head.repo`, a deleted fork,
  never matches) and whose `base.ref` is the base. Unknown or missing JSON fields are tolerated. Then
  `PATCH …/pulls/{number}` `{body,title}`, or `POST …/pulls` `{title,body,head,base}` (any JSON answer is accepted).
- Issues: `GET {base}/repos/{o}/{r}/issues?labels={comma-joined}&state=open&limit=30` → `[{number,title}]`.
- `TryFromRemote(url)`: `git@host:owner/repo(.git)` → host `https://host`. `https://host/owner/repo(.git)` → the same.
  Anything else → none. Known public Gitea hosts for auto-detection: `codeberg.org`.

---

## F18: `CreatePullRequest` step

1. Substitute variables into `title` and `body` (F4.4).
2. No `[github]` and no `[gitea]` → `No forge has been configured`.
3. For each configured forge (GitHub first, then Gitea):
   - `branch = CurrentBranch()`. Dry run: `Would create or update a pull request from {branch} to {base}:`,
     `\tTitle: {title}`, `\tBody: {body}`.
   - GitHub (go-github, F16.0): `PullRequests.List` with `Head: owner+":"+branch`, `Base: base`. If one exists →
     `PullRequests.Edit` with `{title, body}`. Else → `PullRequests.Create` with `{title, body, head: branch, base}`.
   - Gitea: as in F17 (Pulls), with the full short branch name as `branch` (Q3).

**Quirk Q3:** for GitHub, Knope sends `head=owner:refs/heads/<branch>` and `"head": "refs/heads/<branch>"`, and the
dry-run text shows `refs/heads/main`. **Decision:** use the short branch name everywhere in Go (this fixes a real
bug). Update the three `tests/create_pull_requests/*/stdout.log` golden files and record the deviation. Using the
last path segment (as Knope's Gitea code does) is also wrong for branches that contain `/`, so Gitea gets the full short
name too, in the dry-run text, the lookup and the `head` of the new pull request (`git rev-parse --abbrev-ref HEAD`).

---

## F19: Issue steps

### F19.1 `SelectGitHubIssue { labels? }`
- Dry run: needs `[github]` (`NotConfigured`). Logs `Would query configured GitHub instance for issues with labels a, b`
  (or `with any labels`), then `Would prompt user to select an issue and move workflow to IssueSelected state.` Sets
  the fake issue `{key:"123", summary:"Test issue"}`.
- Real: needs auth. Knope uses a GraphQL query (`issues(states:OPEN, first: 30, labels: $labels)`). go-github is
  REST-only, so use `Issues.ListByRepo` with `State: "open"`, `Labels: labels`, `PerPage: 30`. The REST endpoint also
  returns pull requests: skip items where `IsPullRequest()` is true, and fetch further pages until 30 issues are
  collected or there are no more pages (same result set as the GraphQL query). Show a Select `Select an Issue` with
  items `"{key}: {summary}"`. Log `Selected item : {issue}`.

### F19.2 `SelectGiteaIssue { labels? }`
The same flow with the Gitea API (F17). Dry-run text says `Gitea instance`. Fake issue `{123, "Test issue"}`.

### F19.3 `SelectJiraIssue { status }`
- Needs `[jira]`. Dry run: `Would query configured Jira instance for issues with status {status}` + the prompt line,
  and the fake issue `{FAKE-123, Test issue}`.
- Real: auth = `Basic base64("{email}:{jira_token}")` (both from the credential store, F15.2).
  **Decision (D5):** use the current endpoint, not Knope's deprecated `/rest/api/3/search`:
  `POST {url}/rest/api/3/search/jql` with
  `{"jql": "status = {status} AND project = {project}", "fields": ["summary"], "maxResults": 50}`.
  Response: `issues[].{key, fields.summary}` (plus `nextPageToken`, ignored: like Knope, only the first page is
  shown) → Select. Quote `status` in the JQL (`status = "In Progress"`) so multi-word statuses work; Knope did not.

### F19.4 `TransitionJiraIssue { status }`
Needs a selected issue (`NoIssueSelected`, help `You must use the SelectJiraIssue step before TransitionJiraIssue in the same workflow`)
and `[jira]`. Dry run: `Would transition currently selected issue to status {status}`. Real:
`GET {url}/rest/api/3/issue/{key}/transitions` → find the transition whose `name == status` (exact match), else
`Transition` error. Then `POST` the same URL with `{"transition": {"id": id}}`. Log `{key} transitioned to {status}`.

### F19.5 `SelectIssueFromBranch`
- Dry run: log `Would attempt to parse current branch name to select current issue` and set `{123, Fake Issue}`.
- Real: split the branch name on `-`. If the first part parses as an integer → GitHub-style `{key: n, summary: rest joined "-"}`.
  Otherwise the second part must be an integer → Jira-style `{key: "{p0}-{p1}", summary: rest}`. Else `Bad branch name`.
  Log `Auto-selecting issue {key} from ref {branch}`.
- **Quirk Q3:** Knope parses the full ref (`refs/heads/ABC-123-x`), which gives key `refs/heads/ABC-123`, and a
  GitHub-style branch never matches. Go uses the short name.

### F19.6 `branchNameFromIssue`
`strings.ReplaceAll(fmt.Sprintf("%s-%s", key, asciiLower(summary)), " ", "-")`. Only spaces are replaced. Other
characters are kept (they may be invalid in Git refs, and Knope does not clean them either).

### F19.7 `SwitchBranches`
Needs a selected issue. Dry run: `Would switch to or create a branch named {name}`. Real:
- The local branch exists → `Found existing branch named {name}, switching to it.` → checkout.
- Otherwise `Creating a new branch called {name}` → Select `Which branch do you want to base off of?` from the local
  branches → create the branch at the chosen branch's commit → checkout.
- Checkout refuses when there are uncommitted changes (any status entry for a path that is not ignored) →
  `Uncommitted changes`. Checkout is a forced checkout of HEAD after setting HEAD to the branch.

### F19.8 `RebaseBranch { to }`
**What Knope actually does** (docs and log text say otherwise): it calls libgit2
`repo.rebase(branch = to, upstream = None, onto = HEAD)`. That replays the commits of `to` that are not in the current
branch **on top of the current branch**, moves the `to` ref, then checks out `to`. In effect it lands the current
(feature) branch into `to` as a linear history: if `to` has no new commits, `to` is fast-forwarded to the feature
branch. The feature branch itself is not changed. "Don't forget to push!" means push `to`. Knope has no tests for
this step.

**Decision (D6):** keep Knope's behaviour.
- Dry run: `Would rebase current branch onto {to}` (Knope's wording, kept for parity).
- Real: `cur = CurrentBranch()`; `to` must be a local branch (remotes are not checked). CLI backend:
  `git rebase {cur} {to}`. This checks out `to`, replays its own commits onto `cur`, and leaves `to` checked out.
  go-git backend: see F9.0. Then log `Rebased current branch onto {to}` and `Switched to branch {to}, don't forget to push!`.
- Conflicts: the CLI leaves the rebase in progress. Fail with `Rebase stopped because of conflicts` (help:
  `Resolve them and run 'git rebase --continue', or 'git rebase --abort'.`). Knope fails with a generic Git error
  instead.
- Fix the step's reference docs when we write the remeris docs: describe it as "integrate the current branch into
  `to` with a linear history, then switch to `to`".

---

## F20: Default config, auto-detection and `--generate`

Sources: `config/mod.rs::{generate, generate_workflows}`, `config/package.rs::find_in_working_dir`

### F20.1 Package discovery (`findInWorkingDir`)
Run all three detectors and concatenate the results: **Cargo workspace members**, then **npm workspaces**, then
**Deno workspaces**. If any found packages, return them. Otherwise build a single default package:
`versioned_files` = every name from the F6.1 table that exists in the CWD (in table order), and
`changelog = "CHANGELOG.md"` if that file exists. No files at all → no packages.

### F20.2 Cargo workspace
Read `./Cargo.toml`; a missing (unreadable) file means no packages, but a file that is not valid TOML is an error
(`Could not parse TOML in Cargo.toml: {err}`, code `workspace::toml`). If it has `workspace.members` (an array of **literal** strings, no glob support; a non-string
→ `The Cargo workspace members array should contain only strings`):
- `workspace` must be a standard table (an inline `workspace = { ... }` is not a workspace), and `members` an array.
  An empty array is no workspace. A member that is empty, absolute or has a `..` component is an error (deviation from
  Knope); manifests are read only if regular files of at most 8 MiB.
- Each member: `{member}/Cargo.toml` must exist, parse, and have `package.name`
  (`Could not find a package.name in {path}`).
- For each member M, the package is named `M.name` with `scopes = [M.name]` and `versioned_files` =
  - for each member O in order: O's Cargo.toml if O is M. Otherwise `{O's Cargo.toml, dependency = M.name}` if O
    depends on M (`dependencies`, `dev-dependencies` or `workspace.dependencies`).
  - `{Cargo.toml, dependency: M.name}` if the root depends on M.
  - `Cargo.lock` if it exists (the dependency defaults to M's name, F7.2).
  - There is no changelog.

### F20.3 npm workspaces
Read `./package.json` `workspaces` (an array of strings; the object form `{packages: [...]}` and non-string entries
give nothing). Expand each pattern (in order, F-glob semantics; a `!negation` is a literal pattern that matches
nothing; a pattern that does not compile is `Could not process workspaces glob pattern {p} in package.json: {e}`,
`remeris::workspaces::npm_glob`). For each match with a readable, parseable `{dir}/package.json` (a file match or a
directory without one is skipped), add a workspace; a directory matched twice is added once (Knope adds it twice).
Patterns that leave the project lexically (`..`, absolute, NUL, and on Windows backslash, colon, device names) are glob
errors; symbolic links are still followed, as in Knope. A `package.json` over 8 MiB is an error
(`remeris::workspace::too_large`). Once all patterns are expanded, each workspace W must have a
string `name`, else `Could not find a name in {path}` (`remeris::workspaces::npm_no_name`). For each workspace W:
- `versioned_files` = `[W/package.json]`, plus `{package-lock.json, dependency: W.name}` if the root lock exists
  (yarn and pnpm locks are ignored), plus `{O/package.json, dependency: W.name}` for every other workspace O that has
  W.name as a key of `dependencies` or `devDependencies` (not peer or optional dependencies).
- `changelog = {W dir}/CHANGELOG.md` (**always** set, even if the file is missing; it gets created).
- `scopes = [W.name]`.

### F20.4 Deno workspaces
Knope uses `deno_config`'s workspace discovery. Simplified version for Go (differences in `testdata/deviations.md`):
- Only the working directory is looked at: no walk up to a parent workspace, and the `workspaces` of a root
  `package.json` stay with the npm detector (F20.3).
- The root `deno.json` or `deno.jsonc` must exist (`deno.json` wins when both do; one that does not parse is an
  error). It needs no `workspace` array: the `workspace` array (or the `members` array of the object form) lists
  member dirs, globs (`*`, `?`) and `!` exclusions allowed, and without it only the root folder is considered. No
  root Deno config → no packages. A lone root `deno.json(c)` with a `name` and a `version` is one package (the root
  folder, see the last bullet): `versioned_files = [that config]`, `scopes = [name]`, `changelog = CHANGELOG.md`, and
  the tag format of a named package (`name/v{version}`, F7.5). The workspace is used only if at least one folder (the
  root or a member) has a usable config; otherwise discovery falls back to the default package (F20.1).
- Order: the root folder, then the members sorted by directory URL string (`a-b/` before `a/`). A member that is
  missing, outside the root, the root itself, listed twice, or whose config does not parse, and two `deno.json(c)`
  with the same `name`, are errors `Failed to discover Deno workspace: …`.
- For each member folder: if `deno.json(c)` has `name` and `version`, use it (along with any `package.json` next to
  it). Otherwise use `package.json` with `name` and `version`. Otherwise skip the folder.
- The lockfile is `deno.lock` next to the root config (or the root `lock` setting: `false`, a path or `{path}`), only
  if it exists; a path outside the project is an error.
- The package for member T: `versioned_files` = `[T.config]`, plus `{lock, dependency: T.name}` if there is a
  lockfile, plus `{D.config, dependency: T.name}` for each other member D that depends on T:
  - D's deno.json imports have a `jsr:` requirement named T.name (T has a deno.json), or an `npm:` requirement (T
    has a package.json).
  - D's package.json deps (`dependencies` and `devDependencies` only; an entry that does not parse is skipped): `file:` path resolving to T's dir or config, `npm:@jsr/{scope}__{pkg}` for a JSR T, a
    plain name equal to T.name (T has a package.json), or `workspace:` with alias == T.name.
- `changelog = {T dir}/CHANGELOG.md`, `scopes = [T.name]`.
- The root folder is included too if it has a name and version.
Check against `tests/prepare_release/deno/*` and `tests/generate/*`.

### F20.5 Default workflows (`hasForge`, `packages`)
```
release (help "Prepare and create a new release (update changelog, bump version, create release)"):
  PrepareRelease
  Command `git commit -m "chore: prepare release $version"`   ← `"chore: prepare releases"` when ≥2 packages
  if hasForge: Command `git push`, Release
  else:        Release, Command `git push`, Command `git push --tags`
document-change (help "Create a new change file to be included in the next release"):
  CreateChangeFile
get-version (help "Get the current version of the project")      ← only when EXACTLY 1 package
  Command `echo "$version"`
```

### F20.6 Forge detection (only when there is **no** `knope.toml`)
First remote URL:
- Contains `github.com`: split on `/`. owner = the second-to-last part without a `git@github.com:` prefix. repo =
  the last part without `.git`.
- Otherwise, a known Gitea host (`codeberg.org`): `TryFromRemote` (F17). Knope tests `remote.contains(host)`;
  remeris requires the parsed host to equal the known host and drops the userinfo (testdata/deviations.md).

### F20.7 `--generate` output
Serialise:
```
[changes]            (only when ignore_conventional_commits = true)
[package] or [packages.<name>]   (package when <2 packages)
[[workflows]] ...
[github] / [gitea]   (when set)
[jira] / [release_notes]   (remeris only, when set: Knope's writer drops them)
```
`Config.Document()` (`internal/config/document.go`) builds this with `internal/tomlwrite`. Formatting must match the
golden files in `tests/generate/*/out/knope.toml`. Details: inline arrays on one line, versioned files as bare strings,
or `{ path = "...", dependency = "..." }` / `{ path, regex }` tables, which are written inline when the array also holds
a bare path and as `[[package.versioned_files]]` sections when every entry is a table; a single regex is a plain
string. Template variables of `CreatePullRequest` are always written (the defaults when the key was absent). Strings
that contain `"` use literal single quotes (`'git commit -m "..."'`). Empty or false optional fields are omitted
(`allow_empty`, `ignore_conventional_commits`, `shell`, `prerelease_label`, empty `versioned_files`,
`extra_changelog_sections`). A step table is `[[workflows.steps]]` with `type = "…"` first. A blank line separates
tables. The file ends with a single `\n`.

---

## F21: `--upgrade` and `--validate`

- `--upgrade`: for every `PrepareRelease` step with `ignore_conventional_commits = true`, clear it and set the
  top-level `[changes] ignore_conventional_commits = true`. If anything changed, rewrite `knope.toml` with the F20.7
  serialiser (**Quirk Q9:** the whole file is regenerated, so comments and formatting are lost, and packages or
  workflows that were auto-detected in Hybrid mode get written out). Otherwise log `Nothing to upgrade`.
- `--validate`: build the state and run **every** workflow in **dry-run** mode with logs suppressed. Collect the
  errors (`There are problems with the defined workflows`, one related error per workflow `Problem with workflow {name}`).
  Exit 0 if there are none. Config load errors (F2.2) are reported before validation starts.

Fixtures: `tests/upgrade/*`, `tests/validate/*`.

---

## Phase 2: Extensions beyond Knope

Phase 1 (M0–M11) reaches Knope parity. Phase 2 adds features Knope doesn't have. Each extension below is a
design sketch: enough to keep Phase 1 from closing doors, and a starting point for its own detailed spec when we
get there. All new config keys are optional, and Knope ignores unknown keys, so a `knope.toml` that uses them still
loads in Knope (it just won't do the new things).

### Phase 1 hooks (build these during parity work)

These cost little now and avoid rewrites later:

| Hook | Where | Why |
| --- | --- | --- |
| Full SemVer 2.0.0 `Version` | F1 (D8) | Base for E1 channels. |
| `changes.Source` interface; conventional commits and changesets implemented as sources | F10, F11, F13.1 | E3 adds sources without touching `PrepareRelease`. |
| Variable lookup takes a scope: `Resolve(v Variable, scope *PackageScope)`. Phase 1 always passes `nil` (= Knope's single-package rule). | F4.4 | E4 per-package variables. |
| `Git.Commit(msg string, opts CommitOptions)` and `Git.CreateTag(name string, opts TagOptions)` with a `Sign` field, unused in Phase 1 | F9 | E2 signing. |
| GitHub client built from an auth provider (token or App), not from a raw token | F16.0, F16.1 | E2 commits through the API. |
| Config decoding ignores unknown keys | F2.4 | Phase 2 keys can be added to `knope.toml` without breaking Phase 1 builds. |

### E1: Pre-release channels (alpha → beta → rc → stable)

Knope supports one pre-release label at a time, chosen per run (`prerelease_label`, `--prerelease-label`). There is
no notion of an ordered progression, so nothing stops going from `rc` back to `alpha`.

Sketch:
```toml
[release]
channels = ["alpha", "beta", "rc"]   # ordered, lowest first; any SemVer alphanumeric identifiers
```
- `remeris release --channel beta` (env `REMERIS_CHANNEL`) replaces `--prerelease-label` when `channels` is set. The
  label must be one of the channels.
- **Promotion rule:** for the same base version, a channel lower than the latest existing pre-release is an error
  (`Cannot release alpha after rc for 1.3.0`), unless `--allow-channel-downgrade`. Moving up starts that channel's
  counter at 0 (`1.3.0-rc.2` → `--channel stable` gives `1.3.0`).
- **Base version:** the change set since the last stable tag decides the base (as today). If the bump rule grows
  mid-cycle (a breaking change lands during `beta`), the base moves (`1.3.0-beta.2` → `2.0.0-beta.0`), and the
  channel order check applies to the new base.
- **Counter start:** option `counter_start = 0 | 1` (Knope uses 0).
- **Changelog:** option `collapse_prereleases = true` removes the pre-release sections of a cycle from the changelog
  when the stable version is released, leaving one combined entry.
- Precedence: SemVer compares `alpha < beta < rc` correctly as strings, but the configured order (not string order) is
  what the promotion rule uses, so custom names like `preview` work.

### E2: Signed commits and tags

Knope never commits: the default workflows run `git commit` through a `Command` step. E2 adds first-class steps:

```toml
[[workflows.steps]]
type = "Commit"
message = "chore: prepare release $version"   # a Template (F4.4)
sign = "auto"        # "auto" | "git" | "github" | "none"
push = true          # push the branch (and new tags) after committing

[[workflows.steps]]
type = "Release"
sign_tags = true     # annotated, signed tags for git-only releases
```

**`sign = "git"`:** use the user's own git signing setup. CLI backend: `git commit -S -m …` / `git tag -s`. This
respects `gpg.format` (`openpgp`, `ssh`, `x509`), `user.signingkey` and `gpg.program`. go-git fallback: OpenPGP keys
through `CommitOptions.SignKey`, and SSH keys through go-git's signer support (check which go-git v5 release has it
during the spike); `x509` is CLI-only. If signing fails, the step fails; it never silently falls back to unsigned.

**`sign = "github"`: GitHub App, commits made through the API.** GitHub signs commits created through the API by
an App and shows them as Verified, as long as the author/committer are not overridden (confirm this, and the exact
conditions, in a spike before building on it). Flow, all through go-github with the App installation token (F16.1):
1. Precondition: the local branch must equal the remote branch (`git fetch` then compare SHAs). Otherwise fail:
   `Local branch is not in sync with origin; push or pull first`.
2. Read the staged changes (`git diff --cached --name-status -z`). For each added/modified file:
   `Git.CreateBlob` (base64). Deleted files become tree entries with a nil SHA.
3. `Git.CreateTree` with `base_tree` = the HEAD tree. Keep file modes (executable bit, symlinks).
4. `Git.CreateCommit` with the message and parent = HEAD, **no author or committer** (so GitHub signs it).
5. `Git.UpdateRef("heads/{branch}", sha, force=false)`. A race with someone else's push fails here, with no force.
6. Sync locally: `git fetch origin {branch}` then `git reset --soft {sha}` (the index already matches the commit).
7. Tags: `Release` with a forge creates the tag at `target_commitish` = this SHA, so no local tag or push is needed.
   Signed annotated tags are only supported with `sign = "git"` (verify whether App-created tag objects are signed).

**`sign = "auto"`:** `github` when GitHub App credentials are configured and `[github]` is set; else `git` when
git's `commit.gpgsign` is true; else `none`.

Dry run: `Would create a signed commit via the GitHub API on {branch}: {message}` (or `…with git`), and list the files.

### E3: More changelog sources

Today a change comes from either a conventional commit or a change file. E3 turns these into pluggable sources:

```go
// pkg/versioning/changes
type Source interface {
    Name() string
    // Changes returns the changes for one package since its last stable release.
    Changes(ctx context.Context, pkg PackageInfo, since ReleaseRef) ([]Change, error)
}
```
Config:
```toml
[changes]
sources = ["conventional_commits", "changesets"]          # default; order = order in the changelog
ignore_conventional_commits = true                        # still works: removes that source (Knope compatibility)

[changes.github_pull_requests]                            # example of a new source
labels = { major = ["breaking"], minor = ["feature", "enhancement"], patch = ["bug", "fix"] }
skip_labels = ["no-changelog"]
```
Candidate sources:
| Source | Change from | Notes |
| --- | --- | --- |
| `conventional_commits` | commit messages | Phase 1 (F10) |
| `changesets` | `.changeset/*.md` | Phase 1 (F11) |
| `github_pull_requests` | merged PRs since the last tag | Type from labels; summary = title; details = a `## Changelog` section of the body, if present. Uses go-github. |
| `commits` | every commit subject | For repos without conventions; all patch unless configured. |
| `command` | an external program | Runs a command that prints a JSON array `[{type, summary, details?, packages?}]`. Escape hatch for custom systems. |
| `jira` | Jira issues in a fix version | Uses the existing Jira client (F19.3). |

Dedupe: when two sources describe the same work (a PR and its squash commit), keep the first source's change
and drop others with the same commit hash or PR number. Controlled by `dedupe = true` (default).

### E4: Monorepo releases with per-package variables

Knope already releases several packages from one repo with their own versions and `{name}/v{version}` tags. Two
gaps:
1. `Version` and `ChangelogEntry` only work with a single package (`Too many packages defined`), so with several
   packages you can't put versions in commit messages, PR titles or commands.
2. Commits are assigned to packages only by conventional-commit scope.

Sketch:
- **Qualified variables.** A variable value can name a package:
  ```toml
  variables = { "$api_version" = { variable = "Version", package = "api" },
                "$web_notes"   = { variable = "ChangelogEntry", package = "web" } }
  ```
  The plain string form (`"Version"`) keeps Knope's single-package rule.
- **Per-package iteration.** `Command` and `CreatePullRequest` get `for_each = "released_package"` (only packages
  released in this run, i.e. with a pending `CreateRelease`) or `for_each = "package"` (all packages). Inside the
  loop these default variables apply: `$package`, `$version`, `$changelog`, `$tag`. Example:
  ```toml
  [[workflows.steps]]
  type = "Command"
  for_each = "released_package"
  command = "npm publish --workspace $package"
  ```
- **Aggregate variables** for multi-package text: `ReleasedPackages` (`api@1.2.0, web@0.4.1`) and `ReleaseNotes` (each
  released package as `## {package} {version}` followed by its notes, ready for a PR body). The default multi-package
  commit message becomes `chore: prepare releases $released_packages` (opt-in, to keep parity by default).
- **Path-based attribution.** Package option `paths = ["packages/api/**"]`: a commit counts for the package if it
  touches a matching file. `commit_filter = "scopes" | "paths" | "both"` (default `scopes`, Knope's behaviour).
  Requires the `Git` interface to return changed paths per commit.
- **Tag format.** Option `tag_format = "{name}@{version}"` per package (default Knope's `{name}/v{version}`), for
  ecosystems like npm that use `@`. Tag parsing (F7.5) must use the same format.

### Phase 2 order (proposal)

| # | Extension | Depends on |
| --- | --- | --- |
| P2.1 | E4 per-package variables and `for_each` | Phase 1 F4.4 scope hook |
| P2.2 | E2 signed commits: `Commit` step with `sign = "git"` | F9 |
| P2.3 | E2 GitHub App commits (`sign = "github"`) | F16.1, spike on verification rules |
| P2.4 | E1 pre-release channels | F1 |
| P2.5 | E3 change sources: `github_pull_requests`, `command` | F13.1 Source interface |
| P2.6 | E4 path-based attribution, tag formats | F9 changed-paths API |

---

## Testing strategy

1. **Unit tests** for `pkg/versioning/*`. Port every `#[cfg(test)]` module from `knope-versioning` and from the CLI
   (`git.rs` branch parsing, `config/toml.rs` Gitea remote parsing, etc.).
2. **Golden fixture tests (key to parity).** Knope has about 920 fixture files under `crates/knope/tests/**`. Each
   case has:
   - `in/`: the starting files (including `knope.toml`, `.changeset/`).
   - A `mod.rs` with the Git setup (`Commit("msg")`, `Tag("v1.2.3")`, optional remote, env vars) and the command
     (such as `release`, `--dry-run`).
   - `out/`: the expected files after a real run. `dryrun_stdout.log` / `stdout.log` / `stderr.log` hold the
     expected output, with `[DATE]` standing in for today's date.

   Use `github.com/rogpeppe/go-internal/testscript` as the harness. Convert each Knope case with a one-off script
   into a `.txtar` file under `testdata/script/<suite>/<case>.txtar`, holding:
   - the `in/` files as txtar sections;
   - the Git setup as commands (`git init`, `git commit --allow-empty -m '…'`, `git tag …`, with a fixed author
     through `env GIT_AUTHOR_NAME=…`);
   - `exec remeris --dry-run release` with `cmp stdout dryrun_stdout.log`, then a real run and `cmp` against each
     `out/` file;
   - `[DATE]` handled by a custom `date` command, or by setting `REMERIS_FAKE_DATE` in tests (read only by a test hook).

   `testscript.Main` registers the binary in-process through `TestMain`, so no separate build step is needed.
   Credit the fixtures to Knope (MIT-licensed). Enable them one milestone at a time. Keep a
   `testdata/deviations.md` listing each fixture changed for a Quirk decision.
3. **HTTP tests:** use `httptest.Server` with a configurable API base URL (GitHub base URL and Gitea host), and
   check the request bodies against F16/F17. The Knope `integration_github` / `integration_gitea` suites hit real
   services, so do not port those.
4. **Prompt tests:** put prompts behind an interface and script them in tests (CreateChangeFile, Select*).

---

## Quirks register

| ID | Quirk | Where | Decision |
| --- | --- | --- | --- |
| Q1 | Unknown TOML keys are silently ignored (for example the docs' `label` vs the code's `labels`) | F2, F19 | Keep. Maybe add a `--validate` warning later. |
| Q2 | Pre-release ordering compares labels as strings | F1 | **Change**: SemVer precedence (D8) |
| Q3 | `CurrentBranch` returns `refs/heads/...`. This breaks the GitHub PR head and `SelectIssueFromBranch` | F9.6, F18, F19.5 | **Fix** (short name). Update the golden logs. |
| Q4 | Changeset package names are not unquoted, so `"pkg": minor` gives name `"pkg"` | F11.1 | Keep (compatibility). Consider stripping quotes later. |
| Q5 | Change-file front-matter order comes from a HashMap (random) | F11.4 | Use selection order (deterministic) |
| Q6 | `deno.json(c)` edits drop comments, despite the docs | F6.2 | **Fix** with an in-place `hujson` edit |
| Q7 | `pom.xml` is fully re-emitted and loses formatting | F6.2 | **Fix** with a surgical edit |
| Q8 | Regex write replaces every occurrence of the version text inside the match | F6.2 | Keep |
| Q9 | `--upgrade` regenerates the whole file (comments lost, hybrid defaults written out) | F21 | Keep for v1. Later: edit in place. |
| Q10 | JSON outputs lose the trailing newline (`package.json` etc.) | F6.2 | Keep (golden parity). Maybe keep the original trailing newline later. |
| Q11 | `deno.jsonc` is missing from the explicit file-name table | F6.1 | **Fix** (add it) |
| Q12 | `CreateTag` makes annotated tags with empty messages | F9.5 | Keep |
| Q13 | The npm workspace `changelog` is always set, even when the file doesn't exist | F20.3 | Keep |
| Q14 | `RebaseBranch` rebases `to` onto the current branch and switches to it (docs say the opposite) | F19.8 | Keep (D6) |

---

## Decisions

Recorded 2026-10-02.

| ID | Question | Decision | Where |
| --- | --- | --- | --- |
| D1 | Name and config file | The program is `remeris` (binary, messages, codes, User-Agent, credential dir). Knope's configuration is the first supported one, not a legacy format: it keeps its own names (`knope.toml`, `KNOPE_*`), owned by the code that implements it. No global name package. | F3.1, F15 |
| D2 | Git backend | `git` CLI first, go-git v5 fallback when `git` is not installed | F9.0 |
| D3 | Knope Bot (GitHub App) | Out of scope for now | — |
| D4 | Diagnostics | Match Knope's message and help text. No source snippets or docs URLs until the remeris docs exist. | F3.3 |
| D5 | Jira search endpoint | Use `POST /rest/api/3/search/jql` | F19.3 |
| D6 | `RebaseBranch` | Keep Knope's behaviour (integrate the current branch into `to`, then switch to `to`) | F19.8 |
| D7 | Minimum Go version | Go 1.27 | 0.3 |
| D8 | Version grammar | Full SemVer 2.0.0 from Phase 1 (pre-release ids, build metadata, §11 precedence). Bumps keep Knope's `label.N` shape. | F1 |
| D9 | CLI flags and config | Reuse readykit's urfave/cli v3 + koanf (`cliflagv3`) pattern, with a lineage-based key prefix for subcommands | F3.1 |
| D10 | GitHub client | `google/go-github` (v92) for every GitHub call | F16.0 |
| D11 | GitHub App auth | `jferrl/go-githubauth`; App credentials only from flags/env, never `knope.toml` | F16.1, E2 |
| D12 | Phases | Knope parity first (Phase 1), then E1 channels, E2 signed commits, E3 change sources, E4 monorepo variables (Phase 2) | Phase 2 |
