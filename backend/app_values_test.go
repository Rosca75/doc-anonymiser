// app_values_test.go — the value-management surface the Anonymise step's
// Replaced values table drives: removing a value, restoring it and renaming its
// placeholder.
//
// A removal must not free the placeholder NUMBER it used, and neither a removal
// nor a rename may survive only in memory: both facts are load-bearing for the
// re-identification key, so both are asserted here.
package backend

import (
	"context"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
)

// runOnce is a full deterministic run over one fixture document, which is what
// gives the session a registry to remove values from.
func runOnce(t *testing.T, app *App, req RunRequest) *engine.Results {
	t.Helper()
	res, err := app.runPipelineBlocking(context.Background(), req)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res == nil {
		t.Fatal("run returned no results")
	}
	return res
}

// valuesApp is a session with one document naming one person twice, so a
// removal has something to remove and a re-run has somewhere to bring it back.
func valuesApp(t *testing.T) (*App, RunRequest) {
	t.Helper()
	app := NewApp()
	app.docs = []engine.Document{{
		Name:     "note.txt",
		Format:   engine.FormatTXT,
		Markdown: "Marie Duval chaired the meeting. Marie Duval signed it.",
	}}
	req := RunRequest{
		Entities: []engine.Entity{{Category: engine.CatPersonNames, Canonical: "Marie Duval"}},
	}
	return app, req
}

func TestRemoveValuePrunesTheKeyAndKeepsTheNumber(t *testing.T) {
	app, req := valuesApp(t)
	runOnce(t, app, req)

	rows := app.ValuePlaceholders()
	if len(rows) != 1 || rows[0].Original != "Marie Duval" {
		t.Fatalf("the run should have replaced one value, got %+v", rows)
	}
	placeholder := rows[0].Placeholder

	info, err := app.RemoveValue(placeholder)
	if err != nil {
		t.Fatalf("RemoveValue: %v", err)
	}
	if info.Original != "Marie Duval" || info.Placeholder != placeholder {
		t.Errorf("RemoveValue reported %+v, want the row that was removed", info)
	}

	// The key stops describing a replacement that no longer happens...
	if got := app.ValuePlaceholders(); len(got) != 0 {
		t.Errorf("the removed value is still in the key: %+v", got)
	}
	// ...and the removed list is what the UI reads instead.
	removed := app.ListRemovedValues()
	if len(removed) != 1 || removed[0].Placeholder != placeholder {
		t.Fatalf("ListRemovedValues = %+v, want the one removal", removed)
	}

	// The NUMBER is not freed. The user may already hold an export in which this
	// placeholder means this person, and handing it to somebody else would make
	// two artefacts of one session disagree with nothing able to detect it.
	app.docs[0].Markdown = "Alpine Trust and Marie Duval."
	next := RunRequest{Entities: []engine.Entity{
		{Category: engine.CatPersonNames, Canonical: "Alpine Trust"},
	}}
	runOnce(t, app, next)
	for _, row := range app.ValuePlaceholders() {
		if row.Placeholder == placeholder {
			t.Errorf("%q was handed to %q after being retired", placeholder, row.Original)
		}
	}
}

func TestARemovedValueSurvivesAFullReRunUnreplaced(t *testing.T) {
	// Bug 2 in the plan: pass 4 re-applies every registry entry to every
	// document on every re-run, so before this a removed value came back
	// forever. The exclusion is what stops it, and it must hold even though the
	// entity is still in the request the frontend sends.
	app, req := valuesApp(t)
	runOnce(t, app, req)
	placeholder := app.ValuePlaceholders()[0].Placeholder

	if _, err := app.RemoveValue(placeholder); err != nil {
		t.Fatalf("RemoveValue: %v", err)
	}

	res := runOnce(t, app, req)
	out := res.Documents[0].Anonymised
	if !strings.Contains(out, "Marie Duval") {
		t.Errorf("a removed value must be left alone, got %q", out)
	}
	if strings.Contains(out, "[PERSON_") {
		t.Errorf("a removed value must not be replaced under any placeholder, got %q", out)
	}
}

