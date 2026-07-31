// app_entities_test.go — Phase 7 Go-side tests: the ExpandEntityVariants
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

func TestRunDiscoveryMergesAndDedupes(t *testing.T) {
	// Two docs mention the same client with different casing plus one
	// distinct person each; the merged result must contain the client
	// once (first spelling) and both persons.
	app := newTestApp(t, func(user string) string {
		if strings.Contains(user, "doc one") {
			return `{"entity_names":["Alpine Trust"],"project_names":[],"person_names":["Marie Duval"]}`
		}
		return `{"entity_names":["ALPINE TRUST"],"project_names":[],"person_names":["Peter Stone"]}`
	})
	app.docs = []engine.Document{
		{Name: "one.txt", Format: engine.FormatTXT, Markdown: "doc one: Alpine Trust with Marie Duval"},
		{Name: "two.txt", Format: engine.FormatTXT, Markdown: "doc two: ALPINE TRUST with Peter Stone"},
	}

	res, err := app.RunDiscovery([]string{"one.txt", "two.txt"}, nil)
	if err != nil {
		t.Fatalf("RunDiscovery: %v", err)
	}
	got := res.Proposals
	if len(got) != 3 {
		t.Fatalf("want 3 merged proposals (client deduped), got %+v", got)
	}
	if got[0].Text != "Alpine Trust" {
		t.Errorf("first-seen spelling must win, got %q", got[0].Text)
	}
	if res.Cancelled || !strings.Contains(res.Status, "2 file(s)") {
		t.Errorf("status = %+v, want a completed 2-file status", res)
	}
}

func TestRunDiscoveryRespectsAllowlist(t *testing.T) {
	app := newTestApp(t, func(string) string {
		return `{"entity_names":["CSSF","Alpine Trust"],"project_names":[],"person_names":[]}`
	})
	app.docs = []engine.Document{{Name: "a.txt", Format: engine.FormatTXT, Markdown: "CSSF and Alpine Trust"}}

	res, err := app.RunDiscovery([]string{"a.txt"}, []string{"CSSF"})
	if err != nil {
		t.Fatalf("RunDiscovery: %v", err)
	}
	if len(res.Proposals) != 1 || res.Proposals[0].Text != "Alpine Trust" {
		t.Errorf("allowlisted proposal must be vetoed, got %+v", res.Proposals)
	}
}

