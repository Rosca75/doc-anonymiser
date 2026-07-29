// engine/tracing_test.go — BUILD-03 Phase E (decision tracing hook) and
// Phase F (confidence-aware overlap resolution) tests.
package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestOnTraceReceivesResolvedSpans: with OnTrace set, the pipeline reports
// exactly the spans that were about to be replaced, in the order they were
// applied, tagged with the empty region for a text document.
func TestOnTraceReceivesResolvedSpans(t *testing.T) {
	text := "contact marie.duval@example.com about card 4532 0151 1283 0366."

	traces := map[string][]SpanTrace{}
	_, err := Run(context.Background(), PipelineInput{
		Documents: []Document{{Name: "note.txt", Format: FormatTXT, Markdown: text}},
		Level:     LevelSoft,
		Allowlist: NewEmptyAllowlist(),
		OnTrace: func(docName string, ts []SpanTrace) {
			traces[docName] = ts
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := traces["note.txt"]
	if !ok {
		t.Fatal("OnTrace was never called for note.txt")
	}
	if len(got) != 1 {
		t.Fatalf("want 1 trace region (document body), got %d: %+v", len(got), got)
	}
	if got[0].Region != "" {
		t.Errorf("text-doc trace region = %q, want empty", got[0].Region)
	}
	// The trace must cover both the email and the credit card.
	var cats []string
	for _, s := range got[0].Spans {
		cats = append(cats, s.Category)
	}
	joined := strings.Join(cats, ",")
	if !strings.Contains(joined, CatEmail) || !strings.Contains(joined, CatCreditCard) {
		t.Errorf("expected email + credit_card in trace, got %v", cats)
	}
	// Every traced span must carry a positive confidence — the tracer must
	// not silently strip the Phase-C field.
	for _, s := range got[0].Spans {
		if s.Confidence <= 0 || s.Confidence > 1 {
			t.Errorf("trace span %+v has invalid confidence %v", s, s.Confidence)
		}
	}
}

// TestOnTraceJSONShape: the SpanTrace / Span types serialise to a stable
// JSON shape suitable for a debug export (BUILD-03 Phase E acceptance).
func TestOnTraceJSONShape(t *testing.T) {
	trace := SpanTrace{
		Region: "row 0 col 1",
		Spans: []Span{{
			Start: 0, End: 5, Category: CatEmail, Original: "a@b.c", Confidence: 1.0,
		}},
	}
	raw, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, needle := range []string{`"region":"row 0 col 1"`, `"category":"email"`, `"confidence":1`} {
		if !strings.Contains(s, needle) {
			t.Errorf("json %s missing %q", s, needle)
		}
	}
}

// TestOnTraceSkipsEmptyRegions: cells that produced no spans do not appear
// in the trace — keeps the debug output focused on where things fired.
func TestOnTraceSkipsEmptyRegions(t *testing.T) {
	// CSV → grid → two cells; only the second holds PII.
	csv := []byte("note,contact\nhello,marie@example.com\n")
	doc, err := Load("data.csv", csv)
	if err != nil {
		t.Fatal(err)
	}
	var got []SpanTrace
	_, err = Run(context.Background(), PipelineInput{
		Documents: []Document{doc},
		Level:     LevelSoft,
		Allowlist: NewEmptyAllowlist(),
		OnTrace:   func(_ string, ts []SpanTrace) { got = ts },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 non-empty region trace, got %d: %+v", len(got), got)
	}
	if !strings.HasPrefix(got[0].Region, "row ") {
		t.Errorf("grid trace must carry a row/col region label, got %q", got[0].Region)
	}
	if len(got[0].Spans) == 0 {
		t.Error("region entry must carry its spans")
	}
}

// TestOnTraceNilIsFree: OnTrace = nil must not affect output at all — the
// pipeline's tracing plumbing is opt-in and side-effect-free when unused.
func TestOnTraceNilIsFree(t *testing.T) {
	text := "call +352 621 000 111 about ip 192.168.0.1"
	base, err := Run(context.Background(), PipelineInput{
		Documents: []Document{{Name: "f.txt", Format: FormatTXT, Markdown: text}},
		Level:     LevelSoft,
		Allowlist: NewEmptyAllowlist(),
	})
	if err != nil {
		t.Fatal(err)
	}
	withTrace, err := Run(context.Background(), PipelineInput{
		Documents: []Document{{Name: "f.txt", Format: FormatTXT, Markdown: text}},
		Level:     LevelSoft,
		Allowlist: NewEmptyAllowlist(),
		OnTrace:   func(string, []SpanTrace) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if base.Documents[0].Anonymised != withTrace.Documents[0].Anonymised {
		t.Errorf("tracing must not alter output:\n%s\n---\n%s",
			base.Documents[0].Anonymised, withTrace.Documents[0].Anonymised)
	}
}

// --- Phase F: confidence-aware overlap resolution ----------------------

// TestOverlapResolutionByConfidence: when two spans overlap identically,
// the higher-confidence one wins (BUILD-03 Phase F). The tie-break chain
// below length is start, then category — same as v1.
func TestOverlapResolutionByConfidence(t *testing.T) {
	// Two identical-length spans covering the same range; different
	// confidences.
	spans := []Span{
		{Start: 0, End: 10, Category: "b_cat", Original: "0123456789", Confidence: 0.7},
		{Start: 0, End: 10, Category: "a_cat", Original: "0123456789", Confidence: 0.9},
	}
	kept := ResolveOverlaps(spans)
	if len(kept) != 1 {
		t.Fatalf("want 1 span, got %+v", kept)
	}
	if kept[0].Category != "a_cat" {
		t.Errorf("higher-confidence span must win, got %+v", kept[0])
	}
}

// TestOverlapResolutionLegacyZeroConfidence: pre-BUILD-03 spans (Confidence
// = 0) must behave exactly like v1 — length wins.
func TestOverlapResolutionLegacyZeroConfidence(t *testing.T) {
	spans := []Span{
		{Start: 0, End: 5, Category: "short", Original: "01234"},
		{Start: 0, End: 10, Category: "long", Original: "0123456789"},
	}
	kept := ResolveOverlaps(spans)
	if len(kept) != 1 || kept[0].Category != "long" {
		t.Errorf("v1 behaviour must hold when confidence is unset, got %+v", kept)
	}
}

// TestOverlapResolutionURLBeatsEmail: the classic "email inside a URL"
// case still resolves to the URL (higher confidence tied at 1.0, length
// then wins). Guards against a Phase-F regression in v1 semantics.
func TestOverlapResolutionURLBeatsEmail(t *testing.T) {
	text := "profile at https://example.com/u/marie.duval@example.com end"
	spans := ResolveOverlaps(DetectPII(text, LevelMedium))
	if len(spans) != 1 || spans[0].Category != CatURL {
		t.Errorf("URL must still beat embedded email, got %+v", spans)
	}
}
