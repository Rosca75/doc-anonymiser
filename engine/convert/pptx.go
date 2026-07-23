// engine/convert/pptx.go — .pptx → markdown (one-way), pure Go.
//
// A .pptx file is a zip archive with one XML file per slide
// (ppt/slides/slide1.xml, slide2.xml, …, DrawingML). Mapping implemented
// (CLAUDE.md §5, BUILD.md Phase 1B):
//   - one "## Slide N — <title>" section per slide, in slide order
//     ("## Slide N" when the slide has no title placeholder)
//   - body text frames with bullet indentation (a:pPr lvl)
//   - tables (a:tbl) → markdown tables
//   - speaker notes under a "**Notes:**" sub-block, resolved through the
//     slide's relationships file (robust against reordered slides)
//   - slide-master / layout / branding shapes are skipped automatically:
//     only slideN.xml is read, and master content lives in other entries.
package convert

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// slideEntryRe matches slide entries and captures the slide number so we
// can sort numerically (slide10 must come after slide9, not after slide1).
var slideEntryRe = regexp.MustCompile(`^ppt/slides/slide(\d+)\.xml$`)

// Pptx converts raw .pptx bytes to markdown, one H2 section per slide.
func Pptx(raw []byte) (markdown string, warnings []string, err error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", nil, fmt.Errorf(
			"the file is not a valid .pptx (it is not a zip archive: %v) — if it is an old binary .ppt file, open it in PowerPoint and save it as .pptx first", err)
	}

	// Collect slide entries and sort them by slide number.
	type slideRef struct {
		n    int
		name string
	}
	var slides []slideRef
	for _, f := range zr.File {
		if m := slideEntryRe.FindStringSubmatch(f.Name); m != nil {
			n, _ := strconv.Atoi(m[1])
			slides = append(slides, slideRef{n: n, name: f.Name})
		}
	}
	if len(slides) == 0 {
		return "", nil, fmt.Errorf(
			"the file contains no slides (no ppt/slides/slideN.xml entries) — it may be corrupted or not a real PowerPoint file")
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].n < slides[j].n })

	var out strings.Builder
	for _, s := range slides {
		data, err := readZipEntry(zr, s.name)
		if err != nil {
			return "", nil, err
		}
		title, body, err := parseSlide(data)
		if err != nil {
			return "", nil, fmt.Errorf("could not parse slide %d: %w — the file may be corrupted; try re-saving it in PowerPoint", s.n, err)
		}

		// Section heading: "## Slide N — Title" (or just "## Slide N").
		if title != "" {
			out.WriteString(fmt.Sprintf("## Slide %d — %s\n\n", s.n, title))
		} else {
			out.WriteString(fmt.Sprintf("## Slide %d\n\n", s.n))
		}
		if body != "" {
			out.WriteString(body)
		}

		// Speaker notes: resolved through the slide's own rels file so a
		// reordered deck still finds the right notesSlide entry.
		if notes := slideNotes(zr, s.name); notes != "" {
			out.WriteString("**Notes:**\n\n" + notes + "\n\n")
		}
	}
	return out.String(), warnings, nil
}

// slideNotes returns the speaker-notes text for a slide entry, or "".
func slideNotes(zr *zip.Reader, slideName string) string {
	// ppt/slides/slide3.xml → ppt/slides/_rels/slide3.xml.rels
	base := slideName[strings.LastIndex(slideName, "/")+1:]
	rels := parseRelsTyped(zr, "ppt/slides/_rels/"+base+".rels", "notesSlide")
	if rels == "" {
		return ""
	}
	// Targets are relative to ppt/slides/ (e.g. "../notesSlides/notesSlide1.xml").
	target := strings.TrimPrefix(rels, "../")
	data, err := readZipEntry(zr, "ppt/"+target)
	if err != nil {
		return ""
	}
	_, body, err := parseNotesSlide(data)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(body)
}

