# Security policy

## Supported versions

Only the latest release of remeris receives security fixes. Please check that the problem exists in the latest release (`remeris --version`) before you report it.

## Reporting a vulnerability

Do not open a public issue, discussion or pull request for a vulnerability.

Report it privately with GitHub's private vulnerability reporting: open the [Security tab of the repository](https://github.com/celest-io/remeris/security), choose "Report a
vulnerability", and describe the problem. Include the version, the steps to reproduce it, and the impact you see. Do not include working credentials. If that option is not
available, ask a maintainer listed in [.github/CODEOWNERS](.github/CODEOWNERS) for a private way to reach them, without any detail of the problem.

remeris is maintained by volunteers. We will acknowledge a report as soon as we can, work on a fix with you, and credit you in the advisory if you wish.

## Scope

In scope:

- **Credential handling.** How remeris reads, stores and sends tokens for GitHub, Gitea and Jira (environment variables, the stored token files, prompts), and whether a token can
    be sent to a host it should not reach.
- **Token leakage.** A token or other secret that appears in output, logs, error messages, `--verbose` output, generated files or the environment of a command it starts.
- **The release pipeline.** The workflows in `.github/workflows/`, the scripts they run, and anything that lets someone other than the release workflow publish a release, a tag
    or a signed artifact.
- Code execution or file writes outside what a `knope.toml` and its commands are meant to do.

Out of scope: a `Command` step running the command that the `knope.toml` tells it to run (the configuration is trusted), behaviour that matches Knope and has no security impact,
and problems in your own forge account or tokens.

## Verify a release

Releases are built reproducibly and signed. [docs/releasing.md](docs/releasing.md#verify-a-download) shows how to check a download with cosign, `sha256sum` and the GitHub CLI, and
[how to install with mise and verify with packslip](docs/releasing.md#install-with-mise-and-verify-with-packslip), including the signer fingerprint to pin.
