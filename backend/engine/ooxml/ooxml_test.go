// engine/ooxml/ooxml_test.go — the shared OOXML plumbing.
//
// The behaviour worth pinning is the ABSENT case. Every docProps part is
// optional, files written by minimal tooling omit them, and the whole reason
// this package exists is that two callers need "absent" to mean "no answer"
// rather than "error".
package ooxml

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// buildZip assembles an in-memory archive from name/content pairs.
func buildZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("building the fixture: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("building the fixture: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("building the fixture: %v", err)
	}
	return buf.Bytes()
}

func TestReadPartsSkipsAbsentPartsWithoutAnError(t *testing.T) {
	raw := buildZip(t, map[string]string{
		"docProps/core.xml": "<core/>",
		"word/document.xml": "<doc/>",
	})

	parts, err := ReadParts(raw, "docProps/core.xml", "docProps/app.xml")
	if err != nil {
		t.Fatalf("an absent part is not an error: %v", err)
	}
	if _, ok := parts["docProps/core.xml"]; !ok {
		t.Error("the present part must be returned")
	}
	if _, ok := parts["docProps/app.xml"]; ok {
		t.Error("an absent part must produce NO map entry, so callers can tell it apart " +
			"from a part that exists and is empty")
	}
	if len(parts) != 2-1 {
		t.Errorf("parts = %v, want exactly the one present part", parts)
	}
}

func TestReadPartsRejectsSomethingThatIsNotAZip(t *testing.T) {
	_, err := ReadParts([]byte("this is not a zip archive"), AppPropsPart)
	if err == nil {
		t.Fatal("a non-zip must be refused")
	}
	// The message has to say what to DO, not just what went wrong: the owner
	// hitting this has a file they believe is a Word document.
	for _, want := range []string{"zip archive", "re-export"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must mention %q, got: %v", want, err)
		}
	}
}

func TestReadAppPropsAbsentVersusPresent(t *testing.T) {
	// Absent: no error, Present false, every count zero. This is the COMMON
	// case, and the caller's cue to fall back to counting lines.
	absent, err := ReadAppProps(buildZip(t, map[string]string{"word/document.xml": "<doc/>"}))
	if err != nil {
		t.Fatalf("a file with no app.xml must not be an error: %v", err)
	}
	if absent.Present {
		t.Error("Present must be false when the part is missing")
	}
	if absent.Pages != 0 || absent.Slides != 0 {
		t.Errorf("absent counts must be zero, got %+v", absent)
	}

	// Present: the cached counts come back, namespace prefix and all.
	present, err := ReadAppProps(buildZip(t, map[string]string{
		AppPropsPart: `<?xml version="1.0"?>` +
			`<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties">` +
			`<Pages>6</Pages><Slides>12</Slides><Words>412</Words><Lines>37</Lines></Properties>`,
	}))
	if err != nil {
		t.Fatalf("ReadAppProps: %v", err)
	}
	if !present.Present {
		t.Error("Present must be true when the part is there")
	}
	if present.Pages != 6 || present.Slides != 12 || present.Words != 412 || present.Lines != 37 {
		t.Errorf("counts = %+v, want 6/12/412/37", present)
	}
}

func TestReadAppPropsTreatsAnUnparseableCountAsAbsent(t *testing.T) {
	// A property that says "many" is no more useful than a missing one, and
	// failing the whole import over it would be absurd.
	props, err := ReadAppProps(buildZip(t, map[string]string{
		AppPropsPart: `<Properties><Pages>many</Pages><Slides>-3</Slides></Properties>`,
	}))
	if err != nil {
		t.Fatalf("an unparseable count must not be an error: %v", err)
	}
	if props.Pages != 0 {
		t.Errorf("Pages = %d, want 0 for an unparseable value", props.Pages)
	}
	if props.Slides != 0 {
		t.Errorf("Slides = %d, want 0 for a negative value", props.Slides)
	}
	// The part WAS there, and saying so is different from saying it had answers.
	if !props.Present {
		t.Error("Present must still be true: the part existed")
	}
}

func TestElementTextMatchesLocalNamesAcrossPrefixes(t *testing.T) {
	// Producers differ: Word writes cp:/dc:/dcterms: prefixes, LibreOffice
	// writes others, some write none. Matching on the local name is what makes
	// one reader work for all of them.
	data := []byte(`<cp:coreProperties xmlns:cp="urn:a" xmlns:dc="urn:b">` +
		`<dc:creator>Marie Duval</dc:creator><cp:lastModifiedBy>Thomas Berger</cp:lastModifiedBy>` +
		`<title>Services agreement</title></cp:coreProperties>`)
	got, err := ElementText(data, map[string]bool{
		"creator": true, "lastModifiedBy": true, "title": true, "subject": true,
	})
	if err != nil {
		t.Fatalf("ElementText: %v", err)
	}
	want := map[string]string{
		"creator":        "Marie Duval",
		"lastModifiedBy": "Thomas Berger",
		"title":          "Services agreement",
	}
	for name, value := range want {
		if got[name] != value {
			t.Errorf("%s = %q, want %q", name, got[name], value)
		}
	}
	if _, ok := got["subject"]; ok {
		t.Error("an element that is not in the document must produce no entry")
	}
}

func TestElementTextRefusesMalformedXML(t *testing.T) {
	_, err := ElementText([]byte("<Properties><Pages>6</Pages"), map[string]bool{"Pages": true})
	if err == nil {
		t.Fatal("a truncated part must be refused: a properties part that exists and " +
			"does not parse means the file is damaged")
	}
	if !strings.Contains(err.Error(), "well-formed") {
		t.Errorf("the message must say what is wrong with it, got: %v", err)
	}
}

func TestCountEntriesCountsWhatThePredicateAccepts(t *testing.T) {
	raw := buildZip(t, map[string]string{
		"ppt/slides/slide1.xml":            "<s/>",
		"ppt/slides/slide2.xml":            "<s/>",
		"ppt/slides/slide10.xml":           "<s/>",
		"ppt/slides/_rels/slide1.xml.rels": "<r/>", // NOT a slide
		"ppt/notesSlides/notesSlide1.xml":  "<n/>", // NOT a slide either
	})
	n, err := CountEntries(raw, func(name string) bool {
		return strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml")
	})
	if err != nil {
		t.Fatalf("CountEntries: %v", err)
	}
	if n != 3 {
		t.Errorf("counted %d slides, want 3: the rels and notes parts must not count", n)
	}
}
