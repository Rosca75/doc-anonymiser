// engine/convert/docx.go — .docx → markdown (one-way), pure Go.
//
// A .docx file is a zip archive; the document body lives in
// word/document.xml (WordprocessingML). This converter walks that XML with
// encoding/xml's token API — token walking (rather than struct unmarshal)
// is deliberate: paragraphs mix runs, hyperlinks and drawings in document
// order, and only a token walk preserves that order faithfully.
//
// Mapping implemented (CLAUDE.md §5, BUILD.md Phase 1B):
//   - paragraph styles Heading1–6 (and French Titre1–6) → # … ######
//   - bold/italic run properties → **bold** / *italic*
//   - numbered/bulleted lists (w:numPr) → "1." / "-" items with indentation
//   - tables (w:tbl) → markdown tables (first row as header)
//   - hyperlinks (w:hyperlink, resolved via word/_rels/document.xml.rels)
//     → [text](url)
//   - images (w:drawing / w:pict) → *[image omitted]* + a warning
//   - headers/footers/footnotes: NOT read at all (separate zip entries) —
//     dropped by design as pagination noise.
package convert

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// Docx converts raw .docx bytes to markdown. Returned warnings are
// human-readable notes for the import UI (e.g. how many images were
// dropped); they never block the conversion.
func Docx(raw []byte) (markdown string, warnings []string, err error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", nil, fmt.Errorf(
			"the file is not a valid .docx (it is not a zip archive: %v), if it is an old binary .doc file, open it in Word and save it as .docx first", err)
	}

	docXML, err := readZipEntry(zr, "word/document.xml")
	if err != nil {
		return "", nil, fmt.Errorf(
			"the file is not a valid .docx (missing word/document.xml), if it is an old binary .doc file, open it in Word and save it as .docx first")
	}

	// Relationships resolve hyperlink r:id values to real URLs. Missing
	// rels are fine (a document without hyperlinks has no rels we need).
	rels := parseRels(zr, "word/_rels/document.xml.rels")

	// numbering.xml tells us whether a list is bulleted or numbered.
	// Missing numbering.xml simply means every list renders as bullets.
	numFmts := parseDocxNumbering(zr)

	p := &docxParser{rels: rels, numFmts: numFmts}
	if err := p.parseBody(docXML); err != nil {
		return "", nil, fmt.Errorf("could not parse word/document.xml: %w, the file may be corrupted; try re-saving it in Word", err)
	}

	if p.imagesDropped > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d image(s) were dropped and replaced by an *[image omitted]* placeholder, images cannot be anonymised as text", p.imagesDropped))
	}
	return p.out.String(), warnings, nil
}

// readZipEntry returns the full content of one entry in the archive.
func readZipEntry(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("zip entry %q not found", name)
}

