// engine/conflicts.go — conflict detection for values.
//
// Conflict detection runs before the pipeline touches any text. It checks
// for blocking conflicts that would produce an ambiguous re-identification
// key, and for warnings about overlaps the pipeline will resolve.
//
// Two places emit warnings: ResolveOverlapsWithLosers (which spans are
// discarded because a stronger span covered them) and the pipeline's
// post-detection pass where custom patterns overlap declarative values.
package engine

import (
	"fmt"
	"strings"
)

// ValueRef identifies one occurrence of a value across the system.
//
// The json tags matter: this rides inside Results.Validation on the
// "pipeline:done" event, and the Anonymise screen reads it to explain a refused
// run. Without them the frontend would receive Go's capitalised field names and
// the report would silently disagree with what the engine produced.
type ValueRef struct {
	Kind     string `json:"kind"`             // "regex" | "value" | "custom_pattern"
	Category string `json:"category"`         // category the value belongs to
	MainText string `json:"mainText"`         // lower-cased value
	Detail   string `json:"detail,omitempty"` // extra context (e.g., the regex pattern)
}

// Conflict resolution actions: what an interface can DO, in one gesture, to
// clear a conflict.
//
// They are identifiers rather than prose because Fix below is the sentence and a
// sentence cannot be executed. Mirrored by frontend/state.js
// CONFLICT_RESOLUTIONS and guarded by ../../detection_parity_test.go, for the
// reason every other shared vocabulary is: an action only Go knows is a fix no
// button performs, and one only the frontend knows is a button that does
// something the engine never described.
const (
	// ResolutionDropAllowTerm clears an allowlist collision by taking the term
	// off the never-anonymise list. The term is in ConflictResolution.Term.
	ResolutionDropAllowTerm = "drop_allow_term"
)

// AllConflictResolutions lists the actions this build states.
var AllConflictResolutions = []string{ResolutionDropAllowTerm}

// ConflictResolution is the action that clears a conflict, stated by the ENGINE
// rather than inferred by each screen from the conflict's refs.
//
// It exists because the refusal reaches the user on two different screens (the
// value's own card on Identify, and the refused-run panel on Anonymise) and both
// have to offer the same way out. Each inferring it from a ref kind is two
// places deciding "how is this conflict fixed", which is the shape of duplication
// the pre-run intersection check exists to avoid: two answers can disagree, and
// then a button offers a fix the engine never described.
//
// A conflict with NO resolution is one no single gesture clears, and that is
// honest rather than missing: an ambiguity is cleared by deleting one of two
// Values, and only the user can say which.
type ConflictResolution struct {
	// Action is one of AllConflictResolutions.
	Action string `json:"action"`
	// Term is the argument the action needs: the never-anonymise term to drop.
	// It carries the spelling the ALLOWLIST holds rather than a lower-cased one,
	// because it is what the user will see disappear from their list.
	Term string `json:"term,omitempty"`
}

// Conflict is a value that violates an invariant and must be resolved.
type Conflict struct {
	Kind     string     `json:"kind"`           // "ambiguity" | "overlap" | "collision"
	Severity string     `json:"severity"`       // "block" | "warn"
	Value    string     `json:"value"`          // the problematic value
	Refs     []ValueRef `json:"refs,omitempty"` // all places this value appears
	Message  string     `json:"message"`        // what failed
	Fix      string     `json:"fix"`            // how to fix it
	// Resolution is the action an interface can perform to clear this conflict,
	// or nil when no single gesture clears it.
	Resolution *ConflictResolution `json:"resolution,omitempty"`
}

// ValidationResult is the outcome of ValidateValues.
type ValidationResult struct {
	Blocking []Conflict `json:"blocking,omitempty"` // conflicts that prevent running the pipeline
	Warnings []Conflict `json:"warnings,omitempty"` // conflicts that the pipeline will handle but should notify about
}

// ValidationInput is what ValidateValues needs to check.
type ValidationInput struct {
	Values         []Value           // the accepted Values
	Patterns       []CustomPattern   // user-written regex patterns
	Allowlist      *Allowlist        // terms never to replace
	Categories     CategorySelection // which categories are active
	Registry       *Registry         // may be nil before the first run
	SkipValidation bool              // for tests that deliberately create conflicts
}

// ValidateValues checks the declarations for conflicts and returns them split
// into blocking and warning.
//
// Pure set arithmetic over the declarations, reading no document text, so it is
// cheap enough to run on every run and every fast re-run. It runs inside Run
// BEFORE pass 1: the App has two entry points and the engine has one, and a
// blocking conflict has to abort before the registry is mutated. Half a run
// that assigned placeholders for a configuration the user was just told is
// invalid cannot be undone without starting a new session.
//
// A category that is switched OFF is never a conflict. Its values are not going
// to be replaced, so nothing about them can be ambiguous.
//
// @param in the declarations, the allowlist, the active categories and the
//
//	registry (nil before the first run)
//
// @return blocking conflicts, which refuse the run, and warnings, which do not
func ValidateValues(in ValidationInput) ValidationResult {
	if in.SkipValidation {
		return ValidationResult{}
	}

	active := activeValues(in.Values, in.Categories)

	var result ValidationResult
	result.Blocking = append(result.Blocking, checkDuplicateValues(active)...)
	result.Blocking = append(result.Blocking, checkVariantCollisions(active)...)
	result.Blocking = append(result.Blocking, checkAllowlistCollisions(active, in.Allowlist)...)
	return result
}

