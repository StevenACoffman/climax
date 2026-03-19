// Package version provides utilities to get the Go module version
// information.
// This package also allows us to inject build time variables such
// as commit hash.
// When we run this in Docker in Kubernetes, we want to be able to
// introspectively identify what source code generated the runtime we
// encounter.
// See https://github.com/StevenACoffman/small for more information
// Also https://blog.alexellis.io/inject-build-time-vars-golang/
// These variables may be injected at build time in the Docker container
// Please see the README.md for more information as to how this is done here.
package version
