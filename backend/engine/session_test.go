// engine/session_test.go — tests zip contents, CSV round-trip
// export equals the anonymised grid, session save→load equality, and the
// mapping export golden file.
package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine/imaging"
)

// runExportFixture runs a tiny two-document pipeline (one text, one CSV)
// shared by the export tests.
func runExportFixture(t *testing.T) (*Results, *Registry) {
	t.Helper()
	csvDoc, err := Load("clients.csv", []byte("name,email\nMarie Duval,marie.duval@example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	res, err := Run(context.Background(), PipelineInput{
		Documents: []Document{
			{Name: "notes.txt", Format: FormatTXT, Markdown: "Alpine Trust wrote to marie.duval@example.com."},
			csvDoc,
		},
		Values:     []Value{{Category: "entity_names", MainText: "Alpine Trust"}},
		Categories: DepthSelection(PresetStandard, CountryLU),
		Allowlist:  NewEmptyAllowlist(),
		Registry:   reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	return res, reg
}

func TestBuildExportZipContents(t *testing.T) {
	res, _ := runExportFixture(t)
	data, err := BuildExportZip(res)
	if err != nil {
		t.Fatalf("BuildExportZip: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip unreadable: %v", err)
	}

	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = string(content)
	}

	// Default formats: txt for the text doc, csv for the grid doc; names
	// carry the _anon suffix.
	if !strings.Contains(got["notes_anon.txt"], "[ENTITY_1]") {
		t.Errorf("notes_anon.txt missing or not anonymised: %v", got)
	}
	if !strings.Contains(got["clients_anon.csv"], "[EMAIL_1]") {
		t.Errorf("clients_anon.csv missing or not anonymised: %v", got)
	}
	// The empty-results guard must be actionable.
	if _, err := BuildExportZip(&Results{}); err == nil {
		t.Error("empty results must be rejected")
	}
}

func TestCSVExportEqualsAnonymisedGrid(t *testing.T) {
	res, _ := runExportFixture(t)
	var csvDoc *ResultDocument
	for i := range res.Documents {
		if res.Documents[i].Format == FormatCSV {
			csvDoc = &res.Documents[i]
		}
	}
	out, err := ExportBytes(*csvDoc, "csv")
	if err != nil {
		t.Fatalf("ExportBytes: %v", err)
	}
	// Re-parse: every cell must equal the anonymised grid exactly.
	grid, _, err := ParseCSV(out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	for r := range csvDoc.Grid {
		for c := range csvDoc.Grid[r] {
			if grid[r][c] != csvDoc.Grid[r][c] {
				t.Errorf("cell [%d][%d]: exported %q != grid %q", r, c, grid[r][c], csvDoc.Grid[r][c])
			}
		}
	}
	// A format the document does not offer must be refused actionably.
	if _, err := ExportBytes(*csvDoc, "json"); err == nil {
		t.Error("csv document exported as json?!")
	}
}

func TestSessionSaveLoadEquality(t *testing.T) {
	_, reg := runExportFixture(t)
	original := Session{
		Values:     []Value{{Category: "entity_names", MainText: "Alpine Trust", Spellings: []string{"Alpine"}}},
		AllowTerms: []string{"CSSF", "Luxembourg"},
		Patterns:   []CustomPattern{{Expr: "PRJ-[0-9]+"}},
		Settings:   SessionSettings{Presets: depthPresets(PresetThorough), OllamaPort: 12345, Model: "qwen3.5:0.8b"},
		Registry:   reg.Export(),
	}
	raw, err := SaveSession(original)
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loaded, err := LoadSession(raw)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	// Round-trip equality (version is stamped by Save).
	original.Version = SessionVersion
	rawAgain, err := SaveSession(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(rawAgain) {
		t.Errorf("session save→load→save changed bytes:\n%s\nvs\n%s", raw, rawAgain)
	}

	// Registry restore: existing mappings keep their placeholders and new
	// assignments continue the numbering (no collisions).
	restored, err := NewRegistryFromEntries(loaded.Registry)
	if err != nil {
		t.Fatalf("NewRegistryFromEntries: %v", err)
	}
	if p, ok := restored.Lookup(CatEmail, "marie.duval@example.com"); !ok || p != "[EMAIL_1]" {
		t.Errorf("restored registry lost the email mapping: %q %v", p, ok)
	}
	if p := restored.Assign(CatEmail, "new@example.org"); p != "[EMAIL_2]" {
		t.Errorf("numbering must continue after restore, got %q", p)
	}

	// Corrupt and wrong-version files fail actionably.
	if _, err := LoadSession([]byte("not json")); err == nil || !strings.Contains(err.Error(), "session file") {
		t.Errorf("corrupt session error not actionable: %v", err)
	}
	// A wrong-version file is REFUSED, not migrated, and
	// the message has to say which direction the mismatch goes, because the fix
	// differs: an older file needs re-creating, a newer one needs a newer app.
	bad, _ := SaveSession(original)
	for _, tt := range []struct {
		version   int
		wantFix   string
		wantWrote string
	}{
		{version: 1, wantFix: "start a new session", wantWrote: "an older version"},
		// The IMMEDIATELY previous version is refused too, not migrated. A v12
		// file carries the retired confidence floor, a number that cannot say
		// whether the user wanted a checksum-failed match replaced: a reader
		// guessing either way changes what the restored session anonymises.
		{version: SessionVersion - 1, wantFix: "start a new session", wantWrote: "an older version"},
		{version: 99, wantFix: "update the application", wantWrote: "a newer version"},
	} {
		badStr := strings.Replace(string(bad),
			fmt.Sprintf(`"version": %d`, SessionVersion),
			fmt.Sprintf(`"version": %d`, tt.version), 1)
		_, err := LoadSession([]byte(badStr))
		if err == nil {
			t.Fatalf("version %d must be refused", tt.version)
		}
		for _, want := range []string{"version", tt.wantWrote, tt.wantFix, "re-identification key"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("version %d refusal must mention %q, got: %v", tt.version, want, err)
			}
		}
	}
}

// TestSessionRoundTripsACuratedValue: a Value whose spellings the user set by
// hand must come back curated. If the policy were dropped on the way through the
// file, reloading a session would silently re-derive the list and start
// replacing spellings the user had deleted.
func TestSessionRoundTripsACuratedValue(t *testing.T) {
	raw, err := SaveSession(Session{
		Values: []Value{{
			Category:         CatPersonNames,
			MainText:         "Marie Duval",
			Spellings:        []string{"Duval"},
			SpellingPolicy:   SpellingPolicyCurated,
			DiscoveryMethods: []string{MethodHeuristic, MethodLocalLLM},
			Evidence: []Evidence{{
				Kind: EvidenceEmailLocalPart, SignalCategory: CatEmail,
				SignalText: "marie.duval@example.com", Documents: []string{"engagement.md"},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loaded, err := LoadSession(raw)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(loaded.Values) != 1 {
		t.Fatalf("expected one Value back, got %d", len(loaded.Values))
	}
	got := loaded.Values[0]
	if got.DerivesSpellings() {
		t.Errorf("a curated Value must reload curated, got policy %q", got.SpellingPolicy)
	}
	// Provenance and evidence survive the file too. Losing them would leave the
	// workspace unable to say why a Value is there after a reload, and would
	// silently promote a local model finding to a user declaration in precedence.
	if len(got.DiscoveryMethods) != 2 {
		t.Errorf("every discovery method must survive the file, got %v", got.DiscoveryMethods)
	}
	if len(got.Evidence) != 1 || got.Evidence[0].Kind != EvidenceEmailLocalPart {
		t.Errorf("the evidence must survive the file, got %+v", got.Evidence)
	}
}

func TestMappingExportGolden(t *testing.T) {
	reg := NewRegistry()
	reg.Assign(CatEmail, "marie.duval@example.com")
	reg.Assign("entity_names", "Alpine Trust")
	reg.Assign(CatEmail, "marie.duval@example.com") // second occurrence

	out, err := MappingToCSV(reg.Export())
	if err != nil {
		t.Fatalf("MappingToCSV: %v", err)
	}
	// Rows come out longest-original-first (Registry.Export), which is why the
	// email precedes the shorter value name here.
	want := "original,placeholder,category,count\n" +
		"marie.duval@example.com,[EMAIL_1],email,2\n" +
		"Alpine Trust,[ENTITY_1],entity_names,1\n"
	if string(out) != want {
		t.Errorf("mapping CSV golden mismatch:\n--- got ---\n%s--- want ---\n%s", out, want)
	}
}

func TestExportFileName(t *testing.T) {
	tests := [][3]string{
		{"notes.txt", "txt", "notes_anon.txt"},
		{"report.docx", "md", "report_anon.md"},
		{"workbook.xlsx#Clients", "csv", "workbook.xlsx_Clients_anon.csv"},
	}
	for _, tt := range tests {
		if got := ExportFileName(tt[0], tt[1]); got != tt[2] {
			t.Errorf("ExportFileName(%q,%q) = %q, want %q", tt[0], tt[1], got, tt[2])
		}
	}
}

// TestLoadSessionWithoutOptionalFields: a session file that omits the optional
// settings blocks must load, and each missing field must land on the value that
// reproduces the behaviour of a session that never had it. requireChecksum in
// particular must land on false, which is the shipped default: anything else
// would mean opening a session quietly changed what gets replaced
//
// The version is the CURRENT one on purpose.  refuses a file
// written by another version outright (see TestSessionSaveLoadEquality), so
// "field absent" and "file too old" are now two different situations and only
// the first one is a compatibility question.
func TestLoadSessionWithoutOptionalFields(t *testing.T) {
	// The version number is DERIVED, not typed. This fixture is about absent
	// optional fields, so a version bump elsewhere must not fail it: it failed
	// exactly that way on the bump, reporting a refusal that
	// was correct and irrelevant.
	legacy := fmt.Appendf(nil, `{
	  "version": %d,
	  "values": [{"category": "entity_names", "mainText": "Alpine Trust"}],
	  "allowTerms": ["CSSF"],
	  "patterns": [],
	  "settings": {
	    "presets": {"patterns.depth": "standard", "names.depth": "standard"},
	    "categories": {"email": true, "person_names": true},
	    "ollamaPort": 11434,
	    "model": "qwen3.5:0.8b",
	    "contextSize": 8192,
	    "useLocalLLM": true
	  },
	  "registry": []
	}`, SessionVersion)

	s, err := LoadSession(legacy)
	if err != nil {
		t.Fatalf("a session omitting the optional blocks must load, got: %v", err)
	}
	if s.Settings.RequireChecksum {
		t.Error("RequireChecksum = true from a file that says nothing about it; " +
			"absence must read as the shipped default, so a restored session replaces " +
			"exactly what it always did")
	}
	// The fields that WERE present must survive untouched.
	if s.Settings.Presets[PresetKey(ScopePatterns, FamilyDepth)] != PresetStandard ||
		!s.Settings.UseLocalLLM || s.Settings.ContextSize != 8192 {
		t.Errorf("existing settings were not preserved: %+v", s.Settings)
	}
	if len(s.Values) != 1 || s.Values[0].MainText != "Alpine Trust" {
		t.Errorf("Values were not preserved: %+v", s.Values)
	}
	// A Value with no stated confidence is one the USER declared, and must stay
	// filterable only as one (see engine/confidence_test.go).
	if s.Values[0].Confidence != 0 {
		t.Errorf("a Value with no stated confidence must carry none, got %v", s.Values[0].Confidence)
	}
}

// TestSessionRoundTripsRequireChecksum: the switch survives save/load. It has to
// travel, because it changes what a run replaces, and a restored session that
// silently lost it would anonymise a checksum-failed identifier the user had
// deliberately chosen to leave in clear.
func TestSessionRoundTripsRequireChecksum(t *testing.T) {
	raw, err := SaveSession(Session{
		Settings: SessionSettings{Presets: depthPresets(PresetStandard), OllamaPort: 11434, RequireChecksum: true},
	})
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	back, err := LoadSession(raw)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if !back.Settings.RequireChecksum {
		t.Error("RequireChecksum did not survive the file")
	}
}

// TestHeuristicDiscoveryAbsentVersusExplicitZero: the pointer field must tell
// "the file said nothing" apart from "the user deliberately turned every filter
// off". Collapsing the two would silently re-enable filtering for someone who
// switched it off.
func TestHeuristicDiscoveryAbsentVersusExplicitZero(t *testing.T) {
	absent, err := LoadSession(fmt.Appendf(nil,
		`{"version":%d,"settings":{"level":"medium"}}`, SessionVersion))
	if err != nil {
		t.Fatalf("a session with no heuristicDiscovery block: %v", err)
	}
	if absent.Settings.HeuristicDiscovery != nil {
		t.Errorf("an absent block must load as nil, got %+v", absent.Settings.HeuristicDiscovery)
	}

	off := HeuristicDiscoveryOptions{}
	raw, err := SaveSession(Session{Settings: SessionSettings{Presets: depthPresets(PresetStandard), HeuristicDiscovery: &off}})
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	back, err := LoadSession(raw)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if back.Settings.HeuristicDiscovery == nil {
		t.Fatal("an explicitly all-zero block must survive as a present value")
	}
	if *back.Settings.HeuristicDiscovery != off {
		t.Errorf("HeuristicDiscovery = %+v, want %+v", *back.Settings.HeuristicDiscovery, off)
	}
}

// TestSessionRoundTripsHeuristicDiscovery: the tuning survives save and load.
func TestSessionRoundTripsHeuristicDiscovery(t *testing.T) {
	want := HeuristicDiscoveryOptions{
		MinLength: 6, MinOccurrences: 2, ExcludeCommonWords: true, MinConfidence: 0.8,
	}
	raw, err := SaveSession(Session{Settings: SessionSettings{Presets: depthPresets(PresetStandard), HeuristicDiscovery: &want}})
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	back, err := LoadSession(raw)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if back.Settings.HeuristicDiscovery == nil || *back.Settings.HeuristicDiscovery != want {
		t.Errorf("HeuristicDiscovery = %+v, want %+v", back.Settings.HeuristicDiscovery, want)
	}
}

// TestSessionRoundTripsSignalSources: which READINGS of which built-in signals may
// derive Suggestions is a user decision, and a file that cannot state it would
// silently re-enable a reading the user switched off.
//
// Both readings off and one of two off are asserted, because the second is the
// case a per-source boolean could not express: it is the reason the stored shape is
// keyed by derivation and the reason SessionVersion moved to 8.
func TestSessionRoundTripsSignalSources(t *testing.T) {
	t.Run("roundtrip/every_reading_off", func(t *testing.T) {
		raw, err := SaveSession(Session{Settings: SessionSettings{
			Presets: depthPresets(PresetStandard),
			SignalSuggestionSources: SignalSourceSelection{SignalSourceEmail: {
				DerivationEmailPerson:       false,
				DerivationEmailOrganisation: false,
			}},
		}})
		if err != nil {
			t.Fatalf("SaveSession: %v", err)
		}
		back, err := LoadSession(raw)
		if err != nil {
			t.Fatalf("LoadSession: %v", err)
		}
		if SignalSourceEnabled(back.Settings.SignalSuggestionSources, SignalSourceEmail) {
			t.Errorf("every reading switched off must reload off, got %+v",
				back.Settings.SignalSuggestionSources)
		}
	})

	t.Run("roundtrip/one_reading_off", func(t *testing.T) {
		raw, err := SaveSession(Session{Settings: SessionSettings{
			Presets: depthPresets(PresetStandard),
			SignalSuggestionSources: SignalSourceSelection{SignalSourceEmail: {
				DerivationEmailPerson:       false,
				DerivationEmailOrganisation: true,
			}},
		}})
		if err != nil {
			t.Fatalf("SaveSession: %v", err)
		}
		back, err := LoadSession(raw)
		if err != nil {
			t.Fatalf("LoadSession: %v", err)
		}
		sel := back.Settings.SignalSuggestionSources
		if SignalDerivationEnabled(sel, SignalSourceEmail, DerivationEmailPerson) {
			t.Errorf("the cleared reading must reload cleared, got %+v", sel)
		}
		if !SignalDerivationEnabled(sel, SignalSourceEmail, DerivationEmailOrganisation) {
			t.Errorf("the reading left on must reload on, got %+v", sel)
		}
		if !SignalSourceEnabled(sel, SignalSourceEmail) {
			t.Errorf("the signal still derives something, so its master must read on, got %+v", sel)
		}
	})
}

// TestSessionRoundTripsImageDecisions: a picture decision has to survive the file
// or a restored session exports the client logo the user had boxed, silently,
// while the screen they saved from said it was anonymised.
func TestSessionRoundTripsImageDecisions(t *testing.T) {
	t.Run("roundtrip/session_v9_image_decisions", func(t *testing.T) {
		saved := Session{
			Settings: SessionSettings{Presets: depthPresets(PresetStandard), OllamaPort: 11434},
			ImageDecisions: map[string]map[string]imaging.Decision{
				"deck.pptx": {
					"ppt/media/image1.png": {
						Treatment: imaging.TreatmentBox,
						BoxText:   "Client logo removed",
					},
					"ppt/media/image4.jpeg": {
						Treatment:    imaging.TreatmentBlur,
						BlurStrength: 8,
					},
				},
				"report.docx": {
					"word/media/image3.png": {Treatment: imaging.TreatmentRemove},
				},
			},
		}
		raw, err := SaveSession(saved)
		if err != nil {
			t.Fatalf("SaveSession: %v", err)
		}
		back, err := LoadSession(raw)
		if err != nil {
			t.Fatalf("LoadSession: %v", err)
		}

		for doc, byAsset := range saved.ImageDecisions {
			for id, want := range byAsset {
				got, ok := back.ImageDecisions[doc][id]
				if !ok {
					t.Errorf("the decision for %s in %s did not survive the file; the restored "+
						"session would export the original picture", id, doc)
					continue
				}
				if got != want {
					t.Errorf("the decision for %s in %s came back %+v, want %+v", id, doc, got, want)
				}
			}
		}
	})

	t.Run("roundtrip/session_v9_absent_decisions_are_absent", func(t *testing.T) {
		raw, err := SaveSession(Session{Settings: SessionSettings{Presets: depthPresets(PresetStandard)}})
		if err != nil {
			t.Fatalf("SaveSession: %v", err)
		}
		if strings.Contains(string(raw), "imageDecisions") {
			t.Errorf("a session with no picture decisions wrote the field anyway:\n%s", raw)
		}
		back, err := LoadSession(raw)
		if err != nil {
			t.Fatalf("LoadSession: %v", err)
		}
		if len(back.ImageDecisions) != 0 {
			t.Errorf("a file with no decisions loaded %d of them", len(back.ImageDecisions))
		}
	})
}

// TestSessionVersionRefusesAnOlderFile: the strict-version rule, at the version
// the cross-route confidence floor LEFT the schema and the checksum switch
// entered it.
//
// A v12 file carries `minConfidence`, a number that was doing two unrelated
// things: deciding whether a checksum-failed pattern match is replaced, and,
// above roughly 0.8, dropping Values the user had already accepted. This build
// carries `requireChecksum`, which is only the first of those, in words. Neither
// is readable as the other. A v12 file's 0.9 says nothing about the checksum
// question and, read as a boolean, either invents a veto the user never asked for
// or loses one they did; and a v12 reader finding no floor falls back to 0, which
// silently restores the replacement of every accepted Value the saved floor had
// been suppressing. Either way a restored session replaces a different set of
// text than the file describes, which is the shape of silent disagreement the
// rule exists for.
//
// The v12 fixture below spells `minConfidence` out, because a file carrying the
// CURRENT key under the OLD version would not be the file this test is about.
func TestSessionVersionRefusesAnOlderFile(t *testing.T) {
	t.Run("errors/older_versions_are_refused", func(t *testing.T) {
		if SessionVersion != 13 {
			t.Fatalf("SessionVersion is %d; this test describes the move to 13 and must be "+
				"rewritten for the version that replaces it", SessionVersion)
		}
		for _, older := range []int{10, 11, 12} {
			raw := fmt.Sprintf(`{"version":%d,"values":[],"allowTerms":[],"patterns":[],`+
				`"settings":{"ollamaPort":11434,"model":"qwen3.5:0.8b"},`+
				`"registry":[]}`, older)
			_, err := LoadSession([]byte(raw))
			if err == nil {
				t.Fatalf("a version %d session file was accepted; a session file is read only by "+
					"the version that wrote it, because a half-read one silently reassigns "+
					"placeholders", older)
			}
			// The message has to name BOTH numbers, or the user is told the file is
			// wrong without being told what would be right.
			if !strings.Contains(err.Error(), "13") {
				t.Errorf("the refusal does not say which version this build reads:\n%v", err)
			}
			if !strings.Contains(err.Error(), fmt.Sprint(older)) {
				t.Errorf("the refusal does not say which version the file holds:\n%v", err)
			}
		}
	})

	t.Run("errors/no_migration_path_exists", func(t *testing.T) {
		// The policy is refusal, never migration: a session file holds the
		// re-identification key, and a half-migrated one silently reassigns
		// placeholders. A v12 file carrying the retired `minConfidence` key must be
		// refused on the version alone, before anything reads a field, and the
		// loader must hold no alias that would turn 0.9 into a boolean.
		raw := `{"version":12,"values":[{"category":"person_names","mainText":"Marie Duval",` +
			`"discoveryMethods":["local_llm"]}],"allowTerms":[],"patterns":[],` +
			`"settings":{"minConfidence":0.9,"ollamaPort":11434,"model":"qwen3.5:0.8b"},` +
			`"registry":[]}`
		if _, err := LoadSession([]byte(raw)); err == nil {
			t.Fatal("a v12 file carrying the retired minConfidence key was accepted; there is no " +
				"migration table and no compatibility alias anywhere in the loader")
		}
	})
}

// TestNewValueCategoriesHaveAPlaceholderLabel is the assertion the version bump
// exists for: every value category the engine ships can actually be assigned a
// placeholder. Registry.Assign panics on a category with no label, so a category
// added without its row is a crash on the first run that uses it.
func TestNewValueCategoriesHaveAPlaceholderLabel(t *testing.T) {
	for _, category := range append(
		append([]string{}, AllValueCategories...), AllPIICategories...) {
		reg := NewRegistry()
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("assigning a placeholder for category %q panicked (%v): "+
						"every shipped category needs a placeholderLabels row", category, r)
				}
			}()
			if got := reg.Assign(category, "sample"); got == "" {
				t.Errorf("category %q produced an empty placeholder", category)
			}
		}()
	}
}

// TestDefinedTermsSurviveTheFile: the terms a document declares about itself are
// enforced through the allowlist, so a restored session that lost them would
// start suggesting every one of them again. They are stored SEPARATELY from the
// user's own never-anonymise terms, because deleting a term the user typed is not
// the same gesture as dropping a definition read out of a document.
func TestDefinedTermsSurviveTheFile(t *testing.T) {
	saved := Session{
		Settings: SessionSettings{Presets: depthPresets(PresetStandard)},
		DefinedTerms: []DefinedTerm{
			{Term: "Work Order", Idiom: DefinitionIdiomMeans, Document: "a.docx"},
			{Term: "Dedicated Advisors", Idiom: DefinitionIdiomParenthetical, Document: "a.docx"},
		},
	}
	raw, err := SaveSession(saved)
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	back, err := LoadSession(raw)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(back.DefinedTerms) != len(saved.DefinedTerms) {
		t.Fatalf("the file restored %d defined terms, want %d", len(back.DefinedTerms), len(saved.DefinedTerms))
	}
	for i, want := range saved.DefinedTerms {
		if back.DefinedTerms[i] != want {
			t.Errorf("defined term %d came back %+v, want %+v", i, back.DefinedTerms[i], want)
		}
	}
	if strings.Contains(string(raw), `"allowTerms": [\n    "Work Order"`) {
		t.Error("a defined term was written into the user's own never-anonymise list")
	}
}
