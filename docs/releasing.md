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
    with [ghcommit](https://github.com/planetscale/ghcommit), which GitHub signs (Verified) under the identity of the GitHub App. The workflow downloads a pinned ghcommit version
    and checks its SHA-256 before use. Renovate keeps the pin current.
- **App token, not `GITHUB_TOKEN`.** A tag or a push made with the default token does not trigger other workflows. The release and the PR use a token minted for the GitHub App,
    so the tag push starts `push-artifact.yaml`.
- **The release PR is found by its head branch** (`release`), not by its title.
- **`VERSION`.** The package in `knope.toml` has one versioned file, `VERSION`, a plain text file with the version. The binary does not read it: its version comes from the tag
    through `-ldflags -X`. It exists because `Release` runs after the PR is merged, in a new workflow run, and finds the release to publish by comparing the package version with
    the last tag. A package with no versioned file has no version except its tags, so `Release` would find nothing. `push-artifact.yaml` also refuses a tag that differs from
    `VERSION`.
- **Formatters.** `prepare-release-bot` runs `hk fix --no-stage` after `PrepareRelease`, so the generated changelog passes the same Markdown checks as the rest of the repository.
- **Bootstrap.** The first workflows build remeris from the commit being released (`mise run build`) and use that binary. See [Bootstrap](#bootstrap).

The workflows of `knope.toml`:

| Workflow | Use |
| --- | --- |
| `prepare-release-bot` | The CI path above (signed commit through ghcommit). |
| `prepare-release` | The same result on your machine, with an ordinary commit, `git push` and the `CreatePullRequest` step. Needs a `GITHUB_TOKEN`. |
| `release` | Creates the release and the tag. |
| `document-change` | Writes a change file in `.changeset/` for a change that no conventional commit describes. |

## Artifacts

`mise run dist -- v0.0.1` (the task `mise-tasks/dist`) builds, for linux, darwin and windows on amd64 and arm64, with `CGO_ENABLED=0 go build -trimpath` and the version information
injected like `mise run build` does:

- `remeris_<tag>_<os>_<arch>.tar.gz`, or `.zip` for windows, holding `remeris` (`remeris.exe`), `LICENSE` and `README.md`;
- `SHA256SUMS` for all archives;
- `SHA256SUMS.sigstore.json`, a keyless [cosign](https://docs.sigstore.dev/cosign/signing/overview/) signature bundle of `SHA256SUMS`, made with the GitHub OIDC identity of the
    workflow;
- a GitHub build provenance attestation for every archive (stored by GitHub, not a release asset).

`DIST_DIR` changes the output directory and `DIST_TARGETS` (for example `"linux/amd64 windows/arm64"`) limits the platforms, which is handy to try the task locally.

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
3. Add two repository secrets (Settings, Secrets and variables, Actions):

| Secret | Value |
| --- | --- |
| `RELEASE_BOT_APP_CLIENT_ID` | The Client ID of the App. |
| `RELEASE_BOT_APP_PRIVATE_KEY` | The full contents of the `.pem` file. |

4. Delete the local `.pem` file. The key is only needed by the workflows, which mint a short-lived installation token with `actions/create-github-app-token`, limited to the
    permissions each job asks for.

Until the App and the secrets exist, `prepare-release.yaml` fails at its first step on every push to `main`. The failure is harmless: nothing has been created.

### Branch and tag protection

Use rulesets (Settings, Rules) so the flow works and stays safe:

- **`main`**: require a pull request, require signed commits, require the CI checks. Nobody needs a bypass: the release commit is signed by GitHub through ghcommit, and the
    release PR is merged like any other.
- **Branches `release` and `release-staging`**: the App force-pushes them on every run. Do not protect them against force pushes, or add the App as a bypass actor. Only the App
    (and administrators) should be able to push to them.
- **Tags `v*`**: restrict creation, update and deletion to the App (bypass actor) and to administrators. The tag push is what starts the artifact build, so it should come only
    from the release workflow. Add "block deletions" and "block force pushes".
- **Actions**: Settings, Actions, General: workflow permissions "Read repository contents" by default. The workflows declare their own permissions.

### Cut v0.0.1

`remeris` has no tag yet and no previous version, so a computed first release would be `0.0.0`. Force the first version:

1. Merge the pipeline into `main`, and finish the one-time setup above.
2. Open Actions, **Prepare release**, **Run workflow** on `main`, and enter `0.0.1` as the version (no `v`). The workflow runs
    `remeris prepare-release-bot --override-version 0.0.1`.
3. A pull request named `chore: prepare release 0.0.1` appears, from `release` to `main`, with the changelog as its body. The commit is Verified. Review `CHANGELOG.md` and
    `VERSION` (`0.0.1`) in it.
4. Merge it **before anything else is merged into `main`**. Each push to `main` runs the workflow again without a version and would recompute (and overwrite) the forced release
    PR. If that happens, run the workflow again with `0.0.1`.
5. `release.yaml` runs on the merge: it creates the `v0.0.1` GitHub release with the changelog as notes, and the tag.
6. `push-artifact.yaml` runs on the tag, and attaches the archives, `SHA256SUMS` and `SHA256SUMS.sigstore.json` to the release.
7. [Verify](#verify-a-download) one download.

The merge of the release PR pushes a commit that starts with `chore: prepare release`, which `prepare-release.yaml` skips. From then on the version is computed from the commits
since the last tag.

If something fails halfway:

- **Release job failed, no release:** re-run the failed `release.yaml` run. `remeris release` publishes the release for a `VERSION` that has no tag.
- **Release exists, no archives:** re-run `push-artifact.yaml` from the Actions tab; `gh release upload` refuses to overwrite, so delete the partial assets first.
- **Wrong version in the PR:** run **Prepare release** again with the right version, or close the PR and delete the `release` branch.

## Verify a download

Download the archive for your platform, `SHA256SUMS` and `SHA256SUMS.sigstore.json` from the release page, then:

```sh
# 1. Check the signature of SHA256SUMS: it was made by the push-artifact workflow of this repository, for a tag.
cosign verify-blob SHA256SUMS \
  --bundle SHA256SUMS.sigstore.json \
  --certificate-identity-regexp '^https://github\.com/celest-io/remeris/\.github/workflows/push-artifact\.yaml@refs/tags/v[0-9].*$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

# 2. Check the archive against SHA256SUMS (--ignore-missing: you downloaded one archive only).
sha256sum --ignore-missing --check SHA256SUMS      # macOS: shasum -a 256 --ignore-missing --check SHA256SUMS

# 3. Optional: check the build provenance attestation with the GitHub CLI.
gh attestation verify remeris_v0.0.1_linux_amd64.tar.gz --repo celest-io/remeris
```

To pin one version, replace the regular expression by `--certificate-identity https://github.com/celest-io/remeris/.github/workflows/push-artifact.yaml@refs/tags/v0.0.1`. After
extracting, `remeris --version` prints the tag, the revision and the build date.

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
(`GHCOMMIT_VERSION`, marked with a `# renovate:` comment). The checksum is not stored: the workflow reads it from the `checksums.txt` of the same ghcommit release, so a version
bump needs no other edit.
