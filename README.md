# remeris

Remeris is a release tool written in Go. It reads Knope's `knope.toml`, `.changeset/` files and Git tag formats, writes the same file edits and changelog output, and prints the
same messages.

Status: the first phase is a drop-in replacement for [Knope](https://github.com/knope-dev/knope) 0.23.1. Every step that Knope's `knope.toml` can use is implemented. The
differences are listed in
[docs/differences.md](docs/differences.md).

## Install

With Go:

```sh
go install github.com/celest-io/remeris/cmd/remeris@latest
```

From a checkout, with [mise](https://mise.jdx.dev) (the build task injects the version information):

```sh
mise install
mise run build   # writes build/remeris
```

## Quick start

In a Git repository with a package file such as `Cargo.toml`, `package.json` or `pyproject.toml`, and no `knope.toml`:

```sh
remeris --generate           # write a knope.toml with the release, document-change and get-version workflows
remeris document-change      # record a change file for the next release
remeris release --dry-run    # show what a release would do, changing nothing
remeris release              # prepare and create the release
```

A repository without a `knope.toml` already has these three workflows with the same defaults, so `--generate` only saves them to a file you can edit.

## Documentation

- [docs/usage.md](docs/usage.md): command line, output, exit codes, environment variables and credentials.
- [docs/configuration.md](docs/configuration.md): the `knope.toml` that remeris reads.
- [docs/differences.md](docs/differences.md): where remeris differs from Knope.
- [docs/spec.md](docs/spec.md): the specification, the source of truth for behaviour.

Contributors: see [CLAUDE.md](CLAUDE.md) for the commands and the code layout.