// activeValues keeps the values whose category is switched on. A nil
// selection means "no selection was supplied", which every caller inside Run
// resolves before calling, so it is read as "everything is active" rather than
// "nothing is".
func activeValues(values []Value, categories CategorySelection) []Value {
	if categories == nil {
		return values
	}
	out := make([]Value, 0, len(values))
	for _, e := range values {
		if categories[e.Category] {
			out = append(out, e)
		}
	}
	return out
}

// checkDuplicateValues finds the same string declared in two categories.
//
// This is the invariant stated directly: one value maps to exactly one
// replacement string. The registry enforces it at assignment time too, by
// keeping the FIRST category that claimed a string; blocking here is what tells
// the user, because the registry's silent resolution is correct and invisible.
func checkDuplicateValues(values []Value) []Conflict {
	var conflicts []Conflict
	owner := map[string]string{} // lower(mainText) -> category

	for _, e := range values {
		mainText := strings.ToLower(e.MainText)
		previous, seen := owner[mainText]
		if seen && previous != e.Category {
			conflicts = append(conflicts, Conflict{
				Kind:     "ambiguity",
				Severity: "block",
				Value:    e.MainText,
				Refs: []ValueRef{
					{Kind: "value", Category: previous, MainText: mainText},
					{Kind: "value", Category: e.Category, MainText: mainText},
				},
				Message: fmt.Sprintf(
					"the value %q is declared under both %q and %q, and one value can only have one replacement",
					e.MainText, previous, e.Category),
				Fix: "Delete the value from one of the two categories, or switch one category off.",
			})
		}
		owner[mainText] = e.Category
	}
	return conflicts
}

// checkVariantCollisions finds a spelling two different values would both claim.
//
// This is what makes one-value-one-replacement PROVABLE rather than emergent:
// "Marie Duval" and "Marie Dupont" both expand to "Marie", and whichever the
// resolver picks per occurrence, the other value silently loses occurrences it
// was supposed to own. It is also the case the drag-and-drop regrouping exists
// for, caught for the user who never did the drag.
func checkVariantCollisions(values []Value) []Conflict {
	var conflicts []Conflict
	owner := map[string]string{} // lower(variant) -> the mainText that claimed it

	for _, e := range values {
		for _, variant := range ExpandSpellings(e) {
			key := strings.ToLower(variant)
			previous, seen := owner[key]
			if seen && !strings.EqualFold(previous, e.MainText) {
				conflicts = append(conflicts, Conflict{
					Kind:     "collision",
					Severity: "block",
					Value:    variant,
					Refs: []ValueRef{
						{Kind: "value", Category: e.Category, MainText: strings.ToLower(previous), Detail: previous},
						{Kind: "value", Category: e.Category, MainText: strings.ToLower(e.MainText), Detail: e.MainText},
					},
					Message: fmt.Sprintf(
						"%q is a spelling of both %q and %q, so an occurrence of it belongs to neither",
						variant, previous, e.MainText),
					Fix: "Drag the spelling onto the value it belongs to in the variant list, or remove one of the two values.",
				})
			}
			owner[key] = e.MainText
		}
	}
	return conflicts
}

// checkAllowlistCollisions finds a value declared for replacement that is also
// on the never-anonymise list.
//
// The allowlist wins, by every pass, so the value is simply never replaced.
// That is a defensible rule and a terrible silence: the user listed the value
// on purpose and nothing anywhere says why it survived the run.
func checkAllowlistCollisions(values []Value, allowlist *Allowlist) []Conflict {
	if allowlist == nil {
		return nil
	}
	var conflicts []Conflict
	for _, e := range values {
		if !allowlist.Contains(e.MainText) {
			continue
		}
		conflicts = append(conflicts, Conflict{
			Kind:     "collision",
			Severity: "block",
			Value:    e.MainText,
			Refs: []ValueRef{
				{Kind: "value", Category: e.Category, MainText: strings.ToLower(e.MainText)},
				{Kind: "allowlist", MainText: strings.ToLower(e.MainText)},
			},
			Message: fmt.Sprintf(
				"%q is listed both as a value to replace and as a term never to anonymise, and the never-anonymise list always wins",
				e.MainText),
			Fix: "Remove it from one of the two lists, so the run does what you expect.",
			// This conflict has a one-gesture way out, and the engine names it so
			// both screens that show the refusal offer the same one.
			Resolution: &ConflictResolution{
				Action: ResolutionDropAllowTerm,
				Term:   e.MainText,
			},
		})
	}
	return conflicts
}

