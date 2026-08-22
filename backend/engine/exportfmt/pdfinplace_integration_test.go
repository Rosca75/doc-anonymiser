//go:build integration

// engine/exportfmt/pdfinplace_integration_test.go — the in-place PDF export
// over committed fixtures: the produced file passes the whole-file leak scan,
// the originals are absent, the pixels outside the replaced regions are
// identical, the non-content surfaces are scrubbed or dropped, and the
// refusal fires only on occurrences the whole ladder genuinely cannot locate.
//
// Integration tier: real file I/O over committed binary fixtures, the
// vendored PDF library exercised end to end. Deterministic and hermetic: no
// network, no service.
package exportfmt

import (
	"strings"
	"testing"

	asposepdf "github.com/aspose-pdf-foss/aspose-pdf-foss-for-go"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/engine/convert"
)

// inPlaceConfig is testConfig with the given values pre-assigned to the
// registry, the way a real export arrives after a run.
func inPlaceConfig(values ...engine.Value) Config {
	cfg := testConfig(values...)
	for _, v := range values {
		cfg.Registry.Assign(v.Category, v.MainText)
	}
	return cfg
}

func TestPDFInPlaceExport(t *testing.T) {
	t.Run("roundtrip/produced_file_is_the_original_with_replacements", func(t *testing.T) {
		raw := fixture(t, "pdf_gate_text.pdf")
		cfg := inPlaceConfig(
			engine.Value{Category: "person_names", MainText: "Harriet Volkmer"},
			engine.Value{Category: "entity_names", MainText: "Societe Miradour"},
		)
		before := gateOpenStream(t, raw)

		result, err := ExportPDFInPlace(raw, nil, cfg)
		if err != nil {
			t.Fatalf("ExportPDFInPlace: %v", err)
		}
		if result.Counts.Total() == 0 {
			t.Fatal("the ladder located nothing; the fixture plants both values")
		}

		// The originals are absent from EVERY object of the produced file.
		findings, _, err := ScanPDFForNeedles(result.Data, []string{"Harriet Volkmer", "Societe Miradour"})
		if err != nil {
			t.Fatalf("ScanPDFForNeedles: %v", err)
		}
		if len(findings) != 0 {
			t.Errorf("originals survive in the produced file: %+v", findings)
		}

		after := gateOpenStream(t, result.Data)
		if got, want := after.PageCount(), before.PageCount(); got != want {
			t.Errorf("page count changed: %d, want %d (an in-place export keeps the original's structure)", got, want)
		}
		texts, err := after.ExtractText()
		if err != nil {
			t.Fatalf("ExtractText(produced): %v", err)
		}
		joined := strings.Join(texts, "\n")
		for _, placeholder := range []string{"[PERSON_1]", "[ENTITY_1]"} {
			if !strings.Contains(joined, placeholder) {
				t.Errorf("placeholder %s is not extractable from the produced file", placeholder)
			}
		}
		// Untouched neighbours survive as text.
		for _, neighbour := range []string{"Ostrell Group", "Luxembourg", "Quentin Marsh"} {
			if !strings.Contains(joined, neighbour) {
				t.Errorf("neighbour %q was destroyed by the export", neighbour)
			}
		}

		// Pixels outside the replaced regions are identical: render each page
		// masking the union of the located rectangles for the values on it.
		plan := locateRectsForTest(t, raw, cfg)
		for p := 1; p <= before.PageCount(); p++ {
			mask := plan[p]
			if mask == nil {
				if diff := countPixelDiff(gateRender(t, before, p), gateRender(t, after, p), nil); diff != 0 {
					t.Errorf("page %d: %d pixels changed on a page with no replacement", p, diff)
				}
				continue
			}
			pageH := pageHeightPt(t, before, p)
			diff := countPixelDiff(gateRender(t, before, p), gateRender(t, after, p),
				&pixelMask{rect: *mask, pageH: pageH, margin: 4})
			if diff != 0 {
				t.Errorf("page %d: %d pixels changed OUTSIDE the replaced regions", p, diff)
			}
		}
	})

	t.Run("redaction/fragment_split_values_export_and_manufactured_value_never_refuses", func(t *testing.T) {
		raw := fixture(t, "pdf_gate_fragments.pdf")

		// The extraction must not offer the detector text that was never
		// contiguous: the two labels merely share a baseline.
		_, pages, _, err := convert.PDFWithPages(raw)
		if err != nil {
			t.Fatalf("PDFWithPages: %v", err)
		}
		text := strings.Join(pages, "\n")
		if strings.Contains(text, "Bertrand Malraux") {
			t.Fatal("the extraction manufactures a value out of fragments 384.8 pt apart on one baseline")
		}
		for _, real := range []string{"Sylvie Renard", "Jean Paul Aubry", "Nordwind Associates"} {
			if !strings.Contains(text, real) {
				t.Fatalf("the extraction lost the real value %q (text: %q)", real, text)
			}
		}

		// "Bertrand Malraux" is a value the OLD extraction would have
		// manufactured; declaring it must NEVER cause a refusal, because the
		// new page text no longer contains it, so there is nothing to locate.
		cfg := inPlaceConfig(
			engine.Value{Category: "person_names", MainText: "Sylvie Renard"},
			engine.Value{Category: "person_names", MainText: "Jean Paul Aubry"},
			engine.Value{Category: "entity_names", MainText: "Nordwind Associates"},
		)
		cfg.Values = append(cfg.Values, engine.Value{Category: "person_names", MainText: "Bertrand Malraux"})

		result, err := ExportPDFInPlace(raw, nil, cfg)
		if err != nil {
			t.Fatalf("the export refused over a value the extraction no longer manufactures: %v", err)
		}
		if result.Counts.Fragment < 2 {
			t.Errorf("fragment rung located %d occurrences, want at least the two names split across draw operations", result.Counts.Fragment)
		}
		if result.Counts.Wrapped+result.Counts.Fragment+result.Counts.Literal+result.Counts.Tolerant < 3 {
			t.Errorf("ladder counts %+v, want all three real values located", result.Counts)
		}

		findings, _, err := ScanPDFForNeedles(result.Data, []string{"Sylvie Renard", "Jean Paul Aubry", "Nordwind Associates", "Sylvie", "Renard", "Nordwind", "Associates"})
		if err != nil {
			t.Fatalf("ScanPDFForNeedles: %v", err)
		}
		if len(findings) != 0 {
			t.Errorf("replaced values survive the fragment redactions: %+v", findings)
		}
	})

	t.Run("errors/refusal_names_the_placeholder_the_page_and_the_md_way_out", func(t *testing.T) {
		// The interleaved-capitals repair reorders letters, so the pipeline's
		// "BIDDING RULES" exists in the derived text and NOWHERE on the page:
		// the whole ladder fails, and the export must refuse rather than ship
		// a file that still carries the original.
		raw := fixture(t, "textlayer.pdf")
		cfg := inPlaceConfig(engine.Value{Category: "other_names", MainText: "BIDDING RULES"})

		_, err := ExportPDFInPlace(raw, nil, cfg)
		if err == nil {
			t.Fatal("the export accepted an occurrence the whole ladder cannot locate")
		}
		msg := err.Error()
		for _, want := range []string{"[OTHER_1]", "page 1", ".md", "NOT exported"} {
			if !strings.Contains(msg, want) {
				t.Errorf("refusal %q does not carry %q; the refusal must name the placeholder, the page and the way out", msg, want)
			}
		}
		if strings.Contains(msg, "BIDDING") {
			t.Errorf("refusal %q repeats the original value; only the placeholder may be spoken", msg)
		}
	})

	t.Run("roundtrip/non_content_surfaces_scrubbed_and_drops_reported", func(t *testing.T) {
		// The surfaces fixture plants one name in the body, an annotation, an
		// outline title, the Info dictionary and the XMP packet. Attachments
		// and a JavaScript action are added here through the library, so the
		// drop path has something to drop.
		doc := gateOpenStream(t, fixture(t, "pdf_gate_surfaces.pdf"))
		if _, err := doc.EmbeddedFiles().AddFromStream("notes.txt", strings.NewReader("an inner document the pipeline never read")); err != nil {
			t.Fatalf("AddFromStream: %v", err)
		}
		if err := doc.JavaScript().Add("init", "app.alert('hello');"); err != nil {
			t.Fatalf("JavaScript Add: %v", err)
		}
		raw := gateWrite(t, doc)

		cfg := inPlaceConfig(engine.Value{Category: "person_names", MainText: "Nadia Okonkwo"})
		result, err := ExportPDFInPlace(raw, []MetaField{
			{Part: "pdf:Info", Name: "Title", Value: "Reviewed title"},
		}, cfg)
		if err != nil {
			t.Fatalf("ExportPDFInPlace: %v", err)
		}

		// The planted name is gone from EVERY surface: the body, the
		// annotation, the outline, the Info dictionary and the XMP packet.
		findings, _, err := ScanPDFForNeedles(result.Data, []string{"Nadia Okonkwo"})
		if err != nil {
			t.Fatalf("ScanPDFForNeedles: %v", err)
		}
		if len(findings) != 0 {
			t.Errorf("the planted name survives outside the body: %+v", findings)
		}
		if result.Extras == 0 {
			t.Error("Extras = 0; the annotation and outline scrub made replacements the preview never showed, and they must be reported")
		}
		if len(result.Dropped) < 2 {
			t.Errorf("Dropped = %v, want the attachment and the JavaScript action reported", result.Dropped)
		}

		produced := gateOpenStream(t, result.Data)
		if n := produced.EmbeddedFiles().Count(); n != 0 {
			t.Errorf("the produced file still carries %d embedded attachment(s)", n)
		}
		if n := produced.JavaScript().Count(); n != 0 {
			t.Errorf("the produced file still carries %d JavaScript action(s)", n)
		}
		// The reviewed Info value is the user's word over the proposal.
		info, err := produced.Info()
		if err != nil {
			t.Fatalf("Info(produced): %v", err)
		}
		if info.Title != "Reviewed title" {
			t.Errorf("Info Title = %q, want the reviewed value", info.Title)
		}
	})

	t.Run("roundtrip/save_discipline_leaves_no_orphaned_original", func(t *testing.T) {
		// The whole-file scan over the produced bytes IS the assertion that
		// RemoveUnusedObjects ran: a naked WriteTo keeps the pre-edit content
		// stream readable (pinned by the gate suite), so a clean scan after a
		// replacement proves the discipline held.
		raw := fixture(t, "pdf_gate_text.pdf")
		cfg := inPlaceConfig(engine.Value{Category: "person_names", MainText: "Harriet Volkmer"})
		result, err := ExportPDFInPlace(raw, nil, cfg)
		if err != nil {
			t.Fatalf("ExportPDFInPlace: %v", err)
		}
		if PDFHasIncrementalUpdate(result.Data) {
			t.Error("the produced file has an incremental-update shape; the save must be a single-body full rewrite")
		}
	})
}

