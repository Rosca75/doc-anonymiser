//go:build integration

// engine/exportfmt/pdffoss_gate_integration_test.go — the adoption gate's
// save, replacement, redaction, image and failure measurements
// (docs/change-13b.md steps 6, 7.2-7.7 and 8; criteria G1, G5, G6, G9 and the
// fixture halves of G7's ladder).
//
// Integration tier: real file I/O over committed binary fixtures, the
// vendored PDF library exercised end to end. Deterministic and hermetic: no
// network, no service, and no copilot is ever configured (the root boundary
// guard pdf_boundary_test.go holds that by scan).
//
// Everything measured here is asserted where the plan sets a hard
// requirement, and RECORDED via t.Log where the plan asks for an observation
// (overflow behaviour, per-placement listing): the GO/NO-GO note in
// docs/change-13.md carries the recorded numbers.
package exportfmt

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	asposepdf "github.com/aspose-pdf-foss/aspose-pdf-foss-for-go"

	"doc-anonymiser/backend/engine/imaging"
)

// gateOpenStream opens fixture bytes through the library's bytes-only entry
// point, the only constructor the engine is allowed.
func gateOpenStream(t *testing.T, raw []byte) *asposepdf.Document {
	t.Helper()
	doc, err := asposepdf.OpenStream(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the library could not open the fixture (%v)", err)
	}
	return doc
}

// gateWrite serialises a document back to bytes with the SAVE DISCIPLINE the
// gate measured to be necessary: RemoveUnusedObjects() first, then WriteTo.
//
// WriteTo alone is a full rewrite of a SINGLE body (no incremental update, no
// /Prev chain), but it serialises every object in the document's table,
// including one an edit orphaned: after ReplaceText the page points at the
// new content stream while the OLD one survives as an unreferenced object,
// readable with the original text. RemoveUnusedObjects() is the library's own
// documented remedy, and TestPDFFossGateSaveSemantics pins both halves of
// that fact.
func gateWrite(t *testing.T, doc *asposepdf.Document) []byte {
	t.Helper()
	doc.RemoveUnusedObjects()
	var buf bytes.Buffer
	if _, err := doc.WriteTo(&buf); err != nil {
		t.Fatalf("Document.WriteTo: %v", err)
	}
	return buf.Bytes()
}

// gateRender rasterises one page at 72 DPI, where one PDF point is one pixel,
// so rectangle arithmetic needs no scaling.
func gateRender(t *testing.T, doc *asposepdf.Document, pageNum int) image.Image {
	t.Helper()
	img, err := doc.RenderImage(pageNum, asposepdf.RenderOptions{DPI: 72})
	if err != nil {
		t.Fatalf("RenderImage(page %d): %v", pageNum, err)
	}
	return img
}

// --- G1: the save-semantics proof ------------------------------------------

