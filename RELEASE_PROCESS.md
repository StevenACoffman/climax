# Release Process

Releases are published to GitHub using [GoReleaser](https://goreleaser.com). The configuration lives in [`.goreleaser.yaml`](.goreleaser.yaml).

## What GoReleaser does

- Runs `go mod tidy` and `go generate ./...` before building
- Builds binaries for Linux, macOS, and Windows on `amd64`, `arm64`, and `386`
- Packages binaries as `.tar.gz` (`.zip` on Windows)
- Generates a changelog from commits since the previous tag (excluding `docs:` and `test:` prefixes)
- Creates a GitHub release and uploads all artifacts

## Prerequisites

- [GoReleaser](https://goreleaser.com/install/) installed
- A GitHub personal access token with the `repo` scope, exported as `GITHUB_TOKEN`

```sh
export GITHUB_TOKEN=<your-token>
```

## Steps

### 1. Tag the release

```sh
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

Use [semantic versioning](https://semver.org): `vMAJOR.MINOR.PATCH`.

### 2. Run GoReleaser

```sh
goreleaser release --clean
```

`--clean` removes the `dist/` directory before building to ensure a fresh output.

## Testing a release locally

Build and package artifacts without publishing to GitHub:

```sh
goreleaser release --clean --skip=publish
```

Or build a snapshot (no tag required):

```sh
goreleaser release --snapshot --clean
```

Artifacts are written to `dist/`.

## Useful flags

| Flag | Effect |
|---|---|
| `--clean` | Delete `dist/` before building |
| `--skip=publish` | Build and package but do not publish to GitHub |
| `--skip=validate` | Skip dirty-tree and tag checks |
| `--snapshot` | Build without a tag; implies `--skip=publish` |
| `--draft` | Create a draft GitHub release instead of publishing |
