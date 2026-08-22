//go:build integration

// engine/convert/convert_integration_test.go — the INTEGRATION-tier converter
// golden tests.
//
// TIER: integration (docs/TESTING.md). Each test decodes a committed BINARY
// fixture (.docx/.pptx/.xlsx/.pdf) through the real format machinery: the
// standard-library zip reader, excelize, and the vendored PDF library. That is
// real-format behaviour, which the integration tier owns; the unit tier next
// door keeps only the fixture-free logic (RepairPDFText, the non-zip
// rejection). These are also the one end-to-end happy path per input format
// the tier discipline asks for. The shared fixture(...) helper is defined in
// the untagged fixtures_test.go.
package convert

import (
	"bytes"
	"strings"
	"testing"
)

// TestDocxGolden pins the full markdown output for the committed .docx
// fixture: heading, emphasis, hyperlink, image placeholder, both list
// kinds, table and French text.
func TestDocxGolden(t *testing.T) {
	raw := fixture(t, "report.docx")
	md, warnings, err := Docx(raw)
	if err != nil {
		t.Fatalf("Docx: %v", err)
	}

	want := "# Engagement Report\n\n" +
		"**Confidential** status: *draft* see [project site](https://alpine.example.com) *[image omitted]*\n\n" +
		"- First bullet\n" +
		"- Second bullet\n" +
		"1. Step one\n" +
		"  1. Sub step\n" +
		"\n" +
		"| Name | Role |\n| --- | --- |\n| Marie Duval | Reviewer |\n\n" +
		"Réunion avec Amélie Lefèvre à Luxembourg.\n\n"
	if md != want {
		t.Errorf("docx markdown mismatch:\n--- got ---\n%s\n--- want ---\n%s", md, want)
	}

	// One dropped image must surface as exactly one warning.
	if len(warnings) != 1 || !strings.Contains(warnings[0], "image") {
		t.Errorf("want one image warning, got %v", warnings)
	}
}

// TestPptxGolden pins slide sections, bullet indentation, the table, the
// speaker notes (with the slide-number placeholder excluded) and the
// untitled French slide 2. Slide 1's title is three lines joined by soft
// breaks, so it also pins the split: line one is the heading, the other two
// survive as body lines.
func TestPptxGolden(t *testing.T) {
	raw := fixture(t, "deck.pptx")
	md, _, err := Pptx(raw)
	if err != nil {
		t.Fatalf("Pptx: %v", err)
	}

	want := "## Slide 1: Quarterly Review\n\n" +
		"Prepared by Marie Duval\nInternal draft, 12 February 2024\n\n" +
		"- Revenue grew\n" +
		"  - Driven by Borealis Fund\n" +
		"\n" +
		"| KPI | Value |\n| --- | --- |\n| NPS | 42 |\n\n" +
		"**Notes:**\n\nMention the Alpine Trust engagement.\n\n" +
		"## Slide 2\n\n" +
		"- Prochaines étapes — réunion à Luxembourg\n\n"
	if md != want {
		t.Errorf("pptx markdown mismatch:\n--- got ---\n%s\n--- want ---\n%s", md, want)
	}
	// The slide-number placeholder text must not leak into the notes.
	if strings.Contains(md, "**Notes:**\n\n1\n") {
		t.Error("slide-number placeholder leaked into the notes block")
	}
}

// TestXlsxRouting pins the smart routing: the flat sheet becomes a Grid
// (with trailing empties trimmed), the merged-cell sheet becomes JSON.
func TestXlsxRouting(t *testing.T) {
	raw := fixture(t, "workbook.xlsx")
	sheets, _, err := Xlsx(raw)
	if err != nil {
		t.Fatalf("Xlsx: %v", err)
	}
	if len(sheets) != 2 {
		t.Fatalf("want 2 sheets, got %d", len(sheets))
	}

	clients := sheets[0]
	if clients.Name != "Clients" || !clients.Flat {
		t.Fatalf("sheet 1: want FLAT 'Clients', got %+v", clients.Name)
	}
	// Data bounds: 4 rows × 3 cols — the whitespace-only E7 cell must be
	// trimmed away, not force 7 rows × 5 cols.
	if len(clients.Grid) != 4 || len(clients.Grid[0]) != 3 {
		t.Errorf("flat grid = %d×%d, want 4×3 (trailing empties trimmed)", len(clients.Grid), len(clients.Grid[0]))
	}
	if clients.Grid[1][0] != "Marie Duval" {
		t.Errorf("grid content wrong: %v", clients.Grid[1])
	}

	resume := sheets[1]
	if resume.Name != "Résumé" || resume.Flat {
		t.Fatalf("sheet 2: want COMPLEX 'Résumé', got %+v", resume.Name)
	}
	// The JSON must carry cell addresses and values, and the warning must
	// explain the routing decision.
	for _, wantFrag := range []string{`"A1": "Budget prévisionnel"`, `"B3": "12500"`} {
		if !strings.Contains(resume.JSON, wantFrag) {
			t.Errorf("complex JSON missing %s:\n%s", wantFrag, resume.JSON)
		}
	}
	if len(resume.Warnings) != 1 || !strings.Contains(resume.Warnings[0], "merged") {
		t.Errorf("want a merged-cells routing warning, got %v", resume.Warnings)
	}
}

