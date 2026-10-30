# Maintaining the repository

This page is for the maintainers of `celest-io/remeris`. It lists the repository settings to apply once, in the order of the GitHub settings pages, so that the contributor rules in
[CONTRIBUTING.md](https://github.com/celest-io/remeris/blob/main/CONTRIBUTING.md) hold. The release pipeline (GitHub App, `release` environment, secrets, tag protection) is
described in [releasing.md](releasing.md) and is not repeated here.

GitHub changes the wording and the layout of its settings pages often. The paths below are the ones at the time of writing; where a label may differ, check the current UI wording
and the [GitHub documentation](https://docs.github.com). You need the Admin role on the repository (or Owner of the organization) for all of it.

## 1. General

Open the repository, then **Settings**, **General**.

1. Features:
    - Check **Issues**.
    - Check **Discussions** (it creates the categories of the next section).
    - Uncheck **Wikis** (the documentation lives in `docs/`).
    - Uncheck **Projects** (use a project of the organization if you want a board).
2. Pull Requests:
    - Uncheck **Allow merge commits** and **Allow rebase merging**. Check **Allow squash merging** and set its default commit message to **Pull request title** (in some versions
        of the page: "Pull request title" under "Default commit message"). The release flow depends on the title: the squashed commit message is what the next release is computed
        from, and `prepare-release.yaml` and `release.yaml` recognise the release commit by its title `chore: prepare release ...` (see
        [releasing.md](releasing.md#how-a-release-happens)).
    - Check **Always suggest updating pull request branches**.
    - Check **Automatically delete head branches**.
    - **Allow auto-merge** is optional. It only helps with the required checks and reviews of the ruleset below; Renovate does not use it (see the [Renovate](#9-renovate)
        section).
3. Leave "Releases: immutable" off, as [releasing.md](releasing.md#branch-and-tag-protection) says.

[releasing.md](releasing.md#branch-and-tag-protection) allows squash and rebase merges, because `prepare-release.yaml` handles both. This page asks for squash only because it gives
one conventional commit per pull request. If you allow rebase merging, every commit of the branch must be a valid conventional commit.

## 2. Discussions

**Settings**, **General**, **Features**, **Discussions**, **Set up discussions** (or the **Discussions** tab, then the pencil next to **Categories**). Create or edit the categories
so that they are exactly these. The format of a category cannot be changed afterwards, so choose it when you create the category. The slug is what the files of
`.github/DISCUSSION_TEMPLATE/` must be named after (the slug is the name lowercased with dashes).

| Category | Format | Slug | Use |
| --- | --- | --- | --- |
| Announcements | Announcement (only maintainers start a discussion, anyone can comment) | `announcements` | Releases and project news. |
| General | Open-ended discussion | `general` | Anything about remeris that is not a question or an idea. |
| Q&A | Question / Answer (a comment can be marked as the answer) | `q-a` | How do I use it, why does it behave like this. |
| Ideas | Open-ended discussion | `ideas` | Proposals to talk about before an issue or a pull request. |
| Show and tell | Open-ended discussion | `show-and-tell` | Workflows and `knope.toml` files people run with remeris. |

GitHub creates default categories (Announcements, General, Ideas, Polls, Q&A, Show and tell) when you enable Discussions; delete or keep **Polls** as you like.

## 3. Labels

**Issues**, **Labels** (or `https://github.com/celest-io/remeris/labels`), **New label**. `bug`, `documentation`, `enhancement`, `good first issue`, `help wanted` and `question`
exist by default in a new repository; edit them if they do not match. Create the others.

| Label | Colour | Meaning |
| --- | --- | --- |
| `bug` | `d73a4a` | Something does not work as the spec or Knope says it should. |
| `enhancement` | `a2eeef` | A new feature or an improvement. |
| `documentation` | `0075ca` | A change to the documentation only. |
| `question` | `d876e3` | A question that belongs in Discussions (move it there and close the issue). |
| `good first issue` | `7057ff` | Small, well described, with the files to touch named. GitHub lists these for new contributors. |
| `help wanted` | `008672` | The maintainers would like help with this. |
| `dependencies` | `0366d6` | A dependency update. Renovate adds it to every pull request (`renovate.json`). |
| `security` | `ee0701` | A security-related change or a vulnerability alert update. Renovate puts it on vulnerability pull requests, so the label must exist. |
| `breaking` | `b60205` | A change that breaks users or differs from Knope on purpose. Needs an entry in `docs/differences.md` and `testdata/deviations.md`. |
| `needs-triage` | `fbca04` | New, not yet looked at by a maintainer. The issue forms may add it; remove it when you triage. |

## 4. Rulesets

**Settings**, **Rules**, **Rulesets**, **New ruleset**, **New branch ruleset** (or **New tag ruleset**). Give each ruleset a name, set **Enforcement status** to **Active**, and add
the targets as written. Use rulesets, not the older branch protection rules. The App is the GitHub App of [releasing.md](releasing.md#the-github-app); create the App before you add
it as a bypass actor (**Bypass list**, **Add bypass**, choose the App from the list of apps installed on the repository).

### `main`

- Target: **Add target**, **Include default branch** (or **Include by pattern**, `main`).
- **Bypass list:** empty. [releasing.md](releasing.md#branch-and-tag-protection) says nobody needs a bypass: the release commit is signed by GitHub through ghcommit and the
    release pull request is merged like any other.
- Rules to check:
    - **Restrict deletions**.
    - **Require linear history**.
    - **Require signed commits**.
    - **Require a pull request before merging**, with: **Required approvals** 1; **Dismiss stale pull request approvals when new commits are pushed**;
        **Require review from Code Owners**; **Require approval of the most recent reviewable push**; **Require conversation resolution before merging** (optional); allowed merge
        methods: **Squash** only.
    - **Require status checks to pass**, with **Require branches to be up to date before merging** (optional, it forces a rebase after every merge), and these checks, which are
        the job names of `.github/workflows/ci.yml`: `ci`, `vet (windows)`, `vet (darwin)` and `mise-lock`. Add each in the **Add checks** box (a check only appears in the list
        after it has run once; pick the GitHub Actions app as the source). The workflow is `CI`; the job `ci` runs `mise run ci`, and the job named `vet (${{ matrix.goos }})`
        expands to `vet (windows)` and `vet (darwin)`; the job `mise-lock` checks that `mise.lock` is current. Check the current UI wording of the check names after the first run.
    - **Block force pushes**.

With a single maintainer, **Require review from Code Owners** and one approval mean that a maintainer cannot merge their own pull request (GitHub does not count the approval of the
author). Either add a second maintainer or code owner, or consciously add the repository admin role to the bypass list while the project is small. Decide this before you enable the
rule; the release pull request is opened by the App, so a maintainer can approve it.

### Branches `release` and `release-staging`

- Target: **Include by pattern**, `release` and `release-staging`.
- **Bypass list:** the GitHub App only, with bypass mode **Always**.
- Rules: **Restrict creations**, **Restrict updates**, **Restrict deletions**. Do not add **Block force pushes**: the App force-pushes both branches on every run
    ([releasing.md](releasing.md#branch-and-tag-protection)).

### Tags `v*`

- **New tag ruleset**, target: **Include by pattern**, `v*`.
- **Bypass list:** the GitHub App only.
- Rules: **Restrict creations**, **Restrict updates**, **Restrict deletions**, **Block force pushes**.

The reasons, and the limit of what the signature proves, are in [releasing.md](releasing.md#branch-and-tag-protection). That page also suggests restricting who can create branches
or change `.github/workflows/`; with the code owner rule above, a change of the workflows needs a code owner anyway.

## 5. Actions

**Settings**, **Actions**, **General**.

- **Actions permissions:** allow actions and reusable workflows. If your organization offers it, choose
    **Allow `celest-io`, and select non-`celest-io`, actions and reusable workflows** and, when the page has the option,
    **Require actions to be pinned to a full-length commit SHA**. Every workflow of the repository already uses full SHAs (the tag is in a comment); Renovate keeps them current.
- **Workflow permissions:** **Read repository contents and packages permissions**. Leave **Allow GitHub Actions to create and approve pull requests** unchecked: the release pull
    request is opened with the App token, not with `GITHUB_TOKEN`.
- **Fork pull request workflows from outside collaborators:** **Require approval for all outside collaborators** (the strictest option on the page), or at least
    **Require approval for first-time contributors**. Fork pull requests then run the CI workflow only after a maintainer approves it. Also check that
    **Send write tokens to workflows from fork pull requests** and **Send secrets and variables to workflows from fork pull requests** stay off. (The CI workflow uses no secret,
    and the `release` environment is limited to `main`.)
- **Artifact and log retention:** the default is fine.

## 6. Security

**Settings**, **Advanced Security** (named **Code security and analysis** in older versions of the page; check the current wording).

1. **Private vulnerability reporting:** **Enable**. This is what [SECURITY.md](https://github.com/celest-io/remeris/blob/main/SECURITY.md) and the Security tab rely on.
2. **Dependency graph:** enable.
3. **Dependabot alerts:** enable. Renovate reads them for its security pull requests (`vulnerabilityAlerts`).
4. **Dependabot security updates:** leave disabled if Renovate handles vulnerabilities, to avoid two pull requests for one problem; enable them if you do not install Renovate.
5. **Secret scanning:** enable, and **Push protection**: enable.
6. **Code scanning** (optional): **Set up**, **Default**, language Go. This runs CodeQL on a schedule and on pull requests with GitHub's own workflow, without a file in the
    repository. If you want a workflow file instead, pin the actions in it to SHAs like the other workflows, and add the job name to the required checks only after it ran.

With `SECURITY.md` in the repository and private reporting enabled, the Security tab offers "Report a vulnerability" to everyone.

## 7. The `release` environment and the secrets

Create the GitHub App, the `release` environment (deployment branches: `main` only) and its two secrets as written in [releasing.md](releasing.md#one-time-setup). Do not create
repository secrets with the same names. Nothing else on this page needs a secret.

## 8. CODEOWNERS and review rules

`.github/CODEOWNERS` assigns every path to a maintainer and lists the sensitive areas separately: `.github/workflows/`, `.github/scripts/`, `mise-tasks/dist`, `knope.toml` and
`docs/releasing.md`. With **Require review from Code Owners** in the `main` ruleset, a pull request that touches them needs the approval of the owner of the path. Keep the file
valid: GitHub shows errors on the file's page (open it in the web UI, "Code owners" menu) when a handle does not exist or has no write access. Owners need the Write role at least.
When the project has a team, replace the handles by `@celest-io/<team>`.

Review rules in practice: a maintainer approves only after `ci`, `vet (windows)`, `vet (darwin)` and `mise-lock` are green; the pull request title is a conventional commit (the
squash message); a change of behaviour has its spec, `docs/differences.md` and `testdata/deviations.md` updates; no secret appears anywhere.

## 9. Renovate

Renovate keeps the dependencies current through `renovate.json`. The ghcommit pin and its checksum are described in [releasing.md](releasing.md#renovate).

### Install

1. Open the [Mend Renovate app](https://github.com/apps/renovate) on GitHub Marketplace and choose **Install** (or **Configure** if it is already installed on the organization).
2. Choose the `celest-io` organization, select **Only select repositories**, and pick `remeris`. Confirm. (Check the current UI wording.)
3. `renovate.json` already exists, so Renovate does not open an onboarding pull request with a new file. Depending on the app version it may still open a "Configure Renovate" pull
    request; the file is valid, so merge it only if it changes nothing you did not expect, or close it.
4. Within a few minutes Renovate creates an issue named **Dependency Dashboard** (`dependencyDashboard` is on in `renovate.json`). The first pull requests follow the schedule, or
    tick them in the dashboard.

### What the configuration does

- Runs on Monday before 06:00 UTC, with at most 5 open pull requests and 2 new ones per hour. A new release must be 3 days old (`minimumReleaseAge`, with
    `internalChecksFilter: "strict"`, so no branch exists before then). Security updates (`vulnerabilityAlerts`) ignore the schedule, are labelled `security`, and are not delayed.
- Titles are `chore(deps): ...`, so they pass the conventional-commit rule and are squash-merged like other changes.
- Groups: non-major Go modules, all GitHub Actions, the mise tools, the non-major npm packages of a future `website/`, and the existing `go toolchain` and `hk` groups. Major
    updates are separate pull requests.
- `testdata/` and the other usual test paths are ignored (`ignorePaths`), so fixtures are never updated.
- **Automerge is off** everywhere: `main` requires signed commits and a human review, and the release pipeline trusts what is merged. Every Renovate pull request is merged by a
    maintainer.
- `postUpdateOptions: ["gomodTidy"]` runs `go mod tidy` for Go updates. Renovate does not run mise, so `mise.lock` is not regenerated (below).

### The dependency dashboard

The issue **Dependency Dashboard** lists the open, pending, rate-limited and ignored updates. Tick the checkbox of an update to create its pull request now (for example one that
waits for the schedule or the release age), and tick **Rebase** next to a pull request to have it rebased. Do not close the issue: Renovate keeps it up to date.

### Review a Renovate pull request

1. Read the release notes in the pull request body. Grouped pull requests mix packages: check each one.
2. `ci`, `vet (windows)`, `vet (darwin)` and `mise-lock` must be green. The squash merge made in the GitHub UI is signed by GitHub, which is what **Require signed commits** checks
    on `main`. If the ruleset still rejects a Renovate pull request, check whether it comes from the commits of its branch (rulesets apply to the commits that land on `main`) and
    look at the current GitHub documentation.
3. To split a group, comment on the dashboard or edit `renovate.json` (a new `packageRules` entry with its own `groupName`, or none).
4. The ghcommit pull request needs a manual edit: Renovate bumps `GHCOMMIT_VERSION` but not `GHCOMMIT_SHA256` (see [releasing.md](releasing.md#renovate)).

### The `mise.lock` step

A change of a tool version in `mise.toml` also needs `mise.lock`, which Renovate cannot regenerate (the hosted Mend Renovate app does not run mise, and `postUpgradeTasks` are not
available in it). The job `mise-lock` of `ci.yml` runs `mise lock --platform linux-x64,linux-arm64,macos-arm64` and fails the pull request, with an error annotation that names the
command, when `mise.lock` differs. The job pins the mise version (`version:` of `jdx/mise-action`, currently 2026.10.21) because the lockfile format changes between mise releases:
write the lockfile with that version (`mise self-update` or `mise use -g mise@2026.10.21`), and change the pin together with the lockfile when you upgrade mise. To fix a mise tool
pull request:

```sh
git fetch origin
git checkout renovate/<branch>        # the branch of the pull request
mise install
mise lock --platform linux-x64,linux-arm64,macos-arm64
git add mise.lock
git commit -m "chore(deps): update mise.lock"
git push
```

If `hk.pkl` and `mise.toml` disagree on the hk version, the `hk` group moves both; check that the three places named in CLAUDE.md agree. `mise.claude.lock` (agent tools only) is
not checked in CI; regenerate it with `mise --env claude lock --platform linux-x64,linux-arm64,macos-arm64` when you change `mise.claude.toml`.

## 10. First contributor checklist

For the maintainers, when a new contributor shows up:

- Welcome them by name in the issue or pull request, and point to [CONTRIBUTING.md](https://github.com/celest-io/remeris/blob/main/CONTRIBUTING.md) if they missed something.
- Approve the workflow run of a first pull request from a fork (**Approve and run workflows**), after you have looked at the diff for changes to `.github/`, `mise-tasks/` and
    `mise.toml`.
- Keep a few issues labelled `good first issue` and `help wanted`, each with the files to touch, the Knope behaviour to match and the test to add.
- Aim to answer a new issue, discussion or pull request within a week, and say so when you cannot. Close issues that need a reply that never comes after a polite reminder.
- Move questions from issues to Discussions (label `question`, then close), and ideas to Discussions, Ideas.
- Thank people, in the pull request and in the release notes.

## 11. Issue triage

The issue forms add `needs-triage` and either `bug` or `enhancement`. A maintainer goes through the issues with `label:needs-triage` (Issues, filter `is:open label:needs-triage`)
at least weekly:

1. **Is it a security problem?** Ask the reporter to delete the details, close the issue as not planned, and point to
    [SECURITY.md](https://github.com/celest-io/remeris/blob/main/SECURITY.md). Hide the comment that holds the details.
2. **Is it a question?** Convert it to a discussion (**Convert to discussion** in the right column of the issue, category Q&A) or label it `question`, answer, and close it.
3. **Duplicate?** Close it as a duplicate (**Close as duplicate** in the close menu) and link the original.
4. **Can it be reproduced?** Ask for what is missing from the form (version, `knope.toml`, `--verbose` output without tokens). Try to reproduce it with the version given.
5. **Compare with Knope.** If Knope 0.23.1 does the same, check [docs/differences.md](differences.md),
    [testdata/deviations.md](https://github.com/celest-io/remeris/blob/main/testdata/deviations.md) and the Quirks register and Decisions at the end of [spec.md](spec.md). Parity
    is the default: a report that matches Knope and the spec is a request for a change (`enhancement`, discussed first), not a bug.
6. **Label it:** `bug`, `enhancement` or `documentation`; add `breaking` if the fix changes behaviour that users rely on; add `good first issue` or `help wanted` when it is
    suitable, and write down the files to touch and the test to add. Remove `needs-triage`.
7. **Answer within a week** (see the checklist above), even if the answer is "not now".

Pull requests: check the title (conventional commit), the checks, that the spec and the deviations file are updated, then review.

## 12. Welcome post for Discussions

Post it in the **Announcements** category after the settings above are in place, and pin it (**Pin discussion**, from the three dots menu). Adapt the text to the current state of
the project.

Title: `Welcome to the remeris community`

```markdown
Welcome, and thank you for being here.

remeris is a release tool written in Go. It starts as a drop-in replacement for [Knope](https://github.com/knope-dev/knope): it reads `knope.toml`, `.changeset/` files and Git
tags, writes the same file edits and changelogs, and prints the same messages. Where it differs on purpose, [docs/differences.md](https://github.com/celest-io/remeris/blob/main/docs/differences.md)
says so.

**Where to go**

- **Q&A:** how do I configure or use remeris?
- **Ideas:** a proposal you want to talk about before it becomes an issue.
- **Show and tell:** your workflows and `knope.toml` files.
- **Issues:** bugs and feature requests, with the forms.
- **Security:** please do not post vulnerabilities here; use the private reporting described in [SECURITY.md](https://github.com/celest-io/remeris/blob/main/SECURITY.md).

**Contributing:** [CONTRIBUTING.md](https://github.com/celest-io/remeris/blob/main/CONTRIBUTING.md) explains the setup (`mise install`), the spec-first rule, the tests and the pull
request flow. Issues labelled `good first issue` are a good start.

Please be kind: we follow the [Code of Conduct](https://github.com/celest-io/remeris/blob/main/CODE_OF_CONDUCT.md). remeris is maintained by volunteers, so answers may take a few
days.
```
