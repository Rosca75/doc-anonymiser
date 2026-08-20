// engine/allowlist_test.go —  tests: tolerant allowlist
// CSV parsing, the downloadable template, and the surfaced seed defaults.
package engine

import (
	"strings"
	"testing"
)

func TestParseAllowlistCSV(t *testing.T) {
	cases := []struct {
		name    string
		data    string
		want    []string
		wantErr string // fragment of the expected error ("" = no error)
	}{
		{
			name: "header row with term column",
			data: "term\nCSSF\nIFRS 17\n",
			want: []string{"CSSF", "IFRS 17"},
		},
		{
			name: "no header, first column",
			data: "CSSF\nECB\n",
			want: []string{"CSSF", "ECB"},
		},
		{
			name: "term column not first",
			data: "note,term\nregulator,CSSF\nstandard,IFRS 17\n",
			want: []string{"CSSF", "IFRS 17"},
		},
		{
			name: "UTF-8 BOM and CRLF",
			data: "\xEF\xBB\xBFterm\r\nCSSF\r\n",
			want: []string{"CSSF"},
		},
		{
			name: "quoted terms with embedded commas",
			data: "term\n\"Basel III, revised\"\nCSSF\n",
			want: []string{"Basel III, revised", "CSSF"},
		},
		{
			name:    "semicolon-delimited rejected with actionable message",
			data:    "CSSF;regulator\nECB;central bank\n",
			wantErr: "semicolon",
		},
		{
			name: "case-insensitive duplicate collapse keeps first spelling",
			data: "term\nCSSF\ncssf\nCssf\nECB\n",
			want: []string{"CSSF", "ECB"},
		},
		{
			name: "whitespace trimmed and empties dropped",
			data: "term\n  CSSF  \n\n   \nECB\n",
			want: []string{"CSSF", "ECB"},
		},
		{
			name: "empty file is an empty list, not an error",
			data: "",
			want: []string{},
		},
		{
			name:    "unbalanced quote names the bad line",
			data:    "term\nCSSF\n\"broken\nECB\n",
			wantErr: "line",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAllowlistCSV([]byte(tc.data))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v (terms %v)", tc.wantErr, err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("terms = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("term[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestAllowlistTemplateRoundTrip: the shipped template must parse through
// its own parser and yield the three example terms.
func TestAllowlistTemplateRoundTrip(t *testing.T) {
	terms, err := ParseAllowlistCSV(AllowlistTemplateCSV())
	if err != nil {
		t.Fatalf("the template must parse cleanly: %v", err)
	}
	want := []string{"CSSF", "IFRS 17", "Luxembourg"}
	if len(terms) != len(want) {
		t.Fatalf("template terms = %v, want %v", terms, want)
	}
	for i := range want {
		if terms[i] != want[i] {
			t.Errorf("template term[%d] = %q, want %q", i, terms[i], want[i])
		}
	}
}

// TestDefaultAllowlistTerms: the seed is surfaced (non-empty, sorted,
// contains the documented regulators) and returns a COPY (mutating the
// result must not corrupt the seed).
func TestDefaultAllowlistTerms(t *testing.T) {
	terms := DefaultAllowlistTerms()
	if len(terms) == 0 {
		t.Fatal("the seeded defaults must be surfaced")
	}
	found := false
	for _, term := range terms {
		if term == "CSSF" {
			found = true
		}
	}
	if !found {
		t.Error("the seed must contain CSSF")
	}
	terms[0] = "MUTATED"
	if DefaultAllowlistTerms()[0] == "MUTATED" {
		t.Error("DefaultAllowlistTerms must return a copy, not the seed slice")
	}
}

// --- The vocabulary a document defines about itself --------------------------

// TestDiscoverDefinedTermsRecognisesBothIdioms: the dictionary form alone caught
// six of the nineteen defined terms in the measured contract; adding the inline
// parenthetical form caught all nineteen.
func TestDiscoverDefinedTermsRecognisesBothIdioms(t *testing.T) {
	const text = "“**Work Order**” means a document issued under this Agreement. " +
		"\"Confidential Information\" means anything marked as such. " +
		"“Loss” shall mean any damage. " +
		"the advisors named in Annex 1 (the “**Dedicated Advisors**”) shall " +
		"perform the work (together the “***Experts***”) (each a “Party”), " +
		"and each a “Counterparty”."

	got := map[string]string{}
	for _, d := range DiscoverDefinedTerms("a.docx", text) {
		got[d.Term] = d.Idiom
	}
	want := map[string]string{
		"Work Order":               DefinitionIdiomMeans,
		"Confidential Information": DefinitionIdiomMeans,
		"Loss":                     DefinitionIdiomMeans,
		"Dedicated Advisors":       DefinitionIdiomParenthetical,
		"Experts":                  DefinitionIdiomParenthetical,
		"Party":                    DefinitionIdiomParenthetical,
	}
	// An UNPARENTHESISED quotation is not a definition, whatever introduces it.
	// That bound is what keeps a party's short name out of the suppressor, since
	// a document introduces one as `referred to as "Contoso"`.
	if _, claimed := got["Counterparty"]; claimed {
		t.Error(`an unparenthesised "each a X" was read as a definition; the same shape ` +
			`introduces a party's short name, which is the value that most needs replacing`)
	}
	for term, idiom := range want {
		if got[term] != idiom {
			t.Errorf("%q was read as idiom %q, want %q (all terms found: %v)",
				term, got[term], idiom, got)
		}
	}
	// The markdown emphasis the converter wraps a bold term in is not part of the
	// term: the entry the user sees has to be the drafter's words.
	for term := range got {
		if strings.ContainsAny(term, "*_") {
			t.Errorf("a defined term kept its emphasis markers: %q", term)
		}
	}
}

// TestDefinedTermsLeaveAPartyNameAlone: a party's short name is introduced
// UNPARENTHESISED ("hereinafter referred to as “Contoso”"), and it is
// the value that most needs replacing.
//
// This is why the parenthetical idiom REQUIRES an article: it is what separates
// a definition from an ordinary aside, and dropping it would let a document that
// writes (“Contoso”) suppress its own party name.
func TestDefinedTermsLeaveAPartyNameAlone(t *testing.T) {
	const text = "hereinafter referred to as “**Contoso**” and " +
		"hereinafter referred to as the “**Consultant**”, trading as “NStar”."
	for _, d := range DiscoverDefinedTerms("a.docx", text) {
		if d.Term == "Contoso" || d.Term == "NStar" {
			t.Errorf("the suppressor claimed a party name (%q, idiom %q); the parties are "+
				"exactly the values the review list exists to surface", d.Term, d.Idiom)
		}
	}
}

// TestDefinedTermsMatchWholeTermsOnly is the trap measured while building this:
// a prefix rule suppressed "Services NStar", because "Services" is a defined
// term and "Services NStar" contains a real entity.
//
// The suppression is enforced through Allowlist.Contains, which matches a WHOLE
// term case-insensitively, and that is load-bearing rather than incidental.
func TestDefinedTermsMatchWholeTermsOnly(t *testing.T) {
	allow := NewEmptyAllowlist()
	ApplyDefinedTerms(allow, DiscoverDefinedTerms("a.docx",
		"“Services” means the work described in a Work Order. "+
			"“Work Order” means the ordering document."))

	for _, suppressed := range []string{"Services", "services", "Work Order", "Work Orders"} {
		if !allow.Contains(suppressed) {
			t.Errorf("%q is a defined term or one of its inflections and is not suppressed", suppressed)
		}
	}
	for _, kept := range []string{"Services NStar", "NStar Services", "Contoso"} {
		if allow.Contains(kept) {
			t.Errorf("%q was suppressed by a defined term it merely CONTAINS; the real entity "+
				"inside it is exactly what has to be found", kept)
		}
	}
}

// TestDefinedTermInflectionsAreSuppressedToo: a document that defines "Work
// Order" writes "Work Orders" in the same breath, and they are one vocabulary
// item. Leaving the plural in the review list beside the suppressed singular
// reads as the suppressor not working.
func TestDefinedTermInflectionsAreSuppressedToo(t *testing.T) {
	cases := map[string][]string{
		"Work Order":       {"Work Orders", "Work Order's"},
		"Disclosing Party": {"Disclosing Parties", "Disclosing Party's"},
		"Annex":            {"Annexes"},
	}
	for term, inflections := range cases {
		allow := NewEmptyAllowlist()
		ApplyDefinedTerms(allow, []DefinedTerm{{Term: term, Idiom: DefinitionIdiomMeans}})
		for _, form := range inflections {
			if !allow.Contains(form) {
				t.Errorf("%q is an inflection of the defined term %q and is not suppressed", form, term)
			}
		}
	}
}
