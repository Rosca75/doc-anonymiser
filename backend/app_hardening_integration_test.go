//go:build integration

// app_hardening_integration_test.go — the export-write guards that need a real
// filesystem.
//
// TIER: integration (docs/TESTING.md). Both tests exercise real file I/O: they
// create temp directories and files with t.TempDir/os.WriteFile and assert how
// the export layer validates a chosen destination and numbers a colliding
// output path. That genuinely requires the filesystem, so it is not a unit
// test; it stays hermetic (a temp dir, no network), so it is not deep. The
// fixture-free hardening guards (error-message format, preview truncation, the
// before-a-run refusals) are in app_hardening_test.go.
package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
