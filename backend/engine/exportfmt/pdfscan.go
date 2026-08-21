// engine/exportfmt/pdfscan.go — the whole-file PDF leak scanner.
//
// The question this file answers: does a produced PDF still carry a given
// original string ANYWHERE, not only in the page text an extractor happens to
// read back? A PDF can hide a value in a compressed content stream, an Info
// dictionary, an XMP packet, an annotation, an outline title, or in a
// superseded object left readable by an incremental save. So the scan walks
// the RAW BYTES: every "N G obj ... endobj" span in the file, live or not,
// with every FlateDecode stream inflated and every string object decoded in
// the encodings a PDF can carry text in (literal with escapes, hex,
// UTF-16BE).
//
// It reports each hit with the SURFACE it sits in, so a refusal can name the
// culprit ("the Info dictionary", "an annotation") instead of an offset.
//
// Honest limits, stated here because a guarantee is only as good as its
// stated scope: the scan proves the ABSENCE OF THE NEEDLES AS TEXT. It cannot
// prove anything about a picture's pixels (that is the image review's job),
// about text rasterised into an image, or about a value nobody asked it to
// look for. Streams under a filter it cannot decode (DCTDecode and the other
// image codecs) are scanned as raw bytes and reported as unscannable by
// filter name, so the caller can say so rather than imply they were read.
package exportfmt

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf16"
)

// PDFLeakFinding is one needle found in one object of the scanned file.
type PDFLeakFinding struct {
	// Needle is the string that was found, exactly as the caller passed it.
	// The caller decides how to redact it in any message a user sees.
	Needle string
	// Surface names where it sits, in words a refusal can use.
	Surface string
	// Object is the PDF object number the hit is in.
	Object int
}

// pdfObjStart matches the "N G obj" header of every object in the file. The
// scan finds objects by this raw-byte shape rather than through the xref
// table ON PURPOSE: an incremental save leaves superseded objects in the file
// with no live xref entry, and those are exactly the leak this scan exists to
// catch.
var pdfObjStart = regexp.MustCompile(`(?s)(\d+)\s+\d+\s+obj\b`)

// pdfTrailerInfo finds the trailer's /Info reference, so the Info
// dictionary's object can be named as what it is.
var pdfTrailerInfo = regexp.MustCompile(`/Info\s+(\d+)\s+\d+\s+R`)

