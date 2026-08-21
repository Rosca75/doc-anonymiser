// engine/pii.go — Pass 1 of the pipeline: deterministic regex detection of
// hard PII (CLAUDE.md §5), mirroring the notebook's deterministic pre-pass.
//
// Design:
//   - Detection returns SPANS (start, end, category, original), it never
//     mutates text. Replacement is a separate step (ApplySpans) applying
//     spans longest-first, non-overlapping — this span model is reused by
//     every later pass.
//   - Every regex is compiled once at package init (performance budget:
//     per-call compilation is the classic budget killer) and documented
//     with examples of what it matches and deliberately does NOT match.
//   - A checksum is one of two things, and the field name says which
//     (piiPattern.validate vs piiPattern.checksum). Where the checksum IS the
//     recognizer (Luhn over a bare digit run) a failure VETOES the span; where
//     the pattern already stands on its own shape (an IBAN's country code,
//     check digits and grouped BBAN) a failure only LOWERS the confidence, so
//     a mistyped or synthetic bank identifier is still anonymised.
package engine

import (
	"regexp"
	"sort"
	"strings"
)

// Span is one detected occurrence inside a document's markdown working
// form. Start/End are byte offsets (End exclusive), Original is the exact
// matched text — kept so the registry can map it to a stable placeholder.
type Span struct {
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Category string `json:"category"`
	Original string `json:"original"`
	// MainText is the value's mainText name when the span came from a
	// variant match ("M. Duval" → mainText "Marie Duval"), so every
	// variant shares one placeholder. Empty for PII spans (the matched
	// text IS the mainText value).
	MainText string `json:"mainText,omitempty"`
	// Confidence in [0.0, 1.0]. Deterministic regex hits
	// default to 1.0; a local model finding to ConfidenceLLMDefault; a declared
	// Value to ConfidenceManualDefault. Context-word boosting may nudge a
	// value up (capped at 1.0). Zero means "not scored" and is read as 1.0 by
	// every comparator, so a producer that states nothing is trusted rather than
	// ranked last.
	Confidence float32 `json:"confidence,omitempty"`
	// MatchClass is the ROUTE that produced this span (matchClass.go). It decides
	// precedence when two routes claim the same characters, which Confidence
	// cannot do: confidence answers "how sure is this", and a producer scoring
	// lower than another is not the same fact as one whose claim should lose.
	// Empty ranks with MatchClassUserDefined.
	MatchClass string `json:"matchClass,omitempty"`
}