// maxOverlapWarnings caps how many overlap warnings one run reports, and
// maxOverlapSpansExamined caps how much work finding them may cost.
//
// Both exist because overlaps are per OCCURRENCE while a warning is about a
// CONFIGURATION: a 2 MB document with one recurring collision produces the same
// single warning ten thousand times over. Warnings are deduplicated by
// (category, value), so the first cap bounds what the user reads; the second
// bounds what the pipeline pays to tell them, because collecting the discarded
// spans costs an allocation each on a path that runs over every document.
//
// A run that has examined this many overlapping spans has already seen every
// distinct pairing its documents contain, several thousand times over.
const (
	maxOverlapWarnings      = 50
	maxOverlapSpansExamined = 20000
)

// overlapWarnings turns the spans overlap resolution discarded into warnings,
// deduplicated and capped.
//
// It is a type rather than a function because a run collects across every
// document and every grid cell, and the deduplication has to span all of them.
type overlapWarnings struct {
	seen     map[string]bool
	examined int
	out      []Conflict
}

func newOverlapWarnings() *overlapWarnings {
	return &overlapWarnings{seen: map[string]bool{}}
}

// wants reports whether there is any point collecting more discarded spans.
// The caller asks BEFORE resolving, so a run that has said everything it can
// stops paying to gather losers at all.
func (w *overlapWarnings) wants() bool {
	return len(w.out) < maxOverlapWarnings && w.examined < maxOverlapSpansExamined
}

// add records the spans one anonymiseText call threw away.
//
// A dropped span was covered by a STRONGER one, and "stronger" is the fixed
// order in ResolveOverlaps: higher confidence, then longer, then earlier. Both
// shapes worth reporting come out of that:
//
//	a declared value covered by a regex match. The regex wins because pass 1
//	carries 1.0 against a declaration's 0.95, and that is usually right: an
//	address that is also an email is an email.
//	a custom pattern and a declared value covering each other. Both carry 1.0,
//	so the longer match wins, then the earlier start, then the category name.
//
// Neither is an error, which is why they are warnings: the user is told what
// the pipeline decided, in the words of the rule that decided it.
func (w *overlapWarnings) add(dropped []Span) {
	for _, span := range dropped {
		if !w.wants() {
			return
		}
		w.examined++
		value := span.MainTextOrOriginal()
		key := span.Category + "|" + strings.ToLower(value)
		if w.seen[key] {
			continue
		}
		w.seen[key] = true
		w.out = append(w.out, Conflict{
			Kind:     "overlap",
			Severity: "warn",
			Value:    value,
			Refs:     []ValueRef{{Kind: "detection", Category: span.Category, MainText: strings.ToLower(value)}},
			Message: fmt.Sprintf(
				"%q was detected as %s but a stronger detection covered the same text, so the stronger one was used",
				value, span.Category),
			Fix: "If the wrong one won, switch off the category that covered it, or add the value to the never anonymise list.",
		})
	}
}

// addOwnershipLosses records the claims ownership unification overruled.
//
// These are the same KIND of event as an overlap: two routes wanted the same
// text, and the precedence rule decided. They are reported separately because
// they carry more, and the difference matters to the reader: an overlap warning
// says a stronger detection covered the text, while this one can name BOTH
// routes, so the message explains the decision instead of only announcing it.
//
// The same caps apply. They exist so a pathological document cannot generate
// megabytes of warnings out of one repeated name.
func (w *overlapWarnings) addOwnershipLosses(losses []ownershipLoss) {
	for _, loss := range losses {
		if !w.wants() {
			return
		}
		w.examined++
		value := loss.loser.MainTextOrOriginal()
		key := loss.loser.Category + "|" + strings.ToLower(value)
		if w.seen[key] {
			continue
		}
		w.seen[key] = true
		w.out = append(w.out, Conflict{
			Kind:     "overlap",
			Severity: "warn",
			Value:    value,
			Refs:     []ValueRef{{Kind: "detection", Category: loss.loser.Category, MainText: strings.ToLower(value)}},
			Message: fmt.Sprintf(
				"%q was found by %s as %s and by %s as %s. %s takes priority, so it is replaced as %s everywhere.",
				value,
				matchClassWord(loss.loser.MatchClass), loss.loser.Category,
				matchClassWord(loss.winner.MatchClass), loss.winner.Category,
				matchClassWord(loss.winner.MatchClass), loss.winner.Category),
			Fix: "If the other one should win, switch off the type that covered it, narrow the pattern, or add the covering term to the never anonymise list.",
		})
	}
}

// matchClassWord is the route named in a warning sentence. The engine's identifiers
// are contracts, not prose, so a message that printed a class name would read as
// jargon; these are the same words the value card's matchClass chip uses.
func matchClassWord(matchClass string) string {
	switch matchClass {
	case MatchClassBuiltInPattern:
		return "native detection"
	case MatchClassSmartDiscovered:
		return "heuristic discovery"
	case MatchClassLocalAIDiscovered:
		return "local LLM discovery"
	default:
		return "your own values and patterns"
	}
}

// conflicts returns the collected warnings.
func (w *overlapWarnings) conflicts() []Conflict {
	return w.out
}
