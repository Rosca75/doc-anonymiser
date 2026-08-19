// engine/aichunks.go — how a document is divided for one local-AI request.
//
// The engine owns this because the engine is what knows what a UNIT of a
// document is: a slide, a page, a row, a line (see pagescope.go). The Ollama
// client owns what a request COSTS, and nothing more. The two questions are
// deliberately kept apart, and that separation is the whole point of this file:
// sizing a slice from the model's context window answers "how much text FITS",
// which measurement shows is a different question from "how much text can the
// model still extract names from". One request carrying a whole document fits
// comfortably inside an 8k window and returns no values at all, on every model
// tried, so what fits is not a safe sizing rule.
//
// A slice is therefore aligned to the document's own units and sized by the
// user's detail level. The context window survives only as hardMaxBytes, an
// absolute ceiling that bites when ONE unit is bigger than a request can carry.
package engine

import (
	"fmt"
	"strings"
)

// Detail levels: how much text one local-AI request carries. They are the
// user's speed-versus-recall dial, so they are two named sizes rather than a
// byte figure nobody outside this file could choose sensibly.
//
// The numbers are measured, not guessed. On a slide-heavy reference deck the
// recall of a small model falls off a cliff between one and two kilobytes of
// prompt: at 1 KB it finds values, at 2 KB it finds none. So DetailThorough
// targets just under the cliff, and DetailFaster is deliberately past it,
// because on a larger model the extra text costs recall little and saves real
// time. The tooltip in the interface says exactly that, in those terms.
//
// There is deliberately NO "whole document in one request" level. It measures
// zero values on every model and both reference documents, and truncates the
// reply on the larger one. A setting whose measured outcome is "finds nothing"
// is a broken switch rather than a choice, so it is not offered.
const (
	DetailThorough = "thorough"
	DetailFaster   = "faster"

	thoroughTargetBytes = 1200
	fasterTargetBytes   = 4000
)

// scanOverlapBytes is how much consecutive pieces of ONE oversized unit
// overlap, so a name sitting exactly on a byte cut is still seen whole by at
// least one piece. It applies only to the last-resort byte split: unit-aligned
// slices need no overlap, because a unit boundary is a boundary the document
// itself drew.
const scanOverlapBytes = 512

// ScanTargetBytes is the target size of one request at the given level.
//
// An unknown or empty level reads as DetailThorough, so a payload that omits it
// lands on the safe end rather than the fast one: a level nobody chose must not
// be the one that silently finds less.
func ScanTargetBytes(level string) int {
	if level == DetailFaster {
		return fasterTargetBytes
	}
	return thoroughTargetBytes
}

// ScanChunk is one local-AI request's worth of text, plus the units it covers.
//
// The unit numbers travel with the text so progress can say "slides 4 to 6 of
// 15" in the same word the import list already uses for this document. A chunk
// index means nothing to the person watching.
type ScanChunk struct {
	Text     string
	FromUnit int
	ToUnit   int
}

