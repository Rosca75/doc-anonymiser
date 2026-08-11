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
	"regexp"
	"strings"
)

// ValueRef identifies one occurrence of a value across the system.
type ValueRef struct {
	Kind      string // "regex" | "entity" | "custom_pattern" | "simple_rule"
	Category  string // category the value belongs to
	Canonical string // lower-cased value
	Detail    string // extra context (e.g., the regex pattern)
}

// Conflict is a value that violates an invariant and must be resolved.
type Conflict struct {
	Kind     string     // "ambiguity" | "overlap" | "collision" | "reserved"
	Severity string     // "block" | "warn"
	Value    string     // the problematic value
	Refs     []ValueRef // all places this value appears
	Message  string     // what failed
	Fix      string     // how to fix it
}

// ValidationResult is the outcome of ValidateValues.
type ValidationResult struct {
	Blocking []Conflict // conflicts that prevent running the pipeline
	Warnings []Conflict // conflicts that the pipeline will handle but should notify about
}

// ValidationInput is what ValidateValues needs to check.
type ValidationInput struct {
	Entities       []Entity          // declared entities
	Patterns       []CustomPattern   // user-written regex patterns
	SimpleRules    []SimpleRule      // find-and-replace rules
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

	active := activeEntities(in.Entities, in.Categories)

	var result ValidationResult
	result.Blocking = append(result.Blocking, checkDuplicateValues(active)...)
	result.Blocking = append(result.Blocking, checkVariantCollisions(active)...)
	result.Blocking = append(result.Blocking, checkAllowlistCollisions(active, in.Allowlist)...)
	result.Blocking = append(result.Blocking, checkSimpleRuleConflicts(in.SimpleRules, in.Registry)...)
	return result
}