// ScanPDFForNeedles scans every object of pdfBytes for every needle.
//
// Matching is case-insensitive and whitespace-collapsed (matching what the
// registry's own self-check semantics are), and additionally
// whitespace-STRIPPED over the concatenated string objects of each stream, so
// a value a writer split across kerned Tj segments is still found.
//
// The second return value lists the surfaces that could NOT be decoded (a
// stream under an image codec), scanned as raw bytes only, so a caller can
// report the honest limit instead of implying full coverage.
func ScanPDFForNeedles(pdfBytes []byte, needles []string) (findings []PDFLeakFinding, unscannable []string, err error) {
	if len(needles) == 0 {
		return nil, nil, nil
	}
	// Pre-normalise the needles once: lowercase + collapsed, and a
	// space-stripped form for the concatenated-strings view.
	type needleForm struct {
		original  string
		collapsed string
		stripped  string
	}
	forms := make([]needleForm, 0, len(needles))
	for _, n := range needles {
		c := collapseSpaces(strings.ToLower(n))
		if c == "" {
			continue
		}
		forms = append(forms, needleForm{original: n, collapsed: c, stripped: strings.ReplaceAll(c, " ", "")})
	}

	infoObjects := map[int]bool{}
	for _, m := range pdfTrailerInfo.FindAllSubmatch(pdfBytes, -1) {
		infoObjects[atoiBytes(m[1])] = true
	}
	// Everything after the first %%EOF belongs to an appended (incremental)
	// body; a hit there is named as such, because it means the file carries
	// a superseded generation of itself.
	firstBodyEnd := bytes.Index(pdfBytes, []byte("%%EOF"))

	seen := map[string]bool{} // dedupe: one finding per (object, needle)
	starts := pdfObjStart.FindAllSubmatchIndex(pdfBytes, -1)
	for i, loc := range starts {
		objNum := atoiBytes(pdfBytes[loc[2]:loc[3]])
		bodyStart := loc[1]
		bodyEnd := len(pdfBytes)
		if end := bytes.Index(pdfBytes[bodyStart:], []byte("endobj")); end >= 0 {
			bodyEnd = bodyStart + end
		} else if i+1 < len(starts) {
			// A damaged object without endobj: stop at the next object header
			// rather than swallowing the rest of the file into one surface.
			bodyEnd = starts[i+1][0]
		}
		body := pdfBytes[bodyStart:bodyEnd]

		// Split the object into its dictionary part and its stream payload.
		dict := body
		var stream []byte
		if at := bytes.Index(body, []byte("stream")); at >= 0 {
			dict = body[:at]
			payload := body[at+len("stream"):]
			// The keyword is followed by CRLF or LF; the payload runs to the
			// matching endstream.
			payload = bytes.TrimPrefix(payload, []byte("\r"))
			payload = bytes.TrimPrefix(payload, []byte("\n"))
			if end := bytes.LastIndex(payload, []byte("endstream")); end >= 0 {
				payload = payload[:end]
			}
			stream = payload
		}

		surface := classifyPDFSurface(dict, stream, infoObjects[objNum])
		if firstBodyEnd >= 0 && loc[0] > firstBodyEnd {
			surface += " in an appended incremental body"
		}

		// Decode the stream where a text filter allows it; note the surfaces
		// that stay raw.
		decoded, filterName := decodePDFStream(dict, stream)
		if filterName != "" {
			unscannable = append(unscannable,
				fmt.Sprintf("object %d (%s): stream filter %s is not decodable here, raw bytes scanned only", objNum, surface, filterName))
		}

		// The three views of this object's text: the dictionary with every
		// string object decoded, the decoded stream likewise, and the
		// concatenation of the stream's string objects alone (which is what
		// re-joins a value a writer split across Tj segments).
		views := []string{
			collapseSpaces(strings.ToLower(decodeAllPDFStrings(dict))),
			collapseSpaces(strings.ToLower(decodeAllPDFStrings(decoded))),
			collapseSpaces(strings.ToLower(concatPDFStringObjects(decoded))),
		}
		for _, f := range forms {
			key := fmt.Sprintf("%d\x00%s", objNum, f.original)
			if seen[key] {
				continue
			}
			for vi, v := range views {
				hit := strings.Contains(v, f.collapsed)
				if !hit && vi == 2 {
					// The concatenated view also matches with spaces
					// stripped from both sides, because a kerned split can
					// fall inside a word.
					hit = strings.Contains(strings.ReplaceAll(v, " ", ""), f.stripped)
				}
				if hit {
					findings = append(findings, PDFLeakFinding{Needle: f.original, Surface: surface, Object: objNum})
					seen[key] = true
					break
				}
			}
		}
	}
	return findings, unscannable, nil
}

// PDFBodyCount counts the file's %%EOF markers: a freshly written PDF has
// exactly one, and every additional one is an appended incremental-update
// body carrying a superseded generation of the document.
func PDFBodyCount(pdfBytes []byte) int {
	return bytes.Count(pdfBytes, []byte("%%EOF"))
}

// PDFHasIncrementalUpdate reports whether the file has the shape of an
// incremental save: more than one body, or a trailer chaining to a previous
// cross-reference section with /Prev.
func PDFHasIncrementalUpdate(pdfBytes []byte) bool {
	if PDFBodyCount(pdfBytes) > 1 {
		return true
	}
	// /Prev in a trailer or cross-reference stream dictionary points at the
	// superseded body's xref. Whole-token: /Prev followed by a digit.
	return regexp.MustCompile(`/Prev\s+\d`).Match(pdfBytes)
}

