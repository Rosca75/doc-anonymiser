// app_entities.go — bound methods for the Entities screen (Phase 7):
// LLM discovery over selected files, variant expansion for the review
// table, and custom-pattern validation/testing. Thin adapters only
// (CLAUDE.md §3): all logic lives in engine/* and ollama/*.
package backend

import (
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"doc-anonymiser/backend/engine"
)

// runtimeEventsEmit is an indirection over the Wails event runtime so unit
// tests (which have no Wails context) never touch it — emit() already
// guards on a nil ctx, and this var could be swapped in tests if needed.
var runtimeEventsEmit = func(a *App, name string, payload interface{}) {
	runtime.EventsEmit(a.ctx, name, payload)
}

// ExpandEntityVariants returns the automatic + manual variants of one
// entity for the expandable variant list in the review table.
func (a *App) ExpandEntityVariants(e engine.Entity) []string {
	return engine.ExpandVariants(e)
}

// --- The step 3 value surface -----------------------------------------------
//
// Every method here is addressed BY PLACEHOLDER, because on step 3 the user is
// looking at report rows and at marks in the Compare pane and both carry the
// placeholder. Renaming a value and removing it are the two rules a Value obeys
// once a run has produced it, and this is the only surface for either.

// SetValuePlaceholder renames what one value is replaced with.
//
// The rename takes effect on the NEXT run, not retroactively: the text on
// screen was produced with the old placeholder, and rewriting it here would
// leave the report and the mapping describing text that no longer exists.
//
// @param placeholder the placeholder as it stands, in [NAME_N] form
// @param newPlaceholder what the user wants instead
// @return an actionable error the UI shows verbatim, or nil
func (a *App) SetValuePlaceholder(placeholder, newPlaceholder string) error {
	a.mu.Lock()
	reg := a.registry
	a.mu.Unlock()

	if reg == nil {
		return fmt.Errorf(
			"there are no placeholders to edit yet: run the anonymisation once, " +
				"then edit any placeholder in the replaced-values list")
	}
	return reg.Rename(placeholder, newPlaceholder)
}

// ValuePlaceholders returns all current placeholder assignments:
// the map of placeholder → {original value, category} for display in step 3.
//
// ValuePlaceholders returns one row per value the session has replaced: the
// source for the step 3 Replaced values table (BUILD-06 Phase 5).
//
// It reads the REGISTRY rather than deriving rows from report text, because the
// registry is what the placeholder editing and the removals both act on: a table
// built from the report could offer a row the registry has no entry for, and the
// edit behind it would fail with nothing to point at.
//
// @return the mapping rows, sorted by category then placeholder number; empty
//
//	before the first run, which is not an error, only an empty table
func (a *App) ValuePlaceholders() []engine.MappingEntry {
	a.mu.Lock()
	reg := a.registry
	a.mu.Unlock()

	if reg == nil {
		return []engine.MappingEntry{}
	}
	return reg.Export()
}

// RemovedValueInfo is the frontend-facing summary of a removed value: what the
// collapsed "removed" list shows, and what RestoreValue is addressed by.
type RemovedValueInfo struct {
	Original string `json:"original"`
	Category string `json:"category"`
	// Placeholder is what the value USED to become. It is the address for
	// RestoreValue, and the reason the removed list is readable at all: the user
	// removed a row from a table of placeholders, so that is what they recognise.
	Placeholder string `json:"placeholder"`
	// Variants are the spellings the exclusion also covers, so the UI can say
	// that removing "Marie Duval" also stopped "M. Duval" being replaced.
	Variants []string `json:"variants,omitempty"`
}

// ValidationError is a single validation issue (BUILD-06 Phase 3/4).
type ValidationError struct {
	Kind     string `json:"kind"`     // "duplicate", "collision", "conflict"
	Severity string `json:"severity"` // "block", "warn"
	Message  string `json:"message"`
}

