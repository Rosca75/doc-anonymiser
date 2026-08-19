//go:build integration

// app_detect_integration_test.go — tests for the unified detection run.
//
// TIER: integration (docs/TESTING.md). Every test here drives App.RunDetection
// against a MOCK Ollama server (httptest) and asserts the bound detection
// wiring end to end: the event stream, cancellation, family folding across
// routes, and signal-based discovery through the App. That is external-boundary
// wiring with a stand-in for the real model, which the integration tier owns;
// it stays hermetic (loopback httptest, zero real network). Its helpers
// (recorder, detectionApp, signalApp, scopeChatServer) and newTestApp, shared
// from app_validation_integration_test.go, are compiled only in this tier.
//
// These guard the reported issue "detection sometimes does not complete, the
// progress is difficult to follow". Each test names the specific way the old
// two-call design could leave the UI stuck or lying.
package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/ollama"
)

// recorder captures the events an App emits, so a test can assert on the
// stream the UI would actually receive.
type recorder struct {
	mu     sync.Mutex
	events []struct {
		Name    string
		Payload interface{}
	}
}

func (r *recorder) add(name string, payload interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, struct {
		Name    string
		Payload interface{}
	}{name, payload})
}

func (r *recorder) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.events {
		out = append(out, e.Name)
	}
	return out
}

func (r *recorder) progress() []DetectionProgress {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []DetectionProgress
	for _, e := range r.events {
		if e.Name != "detection:progress" {
			continue
		}
		if p, ok := e.Payload.(DetectionProgress); ok {
			out = append(out, p)
		}
	}
	return out
}

func (r *recorder) count(name string) int {
	n := 0
	for _, got := range r.names() {
		if got == name {
			n++
		}
	}
	return n
}

// withRecorder swaps the Wails emit indirection for a recorder. a.ctx has to
// be non-nil for emit() to fire at all, and it is never dereferenced here.
func withRecorder(t *testing.T, a *App) *recorder {
	t.Helper()
	rec := &recorder{}
	a.ctx = context.Background()
	previous := runtimeEventsEmit
	runtimeEventsEmit = func(_ *App, name string, payload interface{}) { rec.add(name, payload) }
	t.Cleanup(func() { runtimeEventsEmit = previous })
	return rec
}

// detectionApp is an App with three small documents and Smart detection on.
func detectionApp() *App {
	app := NewApp()
	app.docs = []engine.Document{
		{Name: "a.txt", Format: engine.FormatTXT, Markdown: "Alpine Trust S.A. met Marie Duval. Alpine Trust signed."},
		{Name: "b.txt", Format: engine.FormatTXT, Markdown: "Borealis Fund GmbH replied. Borealis Fund agreed."},
		{Name: "c.txt", Format: engine.FormatTXT, Markdown: "Zephyr Capital Ltd invoiced. Zephyr Capital paid."},
	}
	return app
}

// TestDetectionAlwaysEndsWithATerminalEvent is the core of the "does not
// complete" report: the progress bar used to be cleared only by a `finally`
// in the caller, so anything that escaped in between left it spinning.
func TestDetectionAlwaysEndsWithATerminalEvent(t *testing.T) {
	app := detectionApp()
	rec := withRecorder(t, app)

	if _, err := app.RunDetection([]string{"a.txt", "b.txt", "c.txt"}, nil, nil); err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if got := rec.count("detection:done"); got != 1 {
		t.Errorf("want exactly one detection:done, got %d", got)
	}
	if got := rec.count("detection:error"); got != 0 {
		t.Errorf("a clean run must not emit detection:error, got %d", got)
	}
	if rec.count("detection:progress") == 0 {
		t.Error("a run over three files must report progress")
	}
}

