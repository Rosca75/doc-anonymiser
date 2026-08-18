// engine/values.go — Pass 2 of the pipeline: the accepted Values
// (CLAUDE.md §5), with their spelling expansion.
//
// A Value is a real-world thing to be replaced: a client, project, internal
// name or person. It has ONE main text, none or many SPELLINGS of the same
// thing ("Marie Duval" → "M. Duval", "Duval", "Marie", …), and exactly one
// placeholder for the whole family, so an informal reference and a formal one
// cannot end up as two different numbers. Spellings are matched longest first
// with word-boundary anchoring: "Alten" must never fire inside "Altenberg".
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

// Spelling policies for Value.SpellingPolicy.
//
// Kept as string constants rather than a bool so both states have a NAME on
// both sides of the bridge and in a session file. A bool called "autoExpand"
// forced every reader to work out which way round false meant.
const (
	// SpellingPolicyAutomatic means the engine may derive further spellings
	// from the main text according to the category. This is the state of every
	// Value the moment it is created, however it was found.
	SpellingPolicyAutomatic = "automatic"
	// SpellingPolicyCurated means the spellings are the user's. Main text plus
	// exactly the Spellings listed IS the complete replacement set, and the
	// engine derives nothing further.
	SpellingPolicyCurated = "curated"
)

// Value is one accepted replacement unit: one placeholder, one family of
// spellings.
type Value struct {
	// Category is one of AllValueCategories.
	Category string `json:"category"`
	// MainText is the primary textual form naming this Value. It is always
	// matched, and it is deliberately NOT duplicated in Spellings.
	MainText string `json:"mainText"`
	// Spellings are the alternative textual forms of the same Value, EXCLUDING
	// MainText. While the policy is automatic they sit ON TOP of what the engine
	// derives; once the user has curated the list they ARE the list.
	Spellings []string `json:"spellings,omitempty"`
	// SpellingPolicy is "automatic" or "curated". Empty reads as automatic, so
	// a producer that never sets it cannot accidentally freeze a Value's
	// spellings, and the frontend can send a Value without inventing a default.
	//
	// It goes curated the first time the user adds, deletes, renames or moves a
	// spelling. From then on the chips on the card are exactly what will be
	// replaced, which is what makes deleting a spelling STICK without a negative
	// rule. A per-Value list of suppressed spellings is a rule with no home in
	// the interface: invisible except as the absence of a chip, impossible to
	// undo, and doing the job of the never-anonymise list, which is the one
	// place a negative rule is meant to live and be visible.
	SpellingPolicy string `json:"spellingPolicy,omitempty"`
	// DiscoveryMethods is PROVENANCE: every method that found this Value
	// (matchclass.go). A set, because several can find the same thing, and
	// accepting a Suggestion keeps all of them. A manually declared Value
	// carries exactly ["manual"].
	//
	// It is deliberately not the same field as the match class precedence uses:
	// MatchClassForMethods reduces this set to one rank when a contest has to be
	// decided, and nothing reduces it anywhere else.
	DiscoveryMethods []string `json:"discoveryMethods,omitempty"`
	// Evidence is WHY a discovery method produced this Value, carried across
	// from the Suggestion it was accepted from. Structured and bounded, so the
	// workspace can explain a Value without the engine returning prose.
	Evidence []Evidence `json:"evidence,omitempty"`
	// Confidence is how much this Value is trusted, in [0.0, 1.0]. Zero means
	// "not stated", which DetectValues reads as ConfidenceManualDefault: a Value
	// the USER declared is a high-trust Value.
	//
	// A Value accepted from a Local AI Suggestion carries ConfidenceLLMDefault
	// instead. That is what gives the Configure panel's minimum-confidence
	// setting something real to act on: raising the minimum above the AI level
	// stops replacing Values only the model suggested, while everything the user
	// declared keeps being replaced.
	Confidence float32 `json:"confidence,omitempty"`
}

// Spelling derivation has three classes, and a category belongs to exactly one.
//
//	person        initials, surname-only, first-name-only, hyphen/space swaps
//	organisation  the name with a legal suffix stripped
//	literal       no expansion at all
//
// personCategories holds the first. Only person_names is a human being;
// entity_names in particular is dominated by organisations, and expanding
// "Delta Industries" into the surname spelling "Industries" would replace an
// ordinary noun everywhere it appears.
var personCategories = map[string]bool{
	CatPersonNames: true,
}

// literalOnlyCategories holds the third: Values with no name structure to
// derive from. A reference code like "PRJ-4471-A" has no legal suffix and no
// surname, and organisation-style stripping would happily remove a trailing
// token that resembles one and invent a spelling matching a DIFFERENT code.
// other_names is here because it is defined by exclusion: nothing is known
// about the shape of a Value filed there, so nothing can be inferred from it.
var literalOnlyCategories = map[string]bool{
	CatIdentifierNames: true,
	CatOtherNames:      true,
}

// nameParticles are the lower-case surname particles that glue multi-word
// surnames together ("Jean de la Croix" -> surname "de la Croix"). Checked
// case-insensitively.
var nameParticles = map[string]bool{
	"de": true, "la": true, "le": true, "du": true, "des": true,
	"van": true, "von": true, "der": true, "den": true, "d'": true,
}

