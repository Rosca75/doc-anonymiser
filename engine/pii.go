// engine/pii.go — Pass 1 of the pipeline: deterministic regex detection of
// hard PII (CLAUDE.md §5), mirroring the notebook's deterministic pre-pass.
//
// Design (BUILD.md Phase 2):
//   - Detection returns SPANS (start, end, category, original), it never
//     mutates text. Replacement is a separate step (ApplySpans) applying
//     spans longest-first, non-overlapping — this span model is reused by
//     every later pass (entities, LLM deep-scan).
//   - Every regex is compiled once at package init (performance budget:
//     per-call compilation is the classic budget killer) and documented
//     with examples of what it matches and deliberately does NOT match.
//   - IBAN candidates additionally pass a mod-97 checksum in Go, killing
//     the false positives a regex alone would produce.
package engine

import (
	"regexp"
	"sort"
	"strings"
)

// Level is the anonymisation level (CLAUDE.md §5). It decides which PII
// categories fire in pass 1 and which entity categories later passes use.
type Level string

const (
	LevelSoft     Level = "soft"
	LevelMedium   Level = "medium" // default
	LevelAdvanced Level = "advanced"
)

// Span is one detected occurrence inside a document's markdown working
// form. Start/End are byte offsets (End exclusive), Original is the exact
// matched text — kept so the registry can map it to a stable placeholder.
type Span struct {
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Category string `json:"category"`
	Original string `json:"original"`
	// Canonical is the entity's canonical name when the span came from a
	// variant match ("M. Duval" → canonical "Marie Duval"), so every
	// variant shares one placeholder. Empty for PII spans (the matched
	// text IS the canonical value).
	Canonical string `json:"canonical,omitempty"`
}

// CanonicalOrOriginal returns the registry key for this span: the
// canonical entity name when set, the matched text otherwise.
func (s Span) CanonicalOrOriginal() string {
	if s.Canonical != "" {
		return s.Canonical
	}
	return s.Original
}

// PII category identifiers, used as registry categories and report keys.
const (
	CatEmail     = "email"
	CatPhone     = "phone"
	CatIBAN      = "iban"
	CatVAT       = "vat"
	CatMatricule = "matricule"
	CatURL       = "url"
	CatAmount    = "amount"
	CatDate      = "date"
)

// piiPattern couples a compiled regex with its category and the index of
// the capture group that holds the actual PII (0 = whole match). Group
// captures are needed because RE2 has no lookarounds: patterns like the
// matricule guard their boundaries with context characters that must not
// become part of the span.
type piiPattern struct {
	category string
	re       *regexp.Regexp
	group    int
	// validate, when set, gets the matched text and may veto the span
	// (used for the IBAN checksum).
	validate func(string) bool
}

// Which categories fire at which preset level lives in ONE place since
// BUILD-02 Phase 3: PresetSelection (pipeline.go). The patterns below are
// unconditional; DetectPIISelected gates them by the selection.

