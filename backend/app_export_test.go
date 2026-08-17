// app_export_test.go — the export-layer bound methods' guards.
//
// The Wails runtime refuses a context it was not given by a lifecycle hook, and
// there is none headless, so the clipboard write itself cannot be exercised
// here. What CAN be exercised, and is what matters, is the guard in front of it:
// an empty or over-long input must be refused with a sentence that says what to
// do, before anything reaches the runtime. That is why the guard is its own
// function.
package backend

import (
	"strings"
	"testing"
)

// TestCopyTextRejectsAnEmptySelection: nothing to copy is a mistake worth
// naming, not a silent no-op. A silent success would read as a clipboard that
// stopped working.
func TestCopyTextRejectsAnEmptySelection(t *testing.T) {
	for _, text := range []string{"", "   ", "\n\t"} {
		err := validateCopyText(text)
		if err == nil {
			t.Errorf("CopyText(%q) must be refused", text)
			continue
		}
		if !strings.Contains(err.Error(), "select some text") {
			t.Errorf("the message must say what to do, got: %v", err)
		}
	}
}

// TestCopyTextRejectsAnOverLongSelection: the cap is a MIS-DRAG guard. The
// panel copies a value out of a preview, and a drag that ran away down the pane
// would otherwise push a whole document through the clipboard.
func TestCopyTextRejectsAnOverLongSelection(t *testing.T) {
	err := validateCopyText(strings.Repeat("x", maxCopyTextBytes+1))
	if err == nil {
		t.Fatal("a selection past the cap must be refused")
	}
	for _, want := range []string{"too long", "single value"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message must mention %q, got: %v", want, err)
		}
	}
	// The length is named, because "too long" without a number is not
	// actionable: the user cannot tell how much to trim.
	if !strings.Contains(err.Error(), "characters") {
		t.Errorf("the message must say how long the selection was, got: %v", err)
	}
}

// TestCopyTextAcceptsANormalSelection: a value-sized string passes the guard,
// and so does one exactly at the cap. The bound is inclusive.
func TestCopyTextAcceptsANormalSelection(t *testing.T) {
	if err := validateCopyText("Marie Duval"); err != nil {
		t.Errorf("a normal selection must pass the guard, got: %v", err)
	}
	if err := validateCopyText(strings.Repeat("x", maxCopyTextBytes)); err != nil {
		t.Errorf("the cap is inclusive, got: %v", err)
	}
}
