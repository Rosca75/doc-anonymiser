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

// TestDetectionRouteDefaults: neither offline route needs anything installed, so
// both are on; Local LLM discovery hands the document to a model, so the
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
	if s.UseLocalLLM {
		t.Error("the local model route must be off by default")
	}
}

// TestSessionSettingsRoundTrip is the guard over the settings a restore is
// allowed to lose, which is none of them. LoadSessionFromFile rebuilds Settings
// from a literal, so a field left out of that literal is a setting the user saved
// and got back at its default. Every field checked here decides what gets
// replaced, and the user reads "load session" as "restore what I saved".
func TestSessionSettingsRoundTrip(t *testing.T) {
	saved := engine.Session{
		Settings: engine.SessionSettings{
			Presets: depthPresets(engine.PresetThorough), OllamaPort: 11500, Model: "m:1b", ContextSize: 4096,
			RequireChecksum: true,
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
	if !loaded.Settings.RequireChecksum {
		t.Error("the checksum switch did not survive the file")
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
	if !got.RequireChecksum {
		t.Error("the restored checksum switch is off, want the saved on")
	}
	if got.HeuristicDiscovery.MinLength != 7 || got.HeuristicDiscovery.ExcludeCommonWords {
		t.Errorf("the restored heuristic tuning is %+v, want the saved one", got.HeuristicDiscovery)
	}
	// A file that says nothing about a method must restore it ON, never off: the
	// safe reading of silence is the shipped default, and reading it as "off"
	// would silently stop detecting after a reload.
	if !got.UseBuiltInPatterns || !got.UseHeuristicDiscovery {
		t.Error("a file that says nothing about the offline routes must restore them ON")
	}
	if !engine.SignalSourceEnabled(got.SignalSuggestionSources, engine.SignalSourceEmail) {
		t.Error("a file that says nothing about a signal source must restore its default")
	}
}

// TestApplySettingsCarriesTheStrictFormatChoice: the discovery reply format is a
// setting, and all three states of the pointer have to reach the client
// correctly.
//
// It is a POINTER so a session file can tell "absent" from "switched off", and
// absent has to read as OFF here, because off is the shipped default. Reading nil
// as on would double every scan's wall clock for a user who never touched the
// control.
func TestApplySettingsCarriesTheStrictFormatChoice(t *testing.T) {
	on, off := true, false
	for _, tc := range []struct {
		name string
		set  *bool
		want bool
	}{
		{"absent reads as off, the shipped default", nil, false},
		{"switched off is obeyed", &off, false},
		{"switched on reaches the client", &on, true},
	} {
		t.Run("config/"+tc.name, func(t *testing.T) {
			app := NewApp()
			s := app.GetSettings()
			s.LLMStrictFormat = tc.set

			if _, err := app.ApplySettings(s); err != nil {
				t.Fatalf("ApplySettings: %v", err)
			}
			if got := app.llm.StrictFormat; got != tc.want {
				t.Errorf("the client's StrictFormat is %v, want %v: the setting decides what shape "+
					"the discovery call asks its reply to be", got, tc.want)
			}
			// The stored setting round-trips as it arrived, so the rail redraws the
			// checkbox the user left rather than a value Go invented.
			if got := app.GetSettings().LLMStrictFormat; (got == nil) != (tc.set == nil) {
				t.Errorf("the stored setting is %v, want the pointer it was given (%v)", got, tc.set)
			} else if got != nil && *got != *tc.set {
				t.Errorf("the stored setting is %v, want %v", *got, *tc.set)
			}
		})
	}
}

// TestStrictFormatSurvivesTheSessionFile: the format choice persists, and a file
// that says nothing about it restores as OFF without bumping the version.
//
// A bump is for a field whose old meaning cannot be recovered. Here silence and
// the default agree, so no migration is needed and none exists. The property
// this test owns is that pair of readings, not the version NUMBER: the number
// moves whenever some other field's old meaning becomes unrecoverable, and
// pinning it here would make an unrelated bump look like this field's fault.
func TestStrictFormatSurvivesTheSessionFile(t *testing.T) {
	on := true
	data, err := engine.SaveSession(engine.Session{
		Settings: engine.SessionSettings{Presets: depthPresets(engine.PresetStandard), OllamaPort: 11434, LLMStrictFormat: &on},
	})
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loaded, err := engine.LoadSession(data)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded.Settings.LLMStrictFormat == nil || !*loaded.Settings.LLMStrictFormat {
		t.Errorf("the format choice did not survive the file: %v", loaded.Settings.LLMStrictFormat)
	}

	app := NewApp()
	if _, err := app.applyRestoredSession(loaded); err != nil {
		t.Fatalf("applyRestoredSession: %v", err)
	}
	if !app.llm.StrictFormat {
		t.Error("a restored session that asked for the schema must reach the client with it")
	}

	// And a file with nothing to say about it restores as off, never as on.
	silent, err := engine.SaveSession(engine.Session{Settings: engine.SessionSettings{Presets: depthPresets(engine.PresetStandard), OllamaPort: 11434}})
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loadedSilent, err := engine.LoadSession(silent)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	fresh := NewApp()
	if _, err := fresh.applyRestoredSession(loadedSilent); err != nil {
		t.Fatalf("applyRestoredSession: %v", err)
	}
	if fresh.llm.StrictFormat {
		t.Error("a file that says nothing about the reply format must restore the fast default, not the slow one")
	}
}

// TestApplySettingsValidatesTheDetailLevel: the speed-versus-recall dial is a
// named level, and the boundary is where a name nothing sizes gets refused.
//
// Stored, a misspelt level would read as thorough for the rest of the session
// while the rail showed whatever the user picked, which is a control that
// appears to do something and does not. An EMPTY level is a different fact: it
// is what a session file written before the setting existed carries, so it is
// accepted and filled out to the default it already means.
func TestApplySettingsValidatesTheDetailLevel(t *testing.T) {
	for _, tc := range []struct {
		name  string
		level string
		want  string
	}{
		{"thorough is accepted", engine.DetailThorough, engine.DetailThorough},
		{"faster is accepted", engine.DetailFaster, engine.DetailFaster},
		{"absent is filled out to thorough", "", engine.DetailThorough},
	} {
		t.Run("config/"+tc.name, func(t *testing.T) {
			app := NewApp()
			s := app.GetSettings()
			s.LLMDetailLevel = tc.level

			if _, err := app.ApplySettings(s); err != nil {
				t.Fatalf("ApplySettings(%q): %v", tc.level, err)
			}
			// The stored value is what the rail's dropdown marks as selected, so
			// it has to be a real level rather than whatever arrived.
			if got := app.GetSettings().LLMDetailLevel; got != tc.want {
				t.Errorf("the stored detail level is %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("errors/an_unknown_level_is_refused_and_names_the_valid_ones", func(t *testing.T) {
		app := NewApp()
		s := app.GetSettings()
		s.LLMDetailLevel = "exhaustive"

		_, err := app.ApplySettings(s)
		if err == nil {
			t.Fatal("an unknown detail level must be refused, not stored as one nothing sizes")
		}
		for _, want := range []string{"exhaustive", engine.DetailThorough, engine.DetailFaster} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal must name %q so the user can act on it, got: %v", want, err)
			}
		}
		if got := app.GetSettings().LLMDetailLevel; got == "exhaustive" {
			t.Error("a refused level must not be stored")
		}
	})
}

// TestDetailLevelSurvivesTheSessionFile: the level persists, and a file that
// says nothing about it restores as THOROUGH without bumping the version.
//
// Thorough rather than the live setting, because a file with no level was
// written under thorough: carrying the current choice over would let loading an
// old session restore a scan the file never recorded.
func TestDetailLevelSurvivesTheSessionFile(t *testing.T) {
	data, err := engine.SaveSession(engine.Session{
		Settings: engine.SessionSettings{
			Presets: depthPresets(engine.PresetStandard), OllamaPort: 11434, LLMDetailLevel: engine.DetailFaster,
		},
	})
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loaded, err := engine.LoadSession(data)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded.Settings.LLMDetailLevel != engine.DetailFaster {
		t.Errorf("the detail level did not survive the file: %q", loaded.Settings.LLMDetailLevel)
	}

	app := NewApp()
	if _, err := app.applyRestoredSession(loaded); err != nil {
		t.Fatalf("applyRestoredSession: %v", err)
	}
	if got := app.GetSettings().LLMDetailLevel; got != engine.DetailFaster {
		t.Errorf("the restored detail level is %q, want %q", got, engine.DetailFaster)
	}

	// A file written before the setting existed says nothing about it, and must
	// restore as thorough even when the live session is on faster.
	silent, err := engine.SaveSession(engine.Session{
		Settings: engine.SessionSettings{Presets: depthPresets(engine.PresetStandard), OllamaPort: 11434},
	})
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loadedSilent, err := engine.LoadSession(silent)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if _, err := app.applyRestoredSession(loadedSilent); err != nil {
		t.Fatalf("applyRestoredSession: %v", err)
	}
	if got := app.GetSettings().LLMDetailLevel; got != engine.DetailThorough {
		t.Errorf("a file that says nothing about the detail level restored %q, want %q: silence means the level the file was written under",
			got, engine.DetailThorough)
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
	s.SignalSuggestionSources = engine.SignalSourceSelection{
		"telepathy": {engine.DerivationEmailPerson: true},
	}

	if _, err := app.ApplySettings(s); err == nil {
		t.Fatal("an unknown signal source must be refused")
	} else if !strings.Contains(err.Error(), "telepathy") {
		t.Errorf("the refusal must name the source it rejected, got: %v", err)
	}
	if _, ok := app.GetSettings().SignalSuggestionSources["telepathy"]; ok {
		t.Error("a refused source must not be stored")
	}
}

// TestApplySettingsRefusesAnUnknownSignalDerivation is the same rule one level
// down. A derivation is only meaningful UNDER its source, so a reading this build
// does not produce, or a real reading filed under the wrong source, is refused for
// the same reason: stored, it is a switch nothing reads.
func TestApplySettingsRefusesAnUnknownSignalDerivation(t *testing.T) {
	app := NewApp()
	s := app.GetSettings()
	s.SignalSuggestionSources = engine.SignalSourceSelection{
		engine.SignalSourceEmail: {"email.telepathy": true},
	}

	if _, err := app.ApplySettings(s); err == nil {
		t.Fatal("an unknown signal derivation must be refused")
	} else if !strings.Contains(err.Error(), "email.telepathy") {
		t.Errorf("the refusal must name the derivation it rejected, got: %v", err)
	}
	if _, ok := app.GetSettings().SignalSuggestionSources[engine.SignalSourceEmail]["email.telepathy"]; ok {
		t.Error("a refused derivation must not be stored")
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
	s.SignalSuggestionSources = engine.SignalSourceSelection{engine.SignalSourceEmail: {
		engine.DerivationEmailPerson:       false,
		engine.DerivationEmailOrganisation: false,
	}}

	if _, err := app.ApplySettings(s); err != nil {
		t.Fatalf("ApplySettings: %v", err)
	}
	if engine.SignalSourceEnabled(app.GetSettings().SignalSuggestionSources, engine.SignalSourceEmail) {
		t.Error("an explicit false must be obeyed, not filled back in from the defaults")
	}
}

// TestApplySettingsKeepsOneReadingWhenTheOtherIsCleared: the readings are
// independent through the bound layer too, not only inside the engine. A
// normalisation that filled both from the defaults, or cleared both together,
// would make the per-reading switches decoration.
func TestApplySettingsKeepsOneReadingWhenTheOtherIsCleared(t *testing.T) {
	app := NewApp()
	s := app.GetSettings()
	s.SignalSuggestionSources = engine.SignalSourceSelection{engine.SignalSourceEmail: {
		engine.DerivationEmailPerson: false,
	}}

	if _, err := app.ApplySettings(s); err != nil {
		t.Fatalf("ApplySettings: %v", err)
	}
	got := app.GetSettings().SignalSuggestionSources
	if engine.SignalDerivationEnabled(got, engine.SignalSourceEmail, engine.DerivationEmailPerson) {
		t.Errorf("the cleared reading must stay cleared, got %+v", got)
	}
	if !engine.SignalDerivationEnabled(got, engine.SignalSourceEmail, engine.DerivationEmailOrganisation) {
		t.Errorf("the omitted reading must fill from the DEFAULTS, not from off, got %+v", got)
	}
	if !engine.SignalSourceEnabled(got, engine.SignalSourceEmail) {
		t.Errorf("the signal still derives something, so its master must read on, got %+v", got)
	}
}
