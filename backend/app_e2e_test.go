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
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
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
			{Category: "client_names", Canonical: "Alpine Trust"},
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