func TestPDFFossGateSaveSemantics(t *testing.T) {
	t.Run("roundtrip/writeto_is_a_full_rewrite_not_an_incremental_update", func(t *testing.T) {
		raw := gateFixture(t, "pdf_gate_text.pdf")
		doc := gateOpenStream(t, raw)

		const sentinel = "Harriet Volkmer"
		n, err := doc.ReplaceText(sentinel, "[PERSON_1]")
		if err != nil {
			t.Fatalf("ReplaceText(%q): %v", sentinel, err)
		}
		if n == 0 {
			t.Fatalf("ReplaceText(%q) replaced nothing; the fixture plants it once", sentinel)
		}
		out := gateWrite(t, doc)

		// The leak scanner over the raw output: the sentinel must be gone from
		// every object, live or superseded.
		findings, _, err := ScanPDFForNeedles(out, []string{sentinel})
		if err != nil {
			t.Fatalf("ScanPDFForNeedles(output): %v", err)
		}
		if len(findings) != 0 {
			t.Errorf("the replaced sentinel survives in the output: %+v. If it sits in an appended body, WriteTo is an incremental update and D2 says NO-GO", findings)
		}
		// And the output must not have the incremental-update SHAPE at all,
		// even with nothing leaked through it.
		if PDFHasIncrementalUpdate(out) {
			t.Error("the output has an incremental-update shape (second body or /Prev chain); D2 makes this an automatic NO-GO")
		}
		if got := PDFBodyCount(out); got != 1 {
			t.Errorf("PDFBodyCount(output) = %d, want exactly 1", got)
		}
	})

	// The pin on the behaviour that makes the discipline necessary: WriteTo
	// WITHOUT RemoveUnusedObjects keeps the orphaned pre-edit content stream
	// in the output, readable. If a library version stops retaining orphans,
	// this pin fails and says the save discipline can simplify; if the
	// discipline is ever dropped while the retention stands, the subtest
	// above fails and blocks the leak. Between the two pins the behaviour
	// cannot drift unnoticed.
	t.Run("roundtrip/naked_writeto_retains_the_orphaned_original_stream", func(t *testing.T) {
		raw := gateFixture(t, "pdf_gate_text.pdf")
		doc := gateOpenStream(t, raw)
		const sentinel = "Harriet Volkmer"
		if _, err := doc.ReplaceText(sentinel, "[PERSON_1]"); err != nil {
			t.Fatalf("ReplaceText: %v", err)
		}
		var buf bytes.Buffer
		if _, err := doc.WriteTo(&buf); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		findings, _, err := ScanPDFForNeedles(buf.Bytes(), []string{sentinel})
		if err != nil {
			t.Fatalf("ScanPDFForNeedles: %v", err)
		}
		if len(findings) == 0 {
			t.Error("the library no longer retains orphaned objects across a naked WriteTo; RemoveUnusedObjects() may have become redundant, re-run the gate's save measurements before relying on that")
		}
	})
}

// --- G5: round-trip fidelity -------------------------------------------------

func TestPDFFossGateRoundTrip(t *testing.T) {
	for _, name := range []string{"pdf_gate_text.pdf", "pdf_gate_surfaces.pdf", "pdf_gate_images.pdf"} {
		t.Run("roundtrip/no_edit_writeto_preserves_"+name, func(t *testing.T) {
			raw := gateFixture(t, name)
			before := gateOpenStream(t, raw)
			out := gateWrite(t, before)
			after := gateOpenStream(t, out)

			if got, want := after.PageCount(), before.PageCount(); got != want {
				t.Errorf("page count %d after round trip, want %d", got, want)
			}

			beforeText, err := before.ExtractText()
			if err != nil {
				t.Fatalf("ExtractText(before): %v", err)
			}
			afterText, err := after.ExtractText()
			if err != nil {
				t.Fatalf("ExtractText(after): %v", err)
			}
			for i := range beforeText {
				if i >= len(afterText) || collapseSpaces(afterText[i]) != collapseSpaces(beforeText[i]) {
					t.Errorf("page %d text changed across a no-edit round trip.\nbefore: %q\nafter:  %q", i+1, beforeText[i], afterText[i])
				}
			}

			beforeImgs, err := before.ImageInfos()
			if err != nil {
				t.Fatalf("ImageInfos(before): %v", err)
			}
			afterImgs, err := after.ImageInfos()
			if err != nil {
				t.Fatalf("ImageInfos(after): %v", err)
			}
			for i := range beforeImgs {
				if i >= len(afterImgs) || len(afterImgs[i]) != len(beforeImgs[i]) {
					t.Errorf("page %d image inventory changed across a no-edit round trip: %d before, %d after", i+1, len(beforeImgs[i]), len(afterImgs[i]))
				}
			}

			// Rasterised difference, page by page, counted per pixel. The
			// count is a G5 measurement: zero is the hope, and anything else
			// is enumerated in the GO/NO-GO note and judged there.
			for p := 1; p <= before.PageCount(); p++ {
				diff := countPixelDiff(gateRender(t, before, p), gateRender(t, after, p), nil)
				t.Logf("G5 %s page %d: %d differing pixels across a no-edit round trip", name, p, diff)
				if diff != 0 {
					t.Errorf("G5 %s page %d: %d pixels differ across a no-edit round trip (recorded; judged in the GO/NO-GO note if cosmetic)", name, p, diff)
				}
			}
		})
	}
}

// --- G6: ReplaceText behaviour ------------------------------------------------