// parseRels reads an OOXML relationships file into an id → target map.
// Used for docx hyperlinks and pptx notes-slide resolution. Any failure
// returns an empty map — a missing link target degrades to plain text,
// which is acceptable for an anonymiser (the text still gets processed).
func parseRels(zr *zip.Reader, name string) map[string]string {
	rels := map[string]string{}
	data, err := readZipEntry(zr, name)
	if err != nil {
		return rels
	}
	// The rels file is flat: <Relationship Id="rId1" Type="..." Target="..."/>
	var doc struct {
		Rel []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if xml.Unmarshal(data, &doc) == nil {
		for _, r := range doc.Rel {
			rels[r.ID] = r.Target
		}
	}
	return rels
}

// parseDocxNumbering extracts numId → ilvl → numFmt ("bullet", "decimal",
// ...) from word/numbering.xml. The indirection mirrors the file format:
// w:num maps numId → abstractNumId, w:abstractNum holds the per-level
// formats.
func parseDocxNumbering(zr *zip.Reader) map[string]map[string]string {
	out := map[string]map[string]string{}
	data, err := readZipEntry(zr, "word/numbering.xml")
	if err != nil {
		return out
	}
	var doc struct {
		AbstractNum []struct {
			ID  string `xml:"abstractNumId,attr"`
			Lvl []struct {
				Ilvl   string `xml:"ilvl,attr"`
				NumFmt struct {
					Val string `xml:"val,attr"`
				} `xml:"numFmt"`
			} `xml:"lvl"`
		} `xml:"abstractNum"`
		Num []struct {
			NumID         string `xml:"numId,attr"`
			AbstractNumID struct {
				Val string `xml:"val,attr"`
			} `xml:"abstractNumId"`
		} `xml:"num"`
	}
	if xml.Unmarshal(data, &doc) != nil {
		return out
	}
	abstract := map[string]map[string]string{}
	for _, a := range doc.AbstractNum {
		lvls := map[string]string{}
		for _, l := range a.Lvl {
			lvls[l.Ilvl] = l.NumFmt.Val
		}
		abstract[a.ID] = lvls
	}
	for _, n := range doc.Num {
		if lvls, ok := abstract[n.AbstractNumID.Val]; ok {
			out[n.NumID] = lvls
		}
	}
	return out
}

// docxParser carries the state of one document.xml walk.
type docxParser struct {
	rels          map[string]string            // hyperlink id → URL
	numFmts       map[string]map[string]string // numId → ilvl → numFmt
	out           strings.Builder              // accumulated markdown
	imagesDropped int
}

// parseBody walks the top level of document.xml: a sequence of paragraphs
// (w:p) and tables (w:tbl) inside w:body. Everything else at this level
// (section properties, bookmarks) is skipped.
func (p *docxParser) parseBody(docXML []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(docXML))
	// lastWasList lets consecutive list items sit on adjacent lines (a
	// blank line between every item would render as a "loose" list) while
	// everything else stays separated by blank lines.
	lastWasList := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if se, ok := tok.(xml.StartElement); ok {
			switch se.Name.Local {
			case "p":
				line, isList := p.parseParagraph(dec)
				if strings.TrimSpace(line) == "" {
					continue
				}
				if isList {
					p.out.WriteString(line + "\n")
				} else {
					if lastWasList {
						p.out.WriteString("\n") // close the list block
					}
					p.out.WriteString(line + "\n\n")
				}
				lastWasList = isList
			case "tbl":
				rows, err := p.parseTable(dec)
				if err != nil {
					return err
				}
				if md := tableToMarkdown(rows); md != "" {
					if lastWasList {
						p.out.WriteString("\n")
						lastWasList = false
					}
					p.out.WriteString(md + "\n")
				}
			}
		}
	}
}

// parseParagraph consumes tokens until </w:p> and returns the paragraph as
// one markdown line: heading, list item, or plain text. isList tells the
// caller whether the line is a list item (drives blank-line placement).
func (p *docxParser) parseParagraph(dec *xml.Decoder) (line string, isList bool) {
	var (
		text    strings.Builder
		style   string // pStyle val, e.g. "Heading1"
		numID   string // set when the paragraph is a list item
		ilvl    = "0"  // list indentation level
		depth   = 1    // we are inside one <w:p>
		inPPr   bool   // pStyle/numPr only count inside paragraph properties
		curLink string // hyperlink URL currently in effect ("" = none)
	)

	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break // truncated XML: return what we have rather than losing it
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch t.Name.Local {
			case "pPr":
				inPPr = true
			case "pStyle":
				if inPPr {
					style = attrVal(t, "val")
				}
			case "numId":
				if inPPr {
					numID = attrVal(t, "val")
				}
			case "ilvl":
				if inPPr {
					ilvl = attrVal(t, "val")
				}
			case "hyperlink":
				// Resolve r:id via the rels map; open a markdown link.
				if target, ok := p.rels[attrVal(t, "id")]; ok && target != "" {
					curLink = target
					text.WriteString("[")
				}
			case "r":
				depth-- // parseRun consumes up to and including </w:r>
				text.WriteString(p.parseRun(dec))
			}
		case xml.EndElement:
			depth--
			switch t.Name.Local {
			case "pPr":
				inPPr = false
			case "hyperlink":
				if curLink != "" {
					text.WriteString("](" + curLink + ")")
					curLink = ""
				}
			}
		}
	}

	body := text.String()

	// Heading styles map to markdown # levels. Word's built-in style IDs
	// are "Heading1".."Heading6" in English documents and "Titre1".. in
	// French ones — we accept both (fixtures are English + French).
	for _, prefix := range []string{"Heading", "Titre"} {
		if strings.HasPrefix(style, prefix) {
			lvl := strings.TrimPrefix(style, prefix)
			if len(lvl) == 1 && lvl >= "1" && lvl <= "6" {
				n := int(lvl[0] - '0')
				return strings.Repeat("#", n) + " " + strings.TrimSpace(body), false
			}
		}
	}

	// List paragraphs: indentation from ilvl, marker from numbering.xml.
	// Markdown renumbers ordered lists itself, so a literal "1." for every
	// numbered item is correct and avoids tracking counters.
	if numID != "" {
		marker := "-"
		if fmts, ok := p.numFmts[numID]; ok && fmts[ilvl] != "bullet" && fmts[ilvl] != "" {
			marker = "1."
		}
		indent := strings.Repeat("  ", atoiSafe(ilvl))
		return indent + marker + " " + strings.TrimSpace(body), true
	}

	return body, false
}

