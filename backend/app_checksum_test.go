// app_checksum_test.go — the checksum switch, through the bound layer
//
// TIER: unit (docs/TESTING.md). Nothing here touches the filesystem, spawns a
// binary or reaches a network: it drives the two in-process paths that read
// Settings.RequireChecksum and asserts they read it the same way.
//
// The engine's own behaviour is locked in engine/checksum_test.go. What is left
// for this file is the WIRING, which the engine structurally cannot see: the
// setting has one home and two readers, the Identify preview and the run, and
// they have to agree. A preview promising a replacement the run does not make is
// the one thing engine/patternpreview.go may not do, and the App is where that
// promise is either kept or broken.
package backend

import (
	"context"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
)

// Both IBANs are shape-valid; only the second's mod-97 remainder is right.
const (
	appChecksumFailedIBAN = "LU88 0055 6600 4321 6501"
	appChecksumValidIBAN  = "LU28 0019 4006 4475 0000"
)

// checksumApp is one document holding one of each: a checksum-failed IBAN, a
// good one, and an email that is not what the switch is about.
func checksumApp(requireChecksum bool) *App {
	app := NewApp()
	app.settings.Country = engine.CountryLU
	app.settings.UseBuiltInPatterns = true
	app.settings.Categories = engine.CategorySelection{
		engine.CatIBAN: true, engine.CatEmail: true,
	}
	app.settings.RequireChecksum = requireChecksum
	app.docs = []engine.Document{{
		Name: "accounts.md", Format: engine.FormatMD,
		Markdown: "Bad " + appChecksumFailedIBAN + ", good " + appChecksumValidIBAN +
			", mail marie.duval@example.com\n",
	}}
	return app
}

// previewTexts is what the Identify step would SHOW, as a set of matched texts.
func previewedTexts(t *testing.T, app *App) map[string]bool {
	t.Helper()
	res := &DetectionResult{}
	app.previewBuiltInPatterns(app.docs, app.settings, app.allowlistFor(nil), res)
	out := map[string]bool{}
	for _, m := range res.PatternMatches {
		out[m.Text] = true
	}
	return out
}

// TestChecksumSwitchReachesThePreviewAndTheRunAlike: one setting, two readers,
// one answer. The assertion is deliberately an equality between the two sets
// rather than two separate expectations, because the defect this guards is the
// two DISAGREEING, which two independent expectations would not catch.
func TestChecksumSwitchReachesThePreviewAndTheRunAlike(t *testing.T) {
	for _, requireChecksum := range []bool{false, true} {
		app := checksumApp(requireChecksum)
		previewed := previewedTexts(t, app)

		res, err := app.runPipelineBlocking(context.Background(), RunRequest{
			Categories: app.settings.Categories,
		})
		if err != nil {
			t.Fatalf("requireChecksum=%v: runPipelineBlocking: %v", requireChecksum, err)
		}
		replaced := map[string]bool{}
		for _, row := range res.Report.Values {
			replaced[row.Original] = true
		}

		for text := range previewed {
			if !replaced[text] {
				t.Errorf("requireChecksum=%v: the preview promised %q and the run did not make it",
					requireChecksum, text)
			}
		}
		for text := range replaced {
			if !previewed[text] {
				t.Errorf("requireChecksum=%v: the run replaced %q and the preview did not show it",
					requireChecksum, text)
			}
		}

		// And the switch does what its label says, at both ends. The switch ON
		// means the failed match is NOT shown and NOT replaced.
		wantShown := !requireChecksum
		if previewed[appChecksumFailedIBAN] != wantShown {
			t.Errorf("requireChecksum=%v: the preview shows the checksum-failed IBAN = %v, want %v",
				requireChecksum, previewed[appChecksumFailedIBAN], wantShown)
		}
		if !previewed[appChecksumValidIBAN] || !previewed["marie.duval@example.com"] {
			t.Errorf("requireChecksum=%v: the switch is only about the failed check, so the good IBAN "+
				"and the address must both stay previewed, got %v", requireChecksum, previewed)
		}
		got := res.Documents[0].Anonymised
		if strings.Contains(got, appChecksumFailedIBAN) != requireChecksum {
			t.Errorf("requireChecksum=%v: the run left the checksum-failed IBAN in clear = %v, want %v, in:\n%s",
				requireChecksum, strings.Contains(got, appChecksumFailedIBAN), requireChecksum, got)
		}
	}
}

// TestChecksumSwitchDefaultsOff: off is today's behaviour, and the default has to
// be it. A user who never opens the rail must get the same replacements they have
// always got, so a checksum-failed bank identifier is anonymised rather than left
// in clear.
func TestChecksumSwitchDefaultsOff(t *testing.T) {
	if NewApp().GetSettings().RequireChecksum {
		t.Error("Settings.RequireChecksum must default to false: off is what the application " +
			"has always done, and a default that vetoes would silently start leaving " +
			"mistyped bank identifiers in the exported document")
	}
}
