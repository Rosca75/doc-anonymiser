// engine/pii_test.go — table-driven tests for pass 1:
// per-category positives and negatives, IBAN checksum, level awareness,
// overlap resolution, registry stability, and the CSV budget measurement.
//
// Budget measurements, recorded 2026-07-23 on
// the CI-class Linux container:
//   - CSV import → markdown render, 10 000 rows × 20 cols: ~36 ms
//     (budget ≤ 2 s) — see TestCSVImportBudget.
//   - The 50-document pipeline budget is measured in
//     (pipeline_test.go), where the full pass chain exists.
package engine

import (
	"testing"
)

// TestDetectPIICategories runs one positive and one negative case per
// category.
func TestDetectPIICategories(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		preset   string
		country  string
		category string
		want     string // expected matched text ("" = expect NO match of category)
	}{
		// --- email -------------------------------------------------------------
		{"email positive", "contact marie.duval@example.com today", PresetSoft, CountryLU, CatEmail, "marie.duval@example.com"},
		{"email negative: no TLD", "user@localhost is not external", PresetSoft, CountryLU, CatEmail, ""},
		// --- url ---------------------------------------------------------------
		{"url positive", "see https://intra.example.com/report?id=1 now", PresetSoft, CountryLU, CatURL, "https://intra.example.com/report?id=1"},
		{"url with credentials", "wget https://svc:tok123@host.example.com/f", PresetSoft, CountryLU, CatURL, "https://svc:tok123@host.example.com/f"},
		{"url negative: bare domain", "visit example.com sometime", PresetSoft, CountryLU, CatURL, ""},
		// --- iban --------------------------------------------------------------
		{"iban positive spaced", "account LU28 0019 4006 4475 0000 please", PresetSoft, CountryLU, CatIBAN, "LU28 0019 4006 4475 0000"},
		{"iban positive compact", "send to DE89370400440532013000 now", PresetSoft, CountryLU, CatIBAN, "DE89370400440532013000"},
		// A failed mod-97 check no longer vetoes the span; it lowers the
		// confidence (TestChecksumFailureScoresRatherThanVetoes below). Leaving a
		// mistyped or synthetic bank identifier in the document is the harm the
		// old veto caused, so the match is expected here.
		{"iban positive despite failed checksum", "account LU28 0019 4006 4475 0001 please", PresetSoft, CountryLU, CatIBAN, "LU28 0019 4006 4475 0001"},
		{"iban negative: country word", "LUXEMBOURG is not an IBAN", PresetSoft, CountryLU, CatIBAN, ""},
		// --- vat ---------------------------------------------------------------
		{"vat positive LU", "VAT number LU12345678 on file", PresetSoft, CountryLU, CatVAT, "LU12345678"},
		{"vat positive FR", "TVA FR40303265045 enregistr?e", PresetSoft, CountryFR, CatVAT, "FR40303265045"},
		{"vat negative: too short", "code LU1234 is not a VAT number", PresetSoft, CountryLU, CatVAT, ""},
		// Country scoping, pattern level: VAT applies to
		// every country, but each VAT pattern is ONE national format, so the
		// French format must stay silent under a Luxembourg selection.
		{"vat negative: FR format under LU selection", "TVA FR40303265045 enregistree", PresetSoft, CountryLU, CatVAT, ""},
		// --- matricule ---------------------------------------------------------
		{"matricule positive", "matricule 1893120105732 registered", PresetSoft, CountryLU, CatMatricule, "1893120105732"},
		// Country scoping, category level: the matricule is a Luxembourg
		// national ID, so the whole category is off under another country.
		{"matricule negative: LU number under FR selection", "matricule 1893120105732 registered", PresetSoft, CountryFR, CatMatricule, ""},
		{"matricule negative: 12 digits", "ref 189312010573 stays", PresetSoft, CountryLU, CatMatricule, ""},
		{"matricule negative: 14 digits", "ref 18931201057321 stays", PresetSoft, CountryLU, CatMatricule, ""},
		// --- phone -------------------------------------------------------------
		{"phone positive international", "call +352 621 000 111 today", PresetSoft, CountryLU, CatPhone, "+352 621 000 111"},
		{"phone positive FR mobile", "t?l. 06 12 34 56 78 merci", PresetSoft, CountryFR, CatPhone, "06 12 34 56 78"},
		{"phone negative: plain year", "the year 2026 report", PresetSoft, CountryLU, CatPhone, ""},
		// --- amount (advanced only) --------------------------------------------
		{"amount positive prefix currency", "fee of EUR 1,500.00 agreed", PresetThorough, CountryLU, CatAmount, "EUR 1,500.00"},
		{"amount positive suffix", "budget 12 500 EUR total", PresetThorough, CountryLU, CatAmount, "12 500 EUR"},
		{"amount negative: bare number", "about 1500 items", PresetThorough, CountryLU, CatAmount, ""},
		// Non-breaking spaces (U+00A0) and thin/narrow spaces appear in
		// European/French documents both before the currency symbol and as
		// the thousands separator; the regex must treat them like a space.
		{"amount positive nbsp before euro", "total 1.500,00\u00a0\u20ac due", PresetThorough, CountryLU, CatAmount, "1.500,00\u00a0\u20ac"},
		{"amount positive ascii space before euro decimal", "total 1.250,50 \u20ac due", PresetThorough, CountryLU, CatAmount, "1.250,50 \u20ac"},
		{"amount positive ascii space before euro", "total 25.150 \u20ac due", PresetThorough, CountryLU, CatAmount, "25.150 \u20ac"},
		// Magnitude suffix k/M is honoured only next to a currency marker.
		{"amount positive k suffix with euro", "cost 1,5k \u20ac roughly", PresetThorough, CountryLU, CatAmount, "1,5k \u20ac"},
		{"amount negative: bare magnitude", "revenue grew 2M last year", PresetThorough, CountryLU, CatAmount, ""},
		// --- date (advanced only) ----------------------------------------------
		{"date positive iso", "due 2026-07-23 latest", PresetThorough, CountryLU, CatDate, "2026-07-23"},
		{"date positive eu numeric", "signed 23/07/2026 in Luxembourg", PresetThorough, CountryLU, CatDate, "23/07/2026"},
		{"date positive written en", "meeting on 23 July 2026 confirmed", PresetThorough, CountryLU, CatDate, "23 July 2026"},
		{"date positive written fr", "r?union le 23 juillet 2026 confirm?e", PresetThorough, CountryLU, CatDate, "23 juillet 2026"},
		{"date negative: bare year", "since 2026 things changed", PresetThorough, CountryLU, CatDate, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans := DetectPIISelected(tt.text, DepthSelection(tt.preset, tt.country), tt.country)
			var got []string
			for _, s := range spans {
				if s.Category == tt.category {
					got = append(got, s.Original)
				}
			}
			if tt.want == "" {
				if len(got) > 0 {
					t.Errorf("expected no %s match, got %v", tt.category, got)
				}
				return
			}
			if len(got) == 0 {
				t.Fatalf("expected %s match %q, got none (all spans: %+v)", tt.category, tt.want, spans)
			}
			if got[0] != tt.want {
				t.Errorf("matched %q, want %q", got[0], tt.want)
			}
		})
	}
}

