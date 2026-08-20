// Package imaging is the engine's picture half: it answers "what pictures are
// in this document", and (later) "what bytes replace this one".
//
// It is a package of its own, beside convert/ and exportfmt/, because BOTH of
// them need it and neither may own it: the scan reads the bytes captured at
// IMPORT (convert's side of the world) and the treatments write the bytes an
// EXPORT produces (exportfmt's side). A package owned by either would have to
// be imported by the other, and the two are deliberately independent.
//
// It is UI-agnostic like the rest of the engine (CLAUDE.md §4): every function
// here takes bytes and returns bytes or plain data. Nothing in this package
// sees a filesystem path, a Wails context or a filename the user chose, and
// nothing here decides what the interface shows.
//
// Its whole dependency list is the standard library: archive/zip, encoding/xml,
// image, image/png, image/jpeg and arithmetic. That is not frugality for its own
// sake: the owner decided against a new Go module for this feature, so anything
// this package cannot do with the standard library is something the feature does
// not do.
package imaging

// The PNG and JPEG decoders register themselves with image.DecodeConfig when
// their packages are imported, which is what lets Measure read a header without
// naming a format-specific decoder.
import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strconv"
	"strings"
)

// Format is what a picture's BYTES turn out to be. It is sniffed from the
// content and never from the part's extension: a part named image3.png holding
// JPEG bytes is common enough in real documents to matter, and re-encoding a
// JPEG as PNG under its old name would leave the archive's [Content_Types].xml
// describing a file that is no longer there.
type Format string

const (
	FormatPNG  Format = "png"
	FormatJPEG Format = "jpeg"
	FormatSVG  Format = "svg"
	// FormatOther is every picture format this application cannot redraw:
	// emf, wmf, gif, tiff, bmp and anything unrecognised. Such a picture is
	// still LISTED, because an image the user cannot see in the review is an
	// image they believe was reviewed.
	FormatOther Format = "other"
)

// AllFormats is the ordered list the frontend mirrors, held to the Go side by
// the parity guard exactly as the categories are.
var AllFormats = []Format{FormatPNG, FormatJPEG, FormatSVG, FormatOther}

// Kind is what ENCLOSES a picture in the document XML, and it is a separate
// question from what the picture is: it decides what removing it can mean. A
// picture element can be deleted; a shape's fill or a slide background has no
// element of its own to delete, so removing it means overwriting its bytes.
type Kind string

const (
	// KindPicture is a picture element in its own right: p:pic in a deck,
	// w:drawing or the legacy w:pict in a document.
	KindPicture Kind = "picture"
	// KindFill is a picture used as the fill of a shape or a table cell.
	KindFill Kind = "fill"
	// KindBackground is a slide or master background picture.
	KindBackground Kind = "background"
)

// AllKinds is the ordered list the frontend mirrors.
var AllKinds = []Kind{KindPicture, KindFill, KindBackground}

// Reason codes explain why a document has no image review. They are CODES and
// not sentences because copy.js is the single home for user-visible strings
// (frontend/CLAUDE.md): a sentence returned from Go is a string that lives
// outside the place the interface's copy is reviewed.
const (
	ReasonFormatNotSupported = "format_not_supported"
	ReasonPDFImagesRemoved   = "pdf_images_removed"
)

// Warning codes are per-document notes worth showing above the list.
const (
	// WarnUnreadablePart says at least one picture part could not be decoded.
	// The asset is still listed, with its size in bytes and no dimensions.
	WarnUnreadablePart = "unreadable_part"
	// WarnLinkedImages says at least one picture lives OUTSIDE the file, so
	// its bytes are not here to change.
	WarnLinkedImages = "linked_images"
)