func TestRunDiscoveryNoMatchingFiles(t *testing.T) {
	app := NewApp()
	if _, err := app.RunDiscovery([]string{"ghost.txt"}, nil); err == nil {
		t.Error("discovery over zero files must fail actionably")
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

// --- BUILD-02 Phase 7 tests ------------------------------------------------

// TestRunDiscoveryCancellation: cancelling after the first file returns
// partial proposals, no error, and the documented status line.
func TestRunDiscoveryCancellation(t *testing.T) {
	var calls atomic.Int32
	app := newTestApp(t, func(user string) string {
		calls.Add(1)
		return `{"entity_names":["Alpine Trust"],"project_names":[],"person_names":[]}`
	})
	app.docs = []engine.Document{
		{Name: "one.txt", Format: engine.FormatTXT, Markdown: "Alpine Trust one"},
		{Name: "two.txt", Format: engine.FormatTXT, Markdown: "Alpine Trust two"},
		{Name: "three.txt", Format: engine.FormatTXT, Markdown: "Alpine Trust three"},
	}

	// Cancel as soon as the first progress event fires: the run stops
	// after the in-flight file, keeping its proposals.
	old := runtimeEventsEmit
	runtimeEventsEmit = func(a *App, name string, payload interface{}) {
		if name == "discovery:progress" {
			if idx, _ := payload.(map[string]interface{})["docIndex"].(int); idx == 1 {
				a.CancelDiscovery()
			}
		}
	}
	defer func() { runtimeEventsEmit = old }()
	app.ctx = context.Background() // any non-nil ctx routes emit() to our stub

	res, err := app.RunDiscovery([]string{"one.txt", "two.txt", "three.txt"}, nil)
	if err != nil {
		t.Fatalf("a cancelled discovery must not be an error: %v", err)
	}
	if !res.Cancelled || !strings.Contains(res.Status, "of 3 files") {
		t.Errorf("status = %+v, want cancelled after N of 3 files", res)
	}
	if len(res.Proposals) == 0 {
		t.Error("partial proposals must survive cancellation")
	}
	if calls.Load() > 2 {
		t.Errorf("scan must stop after the cancelled file, made %d chat calls", calls.Load())
	}
}

// TestEstimateDiscoveryOversize: a file beyond the chunk cap is flagged
// TooLarge with the documented advice; a small file is not.
func TestEstimateDiscoveryOversize(t *testing.T) {
	app := NewApp()
	app.llm.ContextSize = 512 // 1152-byte chunks
	app.docs = []engine.Document{
		{Name: "small.txt", Format: engine.FormatTXT, Markdown: "tiny"},
		{Name: "huge.txt", Format: engine.FormatTXT, Markdown: strings.Repeat("line of text\n", 20000)},
	}
	ests := app.EstimateDiscovery([]string{"small.txt", "huge.txt"})
	if len(ests) != 2 {
		t.Fatalf("want 2 estimates, got %+v", ests)
	}
	if ests[0].TooLarge || ests[0].Chunks != 1 {
		t.Errorf("small file flagged: %+v", ests[0])
	}
	if !ests[1].TooLarge || !strings.Contains(ests[1].Message, "Smart detection") {
		t.Errorf("huge file must be flagged with the smart-detection advice: %+v", ests[1])
	}
}

// TestDiscoveryProgressEvents: exactly one progress event per file, with
// the file name in the payload.
func TestDiscoveryProgressEvents(t *testing.T) {
	app := newTestApp(t, func(string) string {
		return `{"entity_names":[],"project_names":[],"person_names":[]}`
	})
	app.docs = []engine.Document{
		{Name: "one.txt", Format: engine.FormatTXT, Markdown: "text one"},
		{Name: "two.txt", Format: engine.FormatTXT, Markdown: "text two"},
	}

	var events []map[string]interface{}
	old := runtimeEventsEmit
	runtimeEventsEmit = func(a *App, name string, payload interface{}) {
		if name == "discovery:progress" {
			events = append(events, payload.(map[string]interface{}))
		}
	}
	defer func() { runtimeEventsEmit = old }()
	app.ctx = context.Background()

	if _, err := app.RunDiscovery([]string{"one.txt", "two.txt"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("want one progress event per file, got %d", len(events))
	}
	if events[0]["docName"] != "one.txt" || events[1]["docName"] != "two.txt" {
		t.Errorf("progress payloads wrong: %+v", events)
	}
}

// --- BUILD-02 Phase 9 tests ------------------------------------------------

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

// TestExcludedVariants: an excluded spelling disappears from expansion,
// so a moved variant matches exactly one entity afterwards.
func TestExcludedVariants(t *testing.T) {
	got := engine.ExpandVariants(engine.Entity{
		Category:         "person_names",
		Canonical:        "Jean Muller",
		ExcludedVariants: []string{"J. Muller", "muller"},
	})
	for _, v := range got {
		if strings.EqualFold(v, "J. Muller") || strings.EqualFold(v, "Muller") {
			t.Errorf("excluded variant %q still expanded: %v", v, got)
		}
	}
	found := false
	for _, v := range got {
		if v == "Jean Muller" {
			found = true
		}
	}
	if !found {
		t.Errorf("canonical must survive exclusion of other variants: %v", got)
	}
}

// TestRunSmartDetectionReturnsCandidatesNotEntities: the binding returns
// candidates for review; the App holds no entity state to mutate, and the
// result carries counts and the offline status.
func TestRunSmartDetectionReturnsCandidatesNotEntities(t *testing.T) {
	app := NewApp()
	app.docs = []engine.Document{
		{Name: "a.txt", Format: engine.FormatTXT,
			Markdown: "Meeting with Marie Duval about Alpine Trust S.A. Later Marie Duval called again."},
	}
	// A zero options value means "no filtering", so this test keeps
	// asserting the unfiltered detector output (BUILD-04 CR13).
	res, err := app.RunSmartDetection([]string{"a.txt"}, []string{"CSSF"}, false, engine.SmartDetectOptions{})
	if err != nil {
		t.Fatalf("RunSmartDetection: %v", err)
	}
	if res.Cancelled || !strings.Contains(res.Status, "1 file(s)") {
		t.Errorf("status = %+v", res)
	}
	byText := map[string]engine.Candidate{}
	for _, c := range res.Candidates {
		byText[c.Text] = c
	}
	if c, ok := byText["Marie Duval"]; !ok || c.Category != "person_names" || c.Count < 2 {
		t.Errorf("Marie Duval candidate wrong: %+v", res.Candidates)
	}
	if c, ok := byText["Alpine Trust S.A."]; !ok || c.Category != "entity_names" {
		t.Errorf("suffix client candidate wrong: %+v", res.Candidates)
	}
}