// TestBuiltInPatternsAloneRunsNoSmartPhase: the Smart PHASE is Smart detection's
// two DISCOVERY methods. Built-in pattern matching is not one of them: it
// produces direct matches at anonymisation time, so having it on must not, by
// itself, make the detect button produce Suggestions.
func TestBuiltInPatternsAloneRunsNoSmartPhase(t *testing.T) {
	app := detectionApp()
	app.settings.UseHeuristicDiscovery = false
	app.settings.SignalSuggestionSources = engine.SignalSourceSelection{engine.SignalSourceEmail: {
		engine.DerivationEmailPerson:       false,
		engine.DerivationEmailOrganisation: false,
	}}
	app.settings.UseBuiltInPatterns = true // on, and still not a discovery method
	app.settings.UseLocalAI = false
	withRecorder(t, app)

	res, err := app.RunDetection([]string{"a.txt"}, nil, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	for _, p := range res.Phases {
		if p == PhaseSmart {
			t.Errorf("every discovery method is off, so the smart phase must not run; phases %v",
				res.Phases)
		}
	}
	if len(res.Suggestions) != 0 {
		t.Errorf("no smart phase means no Suggestions, got %+v", res.Suggestions)
	}
}

// TestDetectionWithNoRouteOnStillEnds: with every switch off there is nothing
// to run, and that has to be said, not left as a spinning bar.
func TestDetectionWithNoRouteOnStillEnds(t *testing.T) {
	app := detectionApp()
	app.settings.UseHeuristicDiscovery = false
	app.settings.SignalSuggestionSources = engine.SignalSourceSelection{engine.SignalSourceEmail: {
		engine.DerivationEmailPerson:       false,
		engine.DerivationEmailOrganisation: false,
	}}
	app.settings.UseLocalAI = false
	rec := withRecorder(t, app)

	res, err := app.RunDetection([]string{"a.txt"}, nil, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if len(res.Phases) != 0 || len(res.Suggestions) != 0 {
		t.Errorf("no route is on, so nothing should have run: %+v", res)
	}
	if !strings.Contains(res.Status, "no detection route") {
		t.Errorf("the status must say why nothing happened, got %q", res.Status)
	}
	if rec.count("detection:done") != 1 {
		t.Error("even a run with nothing to do ends with its terminal event")
	}
}

// TestDetectionProgressNeverGoesBackwards guards the second half of the
// report. With two routes the bar used to rewind to file 1 of a SMALLER total
// when the AI pass started, which reads as a run that restarted itself.
func TestDetectionProgressNeverGoesBackwards(t *testing.T) {
	// A model that answers instantly, so both routes run over all three files.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"m"}]}`))
		case "/api/chat":
			resp, _ := json.Marshal(map[string]interface{}{
				"message": map[string]string{
					"role":    "assistant",
					"content": `{"entity_names":["Alpine Trust"],"project_names":[],"person_names":[]}`,
				},
			})
			w.Write(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	app := detectionApp()
	app.llm = ollama.New(srv.URL)
	app.settings.UseLocalAI = true
	rec := withRecorder(t, app)

	res, err := app.RunDetection([]string{"a.txt", "b.txt", "c.txt"}, nil, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if len(res.Phases) != 2 {
		t.Fatalf("both routes should have run, got %v", res.Phases)
	}

	events := rec.progress()
	if len(events) < 4 {
		t.Fatalf("want progress from both routes, got %d events", len(events))
	}
	previous := -1.0
	for i, p := range events {
		if p.Fraction < previous {
			t.Errorf("progress went backwards at event %d: %.3f after %.3f (phase %q)",
				i, p.Fraction, previous, p.Phase)
		}
		if p.Fraction < 0 || p.Fraction > 1 {
			t.Errorf("event %d reports an impossible fraction %.3f", i, p.Fraction)
		}
		previous = p.Fraction
	}
	// And the two routes are distinguishable, so the caption can name them.
	if events[0].Phase != PhaseSmart || events[len(events)-1].Phase != PhaseLocalAI {
		t.Errorf("the routes must run in order, got %q then %q",
			events[0].Phase, events[len(events)-1].Phase)
	}
}

// TestOverallFractionIsMonotonicAcrossUnevenPhases pins the arithmetic
// directly, including the case that produced the visible rewind: the second
// route reading FEWER files than the first.
func TestOverallFractionIsMonotonicAcrossUnevenPhases(t *testing.T) {
	previous := -1.0
	check := func(phaseIndex, phaseCount, docIndex, docCount, chunkIndex, chunkCount int) {
		got := overallFraction(phaseIndex, phaseCount, docIndex, docCount, chunkIndex, chunkCount)
		if got < previous {
			t.Errorf("fraction went backwards: %.3f after %.3f (phase %d, doc %d/%d)",
				got, previous, phaseIndex, docIndex, docCount)
		}
		previous = got
	}
	// reads ten files, phase 1 reads two (eight were too large).
	for i := 0; i < 10; i++ {
		check(0, 2, i, 10, 0, 0)
	}
	for i := 0; i < 2; i++ {
		for c := 0; c < 4; c++ {
			check(1, 2, i, 2, c, 4)
		}
	}
	if previous > 1 {
		t.Errorf("the fraction must stay within 0..1, ended at %.3f", previous)
	}
}

// TestDetectionKeepsGoingWhenOneFileFails: one file the model chokes on used
// to abort the whole run with an error, throwing away every suggestion found
// in the others.
func TestDetectionKeepsGoingWhenOneFileFails(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"m"}]}`))
		case "/api/chat":
			calls++
			if calls == 2 { // the second document
				http.Error(w, "model exploded", http.StatusInternalServerError)
				return
			}
			resp, _ := json.Marshal(map[string]interface{}{
				"message": map[string]string{
					"role":    "assistant",
					"content": `{"entity_names":["Alpine Trust"],"project_names":[],"person_names":[]}`,
				},
			})
			w.Write(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	app := detectionApp()
	app.llm = ollama.New(srv.URL)
	app.settings.UseLocalAI = true
	rec := withRecorder(t, app)

	res, err := app.RunDetection([]string{"a.txt", "b.txt", "c.txt"}, nil, nil)
	if err != nil {
		t.Fatalf("one failing file must not fail the run: %v", err)
	}
	if len(res.Errors) != 1 {
		t.Errorf("the failure must be reported, got %v", res.Errors)
	}
	if !strings.Contains(strings.Join(res.Errors, " "), "b.txt") {
		t.Errorf("the report must name the file that failed: %v", res.Errors)
	}
	if len(res.Suggestions) == 0 {
		t.Error("the offline route's findings must survive an AI failure")
	}
	if rec.count("detection:done") != 1 {
		t.Error("a run with a failed file still ends with detection:done")
	}
}

// TestDetectionCancellationIsHonestAboutIt: both results carried a Cancelled
// flag that the caller never read, so a cancelled run reported "detection
// done". The flag now comes back on one result, with the partial findings.
func TestDetectionCancellationIsHonestAboutIt(t *testing.T) {
	app := detectionApp()
	// A big document so the offline pass has something to be interrupted in.
	app.docs = append(app.docs, engine.Document{
		Name: "big.txt", Format: engine.FormatTXT,
		Markdown: strings.Repeat("Alpine Trust S.A. met Marie Duval in Luxembourg. ", 20000),
	})
	rec := withRecorder(t, app)

	// Cancel as soon as the run has started.
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			app.mu.Lock()
			running := app.cancelDetection != nil
			app.mu.Unlock()
			if running {
				app.CancelDetection()
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()

	res, err := app.RunDetection([]string{"big.txt", "a.txt", "b.txt", "c.txt"}, nil, nil)
	if err != nil {
		t.Fatalf("a cancelled run resolves, it does not fail: %v", err)
	}
	if !res.Cancelled {
		t.Error("the result must say it was cancelled; the old code dropped this flag")
	}
	if !strings.Contains(res.Status, "cancelled") {
		t.Errorf("the status must say so too, got %q", res.Status)
	}
	if rec.count("detection:done") != 1 {
		t.Error("a cancelled run still ends with exactly one terminal event")
	}
}

// TestDetectionRefusesAConcurrentRun: one cancellation slot means one run.
func TestDetectionRefusesAConcurrentRun(t *testing.T) {
	app := detectionApp()
	_, cancel := context.WithCancel(context.Background())
	app.cancelDetection = cancel
	defer cancel()

	if _, err := app.RunDetection([]string{"a.txt"}, nil, nil); err == nil {
		t.Error("a second run must be refused while one is in flight")
	} else if !strings.Contains(err.Error(), "already in progress") {
		t.Errorf("the refusal must say why: %v", err)
	}
}

// scopeChatServer is an Ollama stand-in that records the user content of every
// /api/chat call, so a test can prove exactly which document text the local AI
// was handed.
func scopeChatServer(seen *[]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"m"}]}`))
		case "/api/chat":
			var body struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			for _, m := range body.Messages {
				if m.Role == "user" {
					*seen = append(*seen, m.Content)
				}
			}
			resp, _ := json.Marshal(map[string]interface{}{
				"message": map[string]string{
					"role":    "assistant",
					"content": `{"entity_names":[],"project_names":[],"person_names":[]}`,
				},
			})
			w.Write(resp)
		default:
			http.NotFound(w, r)
		}
	}))
}