// Occurrence is one PLACE an asset is used.
type Occurrence struct {
	// Part is the XML part the picture element sits in
	// ("ppt/slides/slide4.xml", "word/header2.xml").
	Part string `json:"part"`
	// Ordinal is this occurrence's index among the picture elements of that
	// part, from 0. Part plus Ordinal identifies the element without carrying
	// a byte offset, which is what lets an export re-scan its own rewritten
	// bytes and still find the same picture.
	Ordinal int `json:"ordinal"`
	// Kind is what encloses the blip, and it decides what removing it means.
	Kind Kind `json:"kind"`
	// Location is the ready-to-print place, in the document's own words:
	// "Slide 4", "Page 2", "Header", "Slide master", "Hidden slide 7".
	Location string `json:"location"`
	// DisplayCX and DisplayCY are the frame the picture is drawn in, in
	// English Metric Units (914400 per inch). Zero when the source does not
	// state it, which is normal for a fill and for a background.
	DisplayCX int `json:"displayCX"`
	DisplayCY int `json:"displayCY"`
}

// Asset is ONE picture file inside the document archive. It is the unit a
// decision is attached to: a logo used on five slides is one Asset with five
// Occurrences, because a user reviewing "the logo" is answering one question.
type Asset struct {
	// ID is the archive part path, e.g. "ppt/media/image3.png". It is stable
	// across re-imports of the same file, which is what lets a decision taken
	// in one session be re-applied in the next.
	ID string `json:"id"`
	// Name is what the picture calls itself: the first occurrence's name or
	// description attribute, falling back to the part's base name. A user
	// recognises "Acme group logo"; nobody recognises "image7.png".
	Name string `json:"name"`
	// Format is sniffed from the BYTES, never from the extension.
	Format Format `json:"format"`
	// Bytes is the part's decompressed size.
	Bytes int `json:"bytes"`
	// Width and Height are pixels for raster assets, and the declared or
	// viewBox size for SVG. Zero for a format we cannot read.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Companion is the SVG part of an SVG picture, whose primary part is the
	// PNG fallback Office writes beside it. Empty for everything else. A
	// treatment must reach BOTH parts or Office shows the untreated one.
	Companion string `json:"companion,omitempty"`
	// Linked marks an image referenced by a link rather than embedded: its
	// bytes are NOT in the archive. It can be removed and nothing else.
	Linked bool `json:"linked,omitempty"`
	// Occurrences are every place this asset is used, in document order.
	Occurrences []Occurrence `json:"occurrences"`
	// Decision is what the user has decided about this picture, or the zero
	// value (which reads as keep) when they have decided nothing.
	//
	// It travels WITH the asset so the review screen has one call rather than
	// two, and therefore cannot draw a row whose decision it has not read. The
	// scan itself never fills it: the scan describes the file, and a decision is
	// something the user did.
	Decision Decision `json:"decision"`
}

// Inventory is what crosses the bridge: one document's whole picture answer,
// including the answer "this format has no image review and here is why".
type Inventory struct {
	// Applicable is false for every format that has no image review, and
	// Reason is then one of the reason codes above.
	Applicable bool   `json:"applicable"`
	Reason     string `json:"reason,omitempty"`
	// Assets is empty rather than absent for an applicable document with no
	// pictures: an empty list is an answer, not a failure.
	Assets []Asset `json:"assets"`
	// Warnings are per-document notes, as codes.
	Warnings []string `json:"warnings,omitempty"`
}

// NotApplicable builds the answer for a format that has no image review.
func NotApplicable(reason string) Inventory {
	return Inventory{Applicable: false, Reason: reason, Assets: []Asset{}}
}

// maxSVGSniff is how far into a part we look for an <svg element before giving
// up. An SVG file states itself in its first tag; anything further in is a file
// that merely mentions SVG.
const maxSVGSniff = 1024

