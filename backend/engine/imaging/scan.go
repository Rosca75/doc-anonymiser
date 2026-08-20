// engine/imaging/scan.go — the OOXML picture scanner, shared by .docx and .pptx.
//
// Both formats are zip archives whose pictures are DrawingML blips (plus the
// legacy VML imagedata Word still writes for pasted content), whose bytes live
// in a media/ folder, and whose XML parts point at those bytes through a
// relationships file. That is one algorithm, and the two formats differ only in
// WHICH parts are walked and WHAT a place is called. So there is one walker and
// two profiles, and keeping it that way is the point: an export that rewrites a
// picture has to find exactly the element the scan listed, and it can only be
// sure of that if it walks the same parts by the same rule.
package imaging

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ScanDocx lists the pictures of a .docx, in document order.
//
// @param raw the whole file, as captured at import (Document.Raw)
// @return the inventory; an error only when the file cannot be read as OOXML
func ScanDocx(raw []byte) (Inventory, error) {
	return scanOOXML(raw, docxProfile)
}

// ScanPptx lists the pictures of a .pptx, in slide order.
//
// @param raw the whole file, as captured at import (Document.Raw)
// @return the inventory; an error only when the file cannot be read as OOXML
func ScanPptx(raw []byte) (Inventory, error) {
	return scanOOXML(raw, pptxProfile)
}

// partKind says what a walked part IS, which is all the location resolver needs
// to name the place in the document's own words.
type partKind string

const (
	partBody      partKind = "body"
	partHeader    partKind = "header"
	partFooter    partKind = "footer"
	partFootnotes partKind = "footnotes"
	partEndnotes  partKind = "endnotes"
	partSlide     partKind = "slide"
	partLayout    partKind = "layout"
	partMaster    partKind = "master"
	partNotes     partKind = "notes"
)

// partRef is one part to walk, with what the resolver needs to name it.
type partRef struct {
	name string
	kind partKind
	// slide is the slide number for a slide part, and the slide a notes part
	// belongs to. Zero elsewhere.
	slide int
}

// profile is what differs between the two formats: which parts to walk.
// Everything else about a picture is identical in a document and in a deck.
type profile struct {
	// format names the format in error messages, so a failure says "PowerPoint
	// file" rather than "OOXML part".
	format string
	// parts lists the parts to walk, in DOCUMENT ORDER. That order is what
	// makes the asset list read like the document rather than like a zip
	// directory.
	parts func(zr *zip.Reader) []partRef
}

var (
	docxProfile = profile{format: "Word document", parts: docxParts}
	pptxProfile = profile{format: "PowerPoint file", parts: pptxParts}
)

