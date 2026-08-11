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
	// Confidence in [0.0, 1.0] (BUILD-03 Phase C). Deterministic regex hits
	// default to 1.0; LLM proposals default to ConfidenceLLMDefault; manual
	// entities to ConfidenceManualDefault. Context-word boosting may nudge a
	// value up (capped at 1.0). Zero means "not scored" and is treated as
	// 1.0 by the threshold filter for back-compat.
	Confidence float32 `json:"confidence,omitempty"`
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
	// BUILD-03 Phase B — extended recognizers inspired by Presidio's
	// deterministic layer. All are hard PII (fire at every level).
	CatCreditCard  = "credit_card"  // Visa/Mastercard/Amex, Luhn-validated
	CatNHS         = "uk_nhs"       // UK National Health Service number, mod-11 validated
	CatIPAddress   = "ip_address"   // IPv4 + IPv6
	CatMACAddress  = "mac_address"  // 48-bit MAC (colon/hyphen separated)
	CatCrypto      = "crypto"       // Bitcoin (P2PKH/P2SH/Bech32)
	CatDatabaseURI = "database_uri" // postgres://, mysql://, mongodb://, redis:// with creds
	CatDESteuerID  = "de_steuer_id" // Germany national tax ID (11 digits)
	CatESNIF       = "es_nif"       // Spain NIF (8 digits + letter, letter validated)
)

