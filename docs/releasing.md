# Releasing

remeris releases itself: its own `knope.toml` drives three GitHub Actions workflows, in the same way as Knope-based projects such as mizuya. This page explains the flow, what to
set up once, how to cut the first release (v0.0.1), and how users verify a download. For the commands, see [usage.md](usage.md); for the configuration, see
[configuration.md](configuration.md).

## How a release happens

| Step | Workflow | What it does |
| --- | --- | --- |
| 1 | `prepare-release.yaml` | On every push to `main` (and by hand), runs `remeris prepare-release-bot`. It computes the next version from the conventional commits since the last tag, writes `CHANGELOG.md` and `VERSION` on a `release` branch, and opens or updates the `release` pull request. |
| 2 | you | Review the PR (the changelog is its body) and merge it. |
| 3 | `release.yaml` | On the push of the squashed or rebased release commit to `main`, runs `remeris release`: it creates the GitHub release and the `vX.Y.Z` tag at that commit. |
| 4 | `push-artifact.yaml` | The tag push builds the archives, signs and attests them, uploads them to the release, then signs the packslip bundle and uploads it too. |

Design points:

- **Signed commit.** `main` requires signed commits, and a bot cannot sign locally. `.github/scripts/prepare-release-commit.sh` therefore makes the commit through the GitHub API
    with [ghcommit](https://github.com/planetscale/ghcommit), which GitHub signs (Verified) under the identity of the GitHub App. The workflow downloads the ghcommit binary of a
    pinned version with the default `github.token` (read-only), and refuses it unless its SHA-256 is the one written next to the version in the workflow. That pin is what keeps a
    changed or swapped release asset out of a job that holds the App token. Both values change together: see [Renovate](#renovate).
- **App token, not `GITHUB_TOKEN`.** A tag or a push made with the default token does not trigger other workflows. The release and the PR use a token minted for the GitHub App,
    so the tag push starts `push-artifact.yaml`.
- **The release is created on a push to `main`, not on the PR event.** `release.yaml` runs on every push to `main` but only starts its job when the head commit message starts
    with `chore: prepare release`. The title is not trusted: the first step asks the API (`commits/<sha>/pulls`, with the read-only default token) and requires exactly one PR for
    the pushed commit that is merged, from the `release` branch of this repository, into `main`, and opened by the App (`<app-slug>[bot]`). If the API check fails, or the App slug
    or the lookup is missing, nothing is released: read the log and re-run the job (the PR association of a new commit can lag, and the step already retries for 30 seconds). The
    tag and release point at the pushed commit. The `release` environment is restricted to `main` because the run is on `main`.
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

- `remeris_<tag>_<os>_<arch>.tar.xz`, or `.zip` for windows, holding `remeris` (`remeris.exe`), `LICENSE` and `README.md`;
- `SHA256SUMS` for all archives;
- `SHA256SUMS.sigstore.json`, a keyless [cosign](https://docs.sigstore.dev/cosign/signing/overview/) (3.x) signature bundle of `SHA256SUMS`, made with the GitHub OIDC identity of
    the workflow;
- a GitHub build provenance attestation for every archive (stored by GitHub, not a release asset);
- `packslip.sigstore.json`, the signed [packslip](https://packslip.dev) release bundle that mise's `packslip:` backend verifies. It records the project, the version, and the
    platform, format, executable (`remeris`), size and digest of every archive, and links the attestations above. packslip reads the platform and the format from the archive names
    (`linux|darwin|windows`, `amd64|arm64`, `.tar.xz|.zip`), so those names must keep this shape.

The binaries are stripped (`-ldflags '-s -w'`: no symbol table, no DWARF), which is what makes the archives about 2.5 times smaller; see [Archive size](#archive-size).
`mise run build` stays unstripped, for debugging. Stack traces of panics, `debug.ReadBuildInfo` and `remeris --version` are unaffected.

The build is reproducible: the same commit, Go toolchain and GNU tools give byte-identical archives. The build date is the commit time (`SOURCE_DATE_EPOCH`, by default
`git log -1 --format=%ct`), the build user and branch are the fixed strings `release`, the build number stays `unknown`, and the archives have sorted entries, fixed owners, modes
and times; xz runs with `-9e -T1` (one thread, so the output does not depend on the number of CPUs; it does depend on the xz version: the release job runs on `ubuntu-26.04`, which
ships a newer xz than the 5.4.5 used for the measurements below, so the published archives are not byte-identical to a local build with another xz). The task needs GNU tools
(`tar`, `xz`, `zip`, `date`, `sha256sum`), so run it on Linux, not on macOS. `DIST_DIR` changes the output directory and `DIST_TARGETS` (for example `"linux/amd64 windows/arm64"`)
limits the platforms, which is handy to try the task locally.

`push-artifact.yaml` has two jobs, both on the tag push. The `build` job (`contents: read`, pinned to `ubuntu-26.04`: the archives are reproducible only for the same image and tool
versions, and `ubuntu-latest` moves without a change of the file) checks that the tagged commit is on `main` and that the tag matches `VERSION`, builds, and records the SHA-256 of
`SHA256SUMS` and the list of archives. The `publish` job (`contents: write`, `id-token: write`, `attestations: write`) checks out and runs nothing from the repository: it checks
the downloaded files against what the build job recorded, signs `SHA256SUMS`, attests the archives, and uploads exactly the listed archives, `SHA256SUMS` and its signature. Its
last step runs `jdx/packslip` (pinned to the commit of v1.11.1) on the verified local archives with `bin: remeris` and `attest: link` (the attestation of the step before is linked,
not made a second time), signs the packslip with the OIDC identity of the job, and uploads `packslip.sigstore.json` to the release. The packslip step runs after the other assets
are on the release and runs no repository code. Signing and uploading happen in the one job, so the packslip action is trusted with the job's write token: it is pinned to a commit
for that reason, and Renovate keeps the pin current.

## Archive size

Measured on Linux with Go 1.27.1 and xz 5.4.5, from the commit of this change (the sizes grow slowly with the code; another xz version changes them by a few percent at most). The
binary is the file in the archive; "before" is what v0.0.1 shipped (unstripped binary, `tar.gz` at `gzip -9`, `zip` for windows), "after" is what `mise run dist` makes now
(stripped binary, `tar.xz` at `xz -9e` for linux and darwin, `zip` for windows).

| Target | Binary before | Binary after | Archive before | Archive after | Archive saved |
| --- | --- | --- | --- | --- | --- |
| linux/amd64 | 33.5 MB | 23.2 MB | 16.0 MB (`.tar.gz`) | 6.1 MB (`.tar.xz`) | 62% |
| linux/arm64 | 31.0 MB | 21.4 MB | 14.6 MB (`.tar.gz`) | 5.2 MB (`.tar.xz`) | 65% |
| darwin/amd64 | 34.3 MB | 23.9 MB | 16.5 MB (`.tar.gz`) | 6.2 MB (`.tar.xz`) | 62% |
| darwin/arm64 | 32.1 MB | 22.1 MB | 15.4 MB (`.tar.gz`) | 5.5 MB (`.tar.xz`) | 65% |
| windows/amd64 | 34.1 MB | 24.0 MB | 16.4 MB (`.zip`) | 8.4 MB (`.zip`) | 49% |
| windows/arm64 | 31.3 MB | 21.8 MB | 14.8 MB (`.zip`) | 7.4 MB (`.zip`) | 50% |

Where the savings come from, for linux/amd64: stripping alone takes the gzip archive from 16.0 MB to 8.1 MB; xz in place of gzip takes it from 8.1 MB to 6.1 MB. `-ldflags=-s` and
`-ldflags='-s -w'` give the same binary (`-s` implies no DWARF); `-w` alone keeps the symbol table and gives 25.9 MB.

Windows stays `.zip` on purpose. A `.tar.xz` for windows would be about 25% smaller (6.3 MB against 8.4 MB for amd64), but Explorer and PowerShell's `Expand-Archive` open only zip
files, and nothing here tests mise's extraction of `.tar.xz` on Windows. If that is checked, switching is a one-line change in `mise-tasks/dist` (and the `zip` patterns of the
workflow).

### What is in the binary

The stripped linux/amd64 binary is 23.2 MB on disk: `.text` 10.1 MB, `.gopclntab` 8.6 MB, `.go.type` 2.9 MB, `.rodata` 0.7 MB, `.noptrdata` 0.8 MB (`size -A`). The unstripped
binary adds 7.5 MB of DWARF, which `-s -w` removes. The `.gopclntab` (function tables and line numbers for stack traces and the garbage collector) and the type data are needed by
the runtime, and grow with the amount of code. The sections are measured with `size -A`; the contributors with `go tool nm -size -sort size` summed per package family (bss is left
out, it takes no space in the file). `go-size-analyzer` could not be run in the environment where this was measured, so there are no treemap numbers.

| Rank | Contributor | Symbol bytes | Share of the symbols |
| --- | --- | --- | --- |
| 1 | Go runtime, reflect, sync and the other core std packages | 2.8 MB | 24% |
| 2 | `net`, `net/http`, `crypto/tls` and `crypto` of the standard library | 1.9 MB | 17% |
| 3 | go-git with its SSH, OpenPGP and x/crypto dependencies | 1.5 MB | 13% |
| 4 | go-github (with go-querystring and oauth2) | 1.3 MB | 11% |
| 5 | the terminal UI: huh, bubbletea, lipgloss, the charm and ansi libraries | 0.9 MB | 7% |
| 6 | remeris itself | 0.9 MB | 7% |
| 7 | configuration: koanf, go-toml, YAML, mergo | 0.5 MB | 4.5% |
| 8 | urfave/cli | 0.2 MB | 1.6% |

(The symbols total 11.5 MB; the shares do not add up to 100% because 1.3 MB are `go:func` closures data and small packages not listed. The other 11.7 MB of the file are the
`.gopclntab` and the type data, which grow roughly in proportion to the code of each row.)

What would shrink it further, none of it done here (no dependency or code change in this PR), in the order of the likely gain:

1. Dropping go-git: it is only the fallback when the `git` command is missing. About 10 to 14% of the binary, about 1 MB of the xz archive.
2. A thin GitHub client in place of go-github (the Gitea and Jira clients already are on `net/http`): about 10%.
3. A lighter prompt library or hand-written prompts in place of the huh/bubbletea stack: about 5%.

Considered and rejected:

- UPX: xz already gives the same ratio, so the download does not shrink; it costs start-up time and memory, has macOS crash and antivirus false-positive reports, and conflicts
    with the provenance of the binary.
- `garble -tiny`: an obfuscating toolchain; panics lose their stack traces, and the gain (about 15% raw) is small after xz.
- Removing the `.gopclntab`: the runtime needs it to walk stacks, and a linker patch that only drops function names breaks `log` and `testing`.

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
3. Create an Environment named `release` (Settings, Environments). Its deployment branches are `main` only (both workflows that use it run on `main`). Store the two secrets
    **in this environment**, not as repository secrets:

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
- **What the packslip signature proves**: the workflow runs only on a `v*.*.*` tag, which only the App can create, so the real signatures are made at `refs/tags/<tag>`. But a
    modified copy of `push-artifact.yaml` on a branch is not stopped by any check inside the workflow: someone with write access can push such a branch, add a trigger to the copy
    and run it, and that copy can sign a bundle for this repository at `refs/heads/<branch>`. mise with default options and `packslip verify` with no trust flags check the
    repository, not the ref, and would accept such a bundle. Three things reduce this. Strict consumers pin the workflow at a tag (mise `workflow` option, `packslip verify
    --identity-prefix .../push-artifact.yaml@refs/tags/`, see below), and tags are restricted to the App. A ruleset should restrict who can create branches (or who can push a branch
    that changes `.github/workflows/`), for example to the maintainers. Immutable releases would stop anyone from replacing the assets of a published release, which also bounds this
    risk, but the current workflow cannot work with them (see the Releases item). The packslip signature names this workflow file, and mise and packslip remember that
    file as the signer, so do not rename it or move the signing to another file without a plan for the users who pinned the signer.
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
5. `release.yaml` runs on the push of the squashed release commit to `main`: it creates the `v0.0.1` GitHub release with the changelog as notes, and the tag.
6. `push-artifact.yaml` runs on the tag, and attaches the archives, `SHA256SUMS`, `SHA256SUMS.sigstore.json` and `packslip.sigstore.json` to the release. (The real v0.0.1 predates
    the packslip step and is deliberately left without a bundle: mise installs start with the first release made by this workflow.)
7. [Verify](#verify-a-download) one download.

The merge of the release PR pushes a commit that `prepare-release.yaml` skips (see the loop guard). From then on the version is computed from the commits since the last tag. The
same rule as in step 4 holds for every later release: merge the release PR only when the latest Prepare release run is finished and green.

If something fails halfway:

- **Release job failed, no release:** re-run the failed `release.yaml` run (including when its PR check failed: nothing was released). `remeris release` publishes the release for
    a `VERSION` that has no tag.
- **Release exists, no archives:** re-run `push-artifact.yaml` from the Actions tab; `gh release upload` refuses to overwrite, so delete the partial assets first.
- **Archives exist, no `packslip.sigstore.json`** (only the last step of the `publish` job failed; v0.0.1 has no bundle on purpose and stays so): the upload step of the archives
    has already run, and `gh release upload` refuses to overwrite, so a plain re-run of the `publish` job fails at that step. Delete the archives, `SHA256SUMS` and
    `SHA256SUMS.sigstore.json` from the release page (a partial set is also fine to delete), then re-run the `publish` job of the tag run. It downloads the same `dist` artifact
    (kept 7 days), signs and attests again (a second, identical attestation is harmless), uploads everything and the packslip. The packslip upload itself uses `--clobber`, so a
    re-run replaces a bundle. After the 7 days, re-run the whole workflow: the build is reproducible, so the archives have the same hashes.
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
gh attestation verify remeris_v0.0.2_linux_amd64.tar.xz --repo celest-io/remeris \
  --signer-workflow celest-io/remeris/.github/workflows/push-artifact.yaml \
  --source-ref refs/tags/v0.0.2
```

The examples use `v0.0.2` and `.tar.xz`, the archive format of the first release after v0.0.1. The archives of v0.0.1 are `.tar.gz` (and `.zip` for windows), and their names
otherwise follow the same pattern; everything else on this page applies to both.

To pin one version in step 1, replace the regular expression by `--certificate-identity https://github.com/celest-io/remeris/.github/workflows/push-artifact.yaml@refs/tags/v0.0.2`.
Build provenance attestations need a public repository (or GitHub Enterprise Cloud for a private one). After extracting, `remeris --version` prints the tag, the revision and the
build date. Because the build is reproducible, building the tag yourself with `mise run dist -- v0.0.2` on Linux with the same Go version and the same xz version (the compressed
bytes depend on it) gives the same archive hashes.

## Install with mise and verify with packslip

mise installs remeris from the GitHub release when its `packslip:` backend is used. mise checks `packslip.sigstore.json` before it unpacks anything: the signature must be a GitHub
Actions run of `celest-io/remeris`, the signed project and version must be the requested ones, and the archive must have the signed digest and size.

```sh
mise use -g packslip:github.com/celest-io/remeris
```

TODO (fill in after the first release with a `packslip.sigstore.json` exists, and copy the value to the README and to [usage.md](usage.md)): the signer fingerprint, `ps1_` and 26
characters. Take it from the first bundle, on a release you trust, with `packslip pin packslip.sigstore.json`, and publish it where it does not depend on the release (the README).
Until it is filled in, do not pin a fingerprint, and do not copy one from elsewhere. A user then records it apart from the release, for example with
`packslip install github.com/celest-io/remeris --pin ps1_...`.

To check a download by hand with the [packslip CLI](https://packslip.dev/docs/install/):

```sh
V=v0.0.2
curl -fsSLO "https://github.com/celest-io/remeris/releases/download/${V}/packslip.sigstore.json"
curl -fsSLO "https://github.com/celest-io/remeris/releases/download/${V}/remeris_${V}_linux_amd64.tar.xz"
packslip verify packslip.sigstore.json --artifact "remeris_${V}_linux_amd64.tar.xz"   # add --pin ps1_... once the fingerprint is published
packslip show packslip.sigstore.json                                                  # the platforms, formats and executables the bundle records (verifies nothing)
gh attestation verify "remeris_${V}_linux_amd64.tar.xz" --repo celest-io/remeris      # the linked build provenance, which packslip verify does not fetch
```

`packslip verify` with no trust flags requires a run of `celest-io/remeris` as the signer, in any workflow and at any ref. To require this workflow at a tag, add
`--identity-prefix https://github.com/celest-io/remeris/.github/workflows/push-artifact.yaml@refs/tags/ --issuer https://token.actions.githubusercontent.com`; in mise, use the
`workflow` tool option, which accepts tag runs only unless it names another ref. Strict consumers should do this: a bundle signed by a run on a branch (a modified copy of the
workflow, see the ruleset notes) passes the default policy but fails these checks.

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
checksum, for example for the latest release (the archives are `.tar.xz` from the release after v0.0.1; v0.0.1 has `.tar.gz`, so use `tar -xzf` for it):

```yaml
- name: Install the released remeris
  env:
    GH_TOKEN: ${{ github.token }}
  run: |
    set -euo pipefail
    gh release download --repo celest-io/remeris --pattern 'remeris_*_linux_amd64.tar.xz' --pattern SHA256SUMS --dir /tmp/remeris
    ( cd /tmp/remeris && sha256sum --ignore-missing --check SHA256SUMS )
    mkdir /tmp/remeris/bin
    tar -xJf /tmp/remeris/remeris_*_linux_amd64.tar.xz --strip-components=1 --directory /tmp/remeris/bin
    echo /tmp/remeris/bin >> "$GITHUB_PATH"
```

and consider adding `cosign verify-blob` on `SHA256SUMS` as in the previous section, and pinning the version (`gh release download v0.0.1`) with Renovate. Keep the build from
source as a fallback when a release is broken: run the workflow with the build step restored.

## Renovate

`renovate.json` updates the actions (pinned to commit SHAs), the mise tools and, through a regular expression manager, the ghcommit version of `prepare-release.yaml`
(`GHCOMMIT_VERSION`, marked with a `# renovate:` comment). Renovate cannot update the checksum next to it (`GHCOMMIT_SHA256`, the `ghcommit_linux_amd64` line of the release's
`checksums.txt`), so a ghcommit PR from Renovate fails the install step until you edit that line in the same PR. This is deliberate: the pin must be reviewed by a person.
