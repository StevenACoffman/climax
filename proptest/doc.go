// Package proptest holds property-based tests for climax, written with
// pgregory.net/rapid.
//
// It is a module of its own so that rapid stays out of climax's go.mod: the
// main module's tests use the stdlib testing package only (rules.md), and
// rapid is a generator and shrinker rather than an assertion framework, but
// keeping it here makes that boundary something the build enforces instead of
// something a reviewer has to remember. The go.mod replaces climax with the
// working tree, so these properties always check the code beside them.
//
// Being a separate module, it reaches only climax's exported API. Run it with
// `mise run proptest`, or `go test ./... -rapid.checks=1000` from this directory.
package proptest