// Sniff reports what a picture part's bytes actually are.
//
// It reads signatures only. It never decodes pixels, because answering "what is
// this" must not cost what decoding a 40-megapixel screenshot costs.
func Sniff(raw []byte) Format {
	switch {
	case len(raw) >= 8 && bytes.HasPrefix(raw, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return FormatPNG
	case len(raw) >= 3 && bytes.HasPrefix(raw, []byte{0xFF, 0xD8, 0xFF}):
		return FormatJPEG
	case looksLikeSVG(raw):
		return FormatSVG
	default:
		return FormatOther
	}
}

// looksLikeSVG applies the two-part rule: the first non-whitespace bytes are an
// XML or SVG opening tag, AND an <svg element appears near the start. Either
// half alone matches too much: every OOXML part starts with <?xml, and a random
// document can mention "<svg" in prose.
func looksLikeSVG(raw []byte) bool {
	head := raw
	if len(head) > maxSVGSniff {
		head = head[:maxSVGSniff]
	}
	trimmed := bytes.TrimLeft(head, " \t\r\n\uFEFF")
	if !bytes.HasPrefix(trimmed, []byte("<?xml")) && !bytes.HasPrefix(trimmed, []byte("<svg")) &&
		!bytes.HasPrefix(trimmed, []byte("<!DOCTYPE svg")) {
		return false
	}
	return bytes.Contains(head, []byte("<svg"))
}

// Measure reports a picture part's pixel size without materialising it.
//
// Raster formats go through image.DecodeConfig, which reads the header only.
// SVG is measured from its own attributes. Anything else, and anything that
// fails to decode, comes back 0x0 with ok false: the asset is still listed, and
// the caller records the unreadable_part warning.
//
// @param raw one picture part
// @param format what Sniff said it is
// @return width, height in pixels, and whether they could be read at all
func Measure(raw []byte, format Format) (w, h int, ok bool) {
	switch format {
	case FormatPNG, FormatJPEG:
		cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			return 0, 0, false
		}
		return cfg.Width, cfg.Height, true
	case FormatSVG:
		return measureSVG(raw)
	default:
		return 0, 0, false
	}
}

// measureSVG reads the root <svg> element's declared size, falling back to the
// third and fourth values of its viewBox.
//
// The fallback is the common case rather than an edge case: an SVG exported for
// Office routinely declares only a viewBox, and a picture with no size is one
// the box treatment cannot draw a rectangle for.
func measureSVG(raw []byte) (w, h int, ok bool) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	// An SVG may carry entity declarations and undeclared prefixes; RawToken
	// keeps those readable instead of turning the file into a parse error.
	for {
		tok, err := dec.RawToken()
		if err != nil {
			// Both ends the same way: an SVG whose root element never arrived
			// states no size, and neither does one that will not parse.
			if errors.Is(err, io.EOF) {
				return 0, 0, false
			}
			return 0, 0, false
		}
		start, isStart := tok.(xml.StartElement)
		if !isStart || start.Name.Local != "svg" {
			continue
		}
		var width, height, viewBox string
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "width":
				width = attr.Value
			case "height":
				height = attr.Value
			case "viewBox":
				viewBox = attr.Value
			}
		}
		if wv, okw := svgLength(width); okw {
			if hv, okh := svgLength(height); okh {
				return wv, hv, true
			}
		}
		if wv, hv, okv := svgViewBox(viewBox); okv {
			return wv, hv, true
		}
		return 0, 0, false
	}
}

// svgLength parses an SVG length that is a plain number or a px value. Every
// other unit (em, %, mm) depends on a rendering context this application does
// not have, so it reads as "not stated" rather than as a guess.
func svgLength(v string) (int, bool) {
	v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "px"))
	if v == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		return 0, false
	}
	return int(f + 0.5), true
}

// svgViewBox reads "minX minY width height", separated by spaces or commas.
func svgViewBox(v string) (w, h int, ok bool) {
	fields := strings.FieldsFunc(v, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r'
	})
	if len(fields) != 4 {
		return 0, 0, false
	}
	wf, errW := strconv.ParseFloat(fields[2], 64)
	hf, errH := strconv.ParseFloat(fields[3], 64)
	if errW != nil || errH != nil || wf <= 0 || hf <= 0 {
		return 0, 0, false
	}
	return int(wf + 0.5), int(hf + 0.5), true
}

// MIMEType is the media type a data URL carries for one sniffed format. SVG is
// listed here as well because an SVG asset is handed to the interface as it is
// rather than thumbnailed, and it must arrive with the type that makes the
// WebView render it inside an <img>.
func MIMEType(format Format) (string, error) {
	switch format {
	case FormatPNG:
		return "image/png", nil
	case FormatJPEG:
		return "image/jpeg", nil
	case FormatSVG:
		return "image/svg+xml", nil
	default:
		return "", fmt.Errorf("this picture is not a PNG, JPEG or SVG file, so it has no preview; " +
			"it is still listed and can still be removed")
	}
}
