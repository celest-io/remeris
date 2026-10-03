# Remeris

Remeris is a Go reimplementation of [Knope](https://github.com/knope-dev/knope): a drop-in replacement
that reads the same `knope.toml`, `.changeset/` files and Git tag formats, and prints the same messages.

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

The config file stays `knope.toml`. Everything else says `remeris`: the binary, help, log and error text,
diagnostic codes, env vars (`REMERIS_*`, with `KNOPE_PRERELEASE_LABEL` as a fallback) and the credential
directory. Keep the name in one constant (`internal/brand`).

## Commands

Tools are pinned in `mise.toml` and tasks are executable files in `mise-tasks/`. There is no Makefile.

```sh
mise install        # Go, golangci-lint, hk, zizmor
mise run build      # build/remeris
mise run test       # go test ./...
mise run lint       # hk check --all
mise run fix        # hk fix --all: formatting and whatever golangci-lint can fix
mise run ci         # build, lint, test: what CI runs
go test ./internal/config -run TestName   # one package or test
```

`go.mod` has `go 1.27` and `toolchain go1.27.1`, so a newer `go` fetches the right toolchain by itself.
Run `mise run lint` before pushing: CI runs the same checks.

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