func TestRestoreBringsTheValueBackWithANewNumber(t *testing.T) {
	app, req := valuesApp(t)
	runOnce(t, app, req)
	first := app.ValuePlaceholders()[0].Placeholder

	if _, err := app.RemoveValue(first); err != nil {
		t.Fatalf("RemoveValue: %v", err)
	}
	if err := app.RestoreValue(first); err != nil {
		t.Fatalf("RestoreValue: %v", err)
	}
	if got := app.ListRemovedValues(); len(got) != 0 {
		t.Errorf("the restored value is still listed as removed: %+v", got)
	}

	res := runOnce(t, app, req)
	if strings.Contains(res.Documents[0].Anonymised, "Marie Duval") {
		t.Errorf("a restored value must be replaced again, got %q", res.Documents[0].Anonymised)
	}
	rows := app.ValuePlaceholders()
	if len(rows) != 1 {
		t.Fatalf("want one row after the restore, got %+v", rows)
	}
	// A NEW number, deliberately: the old one was retired, and a restore is not
	// evidence that the export carrying it never left the machine.
	if rows[0].Placeholder == first {
		t.Errorf("the restored value reused the retired placeholder %q", first)
	}
}

func TestRemoveAndRestoreRefuseAnUnknownPlaceholder(t *testing.T) {
	app, req := valuesApp(t)

	// Before any run there is no registry at all, and the message has to say so
	// rather than reporting "not found", which reads as "you picked the wrong
	// row" for a user who has picked nothing yet.
	if _, err := app.RemoveValue("[PERSON_1]"); err == nil {
		t.Fatal("removing before the first run must fail")
	} else if !strings.Contains(err.Error(), "run the anonymisation") {
		t.Errorf("the message must say a run has to happen first, got %q", err)
	}

	runOnce(t, app, req)
	if _, err := app.RemoveValue("[PERSON_99]"); err == nil {
		t.Error("removing a placeholder this session never assigned must fail")
	}
	if err := app.RestoreValue("[PERSON_99]"); err == nil {
		t.Error("restoring something that was never removed must fail")
	}
}

func TestSetValuePlaceholderRenamesAndTakesEffectOnTheNextRun(t *testing.T) {
	// Every call to this used to be a fatal "unlock of unlocked mutex", i.e. an
	// application crash with no recovery, on a button the UI offers.
	app, req := valuesApp(t)
	runOnce(t, app, req)
	current := app.ValuePlaceholders()[0].Placeholder

	if err := app.SetValuePlaceholder(current, "[CHAIR_1]"); err != nil {
		t.Fatalf("SetValuePlaceholder: %v", err)
	}
	rows := app.ValuePlaceholders()
	if len(rows) != 1 || rows[0].Placeholder != "[CHAIR_1]" {
		t.Fatalf("the rename did not land: %+v", rows)
	}

	// It takes effect on the NEXT run, not retroactively: the text on screen was
	// produced with the old placeholder, and rewriting it here would leave the
	// report describing text that no longer exists.
	res := runOnce(t, app, req)
	if !strings.Contains(res.Documents[0].Anonymised, "[CHAIR_1]") {
		t.Errorf("the re-run should use the new placeholder, got %q", res.Documents[0].Anonymised)
	}

	// A collision is refused, because two originals behind one placeholder makes
	// the key ambiguous.
	app.docs[0].Markdown += " Jean Weber attended."
	req.Entities = append(req.Entities,
		engine.Entity{Category: engine.CatPersonNames, Canonical: "Jean Weber"})
	runOnce(t, app, req)
	var other string
	for _, row := range app.ValuePlaceholders() {
		if row.Original == "Jean Weber" {
			other = row.Placeholder
		}
	}
	if other == "" {
		t.Fatal("the second value was not assigned a placeholder")
	}
	if err := app.SetValuePlaceholder(other, "[CHAIR_1]"); err == nil {
		t.Error("renaming onto a placeholder another value owns must be refused")
	}
}

// --- Session persistence -------------------------------------------------