// RemoveValue deletes one value from the session (BUILD-06 Phases 4 and 5).
//
// Removal is ONE action with three effects, and they cannot be allowed to
// happen separately:
//
//  1. the registry entry is forgotten, so the re-identification key stops
//     describing a replacement that no longer happens;
//  2. the value and its variants are recorded as a session exclusion, which is
//     what makes the removal stick. A regex-detected value has no Entity to
//     drop, so the exclusion is the whole mechanism for that trigger kind and
//     can never be skipped;
//  3. every later run and every export reads that exclusion through
//     allowlistFor, so the pipeline's registry post-pass stops re-applying the
//     value to every document forever (the pipeline.go hole this closes).
//
// The NUMBER is not freed (Registry.Forget). The user may already hold an
// exported document, a mapping CSV or a session file in which [PERSON_4] means
// one person, and handing 4 to somebody else would make two artefacts of one
// session disagree with nothing able to detect it.
//
// This does NOT re-run the pipeline. RunPipeline holds an in-progress guard and
// FastRerun is synchronous, so re-running from in here while holding a.mu is a
// deadlock shape; the caller re-runs, exactly as the reassign flow already does.
//
// @param placeholder the placeholder of the value to remove, from the table
// @return what was removed (so the UI can say so), or an actionable error
func (a *App) RemoveValue(placeholder string) (*RemovedValueInfo, error) {
	a.mu.Lock()
	reg := a.registry
	a.mu.Unlock()

	if reg == nil {
		return nil, fmt.Errorf(
			"there are no replaced values to remove yet: run the anonymisation once, " +
				"then remove any value it replaced from the list on step 3")
	}

	entry, ok := reg.PlaceholderOwner(placeholder)
	if !ok {
		return nil, fmt.Errorf(
			"the placeholder %q is not one this session assigned, so there is nothing to remove. "+
				"Pick a row from the replaced-values list", placeholder)
	}

	// The exclusion covers the variants too, or removing "Marie Duval" would
	// leave "M. Duval" being replaced under a placeholder whose entry is gone.
	variants := engine.ExpandVariants(engine.Entity{
		Category:  entry.Category,
		Canonical: entry.Original,
	})

	removed := engine.RemovedValue{
		Category:    entry.Category,
		Canonical:   strings.ToLower(entry.Original),
		Variants:    variants,
		Placeholder: entry.Placeholder,
	}

	reg.Forget(entry.Category, entry.Original)

	a.mu.Lock()
	a.removed = append(a.removed, removed)
	a.mu.Unlock()

	return &RemovedValueInfo{
		Original:    entry.Original,
		Category:    entry.Category,
		Placeholder: entry.Placeholder,
		Variants:    variants,
	}, nil
}

// RestoreValue undoes a removal (BUILD-06 Phases 4 and 5).
//
// It drops the exclusion and nothing else, deliberately. The value comes back
// with a NEW number on the next run, because its old one was retired and stays
// retired: the whole reason RemoveValue did not free the number is that an
// artefact carrying it may already have left the machine, and a restore is not
// evidence that it did not.
//
// Like RemoveValue, this does not re-run: the caller does.
//
// @param placeholder the placeholder the value USED to have, from the removed list
// @return an actionable error, or nil
func (a *App) RestoreValue(placeholder string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	for i, r := range a.removed {
		if r.Placeholder != placeholder {
			continue
		}
		a.removed = append(a.removed[:i], a.removed[i+1:]...)
		return nil
	}
	return fmt.Errorf(
		"%q is not in the removed list, so there is nothing to restore. "+
			"Only a value removed in this session can be restored", placeholder)
}

// ListRemovedValues returns the values removed in this session, for the
// collapsed "removed" list on step 3 (BUILD-06 Phases 4 and 5).
//
// The list is the App's own exclusion record, NOT the registry's retired
// placeholders. The two are not the same set and reading the wrong one is
// silently wrong in both directions: a placeholder retired by a rename is not a
// removed value, and a removed value survives a session reload while the
// registry entry behind it does not exist any more at all.
//
// @return one row per removed value, never nil
func (a *App) ListRemovedValues() []RemovedValueInfo {
	a.mu.Lock()
	defer a.mu.Unlock()

	out := make([]RemovedValueInfo, 0, len(a.removed))
	for _, r := range a.removed {
		out = append(out, RemovedValueInfo{
			Original:    r.Canonical,
			Category:    r.Category,
			Placeholder: r.Placeholder,
			Variants:    r.Variants,
		})
	}
	return out
}