func TestPDFFossGateReplaceText(t *testing.T) {
	t.Run("redaction/longer_replacement_stays_inside_its_own_line", func(t *testing.T) {
		raw := gateFixture(t, "pdf_gate_text.pdf")
		doc := gateOpenStream(t, raw)

		// The 9 pt value: a short original replaced by a much longer
		// placeholder, which is the shape every real replacement has.
		const original = "Quentin Marsh"
		const placeholder = "[PERSON_2_PLACEHOLDER]"

		matches, err := doc.SearchText(original)
		if err != nil {
			t.Fatalf("SearchText(%q): %v", original, err)
		}
		if len(matches) != 1 {
			t.Fatalf("SearchText(%q) found %d matches, the fixture plants exactly 1", original, len(matches))
		}
		origRect := matches[0].Rect
		beforeImg := gateRender(t, doc, matches[0].PageNumber)

		if _, err := doc.ReplaceText(original, placeholder); err != nil {
			t.Fatalf("ReplaceText: %v", err)
		}
		out := gateWrite(t, doc)

		// Original absent everywhere, placeholder present in extraction.
		findings, _, err := ScanPDFForNeedles(out, []string{original})
		if err != nil {
			t.Fatalf("ScanPDFForNeedles: %v", err)
		}
		if len(findings) != 0 {
			t.Errorf("the original survives ReplaceText: %+v", findings)
		}
		reopened := gateOpenStream(t, out)
		texts, err := reopened.ExtractText()
		if err != nil {
			t.Fatalf("ExtractText(reopened): %v", err)
		}
		if !strings.Contains(strings.Join(texts, "\n"), placeholder) {
			t.Errorf("the placeholder %q is not extractable from the replaced output", placeholder)
		}

		// Where did the longer text go? The recorded G6 observation: the
		// placeholder's own rectangle, beside the original's.
		newMatches, err := reopened.SearchText(placeholder)
		if err != nil || len(newMatches) != 1 {
			t.Fatalf("SearchText(placeholder) after replacement: %d matches, err %v", len(newMatches), err)
		}
		newRect := newMatches[0].Rect
		t.Logf("G6 observation: original rect (%.1f,%.1f)-(%.1f,%.1f), replacement rect (%.1f,%.1f)-(%.1f,%.1f); width %.1f pt -> %.1f pt",
			origRect.LLX, origRect.LLY, origRect.URX, origRect.URY,
			newRect.LLX, newRect.LLY, newRect.URX, newRect.URY,
			origRect.URX-origRect.LLX, newRect.URX-newRect.LLX)

		// Pixels outside the union of the two rectangles (inflated 3 px for
		// antialiasing bleed) must be untouched: a longer replacement may
		// grow into empty space, it may not repaint a neighbour.
		afterImg := gateRender(t, reopened, matches[0].PageNumber)
		pageH := pageHeightPt(t, doc, matches[0].PageNumber)
		allowed := unionRect(origRect, newRect)
		outside := countPixelDiff(beforeImg, afterImg, &pixelMask{rect: allowed, pageH: pageH, margin: 3})
		if outside != 0 {
			t.Errorf("G6: %d pixels changed OUTSIDE the replaced region; a silent overlap of a neighbour is a NO-GO for ladder rung 1", outside)
		}
	})
}

// --- rung 2: redact and redraw -----------------------------------------------