// piiPattern couples a compiled regex with its category and the index of
// the capture group that holds the actual PII (0 = whole match). Group
// captures are needed because RE2 has no lookarounds: patterns like the
// matricule guard their boundaries with context characters that must not
// become part of the span.
type piiPattern struct {
	category  string
	re        *regexp.Regexp
	group     int
	countries []string
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
		category:  CatVAT,
		re:        regexp.MustCompile(`\bLU[0-9]{8}\b`),
		countries: []string{CountryLU},
	},
	{
		category:  CatVAT,
		re:        regexp.MustCompile(`\bFR[0-9A-Z]{2}[0-9]{9}\b`),
		countries: []string{CountryFR},
	},
	{
		category:  CatVAT,
		re:        regexp.MustCompile(`\bDE[0-9]{9}\b`),
		countries: []string{CountryDE},
	},
	{
		category:  CatVAT,
		re:        regexp.MustCompile(`\bES[A-Z0-9][0-9]{7}[A-Z0-9]\b`),
		countries: []string{CountryES},
	},
	{
		category:  CatVAT,
		re:        regexp.MustCompile(`\bGB(?:[0-9]{9}|[0-9]{12})\b`),
		countries: []string{CountryUK},
	},
	{
		// Luxembourg 13-digit matricule (national ID). RE2 has no
		// lookarounds, so non-digit context is captured around group 1.
		// Matches:      1893120105732 (as a standalone 13-digit run)
		// Does not match: 189312010573 (12 digits), 18931201057321
		// (14 digits — the context guard rejects a digit neighbour).
		category:  CatMatricule,
		re:        regexp.MustCompile(`(?:^|[^0-9])([0-9]{13})`),
		group:     1,
		countries: []string{CountryLU},
	},
	{
		// Phone numbers: international (+352 621 000 111, 0033 6 12 34 56 78)
		// and LU/FR/BE/DE national formats (06 12 34 56 78, 0621 456 789).
		// Matches:      +352 621 000 111, +33 6 12 34 56 78, 06 12 34 56 78
		// Does not match: 1893120105732 (no leading +/0 prefix shape),
		// plain years like 2026 (too short).
		category:  CatPhone,
		re:        regexp.MustCompile(`(?:\+352|00352)(?:[ .\-/]?[0-9]{2,4}){2,4}|\b(?:2[0-9]|4[0-9]|5[0-9]|6[0-9]|7[0-9]|8[0-9]|9[0-9])(?:[ .\-/]?[0-9]{2}){3}\b`),
		countries: []string{CountryLU},
		validate:  validLUPhone,
	},
	{
		category:  CatPhone,
		re:        regexp.MustCompile(`(?:\+33|0033)(?:[ .\-/]?[0-9])(?:[ .\-/]?[0-9]{2}){4}|\b0[1-9](?:[ .\-/]?[0-9]{2}){4}\b`),
		countries: []string{CountryFR},
		validate:  validFRPhone,
	},
	{
		category:  CatPhone,
		re:        regexp.MustCompile(`(?:\+49|0049)(?:[ .\-/]?[0-9]{1,4}){3,5}|\b0[1-9][0-9]{1,4}(?:[ .\-/]?[0-9]{2,4}){2,3}\b`),
		countries: []string{CountryDE},
		validate:  validDEPhone,
	},
	{
		category:  CatPhone,
		re:        regexp.MustCompile(`(?:\+34|0034)(?:[ .\-/]?[0-9]{3}){3}\b|\b[6789][0-9]{2}(?:[ .\-/]?[0-9]{3}){2}\b`),
		countries: []string{CountryES},
		validate:  validESPhone,
	},
	{
		category:  CatPhone,
		re:        regexp.MustCompile(`(?:\+44|0044)(?:[ .\-/]?[0-9]{2,4}){3,4}|\b0(?:7[0-9]{3}|1[0-9]{2,4}|2[0-9]{2,4})(?:[ .\-/]?[0-9]{3,4}){2}\b`),
		countries: []string{CountryUK},
		validate:  validUKPhone,
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

	// --- BUILD-03 Phase B: extended recognizers -------------------------

	{
		// Credit card numbers (Visa, Mastercard, Amex, Discover). 13–19
		// digits, optionally space/hyphen grouped. The Luhn checksum below
		// drops ~99% of the regex-only false positives.
		// Matches:      4532 0151 1283 0366, 4532015112830366, 3782-822463-10005
		// Does not match: 4532 0151 1283 0367 (mutated → Luhn fail),
		// arbitrary 16-digit runs (they must pass Luhn).
		category: CatCreditCard,
		// Two shapes: compact 13–19 digits, and 4-N-N... grouped forms
		// (accommodates 4-4-4-4 for Visa/MC and 4-6-5 for Amex). Luhn
		// vetoes the vast majority of accidental matches.
		re:       regexp.MustCompile(`\b[0-9]{13,19}\b|\b[0-9]{4}(?:[ \-][0-9]{4,6}){2,3}[0-9]?\b`),
		validate: validLuhn,
	},
	{
		// UK NHS number: 10 digits, spaces allowed as "NNN NNN NNNN".
		// Mod-11 checksum validation.
		// Matches:      485 777 3456 (valid mod-11), 4857773456
		// Does not match: 485 777 3457 (mutated → checksum fail)
		category:  CatNHS,
		re:        regexp.MustCompile(`\b[0-9]{3}[ \-]?[0-9]{3}[ \-]?[0-9]{4}\b|\b[0-9]{10}\b`),
		countries: []string{CountryUK},
		validate:  validNHS,
	},
	{
		// IPv4 addresses. Simple dotted-quad with a range check in
		// validate (each octet must be 0–255).
		// Matches:      192.168.0.1, 10.0.0.255
		// Does not match: 999.1.2.3 (validate vetoes), 1.2.3 (regex fails)
		category: CatIPAddress,
		re:       regexp.MustCompile(`\b[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\b`),
		validate: validIPv4,
	},
	{
		// IPv6 addresses — full 8-group form and compressed :: form.
		// Matches:      2001:db8::1, fe80::1, 2001:db8:0:0:0:0:0:1
		// Does not match: bare hex without any colon.
		category: CatIPAddress,
		re:       regexp.MustCompile(`\b(?:[0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}\b|(?:[0-9A-Fa-f]{1,4}:){1,7}:[0-9A-Fa-f]{1,4}(?::[0-9A-Fa-f]{1,4})*`),
	},
	{
		// MAC addresses: 48-bit, colon or hyphen separated.
		// Matches:      00:1A:2B:3C:4D:5E, 00-1a-2b-3c-4d-5e
		// Does not match: 00:1A:2B:3C:4D (5 groups)
		category: CatMACAddress,
		re:       regexp.MustCompile(`\b[0-9A-Fa-f]{2}(?:[:\-][0-9A-Fa-f]{2}){5}\b`),
	},
	{
		// Bitcoin addresses (P2PKH starts with 1, P2SH with 3, Bech32
		// mainnet with bc1). Length bounds match the network's real range.
		// Matches:      1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2, bc1qw508d6...
		// Does not match: shorter alphanumeric runs; casual English words.
		category: CatCrypto,
		re:       regexp.MustCompile(`\b(?:[13][a-km-zA-HJ-NP-Z1-9]{25,34}|bc1[a-z0-9]{25,62})\b`),
	},
	{
		// Database connection URIs with embedded credentials — the whole
		// URI is replaced, so credentials never survive.
		// Matches:      postgres://user:pw@host:5432/db, mongodb+srv://u:p@h/db
		// Does not match: postgres:// without user:pw@ (no credential leak)
		category: CatDatabaseURI,
		re:       regexp.MustCompile(`\b(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis|amqp|mssql|jdbc:[a-z]+)://[^\s<>"')\]]+`),
	},
	{
		// Germany Steuer-ID (tax ID): 11 digits, not starting with 0.
		// Pattern-only; checksum validation is complex and its false-
		// positive rate is low in office documents once the leading-0 rule
		// is enforced.
		// Matches:      12345678901
		// Does not match: 01234567890 (leading 0), 1234567890 (10 digits)
		category:  CatDESteuerID,
		re:        regexp.MustCompile(`(?:^|[^0-9])([1-9][0-9]{10})`),
		group:     1,
		countries: []string{CountryDE},
	},
	{
		// Spain NIF (Número de Identificación Fiscal): 8 digits + a
		// letter whose value is derived from digits mod 23.
		// Matches:      12345678Z (valid), 00000000T
		// Does not match: 12345678A (letter wrong for mod-23)
		category:  CatESNIF,
		re:        regexp.MustCompile(`\b([0-9]{8})([A-Za-z])\b`),
		countries: []string{CountryES},
		validate:  validNIF,
	},
}