// scanOOXML is the whole scan: walk the profile's parts, resolve every blip's
// relationship to a media part, and group the occurrences by that part.
func scanOOXML(raw []byte, prof profile) (Inventory, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return Inventory{}, fmt.Errorf(
			"this %s could not be opened as a zip archive (%v); Word and PowerPoint files are "+
				"zip archives, so the file is either damaged or not really in that format, "+
				"re-export it from the source application and import it again", prof.format, err)
	}
	entries := indexEntries(zr)

	inv := Inventory{Applicable: true, Assets: []Asset{}}
	// assets is keyed by resolved media part; order preserves first appearance,
	// which IS document order because the parts are walked in it.
	assets := map[string]int{}
	warned := map[string]bool{}
	addWarning := func(code string) {
		if warned[code] {
			return
		}
		warned[code] = true
		inv.Warnings = append(inv.Warnings, code)
	}

	for _, part := range prof.parts(zr) {
		data, err := readEntry(entries, part.name)
		if err != nil {
			return Inventory{}, err
		}
		occs, pageBreaks, hiddenPart, err := walkPart(part.name, data)
		if err != nil {
			return Inventory{}, err
		}
		rels, err := readRels(entries, part.name)
		if err != nil {
			return Inventory{}, err
		}

		for _, occ := range occs {
			target, external := rels.resolve(occ.rid)
			if occ.link != "" {
				// A linked picture is external by definition: the relationship
				// names a file outside the archive.
				linkTarget, _ := rels.resolve(occ.link)
				target, external = linkTarget, true
			}
			if target == "" {
				// A blip whose relationship is missing points at nothing. It is
				// not a picture the user can act on, and inventing an asset for
				// it would put a row on the screen with no bytes behind it.
				addWarning(WarnUnreadablePart)
				continue
			}

			id := target
			if !external {
				id = path.Clean(path.Join(path.Dir(part.name), target))
			}

			idx, seen := assets[id]
			if !seen {
				asset := Asset{ID: id, Name: assetName(occ.name, id), Occurrences: []Occurrence{}}
				if external {
					asset.Linked = true
					asset.Format = FormatOther
					addWarning(WarnLinkedImages)
				} else {
					media, err := readEntry(entries, id)
					if err != nil {
						// The relationship points at a part the archive does not
						// hold. The asset is still listed with what is known,
						// because a picture the review hides is one the user
						// believes was reviewed.
						addWarning(WarnUnreadablePart)
					}
					asset.Bytes = len(media)
					asset.Format = Sniff(media)
					w, h, ok := Measure(media, asset.Format)
					asset.Width, asset.Height = w, h
					if !ok && asset.Format != FormatOther {
						addWarning(WarnUnreadablePart)
					}
				}
				inv.Assets = append(inv.Assets, asset)
				idx = len(inv.Assets) - 1
				assets[id] = idx
			}

			// An SVG picture is a PNG fallback in the relationship plus the SVG
			// itself in an extension. The asset keeps the fallback's ID, because
			// that is what the relationship points at and what an export must
			// keep valid, and reports FormatSVG, because SVG is what the user
			// sees and what decides which treatments may be offered.
			if occ.svgRID != "" && !external {
				if svgTarget, svgExternal := rels.resolve(occ.svgRID); svgTarget != "" && !svgExternal {
					inv.Assets[idx].Companion = path.Clean(path.Join(path.Dir(part.name), svgTarget))
					inv.Assets[idx].Format = FormatSVG
				}
			}

			inv.Assets[idx].Occurrences = append(inv.Assets[idx].Occurrences, Occurrence{
				Part:      part.name,
				Ordinal:   occ.ordinal,
				Kind:      occ.kind,
				Location:  locate(part, occ, pageBreaks > 0, hiddenPart),
				DisplayCX: occ.cx,
				DisplayCY: occ.cy,
			})
		}
	}
	return inv, nil
}

// assetName picks what to call a picture: what it calls itself, or the media
// part's base name when it says nothing.
func assetName(declared, id string) string {
	if name := strings.TrimSpace(declared); name != "" {
		return name
	}
	return path.Base(id)
}

// --- the walker ---------------------------------------------------------

// rawOcc is one picture occurrence as the XML walk saw it, before its
// relationship is resolved and before its place has a name.
type rawOcc struct {
	ordinal int
	kind    Kind
	// rid is the embed relationship id, link the external one. Exactly one of
	// the two is set.
	rid    string
	link   string
	svgRID string
	// name is what the enclosing shape calls itself.
	name string
	// hidden is set when the enclosing shape is marked hidden.
	hidden bool
	// page is how many page breaks were seen before this occurrence, so page
	// number is page+1 wherever page numbers are known at all.
	page int
	// cx and cy are the drawn frame in English Metric Units, when stated.
	cx, cy int
}

// containerElements are the elements that RESET the per-picture state: each one
// starts a new thing that can carry its own name, its own hidden flag and its
// own frame. Without the reset a picture inherits the name of the shape before
// it, which is the kind of wrong label a reviewer cannot detect.
var containerElements = map[string]bool{
	"pic": true, "drawing": true, "pict": true, "sp": true,
	"bg": true, "bgPr": true, "tc": true, "graphicFrame": true,
}

// pictureFrame is what one enclosing container says about the pictures inside
// it: what it is called, whether it is hidden, and how large it is drawn.
//
// It is a STACK rather than three variables because the two formats nest their
// containers differently: Word wraps a pic:pic (which carries the name) inside a
// w:drawing (which carries the frame), and PowerPoint states the frame AFTER the
// blip it belongs to. Either shape loses a value with a single set of variables,
// and a picture listed at the wrong size or under the previous picture's name is
// a label a reviewer cannot detect is wrong.
type pictureFrame struct {
	// startOcc is how many occurrences existed when this container opened, so
	// closing it knows which ones it is answering for.
	startOcc int
	cx, cy   int
	name     string
	hidden   bool
}