// The deterministic PII patterns, compiled once at package init
// (CLAUDE.md §6). Order matters only for readability; overlap resolution
// is longest-match-first regardless of pattern order.
var piiPatterns = []piiPattern{
	{
		// Email addresses.
		// Matches:      marie.duval@example.com, a+b@sub.domain.co.uk
		// Does not match: "user@localhost" (no TLD), "@handle" (no local part).
		category: CatEmail,
		re:       regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`),
	},
	{
		// URLs (including any that embed credentials — the whole URL is
		// replaced, so credentials never survive).
		// Matches:      https://example.com/a?b=c, http://user:pw@host.tld/x
		// Does not match: "example.com" (no scheme — too many false hits
		// on ordinary "word.word" text), "ftp://host" (not http/https).
		category: CatURL,
		re:       regexp.MustCompile(`https?://[^\s<>"')\]]+`),
	},
	{
		// IBAN candidates; the mod-97 checksum below is the real filter.
		// Matches:      LU28 0019 4006 4475 0000, DE89370400440532013000
		// Does not match: LU28 0019 4006 4475 0001 (checksum fails, vetoed
		// by validate), "LUXEMBOURG" (needs 2 check digits after country).
		category: CatIBAN,
		re:       regexp.MustCompile(`\b[A-Z]{2}[0-9]{2}(?:[ ]?[A-Z0-9]{4}){2,7}(?:[ ]?[A-Z0-9]{1,4})?\b`),
		validate: validIBAN,
	},
	{
		// EU VAT numbers — the formats relevant to the owner's market
		// (LU, FR, DE, BE, NL, AT) plus the generic CC+digits shape.
		// Matches:      LU12345678, FR40303265045, DE123456789, BE0123456789
		// Does not match: "LUXEMBOURG" (letters after the country code),
		// "LU1234" (too few digits).
		category: CatVAT,
		re:       regexp.MustCompile(`\b(?:LU[0-9]{8}|FR[0-9A-Z]{2}[0-9]{9}|DE[0-9]{9}|BE0?[0-9]{9,10}|NL[0-9]{9}B[0-9]{2}|ATU[0-9]{8})\b`),
	},
	{
		// Luxembourg 13-digit matricule (national ID). RE2 has no
		// lookarounds, so non-digit context is captured around group 1.
		// Matches:      1893120105732 (as a standalone 13-digit run)
		// Does not match: 189312010573 (12 digits), 18931201057321
		// (14 digits — the context guard rejects a digit neighbour).
		category: CatMatricule,
		re:       regexp.MustCompile(`(?:^|[^0-9])([0-9]{13})(?:[^0-9]|$)`),
		group:    1,
	},
	{
		// Phone numbers: international (+352 621 000 111, 0033 6 12 34 56 78)
		// and LU/FR/BE/DE national formats (06 12 34 56 78, 0621 456 789).
		// Matches:      +352 621 000 111, +33 6 12 34 56 78, 06 12 34 56 78
		// Does not match: 1893120105732 (no leading +/0 prefix shape),
		// plain years like 2026 (too short).
		category: CatPhone,
		re:       regexp.MustCompile(`(?:\+|00)[1-9][0-9]{0,2}(?:[ .\-/]?[0-9]{1,4}){2,5}|\b0[0-9](?:[ .\-/]?[0-9]{2,4}){3,5}\b`),
	},
	{
		// Monetary amounts — ADVANCED level only (CLAUDE.md §5).
		// Matches:      €1,500.00, EUR 12 500, 1.250,50 €, $99
		// Does not match: bare numbers without a currency marker (they are
		// too common in ordinary text to replace safely).
		category: CatAmount,
		re:       regexp.MustCompile(`(?:€|EUR|USD|GBP|CHF|\$|£)\s?[0-9]{1,3}(?:[.,' ][0-9]{3})*(?:[.,][0-9]{1,2})?|\b[0-9]{1,3}(?:[.,' ][0-9]{3})*(?:[.,][0-9]{1,2})?\s?(?:€|EUR|USD|GBP|CHF|\$|£)`),
	},
	{
		// Dates — ADVANCED level only. Three shapes: ISO (2026-07-23),
		// numeric EU (23/07/2026, 23.07.26) and written month, English and
		// French (23 July 2026, July 23, 2026, 23 juillet 2026).
		// Does not match: bare years ("2026"), "1/2" (fractions).
		category: CatDate,
		re: regexp.MustCompile(`\b[0-9]{4}-[0-9]{2}-[0-9]{2}\b` +
			`|\b[0-9]{1,2}[./][0-9]{1,2}[./][0-9]{2,4}\b` +
			`|\b[0-9]{1,2}(?:st|nd|rd|th)?\s(?:January|February|March|April|May|June|July|August|September|October|November|December|janvier|février|mars|avril|mai|juin|juillet|août|septembre|octobre|novembre|décembre)\s[0-9]{4}\b` +
			`|\b(?:January|February|March|April|May|June|July|August|September|October|November|December)\s[0-9]{1,2},?\s[0-9]{4}\b`),
	},
}

