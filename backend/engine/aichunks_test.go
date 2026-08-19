// engine/aichunks_test.go — how a document is divided for one local-AI request.
//
// TIER: unit (docs/TESTING.md). Pure functions over in-memory Documents, no
// I/O, no model, no fixtures beyond a few literals.
//
// The subject is the rule the reported failure came down to: a slice is the
// document's own unit, sized by the detail level, and NEVER the whole document
// because it happens to fit the context window.
package engine_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"doc-anonymiser/backend/engine"
)

// TestValidDetailLevel: the boundary refuses a level nothing sizes, and accepts
// the empty string, because absence and a typo are different facts. A session
// file written before the setting existed carries the first; only the second is
// a mistake, and storing it would leave the rail showing a level no run uses.
func TestValidDetailLevel(t *testing.T) {
	t.Run("config/every_offered_level_is_valid", func(t *testing.T) {
		if len(engine.AllDetailLevels) != 2 {
			t.Fatalf("AllDetailLevels = %v, want exactly the two levels the rail offers", engine.AllDetailLevels)
		}
		for _, level := range engine.AllDetailLevels {
			if !engine.ValidDetailLevel(level) {
				t.Errorf("ValidDetailLevel(%q) = false, want true: a level the interface offers must be one the engine sizes", level)
			}
		}
	})
	t.Run("config/absence_is_valid_and_a_typo_is_not", func(t *testing.T) {
		if !engine.ValidDetailLevel("") {
			t.Error("ValidDetailLevel(\"\") = false, want true: an absent level means thorough, which is not a mistake")
		}
		for _, level := range []string{"quick", "exhaustive", "Thorough", "whole"} {
			if engine.ValidDetailLevel(level) {
				t.Errorf("ValidDetailLevel(%q) = true, want false: a level nothing sizes must be refused, not stored", level)
			}
		}
	})
}