// walkPart token-scans ONE XML part and returns every picture occurrence in it.
//
// RawToken rather than Unmarshal, for two reasons that both matter: the parts
// carry namespace prefixes some producers never declare, which Token would
// reject and RawToken reads; and the export has to walk the same bytes with the
// same rule to splice a replacement in, which a struct-shaped unmarshal cannot
// do.
//
// @param partName the archive entry, for the error message
// @param data the part's bytes
// @return the occurrences, how many page breaks the part carried, whether the
//
//	part's root marks it hidden, and a parse error naming the part
func walkPart(partName string, data []byte) (occs []rawOcc, pageBreaks int, hiddenPart bool, err error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var stack []string
	var frames []pictureFrame
	var (
		root    = true
		inBlip  bool
		blip    rawOcc
		ordinal int
	)

	// top is the container the current element belongs to, or nil outside every
	// container (a blip used as a slide background can sit outside them all).
	top := func() *pictureFrame {
		if len(frames) == 0 {
			return nil
		}
		return &frames[len(frames)-1]
	}

	emit := func(occ rawOcc) {
		occ.ordinal = ordinal
		ordinal++
		occ.kind = kindFromStack(stack)
		occ.page = pageBreaks
		occs = append(occs, occ)
	}

	// closeFrame answers for every occurrence opened inside the container that
	// is closing, filling in only what is still missing. That is what lets an
	// inner container name a picture and an outer one size it.
	closeFrame := func(f pictureFrame) {
		for i := f.startOcc; i < len(occs); i++ {
			if occs[i].cx == 0 && occs[i].cy == 0 {
				occs[i].cx, occs[i].cy = f.cx, f.cy
			}
			if occs[i].name == "" {
				occs[i].name = f.name
			}
			if f.hidden {
				occs[i].hidden = true
			}
		}
	}

	for {
		tok, tokErr := dec.RawToken()
		if tokErr != nil {
			if errors.Is(tokErr, io.EOF) {
				break
			}
			return nil, 0, false, fmt.Errorf(
				"the part %q inside the file is not well-formed XML (%v); the file is damaged, "+
					"re-export it from the source application and import it again", partName, tokErr)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			local := t.Name.Local
			if root {
				root = false
				// A deck's slide states its own visibility on its root element.
				if local == "sld" && attrValue(t, "show") == "0" {
					hiddenPart = true
				}
			}
			if containerElements[local] {
				frames = append(frames, pictureFrame{startOcc: len(occs)})
			}
			switch local {
			case "cNvPr", "docPr":
				// The picture's own name and description, and the per-shape
				// hidden flag.
				if f := top(); f != nil {
					if name := attrValue(t, "name"); name != "" {
						f.name = name
					}
					if f.name == "" {
						f.name = attrValue(t, "descr")
					}
					if attrValue(t, "hidden") == "1" {
						f.hidden = true
					}
				}
			case "ext", "extent":
				// The drawn frame, in EMU. The FIRST one inside a container is
				// the picture's own; later ones belong to effects and crops, and
				// the extension list's own ext carries no size at all.
				if f := top(); f != nil && f.cx == 0 && f.cy == 0 {
					f.cx = atoiOrZero(attrValue(t, "cx"))
					f.cy = atoiOrZero(attrValue(t, "cy"))
				}
			case "lastRenderedPageBreak":
				pageBreaks++
			case "br":
				if attrValue(t, "type") == "page" {
					pageBreaks++
				}
			case "blip":
				inBlip = true
				blip = rawOcc{rid: attrValue(t, "embed"), link: attrValue(t, "link")}
			case "svgBlip":
				if inBlip {
					blip.svgRID = attrValue(t, "embed")
				}
			case "imagedata":
				// Legacy VML, inside w:pict. It is a single element with no
				// extension list, so it is complete the moment it is seen.
				rid := attrValue(t, "id")
				if rid == "" {
					rid = attrValue(t, "href")
				}
				if rid != "" {
					emit(rawOcc{rid: rid})
				}
			}
			stack = append(stack, local)

		case xml.EndElement:
			local := t.Name.Local
			if local == "blip" && inBlip {
				// Emitted at the END of the blip so the SVG extension inside it
				// has already been seen.
				inBlip = false
				if blip.rid != "" || blip.link != "" {
					emit(blip)
				}
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if containerElements[local] && len(frames) > 0 {
				f := frames[len(frames)-1]
				frames = frames[:len(frames)-1]
				closeFrame(f)
			}
		}
	}
	// A container left open by a truncated part still answers for its pictures.
	for len(frames) > 0 {
		f := frames[len(frames)-1]
		frames = frames[:len(frames)-1]
		closeFrame(f)
	}
	return occs, pageBreaks, hiddenPart, nil
}

// kindFromStack reads what encloses the blip, which is what decides whether
// removing this picture can delete an element or must overwrite bytes.
//
// The order of the tests is the containment order: a slide background contains
// a blipFill, and a p:pic contains one too, so the outer answer has to be
// checked first or every picture would come back a fill.
func kindFromStack(stack []string) Kind {
	for _, name := range stack {
		if name == "bg" || name == "bgPr" {
			return KindBackground
		}
	}
	for _, name := range stack {
		if name == "pic" || name == "drawing" || name == "pict" {
			return KindPicture
		}
	}
	return KindFill
}

// attrValue reads an attribute by LOCAL name, ignoring its prefix. RawToken
// leaves prefixes unresolved (r:embed arrives as prefix "r", local "embed"),
// and the prefix a producer chose is not something to match on.
func attrValue(start xml.StartElement, local string) string {
	for _, attr := range start.Attr {
		if attr.Name.Local == local {
			return attr.Value
		}
	}
	return ""
}

// atoiOrZero reads an attribute that should be a whole number, treating
// anything else as absent. A frame size that does not parse is a frame size the
// file did not state.
func atoiOrZero(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// --- relationships -------------------------------------------------------

// relTargets is one part's relationship table: id to target, plus whether the
// target lives outside the archive.
type relTargets map[string]relTarget

type relTarget struct {
	target   string
	external bool
}

// resolve answers a relationship id. An unknown id resolves to "", which the
// caller treats as a picture with nothing behind it.
func (r relTargets) resolve(id string) (target string, external bool) {
	if id == "" {
		return "", false
	}
	rel, ok := r[id]
	if !ok {
		return "", false
	}
	return rel.target, rel.external
}

// readRels reads the relationships of one part. An ABSENT rels file is not an
// error: a part with no relationships has no pictures, and that is an ordinary
// state rather than a damaged file.
func readRels(entries map[string]*zip.File, partName string) (relTargets, error) {
	relsPath := path.Join(path.Dir(partName), "_rels", path.Base(partName)+".rels")
	if _, ok := entries[relsPath]; !ok {
		return relTargets{}, nil
	}
	data, err := readEntry(entries, relsPath)
	if err != nil {
		return nil, err
	}

	var doc struct {
		Rel []struct {
			ID         string `xml:"Id,attr"`
			Target     string `xml:"Target,attr"`
			TargetMode string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf(
			"the relationships part %q inside the file is not well-formed XML (%v); the file is "+
				"damaged, re-export it from the source application and import it again", relsPath, err)
	}
	out := make(relTargets, len(doc.Rel))
	for _, rel := range doc.Rel {
		out[rel.ID] = relTarget{
			target:   rel.Target,
			external: strings.EqualFold(rel.TargetMode, "External"),
		}
	}
	return out, nil
}

// --- archive helpers ------------------------------------------------------

// indexEntries maps entry name to entry, so a part is found once rather than by
// walking the directory for every lookup.
func indexEntries(zr *zip.Reader) map[string]*zip.File {
	out := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		out[f.Name] = f
	}
	return out
}

// readEntry reads one archive entry whole. A missing entry is an error here;
// callers that treat absence as normal check the map first.
func readEntry(entries map[string]*zip.File, name string) ([]byte, error) {
	entry, ok := entries[name]
	if !ok {
		return nil, fmt.Errorf("the file does not contain the part %q that another part points at; "+
			"the file is incomplete, re-export it from the source application and import it again", name)
	}
	rc, err := entry.Open()
	if err != nil {
		return nil, fmt.Errorf("could not read %q inside the file: %w", name, err)
	}
	data, readErr := io.ReadAll(rc)
	// The reader is closed explicitly rather than deferred and ignored: a part
	// that failed to decompress reports it at CLOSE, so discarding that error
	// would turn a damaged archive into a silently short picture.
	closeErr := rc.Close()
	if readErr != nil {
		return nil, fmt.Errorf("could not read %q inside the file: %w", name, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("the part %q inside the file did not decompress cleanly (%w); the "+
			"file is damaged, re-export it from the source application and import it again",
			name, closeErr)
	}
	return data, nil
}

// --- the two part lists ---------------------------------------------------

var (
	slideRe   = regexp.MustCompile(`^ppt/slides/slide(\d+)\.xml$`)
	layoutRe  = regexp.MustCompile(`^ppt/slideLayouts/slideLayout(\d+)\.xml$`)
	masterRe  = regexp.MustCompile(`^ppt/slideMasters/slideMaster(\d+)\.xml$`)
	notesRe   = regexp.MustCompile(`^ppt/notesSlides/notesSlide(\d+)\.xml$`)
	headerRe  = regexp.MustCompile(`^word/header(\d+)\.xml$`)
	footerRe  = regexp.MustCompile(`^word/footer(\d+)\.xml$`)
	numberSuf = regexp.MustCompile(`(\d+)`)
)

// pptxParts lists a deck's parts in the order a reader meets them: the slides
// first, then the furniture behind them, then the notes.
func pptxParts(zr *zip.Reader) []partRef {
	var slides, layouts, masters, notes []partRef
	for _, f := range zr.File {
		switch {
		case slideRe.MatchString(f.Name):
			slides = append(slides, partRef{name: f.Name, kind: partSlide, slide: partNumber(slideRe, f.Name)})
		case layoutRe.MatchString(f.Name):
			layouts = append(layouts, partRef{name: f.Name, kind: partLayout})
		case masterRe.MatchString(f.Name):
			masters = append(masters, partRef{name: f.Name, kind: partMaster})
		case notesRe.MatchString(f.Name):
			notes = append(notes, partRef{name: f.Name, kind: partNotes})
		}
	}
	byNumber(slides)
	byNumber(layouts)
	byNumber(masters)
	byNumber(notes)

	// A notes part says nothing about which slide it belongs to; the SLIDE says
	// so, through its own relationships. Resolving it that way is what keeps a
	// reordered deck labelling its notes correctly, and it mirrors what
	// convert/pptx.go already does for the notes text.
	owners := notesOwners(zr, slides)
	for i := range notes {
		notes[i].slide = owners[notes[i].name]
	}

	parts := make([]partRef, 0, len(slides)+len(layouts)+len(masters)+len(notes))
	parts = append(parts, slides...)
	parts = append(parts, layouts...)
	parts = append(parts, masters...)
	parts = append(parts, notes...)
	return parts
}

// notesOwners maps a notes part to the slide that points at it.
func notesOwners(zr *zip.Reader, slides []partRef) map[string]int {
	entries := indexEntries(zr)
	owners := map[string]int{}
	for _, slide := range slides {
		rels, err := readRels(entries, slide.name)
		if err != nil {
			continue
		}
		for _, rel := range rels {
			if rel.external || !strings.Contains(rel.target, "notesSlide") {
				continue
			}
			owners[path.Clean(path.Join(path.Dir(slide.name), rel.target))] = slide.slide
		}
	}
	return owners
}

// docxParts lists a document's parts in reading order: the body, then the
// running heads and feet, then the notes.
func docxParts(zr *zip.Reader) []partRef {
	var body, headers, footers, notes []partRef
	for _, f := range zr.File {
		switch {
		case f.Name == "word/document.xml":
			body = append(body, partRef{name: f.Name, kind: partBody})
		case headerRe.MatchString(f.Name):
			headers = append(headers, partRef{name: f.Name, kind: partHeader})
		case footerRe.MatchString(f.Name):
			footers = append(footers, partRef{name: f.Name, kind: partFooter})
		case f.Name == "word/footnotes.xml":
			notes = append(notes, partRef{name: f.Name, kind: partFootnotes})
		case f.Name == "word/endnotes.xml":
			notes = append(notes, partRef{name: f.Name, kind: partEndnotes})
		}
	}
	byNumber(headers)
	byNumber(footers)
	sort.Slice(notes, func(i, j int) bool { return notes[i].name < notes[j].name })

	parts := make([]partRef, 0, len(body)+len(headers)+len(footers)+len(notes))
	parts = append(parts, body...)
	parts = append(parts, headers...)
	parts = append(parts, footers...)
	parts = append(parts, notes...)
	return parts
}

// byNumber sorts parts NUMERICALLY by the number in their name, so slide10
// comes after slide9 rather than after slide1.
func byNumber(parts []partRef) {
	sort.Slice(parts, func(i, j int) bool {
		return partNumber(numberSuf, parts[i].name) < partNumber(numberSuf, parts[j].name)
	})
}

// partNumber reads the number a part name carries, or 0 when it carries none.
func partNumber(re *regexp.Regexp, name string) int {
	m := re.FindStringSubmatch(name)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[len(m)-1])
	if err != nil {
		return 0
	}
	return n
}