// DetectPIISelected runs exactly the PII patterns whose category is
// enabled in the selection (BUILD-02 Phase 3 granular switches) AND whose
// country scope covers the requested country (BUILD-06 Phase 1).
// Every returned span carries Confidence = ConfidenceDeterministic (1.0).
//
// Two country gates apply, and they are deliberately different things:
//
//  1. The CATEGORY gate (CategoryCountries in country.go). A whole category
//     can be national: the Luxembourg matricule means nothing under a French
//     selection, so the category never runs there.
//  2. The PATTERN gate (piiPattern.countries). Categories such as VAT and
//     phone apply everywhere, but each of their patterns is ONE national
//     format. Only the pattern for the selected country runs. Without this
//     gate every national format is scanned over every document, which both
//     costs a full regex pass per country and reports, say, a French VAT
//     number to a user who selected Luxembourg.
//
// An empty country means "not chosen yet" and falls back to CountryLU, the
// same default the pipeline and the same-format exporter apply, so a direct
// engine caller cannot silently lose all country-scoped detection.
func DetectPIISelected(text string, sel CategorySelection, country string) []Span {
	if country == "" {
		country = CountryLU
	}
	var spans []Span
	for _, p := range piiPatterns {
		if !sel[p.category] {
			continue
		}
		// Gate 1: the category itself must apply to this country.
		if !CategoryAppliesTo(p.category, country) {
			continue
		}
		// Gate 2: a pattern that names countries is a national format and
		// runs only for the country it belongs to.
		if len(p.countries) > 0 && !countryInList(country, p.countries) {
			continue
		}
		for _, m := range p.re.FindAllStringSubmatchIndex(text, -1) {
			start, end := m[2*p.group], m[2*p.group+1]
			if start < 0 || end <= start {
				continue
			}
			if !hasTrailingDigitBoundary(text, end) {
				continue
			}
			original := text[start:end]
			if p.validate != nil && !p.validate(original) {
				continue
			}
			spans = append(spans, Span{
				Start:      start,
				End:        end,
				Category:   p.category,
				Original:   original,
				Confidence: ConfidenceDeterministic,
			})
		}
	}
	return spans
}

