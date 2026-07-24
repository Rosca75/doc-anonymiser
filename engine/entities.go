// engine/entities.go — Pass 2 of the pipeline: known engagement entities
// (CLAUDE.md §5), with the notebook's variant expansion.
//
// An Entity is a real-world name the user (or LLM discovery) declared:
// a client, project, internal name or person. Each entity expands into
// name VARIANTS ("Marie Duval" → "M. Duval", "Duval", "Marie", …) so the
// pass catches informal references, then all variants are matched
// longest-first with word-boundary anchoring — "Alten" must never fire
// inside "Altenberg" (BUILD.md Phase 3).
//
// Matching is case-insensitive (headers shout "ALPINE TRUST"), and every
// span is checked against the allowlist BEFORE being kept — an allowlisted
// term is never replaced, by any pass (CLAUDE.md §5).
package engine

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Entity is one known engagement entity.
type Entity struct {
	// Category is one of the CLAUDE.md §5 entity categories:
	// client_names, project_names, internal_names, person_names.
	Category string `json:"category"`
	// Canonical is the full name as entered/discovered.
	Canonical string `json:"canonical"`
	// ManualVariants are extra spellings added by the user in the review
	// UI, on top of the automatic expansion.
	ManualVariants []string `json:"manualVariants,omitempty"`
	// ExcludedVariants are spellings this entity must NOT match, even
	// when the automatic expansion would produce them. Written by the
	// variant drag-and-drop regrouping (BUILD-02 Phase 9d): moving a
	// variant to another entity excludes it here so exactly one entity
	// matches it afterwards.
	ExcludedVariants []string `json:"excludedVariants,omitempty"`
}

// personCategories lists the categories whose canonical names are people —
// they get person-style variant expansion (initials, surname-only, …).
// Everything else is treated as an organisation-style name.
var personCategories = map[string]bool{
	"person_names":       true,
	"internal_names": true, // internal names are staff names
}

// nameParticles are the lower-case surname particles that glue multi-word
// surnames together ("Jean de la Croix" → surname "de la Croix"). Checked
// case-insensitively; documented per BUILD.md Phase 3.
var nameParticles = map[string]bool{
	"de": true, "la": true, "le": true, "du": true, "des": true,
	"van": true, "von": true, "der": true, "den": true, "d'": true,
}

// legalSuffixes is the documented list of organisation legal forms that
// are stripped to produce a suffix-free variant ("Alpine Trust S.A." also
// matches "Alpine Trust"). Longest-first so "S.à r.l." wins over "S.A.".
var legalSuffixes = []string{
	"S.à r.l.", "S.à.r.l.", "S.àr.l.", "Sàrl", "SARL",
	"S.A.", "SA", "GmbH", "AG", "SE", "Ltd", "Limited", "LLC", "plc", "Inc.", "Inc",
}

// minVariantLen guards against dangerously short variants: replacing every
// "Al" or "Bo" would shred ordinary text. Shorter variants are dropped
// from the automatic expansion (manual variants are kept as typed — the
// user explicitly asked for them).
const minVariantLen = 3

// ExpandVariants returns every spelling the entity should match, canonical
// first, deduplicated, longest first (the order replacement wants).
func ExpandVariants(e Entity) []string {
	// Excluded spellings never make it into the list (drag-and-drop
	// regrouping moved them to another entity, Phase 9d).
	excluded := map[string]bool{}
	for _, x := range e.ExcludedVariants {
		excluded[strings.ToLower(strings.TrimSpace(x))] = true
	}

	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		// Case-insensitive dedupe; keep the first spelling encountered.
		key := strings.ToLower(v)
		if v == "" || len([]rune(v)) < minVariantLen || seen[key] || excluded[key] {
			return
		}
		seen[key] = true
		out = append(out, v)
	}

	add(e.Canonical)

	if personCategories[e.Category] {
		expandPersonInto(e.Canonical, add)
	} else {
		expandOrgInto(e.Canonical, add)
	}

	// Manual variants last (typically added because the automatic ones
	// missed a nickname); dedupe still applies but not the length guard —
	// the user asked for them explicitly.
	for _, v := range e.ManualVariants {
		v = strings.TrimSpace(v)
		key := strings.ToLower(v)
		if v != "" && !seen[key] && !excluded[key] {
			seen[key] = true
			out = append(out, v)
		}
	}

	// Longest first, so longest-match-first replacement can simply walk
	// the list ("Marie Duval" is tried before "Marie").
	sortByLengthDesc(out)
	return out
}