// parseRun consumes one <w:r>…</w:r> and returns its markdown text with
// bold/italic applied. Images inside the run become the placeholder.
func (p *docxParser) parseRun(dec *xml.Decoder) string {
	var (
		text         strings.Builder
		bold, italic bool
		depth        = 1
		inRPr        bool
		inText       bool
	)
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch t.Name.Local {
			case "rPr":
				inRPr = true
			case "b":
				// <w:b/> means bold unless explicitly w:val="false"/"0".
				if inRPr && attrVal(t, "val") != "false" && attrVal(t, "val") != "0" {
					bold = true
				}
			case "i":
				if inRPr && attrVal(t, "val") != "false" && attrVal(t, "val") != "0" {
					italic = true
				}
			case "t":
				inText = true
			case "br":
				// Soft line break inside a paragraph → a space, so the
				// paragraph stays one markdown line.
				text.WriteString(" ")
			case "drawing", "pict":
				// Images are dropped with an inline placeholder
				// (CLAUDE.md §5) — the count feeds the import warning.
				p.imagesDropped++
				text.WriteString("*[image omitted]*")
			}
		case xml.CharData:
			if inText {
				text.Write(t)
			}
		case xml.EndElement:
			depth--
			switch t.Name.Local {
			case "rPr":
				inRPr = false
			case "t":
				inText = false
			}
		}
	}
	return wrapEmphasis(text.String(), bold, italic)
}

// parseTable consumes one <w:tbl>…</w:tbl> and returns its cell text as
// rows × columns. Each cell may contain several paragraphs — they are
// joined with a space because a markdown cell is a single line.
func (p *docxParser) parseTable(dec *xml.Decoder) ([][]string, error) {
	var (
		rows  [][]string
		row   []string
		cell  []string
		depth = 1
	)
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch t.Name.Local {
			case "tr":
				row = nil
			case "tc":
				cell = nil
			case "p":
				depth-- // parseParagraph consumes its own </w:p>
				if para, _ := p.parseParagraph(dec); strings.TrimSpace(para) != "" {
					cell = append(cell, strings.TrimSpace(para))
				}
			}
		case xml.EndElement:
			depth--
			switch t.Name.Local {
			case "tc":
				row = append(row, strings.Join(cell, " "))
			case "tr":
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

// attrVal returns the value of the named attribute (namespace-agnostic:
// OOXML attributes arrive as w:val, r:id, etc. and we match on the local
// part only).
func attrVal(se xml.StartElement, name string) string {
	for _, a := range se.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// atoiSafe parses a small non-negative integer, returning 0 for anything
// unparseable — list levels never justify failing a whole conversion.
func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	if n > 8 { // OOXML list levels are 0..8; clamp garbage
		return 8
	}
	return n
}