// TestPDFGolden proves per-page extraction plus the spacing repair on the
// committed text-layer fixture.
func TestPDFGolden(t *testing.T) {
	raw := fixture(t, "textlayer.pdf")
	md, warnings, err := PDF(raw)
	if err != nil {
		t.Fatalf("PDF: %v", err)
	}
	if !strings.Contains(md, "BIDDING RULES for the Alpine Trust engagement") {
		t.Errorf("spacing repair not applied, got:\n%s", md)
	}
	if !strings.Contains(md, "Contact: marie.duval@example.com (double spaces on purpose)") {
		t.Errorf("double-space collapse missing, got:\n%s", md)
	}
	// The experimental warning must always be present; the repair warning
	// must be present because this fixture needed repair.
	joined := strings.Join(warnings, " | ")
	if !strings.Contains(joined, "EXPERIMENTAL") || !strings.Contains(joined, "repair") {
		t.Errorf("want experimental + repair warnings, got %v", warnings)
	}
}

// TestPDFScannedRejected pins the exact actionable message from CLAUDE.md
// §5 for a PDF without a text layer.
func TestPDFScannedRejected(t *testing.T) {
	raw := fixture(t, "scanned.pdf")
	_, _, err := PDF(raw)
	if err == nil {
		t.Fatal("scanned PDF accepted, want rejection")
	}
	if err.Error() != ErrScannedPDF {
		t.Errorf("error = %q, want exactly %q", err.Error(), ErrScannedPDF)
	}
}

// TestPDFWithAnUnmappableTextLayerIsRefused: a page drawn with a /Type0
// /Identity-H font that carries no /ToUnicode CMap. Nothing can map its glyph
// codes back to characters.
//
// It HAS a text layer, so the scanned refusal would be wrong, and it opens
// cleanly, so the damaged refusal would be wrong too. Without this refusal such
// a file imports, extracts a page of characters of which not one is a letter,
// and every detection route truthfully reports finding nothing, which a user is
// entitled to read as "nothing to anonymise".
func TestPDFWithAnUnmappableTextLayerIsRefused(t *testing.T) {
	_, _, err := PDF(fixture(t, "pdf_no_tounicode.pdf"))
	if err == nil {
		t.Fatal("a PDF whose text layer cannot be mapped to characters must be REFUSED, not imported as a page of unknown glyphs")
	}
	if err.Error() != ErrUnmappablePDF {
		t.Errorf("error = %q, want exactly %q", err.Error(), ErrUnmappablePDF)
	}
}

