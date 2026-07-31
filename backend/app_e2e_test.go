// app_e2e_test.go — headless end-to-end tests through the BOUND APP layer.
//
// Why this file exists: every unit test in this repository passed while four
// bugs were reported against the assembled application. Unit tests prove one
// function; this file proves the sequence a user actually performs, driven
// through the same methods the frontend calls.
//
// It needs no Wails runtime: App.emit no-ops while a.ctx is nil, so importing,
// detecting, running and exporting all work in a plain `go test`.
//
// Each test names the reported issue it guards.
package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/ollama"
)

// placeholderShape matches any placeholder the registry can emit, e.g.
// "[PERSON_2]" or "[NATIONAL_ID_1]". Source text must never contain one.
var placeholderShape = regexp.MustCompile(`\[[A-Z][A-Z0-9_]*_[0-9]+\]`)

// importFixtures loads the named files from backend/testdata into a fresh App
// the same way the dialog path does (read the bytes once, hand them to the
// engine). It returns the App and the markdown of every document as it looked
// immediately after import, keyed by document name.
func importFixtures(t *testing.T, names ...string) (*App, map[string]string) {
	t.Helper()
	app := NewApp()
	paths := make([]string, 0, len(names))
	for _, n := range names {
		paths = append(paths, filepath.Join("testdata", n))
		if _, err := os.Stat(filepath.Join("testdata", n)); err != nil {
			t.Fatalf("fixture %q is missing: %v", n, err)
		}
	}
	result := app.importPaths(paths)
	if len(result.Errors) != 0 {
		t.Fatalf("importing the fixtures failed: %v", result.Errors)
	}
	if len(result.Documents) == 0 {
		t.Fatalf("importing %v produced no documents", names)
	}
	atImport := map[string]string{}
	for _, d := range result.Documents {
		atImport[d.Name] = d.Markdown
	}
	return app, atImport
}

// TestSourceTextSurvivesTheWholeFlow guards reported issues 1 and 4: the
// Import preview and the ORIGINAL pane showing anonymised values.
//
// It drives import → run → run again → fast re-run → same-format export, then
// asserts the source text every preview reads is byte-identical to what was
// imported and contains no placeholder. Both previews now read the SAME
// producer (ListDocuments / GetDocumentSource), so one assertion covers both.
func TestSourceTextSurvivesTheWholeFlow(t *testing.T) {
	app, atImport := importFixtures(t, "sample.txt", "sample.csv", "french.md", "report.docx")

	// Entities chosen to actually hit the fixtures, so the run is not a no-op:
	// a run that replaces nothing cannot prove that replacing does no damage.
	req := RunRequest{
		Entities: []engine.Entity{
			{Category: "person_names", Canonical: "Marie Duval"},
			{Category: "entity_names", Canonical: "Alpine Trust"},
		},
		Categories: engine.PresetSelection(engine.LevelAdvanced),
	}

	res, err := app.FastRerun(req)
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if res.Report.TotalReplacements == 0 {
		t.Fatal("the fixtures produced zero replacements, so this test would prove nothing")
	}
	if _, err := app.FastRerun(req); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	// The same-format export path takes a POINTER into app.docs
	// (sameFormatConfig), which is exactly the kind of access that could
	// rewrite a source document by accident.
	if _, err := app.sameFormatBytes("report.docx", "docx"); err != nil {
		t.Fatalf("same-format export failed: %v", err)
	}

	for _, info := range app.ListDocuments() {
		want, ok := atImport[info.Name]
		if !ok {
			t.Errorf("document %q appeared after import", info.Name)
			continue
		}
		if info.Markdown != want {
			t.Errorf("the import preview of %q changed after the run.\n got: %.120q\nwant: %.120q",
				info.Name, info.Markdown, want)
		}
		if ph := placeholderShape.FindString(info.Markdown); ph != "" {
			t.Errorf("the import preview of %q contains the placeholder %s; it must show the source text only",
				info.Name, ph)
		}

		// The ORIGINAL pane's producer must agree with the import list, byte
		// for byte. Two producers that disagree is the bug this guards.
		src := app.GetDocumentSource(info.Name)
		if !src.Found {
			t.Errorf("GetDocumentSource(%q) reports the document as missing", info.Name)
			continue
		}
		if src.Markdown != info.Markdown {
			t.Errorf("GetDocumentSource(%q) disagrees with the import list", info.Name)
		}
		if src.IsGrid != info.IsGrid || src.Truncated != info.PreviewTruncated {
			t.Errorf("GetDocumentSource(%q) disagrees with the import list on the preview flags", info.Name)
		}
	}
}

