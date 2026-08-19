// engine/convert/convert_test.go — the UNIT-tier converter tests.
//
// TIER: unit (docs/TESTING.md). This file holds only the converter tests that
// need no binary fixture: the pure RepairPDFText table, the non-zip rejection
// path, and the slide-XML walker cases, all cheap and deterministic. The
// walker cases call the unexported parseSlide on a slide XML string, because
// what a shape's text becomes is exactly the unexported behaviour under test
// and a zip archive would add nothing to the question. The golden conversions that
// decode the committed .docx/.pptx/.xlsx/.pdf fixtures live in
// convert_integration_test.go, and the wall-clock budget lives in
// convert_deep_test.go. The shared fixture(...) helper is in fixtures_test.go,
// which carries no build tag so every tier can use it.
package convert

import (
	"strings"
	"testing"
)

// TestDocxRejectsNonZip pins the actionable error for a legacy .doc-style
// (non-zip) payload.
func TestDocxRejectsNonZip(t *testing.T) {
	_, _, err := Docx([]byte("this is not a zip archive"))
	if err == nil || !strings.Contains(err.Error(), ".docx") {
		t.Errorf("want actionable not-a-docx error, got %v", err)
	}
}

// TestRepairPDFText is the table-driven repair suite, including the nb1
// 'B R IDDING ULES' class of defect and the mandatory no-op cases.
func TestRepairPDFText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "nb1 interleaved capitals",
			in:   "B R IDDING ULES",
			want: "BIDDING RULES",
		},
		{
			name: "repair embedded in a sentence",
			in:   "the B R IDDING ULES apply here",
			want: "the BIDDING RULES apply here",
		},
		{
			name: "three-way interleave",
			in:   "T B C ERMS OARD ONTRACT",
			want: "TERMS BOARD CONTRACT",
		},
		{
			name: "lowercase text is a no-op",
			in:   "plain lowercase text stays untouched",
			want: "plain lowercase text stays untouched",
		},
		{
			name: "single capital words are never merged",
			in:   "I saw A REPORT yesterday",
			want: "I saw A REPORT yesterday",
		},
		{
			name: "double spaces collapse",
			in:   "double  spaces   here",
			want: "double spaces here",
		},
		{
			name: "capital run without matching fragments stays",
			in:   "sections A B and C apply",
			want: "sections A B and C apply",
		},
		{
			name: "multi-line input repaired per line",
			in:   "B R IDDING ULES\nplain line",
			want: "BIDDING RULES\nplain line",
		},
		{
			name: "fi ligature folds to ascii",
			in:   "Adam Tymo\uFB01ejewicz",
			want: "Adam Tymofiejewicz",
		},
		{
			name: "ff fl ffi ligatures fold to ascii",
			in:   "o\uFB00ice \uFB02exi \uFB03rst",
			want: "office flexi ffirst",
		},
		{
			name: "text with no ligature is untouched",
			in:   "ordinary fi fl text",
			want: "ordinary fi fl text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RepairPDFText(tt.in); got != tt.want {
				t.Errorf("RepairPDFText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// slideXML wraps shape XML in the minimum slide envelope walkShapes needs, so
// a case can state the shape it is about and nothing else.
func slideXML(shapes string) []byte {
	const ns = ` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`
	return []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<p:sld` + ns + `><p:cSld><p:spTree>` + shapes + `</p:spTree></p:cSld></p:sld>`)
}

// TestPptxSoftLineBreaks covers a:br, the soft line break PowerPoint writes
// when a user presses Shift+Enter inside one paragraph, and the title overflow
// it exposes. Both halves belong together: a break that ends the line is only
// an improvement if the lines after the first one still reach the markdown.
func TestPptxSoftLineBreaks(t *testing.T) {
	t.Run("extraction/soft_break_does_not_glue_runs", func(t *testing.T) {
		// The two runs name an organisation and a person. Concatenated they
		// read as one nonexistent name, which discovery then proposes as a
		// person: a converter defect becomes a wrong suggestion.
		_, body, err := parseSlide(slideXML(
			`<p:sp><p:nvSpPr><p:nvPr><p:ph type="body"/></p:nvPr></p:nvSpPr><p:txBody>` +
				`<a:p><a:r><a:t>Northwind Group</a:t></a:r><a:br/><a:r><a:t>Jane Miller</a:t></a:r></a:p>` +
				`</p:txBody></p:sp>`))
		if err != nil {
			t.Fatalf("parseSlide: %v", err)
		}
		if strings.Contains(body, "Northwind GroupJane Miller") {
			t.Errorf("a soft break glued the runs on either side of it into one word: got %q, want the two lines separated", body)
		}
		want := "- Northwind Group\n- Jane Miller\n\n"
		if body != want {
			t.Errorf("body = %q, want %q (input: two runs separated by a:br)", body, want)
		}
	})

	t.Run("extraction/title_overflow_reaches_the_body", func(t *testing.T) {
		// A cover slide: title, author and date in ONE paragraph, broken by
		// a:br. The heading is one line; the other two are document text and
		// must survive, or anonymisation never sees the author's name.
		title, body, err := parseSlide(slideXML(
			`<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody>` +
				`<a:p><a:r><a:t>Annual Review</a:t></a:r><a:br/>` +
				`<a:r><a:t>Prepared by Jane Miller</a:t></a:r><a:br/>` +
				`<a:r><a:t>Date: 12 February 2024</a:t></a:r></a:p>` +
				`</p:txBody></p:sp>`))
		if err != nil {
			t.Fatalf("parseSlide: %v", err)
		}
		if title != "Annual Review" {
			t.Errorf("title = %q, want %q (only the first line is the heading)", title, "Annual Review")
		}
		want := "Prepared by Jane Miller\nDate: 12 February 2024\n\n"
		if body != want {
			t.Errorf("title overflow = %q, want %q (plain lines, not bullets, and not dropped)", body, want)
		}
	})

	t.Run("extraction/body_soft_break_still_bullets", func(t *testing.T) {
		// The title rule must not turn body text into prose: a body
		// placeholder is bulleted, one bullet per line, break or not.
		_, body, err := parseSlide(slideXML(
			`<p:sp><p:nvSpPr><p:nvPr><p:ph type="body"/></p:nvPr></p:nvSpPr><p:txBody>` +
				`<a:p><a:r><a:t>First point</a:t></a:r><a:br/><a:r><a:t>Second point</a:t></a:r></a:p>` +
				`<a:p><a:pPr lvl="1"/><a:r><a:t>Nested point</a:t></a:r><a:br/><a:r><a:t>Nested again</a:t></a:r></a:p>` +
				`</p:txBody></p:sp>`))
		if err != nil {
			t.Fatalf("parseSlide: %v", err)
		}
		want := "- First point\n- Second point\n  - Nested point\n  - Nested again\n\n"
		if body != want {
			t.Errorf("body = %q, want %q (a break keeps the bullet and the outline level)", body, want)
		}
	})
}