// TestDetectionAIScopeLimitsToPageRange is the whole point of the feature: with
// a scope set, the local AI must see ONLY the chosen document's chosen pages,
// never the rest, so a small model is not handed "too much".
func TestDetectionAIScopeLimitsToPageRange(t *testing.T) {
	var seen []string
	srv := scopeChatServer(&seen)
	defer srv.Close()

	app := NewApp()
	app.docs = []engine.Document{
		{
			Name: "big.txt", Format: engine.FormatTXT, Unit: engine.UnitLine,
			Markdown: "alpha line one\nbravo line two\ncharlie line three\ndelta line four\n",
		},
	}
	app.llm = ollama.New(srv.URL)
	app.settings.UseLocalAI = true
	app.settings.UseHeuristicDiscovery = false // isolate the AI route
	withRecorder(t, app)

	res, err := app.RunDetection([]string{"big.txt"}, nil,
		&AIScope{DocName: "big.txt", Pages: []int{2, 3}})
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("a valid scope must not error: %v", res.Errors)
	}

	joined := strings.Join(seen, "\n")
	if joined == "" {
		t.Fatal("the local AI was never called")
	}
	for _, want := range []string{"bravo", "charlie"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the scoped scan missed line %q; saw %q", want, joined)
		}
	}
	for _, leak := range []string{"alpha", "delta"} {
		if strings.Contains(joined, leak) {
			t.Errorf("the scope leaked out-of-range line %q; saw %q", leak, joined)
		}
	}
}