func TestPDFFossGateRedactAndRedraw(t *testing.T) {
	t.Run("redaction/redact_then_addtext_removes_the_original", func(t *testing.T) {
		raw := gateFixture(t, "pdf_gate_text.pdf")
		doc := gateOpenStream(t, raw)

		const original = "Jean-Baptiste Ferrand"
		const placeholder = "[PERSON_3]"
		matches, err := doc.SearchText(original)
		if err != nil {
			t.Fatalf("SearchText(%q): %v", original, err)
		}
		if len(matches) != 1 {
			t.Fatalf("SearchText(%q) found %d matches, want 1", original, len(matches))
		}
		m := matches[0]
		beforePNG := renderPNG(t, doc, m.PageNumber)

		// A redact annotation is built unbound and applies only once ADDED to
		// its page's annotation collection; ApplyRedactions silently does
		// nothing for one that was only constructed. 13c must keep both
		// halves of that gesture together.
		//
		// The placeholder is drawn through the annotation's own OverlayText,
		// not a separate AddText: the redaction fills its rectangle black, so
		// a black AddText on top is extractable but INVISIBLE (measured), and
		// the overlay defaults to a contrasting colour and is applied by
		// ApplyRedactions in the same gesture.
		page := doc.Pages()[m.PageNumber-1]
		redact := asposepdf.NewRedactAnnotation(page, m.Rect)
		redact.SetOverlayText(placeholder)
		// The colour is set EXPLICITLY: the apply path draws the overlay in
		// black unless told otherwise (only the mark-mode preview defaults to
		// a contrasting colour), and black-on-black is extractable but
		// invisible (measured).
		redact.SetOverlayTextStyle(asposepdf.TextStyle{Size: 10, Color: &asposepdf.Color{R: 1, G: 1, B: 1, A: 1}})
		if err := page.Annotations().Add(redact); err != nil {
			t.Fatalf("Annotations().Add(redact): %v", err)
		}
		if err := doc.ApplyRedactions(); err != nil {
			t.Fatalf("ApplyRedactions: %v", err)
		}
		out := gateWrite(t, doc)

		findings, _, err := ScanPDFForNeedles(out, []string{original})
		if err != nil {
			t.Fatalf("ScanPDFForNeedles: %v", err)
		}
		if len(findings) != 0 {
			t.Errorf("the original survives redact-and-redraw: %+v (ApplyRedactions must remove the text, not cover it)", findings)
		}
		reopened := gateOpenStream(t, out)
		texts, err := reopened.ExtractText()
		if err != nil {
			t.Fatalf("ExtractText(reopened): %v", err)
		}
		joined := strings.Join(texts, "\n")
		if !strings.Contains(joined, placeholder) {
			t.Errorf("the redrawn placeholder %q is not extractable", placeholder)
		}
		// The neighbours on the same page survive.
		for _, neighbour := range []string{"Societe Miradour", "Luxembourg"} {
			if !strings.Contains(joined, neighbour) {
				t.Errorf("neighbour %q was destroyed by the redaction of %q", neighbour, original)
			}
		}

		// The eyeball artefacts for the owner (OQ2's evidence): committed
		// golden files the GO/NO-GO note points at.
		writeGolden(t, "pdf_gate_redact_before.png", beforePNG)
		writeGolden(t, "pdf_gate_redact_after.png", renderPNG(t, reopened, m.PageNumber))
	})
}

// --- the wrapped value (G7's hardest rung, prototyped) -------------------------

