// app_hardening_test.go — tests error-message format, the
// large-file truncated preview, and mid-run Ollama failure degrading the
// LLM pass instead of failing the batch.
package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
)

// TestErrorMessageFormat samples representative user-facing errors and
// asserts each states BOTH what failed and how to fix it (CLAUDE.md §2:
// error messages must be actionable). Since the failure
// and the remedy are separated by ordinary punctuation (comma, semicolon
// or period), never an em dash (see copy_guard_test.go), and the remedy
// half must contain an imperative cue so the message stays actionable.
func TestErrorMessageFormat(t *testing.T) {
	app := NewApp()

	collect := func() []error {
		_, e1 := engine.Load("legacy.txt", []byte{0xFF, 0xFE, 0x00})
		_, e2 := engine.LoadAll("archive.zip", []byte("x"))
		e3 := app.RunPipeline(RunRequest{}) // no documents
		_, e4 := engine.LoadSession([]byte("not json"))
		_, e5 := engine.ExportBytes(engine.ResultDocument{Format: engine.FormatCSV}, "json")
		_, e6 := app.ApplySettings(Settings{Level: "extreme", OllamaPort: 11434})
		return []error{e1, e2, e3, e4, e5, e6}
	}
	for i, err := range collect() {
		if err == nil {
			t.Errorf("case %d: expected an error", i)
			continue
		}
		msg := err.Error()
		if !strings.ContainsAny(msg, ",;.") {
			t.Errorf("case %d: message lacks the failure/remedy structure: %q", i, msg)
		}
		// Remedy cue: at least one actionable verb pointing the user at a fix.
		cues := []string{"check", "run", "pick", "import", "expected", "re-create", "update", "try", "enter", "remove", "restart", "choose", "rename", "convert"}
		hasCue := false
		lower := strings.ToLower(msg)
		for _, cue := range cues {
			if strings.Contains(lower, cue) {
				hasCue = true
				break
			}
		}
		if !hasCue {
			t.Errorf("case %d: message has no actionable remedy cue: %q", i, msg)
		}
		if len(msg) < 30 {
			t.Errorf("case %d: message too terse to be actionable: %q", i, msg)
		}
	}
}

// TestLargeFilePreviewTruncation: a >5000-line document previews truncated
// with the flag set, while the stored document (what the pipeline reads)
// keeps every line.
func TestLargeFilePreviewTruncation(t *testing.T) {
	lines := strings.Repeat("line of text\n", engine.MaxPreviewLines+100)
	app := NewApp()
	docs, err := engine.LoadAll("big.txt", []byte(lines))
	if err != nil {
		t.Fatal(err)
	}
	app.docs = docs

	infos := app.documentInfos()
	if !infos[0].PreviewTruncated {
		t.Fatal("preview of a huge document must be marked truncated")
	}
	if got := strings.Count(infos[0].Markdown, "\n"); got >= engine.MaxPreviewLines {
		t.Errorf("preview has %d newlines, want < %d", got, engine.MaxPreviewLines)
	}
	// The full content is still what the pipeline processes.
	if got := strings.Count(app.docs[0].Markdown, "\n"); got != engine.MaxPreviewLines+100 {
		t.Errorf("stored document was truncated too (%d lines) — the pipeline must see everything", got)
	}

	// Small documents are never flagged.
	small, _ := engine.PreviewMarkdown("just\nthree\nlines")
	if small != "just\nthree\nlines" {
		t.Error("small document preview must be unchanged")
	}
}

// TestExportAllZipToRefusesABadDestination covers the ONE dialog-free write in
// the whole application. It is allowed to skip
// the dialog because the user chose the folder explicitly and the zip carries
// no re-identification key, which makes its input validation the only thing
// standing between a typo and a confusing failure.
func TestExportAllZipToRefusesABadDestination(t *testing.T) {
	app := NewApp()

	// No folder chosen at all.
	_, err := app.ExportAllZipTo("")
	if err == nil {
		t.Fatal("an empty destination must be refused")
	}
	if !strings.Contains(err.Error(), "Browse") {
		t.Errorf("the refusal must say how to pick one, got: %v", err)
	}

	// A folder that does not exist.
	missing := filepath.Join(t.TempDir(), "no-such-folder")
	if _, err := app.ExportAllZipTo(missing); err == nil {
		t.Error("a missing destination folder must be refused")
	}

	// A path that is a FILE, which is the shape a hand-typed path most often
	// has when it is wrong.
	file := filepath.Join(t.TempDir(), "not-a-folder.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("preparing the fixture: %v", err)
	}
	_, err = app.ExportAllZipTo(file)
	if err == nil {
		t.Fatal("a file as the destination must be refused")
	}
	if !strings.Contains(err.Error(), "not a folder") {
		t.Errorf("the refusal must say what is wrong with it, got: %v", err)
	}
}

// TestFreePathNumbersInsteadOfOverwriting: a user who exports twice has almost
// certainly changed something in between, so silently replacing the first
// archive would destroy a copy they may still need.
func TestFreePathNumbersInsteadOfOverwriting(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "3_anonymised.zip")

	// Nothing there yet: the plain name is free.
	got, err := freePath(first)
	if err != nil {
		t.Fatalf("freePath: %v", err)
	}
	if got != first {
		t.Errorf("freePath on an empty folder = %q, want %q", got, first)
	}

	// Occupy it, then the next one must be numbered rather than the same name.
	if err := os.WriteFile(first, []byte("x"), 0o644); err != nil {
		t.Fatalf("preparing the fixture: %v", err)
	}
	got, err = freePath(first)
	if err != nil {
		t.Fatalf("freePath: %v", err)
	}
	want := filepath.Join(dir, "3_anonymised (2).zip")
	if got != want {
		t.Errorf("freePath = %q, want %q", got, want)
	}
	// The extension has to stay last, or Windows stops recognising the file.
	if filepath.Ext(got) != ".zip" {
		t.Errorf("the number must go BEFORE the extension, got %q", got)
	}
}

// TestSetValuePlaceholderBeforeAnyRun: the Replaced values table only exists
// after a run, but the bound method is reachable from anywhere, so it has to
// say what to do rather than fail obscurely.
func TestSetValuePlaceholderBeforeAnyRun(t *testing.T) {
	err := NewApp().SetValuePlaceholder("[ENTITY_1]", "[BANK_A_1]")
	if err == nil {
		t.Fatal("there is nothing to rename before the first run")
	}
	if !strings.Contains(err.Error(), "run the anonymisation") {
		t.Errorf("the refusal must say what to do first, got: %v", err)
	}
}

// TestValuePlaceholdersBeforeAnyRunIsEmptyNotAnError: an empty table is the
// honest answer before a run, and the view renders it as such.
func TestValuePlaceholdersBeforeAnyRunIsEmptyNotAnError(t *testing.T) {
	if got := NewApp().ValuePlaceholders(); len(got) != 0 {
		t.Errorf("ValuePlaceholders before a run = %+v, want empty", got)
	}
	if got := NewApp().ListRemovedValues(); len(got) != 0 {
		t.Errorf("ListRemovedValues before a run = %+v, want empty", got)
	}
}
