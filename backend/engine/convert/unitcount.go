// engine/convert/unitcount.go — the import list's "size in the document's own
// terms" (BUILD-05 Phase 3, decision 5).
//
// A byte size tells a user almost nothing about whether they picked the right
// file. "6 pages" or "12 slides" does. So each converter that can name a unit
// gets a counter here, and engine/document.go falls back to counting lines for
// everything else.
//
// The three counters differ in how much they can be trusted, and the
// difference is the whole reason they are separate functions rather than one
// switch:
//
//	PptxSlides  Derived from the archive's SHAPE (how many slideN.xml parts
//	            there are). ALWAYS correct.
//	PDFPages    From the PDF's own page tree. Always correct.
//	DocxPages   Read from what the last writing application CACHED. Correct
//	            when Word wrote the file, potentially stale otherwise, and
//	            absent entirely for files written by minimal tooling. Zero
//	            means "no answer", and the caller falls back to lines.
//
// None of these returns an error. A count is a nicety on an import list, so a
// file whose properties cannot be read reports no count and the import still
// succeeds; refusing an importable document because its page count was
// unreadable would be indefensible.
package convert

import (
	"bytes"

	pdflib "github.com/ledongthuc/pdf"

	"doc-anonymiser/backend/engine/ooxml"
)

// DocxPages returns the page count a .docx caches in docProps/app.xml, or 0
// when the file does not carry one.
//
// Zero is the COMMON answer, not an edge case: the part is optional and the
// repository's own backend/testdata/report.docx has no docProps/app.xml at all.
// See ooxml.AppProps for the staleness caveat that comes with a non-zero value.
//
// @param raw the whole .docx, already in memory
// @return the cached page count, or 0 when unavailable
func DocxPages(raw []byte) int {
	props, err := ooxml.ReadAppProps(raw)
	if err != nil {
		// The document already imported successfully, so the archive is
		// readable; a properties part that will not parse is not worth failing
		// the import over. No count, fall back to lines.
		return 0
	}
	return props.Pages
}

// PptxSlides returns how many slides a .pptx has.
//
// It counts slide PARTS and only cross-checks the cached <Slides> property,
// because the part count is derived from the archive's shape and is therefore
// always right, while the cached value is whatever the last writing
// application computed. When they disagree the part count wins; the cached
// value is only consulted when there are somehow no parts to count.
//
// @param raw the whole .pptx, already in memory
// @return the slide count, or 0 when neither source has an answer
func PptxSlides(raw []byte) int {
	parts, err := ooxml.CountEntries(raw, func(name string) bool {
		return slideEntryRe.MatchString(name)
	})
	if err == nil && parts > 0 {
		return parts
	}
	props, err := ooxml.ReadAppProps(raw)
	if err != nil {
		return 0
	}
	return props.Slides
}

// PDFPages returns a PDF's page count.
//
// It re-opens the archive rather than having PDF() return the figure, so the
// converter's signature stays "bytes in, markdown and warnings out". The cost
// is one extra parse of an in-memory buffer, once per imported file.
//
// The recover mirrors PDF()'s: ledongthuc/pdf was written for well-formed input
// and can panic on damaged files. A panic here must cost the user a page count,
// never the import.
//
// @param raw the whole .pdf, already in memory
// @return the page count, or 0 when the file cannot be read
func PDFPages(raw []byte) (pages int) {
	defer func() {
		if r := recover(); r != nil {
			pages = 0
		}
	}()
	reader, err := pdflib.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return 0
	}
	n := reader.NumPage()
	if n < 0 {
		return 0
	}
	return n
}