// TestDetectionAIScopeOutOfRangeReportsButFinishes: a stale or hand-typed range
// is the user's request, so it is reported, not crashed on, and the run still
// ends cleanly.
func TestDetectionAIScopeOutOfRangeReportsButFinishes(t *testing.T) {
	var seen []string
	srv := scopeChatServer(&seen)
	defer srv.Close()

	app := NewApp()
	app.docs = []engine.Document{
		{Name: "small.txt", Format: engine.FormatTXT, Unit: engine.UnitLine, Markdown: "one\ntwo\n"},
	}
	app.llm = ollama.New(srv.URL)
	app.settings.UseLocalAI = true
	app.settings.UseHeuristicDiscovery = false
	withRecorder(t, app)

	res, err := app.RunDetection([]string{"small.txt"}, nil,
		&AIScope{DocName: "small.txt", Pages: []int{5, 9}})
	if err != nil {
		t.Fatalf("an out-of-range scope must not fail the run: %v", err)
	}
	if len(res.Errors) == 0 || !strings.Contains(strings.Join(res.Errors, " "), "out of bounds") {
		t.Errorf("the out-of-range scope must be reported, got %v", res.Errors)
	}
	if len(seen) != 0 {
		t.Errorf("nothing should have been sent to the model for an invalid range, saw %q", seen)
	}
}

