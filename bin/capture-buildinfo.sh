#!/bin/bash
set -euo pipefail

# Record the build info each way of producing a climax binary embeds, as
# fixtures for cmd/version's tests.
#
# What `climax version` reports depends on how the binary was built — a
# goreleaser release, `go install pkg@version`, `go build` in a clean or dirty
# checkout, a source tarball — and checking that by hand means producing each
# kind of binary. This script does that once; the tests then replay every case
# in milliseconds through debug.ParseBuildInfo. Re-run it when the Go toolchain
# or goreleaser changes what it embeds, and review the fixture diff.
#
# Usage: bin/capture-buildinfo.sh [TAG [UNTAGGED_COMMIT]]
#   TAG              a published tag (default: the latest v* tag)
#   UNTAGGED_COMMIT  a published commit with no tag, for the pseudo-version case
#                    (default: TAG~2)
#
# Needs network access (module proxy) and goreleaser on PATH.

# has_cmd NAME — true if NAME is an executable file on $PATH.
# Ignores shell functions, aliases, and builtins of the same name.
has_cmd() {
	if [ -n "${ZSH_VERSION:-}" ]; then
		builtin whence -p -- "$1" >/dev/null 2>&1
	elif [ -n "${BASH_VERSION:-}" ]; then
		builtin type -P -- "$1" >/dev/null 2>&1
	else
		command -v -- "$1" >/dev/null 2>&1
	fi
}

for tool in git go goreleaser; do
	if ! has_cmd "$tool"; then
		echo "capture-buildinfo: $tool is not on PATH" >&2
		exit 1
	fi
done

REPO_ROOT=$(git rev-parse --show-toplevel)
MODULE=$(cd "$REPO_ROOT" && go list -m)
TAG=${1:-$(git -C "$REPO_ROOT" tag --list 'v*' --sort=-v:refname | head -n1)}
UNTAGGED=${2:-$(git -C "$REPO_ROOT" rev-parse "${TAG}~2")}
OUT="$REPO_ROOT/cmd/version/testdata/buildinfo"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$OUT" "$WORK/bin"

# Builds must reflect what users get, not this machine's overrides.
export GOFLAGS='' GOTOOLCHAIN=local

# record NAME BINARY — write BINARY's build info in debug.ParseBuildInfo's format.
record() {
	local name=$1 bin=$2
	{
		printf 'go\t%s\n' "$(go version "$bin" | awk '{print $2}')"
		go version -m "$bin" | sed 1d | sed 's/^\t//'
	} >"$OUT/$name.txt"
	echo "recorded $name"
}

# A clone at TAG, so local builds see a real checkout without touching this one.
git clone --quiet "$REPO_ROOT" "$WORK/src"
cd "$WORK/src"
git checkout --quiet "$TAG"

go build -o "$WORK/bin/c" . && record vcs-tag-clean "$WORK/bin/c"

echo "uncommitted" >>README.md
go build -o "$WORK/bin/c" . && record vcs-tag-dirty "$WORK/bin/c"
git checkout --quiet -- README.md

# A commit past the tag. Fixed identity and dates keep its hash, and so the
# pseudo-version in the fixture, the same on every run.
git checkout --quiet -b capture
GIT_AUTHOR_DATE=2026-09-01T12:00:00Z GIT_COMMITTER_DATE=2026-09-01T12:00:00Z \
	git -c user.name=capture -c user.email=capture@example.com \
	commit --quiet --allow-empty -m "capture: commit past the tag"
go build -o "$WORK/bin/c" . && record vcs-pseudo "$WORK/bin/c"
git checkout --quiet "$TAG"

go build -buildvcs=false -o "$WORK/bin/c" . && record local-novcs "$WORK/bin/c"

mkdir "$WORK/tarball"
git archive "$TAG" | tar -x -C "$WORK/tarball"
(cd "$WORK/tarball" && go build -o "$WORK/bin/c" .) && record local-tarball "$WORK/bin/c"

# From the module proxy, outside any module.
cd "$WORK"
GOBIN="$WORK/gobin" go install "$MODULE@$TAG" && record module-tag "$WORK/gobin/${MODULE##*/}"
rm -rf "$WORK/gobin"
GOBIN="$WORK/gobin" go install "$MODULE@${UNTAGGED:0:12}" && record module-pseudo "$WORK/gobin/${MODULE##*/}"

# The release build: TAG's checkout with this checkout's .goreleaser.yaml, so
# the fixture shows what the next release will embed. The config is passed from
# outside the clone to keep the tree clean, as it is in CI. The -ldflags it
# records are what the tests read the link-time stamp from.
cd "$WORK/src"
cp "$REPO_ROOT/.goreleaser.yaml" "$WORK/goreleaser.yaml"
goreleaser build --config "$WORK/goreleaser.yaml" --single-target --clean \
	--skip=before -o "$WORK/bin/c" >/dev/null && record release-proxy "$WORK/bin/c"
