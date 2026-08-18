// value_shape_test.go — the guard on the Value wire shape.
//
// Two decisions live in this file, both easy to undo by accident on either side
// of the bridge, and both invisible in ordinary testing when they are undone.
//
// NO NEGATIVE RULE ON A VALUE. A Value's spellings are the chips on its card,
// and nothing else. The alternative, a per-Value list of spellings the expansion
// must suppress, is a negative rule with no home in the interface: invisible
// except as the absence of a chip, impossible to undo, unlisted anywhere, and
// doing the job of the never-anonymise list, which is the one place a negative
// rule is meant to live and be visible. Deleting a spelling CURATES the list
// instead, so the deleted spelling is simply not in it.
//
// ONE FIELD PER QUESTION. Provenance and precedence are separate: a Value
// carries discoveryMethods, and the match class is derived when a contest has to
// be decided. A single field answering both is what let raising the confidence
// floor silently reorder which route won.
//
// A Go field added back, or a frontend reducer writing an old key onto the object
// it sends, would pass every other test in the repository. This file is the
// mechanical version of both decisions, and it fails naming the key.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
)

// retiredValueFields are the JSON keys the current Value model replaced. Each
// one is a shape that has to stay gone, not merely be unused:
//
//	excludedVariants  the per-Value suppression list the curated model replaced
//	manualVariants    spellings under their old name; a reader that still wrote
//	                  it would have its spellings silently ignored
//	autoExpand        the boolean spellingPolicy replaced
//	canonical         main text under its old name
//	origin            the single field that answered provenance AND precedence
var retiredValueFields = []string{
	"excludedVariants", "manualVariants", "autoExpand", "canonical", "origin",
}

// TestValueWireShapeCarriesNoRetiredField marshals a fully populated Value and
// asserts every retired key is absent. A struct field added back under any Go
// name still fails here as long as it serialises under one of these keys, which
// is the shape the session file and the run request carry.
func TestValueWireShapeCarriesNoRetiredField(t *testing.T) {
	raw, err := json.Marshal(engine.Value{
		Category:         engine.CatPersonNames,
		MainText:         "Marie Duval",
		Spellings:        []string{"M. Duval"},
		SpellingPolicy:   engine.SpellingPolicyCurated,
		DiscoveryMethods: []string{engine.MethodHeuristic, engine.MethodLocalAI},
		Evidence: []engine.Evidence{{
			Kind:           engine.EvidenceEmailLocalPart,
			SignalCategory: engine.CatEmail,
			SignalText:     "marie.duval@example.com",
			Documents:      []string{"engagement.md"},
		}},
		Confidence: engine.ConfidenceManualDefault,
	})
	if err != nil {
		t.Fatalf("could not marshal a Value: %v", err)
	}
	for _, field := range retiredValueFields {
		if strings.Contains(string(raw), `"`+field+`"`) {
			t.Errorf("engine.Value serialises %q, which the current model replaced.\n"+
				"See the header of this file for which decision that key undoes.\ngot: %s",
				field, raw)
		}
	}
}

// TestValueWireShapeCarriesEveryCurrentField is the other half: the guard above
// only proves keys are ABSENT, and a Value that had lost mainText or
// discoveryMethods entirely would satisfy it perfectly. Both halves together say
// what the shape IS.
func TestValueWireShapeCarriesEveryCurrentField(t *testing.T) {
	raw, err := json.Marshal(engine.Value{
		Category:         engine.CatPersonNames,
		MainText:         "Marie Duval",
		Spellings:        []string{"M. Duval"},
		SpellingPolicy:   engine.SpellingPolicyCurated,
		DiscoveryMethods: []string{engine.MethodManual},
		Evidence:         []engine.Evidence{{Kind: engine.EvidenceEmailDomain}},
	})
	if err != nil {
		t.Fatalf("could not marshal a Value: %v", err)
	}
	for _, field := range []string{
		"category", "mainText", "spellings", "spellingPolicy", "discoveryMethods", "evidence",
	} {
		if !strings.Contains(string(raw), `"`+field+`"`) {
			t.Errorf("engine.Value does not serialise %q, so the frontend cannot read it.\ngot: %s",
				field, raw)
		}
	}
}

// TestFrontendWritesNoRetiredValueField sweeps frontend/ for the retired keys.
// The frontend is where they were written from, so the Go-side check alone would
// pass while a reducer kept building an old shape and Go silently ignored it: the
// edit would look like it worked and then not stick.
func TestFrontendWritesNoRetiredValueField(t *testing.T) {
	// Two of these are ordinary English words, so for them the sweep looks for the
	// KEY FORMS only (`origin:`, `"origin"`, `.origin`) and not for the bare word.
	// A mapping row's `original` is literally source text and keeps its name, and
	// prose is allowed to use "origin" as a word; a looser sweep flags both, and a
	// guard that cries wolf gets deleted rather than fixed. The other three are
	// invented names that cannot appear innocently, so they are matched anywhere.
	asWord := map[string]bool{"origin": true, "canonical": true}
	patterns := make([]*regexp.Regexp, 0, len(retiredValueFields))
	for _, field := range retiredValueFields {
		if asWord[field] {
			patterns = append(patterns, regexp.MustCompile(
				`(?:\b`+field+`\s*:|"`+field+`"|\.`+field+`\b)`))
			continue
		}
		patterns = append(patterns, regexp.MustCompile(`\b`+field+`\b`))
	}

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
		for i, re := range patterns {
			if re.Match(raw) {
				t.Errorf("%s mentions %q, which the current Value model replaced.\n"+
					"See the header of this file for which decision that key undoes.",
					path, retiredValueFields[i])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not walk frontend/: %v", err)
	}
}
