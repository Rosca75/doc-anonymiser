// app_reset_test.go — tests the two session reset paths.
//
// These exist because the reported leak was precisely that a reset cleared the
// frontend's copy of the run but not the Go-side registry and removed list, so
// the next run reused old placeholder numbers and kept hiding previously removed
// values. ResetRun and ResetSession are the Go half of the fix, so they carry
// the assertions that the state the frontend can no longer see is actually gone.
package backend

import (
	"testing"

	"doc-anonymiser/backend/engine"
)

// filledSession builds an App that has been through a run and a removal, so a
// reset has something visible to clear.
func filledSession() *App {
	app := NewApp()
	app.docs = []engine.Document{{Name: "a.txt", Format: engine.FormatTXT, Markdown: "hello"}}
	reg := engine.NewRegistry()
	reg.Assign("entity_names", "Alpine Trust")
	app.registry = reg
	app.results = &engine.Results{}
	app.lastReq = &RunRequest{}
	app.removed = []engine.RemovedValue{{MainText: "Marie Duval", Category: "person_names"}}
	// A non-default setting so a reset-to-defaults is observable.
	app.settings.OllamaPort = 12345
	app.settings.Presets = depthPresets(engine.PresetThorough)
	app.settings.Country = engine.CountryFR
	return app
}

func TestResetRunClearsRunStateKeepsDocsAndSettings(t *testing.T) {
	app := filledSession()

	app.ResetRun()

	if app.registry != nil {
		t.Error("ResetRun left the placeholder registry behind, so numbering would not restart")
	}
	if app.results != nil {
		t.Error("ResetRun left the last run's results behind")
	}
	if app.lastReq != nil {
		t.Error("ResetRun left the remembered request behind, so the same-format export would replay it")
	}
	if len(app.removed) != 0 {
		t.Errorf("ResetRun kept %d removed values, so they would still be hidden on the next run", len(app.removed))
	}
	// The documents and the configuration are a backward move's to keep.
	if len(app.docs) != 1 {
		t.Errorf("ResetRun dropped the documents (%d left), but a run reset must keep them", len(app.docs))
	}
	thoroughRow := app.settings.Presets[engine.PresetKey(engine.ScopePatterns, engine.FamilyDepth)]
	if app.settings.OllamaPort != 12345 || thoroughRow != engine.PresetThorough {
		t.Error("ResetRun changed the settings, but a run reset must leave the configuration alone")
	}
}

func TestResetRunIsSkippedWhileRunning(t *testing.T) {
	app := filledSession()
	app.running = true

	app.ResetRun()

	if app.registry == nil || app.results == nil {
		t.Error("ResetRun wiped state while a run was in progress; it must not pull the state out from under the running goroutine")
	}
}

func TestResetSessionReturnsToDefaults(t *testing.T) {
	app := filledSession()

	if err := app.ResetSession(); err != nil {
		t.Fatalf("ResetSession refused a clean session: %v", err)
	}

	if len(app.docs) != 0 {
		t.Errorf("ResetSession kept %d documents, but a clean sheet has none", len(app.docs))
	}
	if app.registry != nil || app.results != nil || app.lastReq != nil {
		t.Error("ResetSession left run state behind")
	}
	if len(app.removed) != 0 {
		t.Error("ResetSession kept the removed-value list")
	}
	// The settings must match a freshly launched app exactly.
	want := defaultSettings()
	if app.settings.OllamaPort != want.OllamaPort {
		t.Errorf("ResetSession left the port at %d, want the default %d", app.settings.OllamaPort, want.OllamaPort)
	}
	row := engine.PresetKey(engine.ScopePatterns, engine.FamilyDepth)
	if app.settings.Presets[row] != want.Presets[row] {
		t.Errorf("ResetSession left the %s row on %q, want the default %q",
			row, app.settings.Presets[row], want.Presets[row])
	}
	if app.settings.Country != want.Country {
		t.Errorf("ResetSession left the country at %q, want the default %q", app.settings.Country, want.Country)
	}
	if app.llm == nil {
		t.Error("ResetSession left no Ollama client; it must rebuild one on the default port")
	}
}

func TestResetSessionRefusesWhileBusy(t *testing.T) {
	app := filledSession()
	app.running = true
	if err := app.ResetSession(); err == nil {
		t.Error("ResetSession must refuse while a run is in progress rather than wiping mid-run")
	}
	app.running = false
	app.cancelDetection = func() {}
	if err := app.ResetSession(); err == nil {
		t.Error("ResetSession must refuse while a detection is in progress")
	}
	// And the state survived both refusals.
	if len(app.docs) != 1 {
		t.Error("a refused ResetSession must leave the session untouched")
	}
}