func TestPDFFossGateWrappedValue(t *testing.T) {
	t.Run("redaction/wrapped_value_located_by_fragments_and_redacted", func(t *testing.T) {
		raw := gateFixture(t, "pdf_gate_text.pdf")
		doc := gateOpenStream(t, raw)

		// The documented single-line limit, confirmed rather than assumed:
		// the wrapped value as a whole is NOT findable.
		wholeMatches, err := doc.SearchText("Victor Beaulieu")
		if err != nil {
			t.Fatalf("SearchText(wrapped whole): %v", err)
		}
		if len(wholeMatches) != 0 {
			t.Logf("unexpected: SearchText found the line-wrapped value whole (%d matches); the documented limit has moved, 13c's ladder can simplify", len(wholeMatches))
		}

		// The 13c mechanism, prototyped: split at whitespace, locate the head
		// at one line's end and the tail at the next line's start, each a
		// single-line search, then redact both fragments and draw the
		// placeholder over the head.
		headMatches, err := doc.SearchText("Victor")
		if err != nil || len(headMatches) != 1 {
			t.Fatalf("SearchText(head) = %d matches, err %v; want exactly 1", len(headMatches), err)
		}
		tailMatches, err := doc.SearchText("Beaulieu")
		if err != nil || len(tailMatches) != 1 {
			t.Fatalf("SearchText(tail) = %d matches, err %v; want exactly 1", len(tailMatches), err)
		}
		head, tail := headMatches[0], tailMatches[0]
		if head.PageNumber != tail.PageNumber {
			t.Fatalf("head on page %d, tail on page %d; the fixture wraps within one page", head.PageNumber, tail.PageNumber)
		}
		// The geometry sanity check G7 needs: the tail sits on the line BELOW
		// the head (smaller Y in PDF space).
		if !(tail.Rect.URY < head.Rect.LLY) {
			t.Errorf("fragment geometry is not two stacked lines: head (%.1f-%.1f), tail (%.1f-%.1f); the wrapped-match step cannot trust the layout", head.Rect.LLY, head.Rect.URY, tail.Rect.LLY, tail.Rect.URY)
		}

		page := doc.Pages()[head.PageNumber-1]
		// The placeholder rides the HEAD fragment's redaction as overlay
		// text; the tail's box stays blank (one placeholder per value, drawn
		// over the first fragment, per D5's wrapped-match step).
		headRedact := asposepdf.NewRedactAnnotation(page, head.Rect)
		headRedact.SetOverlayText("[PERSON_4]")
		headRedact.SetOverlayTextStyle(asposepdf.TextStyle{Size: 10, Color: &asposepdf.Color{R: 1, G: 1, B: 1, A: 1}})
		if err := page.Annotations().Add(headRedact); err != nil {
			t.Fatalf("Annotations().Add(head redact): %v", err)
		}
		if err := page.Annotations().Add(asposepdf.NewRedactAnnotation(page, tail.Rect)); err != nil {
			t.Fatalf("Annotations().Add(tail redact): %v", err)
		}
		if err := doc.ApplyRedactions(); err != nil {
			t.Fatalf("ApplyRedactions(two fragments): %v", err)
		}
		out := gateWrite(t, doc)

		// The scanner's concatenated view is what would catch the wrapped
		// value leaking; it must be clean now.
		findings, _, err := ScanPDFForNeedles(out, []string{"Victor Beaulieu", "Victor", "Beaulieu"})
		if err != nil {
			t.Fatalf("ScanPDFForNeedles: %v", err)
		}
		if len(findings) != 0 {
			t.Errorf("the wrapped value survives fragment redaction: %+v", findings)
		}
		// The rest of both lines survives.
		reopened := gateOpenStream(t, out)
		texts, err := reopened.ExtractText()
		if err != nil {
			t.Fatalf("ExtractText: %v", err)
		}
		joined := strings.Join(texts, "\n")
		for _, neighbour := range []string{"The renewal was approved by", "on behalf of the supervisory board"} {
			if !strings.Contains(joined, neighbour) {
				t.Errorf("neighbour text %q was destroyed by the fragment redaction", neighbour)
			}
		}
	})
}

// --- images (step 7.6, feeding D9 and 13d) -------------------------------------