// slideDoc builds a pptx-shaped document with one "## Slide N" section per body,
// which is the boundary convert.Pptx emits and pagescope reads.
func slideDoc(bodies ...string) engine.Document {
	var b strings.Builder
	for i, body := range bodies {
		b.WriteString("## Slide ")
		b.WriteString(string(rune('1' + i)))
		b.WriteString(": title\n")
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	return engine.Document{
		Name:     "deck.pptx",
		Format:   engine.FormatPPTX,
		Unit:     engine.UnitSlide,
		Markdown: b.String(),
	}
}

// TestScanTargetBytes: the levels map to their measured sizes, and the DIRECTION
// of the default is the invariant. A level nobody chose must not be the one that
// finds less, or a payload that forgot the field silently gets the fast scan.
func TestScanTargetBytes(t *testing.T) {
	t.Run("detection/levels_map_to_their_own_size", func(t *testing.T) {
		thorough := engine.ScanTargetBytes(engine.DetailThorough)
		faster := engine.ScanTargetBytes(engine.DetailFaster)
		if thorough >= faster {
			t.Errorf("thorough must target SMALLER slices than faster: thorough=%d faster=%d",
				thorough, faster)
		}
	})
	t.Run("detection/unknown_level_is_not_the_fast_one", func(t *testing.T) {
		thorough := engine.ScanTargetBytes(engine.DetailThorough)
		for _, level := range []string{"", "quick", "exhaustive", "THOROUGH"} {
			if got := engine.ScanTargetBytes(level); got != thorough {
				t.Errorf("level %q = %d, want the thorough target %d: an unrecognised level must not be the fast one",
					level, got, thorough)
			}
		}
	})
}

// TestScanChunksPacksSlidesByLevel: the detail level decides how many units one
// request carries. This is the fix for the reported failure, at its smallest:
// the same deck is several requests, never one.
func TestScanChunksPacksSlidesByLevel(t *testing.T) {
	// Four slides of roughly 800 bytes each: one fits the thorough target
	// (1200) and two do not; two fit the faster target (4000).
	body := strings.Repeat("Alpine Trust and Marie Duval discussed the plan. ", 16)
	doc := slideDoc(body, body, body, body)

	thorough, err := engine.ScanChunks(doc, nil, engine.DetailThorough, 100000)
	if err != nil {
		t.Fatalf("ScanChunks thorough: %v", err)
	}
	if len(thorough) != 4 {
		t.Errorf("at the thorough level each ~800 byte slide is its own request: got %d request(s), want 4",
			len(thorough))
	}
	for i, chunk := range thorough {
		if chunk.FromUnit != i+1 || chunk.ToUnit != i+1 {
			t.Errorf("request %d covers units %d-%d, want %d-%d",
				i, chunk.FromUnit, chunk.ToUnit, i+1, i+1)
		}
	}

	faster, err := engine.ScanChunks(doc, nil, engine.DetailFaster, 100000)
	if err != nil {
		t.Fatalf("ScanChunks faster: %v", err)
	}
	if len(faster) >= len(thorough) {
		t.Errorf("the faster level must pack MORE units per request: faster=%d requests, thorough=%d",
			len(faster), len(thorough))
	}
	if faster[0].ToUnit == faster[0].FromUnit {
		t.Errorf("the faster level must pack several slides into one request, got units %d-%d",
			faster[0].FromUnit, faster[0].ToUnit)
	}
}

// TestScanChunksPacksManyLinesPerRequest: the target is a SIZE, not a unit
// count. A 400-line text file whose unit is the line must not become 400
// requests, which at a few seconds each is half an hour.
func TestScanChunksPacksManyLinesPerRequest(t *testing.T) {
	doc := engine.Document{
		Name:     "notes.txt",
		Format:   engine.FormatTXT,
		Unit:     engine.UnitLine,
		Markdown: strings.Repeat("Marie Duval called about the audit.\n", 400),
	}
	got, err := engine.ScanChunks(doc, nil, engine.DetailThorough, 100000)
	if err != nil {
		t.Fatalf("ScanChunks: %v", err)
	}
	if len(got) > 30 {
		t.Errorf("400 short lines must pack into a few dozen requests, got %d", len(got))
	}
	if len(got) < 2 {
		t.Fatalf("14 KB of lines must be more than one request, got %d", len(got))
	}
	// Every unit is covered exactly once, in order.
	next := 1
	for i, chunk := range got {
		if chunk.FromUnit != next {
			t.Fatalf("request %d starts at unit %d, want %d: the packing skipped or repeated a line",
				i, chunk.FromUnit, next)
		}
		next = chunk.ToUnit + 1
	}
	if next != 401 {
		t.Errorf("the slices cover units 1-%d, want 1-400", next-1)
	}
}

// TestScanChunksKeepsTheGridHeader: a bare data row means nothing without the
// column names, so every slice of a grid carries the header. It is the reason a
// scoped row is intelligible to a model at all.
func TestScanChunksKeepsTheGridHeader(t *testing.T) {
	grid := [][]string{{"client", "contact"}}
	for i := 0; i < 200; i++ {
		grid = append(grid, []string{"Alpine Trust Luxembourg branch", "Marie Duval"})
	}
	doc := engine.Document{
		Name:   "clients.csv",
		Format: engine.FormatCSV,
		Unit:   engine.UnitRow,
		Grid:   grid,
	}
	got, err := engine.ScanChunks(doc, nil, engine.DetailThorough, 100000)
	if err != nil {
		t.Fatalf("ScanChunks: %v", err)
	}
	if len(got) < 2 {
		t.Fatalf("200 rows must be more than one request, got %d", len(got))
	}
	for i, chunk := range got {
		if !strings.Contains(chunk.Text, "client") || !strings.Contains(chunk.Text, "contact") {
			t.Errorf("request %d (units %d-%d) lost the header row: %q",
				i, chunk.FromUnit, chunk.ToUnit, chunk.Text)
		}
	}
}

// TestScanChunksNeverSpansAGapInTheScope: packing pages 13 and 18 into one
// request would hand the model the pages between them, which the user excluded.
func TestScanChunksNeverSpansAGapInTheScope(t *testing.T) {
	doc := slideDoc("Alpha content", "Bravo content", "Charlie content",
		"Delta content", "Echo content")

	got, err := engine.ScanChunks(doc, []int{1, 2, 5}, engine.DetailFaster, 100000)
	if err != nil {
		t.Fatalf("ScanChunks: %v", err)
	}
	// The whole deck fits the faster target, so only the GAP can split it.
	if len(got) != 2 {
		t.Fatalf("a scope of 1,2,5 must be two requests (1-2 and 5), got %d: %+v", len(got), got)
	}
	if got[0].FromUnit != 1 || got[0].ToUnit != 2 {
		t.Errorf("first request covers units %d-%d, want 1-2", got[0].FromUnit, got[0].ToUnit)
	}
	if got[1].FromUnit != 5 || got[1].ToUnit != 5 {
		t.Errorf("second request covers units %d-%d, want 5-5", got[1].FromUnit, got[1].ToUnit)
	}
	joined := strings.Join([]string{got[0].Text, got[1].Text}, "\n")
	for _, excluded := range []string{"Charlie content", "Delta content"} {
		if strings.Contains(joined, excluded) {
			t.Errorf("a slice spanned the gap and leaked %q", excluded)
		}
	}
}

// TestScanChunksSplitsAUnitTooBigForOneRequest: the hard ceiling is the backstop
// for a dense page no request can carry whole. Its pieces are still that one
// unit, so they say so.
func TestScanChunksSplitsAUnitTooBigForOneRequest(t *testing.T) {
	doc := engine.Document{
		Name:   "dense.pdf",
		Format: engine.FormatPDF,
		Unit:   engine.UnitPage,
		Pages: []string{
			"short first page",
			strings.Repeat("Marie Duval and Alpine Trust. ", 400), // ~12 KB
		},
	}
	doc.Markdown = strings.Join(doc.Pages, "\n\n")

	got, err := engine.ScanChunks(doc, nil, engine.DetailThorough, 3000)
	if err != nil {
		t.Fatalf("ScanChunks: %v", err)
	}
	pieces := 0
	for _, chunk := range got {
		if len(chunk.Text) > 3000 {
			t.Errorf("a slice of %d bytes exceeds the hard ceiling of 3000", len(chunk.Text))
		}
		if chunk.FromUnit == 2 {
			pieces++
			if chunk.ToUnit != 2 {
				t.Errorf("a piece of page 2 claims units %d-%d, want 2-2",
					chunk.FromUnit, chunk.ToUnit)
			}
		}
	}
	if pieces < 2 {
		t.Errorf("a 12 KB page under a 3 KB ceiling must be split, got %d piece(s)", pieces)
	}
}

// TestScanChunksASingleOversizedUnitIsNeverDropped: a run that is still empty
// never overshoots, so a unit bigger than the target gets its own request rather
// than being skipped for being too big.
func TestScanChunksASingleOversizedUnitIsNeverDropped(t *testing.T) {
	big := strings.Repeat("Alpine Trust. ", 300) // ~4 KB, well over the thorough target
	doc := slideDoc("tiny", big, "tiny")

	got, err := engine.ScanChunks(doc, nil, engine.DetailThorough, 100000)
	if err != nil {
		t.Fatalf("ScanChunks: %v", err)
	}
	covered := map[int]bool{}
	for _, chunk := range got {
		for u := chunk.FromUnit; u <= chunk.ToUnit; u++ {
			covered[u] = true
		}
	}
	for u := 1; u <= 3; u++ {
		if !covered[u] {
			t.Errorf("unit %d was dropped; slices = %+v", u, got)
		}
	}
}

// TestScanChunksSlicesAreNeverEmpty: a request carrying nothing still costs a
// model load and returns nothing, so a blank unit produces no request.
func TestScanChunksSlicesAreNeverEmpty(t *testing.T) {
	doc := engine.Document{
		Name:     "gappy.txt",
		Format:   engine.FormatTXT,
		Unit:     engine.UnitLine,
		Markdown: "Marie Duval\n\n\n\n",
	}
	got, err := engine.ScanChunks(doc, nil, engine.DetailThorough, 100000)
	if err != nil {
		t.Fatalf("ScanChunks: %v", err)
	}
	for i, chunk := range got {
		if strings.TrimSpace(chunk.Text) == "" {
			t.Errorf("slice %d is blank, which is a request that asks the model nothing", i)
		}
	}
	if len(got) == 0 {
		t.Error("a document with one line of text must still produce a request")
	}
}

// TestScanChunksAllBlankDocumentProducesNoRequests: the one case that is
// genuinely "there is nothing here to read", which is what keeps the skip
// meaningful now that size no longer refuses a scan.
func TestScanChunksAllBlankDocumentProducesNoRequests(t *testing.T) {
	doc := engine.Document{
		Name: "blank.txt", Format: engine.FormatTXT, Unit: engine.UnitLine,
		Markdown: "   \n\t\n \n",
	}
	got, err := engine.ScanChunks(doc, nil, engine.DetailThorough, 100000)
	if err != nil {
		t.Fatalf("a blank document is not an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("a whitespace-only document has nothing to send, got %d request(s)", len(got))
	}
}

// TestScanChunksOutOfRangeUnitIsActionable: the indices come from a free-text
// field, so a stale one names the document and its real size rather than
// panicking or silently truncating.
func TestScanChunksOutOfRangeUnitIsActionable(t *testing.T) {
	doc := slideDoc("one", "two")
	for _, units := range [][]int{{0}, {3}, {1, 99}, {-1}} {
		_, err := engine.ScanChunks(doc, units, engine.DetailThorough, 100000)
		if err == nil {
			t.Errorf("units %v must be refused for a 2 slide deck", units)
			continue
		}
		msg := err.Error()
		for _, want := range []string{"deck.pptx", "slide", "1-2"} {
			if !strings.Contains(msg, want) {
				t.Errorf("the error for %v must name %q: %v", units, want, msg)
			}
		}
	}
}

// TestSplitOversizedUnit: the last-resort byte splitter. These cases follow the
// splitter from the Ollama client, where it sized every chunk from the context
// window; its home is now the file that owns document division.
func TestSplitOversizedUnit(t *testing.T) {
	t.Run("detection/empty_text_is_one_empty_piece", func(t *testing.T) {
		got := engine.SplitOversizedUnit("", 100)
		if len(got) != 1 || got[0] != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("detection/text_within_the_ceiling_is_one_piece", func(t *testing.T) {
		got := engine.SplitOversizedUnit("short text", 100)
		if len(got) != 1 || got[0] != "short text" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("detection/no_ceiling_is_one_piece", func(t *testing.T) {
		got := engine.SplitOversizedUnit(strings.Repeat("x", 5000), 0)
		if len(got) != 1 {
			t.Errorf("a ceiling of zero means no ceiling, got %d piece(s)", len(got))
		}
	})
	t.Run("detection/paragraph_boundary_preferred_over_mid_word", func(t *testing.T) {
		text := strings.Repeat("a", 60) + "\n\n" + strings.Repeat("b", 60)
		got := engine.SplitOversizedUnit(text, 100)
		if len(got) < 2 {
			t.Fatalf("expected a split, got %d piece(s)", len(got))
		}
		if !strings.HasSuffix(got[0], "\n\n") {
			t.Errorf("the first piece must end at the paragraph break, got %q", got[0][len(got[0])-10:])
		}
	})
	t.Run("detection/consecutive_pieces_overlap", func(t *testing.T) {
		words := strings.Repeat("word ", 100) // 500 bytes of words
		got := engine.SplitOversizedUnit(words, 100)
		if len(got) < 2 {
			t.Fatalf("expected several pieces, got %d", len(got))
		}
		// The tail of piece 1 must reappear at the head of piece 2, or a name
		// landing on the cut is seen by neither.
		tail := got[0][len(got[0])-10:]
		if !strings.Contains(got[1][:30], tail[:5]) {
			t.Errorf("no overlap between pieces: %q ... %q", got[0], got[1])
		}
	})
	t.Run("detection/rune_safety_on_multi_byte_french_text", func(t *testing.T) {
		text := strings.Repeat("éàüöè ", 200) // 7 bytes per group
		for _, piece := range engine.SplitOversizedUnit(text, 128) {
			if !utf8.ValidString(piece) {
				t.Fatalf("a piece splits a UTF-8 sequence: %q", piece)
			}
		}
	})
	t.Run("detection/giant_unbroken_token_still_terminates", func(t *testing.T) {
		got := engine.SplitOversizedUnit(strings.Repeat("x", 1000), 100)
		if len(got) < 10 {
			t.Errorf("expected roughly 11 pieces, got %d", len(got))
		}
	})
}
