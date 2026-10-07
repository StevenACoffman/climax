# Version

This package was inspired by
[`sigs.k8s.io/release-utils`](https://github.com/kubernetes-sigs/release-utils).
The scaffold template `pkg/scaffold/templates/version.go.tmpl` is the same code
as here with placeholders, and generated apps carry it too.

It has a number of differences from that k8s.io code.

## Changes

Full list of differences from the original library:

- drop all dependencies:
  - use std testing only
  - allow to pass a previously generated ASCII art instead of generating it
    at runtime
- optional overrides:
  - caller can pass one or more functions that change the version info, so
    callers are free to use whatever methods they want to provide some options
  - a range of functions are provided by the library
- added more fields:
  - `URL`
  - `BuiltBy`
  - `Source` and `SourceDetail`: where the binary's source came from (a release,
    a module zip, a VCS checkout, or local files), so an `unknown` field can be
    read as expected for that kind of build rather than as a bug
- link-time release values:
  - `Commit`, `CommitDate`, `TreeState`, and `BuiltBy` variables, set with
    `-ldflags -X` alongside `Version`, restore what a build through the module
    proxy strips from build info
  - a value set at link time wins over build info, and is passed in as a `Stamp`
    so the precedence is testable without globals
- more recovered from build info:
  - the commit and commit time encoded in a pseudo-version, for
    `go install module@<commit>`
  - `GitTreeState: clean` for a module-cache build, whose source is the module
    zip checked against `ModuleSum`
  - no fallback that reported the version string as the commit
- testing
  - added more tests, hopefully preventing breaking changes in the future
  - replay real build info recorded by `bin/capture-buildinfo.sh` for each way
    of producing a binary (goreleaser release, `go install` of a tag or a
    pseudo-version, `go build` in a clean or dirty checkout, without VCS)
  - check the scaffold template's copy gives the same result on every recorded
    build, and that each `-X` target in `.goreleaser.yaml` reaches the output