// classifyPDFSurface names an object in the words a refusal uses, from the
// keys of its own dictionary and the shape of its stream.
func classifyPDFSurface(dict, stream []byte, isInfo bool) string {
	d := string(dict)
	switch {
	case isInfo:
		return "the Info dictionary"
	case strings.Contains(d, "/Type /Metadata") || strings.Contains(d, "/Type/Metadata"):
		return "the XMP metadata packet"
	case strings.Contains(d, "/Type /Annot") || strings.Contains(d, "/Type/Annot"):
		return "an annotation"
	case strings.Contains(d, "/Title") && strings.Contains(d, "/Parent"):
		return "an outline item"
	case strings.Contains(d, "/Subtype /Image") || strings.Contains(d, "/Subtype/Image"):
		return "an image stream"
	case strings.Contains(d, "/Type /Catalog") || strings.Contains(d, "/Type/Catalog"):
		return "the document catalog"
	case strings.Contains(d, "/Type /Page ") || strings.Contains(d, "/Type/Page"):
		return "a page dictionary"
	case len(stream) > 0:
		// A stream whose decoded bytes carry text-showing operators is page
		// content; anything else is a generic stream.
		if decoded, _ := decodePDFStream(dict, stream); bytes.Contains(decoded, []byte("BT")) &&
			(bytes.Contains(decoded, []byte("Tj")) || bytes.Contains(decoded, []byte("TJ"))) {
			return "a content stream"
		}
		return "a stream object"
	default:
		return "a dictionary object"
	}
}

// decodePDFStream inflates a /FlateDecode stream. For a filter it cannot
// decode (the image codecs, CCITT, JBIG2, JPX, LZW cascades and so on) it
// returns the raw bytes plus the filter's name, so the caller can report the
// surface as scanned-raw rather than decoded.
func decodePDFStream(dict, stream []byte) (decoded []byte, undecodedFilter string) {
	if len(stream) == 0 {
		return nil, ""
	}
	d := string(dict)
	filter := ""
	if m := regexp.MustCompile(`/Filter\s*/(\w+)`).FindStringSubmatch(d); m != nil {
		filter = m[1]
	} else if m := regexp.MustCompile(`/Filter\s*\[\s*/(\w+)`).FindStringSubmatch(d); m != nil {
		// A filter array: only a single-element [/FlateDecode] is decodable
		// here; a cascade is reported by its first name.
		filter = m[1]
		if strings.Count(d[strings.Index(d, "/Filter"):], "/") > 2 {
			return stream, filter + " (filter cascade)"
		}
	}
	switch filter {
	case "":
		return stream, ""
	case "FlateDecode":
		// zlib first (the correct encoding), raw deflate as the fallback
		// some writers emit.
		if r, err := zlib.NewReader(bytes.NewReader(stream)); err == nil {
			if out, err := io.ReadAll(r); err == nil {
				return out, ""
			}
		}
		if out, err := io.ReadAll(flate.NewReader(bytes.NewReader(stream))); err == nil {
			return out, ""
		}
		return stream, "FlateDecode (corrupt)"
	default:
		return stream, filter
	}
}

// decodeAllPDFStrings returns data with every literal and hex string object
// it contains decoded and appended, so a needle stored escaped, split by an
// octal sequence, or as UTF-16BE is still visible to a plain-text search. The
// original bytes are kept too: a needle in plain bytes outside any string
// (an XMP packet, a content stream's text between operators) must stay
// findable.
func decodeAllPDFStrings(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var out strings.Builder
	out.Write(data)
	for _, s := range extractPDFStringObjects(data) {
		out.WriteByte('\n')
		out.WriteString(s)
	}
	return out.String()
}

// concatPDFStringObjects joins the decoded string objects of data in file
// order with NO separator: the view in which "(Har) 8 (riet)" reads
// "Harriet" again.
func concatPDFStringObjects(data []byte) string {
	return strings.Join(extractPDFStringObjects(data), "")
}

