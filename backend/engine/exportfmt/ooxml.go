// engine/exportfmt/ooxml.go — the shared OOXML text-run replacement
// used for docx (w:t inside w:p) and pptx (a:t
// inside a:p) parts alike.
//
// Algorithm, exactly as specified in the plan:
//  1. Scan the part with encoding/xml tokens, buffering text nodes per
//     paragraph.
//  2. Concatenate the paragraph's text nodes; record each node's offset
//     range in the concatenated (decoded) text AND its raw byte range in
//     the original part.
//  3. Run the same span machinery as the body pipeline (Config
//     .Replacements: deterministic passes + registry mapping).
//  4. Splice replacements back per node by offset: a replacement
//     contained in one node edits that node; a replacement spanning
//     nodes is written wholly into the node where it STARTS and the
//     covered tails of later nodes are emptied. A spanning replacement
//     therefore adopts its first run's formatting. LIMITATION, accepted
//     and documented in the README: run-splitting with format
//     preservation is a v3 candidate.
//  5. Nothing outside text-node CONTENT is ever touched: the rewrite
//     splices into the original bytes, so every tag, attribute, comment
//     and namespace survives byte-for-byte.
package exportfmt

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
)

// textNode is one <w:t>/<a:t> content range.
type textNode struct {
	rawStart, rawEnd int    // byte range of the CONTENT in the part
	text             string // decoded content
	decStart         int    // offset in the paragraph's concatenated text
}

// splice is one raw-byte replacement to apply to the part. Nil content is a
// DELETION, which is how the picture pass removes an element.
type splice struct {
	rawStart, rawEnd int
	content          []byte
}

// applySplices rewrites data with every splice applied.
//
// One implementation for both passes over a part, because "replace these byte
// ranges and touch nothing else" is one operation: a second copy of it is a
// second place for an off-by-one to live, and the two passes would then disagree
// about what "untouched" means.
//
// The splices are sorted and copied through front to back rather than edited in
// place, so an earlier splice cannot shift a later one's offsets. Overlapping
// ranges cannot occur (a text node and a picture element are rewritten in
// separate passes) and are skipped rather than allowed to corrupt the part.
func applySplices(data []byte, splices []splice) []byte {
	if len(splices) == 0 {
		return data
	}
	ordered := append([]splice(nil), splices...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].rawStart < ordered[j].rawStart })

	out := make([]byte, 0, len(data)+256)
	last := 0
	for _, s := range ordered {
		if s.rawStart < last || s.rawEnd > len(data) || s.rawEnd < s.rawStart {
			continue
		}
		out = append(out, data[last:s.rawStart]...)
		out = append(out, s.content...)
		last = s.rawEnd
	}
	return append(out, data[last:]...)
}

// rewriteTextPart rewrites every paragraph's text runs in one XML part,
// returning the new part bytes and the number of replacements made.
func rewriteTextPart(data []byte, cfg Config) ([]byte, int, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))

	var splices []splice
	total := 0

	var group []textNode // text nodes of the current paragraph

	flushGroup := func() {
		if len(group) == 0 {
			return
		}
		// 2. Concatenated paragraph text with per-node offsets.
		var full bytes.Buffer
		for i := range group {
			group[i].decStart = full.Len()
			full.WriteString(group[i].text)
		}
		text := full.String()

		// 3. The same span machinery as the body pipeline.
		repls := cfg.Replacements(text)
		if len(repls) == 0 {
			group = nil
			return
		}
		total += len(repls)

		// 4. Per-node splicing.
		for _, n := range group {
			ds := n.decStart
			de := ds + len(n.text)
			var out bytes.Buffer
			pos := ds
			changed := false
			for _, r := range repls {
				if r.End <= ds || r.Start >= de {
					continue // does not touch this node
				}
				changed = true
				if r.Start >= pos {
					out.WriteString(text[pos:r.Start])
				}
				if r.Start >= ds {
					// The replacement STARTS here: it is written wholly
					// into this node (spanning tails empty out below).
					out.WriteString(r.Text)
				}
				if r.End > pos {
					pos = r.End
				}
				if pos > de {
					break
				}
			}
			if !changed {
				continue
			}
			if pos < de {
				out.WriteString(text[pos:de])
			}
			var escaped bytes.Buffer
			if err := xml.EscapeText(&escaped, out.Bytes()); err == nil {
				splices = append(splices, splice{rawStart: n.rawStart, rawEnd: n.rawEnd, content: escaped.Bytes()})
			}
		}
		group = nil
	}

	// 1. Token scan. RawToken keeps prefixes/namespaces untouched and,
	// combined with InputOffset, gives exact byte ranges for splicing.
	prevOffset := int64(0)
	inText := false
	var current textNode
	var textBuf bytes.Buffer

	for {
		tok, err := decoder.RawToken()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, 0, fmt.Errorf("the document part is not well-formed XML (%v); re-import the original file and try again", err)
		}
		offset := decoder.InputOffset()

		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" && (t.Name.Space == "w" || t.Name.Space == "a") {
				inText = true
				textBuf.Reset()
				current = textNode{rawStart: int(offset)}
			}
		case xml.CharData:
			if inText {
				textBuf.Write(t)
			}
		case xml.EndElement:
			if inText && t.Name.Local == "t" {
				current.rawEnd = int(prevOffset)
				// A self-closing <w:t/> yields rawEnd < rawStart; clamp
				// to an empty range at the start offset.
				if current.rawEnd < current.rawStart {
					current.rawEnd = current.rawStart
				}
				current.text = textBuf.String()
				group = append(group, current)
				inText = false
			}
			if t.Name.Local == "p" {
				flushGroup()
			}
		}
		prevOffset = offset
	}
	flushGroup() // text outside any paragraph (defensive)

	// 5. Apply the splices through the shared helper, so both passes over a part
	// rewrite bytes by exactly one rule.
	return applySplices(data, splices), total, nil
}