func TestRemovalsAndSpentNumbersSurviveTheSessionFile(t *testing.T) {
	app, req := valuesApp(t)
	runOnce(t, app, req)
	placeholder := app.ValuePlaceholders()[0].Placeholder
	if _, err := app.RemoveValue(placeholder); err != nil {
		t.Fatalf("RemoveValue: %v", err)
	}
	// Save exactly what SaveSessionToFile writes, without the dialog.
	app.mu.Lock()
	session := engine.Session{
		Entities:             req.Entities,
		Settings:             engine.SessionSettings{Level: "medium", OllamaPort: 11434, Country: engine.CountryLU},
		Registry:             app.registry.Export(),
		PlaceholderOverrides: app.registry.Overrides(),
		RemovedValues:        app.removed,
		RetiredPlaceholders:  app.registry.Retired(),
	}
	app.mu.Unlock()

	raw, err := engine.SaveSession(session)
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loaded, err := engine.LoadSession(raw)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}

	fresh := NewApp()
	fresh.docs = app.docs
	if _, err := fresh.applyRestoredSession(loaded); err != nil {
		t.Fatalf("applyRestoredSession: %v", err)
	}

	// The removal survived, or every removed value silently comes back on the
	// first run after a reload.
	if got := fresh.ListRemovedValues(); len(got) != 1 || got[0].Placeholder != placeholder {
		t.Errorf("the removal did not survive the file: %+v", got)
	}
	res := runOnce(t, fresh, req)
	if !strings.Contains(res.Documents[0].Anonymised, "Marie Duval") {
		t.Errorf("a removed value came back after a reload: %q", res.Documents[0].Anonymised)
	}

	// The spent number survived too. Without it a reload frees exactly the
	// number the removal refused to free, one save-and-reload after the refusal.
	for _, row := range fresh.ValuePlaceholders() {
		if row.Placeholder == placeholder {
			t.Errorf("%q was handed out again after a reload (to %q)", row.Placeholder, row.Original)
		}
	}
}

func TestARejectedSessionLoadLeavesTheAppUntouched(t *testing.T) {
	// Bug 21: the loader installed the registry and the settings and validated
	// afterwards, so a file this application refuses still left the App holding
	// that file's re-identification key, behind an error the UI reads as
	// "nothing was loaded".
	app, req := valuesApp(t)
	runOnce(t, app, req)
	before := app.ValuePlaceholders()
	settingsBefore := app.GetSettings()

	bad := engine.Session{
		Version: engine.SessionVersion,
		// A level no build has ever had: ApplySettings refuses it.
		Settings: engine.SessionSettings{Level: "paranoid", OllamaPort: 11434, Country: engine.CountryLU},
		Registry: []engine.MappingEntry{
			{Original: "Someone Else", Placeholder: "[PERSON_1]", Category: engine.CatPersonNames, Count: 1},
		},
	}
	if _, err := app.applyRestoredSession(bad); err == nil {
		t.Fatal("a session with an unknown level must be refused")
	}

	if got := app.ValuePlaceholders(); len(got) != len(before) || got[0].Original != before[0].Original {
		t.Errorf("the rejected file's registry was installed anyway: %+v", got)
	}
	if got := app.GetSettings(); got.Level != settingsBefore.Level || got.Country != settingsBefore.Country {
		t.Errorf("the rejected file's settings were installed anyway: %+v", got)
	}
}

func TestACorruptKeyIsRefusedRatherThanCrashing(t *testing.T) {
	// Two entries claiming one value means the key cannot be read at all. This
	// used to panic, which in a bound method takes the application down; the
	// policy everywhere else in the loader is refuse and say why.
	corrupt := engine.Session{
		Version:  engine.SessionVersion,
		Settings: engine.SessionSettings{Level: "medium", OllamaPort: 11434, Country: engine.CountryLU},
		Registry: []engine.MappingEntry{
			{Original: "Alpine Trust", Placeholder: "[ENTITY_1]", Category: engine.CatEntityNames, Count: 1},
			{Original: "alpine trust", Placeholder: "[PERSON_1]", Category: engine.CatPersonNames, Count: 1},
		},
	}
	_, err := NewApp().applyRestoredSession(corrupt)
	if err == nil {
		t.Fatal("a duplicated original must be refused")
	}
	if !strings.Contains(err.Error(), "Alpine Trust") {
		t.Errorf("the message must name the value that appears twice, got %q", err)
	}
}
