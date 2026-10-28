# remeris

Remeris is a release tool written in Go. It reads Knope's `knope.toml`, `.changeset/` files and Git tag formats, writes the same file edits and changelog output, and prints the
same messages.

Status: the first phase is a drop-in replacement for [Knope](https://github.com/knope-dev/knope) 0.23.1. Every step that Knope's `knope.toml` can use is implemented. The
differences are listed in
[docs/differences.md](docs/differences.md).

## Install

With [mise](https://mise.jdx.dev) and its [packslip](https://packslip.dev) backend, which checks the signed release bundle (`packslip.sigstore.json`) before it unpacks anything:

```sh
mise use -g packslip:github.com/celest-io/remeris
```

The signer fingerprint of remeris is `ps1_p2ti4zisstvj4fgdichvcpnhhq` (it comes from the repository ID of `celest-io/remeris`, so it survives a rename). Releases from v0.0.2 on
carry the bundle. To require it with the [packslip CLI](https://packslip.dev/docs/install/):

```sh
packslip install github.com/celest-io/remeris --pin ps1_p2ti4zisstvj4fgdichvcpnhhq
```

mise remembers the signer it accepted (in its local state and in `mise.lock`) and refuses a later release signed by a weaker or different one. See
[docs/releasing.md](docs/releasing.md#install-with-mise-and-verify-with-packslip) for the other ways to verify a download.

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
- [docs/releasing.md](docs/releasing.md): how remeris releases itself, the GitHub App to set up, cutting v0.0.1, installing with mise and verifying a download.
- [docs/differences.md](docs/differences.md): where remeris differs from Knope.
- [docs/spec.md](docs/spec.md): the specification, the source of truth for behaviour.

Contributors: see [CLAUDE.md](CLAUDE.md) for the commands and the code layout.
