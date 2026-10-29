#!/usr/bin/env bash
#
# Prints the next release version (e.g. v0.4.0) based on Conventional Commits
# since the latest vX.Y.Z tag reachable from HEAD. Prints nothing when no
# release should be made.
#
#   feat: ...                      -> minor
#   fix: ... / anything else       -> patch
#   feat!: ... / BREAKING CHANGE:  -> major (minor while still on 0.x)
#   [skip release] in HEAD commit  -> no release
#   no tags yet                    -> v0.1.0
set -euo pipefail

if git log -1 --format=%B | grep -qF '[skip release]'; then
  exit 0
fi

latest=$(git tag --merged HEAD --list 'v*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -n1 || true)

if [ -z "$latest" ]; then
  echo "v0.1.0"
  exit 0
fi

range="${latest}..HEAD"

if [ -z "$(git rev-list "$range")" ]; then
  exit 0
fi

IFS=. read -r major minor patch <<<"${latest#v}"

subjects=$(git log --format=%s "$range")
bodies=$(git log --format=%b "$range")

bump=patch
if grep -qE '^feat(\([^)]*\))?:' <<<"$subjects"; then
  bump=minor
fi
if grep -qE '^[a-zA-Z]+(\([^)]*\))?!:' <<<"$subjects" || grep -qE '^BREAKING[ -]CHANGE:' <<<"$bodies"; then
  bump=major
fi

# Before 1.0, breaking changes only bump the minor version.
if [ "$bump" = major ] && [ "$major" -eq 0 ]; then
  bump=minor
fi

case "$bump" in
  major) major=$((major + 1)); minor=0; patch=0 ;;
  minor) minor=$((minor + 1)); patch=0 ;;
  patch) patch=$((patch + 1)) ;;
esac

echo "v${major}.${minor}.${patch}"