// TestDepthAwareness pins the depth matrix: amounts and dates fire at Thorough
// only, and hard PII fires at every depth.
func TestDepthAwareness(t *testing.T) {
	text := "pay €500 to LU28 0019 4006 4475 0000 on 2026-07-23"
	for _, id := range []string{PresetSoft, PresetStandard} {
		spans := DetectPIISelected(text, DepthSelection(id, CountryLU), CountryLU)
		for _, s := range spans {
			if s.Category == CatAmount || s.Category == CatDate {
				t.Errorf("%s fired at the %s preset, want Thorough only", s.Category, id)
			}
		}
		if !hasCategory(spans, CatIBAN) {
			t.Errorf("IBAN must fire at the %s preset", id)
		}
	}
	thorough := DetectPIISelected(text, DepthSelection(PresetThorough, CountryLU), CountryLU)
	if !hasCategory(thorough, CatAmount) || !hasCategory(thorough, CatDate) {
		t.Errorf("the Thorough preset must add amount and date, got %+v", thorough)
	}
}

func hasCategory(spans []Span, cat string) bool {
	for _, s := range spans {
		if s.Category == cat {
			return true
		}
	}
	return false
}

// TestOverlapResolution: an email inside a URL must resolve to the URL
// (longest match wins), deterministically.
func TestOverlapResolution(t *testing.T) {
	text := "profile at https://example.com/u/marie.duval@example.com end"
	spans := ResolveOverlaps(DetectPIISelected(text, DepthSelection(PresetStandard, CountryLU), CountryLU))
	if len(spans) != 1 {
		t.Fatalf("want exactly 1 span after resolution, got %+v", spans)
	}
	if spans[0].Category != CatURL {
		t.Errorf("URL must win over embedded email, got %s", spans[0].Category)
	}
	if spans[0].Original != "https://example.com/u/marie.duval@example.com" {
		t.Errorf("unexpected span text %q", spans[0].Original)
	}
}

