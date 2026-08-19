// declaration_rule_test.go — the Anonymise step's rule, asserted where prose cannot be
//
// The charter used to compress two different claims into one sentence,
// "Anonymise creates no Value", and the half it dropped is the half that permits
// a real feature: the user may DECLARE a Value while reviewing the anonymised
// result, from the Compare pane selection or the "Add missed Value" card. Read
// literally, the old sentence forbade the surface the application ships, and an
// agent following the charter deleted it once.
//
// The half that was always true is asserted by
// backend.TestAnonymiseNeverCallsOllama: a run reaches no model. This guard holds
// the OTHER half, the one a sentence cannot hold, by failing the build if the
// misleading claim comes back anywhere in the sources or the charters.
//
// It also pins the single-source rule for the conflict sentences. Two screens now
// describe a blocking conflict, the Identify workspace on the card that causes it
// and Anonymise at the declaration that would cause it, and both read ONE
// assembler. A second copy of those sentences is the drift the copy rules exist
// to prevent.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ruleRoots are the trees whose prose and code must not carry the retired claim.
// Walking the two source trees covers their charters, the bridge contract and the
// bundled docs, so nothing is listed twice and one report line means one hit.
var ruleRoots = []string{"CLAUDE.md", "frontend", "backend"}

// retiredClaims are the spellings of the sentence that reads as a prohibition on
// the declaration surfaces. docs/ is deliberately out of scope: a change order
// quotes the wording it replaces, and rewriting history there would be a lie.
var retiredClaims = []string{
	"creates no Value",
	"never invents a Value",
	"can mint a Value",
}

func TestNoFileClaimsAnonymiseCreatesNoValue(t *testing.T) {
	var hits []string
	for _, root := range ruleRoots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if info.Name() == "assets" || info.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".go", ".js", ".md", ".html":
			default:
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for i, line := range strings.Split(string(body), "\n") {
				for _, claim := range retiredClaims {
					if strings.Contains(line, claim) {
						hits = append(hits, fmt.Sprintf("%s:%d: %q", path, i+1, claim))
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
	if len(hits) > 0 {
		t.Errorf("the Anonymise step applies Values the user DECLARED there as well as "+
			"ones accepted on Identify; what it never does is run a discovery method or "+
			"reach a model. Reword these:\n%s", strings.Join(hits, "\n"))
	}
}

// TestConflictSentencesLiveInOnePlace: the wording for a blocking conflict has
// exactly one assembler, and every screen that shows a conflict imports it.
//
// The guard is on the SHAPE, not on the file: whichever module owns
// conflictMessage, no other view may switch on a conflict kind or reach past it
// into the WORKSPACE.conflict* strings, because that is a second set of sentences
// for one conflict.
func TestConflictSentencesLiveInOnePlace(t *testing.T) {
	views, err := filepath.Glob(filepath.Join("frontend", "views", "*.js"))
	if err != nil {
		t.Fatalf("globbing views: %v", err)
	}
	sources := map[string]string{}
	for _, path := range append(views, filepath.Join("frontend", "copy.js")) {
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("reading %s: %v", path, readErr)
		}
		sources[path] = string(body)
	}

	// Exactly one definition, wherever it lives.
	var owners []string
	for path, body := range sources {
		if strings.Contains(body, "function conflictMessage(") {
			owners = append(owners, path)
		}
	}
	if len(owners) != 1 {
		t.Fatalf("conflictMessage must be defined exactly once and shared; found %d definition(s): %v",
			len(owners), owners)
	}
	owner := owners[0]
	if !strings.Contains(sources[owner], "export function conflictMessage(") {
		t.Errorf("%s defines conflictMessage without exporting it, so the second screen "+
			"cannot reach it and will grow its own copy of the sentences", owner)
	}

	// And nobody else assembles them. copy.js is where the strings live, so it is
	// allowed to name them; every other module must go through conflictMessage.
	for path, body := range sources {
		if path == owner || path == filepath.Join("frontend", "copy.js") {
			continue
		}
		for _, sentence := range []string{"conflictAmbiguity(", "conflictCollision(", "conflictAllowlist("} {
			if strings.Contains(body, sentence) {
				t.Errorf("%s builds the conflict sentence %s itself; import conflictMessage "+
					"from %s instead, so two screens cannot describe one conflict two ways",
					path, sentence, owner)
			}
		}
	}
}