// MainTextOrOriginal returns the registry key for this span: the
// mainText value name when set, the matched text otherwise.
func (s Span) MainTextOrOriginal() string {
	if s.MainText != "" {
		return s.MainText
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
	// extended recognizers inspired by Presidio's
	// deterministic layer. All are hard PII (fire at every level).
	CatCreditCard  = "credit_card"  // Visa/Mastercard/Amex, Luhn-validated
	CatNHS         = "uk_nhs"       // UK National Health Service number, mod-11 validated
	CatIPAddress   = "ip_address"   // IPv4 + IPv6
	CatMACAddress  = "mac_address"  // 48-bit MAC (colon/hyphen separated)
	CatCrypto      = "crypto"       // Bitcoin (P2PKH/P2SH/Bech32)
	CatDatabaseURI = "database_uri" // postgres://, mysql://, mongodb://, redis:// with creds
	CatDESteuerID  = "de_steuer_id" // Germany national tax ID (11 digits)
	CatESNIF       = "es_nif"       // Spain NIF (8 digits + letter, letter validated)
	// CatBIC is a bank identifier code (ISO 9362). It travels beside the IBAN it
	// belongs to, so a document that names one names the other's institution.
	CatBIC = "bic"
	// CatPostalCode is a postal code. Country-scoped, because a postal code is a
	// national shape and a four-digit run is otherwise an ordinary number.
	CatPostalCode = "postal_code"
	// CatAddress is a street address line ("1, Avenue de l'Innovation").
	CatAddress = "address"
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
	// validate, when set, gets the matched text and may VETO the span. It is
	// for SHAPE gates and for the checksums that ARE the recognizer: strip the
	// Luhn check off the credit-card rule and every 16-digit run in the document
	// becomes a card, so there the checksum is the only thing separating the
	// pattern from ordinary text and it has to veto.
	validate func(string) bool
	// checksum, when set, is a CORROBORATING check over a pattern that already
	// stands on its own shape. A failure LOWERS the span's confidence to
	// ConfidenceChecksumFailed and never vetoes it.
	//
	// A failed checksum is the wrong reason to leave a bank identifier in a
	// document. "The checksum failed" is not evidence that the string is not an
	// account number, only that it might be a bad one, and a mistyped,
	// partly-redacted or synthetic identifier is exactly what a template or test
	// document contains. Failing closed leaves the country code and check digits
	// in clear text; producing the span at a reduced score leaves the user the
	// choice, through the "Only replace when the checksum matches" switch
	// (RejectFailedChecksums).
	checksum func(string) bool
	// reject, when set, gets the WHOLE text and the match bounds and may veto
	// the span on its SURROUNDINGS rather than on its own characters. RE2 has no
	// lookarounds, so a rule about what sits immediately before a match cannot
	// live in the pattern.
	reject func(text string, start, end int) bool
}

// Which categories a preset switches on lives in ONE place, the preset table
// (presets.go). The patterns below are unconditional; DetectPIISelected gates
// them by the selection.

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
		//
		// Two alternatives, because a scheme is not the only unambiguous marker.
		// A bare "word.word" is left alone deliberately (too many false hits on
		// ordinary prose), but a "www." label is a host name and nothing else,
		// and a document's own website is one of the strongest identifiers it
		// carries. The www. form needs at least two more dot-separated labels
		// after it, so "www." on its own or "www.txt" never matches.
		// A path may not END on sentence punctuation, so "see www.nstar.lu/privacy."
		// yields the URL and leaves the full stop in the sentence.
		// Matches:      https://example.com/a?b=c, http://user:pw@host.tld/x,
		//               www.nstar.lu, www.nstar.lu/privacy,
		//               www.statistiques.public.lu
		// Does not match: "example.com" (no scheme and no www. label),
		//               "ftp://host" (not http/https).
		//
		// Longest-match-first in ResolveOverlaps is what keeps
		// "www.nstar.lu/privacy" one span rather than letting "www.nstar.lu"
		// fire inside it.
		category: CatURL,
		re: regexp.MustCompile(`https?://[^\s<>"')\]]+` +
			`|\bwww\.[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)+(?:/(?:[^\s<>"')\],]*[^\s<>"')\],.;:!?])?)?`),
	},
	{
		// IBANs. The country code, the two check digits and the grouped BBAN
		// are already a specific enough shape to recognise on their own, so the
		// mod-97 check is CORROBORATION and not the recognizer: it scores the
		// span rather than vetoing it.
		// Matches:      LU28 0019 4006 4475 0000, DE89370400440532013000, and
		//               LU88 0055 6600 4321 6501 at ConfidenceChecksumFailed
		//               (mod-97 remainder 74: a synthetic test IBAN, which is
		//               what a template document contains and precisely what
		//               must still be anonymised).
		// Does not match: "LUXEMBOURG" (needs 2 check digits after the country
		//               code), a bare 16-digit run (needs the letter prefix).
		category: CatIBAN,
		re:       regexp.MustCompile(`\b[A-Z]{2}[0-9]{2}(?:[ ]?[A-Z0-9]{4}){2,7}(?:[ ]?[A-Z0-9]{1,4})?\b`),
		checksum: validIBAN,
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
		// Luxembourg phone numbers, in their INTERNATIONAL form only.
		//
		// A Luxembourg subscriber number carries no trunk prefix, so written
		// nationally it is a bare run of eight digits in pairs, which is the same
		// shape as an ISO date ("2026-01-15"), as the interior of an IBAN
		// ("... 4475 0001 0000") and as an ordinary reference number. This
		// repository's own tests measure both collisions, so the national form is
		// deliberately not matched: a rule that turns a date into a phone number
		// costs more than the numbers it finds. A national rule needs a phone cue
		// beside the digits, and that is a change with its own evidence behind it.
		// Matches:      +352 621 000 111, +352 29 19 19 5, +352 29 19 19 2100,
		//               00352 26 12 34 56
		// Does not match: 1893120105732 (no international prefix), "2026" (too
		//               short), a bare "26 12 34 56".
		category:  CatPhone,
		re:        regexp.MustCompile(`(?:\+352|00352)(?:[ .\-/]?[0-9]{1,4}){2,5}`),
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
		// Matches:      €1,500.00, EUR 12 500, 1.250,50 €, $99, €1,5k, 2.3M USD.
		//   The space next to the currency symbol and the thousands separator
		//   may be an ASCII space OR a Unicode non-breaking space: U+00A0
		//   (NO-BREAK SPACE), U+202F (NARROW NO-BREAK SPACE) or U+2009 (THIN
		//   SPACE). European/French documents routinely use these both as the
		//   thousands separator and before the currency symbol, and Go's \s
		//   plus a literal ASCII space match ASCII space only, so they are
		//   listed explicitly.
		//   An optional magnitude suffix k or M (case-insensitive) is accepted
		//   immediately after the number (e.g. 1,5k or 2.3M).
		// Does not match: bare numbers without a currency marker (they are
		//   too common in ordinary text to replace safely). The k/M suffix is
		//   only honoured WHEN a currency marker is present, so a bare "2M"
		//   still does not match.
		category: CatAmount,
		// \x{00a0}=U+00A0, \x{202f}=U+202F, \x{2009}=U+2009 — the three
		// non-breaking/thin spaces plus a plain ASCII space.
		re: regexp.MustCompile(`(?:€|EUR|USD|GBP|CHF|\$|£)[\s\x{00a0}\x{202f}\x{2009}]?[0-9]{1,3}(?:[.,'\x{00a0}\x{202f}\x{2009} ][0-9]{3})*(?:[.,][0-9]{1,2})?[kKmM]?|\b[0-9]{1,3}(?:[.,'\x{00a0}\x{202f}\x{2009} ][0-9]{3})*(?:[.,][0-9]{1,2})?[kKmM]?[\s\x{00a0}\x{202f}\x{2009}]?(?:€|EUR|USD|GBP|CHF|\$|£)`),
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

	// --- extended recognizers -------------------------

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
		// A 16-digit BBAN passes Luhn about one time in ten, so an IBAN's
		// interior is a recurring credit-card false positive, and the harm is
		// worse than a miss: the mapping CSV then asserts the document held a
		// card that does not exist. The guard is independent of the checksum
		// policy above, so it stands whether or not the IBAN span was produced.
		reject: precededByIBANPrefix,
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
		// Bank Identifier Codes (ISO 9362, the "SWIFT code"): four institution
		// letters, a two-letter ISO country code, a two-character location, and
		// an optional three-character branch.
		//
		// TWO gates, and both are needed. The country-code check on positions 5
		// and 6 is not enough on its own: measured over a real contract it
		// accepted OBLIGATIONS, TERMINATION, DEFINITIONS, COOPERATION and
		// PROPERTY, because an eight or eleven letter English word carries a real
		// ISO country code in those positions surprisingly often ("obligATions",
		// "propERty"). So a BIC is additionally required to sit beside its own
		// CUE, which is how a document always writes one: a BIC has no check
		// digits and no other structure, so without a label it is indistinguishable
		// from an ALL-CAPS heading word, and a heading replaced as a bank code is
		// worse than a BIC missed.
		// Matches:      "BIC/SWIFT: BABAAXIL", "SWIFT BGLLLULL", "BIC DEUTDEFF500"
		// Does not match: "TERMINATION" in a heading (no cue), "BABAAXIL" with no
		//               cue in front of it, a lower-case word.
		category: CatBIC,
		re:       regexp.MustCompile(`\b[A-Z]{6}[A-Z0-9]{2}(?:[A-Z0-9]{3})?\b`),
		validate: validBIC,
		reject:   bicCueMissing,
	},
	{
		// Luxembourg postal codes: "L-" plus exactly four digits. A fixed national
		// shape, which is why the category is country-scoped: a bare four-digit
		// run is an ordinary number everywhere else.
		// Matches:      L-1855, L-2550
		// Does not match: "L-185" (three digits), "L-18555" (five, the trailing
		//               digit boundary rejects it), "1855" (no prefix).
		category:  CatPostalCode,
		re:        regexp.MustCompile(`\bL-[0-9]{4}\b`),
		countries: []string{CountryLU},
	},
	{
		// Street address lines in the continental form: a house number, a comma,
		// a street type, and the street's name.
		//
		// The STREET TYPE is the anchor. Without it the pattern is "a number
		// followed by capitalised words", which matches a clause number followed
		// by a heading. The type list is the same street-type vocabulary the
		// discovery pass uses to recognise an address context, kept in one place
		// (addressStreetTypes) so the two cannot disagree about what a street is.
		// Matches:      1, Avenue de l'Innovation
		//               12, rue des Tilleuls
		//               3, Boulevard Royal
		// Does not match: "12, Tilleuls" (no street type), "Avenue de
		//               l'Innovation" on its own (no house number: the number is
		//               what makes the line an address rather than a place).
		category:  CatAddress,
		re:        addressLineRe,
		countries: []string{CountryLU, CountryFR},
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
// enabled in the selection AND whose
// country scope covers the requested country.
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
			if p.reject != nil && p.reject(text, start, end) {
				continue
			}
			// A corroborating checksum scores the span; only a `validate` gate
			// above can remove one.
			confidence := ConfidenceDeterministic
			if p.checksum != nil && !p.checksum(original) {
				confidence = ConfidenceChecksumFailed
			}
			spans = append(spans, Span{
				Start:      start,
				End:        end,
				Category:   p.category,
				Original:   original,
				Confidence: confidence,
				MatchClass: MatchClassBuiltInPattern,
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

// luPhoneMinDigits / luPhoneMaxDigits bound a Luxembourg number by the
// national significant digits its numbering plan allocates (4 to 11).
//
// Luxembourg numbers are NOT fixed length, which an exactly-nine rule got wrong
// for the owner's primary market: a six-digit base can carry a PBX extension, so
// "+352 29 19 19 5" (7 national digits) and "+352 29 19 19 2100" (10) are both
// real numbers and both were left in the document. A length check still has to
// exist, because the regex shape alone would accept a bare year or an article
// number, so the bound moves rather than going away.
const (
	luPhoneMinDigits = 4
	luPhoneMaxDigits = 11
)

func validLUPhone(s string) bool {
	digits := digitsOnly(s)
	// The pattern only matches the international form, so the country code is
	// always there to remove; what is left is the national significant number.
	switch {
	case strings.HasPrefix(digits, "00352"):
		digits = digits[len("00352"):]
	case strings.HasPrefix(digits, "352"):
		digits = digits[len("352"):]
	}
	return len(digits) >= luPhoneMinDigits && len(digits) <= luPhoneMaxDigits
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

// Confidence constants.
const (
	// ConfidenceDeterministic is the baseline for regex matches that
	// survived any checksum/validate step. Callers may boost above via
	// context words; ApplySpans clamps to 1.0.
	ConfidenceDeterministic float32 = 1.0
	// ConfidenceManualDefault is the score for a Value the user declared
	// (high trust, but not "checksum-verified").
	ConfidenceManualDefault float32 = 0.95
	// ConfidenceSignalDerived is the score for a Suggestion signal-based
	// discovery produced. It sits BELOW a user declaration and ABOVE a model
	// finding: the evidence is a deterministic pattern match and the finding is a
	// literal occurrence of text derived from it, but the INFERENCE from evidence
	// to Value is still a guess the user has to confirm.
	ConfidenceSignalDerived float32 = 0.9
	// ConfidenceLLMDefault is the fallback score for a local model finding that
	// carried no explicit confidence.
	ConfidenceLLMDefault float32 = 0.8
	// ConfidenceChecksumFailed is the score for a built-in pattern match whose
	// CORROBORATING checksum did not verify (piiPattern.checksum): the shape is
	// right and the digits do not add up. It sits below every other producer,
	// because the span is the least certain thing pass 1 emits, and above zero,
	// because the span still exists and a failed checksum is not a reason to
	// leave a bank identifier in a document. Keeping it is the default;
	// RejectFailedChecksums below is the user asking for the opposite.
	ConfidenceChecksumFailed float32 = 0.7
)

// RejectFailedChecksums drops the pattern matches whose CORROBORATING checksum
// did not verify, and nothing else. It is what the "Only replace when the
// checksum matches" switch does, off by default.
//
// It is named after what it does rather than after a number, because a caller
// reading a threshold has to hold the whole score table in their head to know
// what it excludes. The score table is only this:
//
//	pattern match                  ConfidenceDeterministic  1.0
//	custom pattern                 ConfidenceDeterministic  1.0
//	Value the user declared        ConfidenceManualDefault  0.95
//	Value from a signal Suggestion ConfidenceSignalDerived  0.9
//	Value from a model Suggestion  ConfidenceLLMDefault     0.8
//	pattern match, checksum failed ConfidenceChecksumFailed 0.7
//
// The comparison is an EQUALITY against the one score pass 1's corroborating
// checks mint, not a floor under it. A floor is what this replaced, and above
// roughly 0.8 a floor reached the accepted Values in the rows above: it dropped
// what the user had already accepted, by the score of whatever originally found
// it, which answers "reject" on the user's behalf and does so invisibly. An
// equality cannot grow into that by someone moving a slider.
//
// It is applied to the PATTERN spans alone (pipeline.go detectText), so an
// accepted Value never passes through it at all.
func RejectFailedChecksums(spans []Span) []Span {
	out := make([]Span, 0, len(spans))
	for _, s := range spans {
		if s.Confidence == ConfidenceChecksumFailed {
			continue
		}
		out = append(out, s)
	}
	return out
}

// effectiveConfidence is the score used by comparators: the span's stored
// Confidence, with 0 (never set) promoted to 1.0 so pre- spans
// order and filter the same way they did before the field existed.
func effectiveConfidence(s Span) float32 {
	if s.Confidence == 0 {
		return 1.0
	}
	return s.Confidence
}

// ResolveOverlaps keeps a non-overlapping subset of spans, in the fixed
// priority order below. The result is sorted by Start.
//
//  1. Lower MatchClassRank wins: native, then declared, then auto, then the local model
//     (matchClass.go). MatchClass comes FIRST because it is the only comparator that
//     answers "which route should own this string", and it is the answer the
//     user is shown. The two comparators below cannot answer it: a regex
//     signal and a custom pattern both score 1.0, so confidence leaves them
//     tied, and length then decides by whichever match happens to be longer,
//     which depends on the text rather than on a rule.
//  2. Higher confidence wins. A checksum-verified card beats a raw pattern hit
//     at the same offset. Now a tie-break WITHIN one route. A zero Confidence
//     is read as 1.0, so a producer that states none is trusted rather than
//     ranked last.
//  3. Longer match wins. This is the "email inside a URL" case: both are
//     native, so this is still what decides it, and the URL is longer.
//  4. Earlier start, then category name. A tie-break that makes the output
//     fully deterministic, so the tests can pin it.
//
// The order agrees with the registry's precedence rule (pass 1 before pass 2
// before pass 3), so the span resolver and the registry can never disagree
// about which category owns a string.
func ResolveOverlaps(spans []Span) []Span {
	kept, _ := resolveOverlaps(spans, false)
	return kept
}

// ResolveOverlapsWithLosers is ResolveOverlaps that also returns what it threw
// away.
//
// The losers are how a run WARNS about an overlap: a declared value covered by
// a regex match, or a custom pattern covering a declared value. They come from
// here rather than from a parallel check over the declarations, because a
// parallel check can disagree with the pipeline, and then the warning describes
// something that did not happen.
//
// Call it only when the losers are actually wanted. Collecting them costs an
// allocation per discarded span, and a document full of name variants discards
// several per replacement: paying that on every call cost the deterministic
// pipeline a third of its time budget.
//
// @param spans every detection, from every pass, in any order
// @return the non-overlapping subset to apply, and the spans a stronger span
//
//	covered
func ResolveOverlapsWithLosers(spans []Span) (kept, dropped []Span) {
	return resolveOverlaps(spans, true)
}

// resolveOverlaps is the shared core. `collect` decides whether the discarded
// spans are gathered or simply skipped.
func resolveOverlaps(spans []Span, collect bool) (kept, dropped []Span) {
	ordered := make([]Span, len(spans))
	copy(ordered, spans)
	sort.Slice(ordered, func(i, j int) bool {
		ri, rj := MatchClassRank(ordered[i].MatchClass), MatchClassRank(ordered[j].MatchClass)
		if ri != rj {
			return ri < rj // the superseding route first
		}
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

	for _, s := range ordered {
		overlaps := false
		for _, k := range kept {
			if s.Start < k.End && k.Start < s.End {
				overlaps = true
				break
			}
		}
		if overlaps {
			if collect {
				dropped = append(dropped, s)
			}
			continue
		}
		kept = append(kept, s)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Start < kept[j].Start })
	return kept, dropped
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

// ibanPrefixRe matches the country-and-check-digits head of an IBAN sitting
// immediately before a candidate: two capitals, two digits, then optional
// whitespace. It exists because RE2 has no lookbehind, so the credit-card rule
// cannot express "not preceded by this" inside its own pattern.
//
// The `$` anchors it to the END of the text handed in, which is the stretch
// immediately before the match.
var ibanPrefixRe = regexp.MustCompile(`\b[A-Z]{2}[0-9]{2}[ ]?$`)

// precededByIBANPrefix reports whether the match at [start,end) is the interior
// of an IBAN: an "LU88 " style head sits directly in front of it.
//
// A 16-digit BBAN passes Luhn roughly one time in ten, so without this guard an
// IBAN's interior is a recurring credit-card false positive, and the mapping CSV
// then states the document contained a card that never existed while the IBAN's
// country code and check digits survive in clear text beside the placeholder.
//
// The end offset is part of the shared `reject` signature and unused here: this
// rule is entirely about what sits in FRONT of the candidate.
//
// @param text the whole document working form
// @param start the match's first byte
// @return true when the span must not be produced
func precededByIBANPrefix(text string, start, _ int) bool {
	// A short window is enough: the head is six bytes at most. Scanning back
	// further would cost a regex pass over the whole document per candidate.
	const window = 8
	from := start - window
	if from < 0 {
		from = 0
	}
	return ibanPrefixRe.MatchString(text[from:start])
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

// isoCountryCodes are the two-letter ISO 3166-1 alpha-2 codes, which is what a
// BIC carries in its fifth and sixth characters.
//
// It is a full list rather than a short one on purpose: this table is the ONLY
// thing separating a BIC from an ordinary eight-letter capitalised word, so a
// missing code is a missed bank identifier left in the document, and an extra
// one would be a word replaced as a bank code. A generated list is checkable; a
// hand-picked subset is not.
var isoCountryCodes = map[string]bool{
	"AD": true, "AE": true, "AF": true, "AG": true, "AI": true, "AL": true,
	"AM": true, "AO": true, "AQ": true, "AR": true, "AS": true, "AT": true,
	"AU": true, "AW": true, "AX": true, "AZ": true, "BA": true, "BB": true,
	"BD": true, "BE": true, "BF": true, "BG": true, "BH": true, "BI": true,
	"BJ": true, "BL": true, "BM": true, "BN": true, "BO": true, "BQ": true,
	"BR": true, "BS": true, "BT": true, "BV": true, "BW": true, "BY": true,
	"BZ": true, "CA": true, "CC": true, "CD": true, "CF": true, "CG": true,
	"CH": true, "CI": true, "CK": true, "CL": true, "CM": true, "CN": true,
	"CO": true, "CR": true, "CU": true, "CV": true, "CW": true, "CX": true,
	"CY": true, "CZ": true, "DE": true, "DJ": true, "DK": true, "DM": true,
	"DO": true, "DZ": true, "EC": true, "EE": true, "EG": true, "EH": true,
	"ER": true, "ES": true, "ET": true, "FI": true, "FJ": true, "FK": true,
	"FM": true, "FO": true, "FR": true, "GA": true, "GB": true, "GD": true,
	"GE": true, "GF": true, "GG": true, "GH": true, "GI": true, "GL": true,
	"GM": true, "GN": true, "GP": true, "GQ": true, "GR": true, "GS": true,
	"GT": true, "GU": true, "GW": true, "GY": true, "HK": true, "HM": true,
	"HN": true, "HR": true, "HT": true, "HU": true, "ID": true, "IE": true,
	"IL": true, "IM": true, "IN": true, "IO": true, "IQ": true, "IR": true,
	"IS": true, "IT": true, "JE": true, "JM": true, "JO": true, "JP": true,
	"KE": true, "KG": true, "KH": true, "KI": true, "KM": true, "KN": true,
	"KP": true, "KR": true, "KW": true, "KY": true, "KZ": true, "LA": true,
	"LB": true, "LC": true, "LI": true, "LK": true, "LR": true, "LS": true,
	"LT": true, "LU": true, "LV": true, "LY": true, "MA": true, "MC": true,
	"MD": true, "ME": true, "MF": true, "MG": true, "MH": true, "MK": true,
	"ML": true, "MM": true, "MN": true, "MO": true, "MP": true, "MQ": true,
	"MR": true, "MS": true, "MT": true, "MU": true, "MV": true, "MW": true,
	"MX": true, "MY": true, "MZ": true, "NA": true, "NC": true, "NE": true,
	"NF": true, "NG": true, "NI": true, "NL": true, "NO": true, "NP": true,
	"NR": true, "NU": true, "NZ": true, "OM": true, "PA": true, "PE": true,
	"PF": true, "PG": true, "PH": true, "PK": true, "PL": true, "PM": true,
	"PN": true, "PR": true, "PS": true, "PT": true, "PW": true, "PY": true,
	"QA": true, "RE": true, "RO": true, "RS": true, "RU": true, "RW": true,
	"SA": true, "SB": true, "SC": true, "SD": true, "SE": true, "SG": true,
	"SH": true, "SI": true, "SJ": true, "SK": true, "SL": true, "SM": true,
	"SN": true, "SO": true, "SR": true, "SS": true, "ST": true, "SV": true,
	"SX": true, "SY": true, "SZ": true, "TC": true, "TD": true, "TF": true,
	"TG": true, "TH": true, "TJ": true, "TK": true, "TL": true, "TM": true,
	"TN": true, "TO": true, "TR": true, "TT": true, "TV": true, "TW": true,
	"TZ": true, "UA": true, "UG": true, "UM": true, "US": true, "UY": true,
	"UZ": true, "VA": true, "VC": true, "VE": true, "VG": true, "VI": true,
	"VN": true, "VU": true, "WF": true, "WS": true, "YE": true, "YT": true,
	"ZA": true, "ZM": true, "ZW": true,
}

// bicCueRe matches the label a document puts in front of a BIC, at the END of
// the stretch immediately before a candidate. The separators after the cue are
// whatever the drafter typed (":", "/", "-", "=", or nothing).
//
// Matches (as the tail of the preceding text): "BIC/SWIFT: ", "SWIFT ",
// "bic code - ", "Code BIC : "
var bicCueRe = regexp.MustCompile(`(?i)\b(?:bic|swift)(?:[ ]?code)?[ ]*[:/=,.\-]*[ ]*$`)

// bicCueMissing reports whether the candidate at [start,end) has NO BIC cue in
// front of it, in which case the span must not be produced.
//
// The window is short on purpose. A generous one would let the cue in front of a
// real BIC vouch for the next ALL-CAPS word after it too, which is the same
// false positive the cue exists to remove.
//
// The end offset is part of the shared `reject` signature and unused here, for
// the same reason: the cue is in front of the candidate, never after it.
//
// @param text the whole document working form
// @param start the candidate's first byte
// @return true when the span must be rejected
func bicCueMissing(text string, start, _ int) bool {
	const window = 32
	from := start - window
	if from < 0 {
		from = 0
	}
	return !bicCueRe.MatchString(text[from:start])
}

// validBIC verifies a candidate's length and that its fifth and sixth
// characters are a real ISO country code. It is one of the BIC rule's two gates;
// bicCueMissing above is the other, and the comment on the pattern says why one
// is not enough.
//
// Both veto rather than score, unlike the IBAN checksum, because here the checks
// ARE the recognizer: without them the pattern matches capitalised words.
func validBIC(s string) bool {
	if len(s) != 8 && len(s) != 11 {
		return false
	}
	return isoCountryCodes[s[4:6]]
}

// addressStreetTypes are the street-type words a postal address line is built
// on, in the continental form the owner's market writes. They are the ANCHOR of
// the address pattern: without one, the pattern is "a number followed by
// capitalised words", which matches a clause number followed by a heading.
//
// Listed with their capitalised spellings as well as their lower-case ones,
// because both occur ("12, rue des Tilleuls" and "1, Avenue de l'Innovation")
// and RE2 has no case-insensitive group that would not also match SHOUTING.
var addressStreetTypes = []string{
	"rue", "Rue", "avenue", "Avenue", "boulevard", "Boulevard",
	"place", "Place", "impasse", "Impasse", "chemin", "Chemin",
	"route", "Route", "quai", "Quai", "allée", "Allée", "allee", "Allee",
	"square", "Square", "street", "Street", "road", "Road", "lane", "Lane",
	"drive", "Drive", "esplanade", "Esplanade", "cours", "Cours",
	"montée", "Montée", "montee", "Montee", "voie", "Voie",
	"passage", "Passage", "rond-point", "Rond-Point", "cité", "Cité",
	"cite", "Cite", "val", "Val", "op", "Op", "am", "Am",
}

// addressLineRe matches a house number, a comma, a street type and the street's
// own name: the shape of a continental address line.
//
// The name is bounded at one to four words so the match cannot run on into the
// rest of the sentence, and it stops at a comma, which is where an address line
// always ends in a document ("1, Avenue de l'Innovation, L-1855 Luxembourg"
// yields the address, the postal code and the country as three spans).
var addressLineRe = regexp.MustCompile(
	`\b[0-9]{1,4}(?:[ ]?[a-zA-Z])?,[ ]?(?:` + strings.Join(addressStreetTypes, "|") + `)` +
		`(?:[ ](?:d[eu]s?|l[ae]|l'|d')?[ ]?[\pL][\pL'’\-]*){1,4}`)