// ScanChunks divides a document into the slices the local AI reads, one request
// each.
//
// units is a 1-based list of the document's own units, as the user's scope
// selected them; nil or empty means every unit. The list may be discontiguous
// ("12,13,18-20"), and a slice NEVER spans a gap in it: packing pages 13 and 18
// into one request would hand the model text the user explicitly excluded.
//
// Contiguous units are packed until adding the next one would exceed the
// level's target, then flushed. A run that is still empty never overshoots, so a
// single unit larger than the target always gets its own request rather than
// being dropped.
//
// hardMaxBytes is the absolute ceiling for one request, which the caller derives
// from the model's context window. It is a BACKSTOP and not the sizing rule: it
// bites only where one unit is bigger than it (a dense PDF page, a complex XLSX
// sheet rendered as a single JSON block), and that unit alone is then split by
// bytes. Zero or negative means no ceiling.
//
// Every slice returned is non-empty: a blank unit is text the model has nothing
// to say about, and a request carrying nothing still costs a model load.
func ScanChunks(d Document, units []int, level string, hardMaxBytes int) ([]ScanChunk, error) {
	count := d.PageCount()
	if len(units) == 0 {
		units = make([]int, count)
		for i := range units {
			units[i] = i + 1
		}
	}
	// Validate first, and in the same actionable style PagesMarkdown uses: the
	// indices come from a free-text field the user typed, so a stale or
	// mistyped one must name the document and its real size rather than panic.
	for _, u := range units {
		if u < 1 || u > count {
			return nil, fmt.Errorf(
				"page %d is out of bounds for %q, which has %d %s(s); pick %ss within 1-%d",
				u, d.Name, count, d.pageUnitWord(), d.pageUnitWord(), count)
		}
	}

	target := ScanTargetBytes(level)
	// One slicer for the whole document, built once. Slicing through
	// PageRangeMarkdown per unit would re-derive the unit boundaries for every
	// unit, which on a line-unit document of any size is quadratic.
	slice := d.unitSlicer()

	var out []ScanChunk
	// The pending run of contiguous units. from == 0 means "no run open".
	from, to := 0, 0
	flush := func() {
		if from == 0 {
			return
		}
		text := slice(from, to)
		pending := from
		pendingTo := to
		from, to = 0, 0
		if strings.TrimSpace(text) == "" {
			return
		}
		if hardMaxBytes > 0 && len(text) > hardMaxBytes {
			// One unit too big for a single request. Its pieces all carry the
			// same unit numbers, because they are all still that one unit.
			for _, piece := range SplitOversizedUnit(text, hardMaxBytes) {
				if strings.TrimSpace(piece) == "" {
					continue
				}
				out = append(out, ScanChunk{Text: piece, FromUnit: pending, ToUnit: pendingTo})
			}
			return
		}
		out = append(out, ScanChunk{Text: text, FromUnit: pending, ToUnit: pendingTo})
	}

	for _, u := range units {
		if from == 0 {
			from, to = u, u
			continue
		}
		// A gap in the requested units ends the run unconditionally: the text
		// between them is text the user excluded.
		if u == to+1 && len(slice(from, u)) <= target {
			to = u
			continue
		}
		flush()
		from, to = u, u
	}
	flush()
	return out, nil
}

// SplitOversizedUnit is the LAST-RESORT divider: one unit that no single
// request can carry, cut by bytes.
//
// It prefers to cut at a paragraph break, then a line break, then a space, and
// never inside a UTF-8 sequence, so a French document's accented characters are
// not split into a byte that means nothing. Consecutive pieces overlap by
// roughly scanOverlapBytes so a name landing on a cut is seen whole at least
// once. Text already within the ceiling, and a ceiling of zero or less, yield
// the text unchanged as a single piece.
//
// Its home is this file rather than the Ollama client because it is document
// division, which the engine owns. The client keeps no copy.
func SplitOversizedUnit(text string, maxBytes int) []string {
	overlapBytes := scanOverlapBytes
	if maxBytes <= 0 || len(text) <= maxBytes {
		return []string{text}
	}
	// Overlap must stay well under the ceiling or the loop cannot advance.
	if overlapBytes >= maxBytes/2 {
		overlapBytes = maxBytes / 4
	}

	var pieces []string
	start := 0
	for start < len(text) {
		end := start + maxBytes
		if end >= len(text) {
			pieces = append(pieces, text[start:])
			break
		}
		// Prefer natural boundaries inside the second half of the window;
		// searching the whole window could produce degenerate tiny pieces on
		// paragraph-dense text.
		windowStart := start + maxBytes/2
		cut := -1
		for _, sep := range []string{"\n\n", "\n", " "} {
			if idx := strings.LastIndex(text[windowStart:end], sep); idx >= 0 {
				cut = windowStart + idx + len(sep)
				break
			}
		}
		if cut < 0 {
			// No boundary at all (one enormous token): cut at a rune edge.
			cut = end
			for cut > start && (text[cut]&0xC0) == 0x80 {
				cut--
			}
		}
		pieces = append(pieces, text[start:cut])

		// The next piece starts overlapBytes BEFORE the cut, aligned forward to
		// a rune boundary, and always past the previous start.
		next := cut - overlapBytes
		if next <= start {
			next = cut
		}
		for next < len(text) && (text[next]&0xC0) == 0x80 {
			next++
		}
		start = next
	}
	return pieces
}