// NextRulePlaceholder mints and RESERVES the next free [CUSTOM_N] for a
// simple-replace rule (BUILD-06 Phase 5).
//
// It replaces the frontend's nextCustomNumber, which counted only the existing
// rules. CUSTOM is also the automatic label for the custom_patterns category, so
// a rule and an automatic assignment could already collide on the same number,
// and the exported key would then have two different values behind one
// placeholder. Asking the registry is the fix: it is the only thing that knows
// every number already spent, whether by an entry, an override, a reservation or
// a retirement.
//
// The number is reserved as it is handed out, not when the rule is saved. A
// number handed to the user and not held is a number the next automatic
// assignment can take while they are still typing.
//
// @return the placeholder to put in the rule, or an actionable error
func (a *App) NextRulePlaceholder() (string, error) {
	a.mu.Lock()
	// The session registry, created here if the user reaches the
	// select-and-replace flow before the first run: it is the same lazily
	// created instance the pipeline uses, so a number reserved now is still
	// reserved when the run starts.
	if a.registry == nil {
		a.registry = engine.NewRegistry()
	}
	reg := a.registry
	a.mu.Unlock()

	// Reserve() refuses a placeholder that is taken, so walking upwards until it
	// accepts one is both the search and the claim, with no second definition of
	// "free" that could drift from the registry's.
	label := engine.PlaceholderLabel(engine.CatCustomPatterns)
	for n := 1; n <= maxRulePlaceholder; n++ {
		candidate := fmt.Sprintf("[%s_%d]", label, n)
		if err := reg.Reserve(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf(
		"every %s placeholder up to %d is already in use, which is far past what a "+
			"session is expected to need. Remove some find-and-replace rules, or start a new session",
		label, maxRulePlaceholder)
}

// maxRulePlaceholder bounds the search above. It is a runaway guard, not a
// product limit: a session with ten thousand custom rules is a bug somewhere
// else, and an unbounded loop would hang the UI thread rather than say so.
const maxRulePlaceholder = 10000

// ValidateValuesRequest is the input for ValidateValues (BUILD-06 Phase 5).
type ValidateValuesRequest struct {
	Entities   []engine.Entity        `json:"entities"`
	Patterns   []engine.CustomPattern `json:"patterns"`
	Rules      []engine.SimpleRule    `json:"rules"`
	AllowTerms []string               `json:"allowTerms"`
}

// ValidateValuesResult is the output from ValidateValues.
type ValidateValuesResult struct {
	Blocking []ValidationError `json:"blocking"`
	Warnings []ValidationError `json:"warnings"`
}

// ValidateValues checks the current entities, patterns and rules for conflicts
// before running the pipeline (BUILD-06 Phase 3/5). Returns blocking errors
// (which prevent the run) and warnings (informational only).
//
// @param req the validation request with entities, patterns, rules and allowlist
// @return blocking errors (must be resolved before running) and warnings
func (a *App) ValidateValues(req ValidateValuesRequest) (*ValidateValuesResult, error) {
	a.mu.Lock()
	reg := a.registry
	a.mu.Unlock()

	// The same allowlist the run will use, removals included, so validation
	// cannot report a conflict the pipeline would not see (or miss one it would).
	allowlist := a.allowlistFor(req.AllowTerms)

	// Run the engine's validation
	result := engine.ValidateValues(engine.ValidationInput{
		Entities:       req.Entities,
		Patterns:       req.Patterns,
		SimpleRules:    req.Rules,
		Allowlist:      allowlist,
		Categories:     nil,
		Registry:       reg,
		SkipValidation: false,
	})

	// Convert to frontend-friendly format
	blocking := make([]ValidationError, len(result.Blocking))
	for i, c := range result.Blocking {
		blocking[i] = ValidationError{
			Kind:     c.Kind,
			Severity: c.Severity,
			Message:  c.Message,
		}
	}

	warnings := make([]ValidationError, len(result.Warnings))
	for i, c := range result.Warnings {
		warnings[i] = ValidationError{
			Kind:     c.Kind,
			Severity: c.Severity,
			Message:  c.Message,
		}
	}

	return &ValidateValuesResult{
		Blocking: blocking,
		Warnings: warnings,
	}, nil
}

// TermMatchInfo is the live manual-entry preview payload (BUILD-02
// Phase 9c): how often a term occurs, and in how many documents.
type TermMatchInfo struct {
	Count     int `json:"count"`
	Documents int `json:"documents"`
}

// CountTermMatches counts case-insensitive word-boundary occurrences of
// term across every loaded document, for the "Found N times in M
// documents" preview under the manual add-entity inputs.
func (a *App) CountTermMatches(term string) TermMatchInfo {
	a.mu.Lock()
	docs := a.docs
	a.mu.Unlock()

	info := TermMatchInfo{}
	for _, doc := range docs {
		if n := engine.CountTermMatches(doc.Markdown, term); n > 0 {
			info.Count += n
			info.Documents++
		}
	}
	return info
}

// ValidatePattern compile-checks a user regex. It returns the error as a
// STRING ("" = valid) instead of a Go error, because a validation failure
// is expected feedback for the live checker, not a rejected promise.
func (a *App) ValidatePattern(expr string) string {
	if err := engine.ValidateCustomPattern(expr); err != nil {
		return err.Error()
	}
	return ""
}

// PatternMatches runs a (valid) custom pattern over every loaded document
// and returns up to 20 sample matches for the tester UI.
func (a *App) PatternMatches(expr string) ([]string, error) {
	if err := engine.ValidateCustomPattern(expr); err != nil {
		return nil, err
	}
	a.mu.Lock()
	docs := a.docs
	a.mu.Unlock()

	const maxSamples = 20
	var samples []string
	seen := map[string]bool{}
	for _, doc := range docs {
		spans := engine.DetectCustomPatterns(doc.Markdown, []engine.CustomPattern{{Expr: expr}}, nil)
		for _, s := range spans {
			if seen[s.Original] {
				continue // show each distinct match once
			}
			seen[s.Original] = true
			samples = append(samples, s.Original)
			if len(samples) >= maxSamples {
				return samples, nil
			}
		}
	}
	return samples, nil
}

// docsByName returns the loaded documents matching the given names, in
// request order. Unknown names are silently skipped (the UI list and the
// Go list can only diverge for a moment during removal).
func (a *App) docsByName(names []string) []engine.Document {
	a.mu.Lock()
	defer a.mu.Unlock()
	byName := map[string]engine.Document{}
	for _, d := range a.docs {
		byName[d.Name] = d
	}
	var out []engine.Document
	for _, n := range names {
		if d, ok := byName[n]; ok {
			out = append(out, d)
		}
	}
	return out
}

// emit fires a frontend event, tolerating the headless case (a.ctx is nil
// in unit tests — events are UI sugar, never load-bearing).
func (a *App) emit(name string, payload interface{}) {
	if a.ctx == nil {
		return
	}
	runtimeEventsEmit(a, name, payload)
}