// DetectPII runs every level-appropriate pattern over the text and returns
// the raw spans (possibly overlapping — e.g. an email inside a URL).
// Callers resolve overlaps via ResolveOverlaps / ApplySpans.
//
// Since BUILD-02 Phase 3 this is a thin preset wrapper over
// DetectPIISelected; the granular selection is what the pipeline uses.
func DetectPII(text string, level Level) []Span {
	return DetectPIISelected(text, PresetSelection(level))
}

// DetectPIISelected runs exactly the PII patterns whose category is
// enabled in the selection (BUILD-02 Phase 3 granular switches).
func DetectPIISelected(text string, sel CategorySelection) []Span {
	var spans []Span
	for _, p := range piiPatterns {
		if !sel[p.category] {
			continue
		}
		for _, m := range p.re.FindAllStringSubmatchIndex(text, -1) {
			start, end := m[2*p.group], m[2*p.group+1]
			if start < 0 || end <= start {
				continue
			}
			original := text[start:end]
			if p.validate != nil && !p.validate(original) {
				continue
			}
			spans = append(spans, Span{
				Start:    start,
				End:      end,
				Category: p.category,
				Original: original,
			})
		}
	}
	return spans
}


// ResolveOverlaps keeps a non-overlapping subset of spans, longest match
// first (BUILD.md Phase 2: "an email inside a URL resolves
// deterministically" — the URL is longer, so the URL wins). Ties on length
// break by earlier start, then by category name, making the outcome fully
// deterministic. The result is sorted by Start.
func ResolveOverlaps(spans []Span) []Span {
	ordered := make([]Span, len(spans))
	copy(ordered, spans)
	sort.Slice(ordered, func(i, j int) bool {
		li, lj := ordered[i].End-ordered[i].Start, ordered[j].End-ordered[j].Start
		if li != lj {
			return li > lj // longest first
		}
		if ordered[i].Start != ordered[j].Start {
			return ordered[i].Start < ordered[j].Start
		}
		return ordered[i].Category < ordered[j].Category
	})

	var kept []Span
	for _, s := range ordered {
		overlaps := false
		for _, k := range kept {
			if s.Start < k.End && k.Start < s.End {
				overlaps = true
				break
			}
		}
		if !overlaps {
			kept = append(kept, s)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Start < kept[j].Start })
	return kept
}

// ApplySpans replaces every span in text with the placeholder returned by
// assign. Spans must already be non-overlapping (run ResolveOverlaps
// first); they are applied back-to-front so earlier offsets stay valid
// during replacement. It returns the rewritten text.
func ApplySpans(text string, spans []Span, assign func(Span) string) string {
	if len(spans) == 0 {
		return text
	}
	ordered := make([]Span, len(spans))
	copy(ordered, spans)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Start < ordered[j].Start })

	// Single forward pass over the text: copy the untouched stretches,
	// splice placeholders in between. O(len(text) + placeholders), which
	// keeps the 50-document budget comfortable even for PII-dense files.
	var b strings.Builder
	b.Grow(len(text))
	last := 0
	for _, s := range ordered {
		b.WriteString(text[last:s.Start])
		b.WriteString(assign(s))
		last = s.End
	}
	b.WriteString(text[last:])
	return b.String()
}

// validIBAN implements the ISO 13616 mod-97 check: move the first four
// characters to the end, convert letters to numbers (A=10 … Z=35) and the
// whole number must be ≡ 1 (mod 97). This turns the loose IBAN regex into
// a near-zero-false-positive detector — a mutated digit fails the check.
func validIBAN(s string) bool {
	compact := strings.ReplaceAll(s, " ", "")
	if len(compact) < 15 || len(compact) > 34 {
		return false
	}
	rearranged := compact[4:] + compact[:4]
	remainder := 0
	for _, c := range rearranged {
		var digits string
		switch {
		case c >= '0' && c <= '9':
			digits = string(c)
		case c >= 'A' && c <= 'Z':
			// Letters expand to two digits (A=10 … Z=35).
			n := int(c-'A') + 10
			digits = string(rune('0'+n/10)) + string(rune('0'+n%10))
		default:
			return false
		}
		for _, d := range digits {
			remainder = (remainder*10 + int(d-'0')) % 97
		}
	}
	return remainder == 1
}
