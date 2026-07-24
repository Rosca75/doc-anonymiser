// app_export.go — bound methods for the Export screen (Phase 9): per-file
// save, export-all zip, clipboard, mapping/report export and session
// save/load. Every path goes through an explicit native dialog — nothing
// is ever written without the user choosing a destination (CLAUDE.md §5),
// and no source file is ever overwritten (originals are immutable; all
// suggested filenames carry the _anon suffix or a distinct extension).
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"doc-anonymiser/engine"
)

// findResultDoc returns the anonymised document by name, or an actionable
// error when there are no results / no such document.
func (a *App) findResultDoc(name string) (engine.ResultDocument, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.results == nil {
		return engine.ResultDocument{}, fmt.Errorf("no results yet, run the pipeline on the Run screen first")
	}
	for _, rd := range a.results.Documents {
		if rd.Name == name {
			return rd, nil
		}
	}
	return engine.ResultDocument{}, fmt.Errorf("document %q is not part of the last run, re-run the pipeline after changing the import list", name)
}

// saveWithDialog opens the native save dialog and writes data. A cancelled
// dialog (empty path) is a silent no-op, not an error.
func (a *App) saveWithDialog(defaultName string, filterName, pattern string, data []byte) error {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save",
		DefaultFilename: defaultName,
		Filters:         []runtime.FileFilter{{DisplayName: filterName, Pattern: pattern}},
	})
	if err != nil {
		return fmt.Errorf("the save dialog could not be opened (%v), try again; if it keeps failing, restart the application", err)
	}
	if path == "" {
		return nil // user cancelled
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("could not write %q: %v, check that the folder exists and is writable (not a read-only network share)", path, err)
	}
	return nil
}

// ExportDocumentFormats returns the offered extensions for one result
// document (default first) so the UI renders the right buttons.
func (a *App) ExportDocumentFormats(name string) ([]string, error) {
	rd, err := a.findResultDoc(name)
	if err != nil {
		return nil, err
	}
	return engine.ExportExtensions(rd.Format), nil
}

// SaveDocument exports one anonymised document in the chosen extension via
// the native save dialog.
func (a *App) SaveDocument(name, ext string) error {
	rd, err := a.findResultDoc(name)
	if err != nil {
		return err
	}
	data, err := engine.ExportBytes(rd, ext)
	if err != nil {
		return err
	}
	return a.saveWithDialog(engine.ExportFileName(name, ext), "."+ext, "*."+ext, data)
}

// ExportAllZip packs every anonymised document (default format each) into
// "<n>_anonymised.zip" behind a save dialog.
func (a *App) ExportAllZip() error {
	a.mu.Lock()
	results := a.results
	n := 0
	if results != nil {
		n = len(results.Documents)
	}
	a.mu.Unlock()

	data, err := engine.BuildExportZip(results)
	if err != nil {
		return err
	}
	return a.saveWithDialog(fmt.Sprintf("%d_anonymised.zip", n), "Zip archive", "*.zip", data)
}

// CopyDocument puts one anonymised document's text on the system clipboard.
func (a *App) CopyDocument(name string) error {
	rd, err := a.findResultDoc(name)
	if err != nil {
		return err
	}
	if err := runtime.ClipboardSetText(a.ctx, rd.Anonymised); err != nil {
		return fmt.Errorf("could not write to the clipboard (%v), try again", err)
	}
	return nil
}

// ExportMapping saves the registry (the RE-IDENTIFICATION KEY) as CSV or
// JSON. The confirmation warning is shown by the UI BEFORE calling this —
// by the time we are here the user has explicitly accepted the risk.
func (a *App) ExportMapping(format string) error {
	a.mu.Lock()
	reg := a.registry
	a.mu.Unlock()
	if reg == nil {
		return fmt.Errorf("no mapping yet, run the pipeline first")
	}

	switch format {
	case "csv":
		data, err := engine.MappingToCSV(reg.Export())
		if err != nil {
			return err
		}
		return a.saveWithDialog("entity_mapping.csv", "CSV", "*.csv", data)
	case "json":
		rep, err := jsonMarshalIndent(reg.Export())
		if err != nil {
			return err
		}
		return a.saveWithDialog("entity_mapping.json", "JSON", "*.json", rep)
	default:
		return fmt.Errorf("unknown mapping format %q, expected csv or json", format)
	}
}

