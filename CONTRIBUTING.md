# Contributing to remeris

Thank you for helping. remeris is a release tool written in Go that re-implements [Knope](https://github.com/knope-dev/knope): it reads Knope's `knope.toml`, `.changeset/` files
and Git tags, writes the same file edits and changelog output, and prints the same messages. Everything below follows from that goal. Please also read the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Ways to contribute

- **Report a bug or ask for a feature.** Search the [existing issues](https://github.com/celest-io/remeris/issues) first, and read [docs/differences.md](docs/differences.md):
    because remeris mirrors Knope, something that behaves like Knope is usually not a bug here. A good bug report has the output of `remeris --version`, your operating system and
    architecture, how you installed remeris, the relevant part of your `knope.toml`, what you expected, what happened, and the output of the command with `--verbose`. Remove any
    token from what you paste.
- **Ask a question or share an idea.** Use [GitHub Discussions](https://github.com/celest-io/remeris/discussions); see [SUPPORT.md](SUPPORT.md).
- **Report a security problem.** Do not open an issue. Follow [SECURITY.md](SECURITY.md).
- **Send a change.** For anything larger than a typo or a small fix, open an issue or a discussion first, so that nobody writes a change that cannot be merged.

## Set up

remeris pins its tools with [mise](https://mise.jdx.dev) (`mise.toml`). With mise installed:

```sh
git clone https://github.com/celest-io/remeris
cd remeris
mise install
```

`mise install` installs Go, golangci-lint, hk, ryl, rumdl, shellcheck, jactionlint and cosign at the versions pinned in `mise.toml`. `go.mod` asks for Go 1.27 and
`toolchain go1.27.1`, so a newer `go` fetches the right toolchain by itself. The tasks are executable files in `mise-tasks/`; there is no Makefile.

| Command | What it does |
| --- | --- |
| `mise run build` | Builds `build/remeris` with the version information injected. A plain `go build` leaves the version as `unknown`. |
| `mise run test` | `go test ./...` |
| `mise run lint` | `hk check --all`: whitespace, YAML, Markdown, `go mod tidy`, golangci-lint and the GitHub Actions workflows. |
| `mise run fix` | `hk fix --all`: formatting and whatever golangci-lint can fix. |
| `mise run ci` | Build, lint and test: what CI runs. Run it before you push. |

One package or test: `go test ./internal/config -run TestName`. To run the hooks of `hk.pkl` on every commit, run `hk install --mise`.

## Where things are

[CLAUDE.md](CLAUDE.md) is the guide to the repository for people and for coding agents: its "Code layout" section lists every package and the direction of imports, and its "Things
that will bite you" section lists the traps (error handling, logging, the dry run). In short: `cmd/remeris` is the entry point, `internal/` holds the command line, the
configuration, the steps and the Git and forge access, `pkg/versioning` is the pure core with no I/O, and `testdata/script` holds the golden tests.

## Spec first, parity with Knope

[docs/spec.md](docs/spec.md) is the source of truth for behaviour. Follow it over your own assumptions. If it is unclear or wrong, say so in the pull request instead of guessing
silently, and change the spec in the same pull request when you change the behaviour.

remeris reproduces Knope's behaviour, including its odd corners, unless the spec lists a deliberate change (the Quirks register Q1 to Q14 and the Decisions D1 to D12 at the end of
the spec say which is which). So:

- A change that makes remeris behave differently from Knope 0.23.1 needs a reason, an entry in [testdata/deviations.md](testdata/deviations.md), and a line in
    [docs/differences.md](docs/differences.md) when users can notice it.
- Use the name `remeris` in help, log and error text, in diagnostic codes (`remeris::...`) and in the credential directory. Each configuration format keeps its own names: Knope's
    are `knope.toml` and the `KNOPE_*` environment variables.
- Error messages: keep Knope's message and help text, with `knope` rewritten to `remeris`. Errors are `diag` errors (a message, a help text, related errors and a cause), returned
    and not logged, and a step never calls `os.Exit`.
- Dependencies: prefer the standard library. Add a module only for a reason given in section 0.3 of the spec, at the version it lists or a newer patch release, and use `go get`
    and `go mod tidy` rather than editing `go.mod` by hand.

## Tests

- Unit tests sit next to the code. Write table-driven tests with a `map[string]struct{...}` table, not a slice.
- Golden tests are `.txtar` scripts in `testdata/script/<suite>/`, run by `cmd/remeris/main_test.go` with the binary registered as `remeris`.
    `UPDATE_SCRIPTS=1 go test ./cmd/remeris` rewrites the files that are compared with `cmp`; read the diff before you commit it.
- To add a fixture, copy a neighbouring script of the same suite, change its input files and commands, and run `go test ./cmd/remeris -run TestScripts/<suite>`. A fixture ported
    from Knope starts with a comment naming its Knope source and is credited in [testdata/README.md](testdata/README.md), which also explains the suites. A fixture that needs a
    feature that is not built yet starts with `skip` and names the milestones. Behaviour that differs from Knope is recorded in `testdata/deviations.md`.
- A step does no I/O in a dry run, and the tests check it.

## Commits, branches and pull requests

- **Commits** follow [Conventional Commits](https://www.conventionalcommits.org): `feat`, `fix`, `chore`, `ci`, `docs`, `refactor`, `test` or `style`, with a scope when it helps,
    an imperative lowercase subject, and a body that says why. Keep them small and focused. The release tooling computes the next version and the changelog from these messages, so
    the type matters: `feat` is a minor version, `fix` a patch, and a `!` or a `BREAKING CHANGE:` footer a breaking change.
- **Branches** are named `feat/...`, `fix/...`, `chore/...` or `docs/...`.
- **Pull requests.** Open a draft pull request and mark it ready when it is. Its title is a conventional commit message too, because the merge uses it. Describe what changed and
    why, and link the issue.
- **Checks.** The CI workflow (`.github/workflows/ci.yml`) runs on every pull request: the job `ci` runs `mise run ci` (build, lint and test), and the jobs `vet (windows)` and
    `vet (darwin)` compile and vet the code for those systems, and the job `mise-lock` checks that `mise.lock` matches `mise.toml` (after you change a tool version, run
    `mise lock --platform linux-x64,linux-arm64,macos-arm64` and commit `mise.lock`). All of them must be green. Run `mise run ci` yourself before you push.
- **Review and merge.** A maintainer reviews the pull request. Pull requests are merged with **squash** (the maintainers' settings in [docs/maintaining.md](docs/maintaining.md)
    allow nothing else), so keep the title right. Keep your branch up to date by rebasing or by using the "Update branch" button.
- **Stacked pull requests.** When a change builds on another one, use the branch of the first as the base of the second, and retarget to `main` when the first one merges.

## Releases

Contributors never edit `CHANGELOG.md` or `VERSION`. remeris releases itself: a workflow computes the next version from the conventional commits on `main` and opens a `release`
pull request that maintainers review and merge, which creates the tag and the GitHub release. [docs/releasing.md](docs/releasing.md) describes the flow and how to verify a
download. If your change needs a changelog entry that no commit describes, run `remeris document-change` (see [docs/usage.md](docs/usage.md)) to write a change file in
`.changeset/`.

## AI-assisted contributions

[CLAUDE.md](CLAUDE.md) exists so that coding agents follow the same rules as people, and you may use an agent. You stay responsible for what you submit: read the diff, run
`mise run ci`, check the behaviour against the spec and against Knope, and be ready to explain every line. Do not submit output that you have not understood, invented commands or
settings, or text that claims a check you did not run. Pull requests that look like unreviewed generated output may be closed.

## Questions

Ask in [GitHub Discussions](https://github.com/celest-io/remeris/discussions) (Q&A category). See [SUPPORT.md](SUPPORT.md).