// TestDetectionAIScopeDiscontiguousPages proves a discontiguous page set (the
// CR3 feature, "1,3") reaches the local AI as exactly those pages, with the
// pages between them left out.
func TestDetectionAIScopeDiscontiguousPages(t *testing.T) {
	var seen []string
	srv := scopeChatServer(&seen)
	defer srv.Close()

	app := NewApp()
	app.docs = []engine.Document{
		{
			Name: "big.txt", Format: engine.FormatTXT, Unit: engine.UnitLine,
			Markdown: "alpha line one\nbravo line two\ncharlie line three\ndelta line four\n",
		},
	}
	app.llm = ollama.New(srv.URL)
	app.settings.UseLocalAI = true
	app.settings.UseHeuristicDiscovery = false // isolate the AI route
	withRecorder(t, app)

	res, err := app.RunDetection([]string{"big.txt"}, nil,
		&AIScope{DocName: "big.txt", Pages: []int{1, 3}})
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("a valid scope must not error: %v", res.Errors)
	}

	joined := strings.Join(seen, "\n")
	for _, want := range []string{"alpha", "charlie"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the discontiguous scan missed line %q; saw %q", want, joined)
		}
	}
	for _, leak := range []string{"bravo", "delta"} {
		if strings.Contains(joined, leak) {
			t.Errorf("the discontiguous scope leaked line %q; saw %q", leak, joined)
		}
	}
}

// TestClassifyRespectsTheAIScope is the scope-leak guard, and its absence is
// what let the leak exist.
//
// The classification call is the LARGER of the two the Local AI route makes, so
// scoping only the discovery call left the user's "just page 1" handing the
// whole batch to the model: about half a scoped run's prompt tokens, spent on
// text the user had explicitly excluded, while the interface promised otherwise.
func TestClassifyRespectsTheAIScope(t *testing.T) {
	var seen []string
	srv := scopeChatServer(&seen)
	defer srv.Close()

	app := NewApp()
	// Two pages, each with a name only heuristic discovery would find. Page 1's
	// name may be classified; page 2's must never reach the model at all.
	app.docs = []engine.Document{{
		Name: "two.txt", Format: engine.FormatTXT, Unit: engine.UnitLine,
		Markdown: "Alpine Trust signed. Alpine Trust confirmed. Alpine Trust paid.\n" +
			"Borealis Fund objected. Borealis Fund replied. Borealis Fund withdrew.\n",
	}}
	app.llm = ollama.New(srv.URL)
	app.settings.UseLocalAI = true
	app.settings.UseHeuristicDiscovery = true // it is the smart route that gets classified
	withRecorder(t, app)

	if _, err := app.RunDetection([]string{"two.txt"}, nil,
		&AIScope{DocName: "two.txt", Pages: []int{1}}); err != nil {
		t.Fatalf("RunDetection: %v", err)
	}

	// The classification call is whichever recorded payload lists suggestions:
	// it is the one built as "- <name> | context: ...", never document prose.
	var classify string
	for _, payload := range seen {
		if strings.Contains(payload, "- Alpine Trust") || strings.Contains(payload, "- Borealis Fund") {
			classify = payload
		}
	}
	if classify == "" {
		t.Fatalf("no classification call was made, so the scope cannot be proven; payloads were %q", seen)
	}
	if !strings.Contains(classify, "Alpine Trust") {
		t.Errorf("the in-scope name must still be classified; the classify payload was %q", classify)
	}
	if strings.Contains(classify, "Borealis Fund") {
		t.Errorf("the classification call leaked a page the scope excluded; the payload was %q", classify)
	}
}