// expandPersonInto generates person-name variants: "First Last" →
// "F. Last", "F.Last", "Last", "First", with hyphen/space swaps and
// French-particle-aware surnames ("Jean de la Croix" → "de la Croix",
// "Croix").
func expandPersonInto(name string, add func(string)) {
	// Hyphen/space swaps of the full name ("Jean-Claude Muller" also
	// matches "Jean Claude Muller" and vice versa).
	if strings.Contains(name, "-") {
		add(strings.ReplaceAll(name, "-", " "))
	}
	if strings.Contains(name, " ") {
		add(strings.ReplaceAll(name, " ", "-"))
	}

	fields := strings.Fields(name)
	if len(fields) < 2 {
		return // a single token has no first/last structure to expand
	}

	first := fields[0]
	// The surname starts at the first particle if any ("de la Croix"),
	// otherwise it is everything after the first name.
	surnameStart := 1
	for i := 1; i < len(fields)-1; i++ {
		if nameParticles[strings.ToLower(fields[i])] {
			surnameStart = i
			break
		}
	}
	surname := strings.Join(fields[surnameStart:], " ")
	lastWord := fields[len(fields)-1]

	initial := string([]rune(first)[0])
	add(initial + ". " + surname) // "J. de la Croix"
	add(initial + "." + surname)  // "J.de la Croix" (tight form)
	add(surname)                  // "de la Croix"
	add(first)                    // "Jean"
	if lastWord != surname {
		add(lastWord) // "Croix" — the bare family name without particles
	}
}

// expandOrgInto generates organisation variants: the name with its legal
// suffix stripped (if one is present) so "Alpine Trust S.A." also matches
// "Alpine Trust". Suffixes are never ADDED — that would invent names that
// may belong to different legal entities.
func expandOrgInto(name string, add func(string)) {
	for _, suffix := range legalSuffixes {
		trimmed := strings.TrimSuffix(name, " "+suffix)
		if trimmed != name {
			add(trimmed)
			return // strip at most one suffix, longest listed first
		}
	}
}

// sortByLengthDesc orders strings longest first (rune length, then
// alphabetically for determinism).
func sortByLengthDesc(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0; j-- {
			li, lj := len([]rune(ss[j-1])), len([]rune(ss[j]))
			if li > lj || (li == lj && ss[j-1] <= ss[j]) {
				break
			}
			ss[j-1], ss[j] = ss[j], ss[j-1]
		}
	}
}

// DetectEntities scans the text for every variant of every entity and
// returns matching spans (category = the entity's category). Allowlisted
// terms are skipped here — before any replacement — per CLAUDE.md §5.
//
// The returned spans may overlap across entities; callers run
// ResolveOverlaps (longest match wins) before ApplySpans, which realises
// the longest-match-first rule across ALL entity variants.
func DetectEntities(text string, entities []Entity, allow *Allowlist) []Span {
	var spans []Span
	for _, e := range entities {
		for _, variant := range ExpandVariants(e) {
			if allow.Contains(variant) {
				continue // allowlist wins over the entity list
			}
			// Case-insensitive literal search. RE2's \b is ASCII-only and
			// fails after accented letters ("Amélie…"), so boundaries are
			// verified on runes instead (isWordBoundary below).
			re, err := regexp.Compile(`(?i)` + regexp.QuoteMeta(variant))
			if err != nil {
				continue // QuoteMeta makes this unreachable; stay safe
			}
			for _, m := range re.FindAllStringIndex(text, -1) {
				if !isWordBoundary(text, m[0], m[1]) {
					continue // "Alten" inside "Altenberg" — reject
				}
				original := text[m[0]:m[1]]
				if allow.Contains(original) {
					continue
				}
				spans = append(spans, Span{
					Start:    m[0],
					End:      m[1],
					Category: e.Category,
					Original: original,
					// Every variant maps back to the canonical name so
					// "M. Duval" and "Marie" share one placeholder.
					Canonical:  e.Canonical,
					Confidence: ConfidenceManualDefault,
				})
			}
		}
	}
	return spans
}

