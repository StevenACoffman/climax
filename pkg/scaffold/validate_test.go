package scaffold_test

import (
	"testing"

	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// TestValidateIdent_rejectsKeywords pins the case proptest found: a keyword
// spells like an identifier, but `climax add func` would write `package func`.
func TestValidateIdent_rejectsKeywords(t *testing.T) {
	for _, name := range []string{"func", "type", "package", "break", "var"} {
		if err := scaffold.ValidateIdent(name); err == nil {
			t.Errorf("ValidateIdent(%q) = nil, want an error", name)
		}
	}
	if err := scaffold.ValidateIdent("funcs"); err != nil {
		t.Errorf("ValidateIdent(%q) = %v, want nil", "funcs", err)
	}
}
