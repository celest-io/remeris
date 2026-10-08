# Remeris

Remeris is a release tool, written in Go, that supports multiple release configurations. It starts by
re-implementing [Knope](https://github.com/knope-dev/knope) and reusing its ideas: it reads Knope's `knope.toml`,
`.changeset/` files and Git tag formats, writes the same file edits and changelog output, and prints the same
messages. Knope's configuration is the first supported one, not a legacy format: treat it as a peer of the
configurations that follow.

## Read first

`docs/spec.md` is the source of truth. Follow it over your own assumptions. Where it is unclear or wrong,
say so in the PR description instead of guessing silently. The parts you will use most:

- section 0: architecture, package layout (0.2) and dependency choices with versions (0.3);
- section 1: milestones M0-M11, built in order, each one shippable on its own;
- the Quirks register (Q1-Q14) and Decisions (D1-D12) at the end: they say which Knope behaviour to keep
  and which to change.

To compare behaviour or reuse test fixtures, clone Knope into a scratch directory (it is not vendored):
`git clone --depth 1 https://github.com/knope-dev/knope`. Fixtures live in `crates/knope/tests/`.

## Naming (decision D1)

The program is `remeris`: the binary, help, log and error text, diagnostic codes (`remeris::...`) and the
credential directory. Each release configuration keeps the names its own format defines, and the code that
implements the configuration owns them: Knope's is `knope.toml` and the `KNOPE_*` environment variables.
There is no global "brand" package; do not add one. A flag that two formats could share accepts one
environment variable per name, `REMERIS_*` first (for example `REMERIS_PRERELEASE_LABEL` and
`KNOPE_PRERELEASE_LABEL`).

## Commands

Tools are pinned in `mise.toml` and tasks are executable files in `mise-tasks/`. There is no Makefile.

```sh
mise install        # Go, golangci-lint, hk, zizmor
mise run build      # build/remeris, with the version information injected
mise run test       # go test ./...
mise run lint       # hk check --all
mise run fix        # hk fix --all: formatting and whatever golangci-lint can fix
mise run ci         # build, lint, test: what CI runs
go test ./internal/config -run TestName   # one package or test
```

The build task sets the variables of `internal/version` with `-ldflags -X` (version from `git describe`,
revision, branch, build date, Go version, and the build number in GitHub Actions). A plain `go build`
leaves them as `unknown`, so build with `mise run build` when the version matters.

`go.mod` has `go 1.27` and `toolchain go1.27.1`, so a newer `go` fetches the right toolchain by itself.
Run `mise run lint` before pushing: CI runs the same checks.

## MCP servers

`.mcp.json` configures two project servers. Claude Code asks for approval the first time it sees them.
Both start through `mise exec`, so they use the versions pinned in this repo.

- **gopls** (`gopls mcp`, pinned in `mise.claude.toml` and installed with `go install` on first use, so
  the first start is slow). Use it for Go work instead of grepping:
  - at the start of a session, `go_workspace`;
  - to find things, `go_search`; to read a file with its package context, `go_file_context`;
    for a package's public API, `go_package_api`;
  - before changing any exported symbol, `go_symbol_references`; to rename, `go_rename_symbol`;
  - after every edit, `go_diagnostics` on the changed files, then run the tests of the changed packages.
- **hk** (`hk mcp --root .`). Tools: `inspect_project`, `plan`, `start_check`, `start_safe_check`,
  `start_safe_fix`, `get_run`, `get_output`, `get_diff`, `cancel_run`. It runs the same steps as
  `mise run lint` and returns structured diagnostics. Prefer `plan` and `start_safe_check`, read the result
  with `get_run` and `get_output`, and look at `get_diff` before accepting a fix. `mise run lint` stays the
  last check before pushing; `mise run ci` is what CI runs.

## Claude Code settings

`.claude/settings.json` is shared by everyone; put personal overrides in `.claude/settings.local.json`
(git-ignored, like `mise.local.toml`).

- **Plan mode is the default.** Propose the approach, then implement it.
- **Allowed without asking:** reading and searching, `mise run`, `go build/test/vet/get/mod tidy`, `golangci-lint`,
  `hk check` and `hk fix`, local git (`add`, `commit`, `branch`, `checkout`, `diff`, `log`, `status`,
  `worktree`, `fetch`, `pull`), the gopls tools and the read-only hk tools.
- **Asks first:** `git push`, and the hk tools that run fixers (`start_check`, `start_safe_fix`).
- **Denied:** `find`, `grep` and `tree` (slow: use `fd` and `rg`), `sudo`, `rm -rf`, and reading `.env` files,
  SSH and cloud credentials.
- **Sandbox:** on, with unsandboxed commands off and network limited to the hosts the toolchain needs (GitHub,
  the Go module proxy and checksum database, the Go downloads, mise). Go caches go to `/tmp/claude`. If a
  command needs another host, add it to `sandbox.network.allowedDomains` instead of working around it.
- **Plugins:** `gopls-lsp`, `code-review` and `code-simplifier` from the official marketplace. The LSP tool is
  switched on by `ENABLE_LSP_TOOL` in `mise.claude.toml`.
- **Cloud sessions:** the SessionStart hook runs `scripts/setup_remote.sh`, which does nothing on a laptop.
  In a remote session it installs mise (checking the installer's signature), selects `MISE_ENV=claude` and runs
  `mise install`. If a tool is missing there, read that script's output first.

## Lint and style

- `hk.pkl` runs hk builtins only: whitespace, end-of-file, line endings, symlinks, merge conflicts, private
  keys, `go mod tidy`, golangci-lint (format and lint) and zizmor for the workflows.
- `.golangci.yaml` is ReadyKit's config. It runs on tests too. Notable rules: `goconst` (3 or more uses of a
  string literal become a constant, tests included), `gosec`, `revive`, line length 128 (golines), imports
  grouped with this module last (goimports). Fix the code, not the config. A `//nolint` needs a reason.
- Follow Effective Go. Write table-driven tests with `map[string]struct{...}` tables, not slices.
- Do not edit `go.mod` by hand: use `go get` and `go mod tidy`.

## Git and PRs

- Conventional commits (`feat`, `fix`, `chore`, `ci`, `docs`, `refactor`, `test`, `style`), with a scope when it
  helps, an imperative lowercase subject and a body that says why. Small, focused commits.
- Branches: `feat/...`, `fix/...`, `chore/...`. Open draft PRs. Stack a PR on another by using its branch as the
  base; retarget to `main` when the one below merges.
- GitHub Actions are pinned to commit SHAs with the tag in a comment, and Renovate keeps them current
  (`renovate.json`). Never use a floating tag in a workflow. zizmor audits the workflows, and its online
  audits need `GH_TOKEN` (CI sets it).
- Dependencies: prefer the standard library. Add a module only for the reasons given in spec 0.3, at the
  versions it lists or newer patch releases.

## Code layout

Milestone M0 is in place: `remeris <workflow>` runs `Command` steps and every other step fails with
`Step X is not implemented yet (milestone Mx)`. The pure versioning core is growing under
`pkg/versioning` (milestones M1 to M5, tracked in issue #5). Not built yet: versioned files, packages, git,
changes, forges, prompts, discovery, `--generate`, `--upgrade` (see spec section 1).

```
cmd/remeris/        entry point (calls cli.Main); main_test.go runs the testscript golden tests
internal/diag/      Diagnostic interface and the renderer for errors (message, help, related errors)
internal/app/       GlobalOptions, WorkflowOptions and the generic LoadConfig[T]
internal/cli/       root command, one subcommand per workflow, logging, --validate
internal/config/    Knope configuration (knope.toml) loading: koanf + a go-toml unstable index for key order and spans
internal/workflow/  Step interface, State, RunType (real or dry run), runner
internal/variables/ Template and Variable substitution
internal/steps/     one file per step (stubs for the unbuilt ones); steps/command splits command lines
internal/fsx/       dry-run aware file writes
testdata/script/    golden tests; testdata/README.md explains the ported Knope fixtures
pkg/versioning/     pure core (the knope-versioning equivalent): no file system, network, exec, env or clock
  semver/           full SemVer 2.0.0 Version: Parse, Compare, rules, bumps, PackageVersions
  action/           Action (WriteToFile, RemoveFile, AddTag, CreateRelease), Release, ReleaseTag: a leaf package
  versionedfile/    Format table, Config, the File interface and one file per format
```

Imports flow one way: `workflow` imports only `diag`, then `variables`, `steps`, `config`, `cli`. Keep it so.
`pkg/versioning` may import `internal/diag` (pure) and nothing else from `internal/`.
Keep the pure core free of I/O; steps do the I/O and must do none in a dry run.

### Adding or finishing a step

1. Every step has its own file in `internal/steps/`, with `toml:"..."` tags on its fields. A step that is not built
   yet has a stub whose `Run` returns `notImplemented(s, "Mx")` (in `not_implemented.go`): replace that body.
2. Implement `Type()` and `Run(ctx, rt)`. In a dry run, log what would happen and change nothing.
3. Make sure it is in `stepConstructors` in `internal/config/decode.go`.
4. Add unit tests, and enable the Knope fixtures that needed it (remove their `skip` line).

### Things that will bite you

- **Errors:** return a `diag` error (`diag.New(...).WithHelp(...)`, `WithRelated`, `WithCause`); never log
  an error and never call `os.Exit` in a step. `cli.Main` renders it to stderr and exits 1. Match Knope's
  message and help text with `knope` rewritten to `remeris`. Wrap step errors with `workflow.Problem`.
- **Logging:** `slog` to stdout through a plain handler (no level or time). `--validate` silences it.
- **`LoadConfig` and cliflagv3:** keys are prefixed with the command lineage (`remeris`, `remeris.release`) and
  only the given command's own flags are readable. Load `GlobalOptions` from the root command
  (`cmd.Root()`) and `WorkflowOptions` from the workflow command.
- **urfave/cli:** backticks in a flag's usage text turn the next word into the flag's placeholder; avoid them.
  Its default `--version` flag claims `-v`, so `cli.VersionFlag` is replaced (`--version`, `-V`).
- **`knope.toml` decoding:** koanf uses the key delimiter `\x1f` so keys with dots survive. Maps lose order,
  so `config/index.go` re-reads the file to restore the order of `packages` and `variables`. Unknown keys
  are ignored on purpose (Quirk Q1). Workflow names must match `^[A-Za-z0-9_-]+$`.
- **Variables:** `Template.Variables` nil means the `variables` key was absent (defaults `$version` and
  `$changelog` apply); an empty non-nil slice means `variables = {}` (substitution off).
- **Command step:** `shell = true` runs `sh -c` (`cmd /C` on Windows); otherwise the line is split by
  `internal/steps/command.Split` and run directly, with no `$VAR` expansion. The Windows path compiles but
  is not run in CI.

### Tests

- Unit tests sit next to the code. `internal/cli` tests build the command tree in process.
- Golden tests are `.txtar` scripts in `testdata/script/<suite>/`, run by `cmd/remeris/main_test.go` with the
  binary registered as `remeris`. `UPDATE_SCRIPTS=1 go test ./cmd/remeris` rewrites files compared with
  `cmp`; read the diff before committing. `--help` is compared by content, not byte for byte.
- Ported Knope fixtures start with a comment naming their Knope source and are credited in
  `testdata/README.md`. A fixture that needs an unbuilt feature starts with `skip` and names the milestones.
  Record behaviour that differs from Knope in `testdata/deviations.md`.
