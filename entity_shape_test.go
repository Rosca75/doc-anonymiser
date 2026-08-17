// entity_shape_test.go — the guard that keeps negative rules off a value.
//
// A value's spellings are the chips on its card, and nothing else. The
// alternative, a per-value list of spellings the expansion must suppress, is a
// negative rule with no home in the interface: invisible except as the absence
// of a chip, impossible to undo, unlisted anywhere, and doing the job of the
// never-anonymise list, which is the one place a negative rule is meant to live
// and be visible. Deleting a spelling curates the list instead, so the deleted
// spelling is simply not in it.
//
// That decision is easy to undo by accident, on either side of the bridge: a Go
// field added back to Entity, or a frontend reducer writing the old key on the
// entity object it sends. This test is the mechanical version of the decision.
// It fails naming the file, so the fix is obvious.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
)

// retiredEntityField is the JSON key the curated-variant model replaced.
const retiredEntityField = "excludedVariants"

// TestEntityCarriesNoExclusionList marshals a fully populated Entity and
// asserts the retired key is absent from the wire shape. A struct field added
// back with any name would still fail here as long as it serialises under this
// key, which is the shape the session file and the run request carry.
func TestEntityCarriesNoExclusionList(t *testing.T) {
	autoExpand := false
	raw, err := json.Marshal(engine.Entity{
		Category:       engine.CatPersonNames,
		Canonical:      "Marie Duval",
		ManualVariants: []string{"M. Duval"},
		AutoExpand:     &autoExpand,
		Confidence:     engine.ConfidenceManualDefault,
	})
	if err != nil {
		t.Fatalf("could not marshal an Entity: %v", err)
	}
	if strings.Contains(string(raw), retiredEntityField) {
		t.Errorf("engine.Entity serialises %q: the curated-variant model replaced\n"+
			"the per-value exclusion list, so a value's spellings are exactly its chips.\n"+
			"Remove the field and curate the list instead (see AutoExpand).\ngot: %s",
			retiredEntityField, raw)
	}
}

// TestFrontendWritesNoExclusionList sweeps frontend/ for the retired key. The
// frontend is where the field was written from, so the Go-side check alone
// would pass while a reducer kept building the old shape and Go silently
// ignored it: the deletion would look like it worked and then not stick.
func TestFrontendWritesNoExclusionList(t *testing.T) {
	err := filepath.WalkDir("frontend", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".js") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), retiredEntityField) {
			t.Errorf("%s mentions %q: a value's spellings are the chips on its card.\n"+
				"Use curate(e, spellings) from state.js, which drops the spelling from\n"+
				"the list instead of recording a rule that suppresses it.", path, retiredEntityField)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not walk frontend/: %v", err)
	}
}