// TestClassifyPayloadIsFoldedAndBounded pins the two compounding wastes in the
// classification payload, both on SHAPE rather than on a byte total: a byte
// assertion is a wall-clock proxy that breaks the moment the fixture text
// changes, and it would not say which of the two regressed.
//
// Folding first is also the correctness half. Every other part of the system
// works with the folded list, so an unfolded classification payload was the one
// place a family's spellings were treated as separate names, and a category
// assigned to one of them could split a family that was about to be folded.
func TestClassifyPayloadIsFoldedAndBounded(t *testing.T) {
	var seen []string
	srv := scopeChatServer(&seen)
	defer srv.Close()

	app := NewApp()
	app.docs = []engine.Document{{
		Name: "fold.txt", Format: engine.FormatTXT,
		Markdown: "Alpine Trust signed here. Alpine Trust S.A. signed there. " +
			"Alpine Trust confirmed later. Alpine Trust S.A. confirmed too.\n",
	}}
	app.llm = ollama.New(srv.URL)
	app.settings.UseLocalAI = true
	app.settings.UseHeuristicDiscovery = true
	withRecorder(t, app)

	if _, err := app.RunDetection([]string{"fold.txt"}, nil, nil); err != nil {
		t.Fatalf("RunDetection: %v", err)
	}

	var classify string
	for _, payload := range seen {
		if strings.Contains(payload, "- Alpine Trust") {
			classify = payload
		}
	}
	if classify == "" {
		t.Fatalf("no classification call was made; payloads were %q", seen)
	}
	rows := 0
	for _, line := range strings.Split(strings.TrimSpace(classify), "\n") {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		rows++
		// Split the row into the NAME being classified and its context, so
		// each assertion reads the half it is about. Matching the whole line
		// would let the context's own words answer for the name.
		name, context, _ := strings.Cut(strings.TrimPrefix(line, "- "), " | context: ")
		// One row per family: the longer spelling folded into the shorter one
		// before the model was ever asked about either.
		if strings.TrimSpace(name) == "Alpine Trust S.A." {
			t.Errorf("the classify payload names a folded spelling as its own row: %q", line)
		}
		// One context snippet per row. Several are joined by " ... ", so a
		// second one shows up as that separator inside the context.
		if strings.Contains(context, " ... ") {
			t.Errorf("a classify row carries more than one context snippet: %q", line)
		}
	}
	if rows != 1 {
		t.Errorf("the folded family must be ONE row in the classify payload, got %d; payload was %q", rows, classify)
	}
}

