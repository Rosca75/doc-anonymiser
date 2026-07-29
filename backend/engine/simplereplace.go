// engine/simplereplace.go — the manual find-and-replace pass (CLAUDE.md
// §3), mirroring notebook Cell 8. Rules are LITERAL (no regex — users who
// want regex use custom_patterns), ORDERED (rule 1 runs before rule 2, and
// a later rule sees the earlier rule's output) and individually toggleable
// between case-sensitive and case-insensitive matching.
//
// This pass runs LAST in the pipeline, after the registry post-pass, so
// users can clean up anything the automated passes left behind. Counts per
// rule are recorded in the report.
package engine

import "strings"

// SimpleRule is one ordered find → replace instruction.
type SimpleRule struct {
	// Find is the literal text to search for. Empty Find is a no-op.
	Find string `json:"find"`
	// Replace is the literal replacement (may be empty = delete).
	Replace string `json:"replace"`
	// CaseSensitive: when false (default), "Foo" also matches "foo"/"FOO".
	CaseSensitive bool `json:"caseSensitive"`
}

// ApplySimpleRules applies the rules in order and returns the rewritten
// text plus the number of replacements per rule (index-aligned with the
// input — the report shows the user which rules actually did something).
func ApplySimpleRules(text string, rules []SimpleRule) (string, []int) {
	counts := make([]int, len(rules))
	for i, rule := range rules {
		if rule.Find == "" {
			continue // an empty needle would loop forever; skip it
		}
		text, counts[i] = replaceLiteral(text, rule)
	}
	return text, counts
}

// replaceLiteral performs one rule over the whole text. The
// case-insensitive branch scans on a lower-cased shadow copy while
// splicing the ORIGINAL text, so untouched regions keep their casing.
func replaceLiteral(text string, rule SimpleRule) (string, int) {
	if rule.CaseSensitive {
		n := strings.Count(text, rule.Find)
		return strings.ReplaceAll(text, rule.Find, rule.Replace), n
	}

	needle := strings.ToLower(rule.Find)
	var b strings.Builder
	b.Grow(len(text))
	count := 0
	for {
		idx := strings.Index(strings.ToLower(text), needle)
		if idx < 0 {
			b.WriteString(text)
			break
		}
		b.WriteString(text[:idx])
		b.WriteString(rule.Replace)
		text = text[idx+len(rule.Find):]
		count++
	}
	return b.String(), count
}
