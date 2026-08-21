// engine/exportfmt/pdfscan_test.go — the leak scanner's proof.
//
// The scanner is only worth trusting if it demonstrably finds a needle in
// EVERY surface a PDF can hide one in, so these tests plant one and watch it
// found: in each surface of the committed pdf_gate_surfaces.pdf fixture
// (content stream, Info dictionary, XMP packet, annotation, outline title),
// in every string encoding a PDF can carry (escaped literal, octal, hex,
// UTF-16BE, a value split across Tj segments, a flate-compressed stream), and
// in a hand-appended incremental-update body. The clean case proves it does
// not cry wolf.
//
// White-box (package exportfmt): the encoding decoders are the subject.
package exportfmt

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gateFixture reads a committed backend/testdata fixture, skipping with the
// regeneration command if it has not been generated yet (the generator lives
// in the convert package's integration tier).
func gateFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture %s is missing (%v); generate it once with: go test -tags=integration ./backend/engine/convert/", path, err)
	}
	return data
}

// surfacesNeedle is the name buildPDFGateSurfaces plants in every surface.
const surfacesNeedle = "Nadia Okonkwo"

func TestScanPDFFindsNeedleInEverySurface(t *testing.T) {
	raw := gateFixture(t, "pdf_gate_surfaces.pdf")
	findings, unscannable, err := ScanPDFForNeedles(raw, []string{surfacesNeedle})
	if err != nil {
		t.Fatalf("ScanPDFForNeedles(pdf_gate_surfaces.pdf, %q): %v", surfacesNeedle, err)
	}
	got := map[string]bool{}
	for _, f := range findings {
		got[f.Surface] = true
	}
	for _, want := range []string{
		"a content stream",
		"the Info dictionary",
		"the XMP metadata packet",
		"an annotation",
		"an outline item",
	} {
		if !got[want] {
			t.Errorf("the planted needle was not found in %s; surfaces found: %v (a surface the scan misses is a surface a value can leak through)", want, keys(got))
		}
	}
	// The fixture's only stream filters are FlateDecode (the thumbnail) and
	// none; nothing should be reported unscannable.
	if len(unscannable) != 0 {
		t.Errorf("unexpected unscannable surfaces in the fixture: %v", unscannable)
	}
}

