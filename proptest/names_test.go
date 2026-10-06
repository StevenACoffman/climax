package proptest_test

import (
	"go/token"
	"strings"
	"testing"
	"unicode/utf8"

	"pgregory.net/rapid"

	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// nameish draws strings that look like identifiers more often than uniform
// noise would: mostly letters, digits, '_' and '-', the reserved words a
// random draw would almost never spell, and the occasional arbitrary rune so
// the validators see what a user might really type.
var nameish = rapid.OneOf(
	rapid.SampledFrom(goKeywords()),
	rapid.StringMatching(`[A-Za-z_][A-Za-z0-9_\-]{0,12}`),
	rapid.StringMatching(`[a-zé日_0-9\-. ]{0,8}`),
	rapid.String(),
)

// goKeywords lists the reserved words, which spell like identifiers but are
// not ones. go/token declares them contiguously from BREAK to VAR.
func goKeywords() []string {
	var kws []string
	for tok := token.BREAK; tok <= token.VAR; tok++ {
		kws = append(kws, tok.String())
	}
	return kws
}

// TestValidateIdent_matchesGoSpec checks ValidateIdent against go/token, the
// standard library's reading of the Go spec. AddCommand uses the name as a
// package name and a directory, so accepting anything the compiler would not
// accept as an identifier produces a scaffold that does not build.
func TestValidateIdent_matchesGoSpec(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := nameish.Draw(t, "name")
		got := scaffold.ValidateIdent(name) == nil
		want := token.IsIdentifier(name)
		if got != want {
			t.Fatalf("ValidateIdent(%q) accepted=%v, go/token.IsIdentifier=%v", name, got, want)
		}
	})
}

// TestValidateIdent_impliesValidCliName checks that every Go identifier is
// also a usable CLI name: the CLI-name alphabet is the identifier alphabet
// plus '-', so a command whose name defaults to its package name is always
// accepted.
func TestValidateIdent_impliesValidCliName(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := nameish.Draw(t, "name")
		if scaffold.ValidateIdent(name) != nil {
			t.Skip("not an identifier")
		}
		if err := scaffold.ValidateCliName(name); err != nil {
			t.Fatalf("ValidateIdent(%q) ok but ValidateCliName: %v", name, err)
		}
	})
}

// TestNormalizeEnvPrefix checks the shape of what NormalizeEnvPrefix produces,
// rather than restating its rules: the output is something a shell will take
// as part of a variable name, normalizing twice changes nothing, and nothing is
// ever added that the input did not ask for.
func TestNormalizeEnvPrefix(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := nameish.Draw(t, "name")
		out := scaffold.NormalizeEnvPrefix(in)

		if i := strings.IndexFunc(out, func(r rune) bool {
			return (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_'
		}); i >= 0 {
			t.Fatalf("NormalizeEnvPrefix(%q) = %q: byte %d is outside [A-Z0-9_]", in, out, i)
		}
		if again := scaffold.NormalizeEnvPrefix(out); again != out {
			t.Fatalf("not idempotent: %q -> %q -> %q", in, out, again)
		}
		if n, m := utf8.RuneCountInString(out), utf8.RuneCountInString(in); n > m {
			t.Fatalf("NormalizeEnvPrefix(%q) = %q grew from %d to %d runes", in, out, m, n)
		}
	})
}
