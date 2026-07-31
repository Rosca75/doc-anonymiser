// app_hardening_test.go — Phase 10 tests: error-message format, the
// large-file truncated preview, and mid-run Ollama failure degrading the
// LLM pass instead of failing the batch.
package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/ollama"
)

// TestErrorMessageFormat samples representative user-facing errors and
// asserts each states BOTH what failed and how to fix it (CLAUDE.md §2:
// error messages must be actionable). Since BUILD-02 Phase 1 the failure
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

	infos := app.ListDocuments()
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

// TestMidRunOllamaCrashDegrades: the mock Ollama answers the first chat
// call then dies; a 2-document deep-scan run must still anonymise BOTH
// documents deterministically, mark the LLM pass degraded and record a
// warning — never fail the batch (CLAUDE.md §4 graceful degradation).
func TestMidRunOllamaCrashDegrades(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"m"}]}`))
		case "/api/chat":
			if calls.Add(1) > 1 {
				// Simulate the crash: hijack and slam the connection.
				hj, _ := w.(http.Hijacker)
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
			resp, _ := json.Marshal(map[string]interface{}{
				"message": map[string]string{"role": "assistant",
					"content": `{"client_names":[],"project_names":[],"internal_names":[],"person_names":[]}`},
			})
			w.Write(resp)
		}
	}))
	defer srv.Close()

	app := NewApp()
	app.llm = ollama.New(srv.URL)
	app.docs = []engine.Document{
		{Name: "1.txt", Format: engine.FormatTXT, Markdown: "first mail one@example.com"},
		{Name: "2.txt", Format: engine.FormatTXT, Markdown: "second mail two@example.com"},
	}

	res, err := app.runPipelineBlocking(context.Background(), RunRequest{UseDeepScan: true})
	if err != nil {
		t.Fatalf("mid-run crash must not fail the batch: %v", err)
	}
	if len(res.Documents) != 2 {
		t.Fatalf("both documents must be processed, got %d", len(res.Documents))
	}
	// The deterministic passes still did their job on the failed file.
	if !strings.Contains(res.Documents[1].Anonymised, "[EMAIL_") {
		t.Errorf("deterministic passes skipped on the degraded document: %q", res.Documents[1].Anonymised)
	}
	if !strings.HasPrefix(res.Report.LLMPass, "degraded") {
		t.Errorf("LLM pass note = %q, want degraded", res.Report.LLMPass)
	}
	found := false
	for _, w := range res.Report.Warnings {
		if strings.Contains(w, "deep-scan failed") && strings.Contains(w, "2.txt") {
			found = true
		}
	}
	if !found {
		t.Errorf("degradation warning missing: %+v", res.Report.Warnings)
	}
}

// TestExportAllZipToRefusesABadDestination covers the ONE dialog-free write in
// the whole application (BUILD-05 Phase 3, decision 4). It is allowed to skip
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

// TestSetEntityPlaceholderBeforeAnyRun: the field exists on the entity cards
// from the start, so pressing it before a run has to say what to do rather than
// fail obscurely.
func TestSetEntityPlaceholderBeforeAnyRun(t *testing.T) {
	app := NewApp()
	err := app.SetEntityPlaceholder("client_names", "Meridian Consulting", "[BANK_A_1]")
	if err == nil {
		t.Fatal("there is nothing to rename before the first run")
	}
	if !strings.Contains(err.Error(), "run the anonymisation") {
		t.Errorf("the refusal must say what to do first, got: %v", err)
	}
	if got := app.EntityPlaceholder("client_names", "Meridian Consulting"); got != "" {
		t.Errorf("EntityPlaceholder before a run = %q, want \"\"", got)
	}
}