func TestScanPDFFindsNothingWhenNothingIsPlanted(t *testing.T) {
	raw := gateFixture(t, "pdf_gate_surfaces.pdf")
	findings, _, err := ScanPDFForNeedles(raw, []string{"Wilhelmina Craddock"})
	if err != nil {
		t.Fatalf("ScanPDFForNeedles(clean needle): %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("a needle that is not in the file was reported found: %+v (a scan that cries wolf gets deleted, which is how the leak comes back)", findings)
	}
}

func TestScanPDFFindsNeedleInAppendedIncrementalBody(t *testing.T) {
	base := gateFixture(t, "pdf_gate_text.pdf")

	// Hand-append the shape an incremental save produces: a superseded
	// object generation, a new xref section chained with /Prev, a second
	// %%EOF. The appended object carries a needle the live body does not.
	var incr bytes.Buffer
	incr.Write(base)
	appendedAt := incr.Len()
	fmt.Fprintf(&incr, "99 0 obj\n<< /Type /Annot /Subtype /Text /Contents (Left by Casimir Delatour) >>\nendobj\n")
	fmt.Fprintf(&incr, "xref\n99 1\n%010d 00000 n \ntrailer\n<< /Size 100 /Root 1 0 R /Prev 42 >>\nstartxref\n%d\n%%%%EOF\n", appendedAt, appendedAt)

	findings, _, err := ScanPDFForNeedles(incr.Bytes(), []string{"Casimir Delatour"})
	if err != nil {
		t.Fatalf("ScanPDFForNeedles(appended body): %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("a needle in an appended incremental body was not found; superseded objects are exactly the leak the raw-byte walk exists to catch")
	}
	if !strings.Contains(findings[0].Surface, "appended incremental body") {
		t.Errorf("the finding's surface is %q, want it to name the appended incremental body so the refusal can", findings[0].Surface)
	}
	if !PDFHasIncrementalUpdate(incr.Bytes()) {
		t.Error("PDFHasIncrementalUpdate is false on a file with two bodies and a /Prev chain")
	}
	if PDFHasIncrementalUpdate(base) {
		t.Error("PDFHasIncrementalUpdate is true on the single-body fixture; the save-semantics proof would then always fail")
	}
	if got := PDFBodyCount(incr.Bytes()); got != 2 {
		t.Errorf("PDFBodyCount(appended) = %d, want 2 (input: the fixture plus one appended body)", got)
	}
}

// TestScanPDFStringEncodings proves each decoder on a minimal hand-built
// object, one encoding per case: the encodings are the unit under test, so
// they get their own table rather than riding on the fixture.
func TestScanPDFStringEncodings(t *testing.T) {
	// wrap makes a one-object PDF-shaped byte string around a body.
	wrap := func(body string) []byte {
		return []byte("%PDF-1.5\n1 0 obj\n" + body + "\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF\n")
	}
	// flateStream builds a stream object whose payload is deflated.
	flateStream := func(payload string) []byte {
		var z bytes.Buffer
		zw := zlib.NewWriter(&z)
		_, _ = zw.Write([]byte(payload))
		_ = zw.Close()
		head := fmt.Sprintf("%%PDF-1.5\n1 0 obj\n<< /Filter /FlateDecode /Length %d >>\nstream\n", z.Len())
		return append(append([]byte(head), z.Bytes()...), []byte("\nendstream\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF\n")...)
	}
	// utf16Hex encodes text as a UTF-16BE hex string with the BOM.
	utf16Hex := func(text string) string {
		var sb strings.Builder
		sb.WriteString("<FEFF")
		for _, r := range text {
			fmt.Fprintf(&sb, "%04X", r)
		}
		sb.WriteString(">")
		return sb.String()
	}

	cases := []struct {
		name   string
		pdf    []byte
		needle string
	}{
		{"escaped_literal", wrap(`<< /T (Har\(riet\) Volkmer) >>`), "Har(riet) Volkmer"},
		{"octal_escape", wrap(`<< /T (Volkm\145r) >>`), "Volkmer"}, // \145 = e
		{"hex_string", wrap(`<< /T <4861727269657420566F6C6B6D6572> >>`), "Harriet Volkmer"},
		{"utf16be_string", wrap(`<< /T ` + utf16Hex("Amélie Lefèvre") + ` >>`), "Amélie Lefèvre"},
		{"split_tj_segments", wrap("<< /Length 44 >>\nstream\nBT (Har) Tj 8 0 Td (riet Volkmer) Tj ET\nendstream"), "Harriet Volkmer"},
		{"flate_compressed_content", flateStream("BT /F1 12 Tf (Quentin Marsh) Tj ET"), "Quentin Marsh"},
		{"line_continuation", wrap("<< /T (Quentin \\\nMarsh) >>"), "Quentin Marsh"},
	}
	for _, tc := range cases {
		t.Run("redaction/"+tc.name, func(t *testing.T) {
			findings, _, err := ScanPDFForNeedles(tc.pdf, []string{tc.needle})
			if err != nil {
				t.Fatalf("ScanPDFForNeedles(%s): %v", tc.name, err)
			}
			if len(findings) == 0 {
				t.Errorf("needle %q not found in the %s case; input:\n%s", tc.needle, tc.name, tc.pdf)
			}
		})
	}
}

// TestScanPDFReportsUnscannableFilters: a stream under an image codec is
// scanned raw and NAMED, never silently treated as covered.
func TestScanPDFReportsUnscannableFilters(t *testing.T) {
	pdf := []byte("%PDF-1.5\n1 0 obj\n<< /Subtype /Image /Filter /DCTDecode /Length 4 >>\nstream\n\xff\xd8\xff\xd9\nendstream\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF\n")
	_, unscannable, err := ScanPDFForNeedles(pdf, []string{"anything"})
	if err != nil {
		t.Fatalf("ScanPDFForNeedles(DCTDecode): %v", err)
	}
	if len(unscannable) != 1 || !strings.Contains(unscannable[0], "DCTDecode") {
		t.Errorf("a DCTDecode stream must be reported unscannable by filter name; got %v", unscannable)
	}
}

// keys lists a set's members for a failure message.
func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
