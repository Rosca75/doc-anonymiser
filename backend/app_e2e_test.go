// app_e2e_test.go — the UNIT-tier App-layer tests: settings, session restore,
// route defaults, and the discovery-prompt source guard.
//
// TIER: unit (docs/TESTING.md). These are hermetic, in-memory tests of the
// bound App's configuration surface. None imports a fixture or reaches a model:
// they construct a fresh App, exercise one method, and assert its result, or
// (for the prompt guard) read a source file and scan it. The fixture-driven
// end-to-end flow through the bound app is in app_e2e_integration_test.go.
package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
)

// TestDiscoveryPromptsNameNoRetiredCategory: the model is told the category
// keys by name, so a prompt left on an old key silently produces proposals the
// engine then drops as unknown categories.
func TestDiscoveryPromptsNameNoRetiredCategory(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("ollama", "client.go"))
	if err != nil {
		t.Fatalf("could not read the Ollama client: %v", err)
	}
	text := string(source)
	for _, retired := range []string{
		`"client_names"`, `"internal_names"`, `"organisation_names"`, `"location_names"`,
		"[CLIENT_1]",
	} {
		if strings.Contains(text, retired) {
			t.Errorf("backend/ollama/client.go still mentions %s; the prompts and the engine must agree on the category keys", retired)
		}
	}
	if !strings.Contains(text, `"entity_names"`) {
		t.Error("the prompts must demand the entity_names key")
	}
}

// --- The detection route switches ----------------------------------------

// TestDetectionRouteDefaults: every Smart detection method needs nothing
// installed, so all three are on; Local AI hands the document to a model, so the
// user turns that one on themselves.
func TestDetectionRouteDefaults(t *testing.T) {
	s := NewApp().GetSettings()
	if !s.UseBuiltInPatterns {
		t.Error("built-in pattern matching must be on by default")
	}
	if !s.UseHeuristicDiscovery {
		t.Error("heuristic discovery must be on by default")
	}
	if !engine.SignalSourceEnabled(s.SignalSuggestionSources, engine.SignalSourceEmail) {
		t.Error("email-derived Suggestions must be on by default")
	}
	if s.UseLocalAI {
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
			HeuristicDiscovery: &engine.HeuristicDiscoveryOptions{
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
	if loaded.Settings.HeuristicDiscovery == nil || loaded.Settings.HeuristicDiscovery.MinLength != 7 {
		t.Errorf("the smart tuning did not survive the file: %+v", loaded.Settings.HeuristicDiscovery)
	}

	// And the same fields must survive the App-side restore, which is where
	// they were being dropped.
	app := NewApp()
	if _, err := app.applyRestoredSession(loaded); err != nil {
		t.Fatalf("applyRestoredSession: %v", err)
	}
	got := app.GetSettings()
	if got.MinConfidence != 0.85 {
		t.Errorf("the restored confidence floor is %v, want 0.85", got.MinConfidence)
	}
	if got.HeuristicDiscovery.MinLength != 7 || got.HeuristicDiscovery.ExcludeCommonWords {
		t.Errorf("the restored heuristic tuning is %+v, want the saved one", got.HeuristicDiscovery)
	}
	// A file that says nothing about a method must restore it ON, never off: the
	// safe reading of silence is the shipped default, and reading it as "off"
	// would silently stop detecting after a reload.
	if !got.UseBuiltInPatterns || !got.UseHeuristicDiscovery {
		t.Error("a file that says nothing about Smart detection's methods must restore them ON")
	}
	if !engine.SignalSourceEnabled(got.SignalSuggestionSources, engine.SignalSourceEmail) {
		t.Error("a file that says nothing about a signal source must restore its default")
	}
}

// TestApplySettingsRefusesAnUnknownSignalSource: an identifier Go does not
// implement must not reach the settings at all.
//
// Stored, it would be a key nothing reads for the rest of the session, which
// looks exactly like a control that works and does not. The frontend refuses one
// too; this is the half that holds when the frontend is wrong.
func TestApplySettingsRefusesAnUnknownSignalSource(t *testing.T) {
	app := NewApp()
	s := app.GetSettings()
	s.SignalSuggestionSources = engine.SignalSourceSelection{"telepathy": true}

	if _, err := app.ApplySettings(s); err == nil {
		t.Fatal("an unknown signal source must be refused")
	} else if !strings.Contains(err.Error(), "telepathy") {
		t.Errorf("the refusal must name the source it rejected, got: %v", err)
	}
	if _, ok := app.GetSettings().SignalSuggestionSources["telepathy"]; ok {
		t.Error("a refused source must not be stored")
	}
}

// TestApplySettingsFillsAnOmittedSignalSource: a payload that says nothing about
// a source must land on its DEFAULT, never on Go's zero value.
//
// Reading an absent key as "off" would silently disable a source the user never
// touched, which is the failure mode the whole "absent means the default" rule
// exists for.
func TestApplySettingsFillsAnOmittedSignalSource(t *testing.T) {
	app := NewApp()
	s := app.GetSettings()
	s.SignalSuggestionSources = engine.SignalSourceSelection{} // says nothing

	if _, err := app.ApplySettings(s); err != nil {
		t.Fatalf("ApplySettings: %v", err)
	}
	if !engine.SignalSourceEnabled(app.GetSettings().SignalSuggestionSources, engine.SignalSourceEmail) {
		t.Errorf("an omitted source must fall back to its default, got %+v",
			app.GetSettings().SignalSuggestionSources)
	}
}

// TestApplySettingsObeysAnExplicitlyDisabledSource is the other half: an explicit
// false must survive, or the fill-in above would quietly re-enable what the user
// switched off.
func TestApplySettingsObeysAnExplicitlyDisabledSource(t *testing.T) {
	app := NewApp()
	s := app.GetSettings()
	s.SignalSuggestionSources = engine.SignalSourceSelection{engine.SignalSourceEmail: false}

	if _, err := app.ApplySettings(s); err != nil {
		t.Fatalf("ApplySettings: %v", err)
	}
	if engine.SignalSourceEnabled(app.GetSettings().SignalSuggestionSources, engine.SignalSourceEmail) {
		t.Error("an explicit false must be obeyed, not filled back in from the defaults")
	}
}