// TestDetectionRespectsTheRouteSwitches: Go decides, not the caller.
func TestDetectionRespectsTheRouteSwitches(t *testing.T) {
	app := detectionApp()
	// UseLocalAI is on but there is no Ollama at this port, so the route cannot run
	// and must not be reported as having run.
	app.llm = ollama.New("http://127.0.0.1:1")
	app.settings.UseLocalAI = true
	withRecorder(t, app)

	res, err := app.RunDetection([]string{"a.txt"}, nil, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if len(res.Phases) != 1 || res.Phases[0] != PhaseSmart {
		t.Errorf("only the offline route can run without Ollama, got %v", res.Phases)
	}
}

// TestDetectionFoldsFamiliesAcrossRoutes: a heuristic finding and a model one
// that are spellings of the same thing come back as ONE value.
//
// Folding per route would leave them unmerged, which is exactly the case that
// matters: left as two values the shorter fires inside the longer, the text
// reads "[ENTITY_1] S.A.", the legal form leaks, and two numbers are spent on
// one company.
//
// The two routes must agree on the CATEGORY for a family to form, which is why
// this fixture uses a name both file under entity_names. That is the rule
// working, not a limitation: a person "Delta" and an organisation "Delta
// Industries" are an intersection, not a family, and folding them would file a
// human being under an organisation.
func TestDetectionFoldsFamiliesAcrossRoutes(t *testing.T) {
	// The model proposes the LONGER form; the offline route finds the shorter.
	app := newTestApp(t, func(string) string {
		return `{"entity_names":["Alpine Trust S.A."],"person_names":[],"brand_names":[]}`
	})
	app.docs = []engine.Document{{
		Name: "a.txt", Format: engine.FormatTXT,
		Markdown: "Alpine Trust is here. Alpine Trust again. Alpine Trust S.A. signed the deed.\n",
	}}
	app.settings.UseLocalAI = true
	app.settings.UseHeuristicDiscovery = true

	res, err := app.RunDetection([]string{"a.txt"}, nil, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}

	// Exactly one row must mention Alpine Trust, and it must be the shorter
	// spelling with the longer one folded in as a spelling.
	var rows []engine.Suggestion
	for _, s := range res.Suggestions {
		if strings.Contains(strings.ToLower(s.MainText), "alpine trust") {
			rows = append(rows, s)
		}
	}

	if len(rows) != 1 {
		t.Fatalf("the two spellings must come back as one Value family, got %+v", rows)
	}
	if rows[0].MainText != "Alpine Trust" {
		t.Errorf("the shorter form is the main text, got %q", rows[0].MainText)
	}
	if len(rows[0].Spellings) != 1 || rows[0].Spellings[0] != "Alpine Trust S.A." {
		t.Errorf("the longer form must fold in as a spelling, got %v", rows[0].Spellings)
	}
	// The methods of BOTH routes survive the fold, which is the guarantee the
	// split response shape used to break: the frontend mapped one route's rows
	// into a shape with no spellings field at all.
	if len(rows[0].DiscoveryMethods) == 0 {
		t.Error("a folded family must still say which methods found it")
	}
}

// --- Signal-based discovery, end to end through the bound app ---------------
//
// The engine tests cover the rules; these cover the WIRING, which is where the
// feature would silently do nothing: a phase that never runs, a source setting
// nobody reads, or findings that reach a list the frontend does not read.

// signalApp is an App with an email address in one file and the text it points at
// in another, which is the shape signal-based discovery exists for.
func signalApp() *App {
	app := NewApp()
	app.docs = []engine.Document{
		{
			Name: "mail.md", Format: engine.FormatTXT,
			Markdown: "From pierre.dupont@tpps.com about the fee note.\n",
		},
		{
			Name: "engagement.md", Format: engine.FormatTXT,
			Markdown: "Contact Pierre Dupont at Tpps France for approval.\n",
		},
	}
	// Heuristic discovery off, so what comes back is the signal method's work and
	// not a heuristic finding that happens to agree with it.
	app.settings.UseHeuristicDiscovery = false
	app.settings.UseLocalAI = false
	return app
}

// findSuggestion returns the suggestion with the given main text, or nil.
func findSuggestion(res *DetectionResult, text string) *engine.Suggestion {
	for i := range res.Suggestions {
		if strings.EqualFold(res.Suggestions[i].MainText, text) {
			return &res.Suggestions[i]
		}
	}
	return nil
}

// TestSignalDiscoveryRunsAsPartOfSmartDetection is acceptance criterion 2: with
// the source enabled and the text present elsewhere, the person and the
// organisation come back as Suggestions carrying their evidence.
func TestSignalDiscoveryRunsAsPartOfSmartDetection(t *testing.T) {
	app := signalApp()
	withRecorder(t, app)

	res, err := app.RunDetection([]string{"mail.md", "engagement.md"}, nil, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	// The phase ran even with heuristic discovery off: signal-based discovery is a
	// Smart detection method in its own right.
	if len(res.Phases) != 1 || res.Phases[0] != PhaseSmart {
		t.Fatalf("the smart phase must run for the signal method alone, got %v", res.Phases)
	}

	person := findSuggestion(res, "Pierre Dupont")
	if person == nil {
		t.Fatalf("the local part must find the person, got %+v", res.Suggestions)
	}
	if person.Category != engine.CatPersonNames {
		t.Errorf("the person must be filed under person_names, got %q", person.Category)
	}
	if len(person.DiscoveryMethods) != 1 || person.DiscoveryMethods[0] != engine.MethodSignal {
		t.Errorf("the method must be signal, got %v", person.DiscoveryMethods)
	}
	if len(person.Evidence) == 0 || person.Evidence[0].SignalText != "pierre.dupont@tpps.com" {
		t.Errorf("the evidence must name the address it came from, got %+v", person.Evidence)
	}

	if org := findSuggestion(res, "Tpps France"); org == nil {
		t.Errorf("the domain must find the organisation name, got %+v", res.Suggestions)
	} else if org.Category != engine.CatEntityNames {
		t.Errorf("the organisation must be filed under entity_names, got %q", org.Category)
	}
}

// TestSignalFindingsAreSuggestionsNeverValues is acceptance criterion 3: a
// signal-derived finding follows the same review lifecycle as every other
// Suggestion, so a run cannot replace anything on its strength alone.
func TestSignalFindingsAreSuggestionsNeverValues(t *testing.T) {
	app := signalApp()
	withRecorder(t, app)

	res, err := app.RunDetection([]string{"mail.md", "engagement.md"}, nil, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if len(res.Suggestions) == 0 {
		t.Fatal("the fixture must produce at least one suggestion for this to mean anything")
	}

	// Nothing was accepted, so a run right now replaces the email (a direct match)
	// and leaves the person and the organisation in clear text.
	out := runOnce(t, app, RunRequest{})
	text := out.Documents[1].Anonymised
	if !strings.Contains(text, "Pierre Dupont") {
		t.Errorf("an unreviewed suggestion must NOT be replaced: %q", text)
	}
	if strings.Contains(out.Documents[0].Anonymised, "pierre.dupont@tpps.com") {
		t.Errorf("the email itself is a direct match and must be replaced: %q",
			out.Documents[0].Anonymised)
	}
}

// TestDisablingTheEmailSourceKeepsEmailAnonymisation is acceptance criterion 4,
// through the bound app: the setting stops the Suggestions and nothing else.
func TestDisablingTheEmailSourceKeepsEmailAnonymisation(t *testing.T) {
	app := signalApp()
	app.settings.SignalSuggestionSources = engine.SignalSourceSelection{
		engine.SignalSourceEmail: {
			engine.DerivationEmailPerson:       false,
			engine.DerivationEmailOrganisation: false,
		},
	}
	withRecorder(t, app)

	res, err := app.RunDetection([]string{"mail.md", "engagement.md"}, nil, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if len(res.Suggestions) != 0 {
		t.Errorf("with the source off there must be no signal suggestions, got %+v", res.Suggestions)
	}
	// With every discovery method off there is no phase to run, and the status
	// says so rather than reporting a scan that found nothing.
	if len(res.Phases) != 0 {
		t.Errorf("no discovery method is on, so no phase should run, got %v", res.Phases)
	}

	// The address is still anonymised: that is Built-in patterns and the email
	// category, neither of which this setting touches.
	out := runOnce(t, app, RunRequest{})
	if strings.Contains(out.Documents[0].Anonymised, "pierre.dupont@tpps.com") {
		t.Errorf("switching off email-derived Suggestions must not stop email anonymisation: %q",
			out.Documents[0].Anonymised)
	}
}

// TestAcceptedSignalSuggestionKeepsItsEvidence is acceptance criterion 6 at the
// bound-app boundary: a Value the frontend sends back with its methods and
// evidence is replaced, and its provenance decides precedence rather than being
// dropped somewhere in the middle.
func TestAcceptedSignalSuggestionKeepsItsEvidence(t *testing.T) {
	app := signalApp()
	withRecorder(t, app)

	res, err := app.RunDetection([]string{"mail.md", "engagement.md"}, nil, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	person := findSuggestion(res, "Pierre Dupont")
	if person == nil {
		t.Fatalf("no person suggestion to accept, got %+v", res.Suggestions)
	}

	// Exactly what the frontend sends when the user accepts the row.
	accepted := engine.Value{
		Category:         person.Category,
		MainText:         person.MainText,
		Spellings:        person.Spellings,
		DiscoveryMethods: person.DiscoveryMethods,
		Evidence:         person.Evidence,
	}
	out := runOnce(t, app, RunRequest{Values: []engine.Value{accepted}})
	if strings.Contains(out.Documents[1].Anonymised, "Pierre Dupont") {
		t.Errorf("an accepted Value must be replaced: %q", out.Documents[1].Anonymised)
	}
	// The match class it resolves to is the one its methods imply, not the
	// user-defined fallback a lost provenance would produce.
	if got := engine.MatchClassForMethods(accepted.DiscoveryMethods); got != engine.MatchClassSmartDiscovered {
		t.Errorf("a signal-derived Value must rank as smart_discovered, got %q", got)
	}
}
