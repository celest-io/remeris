#!/usr/bin/env bash
# Commit the release edits of `remeris prepare-release-bot` and open or update the release PR. The main branch requires signed commits and
# the bot App cannot sign locally, so ghcommit commits through the GitHub API and GitHub signs the commit.
set -euo pipefail

: "${GITHUB_TOKEN:?GITHUB_TOKEN must be set (the create-github-app-token step provides it)}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY must be set to <owner>/<repo> (GitHub Actions sets it)}"

version="${1:?usage: prepare-release-commit.sh <version>}"
branch="release"
staging_branch="release-staging"
base="main"
title="chore: prepare release ${version}"

# mise rewrites its lockfiles for the runner's platform; they are not part of the release.
exclude_paths=(':(exclude,glob)mise*.lock')

# Mirrors planetscale/ghcommit-action's entrypoint.sh.
adds=()
deletes=()
while IFS= read -r -d $'\0' line; do
  index_status="${line:0:1}"
  tree_status="${line:1:1}"

  # A rename's old path is the next NUL-terminated record.
  if [[ "$index_status" == "R" || "$tree_status" == "R" ]]; then
    IFS= read -r -d $'\0' old_filename
    adds+=("${line:3}")
    deletes+=("$old_filename")
    continue
  fi

  filename="${line:3}"
  [[ "$tree_status" =~ A|M|T || "$index_status" =~ A|M|T ]] && adds+=("$filename")
  [[ "$tree_status" == "?" && "$index_status" == "?" ]] && adds+=("$filename")
  [[ "$tree_status" =~ D || "$index_status" =~ D ]] && deletes+=("$filename")
done < <(git status -s --porcelain=v1 -z -- . "${exclude_paths[@]}")

if [[ "${#adds[@]}" -eq 0 && "${#deletes[@]}" -eq 0 ]]; then
  echo "No changes detected, exiting"
  exit 0
fi

# Resetting release to main would leave the open PR with nothing to merge, and GitHub closes it.
git push --force origin "HEAD:refs/heads/${staging_branch}"
trap 'git push --quiet origin --delete "$staging_branch" || true' EXIT

args=()
[[ "${#adds[@]}" -gt 0 ]] && args+=("${adds[@]/#/--add=}")
[[ "${#deletes[@]}" -gt 0 ]] && args+=("${deletes[@]/#/--delete=}")

ghcommit \
  --repository "$GITHUB_REPOSITORY" \
  --branch "$staging_branch" \
  --message "$title" \
  "${args[@]}"

git fetch --quiet origin "refs/heads/${staging_branch}"
git push --force origin "FETCH_HEAD:refs/heads/${branch}"

# The PR body is the newest section of the changelog (from the first "## " heading to the next one) under a sentence that says what
# merging does. The release itself is triggered by the PR's head branch, not by its title or body (see
# .github/workflows/release.yaml).
changelog_entry=""
if [[ -f CHANGELOG.md ]]; then
  changelog_entry="$(awk '/^## /{n++} n==1' CHANGELOG.md)"
fi
body="Merging this PR will tag and release \`v${version}\`."$'\n\n'"${changelog_entry}"

# gh reads the token from the environment, so it never appears in a command line.
export GH_TOKEN="$GITHUB_TOKEN"
pulls_path="repos/${GITHUB_REPOSITORY}/pulls"
open_pr=$(gh api --method GET "$pulls_path" -f head="${GITHUB_REPOSITORY%%/*}:${branch}" -f base="$base" -f state=open \
  --jq '.[0].number // empty')
if [[ -n "$open_pr" ]]; then
  gh api --method PATCH "${pulls_path}/${open_pr}" -f title="$title" -f body="$body" >/dev/null
else
  gh api --method POST "$pulls_path" -f title="$title" -f body="$body" -f head="$branch" -f base="$base" >/dev/null
fi
