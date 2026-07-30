// engine/ooxml/ooxml.go — the shared OOXML plumbing (BUILD-05 Phase 3).
//
// docx, pptx and xlsx are all zip archives with the same `docProps/` layout,
// and TWO parts of the codebase need to read from it:
//
//	engine/exportfmt/metadata.go  harvests the reviewable properties (Author,
//	                              Company, Title, ...) before a same-format
//	                              export rewrites them.
//	engine/convert/*.go           reads the CACHED COUNTS (<Pages>, <Slides>,
//	                              <Words>, <Lines>) so the import list can say
//	                              "6 pages" instead of "412 lines".
//
// Before BUILD-05 the first of those owned a private zip walk and a private
// XML token scan. The second needed the same two things, so this package holds
// them once. It is deliberately tiny and deliberately generic: it knows how to
// pull a named part out of an OOXML archive and how to read the text of named
// elements out of an XML part, and nothing about what any particular part
// means.
//
// The counts themselves come with two caveats that belong wherever they are
// shown to a user, so they are restated in AppProps below.
package ooxml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// ReadParts pulls the named entries out of an OOXML archive.
//
// An ABSENT part is not an error and produces no map entry: every docProps
// part is optional, and files written by minimal tooling omit them entirely
// (the repository's own backend/testdata/report.docx and deck.pptx have no
// docProps/app.xml at all). Callers distinguish "absent" from "present but
// empty" by checking for the key, which is why the map holds []byte rather
// than string.
//
// @param raw the whole archive, already in memory
// @param parts archive entry names, e.g. "docProps/app.xml"
// @return the parts that were present, keyed by entry name
func ReadParts(raw []byte, parts ...string) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("the file could not be opened as a zip archive (%v); "+
			"Word, PowerPoint and Excel files are zip archives, so this is either not one "+
			"of those formats or the file is damaged, re-export it from the source "+
			"application and import it again", err)
	}

	want := make(map[string]bool, len(parts))
	for _, part := range parts {
		want[part] = true
	}

	out := make(map[string][]byte, len(parts))
	for _, entry := range reader.File {
		if !want[entry.Name] {
			continue
		}
		src, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("could not read %q inside the file: %w", entry.Name, err)
		}
		data, err := io.ReadAll(src)
		src.Close()
		if err != nil {
			return nil, fmt.Errorf("could not read %q inside the file: %w", entry.Name, err)
		}
		out[entry.Name] = data
	}
	return out, nil
}

// CountEntries reports how many archive entries satisfy the predicate.
//
// It exists for the pptx slide count, which is the one figure that can be
// derived from the archive's SHAPE rather than from a cached property, and is
// therefore the one figure that is always correct.
//
// @param raw the whole archive, already in memory
// @param match called with each entry name
// @return how many entries matched
func CountEntries(raw []byte, match func(name string) bool) (int, error) {
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return 0, fmt.Errorf("the file could not be opened as a zip archive (%v); "+
			"re-export it from the source application and import it again", err)
	}
	n := 0
	for _, entry := range reader.File {
		if match(entry.Name) {
			n++
		}
	}
	return n, nil
}

// ElementText token-scans an XML part and returns the text content of every
// element whose LOCAL name is wanted.
//
// Local names, not namespaced ones, because the docProps parts use several
// namespace prefixes across producers (`cp:`, `dc:`, `dcterms:`, none at all)
// and matching on the prefix would work for Word and fail for LibreOffice.
//
// A repeated element wins with its LAST occurrence, matching how an XML
// document is normally read. RawToken is used rather than Token so an
// undeclared namespace prefix, which some producers emit, does not turn a
// readable file into a parse error.
//
// @param data one XML part
// @param want the local names to collect
// @return local name to text content, for the names that were present
func ElementText(data []byte, want map[string]bool) (map[string]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	out := map[string]string{}
	var buf bytes.Buffer
	collecting := ""

	for {
		tok, err := decoder.RawToken()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("a properties part inside the file is not well-formed XML (%v)", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if want[t.Name.Local] {
				collecting = t.Name.Local
				buf.Reset()
			}
		case xml.CharData:
			if collecting != "" {
				buf.Write(t)
			}
		case xml.EndElement:
			if collecting != "" && t.Name.Local == collecting {
				out[collecting] = buf.String()
				collecting = ""
			}
		}
	}
	return out, nil
}

// AppPropsPart is the OOXML extended-properties part, which every producer
// that writes one writes here.
const AppPropsPart = "docProps/app.xml"

// AppProps holds the counts docProps/app.xml caches. Every field is 0 when the
// producer did not write it, which is common.
//
// TWO CAVEATS, and they belong with the numbers rather than in a commit
// message, because they are the reason the import list falls back to lines:
//
//  1. These are what the LAST WRITING APPLICATION computed at save time. They
//     are not recomputed on import and cannot be: laying out a document to
//     count its pages means implementing Word's layout engine. A file
//     rewritten by a tool that does not relayout can therefore report a stale
//     figure, and there is no way to tell from the file that it is stale.
//  2. The whole part is OPTIONAL. Files written by minimal tooling omit it,
//     which makes the absent case the COMMON one in tests rather than an edge
//     case, so every caller must degrade rather than showing "0 pages".
type AppProps struct {
	Pages  int
	Slides int
	Words  int
	Lines  int
	// Present is false when the archive carries no docProps/app.xml at all,
	// which is different from a part that exists and caches nothing.
	Present bool
}

// appCountFields are the count elements AppProps reads.
var appCountFields = map[string]bool{
	"Pages": true, "Slides": true, "Words": true, "Lines": true,
}

// ReadAppProps reads the cached counts out of an OOXML archive.
//
// A missing part yields a zero AppProps with Present false and NO error: it is
// a normal state, not a failure. A malformed part is an error, because a
// docProps/app.xml that exists and does not parse means the file is damaged in
// a way the user should hear about.
//
// @param raw the whole archive, already in memory
// @return the counts, and whether the part was there at all
func ReadAppProps(raw []byte) (AppProps, error) {
	parts, err := ReadParts(raw, AppPropsPart)
	if err != nil {
		return AppProps{}, err
	}
	data, ok := parts[AppPropsPart]
	if !ok {
		return AppProps{}, nil
	}

	text, err := ElementText(data, appCountFields)
	if err != nil {
		return AppProps{}, err
	}
	props := AppProps{Present: true}
	props.Pages = atoiOrZero(text["Pages"])
	props.Slides = atoiOrZero(text["Slides"])
	props.Words = atoiOrZero(text["Words"])
	props.Lines = atoiOrZero(text["Lines"])
	return props, nil
}

// atoiOrZero parses a cached count, treating anything unparseable as absent.
// A property that says "many" is no more useful than one that is missing, and
// failing the whole import over it would be absurd.
func atoiOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