// activeEntities keeps the entities whose category is switched on. A nil
// selection means "no selection was supplied", which every caller inside Run
// resolves before calling, so it is read as "everything is active" rather than
// "nothing is".
func activeEntities(entities []Entity, categories CategorySelection) []Entity {
	if categories == nil {
		return entities
	}
	out := make([]Entity, 0, len(entities))
	for _, e := range entities {
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
func checkDuplicateValues(entities []Entity) []Conflict {
	var conflicts []Conflict
	owner := map[string]string{} // lower(canonical) -> category

	for _, e := range entities {
		canonical := strings.ToLower(e.Canonical)
		previous, seen := owner[canonical]
		if seen && previous != e.Category {
			conflicts = append(conflicts, Conflict{
				Kind:     "ambiguity",
				Severity: "block",
				Value:    e.Canonical,
				Refs: []ValueRef{
					{Kind: "entity", Category: previous, Canonical: canonical},
					{Kind: "entity", Category: e.Category, Canonical: canonical},
				},
				Message: fmt.Sprintf(
					"the value %q is declared under both %q and %q, and one value can only have one replacement",
					e.Canonical, previous, e.Category),
				Fix: "Delete the value from one of the two categories, or switch one category off.",
			})
		}
		owner[canonical] = e.Category
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
func checkVariantCollisions(entities []Entity) []Conflict {
	var conflicts []Conflict
	owner := map[string]string{} // lower(variant) -> the canonical that claimed it

	for _, e := range entities {
		for _, variant := range ExpandVariants(e) {
			key := strings.ToLower(variant)
			previous, seen := owner[key]
			if seen && !strings.EqualFold(previous, e.Canonical) {
				conflicts = append(conflicts, Conflict{
					Kind:     "collision",
					Severity: "block",
					Value:    variant,
					Refs: []ValueRef{
						{Kind: "entity", Category: e.Category, Canonical: strings.ToLower(previous), Detail: previous},
						{Kind: "entity", Category: e.Category, Canonical: strings.ToLower(e.Canonical), Detail: e.Canonical},
					},
					Message: fmt.Sprintf(
						"%q is a spelling of both %q and %q, so an occurrence of it belongs to neither",
						variant, previous, e.Canonical),
					Fix: "Drag the spelling onto the value it belongs to in the variant list, or remove one of the two values.",
				})
			}
			owner[key] = e.Canonical
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
func checkAllowlistCollisions(entities []Entity, allowlist *Allowlist) []Conflict {
	if allowlist == nil {
		return nil
	}
	var conflicts []Conflict
	for _, e := range entities {
		if !allowlist.Contains(e.Canonical) {
			continue
		}
		conflicts = append(conflicts, Conflict{
			Kind:     "collision",
			Severity: "block",
			Value:    e.Canonical,
			Refs: []ValueRef{
				{Kind: "entity", Category: e.Category, Canonical: strings.ToLower(e.Canonical)},
				{Kind: "allowlist", Canonical: strings.ToLower(e.Canonical)},
			},
			Message: fmt.Sprintf(
				"%q is listed both as a value to replace and as a term never to anonymise, and the never-anonymise list always wins",
				e.Canonical),
			Fix: "Remove it from one of the two lists, so the run does what you expect.",
		})
	}
	return conflicts
}

// checkSimpleRuleConflicts finds find-and-replace rules that would corrupt the
// re-identification key.
//
// Two shapes, and only two:
//
//	a FIND containing a placeholder. The rules run last, after the passes, so
//	such a rule rewrites anonymised output and the key stops describing the
//	text it claims to describe.
//	a REPLACE naming a placeholder the registry has already given to a value.
//	The exported key would then have two originals behind one placeholder.
//
// An UNASSIGNED placeholder-shaped replace is allowed on purpose: that is the
// shipped select-and-replace flow, which mints a [CUSTOM_N] the App reserves.
func checkSimpleRuleConflicts(rules []SimpleRule, registry *Registry) []Conflict {
	var conflicts []Conflict
	for _, rule := range rules {
		if placeholder := placeholderInsideRe.FindString(rule.Find); placeholder != "" {
			conflicts = append(conflicts, Conflict{
				Kind:     "reserved",
				Severity: "block",
				Value:    rule.Find,
				Refs:     []ValueRef{{Kind: "simple_rule", Detail: rule.Find}},
				Message: fmt.Sprintf(
					"the find-and-replace rule looks for %q, which is a placeholder this run produces, so the rule would rewrite anonymised text",
					placeholder),
				Fix: "Change what the rule looks for to the original text, not the placeholder.",
			})
			continue
		}
		if registry == nil || !placeholderShapeRe.MatchString(rule.Replace) {
			continue
		}
		owner, assigned := registry.PlaceholderOwner(rule.Replace)
		if !assigned {
			continue // free, and the App reserves it when it hands it out
		}
		conflicts = append(conflicts, Conflict{
			Kind:     "reserved",
			Severity: "block",
			Value:    rule.Replace,
			Refs: []ValueRef{
				{Kind: "simple_rule", Detail: rule.Find},
				{Kind: "entity", Category: owner.Category, Canonical: strings.ToLower(owner.Original)},
			},
			Message: fmt.Sprintf(
				"the rule replaces text with %s, which this session already assigned to %q, so the exported key would have two values behind one placeholder",
				rule.Replace, owner.Original),
			Fix: "Pick a different replacement. The select-and-replace action on step 3 mints a free one for you.",
		})
	}
	return conflicts
}

// placeholderInsideRe matches a placeholder ANYWHERE in a string, unlike
// placeholderShapeRe, which anchors. A rule looking for "call [PERSON_1] back"
// is the same mistake as one looking for "[PERSON_1]".
var placeholderInsideRe = regexp.MustCompile(`\[[A-Z][A-Z0-9_]*_[0-9]+\]`)

// maxOverlapWarnings caps how many overlap warnings one run reports.
//
// Overlaps are per OCCURRENCE, so a 2 MB document with one recurring collision
// produces ten thousand identical rows. They are deduplicated by (kind, value)
// first, and the cap is the backstop for a document that genuinely collides in
// many different ways: a list nobody can read is not a warning.
const maxOverlapWarnings = 50

// overlapWarnings turns the spans overlap resolution discarded into warnings,
// deduplicated and capped.
//
// It is a type rather than a function because a run collects across every
// document and every grid cell, and the deduplication has to span all of them.
type overlapWarnings struct {
	seen map[string]bool
	out  []Conflict
}

func newOverlapWarnings() *overlapWarnings {
	return &overlapWarnings{seen: map[string]bool{}}
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
		if len(w.out) >= maxOverlapWarnings {
			return
		}
		value := span.CanonicalOrOriginal()
		key := span.Category + "|" + strings.ToLower(value)
		if w.seen[key] {
			continue
		}
		w.seen[key] = true
		w.out = append(w.out, Conflict{
			Kind:     "overlap",
			Severity: "warn",
			Value:    value,
			Refs:     []ValueRef{{Kind: "detection", Category: span.Category, Canonical: strings.ToLower(value)}},
			Message: fmt.Sprintf(
				"%q was detected as %s but a stronger detection covered the same text, so the stronger one was used",
				value, span.Category),
			Fix: "If the wrong one won, switch off the category that covered it, or add the value to the never anonymise list.",
		})
	}
}

// conflicts returns the collected warnings.
func (w *overlapWarnings) conflicts() []Conflict {
	return w.out
}