func TestPDFFossGateImages(t *testing.T) {
	raw := gateFixture(t, "pdf_gate_images.pdf")

	t.Run("roundtrip/inventory_lists_the_placed_images", func(t *testing.T) {
		doc := gateOpenStream(t, raw)
		infos, err := doc.ImageInfos()
		if err != nil {
			t.Fatalf("ImageInfos: %v", err)
		}
		if len(infos) != 2 {
			t.Fatalf("ImageInfos returned %d pages, the fixture has 2", len(infos))
		}
		// Page 1 places the JPEG and the flate raster; page 2 places the
		// SAME JPEG object again plus an inline image. What the library
		// lists per page is the D9 evidence the GO/NO-GO note records.
		inline := 0
		for p, page := range infos {
			for _, info := range page {
				if info.Inline {
					inline++
				}
				t.Logf("D9 evidence: page %d image %s %dx%d inline=%v format=%v", p+1, info.Name, info.Width, info.Height, info.Inline, info.Format)
			}
		}
		if len(infos[0]) != 2 {
			t.Errorf("page 1 lists %d images, the fixture places 2 XObjects", len(infos[0]))
		}
		if len(infos[1]) < 1 {
			t.Errorf("page 2 lists %d images, the fixture places at least the shared JPEG", len(infos[1]))
		}
		t.Logf("D9 evidence: inline images listed: %d (the fixture draws 1); the shared JPEG object is listed once per PLACEMENT (%d+%d), so asset identity must come from a content hash, as D9 decides", inline, len(infos[0]), len(infos[1]))

		// The shared object decodes to identical bytes from both placements:
		// the content hash D9 builds on is stable across placements.
		j1 := extractNamed(t, infos[0], "/ImJ")
		j2 := extractNamed(t, infos[1], "/ImJ")
		if !bytes.Equal(j1.Data, j2.Data) {
			t.Errorf("the same XObject decodes to different bytes from two placements (%d vs %d bytes); a content-hash asset ID would split one picture into two rows", len(j1.Data), len(j2.Data))
		}
	})

	t.Run("redaction/treated_bytes_replace_the_original_pixels", func(t *testing.T) {
		doc := gateOpenStream(t, raw)
		infos, err := doc.ImageInfos()
		if err != nil {
			t.Fatalf("ImageInfos: %v", err)
		}
		jpegInfo := extractInfoNamed(t, infos[0], "/ImJ")
		img, err := jpegInfo.Extract()
		if err != nil {
			t.Fatalf("ImageInfo.Extract: %v", err)
		}

		// The existing treatment pipeline, unchanged, over the extracted
		// bytes: exactly what 13d will wire.
		asset := imaging.Asset{ID: "pdf:gate-jpeg", Name: "photograph", Format: imaging.Sniff(img.Data), Bytes: len(img.Data), Width: img.Width, Height: img.Height}
		treated, err := imaging.Treat(img.Data, asset, imaging.Decision{Treatment: imaging.TreatmentBox, BoxText: "Removed"})
		if err != nil {
			t.Fatalf("imaging.Treat(box) over the extracted image: %v", err)
		}
		if err := jpegInfo.ReplaceFromStream(bytes.NewReader(treated)); err != nil {
			t.Fatalf("ReplaceFromStream(treated): %v", err)
		}

		// remove the flate raster entirely: the placement AND the bytes go.
		pngInfo := extractInfoNamed(t, infos[0], "/ImP")
		if err := pngInfo.Remove(); err != nil {
			t.Fatalf("ImageInfo.Remove: %v", err)
		}
		out := gateWrite(t, doc)

		// Invariant 1: the ORIGINAL pixel bytes leave the file. The probe is
		// a 64-byte slice from the middle of each original encoded stream,
		// searched in the produced bytes.
		if chunk := middleChunk(t, streamChunkAfter(t, raw, []byte("/DCTDecode"))); bytes.Contains(out, chunk) {
			t.Error("the original JPEG stream bytes survive in the produced file after ReplaceFromStream; invariant 1 (original pixels always leave) fails")
		}
		reopened := gateOpenStream(t, out)
		afterInfos, err := reopened.ImageInfos()
		if err != nil {
			t.Fatalf("ImageInfos(reopened): %v", err)
		}
		if got := len(afterInfos[0]); got != 1 {
			t.Errorf("page 1 lists %d images after one replace and one remove, want 1", got)
		}
		afterImg, err := extractInfoNamed(t, afterInfos[0], "/ImJ").Extract()
		if err != nil {
			t.Fatalf("Extract(replaced image): %v", err)
		}
		if bytes.Equal(afterImg.Data, img.Data) {
			t.Error("the replaced image decodes to the ORIGINAL bytes; the treatment did not reach the file")
		}
		t.Logf("images: box-treated JPEG went from %d to %d bytes in place; flate raster removed (placement and stream)", len(img.Data), len(afterImg.Data))
	})
}

// --- metadata (step 7.7) --------------------------------------------------------

func TestPDFFossGateMetadata(t *testing.T) {
	t.Run("roundtrip/info_and_xmp_read_through_the_library", func(t *testing.T) {
		raw := gateFixture(t, "pdf_gate_surfaces.pdf")

		incumbent, err := ExtractPDFMetadata(raw)
		if err != nil {
			t.Fatalf("ExtractPDFMetadata: %v", err)
		}
		byName := map[string]string{}
		for _, f := range incumbent {
			byName[f.Name] = f.Value
		}

		doc := gateOpenStream(t, raw)
		info, err := doc.Info()
		if err != nil {
			t.Fatalf("Document.Info: %v", err)
		}
		if info.Title != byName["Title"] || info.Author != byName["Author"] {
			t.Errorf("Info parity: library Title=%q Author=%q, incumbent Title=%q Author=%q", info.Title, info.Author, byName["Title"], byName["Author"])
		}

		xmp, err := doc.XMPRaw()
		if err != nil {
			t.Fatalf("Document.XMPRaw: %v", err)
		}
		if !bytes.Contains(xmp, []byte("Nadia Okonkwo")) {
			t.Error("the XMP packet's planted creator is not readable through XMPRaw; the metadata review cannot cover XMP through this library")
		}
	})
}

