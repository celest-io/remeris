# Releasing

remeris releases itself: its own `knope.toml` drives three GitHub Actions workflows, in the same way as Knope-based projects such as mizuya. This page explains the flow, what to
set up once, how to cut the first release (v0.0.1), and how users verify a download. For the commands, see [usage.md](usage.md); for the configuration, see
[configuration.md](configuration.md).

## How a release happens

| Step | Workflow | What it does |
| --- | --- | --- |
| 1 | `prepare-release.yaml` | On every push to `main` (and by hand), runs `remeris prepare-release-bot`. It computes the next version from the conventional commits since the last tag, writes `CHANGELOG.md` and `VERSION` on a `release` branch, and opens or updates the `release` pull request. |
| 2 | you | Review the PR (the changelog is its body) and merge it. |
| 3 | `release.yaml` | When a PR whose head branch is `release` is merged, runs `remeris release`: it creates the GitHub release and the `vX.Y.Z` tag. |
| 4 | `push-artifact.yaml` | The tag push builds the archives, signs and attests them, and uploads them to the release. |

Design points:

- **Signed commit.** `main` requires signed commits, and a bot cannot sign locally. `.github/scripts/prepare-release-commit.sh` therefore makes the commit through the GitHub API
    with [ghcommit](https://github.com/planetscale/ghcommit), which GitHub signs (Verified) under the identity of the GitHub App. The workflow downloads the ghcommit binary of a
    pinned version with the default `github.token` (read-only), and refuses it unless its SHA-256 is the one written next to the version in the workflow. That pin is what keeps a
    changed or swapped release asset out of a job that holds the App token. Both values change together: see [Renovate](#renovate).
- **App token, not `GITHUB_TOKEN`.** A tag or a push made with the default token does not trigger other workflows. The release and the PR use a token minted for the GitHub App,
    so the tag push starts `push-artifact.yaml`.
- **The release PR is found by its head branch** (`release`), not by its title. `release.yaml` also checks that the PR is from this repository and was opened by the App.
- **`VERSION`.** The package in `knope.toml` has one versioned file, `VERSION`, a plain text file with the version. The binary does not read it: its version comes from the tag
    through `-ldflags -X`. It exists because `Release` runs after the PR is merged, in a new workflow run, and finds the release to publish by comparing the package version with
    the last tag. A package with no versioned file has no version except its tags, so `Release` would find nothing. `push-artifact.yaml` also refuses a tag that differs from
    `VERSION`. Do not edit `VERSION` or `CHANGELOG.md` by hand.
- **Formatters.** `prepare-release-bot` runs `hk fix --no-stage` after `PrepareRelease`, so the generated changelog passes the same Markdown checks as the rest of the repository.
- **Loop guard.** The merge of the release PR pushes to `main`, and that push must not prepare the same release again before the tag exists. `prepare-release.yaml` skips a push
    whose head commit message starts with `chore: prepare release` (a squash or rebase merge keeps the PR title as the commit message) or is the merge commit of a PR from the
    `release` branch. Merge the release PR with squash or rebase only, as the ruleset below says, so the first condition is the one that matters.
- **Bootstrap.** The first workflows build remeris from the commit being released (`mise run build`) and use that binary. See [Bootstrap](#bootstrap).

The workflows of `knope.toml`:

| Workflow | Use |
| --- | --- |
| `prepare-release-bot` | The CI path above (signed commit through ghcommit). |
| `prepare-release` | The same result on your machine, with an ordinary commit, `git push` and the `CreatePullRequest` step. Needs a `GITHUB_TOKEN`. |
| `release` | Creates the release and the tag. |
| `document-change` | Writes a change file in `.changeset/` for a change that no conventional commit describes. |

## Artifacts

`mise run dist -- v0.0.1` (the task `mise-tasks/dist`) builds, for linux, darwin and windows on amd64 and arm64, with `CGO_ENABLED=0 go build -trimpath -buildvcs=false` and the
version information injected like `mise run build` does:

- `remeris_<tag>_<os>_<arch>.tar.gz`, or `.zip` for windows, holding `remeris` (`remeris.exe`), `LICENSE` and `README.md`;
- `SHA256SUMS` for all archives;
- `SHA256SUMS.sigstore.json`, a keyless [cosign](https://docs.sigstore.dev/cosign/signing/overview/) (3.x) signature bundle of `SHA256SUMS`, made with the GitHub OIDC identity of
    the workflow;
- a GitHub build provenance attestation for every archive (stored by GitHub, not a release asset).

The build is reproducible: the same commit, Go toolchain and GNU tools give byte-identical archives. The build date is the commit time (`SOURCE_DATE_EPOCH`, by default
`git log -1 --format=%ct`), the build user and branch are the fixed strings `release`, the build number stays `unknown`, and the archives have sorted entries, fixed owners, modes
and times, and no gzip name or time. The task needs GNU tools (`tar`, `gzip`, `zip`, `date`, `sha256sum`), so run it on Linux, not on macOS. `DIST_DIR` changes the output directory
and `DIST_TARGETS` (for example `"linux/amd64 windows/arm64"`) limits the platforms, which is handy to try the task locally.

`push-artifact.yaml` has two jobs. The `build` job (read-only token) checks that the tagged commit is on `main` and that the tag matches `VERSION`, builds, and records the SHA-256
of `SHA256SUMS` and the list of archives. The `publish` job holds the write permissions but checks out and runs nothing from the repository: it checks the downloaded files against
what the build job recorded, signs, attests, and uploads exactly the listed archives, `SHA256SUMS` and the bundle.

## One-time setup

### The GitHub App

Create a GitHub App owned by the `celest-io` organization (Settings, Developer settings, GitHub Apps), for example named `remeris-release-bot`:

- Webhook: off. Homepage URL: the repository URL.
- Repository permissions: **Contents** read and write, **Pull requests** read and write, **Metadata** read (mandatory, always read-only).
- No organization or account permissions. No user authorization.
- Where it can be installed: only on this account.

Then:

1. Install the App on `celest-io/remeris` only.
2. Generate a private key (a `.pem` file) on the App page and note the **Client ID** (starts with `Iv`).
3. Create an Environment named `release` (Settings, Environments). Its deployment branches are `main` and the head ref `release` (the `release.yaml` run is a `pull_request` event
    from the release PR). Store the two secrets **in this environment**, not as repository secrets:

| Environment secret | Value |
| --- | --- |
| `RELEASE_BOT_APP_CLIENT_ID` | The Client ID of the App. |
| `RELEASE_BOT_APP_PRIVATE_KEY` | The full contents of the `.pem` file. |

4. Delete the local `.pem` file. The key is only needed by the workflows, which mint a short-lived installation token with `actions/create-github-app-token`, limited to the
    permissions each job asks for.

The jobs that mint the token (`prepare-release` and `release`) declare `environment: release`. The environment is what limits the App key to runs on `main` and on the release PR. A
branch pushed by someone with write access cannot read it, whatever workflow it carries. Do not also create repository secrets with the same names.

Until the App and the secrets exist, `prepare-release.yaml` fails at its first step on every push to `main`. The failure is harmless: nothing has been created.

### Branch and tag protection

The environment above and the tag ruleset below are what give a release signature its meaning. The cosign certificate only says "the push-artifact workflow of this repository ran
for a `v*` tag". It is trustworthy only if just the release workflow can create such a tag and just `main` runs it with the key. Use rulesets (Settings, Rules):

- **`main`**: require a pull request, require signed commits, require the CI checks, require approval of the most recent reviewable push, and dismiss stale approvals when new
    commits are pushed. Allow only **squash** and **rebase** merges (disable "Allow merge commits"). Nobody needs a bypass: the release commit is signed by GitHub through ghcommit,
    and the release PR is merged like any other.
- **Branches `release` and `release-staging`**: restrict creation and updates (and deletion) to the GitHub App, as the only bypass actor. Nobody else, administrators included,
    should push there by hand: the content of `release` is what gets released. The App force-pushes both branches on every run, so do not add "block force pushes" for the App.
- **Tags `v*`**: restrict creation, update and deletion to the App (bypass actor), and add "block deletions" and "block force pushes". The tag push is what starts the artifact
    build, so it must come only from the release workflow.
- **Actions**: Settings, Actions, General: workflow permissions "Read repository contents" by default. The workflows declare their own permissions.
- **Releases**: leave "immutable releases" **off** for this repository. `push-artifact.yaml` attaches the archives after the release exists, and an immutable release rejects
    that. (The alternative is a draft release that is published after the upload, which needs a change to the workflows.)

### Cut v0.0.1

With `VERSION` at `0.0.0` and no tag, the computed first release is `0.0.1` today. Force it anyway: a later `feat` or breaking commit would change the computed version, and the
first release must be `0.0.1` whatever lands first.

1. Merge the pipeline into `main`, and finish the one-time setup above.
2. Open Actions, **Prepare release**, **Run workflow** on `main`, and enter `0.0.1` as the version (no `v`). The workflow runs
    `remeris prepare-release-bot --override-version 0.0.1`.
3. A pull request named `chore: prepare release 0.0.1` appears, from `release` to `main`, with the changelog as its body. The commit is Verified. Review `CHANGELOG.md` and
    `VERSION` (`0.0.1`) in it.
4. Merge it **only when the latest Prepare release run has finished and is green**, and **before anything else is merged into `main`**. Each push to `main` runs the workflow again
    without a version and would recompute (and overwrite) the forced release PR. A run that is still going can replace the PR content right after you reviewed it. If a push slipped
    in, run the workflow again with `0.0.1` and review again.
5. `release.yaml` runs on the merge: it creates the `v0.0.1` GitHub release with the changelog as notes, and the tag.
6. `push-artifact.yaml` runs on the tag, and attaches the archives, `SHA256SUMS` and `SHA256SUMS.sigstore.json` to the release.
7. [Verify](#verify-a-download) one download.

The merge of the release PR pushes a commit that `prepare-release.yaml` skips (see the loop guard). From then on the version is computed from the commits since the last tag. The
same rule as in step 4 holds for every later release: merge the release PR only when the latest Prepare release run is finished and green.

If something fails halfway:

- **Release job failed, no release:** re-run the failed `release.yaml` run. `remeris release` publishes the release for a `VERSION` that has no tag.
- **Release exists, no archives:** re-run `push-artifact.yaml` from the Actions tab; `gh release upload` refuses to overwrite, so delete the partial assets first.
- **Wrong version in the PR:** run **Prepare release** again with the right version, or close the PR and delete the `release` branch (as the App, or as an administrator who lifts
    the ruleset for the moment).

## Verify a download

Download the archive for your platform, `SHA256SUMS` and `SHA256SUMS.sigstore.json` from the release page, then:

```sh
# 1. Check the signature of SHA256SUMS (cosign 3.x): it was made by the push-artifact workflow of this repository, for a tag.
cosign verify-blob SHA256SUMS \
  --bundle SHA256SUMS.sigstore.json \
  --certificate-identity-regexp '^https://github\.com/celest-io/remeris/\.github/workflows/push-artifact\.yaml@refs/tags/v[0-9].*$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

# 2. Check the archive against SHA256SUMS (--ignore-missing: you downloaded one archive only).
sha256sum --ignore-missing --check SHA256SUMS      # macOS: shasum -a 256 --ignore-missing --check SHA256SUMS

# 3. Optional: check the build provenance attestation with the GitHub CLI.
gh attestation verify remeris_v0.0.1_linux_amd64.tar.gz --repo celest-io/remeris \
  --signer-workflow celest-io/remeris/.github/workflows/push-artifact.yaml \
  --source-ref refs/tags/v0.0.1
```

To pin one version in step 1, replace the regular expression by `--certificate-identity https://github.com/celest-io/remeris/.github/workflows/push-artifact.yaml@refs/tags/v0.0.1`.
Build provenance attestations need a public repository (or GitHub Enterprise Cloud for a private one). After extracting, `remeris --version` prints the tag, the revision and the
build date. Because the build is reproducible, building the tag yourself with `mise run dist -- v0.0.1` on Linux with the same Go version gives the same archive hashes.

## Bootstrap

The workflows `prepare-release.yaml` and `release.yaml` first build remeris from the commit they act on:

```yaml
- name: Build remeris from this commit
  run: |
    mise run build
    echo "${GITHUB_WORKSPACE}/build" >> "$GITHUB_PATH"
```

This needs no earlier release, which is why v0.0.1 can be cut, and it tests the tool on itself. The cost is that a regression of `PrepareRelease` or `Release` breaks the release
that should ship the fix, and the build adds about a minute.

Once a release exists, you can switch to the last released binary (a stable tool releasing the next version). Replace the build step in both workflows by a download that checks the
checksum, for example for the latest release:

```yaml
- name: Install the released remeris
  env:
    GH_TOKEN: ${{ github.token }}
  run: |
    set -euo pipefail
    gh release download --repo celest-io/remeris --pattern 'remeris_*_linux_amd64.tar.gz' --pattern SHA256SUMS --dir /tmp/remeris
    ( cd /tmp/remeris && sha256sum --ignore-missing --check SHA256SUMS )
    mkdir /tmp/remeris/bin
    tar -xzf /tmp/remeris/remeris_*_linux_amd64.tar.gz --strip-components=1 --directory /tmp/remeris/bin
    echo /tmp/remeris/bin >> "$GITHUB_PATH"
```

and consider adding `cosign verify-blob` on `SHA256SUMS` as in the previous section, and pinning the version (`gh release download v0.0.1`) with Renovate. Keep the build from
source as a fallback when a release is broken: run the workflow with the build step restored.

## Renovate

`renovate.json` updates the actions (pinned to commit SHAs), the mise tools and, through a regular expression manager, the ghcommit version of `prepare-release.yaml`
(`GHCOMMIT_VERSION`, marked with a `# renovate:` comment). Renovate cannot update the checksum next to it (`GHCOMMIT_SHA256`, the `ghcommit_linux_amd64` line of the release's
`checksums.txt`), so a ghcommit PR from Renovate fails the install step until you edit that line in the same PR. This is deliberate: the pin must be reviewed by a person.
