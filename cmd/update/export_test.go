// Package update – export_test.go exposes unexported symbols for black-box
// testing in package update_test.
package update

import (
	"io"

	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// PrintReport is a test-only alias for the unexported printReport function.
var PrintReport = func(w io.Writer, total, autoFixCount int, fixable, manual []scaffold.DriftItem) {
	printReport(w, total, autoFixCount, fixable, manual)
}