// TestOneLineToUnicodeCMapIsDecoded PINS THE VENDOR PATCH in
// vendor/.../aspose-pdf-foss-for-go/cmap.go.
//
// working_deck.pdf is a deck printed through Microsoft Print To PDF: nine
// /Type0 /Identity-H fonts, each with a perfectly valid /ToUnicode CMap written
// as a SINGLE LINE, which is legal because a CMap is a token program and a
// newline in one is ordinary whitespace. Upstream v0.7.0 reads those CMaps line
// by line, so it never enters the bfchar section, builds an empty map, and every
// glyph in the document extracts as U+FFFD.
//
// If `go mod vendor` ever silently drops the patch, this test is what fails.
// Without it the loss would show up as this document refusing to import, which
// reads as a fixture problem rather than as a missing patch.
func TestOneLineToUnicodeCMapIsDecoded(t *testing.T) {
	md, pages, _, err := PDFWithPages(fixture(t, "working_deck.pdf"))
	if err != nil {
		t.Fatalf("working_deck.pdf must extract as characters: %v. If this says the text cannot be read as characters, the ToUnicode patch in vendor/github.com/aspose-pdf-foss/aspose-pdf-foss-for-go/cmap.go is gone, most likely because `go mod vendor` re-copied the upstream file. Restore it.", err)
	}
	if share := unmappableShare(md); share > 0.01 {
		t.Errorf("working_deck.pdf extracts %.1f%% unmappable characters; the one-line ToUnicode CMaps are not being decoded", 100*share)
	}
	// A spot check on real content: the deck is French, so an accented letter
	// proves the mapping reaches beyond ASCII.
	if !strings.Contains(md, "\u00e8") && !strings.Contains(md, "\u00e9") {
		t.Error("no accented character survived extraction, so the CMap is only partly applied")
	}
	if len(pages) == 0 {
		t.Error("no pages extracted")
	}
}

// TestReadablePDFFixturesAreNotRefusedAsUnmappable: the other side of the
// threshold. Every committed PDF that is meant to be readable must stay
// readable, or the guard above becomes a refusal of ordinary documents.
func TestReadablePDFFixturesAreNotRefusedAsUnmappable(t *testing.T) {
	for _, name := range []string{
		"framework_contract.pdf", "nstar_contoso_flyer.pdf",
		"textlayer.pdf", "pdf_gate_text.pdf", "pdf_gate_fragments.pdf",
	} {
		t.Run(name, func(t *testing.T) {
			md, _, _, err := PDFWithPages(fixture(t, name))
			if err != nil {
				t.Fatalf("%s must extract: %v", name, err)
			}
			if share := unmappableShare(md); share > maxUnmappableShare {
				t.Errorf("%s extracts %.1f%% unmappable characters, over the %.0f%% threshold; either extraction regressed or the threshold is too tight",
					name, 100*share, 100*maxUnmappableShare)
			}
		})
	}
}

// TestPDFDamagedIsNotScanned pins the two refusals apart: a file so damaged
// that no page survives the open gets the damaged-file message, never the
// scanned-PDF one, because "this is likely scanned" sends the user to an OCR
// tool that cannot help a truncated file.
func TestPDFDamagedIsNotScanned(t *testing.T) {
	raw := fixture(t, "pdf_gate_text.pdf")
	truncated := raw[:len(raw)/10]
	_, _, err := PDF(truncated)
	if err == nil {
		t.Fatal("a file truncated to 10% was accepted")
	}
	if err.Error() == ErrScannedPDF {
		t.Fatalf("a damaged file got the scanned-PDF message; the two need different sentences")
	}
	if !strings.Contains(err.Error(), "damaged") {
		t.Errorf("damaged-file error %q does not tell the user the file is damaged", err)
	}
}

// TestPDFEncryptedIsDistinguishable pins the password refusal: an encrypted
// file has a remedy a damaged one does not, and the message must say it.
func TestPDFEncryptedIsDistinguishable(t *testing.T) {
	doc := gateOpen(t, fixture(t, "pdf_gate_text.pdf"))
	doc.SetPassword("les-mots-de-passe", "les-mots-de-passe")
	doc.RemoveUnusedObjects()
	var buf bytes.Buffer
	if _, err := doc.WriteTo(&buf); err != nil {
		t.Fatalf("writing the encrypted fixture: %v", err)
	}
	_, _, err := PDF(buf.Bytes())
	if err == nil {
		t.Fatal("a password-protected file was accepted without the password")
	}
	if !strings.Contains(err.Error(), "password") {
		t.Errorf("encrypted-file error %q does not name the password remedy", err)
	}
}

// TestEveryCommittedFixtureIsGeneratable materialises every fixture this package
// knows how to build.
//
// It is what makes the instruction the other packages print ("run
// `go test -tags=integration ./backend/engine/convert/` once and commit what it
// writes") true. A fixture no test in this package asks for is never generated,
// so a reader following that message would find nothing had appeared and no
// error explaining why.
func TestEveryCommittedFixtureIsGeneratable(t *testing.T) {
	for _, name := range allFixtures {
		t.Run("extraction/fixture_"+name, func(t *testing.T) {
			if raw := fixture(t, name); len(raw) == 0 {
				t.Errorf("the fixture %s is empty", name)
			}
		})
	}
}