// locateRectsForTest re-runs the plan's location pass and unions, per page,
// every located rectangle, inflated by the grown-replacement direction, so
// the pixel comparison can mask exactly what the export was allowed to touch.
func locateRectsForTest(t *testing.T, raw []byte, cfg Config) map[int]*asposepdf.Rectangle {
	t.Helper()
	doc, layouts, err := openPDFForExport(raw)
	if err != nil {
		t.Fatalf("openPDFForExport: %v", err)
	}
	out := map[int]*asposepdf.Rectangle{}
	work, _, _ := locatePDFWork(doc, layouts, cfg)
	for pi, w := range work {
		page := doc.Pages()[pi]
		var rects []asposepdf.Rectangle
		// Both gestures rewrite their LINE's text operators (a replacement
		// re-emits the fragment, a redaction repositions the surviving glyphs
		// through kerning gaps), so the whole line is the replaced region for
		// the pixel comparison; every other line and the rest of the page
		// must stay identical.
		for _, r := range w.replace {
			for _, m := range (livePDFSearcher{page: page}).search(r.original, false) {
				rects = append(rects, lineExtentForRect(layouts[pi], m.Rect))
			}
		}
		for _, rd := range w.redacts {
			rects = append(rects, lineExtentForRect(layouts[pi], rd.rect))
		}
		if len(rects) == 0 {
			continue
		}
		u := rects[0]
		for _, r := range rects[1:] {
			u = unionRects(u, r)
		}
		out[pi+1] = &u
	}
	return out
}

// lineExtentForRect widens a redaction rectangle to its model line's full
// horizontal extent, keeping the rectangle's own vertical bounds.
func lineExtentForRect(layout convert.PDFPageLayout, r asposepdf.Rectangle) asposepdf.Rectangle {
	for _, line := range layout.Lines {
		for _, f := range line.Fragments {
			if abs64(f.Y-r.LLY) > maxf(f.Height, 1) {
				continue
			}
			left, right := f.X, f.X+f.Width
			for _, g := range line.Fragments {
				if g.X < left {
					left = g.X
				}
				if g.X+g.Width > right {
					right = g.X + g.Width
				}
			}
			r.LLX, r.URX = left, right
			return r
		}
	}
	return r
}
