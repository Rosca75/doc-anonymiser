// vocabulary_guard_test.go — the retired-identifier guard.
//
// The detection vocabulary is the contract (CLAUDE.md §5), and it is a contract
// on BOTH sides of the bridge and in the session file. When an identifier is
// renamed, the old spelling has to leave the tree completely: a single survivor
// is not a compile error anywhere, because every one of these is a STRING at the
// boundary that matters. A leftover `"local_ai"` in a comparison silently stops
// matching, a leftover `useLocalAI` JSON tag silently reads as false, and both
// change what the next run replaces while every other test stays green.
//
// So the check is a source scan rather than a type check, and it is the permanent
// form of the grep the change order asks for. Identifier names only: it does not
// forbid the retired LABELS, which copy_guard_test.go owns, and it does not read
// docs/, where the change orders quote the old spellings as the record of what
// was renamed.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"doc-anonymiser/backend"
	"doc-anonymiser/backend/engine"
)

// retiredIdentifiers are spellings no source file may carry any more, each with
// the spelling that replaced it, so a failure names the fix rather than only the
// problem.
var retiredIdentifiers = map[string]string{
	// Discovery method and match classes: persisted in every session file, and
	// mirrored in frontend/state.js.
	"local_ai":                    "local_llm",
	"local_ai_discovered":         "local_llm_discovered",
	"smart_discovered":            "rules_discovered",
	"MethodLocalAI":               "MethodLocalLLM",
	"MatchClassLocalAIDiscovered": "MatchClassLocalLLMDiscovered",
	"MatchClassSmartDiscovered":   "MatchClassRulesDiscovered",
	// Detection phases: the progress event's payload.
	"PhaseSmart":   "PhaseRules",
	"PhaseLocalAI": "PhaseLocalLLM",
	// Settings fields and their JSON keys: session files and profile files.
	"UseLocalAI":     "UseLocalLLM",
	"useLocalAI":     "useLocalLLM",
	"AIStrictFormat": "LLMStrictFormat",
	"aiStrictFormat": "llmStrictFormat",
	"AIDetailLevel":  "LLMDetailLevel",
	"aiDetailLevel":  "llmDetailLevel",
	// The heuristic options type, renamed before this vocabulary was settled.
	"SmartDetectOptions": "HeuristicDiscoveryOptions",
}

// scannedExts are the file kinds that can carry an identifier: source on both
// sides, the harness scripts, and the fixture JSON a test reads back.
var scannedExts = map[string]bool{
	".go": true, ".js": true, ".ps1": true, ".json": true, ".html": true, ".css": true,
}

// skippedDirs are trees with nothing to hold to the contract. docs/ is skipped on
// purpose: a change order quotes the OLD spellings, which is what makes it the
// record of the rename.
var skippedDirs = map[string]bool{
	".git": true, "docs": true, "node_modules": true, "artifacts": true, "assets": true,
}

// exemptFiles carry a retired spelling because their SUBJECT is the old format.
// The list is deliberately tiny and named file by file: an exemption is a hole in
// the guard, so it has to be visible and it has to be argued.
var exemptFiles = map[string]string{
	// This file names every retired spelling on purpose.
	"vocabulary_guard_test.go": "the guard itself",
	// It builds a v10 session file to prove the loader refuses it, and a v10 file
	// carrying the CURRENT spellings would not be the file the test is about.
	"session_test.go": "the v10-refusal fixture",
}

func TestNoRetiredIdentifierSurvives(t *testing.T) {

	var hits []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skippedDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if _, exempt := exemptFiles[info.Name()]; exempt {
			return nil
		}
		if !scannedExts[filepath.Ext(info.Name())] {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for lineNo, line := range strings.Split(string(raw), "\n") {
			for retired, replacement := range retiredIdentifiers {
				// Whole-token only. "local_ai" is a substring of
				// "local_ai_discovered", and reporting the same line twice under
				// two names would send the reader looking for a second defect.
				if !containsToken(line, retired) {
					continue
				}
				hits = append(hits, fmt.Sprintf("%s:%d: %s (use %s): %s",
					path, lineNo+1, retired, replacement, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository: %v", err)
	}

	sort.Strings(hits)
	if len(hits) > 0 {
		t.Errorf("retired identifiers found. Every one of these is a STRING at a boundary,\n"+
			"so a survivor is not a compile error: a stale comparison stops matching and a\n"+
			"stale JSON tag reads as its zero value, and both change what the next run\n"+
			"replaces with every other test still green.\n%s", strings.Join(hits, "\n"))
	}
}

// containsToken reports whether line carries name as a whole identifier rather
// than as part of a longer one, so local_ai is not reported inside
// local_ai_discovered and UseLocalAI is not reported inside setUseLocalAI.
func containsToken(line, name string) bool {
	for from := 0; ; {
		at := strings.Index(line[from:], name)
		if at < 0 {
			return false
		}
		at += from
		before := byte(' ')
		if at > 0 {
			before = line[at-1]
		}
		after := byte(' ')
		if end := at + len(name); end < len(line) {
			after = line[end]
		}
		if !isIdentByte(before) && !isIdentByte(after) {
			return true
		}
		from = at + 1
	}
}

// isIdentByte reports whether b can appear inside an identifier in either
// language, which is what makes the match a whole token rather than a fragment.
func isIdentByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '_':
		return true
	}
	return false
}

// TestTheCurrentVocabularyIsWhatTheCodeUses is the other half: the guard above
// says the old spellings are gone, and this says the new ones are actually the
// ones in force. Without it, deleting a constant altogether would satisfy the
// first check and leave the vocabulary with a hole.
func TestTheCurrentVocabularyIsWhatTheCodeUses(t *testing.T) {
	if engine.MethodLocalLLM != "local_llm" {
		t.Errorf("MethodLocalLLM is %q, want local_llm", engine.MethodLocalLLM)
	}
	if engine.MatchClassRulesDiscovered != "rules_discovered" {
		t.Errorf("MatchClassRulesDiscovered is %q, want rules_discovered",
			engine.MatchClassRulesDiscovered)
	}
	if engine.MatchClassLocalLLMDiscovered != "local_llm_discovered" {
		t.Errorf("MatchClassLocalLLMDiscovered is %q, want local_llm_discovered",
			engine.MatchClassLocalLLMDiscovered)
	}
	if backend.PhaseRules != "rules" {
		t.Errorf("PhaseRules is %q, want rules", backend.PhaseRules)
	}
	if backend.PhaseLocalLLM != "local_llm" {
		t.Errorf("PhaseLocalLLM is %q, want local_llm", backend.PhaseLocalLLM)
	}
	// The settings keys are what a session file carries, so they are checked
	// through the JSON a save actually writes rather than by naming the fields.
	raw, err := engine.SaveSession(engine.Session{
		Settings: engine.SessionSettings{Level: "medium", UseLocalLLM: true},
	})
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if !strings.Contains(string(raw), `"useLocalLLM"`) {
		t.Errorf("a saved session does not carry useLocalLLM:\n%s", raw)
	}
	for _, retired := range []string{`"useLocalAI"`, `"aiStrictFormat"`, `"aiDetailLevel"`} {
		if strings.Contains(string(raw), retired) {
			t.Errorf("a saved session still writes %s:\n%s", retired, raw)
		}
	}
}