// isWordBoundary reports whether text[start:end] is delimited by
// non-letter/digit runes (or the text edges) on both sides. Unicode-aware,
// unlike RE2's ASCII \b, so it works for accented names.
func isWordBoundary(text string, start, end int) bool {
	if start > 0 {
		r := lastRuneBefore(text, start)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	if end < len(text) {
		r := firstRuneAt(text, end)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func lastRuneBefore(s string, i int) rune {
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return r
}

func firstRuneAt(s string, i int) rune {
	r, _ := utf8.DecodeRuneInString(s[i:])
	return r
}

// CountTermMatches counts case-insensitive, word-boundary-anchored
// occurrences of term in text (BUILD-02 Phase 9c: the live "Found N
// times" preview for manual entries). Same boundary rule as the entity
// pass, so the preview never promises a match the pipeline would reject
// ("Lux" does not match inside "Luxembourg").
func CountTermMatches(text, term string) int {
	term = strings.TrimSpace(term)
	if term == "" {
		return 0
	}
	re, err := regexp.Compile(`(?i)` + regexp.QuoteMeta(term))
	if err != nil {
		return 0 // QuoteMeta makes this unreachable; stay safe
	}
	n := 0
	for _, m := range re.FindAllStringIndex(text, -1) {
		if isWordBoundary(text, m[0], m[1]) {
			n++
		}
	}
	return n
}

// CustomPattern is a user-supplied regex (category custom_patterns).
type CustomPattern struct {
	// Expr is the regular expression as typed by the user.
	Expr string `json:"expr"`
}

// ValidateCustomPattern compile-checks a user regex and returns an
// actionable error for the UI (BUILD.md Phase 3 activity 3).
func ValidateCustomPattern(expr string) error {
	if strings.TrimSpace(expr) == "" {
		return fmt.Errorf("the pattern is empty, enter a regular expression, e.g. PRJ-[0-9]+ to match project codes")
	}
	if _, err := regexp.Compile(expr); err != nil {
		return fmt.Errorf("the pattern %q is not a valid regular expression (%v), check for unbalanced brackets or a trailing backslash", expr, err)
	}
	return nil
}

// DetectCustomPatterns runs every valid user pattern over the text.
// Invalid patterns are skipped here (the UI surfaces validation errors at
// entry time via ValidateCustomPattern); allowlisted matches are dropped.
func DetectCustomPatterns(text string, patterns []CustomPattern, allow *Allowlist) []Span {
	var spans []Span
	for _, p := range patterns {
		re, err := regexp.Compile(p.Expr)
		if err != nil {
			continue
		}
		for _, m := range re.FindAllStringIndex(text, -1) {
			if m[1] <= m[0] {
				continue // ignore empty matches (e.g. patterns like "a*")
			}
			original := text[m[0]:m[1]]
			if allow.Contains(original) {
				continue
			}
			spans = append(spans, Span{
				Start:      m[0],
				End:        m[1],
				Category:   "custom_patterns",
				Original:   original,
				Confidence: ConfidenceDeterministic,
			})
		}
	}
	return spans
}