// extractPDFStringObjects decodes every (literal) and <hex> string in data,
// in order. A decoded value starting with the UTF-16BE byte-order mark is
// transcoded to UTF-8, which is how a PDF carries text outside Latin-1.
func extractPDFStringObjects(data []byte) []string {
	var out []string
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '(':
			s, next := decodePDFLiteralString(data, i)
			out = append(out, decodePDFTextEncoding(s))
			i = next - 1
		case '<':
			// "<<" opens a dictionary, not a string.
			if i+1 < len(data) && data[i+1] == '<' {
				i++
				continue
			}
			s, next := decodePDFHexString(data, i)
			out = append(out, decodePDFTextEncoding(s))
			i = next - 1
		}
	}
	return out
}

// decodePDFLiteralString decodes the literal string starting at data[start]
// (which is '('), honouring backslash escapes, octal sequences, escaped
// newlines and balanced nested parentheses. It returns the decoded bytes and
// the index just past the closing ')'.
func decodePDFLiteralString(data []byte, start int) ([]byte, int) {
	var out []byte
	depth := 1
	i := start + 1
	for i < len(data) && depth > 0 {
		c := data[i]
		switch c {
		case '\\':
			if i+1 >= len(data) {
				i++
				continue
			}
			e := data[i+1]
			switch e {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case '(', ')', '\\':
				out = append(out, e)
			case '\r':
				// A line continuation: the escaped newline vanishes. \r\n
				// counts as one newline.
				if i+2 < len(data) && data[i+2] == '\n' {
					i++
				}
			case '\n':
				// Line continuation, nothing emitted.
			default:
				if e >= '0' && e <= '7' {
					// Up to three octal digits.
					v, n := 0, 0
					for n < 3 && i+1+n < len(data) && data[i+1+n] >= '0' && data[i+1+n] <= '7' {
						v = v*8 + int(data[i+1+n]-'0')
						n++
					}
					out = append(out, byte(v))
					i += n - 1
				} else {
					// An unknown escape: the backslash is dropped, the
					// character kept (ISO 32000-1 §7.3.4.2).
					out = append(out, e)
				}
			}
			i += 2
		case '(':
			depth++
			out = append(out, c)
			i++
		case ')':
			depth--
			if depth > 0 {
				out = append(out, c)
			}
			i++
		default:
			out = append(out, c)
			i++
		}
	}
	return out, i
}

// decodePDFHexString decodes the hex string starting at data[start] (which is
// '<'), ignoring whitespace and padding an odd final digit with zero, and
// returns the decoded bytes and the index just past the closing '>'.
func decodePDFHexString(data []byte, start int) ([]byte, int) {
	var out []byte
	hi := -1
	i := start + 1
	for ; i < len(data); i++ {
		c := data[i]
		if c == '>' {
			i++
			break
		}
		var v int
		switch {
		case c >= '0' && c <= '9':
			v = int(c - '0')
		case c >= 'a' && c <= 'f':
			v = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v = int(c-'A') + 10
		default:
			continue // whitespace inside a hex string is legal and ignored
		}
		if hi < 0 {
			hi = v
		} else {
			out = append(out, byte(hi*16+v))
			hi = -1
		}
	}
	if hi >= 0 {
		out = append(out, byte(hi*16))
	}
	return out, i
}

// decodePDFTextEncoding turns a decoded string object's bytes into UTF-8
// text: UTF-16BE when the byte-order mark says so, the raw bytes otherwise
// (PDFDocEncoding and Latin-1 agree on every character the needles use, and
// a byte-for-byte view errs towards finding more, which is the safe
// direction for a leak check).
func decodePDFTextEncoding(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		u := make([]uint16, 0, (len(b)-2)/2)
		for i := 2; i+1 < len(b); i += 2 {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		}
		return string(utf16.Decode(u))
	}
	return string(b)
}

// collapseSpaces lowers every whitespace run to one space and trims the ends,
// the same comparison the export self-check uses, so the scan and the check
// cannot disagree about what "contains" means.
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// atoiBytes parses an unsigned decimal the regexp already validated.
func atoiBytes(b []byte) int {
	n := 0
	for _, c := range b {
		n = n*10 + int(c-'0')
	}
	return n
}