// ExportReport saves the run report as JSON or human-readable markdown.
func (a *App) ExportReport(format string) error {
	a.mu.Lock()
	results := a.results
	a.mu.Unlock()
	if results == nil {
		return fmt.Errorf("no report yet, run the pipeline first")
	}

	switch format {
	case "json":
		data, err := results.Report.ToJSON()
		if err != nil {
			return err
		}
		return a.saveWithDialog("anonymisation_report.json", "JSON", "*.json", data)
	case "md":
		return a.saveWithDialog("anonymisation_report.md", "Markdown", "*.md",
			[]byte(results.Report.ToMarkdown()))
	default:
		return fmt.Errorf("unknown report format %q, expected json or md", format)
	}
}

// SaveSessionToFile persists entities + allowlist + patterns + rules +
// settings + registry to a .anonsession.json. The UI shows the
// re-identification-key warning before calling (CLAUDE.md §5: sensitive
// state leaves memory only on explicit user action).
func (a *App) SaveSessionToFile(req RunRequest) error {
	a.mu.Lock()
	settings := a.settings
	var registry []engine.MappingEntry
	if a.registry != nil {
		registry = a.registry.Export()
	}
	a.mu.Unlock()

	data, err := engine.SaveSession(engine.Session{
		Entities:    req.Entities,
		AllowTerms:  req.AllowTerms,
		Patterns:    req.Patterns,
		SimpleRules: req.SimpleRules,
		Settings: engine.SessionSettings{
			Level:       settings.Level,
			Categories:  settings.Categories,
			OllamaPort:  settings.OllamaPort,
			Model:       settings.Model,
			ContextSize: settings.ContextSize,
			UseAI:       settings.UseAI,
		},
		Registry: registry,
	})
	if err != nil {
		return err
	}
	return a.saveWithDialog("session.anonsession.json", "Session", "*.anonsession.json;*.json", data)
}

// LoadSessionFromFile opens a session file, restores the Go-side state
// (registry + settings) and returns the session so the frontend can
// restore its own state (entities, allowlist, patterns, rules).
func (a *App) LoadSessionFromFile() (*engine.Session, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Load session",
		Filters: []runtime.FileFilter{{DisplayName: "Session files", Pattern: "*.anonsession.json;*.json"}},
	})
	if err != nil {
		return nil, fmt.Errorf("the file dialog could not be opened (%v), try again", err)
	}
	if path == "" {
		return nil, nil // user cancelled — the UI treats nil as "nothing happened"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read %q: %v, check that the file still exists", path, err)
	}
	session, err := engine.LoadSession(raw)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	a.registry = engine.NewRegistryFromEntries(session.Registry)
	restored := Settings{
		Level:       session.Settings.Level,
		Categories:  session.Settings.Categories,
		OllamaPort:  session.Settings.OllamaPort,
		Model:       session.Settings.Model,
		ContextSize: session.Settings.ContextSize,
		UseAI:       session.Settings.UseAI,
	}
	// v1 sessions predate these fields: zero values mean "keep the
	// current settings" instead of silently resetting them.
	if restored.Categories == nil {
		restored.Categories = a.settings.Categories
	}
	if restored.ContextSize == 0 {
		restored.ContextSize = a.settings.ContextSize
	}
	a.settings = restored
	a.mu.Unlock()
	// Apply the (possibly different) Ollama port/model through the same
	// validated path the settings screen uses.
	if _, err := a.ApplySettings(a.GetSettings()); err != nil {
		return nil, err
	}
	return &session, nil
}

// jsonMarshalIndent is a tiny wrapper so the import list stays tidy above.
func jsonMarshalIndent(v interface{}) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("could not serialise to JSON: %w", err)
	}
	return data, nil
}
