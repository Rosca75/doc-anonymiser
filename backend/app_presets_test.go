// backend/app_presets_test.go — the bound layer's half of the preset model.
//
// The engine owns the table and its scoping rules (engine/presets_test.go); what
// is asserted here is what the App does with a preset map that arrives over the
// bridge: it stores a valid one, refuses an invented one with a message naming
// what is valid, and starts a fresh session on the documented default.
package backend

import (
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
)

// depthPresets is the stored preset map for one depth applied to both rows: the
// shape Settings.Presets carries over the bridge and into the session file.
func depthPresets(id string) map[string]string {
	return map[string]string{
		engine.PresetKey(engine.ScopePatterns, engine.FamilyDepth): id,
		engine.PresetKey(engine.ScopeNames, engine.FamilyDepth):    id,
	}
}

// TestDefaultSettingsStartOnTheStandardDepth: a fresh session's documented
// default, on BOTH rows. It is what the rail's chips render as active before the
// user touches anything, so a drift here is the first thing every session sees.
func TestDefaultSettingsStartOnTheStandardDepth(t *testing.T) {
	got := defaultSettings().Presets
	for _, scope := range engine.AllPresetScopes {
		row := engine.PresetKey(scope, engine.FamilyDepth)
		if got[row] != engine.PresetStandard {
			t.Errorf("a fresh session has the %s row on %q, want %q",
				row, got[row], engine.PresetStandard)
		}
	}
	if len(got) != len(engine.AllPresetScopes) {
		t.Errorf("the defaults carry %d preset rows, want one per scope: %v", len(got), got)
	}
}

// TestApplySettingsRefusesAnUnknownPreset: an unknown scope, family or preset ID
// is refused rather than stored, and the message names what IS valid.
//
// Stored, such a key would be one no reader resolves for the rest of the session:
// a control that appears to do something and does not, which is the same failure
// an invented signal source would be. The person who sees the message has a
// hand-edited session file or a stale frontend, so it has to say what to write
// instead.
func TestApplySettingsRefusesAnUnknownPreset(t *testing.T) {
	for _, tc := range []struct {
		name    string
		presets map[string]string
		names   string
	}{
		{"unknown scope", map[string]string{"pattern.depth": engine.PresetSoft}, "patterns.depth"},
		{"unknown family", map[string]string{"patterns.regulatory": engine.PresetSoft}, "patterns.depth"},
		{"unknown preset id", map[string]string{"patterns.depth": "paranoid"}, engine.PresetSoft},
	} {
		t.Run("errors/"+strings.ReplaceAll(tc.name, " ", "_"), func(t *testing.T) {
			app := NewApp()
			_, err := app.ApplySettings(Settings{
				Presets: tc.presets, OllamaPort: 11434, Country: engine.CountryLU,
				LLMDetailLevel: engine.DetailThorough,
			})
			if err == nil {
				t.Fatalf("%v was accepted", tc.presets)
			}
			if !strings.Contains(err.Error(), tc.names) {
				t.Errorf("the refusal does not name what is valid (%q): %v", tc.names, err)
			}
			// And nothing was stored, so the session keeps the defaults.
			row := engine.PresetKey(engine.ScopePatterns, engine.FamilyDepth)
			if got := app.GetSettings().Presets[row]; got != engine.PresetStandard {
				t.Errorf("the refused map was installed anyway: the %s row reads %q", row, got)
			}
		})
	}
}

// TestApplySettingsAcceptsACustomRow: an ABSENT key is how "Custom" is
// representable, so an empty map has to be valid. A map that had to name every
// row would force the rail to invent a preset for a selection that matches none.
func TestApplySettingsAcceptsACustomRow(t *testing.T) {
	app := NewApp()
	if _, err := app.ApplySettings(Settings{
		Presets: map[string]string{
			engine.PresetKey(engine.ScopeNames, engine.FamilyDepth): engine.PresetThorough,
		},
		OllamaPort: 11434, Country: engine.CountryLU, LLMDetailLevel: engine.DetailThorough,
	}); err != nil {
		t.Fatalf("a map naming one row and leaving the other Custom must be valid: %v", err)
	}
	got := app.GetSettings().Presets
	if _, present := got[engine.PresetKey(engine.ScopePatterns, engine.FamilyDepth)]; present {
		t.Errorf("the patterns row was filled in on the user's behalf: %v", got)
	}
	if got[engine.PresetKey(engine.ScopeNames, engine.FamilyDepth)] != engine.PresetThorough {
		t.Errorf("the names row is %v, want thorough", got)
	}
}

// TestTheRunReportNamesThePresetsTheSelectionMatched: the report's presets are
// DERIVED from the selection the run obeyed, not copied from the settings, so a
// stale settings map can never make the report claim a preset the run did not
// use.
func TestTheRunReportNamesThePresetsTheSelectionMatched(t *testing.T) {
	app := NewApp()
	app.docs = []engine.Document{{
		Name: "a.txt", Format: engine.FormatTXT,
		Markdown: "Alpine Trust wrote to marie.duval@example.com.",
	}}
	// The settings claim Thorough on both rows while the REQUEST carries a Soft
	// selection. The run obeys the selection, so the report has to say Soft.
	if _, err := app.ApplySettings(Settings{
		Presets: depthPresets(engine.PresetThorough), OllamaPort: 11434,
		Country: engine.CountryLU, LLMDetailLevel: engine.DetailThorough,
	}); err != nil {
		t.Fatalf("ApplySettings: %v", err)
	}
	res, err := app.runPipelineBlocking(t.Context(), RunRequest{
		Categories: engine.DepthSelection(engine.PresetSoft, engine.CountryLU),
	})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if got := res.Report.Presets[engine.PresetKey(engine.ScopeNames, engine.FamilyDepth)]; got != engine.PresetSoft {
		t.Errorf("the report's names row is %q, want soft: the report describes the selection "+
			"the run obeyed, not the last chip the settings remember", got)
	}
}