// --- G9: failure behaviour ------------------------------------------------------

func TestPDFFossGateFailures(t *testing.T) {
	t.Run("errors/damaged_file_never_panics_and_never_invents_content", func(t *testing.T) {
		raw := gateFixture(t, "pdf_gate_text.pdf")

		// Truncation at several depths. The measured behaviour (recorded for
		// 13c): the library RECONSTRUCTS the cross-reference table and
		// salvage-opens with the pages still intact in the bytes, rather than
		// erroring. That is tolerance, not a defect, but it must never panic
		// and must never report MORE pages than the original had; and a file
		// cut to almost nothing must come back with zero extractable pages,
		// so the textless refusal downstream still has something to fire on.
		for _, tenths := range []int{9, 6, 3, 1} {
			truncated := raw[:len(raw)*tenths/10]
			var panicked interface{}
			var doc *asposepdf.Document
			var err error
			func() {
				defer func() { panicked = recover() }()
				doc, err = asposepdf.OpenStream(bytes.NewReader(truncated))
			}()
			if panicked != nil {
				t.Fatalf("OpenStream PANICKED on a file truncated to %d0%%: %v. 13c needs the same recover shield convert/pdf.go carries today", tenths, panicked)
			}
			if err != nil {
				t.Logf("G9 truncation at %d0%%: error (recorded): %v", tenths, err)
				continue
			}
			if got := doc.PageCount(); got > 3 {
				t.Errorf("G9 truncation at %d0%%: salvage-opened with %d pages, MORE than the original's 3; invented content is worse than a refusal", tenths, got)
			} else {
				t.Logf("G9 truncation at %d0%%: salvage-opened with %d of 3 pages (recorded; 13c's import inherits this tolerance)", tenths, got)
			}
		}

		// A file that is not a PDF at all must error, actionably.
		_, err := asposepdf.OpenStream(bytes.NewReader([]byte("this is not a pdf at all, just prose long enough to look like a file")))
		if err == nil {
			t.Fatal("OpenStream accepted plain prose as a PDF; a damaged file must error actionably")
		}
		t.Logf("G9 non-PDF error (recorded for 13c's message wrapping): %v", err)
	})

	t.Run("errors/encrypted_file_is_distinguishable", func(t *testing.T) {
		// The encrypted fixture is produced in-test with the library itself:
		// open the committed fixture, set a password, write.
		doc := gateOpenStream(t, gateFixture(t, "pdf_gate_text.pdf"))
		doc.SetPassword("les-mots-de-passe", "les-mots-de-passe")
		encrypted := gateWrite(t, doc)

		_, err := asposepdf.OpenStream(bytes.NewReader(encrypted))
		if err == nil {
			t.Fatal("OpenStream opened a password-protected file without the password")
		}
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "password") && !strings.Contains(msg, "encrypt") {
			t.Errorf("the encrypted-file error %q does not name the password/encryption, so the import refusal cannot say \"remove the password first\"", err)
		}
		t.Logf("G9 encrypted-file error (recorded for 13c's message wrapping): %v", err)

		// And WITH the password, the same bytes open: the distinguishability
		// is real, not a generic parse failure.
		if _, err := asposepdf.OpenStreamWithPassword(bytes.NewReader(encrypted), "les-mots-de-passe"); err != nil {
			t.Errorf("OpenStreamWithPassword with the correct password failed: %v", err)
		}
	})
}

// --- helpers ---------------------------------------------------------------------

// extractInfoNamed finds the page image with the given XObject name.
func extractInfoNamed(t *testing.T, infos []asposepdf.ImageInfo, name string) *asposepdf.ImageInfo {
	t.Helper()
	for i := range infos {
		if infos[i].Name == name {
			return &infos[i]
		}
	}
	t.Fatalf("no image named %s on the page; names present: %v", name, imageNames(infos))
	return nil
}