// TestOverlapOriginBeatsEverythingElse: with equal confidence and equal
// length, the ROUTE decides, in all six pairings of the four routes.
//
// This is the case that had no defined answer before matchClass existed. A regex
// signal and a custom pattern both score ConfidenceDeterministic, so
// confidence left them tied; length then decided, and with equal lengths the
// category name did, alphabetically. Which route won therefore depended on the
// text rather than on a rule anybody chose, which is how it came to be reported
// as random.
func TestOverlapOriginBeatsEverythingElse(t *testing.T) {
	// Every pairing of the four routes, in precedence order, so a reordering
	// of AllMatchClasses cannot silently pass.
	for i := 0; i < len(AllMatchClasses); i++ {
		for j := i + 1; j < len(AllMatchClasses); j++ {
			stronger, weaker := AllMatchClasses[i], AllMatchClasses[j]
			// Identical offsets, identical confidence, identical length: the
			// matchClass is the ONLY thing separating them.
			spans := []Span{
				{
					Start: 0, End: 5, Category: "b_weaker", Original: "Delta",
					Confidence: ConfidenceDeterministic, MatchClass: weaker,
				},
				{
					Start: 0, End: 5, Category: "a_stronger", Original: "Delta",
					Confidence: ConfidenceDeterministic, MatchClass: stronger,
				},
			}
			kept := ResolveOverlaps(spans)
			if len(kept) != 1 {
				t.Fatalf("%s vs %s: want one span, got %+v", stronger, weaker, kept)
			}
			if kept[0].MatchClass != stronger {
				t.Errorf("%s must supersede %s, kept %q", stronger, weaker, kept[0].MatchClass)
			}
		}
	}
}

// TestOverlapOriginBeatsLength: a native span that is SHORTER than a declared
// one still wins. Length is the third comparator, not the first, so it can no
// longer decide which route owns a string.
func TestOverlapOriginBeatsLength(t *testing.T) {
	spans := []Span{
		// The declared value covers more characters...
		{
			Start: 0, End: 16, Category: CatCustomPatterns, Original: "Delta Industries",
			Confidence: ConfidenceDeterministic, MatchClass: MatchClassUserDefined,
		},
		// ...and the native signal still wins, because it is native.
		{
			Start: 0, End: 5, Category: CatEmail, Original: "Delta",
			Confidence: ConfidenceDeterministic, MatchClass: MatchClassBuiltInPattern,
		},
	}
	kept := ResolveOverlaps(spans)
	if len(kept) != 1 {
		t.Fatalf("want one span, got %+v", kept)
	}
	if kept[0].MatchClass != MatchClassBuiltInPattern {
		t.Errorf("matchClass must outrank length, kept %+v", kept[0])
	}
}

