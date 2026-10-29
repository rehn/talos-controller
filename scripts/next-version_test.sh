#!/usr/bin/env bash
# Tests for next-version.sh. Run: scripts/next-version_test.sh
set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/next-version.sh"
fails=0

repo() {
  cd "$(mktemp -d)"
  git init -q -b main
  git config user.email t@example.com
  git config user.name test
  git config commit.gpgsign false
  git config tag.gpgsign false
}

commit() { git commit -q --allow-empty -m "$1"; }

expect() {
  local want="$1" desc="$2" got
  got="$("$SCRIPT" 2>&1)" || got="<exit $?> $got"
  if [ "$got" = "$want" ]; then
    echo "ok   - $desc"
  else
    echo "FAIL - $desc: want '$want', got '$got'"
    fails=$((fails + 1))
  fi
}

repo; commit "initial import"
expect "v0.1.0" "first release without tags is v0.1.0"

repo; commit "a"; git tag v0.1.0; commit "fix: crash on empty node list"
expect "v0.1.1" "fix bumps patch"

repo; commit "a"; git tag v0.1.0; commit "update readme"
expect "v0.1.1" "non-conventional commit bumps patch"

repo; commit "a"; git tag v0.1.3; commit "fix: x"; commit "feat(ui): drift view"
expect "v0.2.0" "feat bumps minor and resets patch"

repo; commit "a"; git tag v1.4.2; commit "feat!: new settings format"
expect "v2.0.0" "breaking (!) bumps major on >=1.0"

repo; commit "a"; git tag v1.4.2; commit $'refactor: settings\n\nBREAKING CHANGE: settings.json moved'
expect "v2.0.0" "BREAKING CHANGE footer bumps major"

repo; commit "a"; git tag v0.3.1; commit "feat!: rename api"
expect "v0.4.0" "breaking change on 0.x bumps minor"

repo; commit "a"; git tag v0.1.0
expect "" "no commits since last tag: no release"

repo; commit "a"; git tag v0.1.0; commit "fix: x"; commit "docs: typo [skip release]"
expect "" "[skip release] in HEAD commit: no release"

repo; commit "a"; git tag v0.1.0; commit "b"; git tag v0.2.0; commit "c"; git tag not-a-version; commit "fix: y"
expect "v0.2.1" "uses the latest semver tag and ignores other tags"

repo; commit "a"; git tag v0.9.0; commit "b"; git tag v0.10.0; commit "fix: y"
expect "v0.10.1" "compares versions numerically, not as strings"

if [ "$fails" -gt 0 ]; then
  echo "$fails test(s) failed"
  exit 1
fi
echo "all tests passed"
