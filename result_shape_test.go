// result_shape_test.go — the guard on the result-document wire shape.
//
// The Anonymise screen reads a ResultDocument field by field: `doc.anonymised`
// for the pane, `doc.byCategory` for the subtitle, `doc.occurrenceSpellings` for
// which spelling each occurrence replaced. JavaScript answers `undefined` for a
// key that is not there and says nothing about it, so a field read under a name
// Go never emits is a feature that silently does nothing: the pane still renders,
// every test still passes, and the information simply never arrives.
//
// That is not hypothetical. The spelling behind each mark was read as
// `occurrenceVariants` while Go emitted `occurrenceSpellings`, so the tooltip
// showed the mainText value for every occurrence and the two panes could not be
// linked at all. This file is the mechanical version of the contract: every
// field the view reads off a result document has to be a real key.
package main

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"doc-anonymiser/backend/engine"
)

// resultDocFieldRead finds every `doc.<field>` read in the Anonymise view. In
// that file `doc` is always the result document currently on screen, which is
// what makes the sweep mechanical rather than a hand-kept list: the list is the
// source itself.
var resultDocFieldRead = regexp.MustCompile(`\bdoc\.([a-zA-Z][a-zA-Z0-9]*)`)

// TestAnonymiseViewReadsOnlyRealResultFields holds the view's reads to the JSON
// keys engine.ResultDocument actually serialises.
func TestAnonymiseViewReadsOnlyRealResultFields(t *testing.T) {
	// Every field populated, so the omitempty ones serialise too: a key missing
	// here only because the sample left it zero would flag a correct read.
	raw, err := json.Marshal(engine.ResultDocument{
		Name:                "engagement.md",
		Format:              engine.FormatMD,
		Anonymised:          "[PERSON_1] signed.",
		Grid:                [][]string{{"a"}},
		JSON:                "{}",
		ByCategory:          map[string]int{engine.CatPersonNames: 1},
		Warnings:            []string{"a warning"},
		OccurrenceSpellings: map[string][]string{"[PERSON_1]": {"Borch"}},
	})
	if err != nil {
		t.Fatalf("could not marshal a ResultDocument: %v", err)
	}
	var emitted map[string]json.RawMessage
	if err := json.Unmarshal(raw, &emitted); err != nil {
		t.Fatalf("could not read back the marshalled ResultDocument: %v", err)
	}
	// A set, so the failure message can reuse the sortedKeys helper the other
	// parity guards in this package already share.
	keys := make(map[string]bool, len(emitted))
	for key := range emitted {
		keys[key] = true
	}

	const path = "frontend/views/anonymise.js"
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read %s: %v", path, err)
	}
	for _, match := range resultDocFieldRead.FindAllStringSubmatch(string(source), -1) {
		field := match[1]
		if keys[field] {
			continue
		}
		t.Errorf("%s reads doc.%s, which engine.ResultDocument does not serialise.\n"+
			"JavaScript reads a missing key as undefined and says nothing, so the "+
			"feature behind it renders and does nothing.\nkeys Go emits: %v",
			path, field, sortedKeys(keys))
	}
}