// TestOverlapLengthStillDecidesWithinOneRoute: with the route equal, length is
// still what separates two spans. This is the "email inside a URL" rule, and
// both of those spans are native, so it is unaffected by matchClass.
func TestOverlapLengthStillDecidesWithinOneRoute(t *testing.T) {
	spans := []Span{
		{
			Start: 11, End: 35, Category: CatEmail, Original: "marie.duval@example.com",
			Confidence: ConfidenceDeterministic, MatchClass: MatchClassBuiltInPattern,
		},
		{
			Start: 0, End: 40, Category: CatURL, Original: "https://example.com/u/marie.duval@ex.com",
			Confidence: ConfidenceDeterministic, MatchClass: MatchClassBuiltInPattern,
		},
	}
	kept := ResolveOverlaps(spans)
	if len(kept) != 1 || kept[0].Category != CatURL {
		t.Errorf("the longer native span must win within one route, got %+v", kept)
	}
}

// TestApplySpans proves back-to-front replacement keeps offsets valid and
// the registry produces stable, numbered placeholders.
func TestApplySpans(t *testing.T) {
	text := "mail marie.duval@example.com or peter.stone@example.org, mail marie.duval@example.com again"
	reg := NewRegistry()
	spans := ResolveOverlaps(DetectPIISelected(text, DepthSelection(PresetStandard, CountryLU), CountryLU))
	out := ApplySpans(text, spans, func(s Span) string {
		return reg.Assign(s.Category, s.MainTextOrOriginal())
	})
	want := "mail [EMAIL_1] or [EMAIL_2], mail [EMAIL_1] again"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// TestRegistryStability: the same value seen in two documents (and in a
// different case) maps to the same placeholder; counts accumulate.
func TestRegistryStability(t *testing.T) {
	reg := NewRegistry()
	p1 := reg.Assign(CatEmail, "Marie.Duval@example.com")
	p2 := reg.Assign(CatEmail, "marie.duval@example.com") // doc B, different case
	if p1 != p2 || p1 != "[EMAIL_1]" {
		t.Errorf("same email must share a placeholder: %q vs %q", p1, p2)
	}
	if p3 := reg.Assign(CatEmail, "other@example.net"); p3 != "[EMAIL_2]" {
		t.Errorf("second email should be [EMAIL_2], got %q", p3)
	}
	// Category label mapping: person_names → PERSON etc. (CLAUDE.md §5).
	if p := reg.Assign(CatPersonNames, "Marie Duval"); p != "[PERSON_1]" {
		t.Errorf("person placeholder = %q, want [PERSON_1]", p)
	}
	if p := reg.Assign(CatEntityNames, "Alpine Trust"); p != "[ENTITY_1]" {
		t.Errorf("client placeholder = %q, want [ENTITY_1]", p)
	}

	export := reg.Export()
	if len(export) != 4 {
		t.Fatalf("want 4 mapping entries, got %d", len(export))
	}
	// The doubly-seen email must report Count 2 for the key export.
	for _, e := range export {
		if e.Placeholder == "[EMAIL_1]" && e.Count != 2 {
			t.Errorf("[EMAIL_1] count = %d, want 2", e.Count)
		}
	}
}

// TestChecksumFailureScoresRatherThanVetoes is the checksum policy: a
// corroborating checksum that fails LOWERS the span's confidence and never
// removes it.
//
// The fixture is the shape a template or test document actually contains: a
// synthetic IBAN whose mod-97 remainder is 74 rather than 1. Vetoing it left the
// country code and the check digits in clear text, and let the credit-card
// recognizer claim the 16-digit interior instead, so the mapping asserted the
// document held a card that never existed.
func TestChecksumFailureScoresRatherThanVetoes(t *testing.T) {
	const text = "IBAN LU88 0055 6600 4321 6501 - BIC/SWIFT: BABAAXIL"
	spans := DetectPIISelected(text, DepthSelection(PresetThorough, CountryLU), CountryLU)

	var iban *Span
	for i := range spans {
		if spans[i].Category == CatIBAN {
			iban = &spans[i]
		}
		if spans[i].Category == CatCreditCard {
			t.Errorf("the IBAN interior was claimed as a credit card (%q): the mapping would state the document held a card that does not exist", spans[i].Original)
		}
	}
	if iban == nil {
		t.Fatalf("no iban span for a checksum-invalid IBAN, all spans: %+v", spans)
	}
	if iban.Original != "LU88 0055 6600 4321 6501" {
		t.Errorf("the iban span is %q, want the whole identifier %q", iban.Original, "LU88 0055 6600 4321 6501")
	}
	if iban.Confidence != ConfidenceChecksumFailed {
		t.Errorf("confidence %v, want ConfidenceChecksumFailed (%v): a failed checksum scores the span, it does not veto it",
			iban.Confidence, ConfidenceChecksumFailed)
	}

	// A checksum-VALID IBAN keeps the full deterministic score, so the reduced
	// score means "the digits did not add up" and nothing else.
	valid := DetectPIISelected("account LU28 0019 4006 4475 0000 please", DepthSelection(PresetSoft, CountryLU), CountryLU)
	for _, sp := range valid {
		if sp.Category == CatIBAN && sp.Confidence != ConfidenceDeterministic {
			t.Errorf("a valid IBAN scored %v, want ConfidenceDeterministic (%v)", sp.Confidence, ConfidenceDeterministic)
		}
	}
}

// TestLUPhoneAcceptsTheAllocatedRange: Luxembourg numbers are not fixed length.
// A six-digit base plus a PBX extension is 7 or 10 national significant digits,
// and an exactly-nine rule left both in the document, which is the direction
// that leaks.
func TestLUPhoneAcceptsTheAllocatedRange(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"call +352 29 19 19 5 for the desk", "+352 29 19 19 5"},
		{"switchboard +352 29 19 19 2100 please", "+352 29 19 19 2100"},
		{"call +352 621 000 111 today", "+352 621 000 111"},
		{"dial 00352 26 12 34 56 now", "00352 26 12 34 56"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			spans := DetectPIISelected(tc.text, DepthSelection(PresetSoft, CountryLU), CountryLU)
			for _, sp := range spans {
				if sp.Category == CatPhone && sp.Original == tc.want {
					return
				}
			}
			t.Errorf("no phone span %q, all spans: %+v", tc.want, spans)
		})
	}
}

// TestBareWWWURLsAreMatched: a "www." label is a host name and nothing else, so
// requiring a scheme missed every website in a document that writes them the way
// documents do. Longest-match-first is what keeps a path from being split off.
func TestBareWWWURLsAreMatched(t *testing.T) {
	const text = "see www.nstar.lu and www.nstar.lu/privacy and www.statistiques.public.lu"
	spans := ResolveOverlaps(DetectPIISelected(text, DepthSelection(PresetSoft, CountryLU), CountryLU))
	got := map[string]bool{}
	for _, sp := range spans {
		if sp.Category == CatURL {
			got[sp.Original] = true
		}
	}
	for _, want := range []string{"www.nstar.lu", "www.nstar.lu/privacy", "www.statistiques.public.lu"} {
		if !got[want] {
			t.Errorf("no url span %q, got %v", want, got)
		}
	}

	// A bare "word.word" stays untouched: that is the false-positive class the
	// scheme requirement exists for, and the www. label is the exception to it.
	bare := DetectPIISelected("visit example.com sometime", DepthSelection(PresetSoft, CountryLU), CountryLU)
	for _, sp := range bare {
		if sp.Category == CatURL {
			t.Errorf("a bare domain was matched as a url: %q", sp.Original)
		}
	}
}
