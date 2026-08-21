// engine/allowlist_regex_test.go —  tests: regex allowlist
// entries. Literals still win at ANY confidence; regexes are compiled once
// at Add time; broken patterns are recorded in RegexErrors and never abort
// import.
package engine

import (
	"context"
	"strings"
	"testing"
)

// TestAllowlistRegexMatch: a "regex:" entry matches case-insensitively and
// short-circuits detection of any span whose text matches.
func TestAllowlistRegexMatch(t *testing.T) {
	a := NewEmptyAllowlist()
	a.Add("regex:^CVE-\\d{4}-\\d+$")
	a.Add("regex:host\\.internal\\.example$")
	if !a.Contains("CVE-2024-12345") {
		t.Error("regex must match CVE-2024-12345")
	}
	if !a.Contains("cve-2024-1") { // case-insensitive
		t.Error("regex must be case-insensitive")
	}
	if a.Contains("CVE-24-12345") { // wrong shape
		t.Error("regex must reject CVE-24-12345")
	}
	if !a.Contains("api.host.internal.example") {
		t.Error("suffix regex must match api.host.internal.example")
	}
	if len(a.RegexErrors) != 0 {
		t.Errorf("no regex errors expected, got %v", a.RegexErrors)
	}
}

// TestAllowlistRegexError: a broken pattern is dropped with an actionable
// error message and does not corrupt the remaining entries.
func TestAllowlistRegexError(t *testing.T) {
	a := NewEmptyAllowlist()
	a.Add("CSSF")
	a.Add("regex:[unbalanced")
	a.Add("regex:")
	a.Add("regex:^ok$")
	if !a.Contains("CSSF") || !a.Contains("ok") {
		t.Error("valid entries must survive broken siblings")
	}
	if len(a.RegexErrors) != 2 {
		t.Errorf("want 2 regex errors, got %v", a.RegexErrors)
	}
	// Errors must name the offending entry so the UI can point to it.
	joined := strings.Join(a.RegexErrors, "|")
	if !strings.Contains(joined, "[unbalanced") {
		t.Errorf("error must mention the bad pattern, got %v", a.RegexErrors)
	}
}

// TestAllowlistRegexTerms: Terms() surfaces both literal and regex entries,
// sorted (deterministic session files).
func TestAllowlistRegexTerms(t *testing.T) {
	a := NewEmptyAllowlist()
	a.Add("CSSF")
	a.Add("regex:^INTERNAL-\\w+$")
	a.Add("ECB")
	got := a.Terms()
	if len(got) != 3 {
		t.Fatalf("terms = %v, want 3", got)
	}
	// Sorted alphabetically: CSSF, ECB, regex:^INTERNAL-\w+$
	if got[0] != "CSSF" || got[1] != "ECB" || !strings.HasPrefix(got[2], "regex:") {
		t.Errorf("terms not sorted as expected: %v", got)
	}
}

// TestAllowlistRegexRemove: a regex entry is removed by its display
// spelling; the literal remove path is unaffected.
func TestAllowlistRegexRemove(t *testing.T) {
	a := NewEmptyAllowlist()
	a.Add("regex:^X-.+$")
	a.Add("CSSF")
	if !a.Contains("X-token") || !a.Contains("CSSF") {
		t.Fatal("preconditions failed")
	}
	a.Remove("regex:^X-.+$")
	if a.Contains("X-token") {
		t.Error("regex entry must be removable")
	}
	if !a.Contains("CSSF") {
		t.Error("removing a regex must not touch literals")
	}
}

// TestAllowlistIntegration: run the pipeline over text carrying both a
// valid credit card and a suffix pattern excluded via regex, and verify
// that (a) the checksum-vetted card IS anonymised, (b) the regex-listed
// substring is NOT.
func TestAllowlistIntegration(t *testing.T) {
	// The invalid card is written compactly (a spaced variant would also
	// match the phone regex, which is a documented existing overlap).
	text := "Test card 4532 0151 1283 0366, invalid 4532015112830367, ref CVE-2024-99999."
	allow := NewEmptyAllowlist()
	allow.Add("regex:^CVE-\\d{4}-\\d+$")

	res, err := Run(context.Background(), PipelineInput{
		Documents:  []Document{{Name: "f.txt", Format: FormatTXT, Markdown: text}},
		Categories: DepthSelection(PresetSoft, CountryLU),
		Allowlist:  allow,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := res.Documents[0].Anonymised
	// The valid credit card is replaced.
	if strings.Contains(out, "4532 0151 1283 0366") {
		t.Errorf("valid credit card must be anonymised, got: %s", out)
	}
	// The Luhn-invalid one is left alone by the credit-card recognizer
	// (proving the checksum vetoed the regex hit).
	if !strings.Contains(out, "4532015112830367") {
		t.Errorf("Luhn-invalid card must survive credit_card detection, got: %s", out)
	}
	if strings.Count(out, "[CREDIT_CARD_") != 1 {
		t.Errorf("exactly one credit_card placeholder expected, got: %s", out)
	}
	// The regex-allowlisted CVE ID is untouched (would have looked like a
	// date otherwise, but level-soft doesn't fire dates; either way it must
	// survive because the allowlist wins).
	if !strings.Contains(out, "CVE-2024-99999") {
		t.Errorf("regex-allowlisted CVE must survive, got: %s", out)
	}
}
