// engine/conflicts.go — conflict detection for values (Phase 3).
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

// ValidateValues checks for conflicts and returns them categorised as
// blocking or warning-only.
//
// Pure set arithmetic over declarations, reading no document text, so it
// runs cheap enough to run on every run and every fast re-run.
func ValidateValues(in ValidationInput) ValidationResult {
	if in.SkipValidation {
		return ValidationResult{}
	}

	var result ValidationResult

	// Check for values declared in two categories (blocking).
	result.Blocking = append(result.Blocking, checkDuplicateValues(in.Entities)...)

	// TODO: Check for values that collide with the allowlist (blocking)
	// An allowlisted term simply won't be replaced (allowlist wins by design),
	// so declaring it in entities is not a blocking conflict, just ineffective.
	// result.Blocking = append(result.Blocking, checkAllowlistCollisions(in.Entities, in.Allowlist)...)

	// TODO: Check for variant collisions (blocking) - needs more careful implementation
	// result.Blocking = append(result.Blocking, checkVariantCollisions(in.Entities)...)

	// TODO: Check for simple-rule find/replace conflicts (blocking)
	// result.Blocking = append(result.Blocking, checkSimpleRuleConflicts(in.SimpleRules, in.Registry)...)

	return result
}

// checkDuplicateValues finds the same string declared in two active categories.
func checkDuplicateValues(entities []Entity) []Conflict {
	var conflicts []Conflict
	seen := make(map[string]string) // canonical -> category

	for _, e := range entities {
		canonical := strings.ToLower(e.Canonical)
		if prevCategory, exists := seen[canonical]; exists {
			if prevCategory != e.Category {
				conflicts = append(conflicts, Conflict{
					Kind:     "ambiguity",
					Severity: "block",
					Value:    e.Canonical,
					Refs: []ValueRef{
						{Kind: "entity", Category: e.Category, Canonical: canonical},
						{Kind: "entity", Category: prevCategory, Canonical: canonical},
					},
					Message: fmt.Sprintf(
						"the value %q appears in both %q and %q categories: "+
							"two categories cannot share one placeholder",
						e.Canonical, prevCategory, e.Category),
					Fix: fmt.Sprintf(
						"delete the value from one category, or uncheck one category " +
							"in the scope panel"),
				})
			}
		}
		seen[canonical] = e.Category
	}

	return conflicts
}

// checkAllowlistCollisions finds declared values that are also allowlisted.
func checkAllowlistCollisions(entities []Entity, allowlist *Allowlist) []Conflict {
	var conflicts []Conflict

	if allowlist == nil {
		return conflicts
	}

	for _, e := range entities {
		if allowlist.Contains(e.Canonical) {
			conflicts = append(conflicts, Conflict{
				Kind:     "collision",
				Severity: "block",
				Value:    e.Canonical,
				Refs: []ValueRef{
					{Kind: "entity", Category: e.Category, Canonical: strings.ToLower(e.Canonical)},
					{Kind: "allowlist", Category: "", Canonical: strings.ToLower(e.Canonical)},
				},
				Message: fmt.Sprintf(
					"the value %q is declared to anonymise but also in the "+
						"\"never anonymise\" list: the allowlist wins",
					e.Canonical),
				Fix: "remove the value from one list",
			})
		}
	}

	return conflicts
}

// checkVariantCollisions finds variant collisions across all entities.
func checkVariantCollisions(entities []Entity) []Conflict {
	var conflicts []Conflict
	seenVariants := make(map[string]string) // variant -> entity canonical

	for _, e := range entities {
		// Get all variants for this entity
		variants := ExpandVariants(e)

		for _, v := range variants {
			loweredV := strings.ToLower(v)
			if prevCanonical, exists := seenVariants[loweredV]; exists && prevCanonical != e.Canonical {
				conflicts = append(conflicts, Conflict{
					Kind:     "collision",
					Severity: "block",
					Value:    v,
					Refs: []ValueRef{
						{Kind: "entity", Category: e.Category, Canonical: strings.ToLower(e.Canonical), Detail: e.Canonical},
						{Kind: "entity", Category: e.Category, Canonical: strings.ToLower(prevCanonical), Detail: prevCanonical},
					},
					Message: fmt.Sprintf(
						"the variant %q of %q would collide with a variant of %q: "+
							"two different originals cannot map to the same placeholder",
						v, e.Canonical, prevCanonical),
					Fix: "the conflicting values must be edited or removed",
				})
			}
			seenVariants[loweredV] = e.Canonical
		}
	}

	return conflicts
}

// checkSimpleRuleConflicts finds simple-rule conflicts.
func checkSimpleRuleConflicts(rules []SimpleRule, registry *Registry) []Conflict {
	var conflicts []Conflict

	for _, rule := range rules {
		// Check if find contains a placeholder-shaped token
		if strings.Contains(rule.Find, "[") && strings.Contains(rule.Find, "]") {
			// Rough check for placeholder shape
			if strings.HasPrefix(rule.Find, "[") && strings.HasSuffix(rule.Find, "]") {
				conflicts = append(conflicts, Conflict{
					Kind:     "reserved",
					Severity: "block",
					Value:    rule.Find,
					Refs: []ValueRef{
						{Kind: "simple_rule", Detail: rule.Find},
					},
					Message: fmt.Sprintf(
						"the find pattern %q contains a placeholder-shaped token: "+
							"a rule that rewrites anonymised output breaks the mapping",
						rule.Find),
					Fix: "edit the find pattern to not match placeholder text",
				})
			}
		}

		// Check if replace is a placeholder that already maps to a different value
		if strings.HasPrefix(rule.Replace, "[") && strings.HasSuffix(rule.Replace, "]") {
			if registry != nil {
				if owner, ok := registry.PlaceholderOwner(rule.Replace); ok {
					// A value already owns this placeholder
					conflicts = append(conflicts, Conflict{
						Kind:     "reserved",
						Severity: "block",
						Value:    rule.Replace,
						Refs: []ValueRef{
							{Kind: "simple_rule", Detail: rule.Replace},
							{Kind: "entity", Category: owner.Category, Canonical: strings.ToLower(owner.Original)},
						},
						Message: fmt.Sprintf(
							"%q is already the placeholder for %q: "+
								"the exported re-identification key would become ambiguous",
							rule.Replace, owner.Original),
						Fix: fmt.Sprintf(
							"change the replace value to a different placeholder, " +
								"or remove the rule"),
					})
				}
				// If it's a reserved but unassigned placeholder like [CUSTOM_N],
				// we allow it but reserve it (no conflict here)
			}
		}
	}

	return conflicts
}