// extractNamed extracts the decoded image with the given XObject name.
func extractNamed(t *testing.T, infos []asposepdf.ImageInfo, name string) *asposepdf.Image {
	t.Helper()
	img, err := extractInfoNamed(t, infos, name).Extract()
	if err != nil {
		t.Fatalf("Extract(%s): %v", name, err)
	}
	return img
}

func imageNames(infos []asposepdf.ImageInfo) []string {
	var out []string
	for _, i := range infos {
		out = append(out, i.Name)
	}
	return out
}

// streamChunkAfter returns the stream payload of the first object whose
// dictionary carries the marker (raw fixture bytes, no decoding).
func streamChunkAfter(t *testing.T, raw, marker []byte) []byte {
	t.Helper()
	at := bytes.Index(raw, marker)
	if at < 0 {
		t.Fatalf("marker %q not found in the fixture", marker)
	}
	rest := raw[at:]
	start := bytes.Index(rest, []byte("stream\n"))
	if start < 0 {
		t.Fatalf("no stream after marker %q", marker)
	}
	rest = rest[start+len("stream\n"):]
	end := bytes.Index(rest, []byte("endstream"))
	if end < 0 {
		t.Fatalf("no endstream after marker %q", marker)
	}
	return rest[:end]
}

// middleChunk returns 64 bytes from the middle of data: long enough to be
// unique, short enough to survive any re-framing of the container around it.
func middleChunk(t *testing.T, data []byte) []byte {
	t.Helper()
	if len(data) < 128 {
		t.Fatalf("stream too short (%d bytes) for a distinctive middle chunk", len(data))
	}
	mid := len(data) / 2
	return data[mid : mid+64]
}

// pixelMask exempts one PDF-space rectangle (inflated by margin pixels) from
// a pixel comparison, converting PDF bottom-left coordinates to image
// top-left rows at 72 DPI.
type pixelMask struct {
	rect   asposepdf.Rectangle
	pageH  float64
	margin int
}

func (m *pixelMask) excluded(x, y int) bool {
	if m == nil {
		return false
	}
	top := int(m.pageH-m.rect.URY) - m.margin
	bottom := int(m.pageH-m.rect.LLY) + m.margin
	left := int(m.rect.LLX) - m.margin
	right := int(m.rect.URX) + m.margin
	return x >= left && x <= right && y >= top && y <= bottom
}

// countPixelDiff counts pixels that differ between two renders, skipping the
// masked region when a mask is given.
func countPixelDiff(a, b image.Image, mask *pixelMask) int {
	bounds := a.Bounds()
	diff := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if mask.excluded(x, y) {
				continue
			}
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb || aa != ba {
				diff++
			}
		}
	}
	return diff
}

// unionRect is the smallest rectangle containing both.
func unionRect(a, b asposepdf.Rectangle) asposepdf.Rectangle {
	r := a
	if b.LLX < r.LLX {
		r.LLX = b.LLX
	}
	if b.LLY < r.LLY {
		r.LLY = b.LLY
	}
	if b.URX > r.URX {
		r.URX = b.URX
	}
	if b.URY > r.URY {
		r.URY = b.URY
	}
	return r
}

// pageHeightPt reads a page's height in points.
func pageHeightPt(t *testing.T, doc *asposepdf.Document, pageNum int) float64 {
	t.Helper()
	box, err := doc.Pages()[pageNum-1].MediaBox()
	if err != nil {
		t.Fatalf("MediaBox(page %d): %v", pageNum, err)
	}
	return box.URY - box.LLY
}

// renderPNG rasterises a page to PNG bytes.
func renderPNG(t *testing.T, doc *asposepdf.Document, pageNum int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, gateRender(t, doc, pageNum)); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// writeGolden commits an eyeball artefact under testdata/golden/, the
// repository's home for regenerable committed outputs.
func writeGolden(t *testing.T, name string, data []byte) {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "golden")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing the eyeball artefact %s: %v", path, err)
	}
	t.Logf("eyeball artefact written: %s (%d bytes)", path, len(data))
}
