# Remeris: spec for a Go reimplementation of Knope

**Source analysed:** [knope-dev/knope](https://github.com/knope-dev/knope) at `c952077` (2026-09-14), Knope CLI `0.23.0`,
`knope-versioning` `0.8.x`, `knope-config` `0.4.x`, plus the [changesets](https://github.com/knope-dev/changesets) crate it
depends on.

**Goal:** Rebuild Knope in Go, one feature at a time. The Go binary (`remeris`) should be a **drop-in replacement**:
it reads the same `knope.toml`, uses the same `.changeset/` files and Git tag formats, writes the same file edits and
changelog output, and prints the same messages. Each section below is one unit of work. It lists the behaviour to
reproduce, the algorithm (taken from the Rust source, not only the docs), edge cases, and acceptance criteria.

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
23. [Testing strategy](#testing-strategy)
24. [Quirks register](#quirks-register)
25. [Open questions](#open-questions)

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
internal/cli/                  # command tree built from config, flags, run/validate/generate/upgrade
internal/config/               # knope.toml loading, defaults, discovery, serialisation (generate/upgrade)
internal/workflow/             # Workflow, Step (tagged union), runner, State, RunType
internal/steps/                # one file per step
internal/variables/            # Template + Variable substitution
internal/git/                  # Git interface + implementation
internal/forge/github/         # REST + GraphQL
internal/forge/gitea/
internal/issues/jira/
internal/httpx/                # client with retry/backoff/rate-limit logging, ApiRequestError
internal/credentials/          # token/email store in user config dir, prompting
internal/prompt/               # select, multiselect, text, password
internal/fsx/                  # dry-run aware write/remove/mkdir
pkg/versioning/                # PURE core (the knope-versioning equivalent)
    semver/                    # Version, Prerelease, Rule, PackageVersions
    changes/                   # Change, ChangeType, conventional commit parsing
    changesets/                # .changeset file format (the changesets crate equivalent)
    releasenotes/              # Sections, ChangeTemplate, Changelog, Release
    versionedfile/             # one file per format + Config/Format
    package.go                 # Package, ChangeConfig, apply changes → []Action
    action.go                  # Action, ReleaseTag
```

### 0.3 Go version and dependencies

**Go version:** `go 1.27` in `go.mod` (the latest toolchain is 1.27.1). Go 1.27 is the first release where
`encoding/json/v2` and `encoding/json/jsontext` are generally available, and the spec relies on them. It also
covers `(?<name>…)` regex groups (Go ≥ 1.22) and `hujson`'s minimum of Go 1.26.

**How dependencies were chosen:** prefer the standard library. Add a third-party module only when it has had a
release in the last ~12 months and is not archived or marked unmaintained. CLI and config use the libraries chosen
by the project: **urfave/cli v3** and **koanf v2**. Versions were checked against `proxy.golang.org` on 2026-10-02.

| Need | Knope uses | Go choice | Latest release (checked 2026-10-02) | Notes |
| --- | --- | --- | --- | --- |
| CLI | clap | **`github.com/urfave/cli/v3`** | v3.14.0, 2026-10-02 | Workflows become `[]*cli.Command`, built at runtime from the config. See F3.1. |
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
| Git | `git2` (libgit2) | **stdlib** `os/exec` calling the `git` CLI, behind the `Git` interface (F9) | stdlib | Exact behaviour, including rebase and annotated tags. Considered: `github.com/go-git/go-git/v5` (v5.19.2, 2026-07-29; no rebase, and v6 is still alpha). |
| HTTP + retry | reqwest-middleware + reessaie | **stdlib** `net/http` with a retrying `RoundTripper` | stdlib | About 80 lines: exponential backoff with jitter, 5 retries, honours `Retry-After` (seconds or HTTP date), logs rate-limit warnings (F15.1). Considered: `hashicorp/go-retryablehttp` (last release v0.7.8, 2025-06). |
| GitHub upload URL template | URI template | **stdlib** `net/url` | stdlib | Remove the `{?name,label}` suffix from `upload_url` and set `?name=`. No template library needed (`yosida95/uritemplate` has had no release since 2022). |
| Command splitting | `execute` | Hand-written tokenizer in `internal/steps/command` | n/a | About 50 lines: whitespace separates words, `'…'` and `"…"` quote, backslash escapes, **no** `$VAR` expansion (matches Knope). Rejected: `google/shlex` (no change since 2019); `mvdan.cc/sh/v3/shell.Fields` (expands variables); `mattn/go-shellwords` (recent tag, but `go 1.13` module and adds little). |
| Base64 (Jira auth) | `base64` | **stdlib** `encoding/base64` | stdlib | |
| Logging | tracing | **stdlib** `log/slog` with a custom plain handler | stdlib | No level, time or target prefix. See F3. |
| Diagnostics | miette | Custom `Diagnostic` interface: `Code()`, `Help()`, `URL()`, optional source span | n/a | See F3. |
| User config dir | `dirs` | **stdlib** `os.UserConfigDir()` | stdlib | Same locations as Rust `dirs::config_dir()`. |
| Dates | `time` | **stdlib** `time` (`time.Now().UTC().Format(time.DateOnly)`) | stdlib | |
| Conventional commits | `git-conventional` | Hand-written parser in `pkg/versioning/changes` | n/a | Must match `git-conventional`'s grammar exactly (F10.1). Considered: `github.com/leodido/go-conventionalcommits` (v0.13.0, 2026-07). Its grammar and footer rules differ, so it would need adapting anyway. |
| Semver | own | Own type in `pkg/versioning/semver` | n/a | Knope's version grammar is a strict subset of SemVer and has non-standard ordering (F1), so a general semver library would not help. |
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

---

## F1: Semantic versions

Source: `knope-versioning/src/semver/{mod,rule,package_versions,prerelease_map}.rs`

### F1.1 Version grammar (a strict **subset** of SemVer)

```
Version    := Stable | Stable "-" Prerelease
Stable     := u64 "." u64 "." u64
Prerelease := Label "." u64          ; split on the FIRST "." after "-"
Label      := any string without "."  ; e.g. "rc", "alpha", "beta"
```

Parsing (`FromStr`):
1. Split on the **first** `-`. The left side must split on `.` into **exactly 3** parts, each parsing as `u64`.
2. If there is a right side, split it on the **first** `.`. The left part is the label (any string, even empty).
   The right part must parse as `u64`. So `1.2.3-rc.4` is valid. `1.2.3-rc4`, `1.2.3-rc.4.5`, `1.2.3+build` and
   `v1.2.3` are all **invalid**.
3. Error message: `Found invalid semantic version {detail}`. Help: `The version must be a valid Semantic Version`.
   URL: `https://knope.tech/reference/concepts/semantic-versioning`.

Display: `M.m.p` or `M.m.p-label.N`.

Ordering: compare the stable part `(major, minor, patch)` first. If equal, stable > pre. Two pre-releases compare
by **label as a string** (lexicographic byte order), then by number. This is not SemVer precedence, but it only
matters inside `PackageVersions`, so copy it exactly.

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

State: `stable *StableVersion` and `prereleases map[StableVersion]map[Label]Prerelease`. Keep both maps **ordered by
key** (Go: sort keys when needed). There is always at least one label per entry.

**`FromTags(prefix *string, tags []string)`**: tags must already be in **newest-first order** (see F9.3).
- The pattern is `"v"`, or `"{prefix}/v"` when a prefix is given. Only tags starting with the pattern are used.
- For each matching tag, remove the pattern (first occurrence only) and try to parse a Version (skip it on error):
  - Stable: set `stable` and **stop** (only pre-releases newer than the latest stable matter).
  - Pre: `update(version)`.

**`update(version)`**:
- Stable `new`: if `stable >= new` do nothing. Otherwise set `stable = new` and **clear all pre-releases**.
- Pre `(s, pre)`: if a pre with the same `s` and label already exists and is `>= pre`, do nothing. Otherwise
  insert or overwrite `prereleases[s][pre.label] = pre`.

**`Latest()`**: if there are pre-releases, return the pre whose stable key is the **largest**, using the largest
label in that map (by string order). Otherwise return `stable`. Otherwise return nothing.

**`CalculateNewVersion(rule)`**:
| rule | stable exists | result |
| --- | --- | --- |
| Stable(r) | yes | `bump_stable(stable, r)` |
| Stable(r) | no | largest pre-release stable key **as-is**, else `0.0.0` |
| Pre{label, r} | any | `calculatePre` (below) |
| Release | any | largest pre-release stable key, else error `PreReleaseNotFound` (`No prerelease version found, but a Release rule was requested`) |

`calculatePre(label, r)`:
1. `base = bump_stable(stable, r)` if stable exists, else the largest pre-release stable key, else `0.0.0`.
2. `n = prereleases[base][label].version + 1` if that entry exists, else `0`.
3. Return `base-label.n`.

Examples to test (from the docs and code):
- `1.2.3`, Pre(rc, Patch) → `1.2.4-rc.0`. Then with `1.2.4-rc.0` tagged → `1.2.4-rc.1`.
- Stable `1.2.3`, tags `1.2.4-rc.0`, then a Minor change with Pre(alpha) → `1.3.0-alpha.0`.
- `0.1.2` + Major → `0.2.0`. `0.2.0` + Minor → `0.2.1`.
- BumpVersion `Release` on `1.2.3-rc.4` → `1.2.3`.

### Acceptance (F1)
- Port every unit test in `semver/*.rs` and `package_versions.rs` as a Go table test.

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
   vars) are where koanf pays off (Open question 1).

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
| `--prerelease-label` (workflow flag, env `KNOPE_PRERELEASE_LABEL`) | workflow has `PrepareRelease` | Sets `prerelease_label` on **every** `PrepareRelease` step in **every** workflow. The flag wins over the env var. |

- Each workflow is a subcommand, and `help_text` is its description.
- No subcommand (and no `--validate`) → error `No workflow selected. Run 'knope --help' for a list of options.`
- Unknown workflow names are rejected by the CLI parser. Knope also has a fallback `No workflow named {name}`.
- `--help` on the root lists workflows. `<workflow> --help` shows the flags for that workflow.

**urfave/cli v3 mapping.**
- Load the config **before** building the command tree (as Knope does), because workflows become subcommands:
  `root.Commands = append(root.Commands, &cli.Command{Name: w.Name, Usage: w.HelpText, Flags: …, Action: runWorkflow(w)})`.
- `--dry-run` and `--verbose` (alias `v`) are `*cli.BoolFlag` on the root. v3 root flags are persistent by default,
  so `remeris release --dry-run` works, as in clap's `global(true)`.
- `--generate`, `--upgrade` and `--validate` are root `BoolFlag`s with `Local: true`, added only for the matching
  ConfigSource. The root `Action` handles them, and errors with `No workflow selected…` when none is set.
- `--override-version`: one package → `*cli.StringFlag` validated by F1 parsing. Several packages →
  `*cli.StringSliceFlag` of `name=version`.
- `--prerelease-label`: `*cli.StringFlag{Sources: cli.EnvVars("KNOPE_PRERELEASE_LABEL")}`. The flag wins over the env
  var because of urfave's value precedence.
- `root.Version` gives `--version`. Help text formatting differs from clap. Golden tests should not compare
  `--help` output byte-for-byte.
- Exit codes: return the `Diagnostic` from the action. `main` renders it to stderr and exits 1 (do not use
  `cli.Exit` inside steps).

> **Decision:** keep `knope` in the user-facing strings for now so the golden files match byte-for-byte. Replace
> them with `remeris` later behind one constant (see Open questions).

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

Golden `stderr.log` files in the Knope tests contain miette's ASCII rendering. Either match that rendering closely,
or normalise the tests to check only messages and help text. **Recommendation:** check message, help and URL text
only. Byte-for-byte miette graphics are not worth copying.

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
- Read top-level `version` (`{path} did not have a 'version' field`).
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
  5. Diff `{d} = {v}`. Serialise in Deno's canonical lockfile format (2-space pretty with a trailing newline, keys in
     Deno's canonical order). Check against fixtures `tests/prepare_release/deno`.

#### `pubspec.yaml`
- Read top-level `version` (YAML).
- Write: find the first line starting with `version: ` and replace **that line only** with `version: {v}`. If there
  is no such line, parse to a YAML mapping, insert `version`, and re-serialise the whole file. Diff `{v}`.

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
   (code `knope_versioning::inconsistent_versions`, help `All files in a package must have the same version`).
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
    HasUncommittedChanges() (bool, error); Rebase(onto string) error
}
```

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

### F13.2 Pre-releases
- `prerelease_label` comes from the step, overridden at runtime by `--prerelease-label` / `KNOPE_PRERELEASE_LABEL`.
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
- `User-Agent: Knope`.
- Retry: exponential backoff, **up to 5 retries** on transient errors (connect errors, 5xx, 408, 429). Honour
  `Retry-After` (seconds or HTTP date).
- On 429 with `Retry-After`: warn `API rate limited; retrying in {n} seconds` (or `retrying after {value}`). If
  unauthenticated, also warn `To increase your rate limit, set the GITHUB_TOKEN environment variable`.
- Status errors are wrapped as `ApiRequestError`:
  `Trouble communicating with {service} while {activity}: {status error} with body: {body}`.
  JSON decode errors: `Trouble decoding the response from {GitHub|the Gitea instance at host} while {activity}: …`.

### F15.2 Credential store
Directory: `os.UserConfigDir()/knope/` (the same place as Rust `dirs::config_dir()` on Linux, macOS and Windows). One
file per key, holding the raw value:

| key | env override | prompt |
| --- | --- | --- |
| `github_token` | `GITHUB_TOKEN` | `No GitHub token found, generate one from https://github.com/settings/tokens with 'repo' permissions and input here` |
| `gitea_token` | `GITEA_TOKEN` | `No Gitea token found, generate one from {host}/user/settings/applications with\n'repository' permissions set to 'Read and Write' and 'issue' permissions set to 'Read' and input here` |
| `jira_token` | (none) | `No Jira token found, generate one from https://id.atlassian.com/manage-profile/security/api-tokens and input here` |
| `email` | (none) | `Input your email address` |

Order: env var → stored file → prompt (masked, with display toggle, no confirmation) → save to file (create the dir).
Note: the stored gitea token is **not** per host.
Prompt failure / non-interactive → `Failed to get user input` (help: `This step requires user input, but no user input was provided. Try running the step again.`).

---

## F16: GitHub integration

### F16.1 Auth states
- `RequireAuthentication`: token from env / store / prompt.
- `MaybeAuthenticated`: env / store, **no prompt**. Anonymous is allowed (used for PR lookups on public repos).
- The client and token are cached in State after first use.

### F16.2 Create release (`POST https://api.github.com/repos/{owner}/{repo}/releases`)
Headers: `Authorization: token {token}`, `Accept: application/vnd.github+json`. Body:
```json
{"tag_name": "...", "name": "...", "body": "... (omitted when empty)", "prerelease": false,
 "generate_release_notes": <body empty>, "draft": <assets configured>, "target_commitish": "<HEAD sha, omitted if unknown>"}
```
With assets:
1. Resolve assets: glob → every match (name = file name), or the explicit list (name = `name`, else the file name).
2. For each: read the file (`Could not read asset file {path}: … Release has been created but not published!`), then
   POST to `upload_url`, expanding the URI template `{?name,label}` with `name`. Headers: `Content-Type: application/octet-stream`
   and `Content-Length`. The body is the raw bytes.
3. `PATCH {release.url}` `{"draft": false}`.

Dry run:
`Would create a {release|prerelease} on GitHub with name {name} and tag {tag}[ at commit {sha}] and {body:\n<notes> | autogenerated body}`,
then `Would upload assets to GitHub:` and `- {name} from {path}` for each asset.

### F16.3 Create or update PR, see F18.

### F16.4 PR info enrichment (for `$pr_number` / `$pr_author_login`)
Dry run: log `Would fetch Pull Request info from GitHub repo {owner}/{repo} for {n} changes`, and set each change
that has GitInfo to `pr_number=1234`, `pr_author_login="some-user"`.
Real: for each change with GitInfo, `GET https://api.github.com/repos/{o}/{r}/commits/{shortHash}/pulls`
(headers `Accept: application/vnd.github+json` and `X-GitHub-Api-Version: 2026-03-10`, plus `Authorization: Bearer`
if there is a token). Take the first PR: `number` and `user.login`. Errors for one commit → warn and continue.
Nothing found → debug log.

### F16.5 Issues (GraphQL), see F19.1.

---

## F17: Gitea integration

Config: `{owner, repo, host}`. Base = `{host}/api/v1`. Auth = query parameter `access_token={token}`.
- Releases: `POST {base}/repos/{o}/{r}/releases`. Same JSON as GitHub, with `draft=false` (assets are not supported).
  Dry run: `Would create a {kind} on Gitea [{host}] with name … and tag …[ at commit …] and …`.
- Pulls: `GET {base}/repos/{o}/{r}/pulls?state=open&head={owner}:{branch}&base={base}` → `[{number,url}]`. Then
  `PATCH …/pulls/{number}` `{body,title}`, or `POST …/pulls` `{title,body,head,base}`.
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
   - GitHub: `GET https://api.github.com/repos/{o}/{r}/pulls?head={owner}:{branch}&base={base}` (Bearer auth). If
     one exists → `PATCH {pr.url}` `{title, body}`. Else → `POST …/pulls` `{title, body, head: branch, base}`.
   - Gitea: as in F17, using only the **last path segment** of the branch ref as `branch`.

**Quirk Q3:** for GitHub, Knope sends `head=owner:refs/heads/<branch>` and `"head": "refs/heads/<branch>"`, and the
dry-run text shows `refs/heads/main`. **Decision:** use the short branch name everywhere in Go (this fixes a real
bug). Update the three `tests/create_pull_requests/*/stdout.log` golden files and record the deviation. Using the
last segment (as the Gitea code does) is also wrong for branches that contain `/`. Use the full short name (`git
rev-parse --abbrev-ref HEAD`).

---

## F19: Issue steps

### F19.1 `SelectGitHubIssue { labels? }`
- Dry run: needs `[github]` (`NotConfigured`). Logs `Would query configured GitHub instance for issues with labels a, b`
  (or `with any labels`), then `Would prompt user to select an issue and move workflow to IssueSelected state.` Sets
  the fake issue `{key:"123", summary:"Test issue"}`.
- Real: needs auth. `POST https://api.github.com/graphql` with `Authorization: bearer {token}`:
  ```graphql
  query($repo: String!, $owner: String!, $labels: [String!]) {
    repository(name:$repo, owner:$owner) { issues(states:OPEN, first: 30, labels: $labels) { nodes { number, title } } }
  }
  ```
  Read `/data/repository/issues/nodes`. If it is missing → `UnexpectedApiResponse({json})`. Show a Select
  `Select an Issue` with items `"{key}: {summary}"`. Log `Selected item : {issue}`.

### F19.2 `SelectGiteaIssue { labels? }`
The same flow with the Gitea API (F17). Dry-run text says `Gitea instance`. Fake issue `{123, "Test issue"}`.

### F19.3 `SelectJiraIssue { status }`
- Needs `[jira]`. Dry run: `Would query configured Jira instance for issues with status {status}` + the prompt line,
  and the fake issue `{FAKE-123, Test issue}`.
- Real: auth = `Basic base64("{email}:{jira_token}")` (both from the credential store, F15.2).
  `POST {url}/rest/api/3/search` `{"jql": "status = {status} AND project = {project}", "fields": ["summary"]}` →
  `issues[].{key, fields.summary}` → Select.
  (Note: Atlassian has deprecated `/rest/api/3/search` in favour of `/rest/api/3/search/jql`. Put this behind a
  constant and check it against current Jira Cloud before shipping.)

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
Dry run: `Would rebase current branch onto {to}`. Real: rebase HEAD onto the local branch `to` (not remotes), then
log `Rebased current branch onto {to}`, then **switch to `to`** (as Knope does), then log
`Switched to branch {to}, don't forget to push!`.
Note: Knope rebases HEAD onto `to` and then checks out `to`. That is a "rebase then fast-forward-style switch" flow.
Copy the logged behaviour: `git rebase {to}` followed by `git checkout {to}`. Confirm this is what users expect (Open questions).

---

## F20: Default config, auto-detection and `--generate`

Sources: `config/mod.rs::{generate, generate_workflows}`, `config/package.rs::find_in_working_dir`

### F20.1 Package discovery (`findInWorkingDir`)
Run all three detectors and concatenate the results: **Cargo workspace members**, then **npm workspaces**, then
**Deno workspaces**. If any found packages, return them. Otherwise build a single default package:
`versioned_files` = every name from the F6.1 table that exists in the CWD (in table order), and
`changelog = "CHANGELOG.md"` if that file exists. No files at all → no packages.

### F20.2 Cargo workspace
Read `./Cargo.toml`. If it has `workspace.members` (an array of **literal** strings, no glob support; a non-string
→ `The Cargo workspace members array should contain only strings`):
- Each member: `{member}/Cargo.toml` must exist, parse, and have `package.name`
  (`Could not find a package.name in {path}`).
- For each member M, the package is named `M.name` with `scopes = [M.name]` and `versioned_files` =
  - for each member O in order: O's Cargo.toml if O is M. Otherwise `{O's Cargo.toml, dependency = M.name}` if O
    depends on M (`dependencies`, `dev-dependencies` or `workspace.dependencies`).
  - `{Cargo.toml, dependency: M.name}` if the root depends on M.
  - `Cargo.lock` if it exists (the dependency defaults to M's name, F7.2).
  - There is no changelog.

### F20.3 npm workspaces
Read `./package.json` `workspaces` (array of glob strings). Expand each pattern (in order). For each match with a
readable, parseable `{dir}/package.json`, add a workspace. For each workspace W (the package must have a `name`, else
`Could not find a name in {path}`):
- `versioned_files` = `[W/package.json]`, plus `{package-lock.json, dependency: W.name}` if the root lock exists,
  plus `{O/package.json, dependency: W.name}` for every other workspace O that lists W.name in `dependencies` or
  `devDependencies`.
- `changelog = {W dir}/CHANGELOG.md` (**always** set, even if the file is missing; it gets created).
- `scopes = [W.name]`.

### F20.4 Deno workspaces
Knope uses `deno_config`'s workspace discovery. Simplified version for Go:
- The root `deno.json` or `deno.jsonc` must exist and have a `workspace` array (member dirs, globs allowed).
  No root Deno config → no packages.
- For each member folder: if `deno.json(c)` has `name` and `version`, use it (along with any `package.json` next to
  it). Otherwise use `package.json` with `name` and `version`. Otherwise skip the folder.
- The lockfile is `deno.lock` next to the root config (or the root `lock` setting), only if it exists.
- The package for member T: `versioned_files` = `[T.config]`, plus `{lock, dependency: T.name}` if there is a
  lockfile, plus `{D.config, dependency: T.name}` for each other member D that depends on T:
  - D's deno.json imports have a `jsr:` requirement named T.name (T has a deno.json), or an `npm:` requirement (T
    has a package.json).
  - D's package.json deps: `file:` path resolving to T's dir or config, `npm:@jsr/{scope}__{pkg}` for a JSR T, a
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
- Contains a known Gitea host (`codeberg.org`): `TryFromRemote` (F17).

### F20.7 `--generate` output
Serialise:
```
[changes]            (only when ignore_conventional_commits = true)
[package] or [packages.<name>]   (package when <2 packages)
[[workflows]] ...
[github] / [gitea]   (when set)
```
Formatting must match the golden files in `tests/generate/*/out/knope.toml`. Details: inline arrays on one line,
versioned files as bare strings, or inline tables `{ path = "...", dependency = "..." }` / `{ path, regex }`. Strings
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
| Q2 | Pre-release ordering compares labels as strings | F1 | Keep |
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
| Q14 | `RebaseBranch` switches to the target branch after rebasing | F19.8 | Keep for now. See Open questions. |

---

## Open questions

1. **Name and config file.** Should `remeris` read only `knope.toml`, or also `remeris.toml` (and which one wins)?
   Should user-facing strings say `knope` (golden parity) or `remeris`? The spec assumes `knope.toml` and Knope's
   strings behind one constant. If `remeris.toml` is added, it becomes a second koanf layer (F2.4).
2. **Git backend.** The spec now uses the `git` CLI through `os/exec` (0.3), so `git` must be installed. Is that
   acceptable for every target environment, or do we need a pure-Go fallback (go-git v5) for the read-only calls?
3. **Scope.** Knope Bot (the GitHub App) shares `knope-config` but is **out of scope** here. Confirm.
4. **Diagnostics fidelity.** Is message, help and URL parity enough, or do we want miette-style source snippets for
   config errors in v1?
5. **Jira search endpoint.** Keep `/rest/api/3/search` (as Knope does) or move to `/rest/api/3/search/jql`?
6. **RebaseBranch semantics** (Q14). Confirm that switching to the target branch afterwards is intended.
7. **Go 1.27 minimum.** The spec needs Go 1.27 for the GA `encoding/json/v2`/`jsontext`. Is it acceptable to require
   the latest toolchain from day one?