// TestGetDocumentSourceUnknownName: asking about a document that is not
// imported is an ordinary UI race (the user removed it while its result was on
// screen), so it resolves with Found=false rather than failing.
func TestGetDocumentSourceUnknownName(t *testing.T) {
	app, _ := importFixtures(t, "sample.txt")
	src := app.GetDocumentSource("never-imported.txt")
	if src.Found || src.Markdown != "" {
		t.Errorf("an unknown document must resolve empty, got %+v", src)
	}
}

// TestResultsCarryNoSourceCopy is the structural half of the same guard: if a
// copy of the source is ever put back on the result, the ORIGINAL pane has two
// possible sources again and the previous test can start passing while the UI
// is wrong. Reflection over the JSON shape is what a future reader will see.
func TestResultsCarryNoSourceCopy(t *testing.T) {
	app, atImport := importFixtures(t, "sample.txt")
	res, err := app.FastRerun(RunRequest{
		Entities:   []engine.Entity{{Category: "person_names", Canonical: "Marie Duval"}},
		Categories: engine.PresetSelection(engine.LevelMedium),
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	for _, rd := range res.Documents {
		source := atImport[rd.Name]
		if rd.Anonymised == source {
			t.Errorf("document %q was not anonymised, so this test proves nothing", rd.Name)
		}
		// The result must not smuggle the source text through any field.
		if strings.Contains(rd.JSON, source) {
			t.Errorf("document %q carries a copy of the source text in JSON", rd.Name)
		}
	}
}

// --- Reported issue 5: the entity-category merge -------------------------

// TestEntityCategoryMergeIsComplete guards the BUILD-06 merge of client_names
// and internal_names into entity_names. A half-done merge is the dangerous
// shape: the pipeline silently DROPS an entity whose category is not in the
// selection, so a value the user listed would simply never be replaced.
func TestEntityCategoryMergeIsComplete(t *testing.T) {
	for _, retired := range []string{"client_names", "internal_names"} {
		for _, cat := range engine.AllEntityCategories {
			if cat == retired {
				t.Errorf("%s is still an engine category", retired)
			}
		}
		for _, level := range []engine.Level{engine.LevelSoft, engine.LevelMedium, engine.LevelAdvanced} {
			if engine.PresetSelection(level)[retired] {
				t.Errorf("preset %q still switches %s on", level, retired)
			}
		}
	}
	if !engine.PresetSelection(engine.LevelSoft)["entity_names"] {
		t.Error("entity_names must be on at every preset, as both merged categories were")
	}

	// The placeholder the user sees, end to end.
	app, _ := importFixtures(t, "sample.txt")
	app.docs = append(app.docs, engine.Document{
		Name: "merge.txt", Format: engine.FormatTXT,
		Markdown: "Alpine Trust and Marie Duval both appear here.",
	})
	res, err := app.FastRerun(RunRequest{
		Entities: []engine.Entity{
			{Category: "entity_names", Canonical: "Alpine Trust"},
			{Category: "person_names", Canonical: "Marie Duval"},
		},
		Categories: engine.PresetSelection(engine.LevelMedium),
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	var merged string
	for _, rd := range res.Documents {
		if rd.Name == "merge.txt" {
			merged = rd.Anonymised
		}
	}
	if !strings.Contains(merged, "[ENTITY_1]") {
		t.Errorf("an entity_names value must become [ENTITY_n], got %q", merged)
	}
	for _, gone := range []string{"[CLIENT_", "[INTERNAL_"} {
		if strings.Contains(merged, gone) {
			t.Errorf("the retired placeholder %s] is still produced: %q", gone, merged)
		}
	}
}

// TestDiscoveryPromptsUseTheMergedCategory: the model is told the category
// keys by name, so a prompt left on the old keys silently produces proposals
// the engine then drops as unknown categories.
func TestDiscoveryPromptsUseTheMergedCategory(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("ollama", "client.go"))
	if err != nil {
		t.Fatalf("could not read the Ollama client: %v", err)
	}
	text := string(source)
	for _, retired := range []string{`"client_names"`, `"internal_names"`, "[CLIENT_1]"} {
		if strings.Contains(text, retired) {
			t.Errorf("backend/ollama/client.go still mentions %s; the prompts and the engine must agree on the category keys", retired)
		}
	}
	if !strings.Contains(text, `"entity_names"`) {
		t.Error("the prompts must demand the entity_names key")
	}
}

// --- Reported issue 3: the detection route switches ----------------------

// TestDetectionRouteDefaults: Smart detection needs nothing installed, so it
// is on; Local AI hands the document to a model, so the user turns it on.
func TestDetectionRouteDefaults(t *testing.T) {
	s := NewApp().GetSettings()
	if !s.UseSmartDetect {
		t.Error("Smart detection must be on by default")
	}
	if s.UseAI {
		t.Error("the local AI route must be off by default")
	}
}

// TestSessionSettingsRoundTrip guards a settings-loss bug found on the way:
// LoadSessionFromFile rebuilt Settings from a literal that omitted
// MinConfidence and SmartDetect, so loading a session silently reset the
// confidence floor to 0 and the smart tuning to the defaults. Both decide what
// gets replaced, and the user reads "load session" as "restore what I saved".
func TestSessionSettingsRoundTrip(t *testing.T) {
	saved := engine.Session{
		Settings: engine.SessionSettings{
			Level: "advanced", OllamaPort: 11500, Model: "m:1b", ContextSize: 4096,
			MinConfidence: 0.85,
			SmartDetect: &engine.SmartDetectOptions{
				MinLength: 7, MinOccurrences: 3, ExcludeCommonWords: false, MinConfidence: 0.4,
			},
		},
	}
	data, err := engine.SaveSession(saved)
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loaded, err := engine.LoadSession(data)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded.Settings.MinConfidence != 0.85 {
		t.Errorf("the confidence floor did not survive the file: %v", loaded.Settings.MinConfidence)
	}
	if loaded.Settings.SmartDetect == nil || loaded.Settings.SmartDetect.MinLength != 7 {
		t.Errorf("the smart tuning did not survive the file: %+v", loaded.Settings.SmartDetect)
	}

	// And the same fields must survive the App-side restore, which is where
	// they were being dropped.
	app := NewApp()
	app.applyRestoredSettings(loaded)
	got := app.GetSettings()
	if got.MinConfidence != 0.85 {
		t.Errorf("the restored confidence floor is %v, want 0.85", got.MinConfidence)
	}
	if got.SmartDetect.MinLength != 7 || got.SmartDetect.ExcludeCommonWords {
		t.Errorf("the restored smart tuning is %+v, want the saved one", got.SmartDetect)
	}
	if !got.UseSmartDetect {
		t.Error("a file that says nothing about the smart route must restore it ON")
	}
}

// TestDeepScanRefusedWhenTheRouteIsOff: Settings.UseAI was stored, written
// into every session file, and read by no decision path. The only thing
// standing between a switched-off route and a running model was a boolean the
// frontend computed, so a stale request could start the AI pass the user had
// turned off. Go decides now, and says so in the report.
func TestDeepScanRefusedWhenTheRouteIsOff(t *testing.T) {
	var chatCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"m"}]}`))
		case "/api/chat":
			chatCalls++
			resp, _ := json.Marshal(map[string]interface{}{
				"message": map[string]string{"role": "assistant",
					"content": `{"entity_names":[],"project_names":[],"person_names":[]}`},
			})
			w.Write(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	app := NewApp()
	app.llm = ollama.New(srv.URL)
	app.settings.UseAI = false // the switch the user operates
	app.docs = []engine.Document{
		{Name: "a.txt", Format: engine.FormatTXT, Markdown: "Marie Duval wrote."},
	}

	// The request asks for the deep scan anyway.
	res, err := app.runPipelineBlocking(context.Background(), RunRequest{UseDeepScan: true})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if chatCalls != 0 {
		t.Errorf("the model was called %d time(s) with the route switched off", chatCalls)
	}
	if !strings.Contains(res.Report.LLMPass, "switched off") {
		t.Errorf("the report must say why the deep scan did not run, got %q", res.Report.LLMPass)
	}
	if len(res.Report.Warnings) == 0 {
		t.Error("a requested pass that did not run is worth a warning")
	}
}

// TestDeepScanSkippedWhenOllamaIsAbsent: the switch being on is not enough
// either. Reaching for a model that is not there used to surface as a
// per-document failure warning; it is now one clear sentence.
func TestDeepScanSkippedWhenOllamaIsAbsent(t *testing.T) {
	app := NewApp()
	app.llm = ollama.New("http://127.0.0.1:1") // nothing listening
	app.settings.UseAI = true
	app.docs = []engine.Document{
		{Name: "a.txt", Format: engine.FormatTXT, Markdown: "Marie Duval wrote."},
	}
	res, err := app.runPipelineBlocking(context.Background(), RunRequest{UseDeepScan: true})
	if err != nil {
		t.Fatalf("a missing Ollama must degrade, not fail: %v", err)
	}
	if !strings.Contains(res.Report.LLMPass, "Ollama did not answer") {
		t.Errorf("the report must name the reason, got %q", res.Report.LLMPass)
	}
}
