// engine/pagescope_test.go — the page/segment slicing that scopes the local-model
// scan (CLAUDE.md §5).
//
// These tests pin the two invariants that make the feature trustworthy:
//   - PageCount matches the document's own unit (lines/rows/slides/pages) and
//     is never below 1, so the UI never offers an empty range;
//   - PageRangeMarkdown returns exactly the requested sub-units, rejects a
//     range outside 1..PageCount, and — asked for the whole document — covers
//     every unit, so a scoped scan can never silently drop content.
package engine

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestPageScopeLines(t *testing.T) {
	doc, err := Load("notes.txt", []byte("alpha\nbravo\ncharlie\ndelta\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := doc.PageCount(); got != 4 {
		t.Fatalf("PageCount = %d, want 4 (one per line, trailing newline excluded)", got)
	}

	got, err := doc.PageRangeMarkdown(2, 3)
	if err != nil {
		t.Fatalf("PageRangeMarkdown(2,3): %v", err)
	}
	if got != "bravo\ncharlie" {
		t.Errorf("PageRangeMarkdown(2,3) = %q, want %q", got, "bravo\ncharlie")
	}

	// The whole-document range must cover every line.
	whole, err := doc.PageRangeMarkdown(1, 4)
	if err != nil {
		t.Fatalf("PageRangeMarkdown(1,4): %v", err)
	}
	for _, line := range []string{"alpha", "bravo", "charlie", "delta"} {
		if !strings.Contains(whole, line) {
			t.Errorf("whole-document range dropped %q: %q", line, whole)
		}
	}
}

func TestPageScopeCSVKeepsHeader(t *testing.T) {
	raw := []byte("name,email\nMarie,m@example.com\nThomas,t@example.com\nAmelie,a@example.com\n")
	doc, err := Load("people.csv", raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := doc.PageCount(); got != 3 {
		t.Fatalf("PageCount = %d, want 3 (records, header excluded)", got)
	}

	// One record still carries the column names, because a bare data row robs
	// the model of the context those names give.
	got, err := doc.PageRangeMarkdown(2, 2)
	if err != nil {
		t.Fatalf("PageRangeMarkdown(2,2): %v", err)
	}
	if !strings.Contains(got, "name") || !strings.Contains(got, "email") {
		t.Errorf("row scope dropped the header row: %q", got)
	}
	if !strings.Contains(got, "Thomas") {
		t.Errorf("row scope missing the selected record: %q", got)
	}
	if strings.Contains(got, "Marie") || strings.Contains(got, "Amelie") {
		t.Errorf("row scope leaked an unselected record: %q", got)
	}
}

func TestPageScopePptxSlides(t *testing.T) {
	docs, err := LoadAll("deck.pptx", readFixture(t, "deck.pptx"))
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	doc := docs[0]
	count := doc.PageCount()
	if count < 2 {
		t.Fatalf("PageCount = %d, want at least 2 slides in the fixture deck", count)
	}
	if count != strings.Count(doc.Markdown, "## Slide ") {
		t.Errorf("PageCount = %d disagrees with the number of slide headings %d",
			count, strings.Count(doc.Markdown, "## Slide "))
	}

	first, err := doc.PageRangeMarkdown(1, 1)
	if err != nil {
		t.Fatalf("PageRangeMarkdown(1,1): %v", err)
	}
	if !strings.HasPrefix(first, "## Slide 1") {
		t.Errorf("first slide scope does not start at slide 1: %q", first)
	}
	if strings.Contains(first, "## Slide 2") {
		t.Errorf("first slide scope leaked slide 2: %q", first)
	}
}

func TestPageScopePdfPages(t *testing.T) {
	docs, err := LoadAll("textlayer.pdf", readFixture(t, "textlayer.pdf"))
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	doc := docs[0]
	if len(doc.Pages) == 0 {
		t.Fatal("a text-layer PDF must expose per-page texts")
	}
	if got, want := doc.PageCount(), len(doc.Pages); got != want {
		t.Fatalf("PageCount = %d, want %d (one per extracted page)", got, want)
	}

	whole, err := doc.PageRangeMarkdown(1, doc.PageCount())
	if err != nil {
		t.Fatalf("PageRangeMarkdown whole: %v", err)
	}
	// Every page's text must survive the whole-document range.
	for i, page := range doc.Pages {
		if !strings.Contains(whole, strings.TrimSpace(page)) {
			t.Errorf("whole-document range dropped page %d", i+1)
		}
	}
}

func TestPageScopeDocxPageBreaks(t *testing.T) {
	// Three paragraphs separated by Word's cached page-break markers, so the
	// document splits into three pages the way Word paginated it.
	raw := docxWithPageBreaks(t,
		[]string{"Opening remarks on page one."},
		[]string{"Second page discussion."},
		[]string{"Closing on the third page."})

	docs, err := LoadAll("paged.docx", raw)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	doc := docs[0]
	if got := doc.PageCount(); got != 3 {
		t.Fatalf("PageCount = %d, want 3 (one per page break group)", got)
	}
	// The page-break sentinel must never reach the working markdown.
	if strings.Contains(doc.Markdown, "\f") {
		t.Errorf("markdown still carries the page-break sentinel: %q", doc.Markdown)
	}

	page2, err := doc.PageRangeMarkdown(2, 2)
	if err != nil {
		t.Fatalf("PageRangeMarkdown(2,2): %v", err)
	}
	if !strings.Contains(page2, "Second page") {
		t.Errorf("page 2 scope missing its content: %q", page2)
	}
	if strings.Contains(page2, "Opening") || strings.Contains(page2, "Closing") {
		t.Errorf("page 2 scope leaked another page: %q", page2)
	}
}

func TestPageScopeSingleUnitFallback(t *testing.T) {
	// A DOCX Word never paginated (no cached page count, no break markers) has
	// no finer boundary than itself: one scannable unit, whole document only.
	docs, err := LoadAll("flat.docx", minimalDocx(t, []string{"Just one block of text."}, -1))
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	doc := docs[0]
	if got := doc.PageCount(); got != 1 {
		// Note: a no-cache docx falls back to the LINE unit, so a multi-line
		// body would count lines; this fixture is a single line on purpose.
		t.Fatalf("PageCount = %d, want 1 for a single-line unpaginated docx", got)
	}
	whole, err := doc.PageRangeMarkdown(1, 1)
	if err != nil {
		t.Fatalf("PageRangeMarkdown(1,1): %v", err)
	}
	if !strings.Contains(whole, "one block") {
		t.Errorf("whole-document scope missing content: %q", whole)
	}
}

func TestPageRangeOutOfBounds(t *testing.T) {
	doc, err := Load("notes.txt", []byte("a\nb\nc\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cases := [][2]int{{0, 1}, {1, 4}, {3, 2}, {-1, 2}, {2, 99}}
	for _, c := range cases {
		if _, err := doc.PageRangeMarkdown(c[0], c[1]); err == nil {
			t.Errorf("PageRangeMarkdown(%d,%d) must reject an out-of-bounds range", c[0], c[1])
		}
	}
}

// TestPagesMarkdownDiscontiguous covers the discontiguous local-model scope (CR3):
// a set like {1,3} returns exactly those units, concatenated, with the units
// between them left out.
func TestPagesMarkdownDiscontiguous(t *testing.T) {
	doc, err := Load("notes.txt", []byte("alpha\nbravo\ncharlie\ndelta\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err := doc.PagesMarkdown([]int{1, 3})
	if err != nil {
		t.Fatalf("PagesMarkdown([1,3]): %v", err)
	}
	if got != "alpha\n\ncharlie" {
		t.Errorf("PagesMarkdown([1,3]) = %q, want %q", got, "alpha\n\ncharlie")
	}
	for _, leak := range []string{"bravo", "delta"} {
		if strings.Contains(got, leak) {
			t.Errorf("PagesMarkdown([1,3]) leaked unselected line %q: %q", leak, got)
		}
	}
}

// TestPagesMarkdownCSVKeepsHeaderOnce proves a grid page set carries the header
// row exactly once, then each selected data record, so the model keeps column
// context without a header repeated per row.
func TestPagesMarkdownCSVKeepsHeaderOnce(t *testing.T) {
	raw := []byte("name,email\nMarie,m@example.com\nThomas,t@example.com\nAmelie,a@example.com\n")
	doc, err := Load("people.csv", raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err := doc.PagesMarkdown([]int{1, 3})
	if err != nil {
		t.Fatalf("PagesMarkdown([1,3]): %v", err)
	}
	if strings.Count(got, "email") != 1 {
		t.Errorf("grid page set must carry the header exactly once: %q", got)
	}
	if !strings.Contains(got, "Marie") || !strings.Contains(got, "Amelie") {
		t.Errorf("grid page set dropped a selected record: %q", got)
	}
	if strings.Contains(got, "Thomas") {
		t.Errorf("grid page set leaked an unselected record: %q", got)
	}
}

// TestPagesMarkdownOutOfBounds: an index outside 1..PageCount, or an empty set,
// gives the same actionable error style as PageRangeMarkdown rather than a
// panic or a silent truncation.
func TestPagesMarkdownOutOfBounds(t *testing.T) {
	doc, err := Load("notes.txt", []byte("a\nb\nc\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cases := [][]int{{}, {0}, {4}, {-1}, {2, 99}}
	for _, c := range cases {
		if _, err := doc.PagesMarkdown(c); err == nil {
			t.Errorf("PagesMarkdown(%v) must reject an out-of-bounds or empty set", c)
		}
	}
}

// docxWithPageBreaks builds a minimal .docx whose paragraph groups are
// separated by <w:lastRenderedPageBreak/> markers, one group per page.
func docxWithPageBreaks(t *testing.T, groups ...[]string) []byte {
	t.Helper()
	var body strings.Builder
	for i, group := range groups {
		for j, line := range group {
			body.WriteString(`<w:p><w:r>`)
			// Open every group except the first with the page-break marker.
			if i > 0 && j == 0 {
				body.WriteString(`<w:lastRenderedPageBreak/>`)
			}
			body.WriteString(`<w:t>` + line + `</w:t></w:r></w:p>`)
		}
	}
	document := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body>` + body.String() + `</w:body></w:document>`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("building the fixture: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("building the fixture: %v", err)
		}
	}
	write("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?>`+
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`+
		`<Default Extension="xml" ContentType="application/xml"/></Types>`)
	write("word/document.xml", document)
	if err := zw.Close(); err != nil {
		t.Fatalf("building the fixture: %v", err)
	}
	return buf.Bytes()
}
