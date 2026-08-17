// app_entities_test.go —  Go-side tests: the ExpandEntityVariants
// bound-method adapter and multi-file discovery merge/dedupe against a
// mocked Ollama server (zero real network).
package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/ollama"
)

// newTestApp builds an App whose Ollama client points at a mock server
// answering /api/chat with per-request content derived from the prompt.
func newTestApp(t *testing.T, replyFor func(userPrompt string) string) *App {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"qwen2.5:3b-instruct"}]}`))
		case "/api/chat":
			var req struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			user := ""
			for _, m := range req.Messages {
				if m.Role == "user" {
					user = m.Content
				}
			}
			resp, _ := json.Marshal(map[string]interface{}{
				"message": map[string]string{"role": "assistant", "content": replyFor(user)},
			})
			w.Write(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	app := NewApp()
	app.llm = ollama.New(srv.URL)
	return app
}

func TestExpandEntityVariantsAdapter(t *testing.T) {
	app := NewApp()
	got := app.ExpandEntityVariants(engine.Entity{Category: "person_names", Canonical: "Marie Duval"})
	joined := strings.Join(got, "|")
	for _, want := range []string{"Marie Duval", "M. Duval", "Duval", "Marie"} {
		if !strings.Contains(joined, want) {
			t.Errorf("adapter lost variant %q: %v", want, got)
		}
	}
}

// aiOnlyApp is newTestApp with the AI route on and the offline route off, so a
// test about what the model proposes is not also reading heuristic candidates.
func aiOnlyApp(t *testing.T, replyFor func(userPrompt string) string) *App {
	t.Helper()
	app := newTestApp(t, replyFor)
	app.settings.UseAI = true
	app.settings.UseAutoDetect = false
	return app
}

func TestDetectionMergesAndDedupesAcrossFiles(t *testing.T) {
	// Two documents name the same entity with different casing plus one distinct
	// person each. The merged result carries the entity once, in the spelling
	// seen first, and both people.
	app := aiOnlyApp(t, func(user string) string {
		if strings.Contains(user, "doc one") {
			return `{"entity_names":["Alpine Trust"],"person_names":["Marie Duval"]}`
		}
		return `{"entity_names":["ALPINE TRUST"],"person_names":["Peter Stone"]}`
	})
	app.docs = []engine.Document{
		{Name: "one.txt", Format: engine.FormatTXT, Markdown: "doc one: Alpine Trust with Marie Duval"},
		{Name: "two.txt", Format: engine.FormatTXT, Markdown: "doc two: ALPINE TRUST with Peter Stone"},
	}

	res, err := app.RunDetection([]string{"one.txt", "two.txt"}, nil, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if len(res.Proposals) != 3 {
		t.Fatalf("want 3 merged proposals with the entity deduped, got %+v", res.Proposals)
	}
	if res.Proposals[0].Text != "Alpine Trust" {
		t.Errorf("the first-seen spelling must win, got %q", res.Proposals[0].Text)
	}
	if res.Cancelled {
		t.Errorf("a completed run must not report itself cancelled: %+v", res)
	}
}

func TestDetectionRespectsTheAllowlist(t *testing.T) {
	app := aiOnlyApp(t, func(string) string {
		return `{"entity_names":["CSSF","Alpine Trust"],"person_names":[]}`
	})
	app.docs = []engine.Document{
		{Name: "a.txt", Format: engine.FormatTXT, Markdown: "CSSF and Alpine Trust"},
	}

	res, err := app.RunDetection([]string{"a.txt"}, []string{"CSSF"}, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if len(res.Proposals) != 1 || res.Proposals[0].Text != "Alpine Trust" {
		t.Errorf("an allowlisted proposal must be vetoed, got %+v", res.Proposals)
	}
}

func TestDetectionOverZeroFilesFailsActionably(t *testing.T) {
	if _, err := NewApp().RunDetection([]string{"ghost.txt"}, nil, nil); err == nil {
		t.Error("detection over zero files must fail rather than report an empty success")
	}
}

func TestDetectionSkipsAFileTooLargeForTheModel(t *testing.T) {
	// A document beyond the context window is SKIPPED and said so, rather than
	// failing the run: the limit is a fact about the model, not a mistake the
	// user made, and the offline route read the file anyway.
	app := aiOnlyApp(t, func(string) string { return `{"entity_names":[],"person_names":[]}` })
	app.llm.ContextSize = 512
	app.docs = []engine.Document{
		{Name: "small.txt", Format: engine.FormatTXT, Markdown: "Alpine Trust is small."},
		{Name: "huge.txt", Format: engine.FormatTXT, Markdown: strings.Repeat("line of text\n", 20000)},
	}

	res, err := app.RunDetection([]string{"small.txt", "huge.txt"}, nil, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Name != "huge.txt" {
		t.Fatalf("the oversized file must be reported as skipped, got %+v", res.Skipped)
	}
	if !strings.Contains(res.Skipped[0].Reason, "Smart detection") {
		t.Errorf("the reason must say the file was still read offline, got %q", res.Skipped[0].Reason)
	}
	if len(res.Errors) != 0 {
		t.Errorf("a skipped file is not an error: %+v", res.Errors)
	}
}

func TestValidateAndTestPattern(t *testing.T) {
	app := NewApp()
	if msg := app.ValidatePattern("PRJ-[0-9]+"); msg != "" {
		t.Errorf("valid pattern flagged: %s", msg)
	}
	if msg := app.ValidatePattern("["); msg == "" || !strings.Contains(msg, "regular expression") {
		t.Errorf("invalid pattern needs an actionable message, got %q", msg)
	}

	app.docs = []engine.Document{{Name: "a.txt", Format: engine.FormatTXT,
		Markdown: "codes PRJ-1 PRJ-2 PRJ-1 here"}}
	samples, err := app.PatternMatches("PRJ-[0-9]+")
	if err != nil {
		t.Fatalf("PatternMatches: %v", err)
	}
	// Distinct matches only.
	if len(samples) != 2 || samples[0] != "PRJ-1" || samples[1] != "PRJ-2" {
		t.Errorf("samples = %v, want [PRJ-1 PRJ-2]", samples)
	}
	if _, err := app.PatternMatches("["); err == nil {
		t.Error("PatternMatches must reject an invalid pattern")
	}
}

// TestDetectionCancellationKeepsWhatItFound: cancelling mid-run returns the
// partial proposals and no error. Partial work is worth keeping, and the caller
// decides how to describe it.
func TestDetectionCancellationKeepsWhatItFound(t *testing.T) {
	var calls atomic.Int32
	app := aiOnlyApp(t, func(string) string {
		calls.Add(1)
		return `{"entity_names":["Alpine Trust"],"person_names":[]}`
	})
	app.docs = []engine.Document{
		{Name: "one.txt", Format: engine.FormatTXT, Markdown: "Alpine Trust one"},
		{Name: "two.txt", Format: engine.FormatTXT, Markdown: "Alpine Trust two"},
		{Name: "three.txt", Format: engine.FormatTXT, Markdown: "Alpine Trust three"},
	}

	// Cancel as soon as the second file starts: the run stops after the file in
	// flight and keeps its proposals.
	old := runtimeEventsEmit
	runtimeEventsEmit = func(a *App, name string, payload interface{}) {
		if p, ok := payload.(DetectionProgress); ok && p.DocIndex == 1 {
			a.CancelDetection()
		}
	}
	defer func() { runtimeEventsEmit = old }()
	app.ctx = context.Background() // any non-nil ctx routes emit() to the stub

	res, err := app.RunDetection([]string{"one.txt", "two.txt", "three.txt"}, nil, nil)
	if err != nil {
		t.Fatalf("a cancelled run must not be an error: %v", err)
	}
	if !res.Cancelled {
		t.Errorf("the run must report itself cancelled: %+v", res)
	}
	if len(res.Proposals) == 0 {
		t.Error("partial proposals must survive cancellation")
	}
	if calls.Load() > 2 {
		t.Errorf("the scan must stop after the cancelled file, made %d chat calls", calls.Load())
	}
}

// TestDetectionReportsProgressPerFile: one event per file at least, each naming
// the file, so a long run never looks hung.
func TestDetectionReportsProgressPerFile(t *testing.T) {
	app := aiOnlyApp(t, func(string) string { return `{"entity_names":[],"person_names":[]}` })
	app.docs = []engine.Document{
		{Name: "one.txt", Format: engine.FormatTXT, Markdown: "text one"},
		{Name: "two.txt", Format: engine.FormatTXT, Markdown: "text two"},
	}

	var named []string
	old := runtimeEventsEmit
	runtimeEventsEmit = func(a *App, name string, payload interface{}) {
		if p, ok := payload.(DetectionProgress); ok && p.ChunkCount == 0 {
			named = append(named, p.DocName)
		}
	}
	defer func() { runtimeEventsEmit = old }()
	app.ctx = context.Background()

	if _, err := app.RunDetection([]string{"one.txt", "two.txt"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(named) != 2 || named[0] != "one.txt" || named[1] != "two.txt" {
		t.Errorf("want one progress event per file, in order, got %v", named)
	}
}

// ---  tests --------------------------------------------------------------

// TestCountTermMatches: word-boundary counting for the live preview
// ("Lux" must not match inside "Luxembourg").
func TestCountTermMatches(t *testing.T) {
	app := NewApp()
	app.docs = []engine.Document{
		{Name: "a.txt", Format: engine.FormatTXT, Markdown: "Lux is nice. Luxembourg is bigger. lux again."},
		{Name: "b.txt", Format: engine.FormatTXT, Markdown: "No mention here."},
		{Name: "c.txt", Format: engine.FormatTXT, Markdown: "Lux once more."},
	}
	cases := []struct {
		term      string
		count     int
		documents int
	}{
		{"Lux", 3, 2},        // case-insensitive, boundary-anchored
		{"Luxembourg", 1, 1}, // the long word matches itself only
		{"absent", 0, 0},
		{"", 0, 0},
	}
	for _, tc := range cases {
		got := app.CountTermMatches(tc.term)
		if got.Count != tc.count || got.Documents != tc.documents {
			t.Errorf("CountTermMatches(%q) = %+v, want {%d %d}", tc.term, got, tc.count, tc.documents)
		}
	}
}

// TestCuratedVariants: once the user has curated a value's spellings, the
// automatic expansion no longer applies, so the chips on the card are exactly
// what the run replaces. This is what makes a deleted spelling stay deleted:
// there is no expansion left to derive it again.
func TestCuratedVariants(t *testing.T) {
	curated := false
	got := engine.ExpandVariants(engine.Entity{
		Category:       "person_names",
		Canonical:      "Jean Muller",
		ManualVariants: []string{"Jean"},
		AutoExpand:     &curated,
	})
	for _, v := range got {
		if strings.EqualFold(v, "J. Muller") || strings.EqualFold(v, "Muller") {
			t.Errorf("curated value still derived %q: %v", v, got)
		}
	}
	want := map[string]bool{"Jean Muller": true, "Jean": true}
	if len(got) != len(want) {
		t.Fatalf("a curated value expands to its name plus its own spellings, got %v", got)
	}
	for _, v := range got {
		if !want[v] {
			t.Errorf("unexpected spelling %q in a curated expansion: %v", v, got)
		}
	}
}

// TestOfflineRouteReturnsCandidatesNotEntities: detection proposes candidates
// for review; the App holds no entity state to mutate, and nothing is replaced
// until the user accepts a suggestion.
func TestOfflineRouteReturnsCandidatesNotEntities(t *testing.T) {
	app := NewApp()
	app.settings.SmartDetect = engine.SmartDetectOptions{} // no filtering
	app.docs = []engine.Document{
		{Name: "a.txt", Format: engine.FormatTXT,
			Markdown: "Meeting with Marie Duval about Alpine Trust S.A. Later Marie Duval called again."},
	}

	res, err := app.RunDetection([]string{"a.txt"}, []string{"CSSF"}, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if res.Cancelled {
		t.Errorf("a completed run must not report itself cancelled: %+v", res)
	}
	byText := map[string]engine.Candidate{}
	for _, c := range res.Candidates {
		byText[c.Text] = c
	}
	if c, ok := byText["Marie Duval"]; !ok || c.Category != engine.CatPersonNames || c.Count < 2 {
		t.Errorf("the person candidate is wrong: %+v", res.Candidates)
	}
	if c, ok := byText["Alpine Trust S.A."]; !ok || c.Category != engine.CatEntityNames {
		t.Errorf("the legal-suffix candidate is wrong: %+v", res.Candidates)
	}
}

// TestCheckIntersectionsAgreesWithARealRun: the bound method and the pipeline
// must reach the same verdict on the same input. That is why the check reuses
// the engine's own detection rather than a parallel one, and it is what this
// test holds: a warning that disagrees with the run describes something that
// did not happen, and the user cannot act on it.
func TestCheckIntersectionsAgreesWithARealRun(t *testing.T) {
	const value = "marie.duval@example.com"
	app := NewApp()
	app.docs = []engine.Document{{
		Name: "a.txt", Format: engine.FormatTXT,
		Markdown: "Write to " + value + " today.\n",
	}}
	entities := []engine.Entity{{Category: engine.CatPersonNames, Canonical: value}}

	res, err := app.CheckIntersections(CheckIntersectionsRequest{Entities: entities})
	if err != nil {
		t.Fatalf("CheckIntersections: %v", err)
	}
	if len(res.Intersections) != 1 {
		t.Fatalf("the declared value is covered by the email signal, got %+v", res.Intersections)
	}
	row := res.Intersections[0]
	if row.Value != value || row.Category != engine.CatPersonNames {
		t.Errorf("the row must name the value that lost, got %+v", row)
	}

	// The check must not have touched the registry: it runs while the user is
	// still editing values, and minting a placeholder there would spend a
	// number on a configuration that may never be run.
	if app.registry != nil && len(app.registry.Entries()) != 0 {
		t.Errorf("CheckIntersections must mint nothing, got %+v", app.registry.Entries())
	}

	// Now run for real and compare verdicts.
	if _, err := app.runPipelineBlocking(context.Background(), RunRequest{
		Entities: entities,
	}); err != nil {
		t.Fatalf("runPipelineBlocking: %v", err)
	}
	var owner string
	for _, e := range app.registry.Entries() {
		if strings.EqualFold(e.Original, value) {
			owner = e.Category
		}
	}
	if owner != row.WinnerCategory {
		t.Errorf("the check predicted %s and the run filed the value under %s",
			row.WinnerCategory, owner)
	}
}

// TestCheckIntersectionsIsQuietWhenNothingOverlaps: an empty list is the
// normal answer, not an error. A screen that calls this on every edit must not
// have to distinguish "nothing overlaps" from "the call failed".
func TestCheckIntersectionsIsQuietWhenNothingOverlaps(t *testing.T) {
	app := NewApp()
	app.docs = []engine.Document{{
		Name: "a.txt", Format: engine.FormatTXT,
		Markdown: "Alpine Trust met Borealis Capital on the Tuesday.\n",
	}}
	res, err := app.CheckIntersections(CheckIntersectionsRequest{
		Entities: []engine.Entity{
			{Category: engine.CatEntityNames, Canonical: "Alpine Trust"},
			{Category: engine.CatBrandNames, Canonical: "Borealis Capital"},
		},
	})
	if err != nil {
		t.Fatalf("CheckIntersections: %v", err)
	}
	if len(res.Intersections) != 0 {
		t.Errorf("nothing overlaps here, got %+v", res.Intersections)
	}
}