// legalSuffixes lists the organisation legal forms this engine knows, longest
// first so "S.à r.l." wins over "S.A.". It is ONE table, used by both the
// variant expansion here and the smart detector in discover.go: with two
// tables, discovery proposed "Bidco SCSp" from a form only it knew, and the
// expansion could then not produce "Bidco".
//
// Suffixes are only ever STRIPPED, never added: adding one invents a name that
// may belong to a different legal value.
var legalSuffixes = []string{
	"S.à r.l.", "S.à.r.l.", "S.àr.l.", "Sàrl", "SARL",
	"S.C.A.", "S.C.S.", "SCSp", "S.p.A.", "S.A.", "SA", "ASBL", "SE",
	"GmbH", "AG", "N.V.", "B.V.", "Ltd", "Limited", "LLC", "plc", "SAS",
	"Inc.", "Inc",
}

// minSpellingLen guards against dangerously short spellings: replacing every
// "Al" or "Bo" would shred ordinary text. Shorter spellings are dropped from
// the derived expansion, and from a curated list too, because a 2-character
// spelling shreds text whether it was derived or typed.
const minSpellingLen = 3

// DerivesSpellings reports whether the engine may still derive spellings for
// this Value. An unset policy means yes: every Value arrives uncurated, and a
// producer that never touches the field must not accidentally freeze the list.
func (v Value) DerivesSpellings() bool {
	return v.SpellingPolicy != SpellingPolicyCurated
}

// ExpandSpellings returns every form the Value should match, main text first,
// deduplicated, longest first (the order replacement wants).
//
// A CURATED Value derives nothing: its main text plus exactly the listed
// Spellings. That is what the chips on the card show, so what the card shows is
// what the run replaces.
func ExpandSpellings(v Value) []string {
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		// Case-insensitive dedupe; keep the first spelling encountered.
		key := strings.ToLower(v)
		if v == "" || len([]rune(v)) < minSpellingLen || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, v)
	}

	add(v.MainText)

	if v.DerivesSpellings() {
		switch {
		case literalOnlyCategories[v.Category]:
			// Nothing to derive; the listed spellings below still apply.
		case personCategories[v.Category]:
			expandPersonInto(v.MainText, add)
		default:
			expandOrgInto(v.MainText, add)
		}
	}

	// The listed spellings last (typically added because the derived ones
	// missed a nickname, or because the user curated the list by hand).
	for _, spelling := range v.Spellings {
		add(spelling)
	}

	// Longest first, so longest-match-first replacement can simply walk
	// the list ("Marie Duval" is tried before "Marie").
	sortByLengthDesc(out)
	return out
}

// expandPersonInto derives person-name spellings: "First Last" →
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

// expandOrgInto derives organisation spellings: the name with its legal
// suffix stripped (if one is present), so "Alpine Trust S.A." also matches
// "Alpine Trust".
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

// DetectValues scans the text for every spelling of every Value and returns
// matching spans (category = the Value's category). Allowlisted terms are
// skipped here — before any replacement — per CLAUDE.md §5.
//
// The returned spans may overlap across Values; callers run ResolveOverlaps
// before ApplySpans, which realises the longest-match-first rule across ALL
// spellings of ALL Values.
func DetectValues(text string, values []Value, allow *Allowlist) []Span {
	var spans []Span
	for _, v := range values {
		for _, spelling := range ExpandSpellings(v) {
			if allow.Contains(spelling) {
				continue // allowlist wins over the Value list
			}
			// Case-insensitive literal search. RE2's \b is ASCII-only and
			// fails after accented letters ("Amélie…"), so boundaries are
			// verified on runes instead (isWordBoundary below).
			re, err := regexp.Compile(`(?i)` + regexp.QuoteMeta(spelling))
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
					Category: v.Category,
					Original: original,
					// Every spelling maps back to the main text, so
					// "M. Duval" and "Marie" share one placeholder.
					MainText: v.MainText,
					// A Value that states its own confidence keeps it (one
					// accepted from an AI Suggestion does); anything else is a
					// Value the user declared, which is high trust.
					Confidence: valueConfidence(v),
					MatchClass: MatchClassForMethods(v.DiscoveryMethods),
				})
			}
		}
	}
	return spans
}

// valueConfidence is the score DetectValues stamps on a Value's spans: the
// Value's own Confidence when it stated one, otherwise ConfidenceManualDefault.
// Keeping the "unset means declared by the user" rule in one function means a
// zero value can never be mistaken for "no confidence at all", which would make
// every user-declared Value filterable by accident.
func valueConfidence(v Value) float32 {
	if v.Confidence > 0 {
		return v.Confidence
	}
	return ConfidenceManualDefault
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

// CountTermMatches counts case-insensitive, word-boundary-anchored occurrences
// of term in text: the live "Found N times" preview for a manual declaration.
// Same boundary rule as pass 2, so the preview never promises a match the
// pipeline would reject ("Lux" does not match inside "Luxembourg").
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
// actionable error for the UI.
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
				Category:   CatCustomPatterns,
				Original:   original,
				Confidence: ConfidenceDeterministic,
				// A pattern the user wrote is a DECLARATION, the same act as
				// typing a Value, so it shares that rank. It loses to a built-in
				// pattern, which is pass 1 beating pass 2, and beats both
				// discovery methods, which is the user beating a guess.
				MatchClass: MatchClassUserDefined,
			})
		}
	}
	return spans
}