// parseRelsTyped returns the Target of the first relationship whose Type
// ends with the given suffix (e.g. "notesSlide"), or "".
func parseRelsTyped(zr *zip.Reader, name, typeSuffix string) string {
	data, err := readZipEntry(zr, name)
	if err != nil {
		return ""
	}
	var doc struct {
		Rel []struct {
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if xml.Unmarshal(data, &doc) != nil {
		return ""
	}
	for _, r := range doc.Rel {
		if strings.HasSuffix(r.Type, typeSuffix) {
			return r.Target
		}
	}
	return ""
}

// parseSlide walks one slideN.xml and returns the title text and the body
// markdown (bullet lines and tables, in document order).
func parseSlide(data []byte) (title, body string, err error) {
	return walkShapes(data, false)
}

// parseNotesSlide extracts the notes text. Notes slides re-use the same
// DrawingML shapes; the body placeholder holds the actual notes while
// slide-number/header placeholders are skipped by walkShapes.
func parseNotesSlide(data []byte) (title, body string, err error) {
	return walkShapes(data, true)
}

// walkShapes is the shared DrawingML walker for slides and notes slides.
//
// notesMode changes placeholder filtering: on a notes slide only the
// "body" placeholder is wanted (the slide-image and slide-number
// placeholders would inject noise like a bare page number into the text).
func walkShapes(data []byte, notesMode bool) (title, body string, err error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var out strings.Builder

	// Per-shape state, reset at each <p:sp>.
	var (
		inShape   bool
		phType    string          // placeholder type: "title", "body", "sldNum", …
		shapeText strings.Builder // accumulated markdown lines of this shape
	)
	// Per-paragraph state.
	var (
		inPara   bool
		paraLvl  int
		paraText strings.Builder
	)
	flushShape := func() {
		text := shapeText.String()
		if strings.TrimSpace(text) == "" {
			return
		}
		isTitle := phType == "title" || phType == "ctrTitle"
		if isTitle && !notesMode {
			// The title becomes the section heading, first line only.
			if title == "" {
				title = strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
			}
			return
		}
		if notesMode && phType != "body" {
			// Notes slides: skip slide-number / slide-image placeholders.
			return
		}
		out.WriteString(text + "\n")
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "sp":
				inShape = true
				phType = ""
				shapeText.Reset()
			case "ph":
				if inShape {
					phType = attrVal(t, "type")
					// A missing type attribute means "body" placeholder
					// by OOXML convention.
					if phType == "" {
						phType = "body"
					}
				}
			case "p":
				// a:p — a paragraph inside the current text body.
				if inShape {
					inPara = true
					paraLvl = 0
					paraText.Reset()
				}
			case "pPr":
				if inPara {
					if lvl := attrVal(t, "lvl"); lvl != "" {
						paraLvl = atoiSafe(lvl)
					}
				}
			case "t":
				// a:t — a text run. Collect chardata until the matching end.
				if inPara {
					var chunk strings.Builder
					for {
						it, err := dec.Token()
						if err != nil {
							return "", "", err
						}
						if cd, ok := it.(xml.CharData); ok {
							chunk.Write(cd)
							continue
						}
						if ee, ok := it.(xml.EndElement); ok && ee.Name.Local == "t" {
							break
						}
					}
					paraText.WriteString(chunk.String())
				}
			case "tbl":
				// a:tbl — a table inside a graphic frame (not inside a
				// shape, so it flushes straight to the output).
				rows, err := parsePptxTable(dec)
				if err != nil {
					return "", "", err
				}
				if md := tableToMarkdown(rows); md != "" {
					out.WriteString(md + "\n")
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p":
				if inPara {
					inPara = false
					line := strings.TrimSpace(paraText.String())
					if line != "" {
						if notesMode || phType == "title" || phType == "ctrTitle" {
							// Notes and titles read as plain lines, not bullets.
							shapeText.WriteString(line + "\n")
						} else {
							// Body paragraphs are bullets by PowerPoint
							// convention; indentation mirrors the outline level.
							shapeText.WriteString(strings.Repeat("  ", paraLvl) + "- " + line + "\n")
						}
					}
				}
			case "sp":
				if inShape {
					flushShape()
					inShape = false
				}
			}
		}
	}
	return title, out.String(), nil
}

// parsePptxTable consumes one <a:tbl>…</a:tbl> and returns rows × cells of
// plain text (runs inside a cell are concatenated; paragraphs joined by a
// space, since a markdown cell is one line).
func parsePptxTable(dec *xml.Decoder) ([][]string, error) {
	var (
		rows   [][]string
		row    []string
		cell   strings.Builder
		inCell bool
		depth  = 1
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
				inCell = true
				cell.Reset()
			case "t":
				if inCell {
					for {
						it, err := dec.Token()
						if err != nil {
							return nil, err
						}
						if cd, ok := it.(xml.CharData); ok {
							cell.Write(cd)
							continue
						}
						if ee, ok := it.(xml.EndElement); ok && ee.Name.Local == "t" {
							depth-- // we consumed </a:t> ourselves
							break
						}
					}
				}
			case "p":
				// Paragraph boundary inside a cell → space separator.
				if inCell && cell.Len() > 0 {
					cell.WriteString(" ")
				}
			}
		case xml.EndElement:
			depth--
			switch t.Name.Local {
			case "tc":
				row = append(row, strings.TrimSpace(cell.String()))
				inCell = false
			case "tr":
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}
