package root_test

import (
	"errors"
	"fmt"
	"io/fs"
	"testing"

	"github.com/StevenACoffman/climax/cmd/root"
)

// TestUsageError checks what callers of a usage error rely on: it matches
// ErrUsage, its message is the formatted text alone, and the details come
// back through errors.AsType.
func TestUsageError(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		err     error
		wantMsg string
	}{
		"plain": {
			&root.UsageError{Err: errors.New("add: command name required")},
			"add: command name required",
		},
		"wrapped cause": {
			&root.UsageError{Err: fmt.Errorf("add: --name: %w", fs.ErrInvalid)},
			"add: --name: invalid argument",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if !errors.Is(tc.err, root.ErrUsage) {
				t.Errorf("errors.Is(%v, ErrUsage) = false, want true", tc.err)
			}
			if got := tc.err.Error(); got != tc.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tc.wantMsg)
			}
			usage, ok := errors.AsType[*root.UsageError](tc.err)
			if !ok {
				t.Fatalf("errors.AsType[*UsageError](%v) found nothing", tc.err)
			}
			if got := usage.Err.Error(); got != tc.wantMsg {
				t.Errorf("UsageError.Err = %q, want %q", got, tc.wantMsg)
			}
		})
	}
}

// TestUsageError_keepsCause checks a %w cause stays inspectable, so a command can
// classify an error as usage without hiding what went wrong underneath.
func TestUsageError_keepsCause(t *testing.T) {
	t.Parallel()
	err := &root.UsageError{Err: fmt.Errorf("add: --name: %w", fs.ErrInvalid)}
	if !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("errors.Is(%v, fs.ErrInvalid) = false; the %%w cause was lost", err)
	}
}

// TestErrUsage_onlyUsageErrors keeps the dispatcher's help decision honest:
// errors that are not usage errors, including ExitError and a wrapped
// runtime failure, must not match ErrUsage.
func TestErrUsage_onlyUsageErrors(t *testing.T) {
	t.Parallel()
	cases := map[string]error{
		"runtime failure": errors.New("add: path is not inside a Go module"),
		"exit code":       root.ExitError(1),
		"wrapped runtime": errors.Join(errors.New("lint"), fs.ErrNotExist),
	}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if errors.Is(err, root.ErrUsage) {
				t.Errorf("errors.Is(%v, ErrUsage) = true, want false", err)
			}
		})
	}
}

// TestUsageError_zeroValue keeps a hand-built UsageError with no cause usable:
// it still matches ErrUsage and reports ErrUsage's message instead of panicking.
func TestUsageError_zeroValue(t *testing.T) {
	t.Parallel()
	var err error = &root.UsageError{}
	if !errors.Is(err, root.ErrUsage) {
		t.Errorf("errors.Is(&UsageError{}, ErrUsage) = false, want true")
	}
	if got, want := err.Error(), root.ErrUsage.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