// countryInList reports whether code is one of the listed country codes.
// Kept as a plain loop (the lists hold at most a handful of codes, so a map
// would cost more than it saves) and used by the pattern-level country gate.
func countryInList(code string, list []string) bool {
	for _, c := range list {
		if c == code {
			return true
		}
	}
	return false
}

func hasTrailingDigitBoundary(text string, end int) bool {
	if end >= len(text) {
		return true
	}
	next := text[end]
	return next < '0' || next > '9'
}

func digitsOnly(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func validLUPhone(s string) bool {
	digits := digitsOnly(s)
	if strings.HasPrefix(digits, "352") {
		digits = digits[3:]
	}
	return len(digits) == 9
}

func validFRPhone(s string) bool {
	digits := digitsOnly(s)
	if strings.HasPrefix(digits, "33") {
		digits = "0" + digits[2:]
	}
	return len(digits) == 10 && digits[0] == '0'
}

func validDEPhone(s string) bool {
	digits := digitsOnly(s)
	if strings.HasPrefix(digits, "49") {
		digits = "0" + digits[2:]
	}
	return len(digits) >= 10 && len(digits) <= 12 && digits[0] == '0'
}

func validESPhone(s string) bool {
	digits := digitsOnly(s)
	if strings.HasPrefix(digits, "34") {
		digits = digits[2:]
	}
	return len(digits) == 9 && strings.ContainsRune("6789", rune(digits[0]))
}

func validUKPhone(s string) bool {
	digits := digitsOnly(s)
	if strings.HasPrefix(digits, "44") {
		digits = "0" + digits[2:]
	}
	return len(digits) >= 10 && len(digits) <= 11 && digits[0] == '0'
}

// Confidence constants (BUILD-03 Phase C).
const (
	// ConfidenceDeterministic is the baseline for regex matches that
	// survived any checksum/validate step. Callers may boost above via
	// context words; ApplySpans clamps to 1.0.
	ConfidenceDeterministic float32 = 1.0
	// ConfidenceManualDefault is the score for user-entered entities
	// (high trust, but not "checksum-verified").
	ConfidenceManualDefault float32 = 0.95
	// ConfidenceLLMDefault is the fallback score for LLM proposals that
	// did not carry an explicit confidence field.
	ConfidenceLLMDefault float32 = 0.8
)

// FilterByMinConfidence drops every span scoring below one GLOBAL minimum
// (BUILD-04 CR9), the shape the Configure screen's confidence control
// needs. It is the flat sibling of FilterByConfidence, which takes a
// per-category map.
//
// min <= 0 is a no-op, and that is the documented default: the setting
// must never quietly remove detections a user did not ask it to remove.
// A span with Confidence == 0 counts as 1.0, same back-compat rule as
// everywhere else in this file.
//
// Examples with the current scale: min 0.9 drops values the local AI
// proposed on its own (ConfidenceLLMDefault, 0.8) and keeps both the
// values the user listed (0.95) and the pattern matches (1.0); min 0.99
// keeps only the pattern matches.
func FilterByMinConfidence(spans []Span, min float32) []Span {
	if min <= 0 {
		return spans
	}
	out := make([]Span, 0, len(spans))
	for _, s := range spans {
		if effectiveConfidence(s) < min {
			continue
		}
		out = append(out, s)
	}
	return out
}

// effectiveConfidence is the score used by comparators: the span's stored
// Confidence, with 0 (never set) promoted to 1.0 so pre-BUILD-03 spans
// order and filter the same way they did before the field existed.
func effectiveConfidence(s Span) float32 {
	if s.Confidence == 0 {
		return 1.0
	}
	return s.Confidence
}

// ResolveOverlaps keeps a non-overlapping subset of spans. Priority order
// (BUILD-03 Phase F, extending the BUILD.md Phase 2 rule):
//  1. Higher confidence wins — a checksum-verified card beats a raw pattern
//     hit at the same offset. Zero-valued Confidence (pre-Phase-C spans)
//     is treated as 1.0 so the ordering degrades to the v1 behaviour when
//     no confidence data is present.
//  2. Longer match wins — the classic "email inside a URL" case: the URL
//     is longer, so the URL wins.
//  3. Earlier start, then category name — tie-break for fully deterministic
//     output the tests can pin against.
//
// The result is sorted by Start.
func ResolveOverlaps(spans []Span) []Span {
	ordered := make([]Span, len(spans))
	copy(ordered, spans)
	sort.Slice(ordered, func(i, j int) bool {
		ci, cj := effectiveConfidence(ordered[i]), effectiveConfidence(ordered[j])
		if ci != cj {
			return ci > cj // higher confidence first
		}
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

// validLuhn implements the Luhn (ISO/IEC 7812-1) checksum used by every
// major credit-card network: double every second digit from the right,
// sum the digits of each doubled value, add the unaltered digits; a valid
// number's total is divisible by 10. Non-digits (spaces, hyphens) are
// ignored so the check accepts grouped and compact forms alike.
//
// Overall length must be 13–19 digits (the ISO range covering Visa 13/16,
// Mastercard 16, Amex 15, Discover 16, UnionPay 16–19).
func validLuhn(s string) bool {
	digits := make([]int, 0, len(s))
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits = append(digits, int(c-'0'))
		} else if c == ' ' || c == '-' {
			continue
		} else {
			return false
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum := 0
	for i, d := range digits {
		// From the right: the last digit is unaltered; every second one
		// walking backwards is doubled.
		if (len(digits)-1-i)%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}

// validNHS implements the UK NHS number mod-11 checksum. Digits 1–9 are
// multiplied by weights 10..2 respectively, the sum is taken mod 11, then
// subtracted from 11. Check digit results:
//   - 11 → the check digit is 0
//   - 10 → the number is invalid (never issued)
//   - N  → must equal digit 10
func validNHS(s string) bool {
	digits := make([]int, 0, 10)
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits = append(digits, int(c-'0'))
		} else if c == ' ' || c == '-' {
			continue
		} else {
			return false
		}
	}
	if len(digits) != 10 {
		return false
	}
	sum := 0
	for i := 0; i < 9; i++ {
		sum += digits[i] * (10 - i)
	}
	rem := 11 - (sum % 11)
	check := digits[9]
	if rem == 11 {
		return check == 0
	}
	if rem == 10 {
		return false // NHS never issues these
	}
	return rem == check
}

// validIPv4 verifies each of the four dotted octets is 0–255.
func validIPv4(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if len(p) == 0 || len(p) > 3 {
			return false
		}
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
			n = n*10 + int(c-'0')
		}
		if n > 255 {
			return false
		}
	}
	return true
}

// validNIF verifies the check letter of a Spanish NIF: letter =
// "TRWAGMYFPDXBNJZSQVHLCKE"[digits mod 23]. Case-insensitive.
func validNIF(s string) bool {
	if len(s) != 9 {
		return false
	}
	n := 0
	for i := 0; i < 8; i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + int(c-'0')
	}
	const letters = "TRWAGMYFPDXBNJZSQVHLCKE"
	expected := letters[n%23]
	got := s[8]
	if got >= 'a' && got <= 'z' {
		got -= 'a' - 'A'
	}
	return got == expected
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
