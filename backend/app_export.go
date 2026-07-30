// app_export.go — bound methods for the Export screen (Phase 9): per-file
// save, export-all zip, clipboard, mapping/report export and session
// save/load. Every path goes through an explicit native dialog — nothing
// is ever written without the user choosing a destination (CLAUDE.md §5),
// and no source file is ever overwritten (originals are immutable; all
// suggested filenames carry the _anon suffix or a distinct extension).
package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/engine/exportfmt"
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
// the native save dialog. Native Office extensions route through the
// same-format rewriter (BUILD-02 Phase 11); text extensions through
// ExportBytes as before.
func (a *App) SaveDocument(name, ext string) error {
	rd, err := a.findResultDoc(name)
	if err != nil {
		return err
	}
	if engine.SameFormatExtensions[ext] {
		// Defensive route: the UI normally goes through the metadata
		// review (SaveSameFormat); a direct call exports with the
		// original properties untouched and the default filename.
		return a.SaveSameFormat(name, ext, nil, "")
	}
	data, err := engine.ExportBytes(rd, ext)
	if err != nil {
		return err
	}
	return a.saveWithDialog(engine.ExportFileName(name, ext), "."+ext, "*."+ext, data)
}

// sameFormatBytes produces the anonymised same-format copy for one
// document from its ORIGINAL in-memory bytes (Document.Raw; the file on
// disk is never read again, let alone written). Replacements reuse the
// last run's inputs plus the session registry, so placeholders match the
// text export exactly. Extra hits in parts the user never previewed
// (docx headers/footers/footnotes) are appended to the report warnings.
func (a *App) sameFormatBytes(name, ext string) ([]byte, error) {
	cfg, src, err := a.sameFormatConfig(name)
	if err != nil {
		return nil, err
	}

	switch ext {
	case "docx":
		data, extras, _, err := exportfmt.ExportDocx(src.Raw, cfg)
		if err != nil {
			return nil, err
		}
		if extras.Total() > 0 {
			a.appendReportWarning(fmt.Sprintf(
				"document_extras: %d replacement(s) were made in parts of %q that the preview does not show (headers, footers or footnotes)",
				extras.Total(), name))
		}
		return data, nil
	case "pptx":
		data, _, err := exportfmt.ExportPptx(src.Raw, cfg)
		return data, err
	case "xlsx":
		data, _, err := exportfmt.ExportXlsx(src.Raw, cfg)
		return data, err
	default:
		return nil, fmt.Errorf("same-format export does not support .%s", ext)
	}
}

// SameFormatMeta is the review payload for one document's same-format
// export (BUILD-02 Phase 12): every document property with its proposed
// replacement, plus the proposed anonymised filename.
type SameFormatMeta struct {
	Fields   []exportfmt.MetaProposal `json:"fields"`
	Filename string                   `json:"filename"`
}

// GetSameFormatMetadata extracts the document properties of one imported
// file and proposes replacements through the same pipeline path as body
// text (allowlist wins; unchanged fields are marked). Nothing is
// rewritten here; the user reviews first (improvement plan §6.1).
func (a *App) GetSameFormatMetadata(name, ext string) (*SameFormatMeta, error) {
	cfg, src, err := a.sameFormatConfig(name)
	if err != nil {
		return nil, err
	}
	var fields []exportfmt.MetaField
	if ext == "pdf" {
		fields, err = exportfmt.ExtractPDFMetadata(src.Raw)
	} else {
		fields, err = exportfmt.ExtractMetadata(src.Raw)
	}
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	reg := a.registry
	docIndex := 0
	for i := range a.docs {
		if a.docs[i].Name == name {
			docIndex = i + 1
		}
	}
	a.mu.Unlock()
	var entries []engine.MappingEntry
	if reg != nil {
		entries = reg.Entries()
	}
	return &SameFormatMeta{
		Fields:   exportfmt.ProposeMetadata(fields, cfg),
		Filename: engine.SameFormatFileName(name, ext, entries, docIndex),
	}, nil
}

// SaveSameFormat writes the same-format copy with the REVIEWED metadata
// values applied (reject = the original value comes back unchanged) and
// the reviewed filename as the dialog default. The body rewrite is
// identical to SaveDocument's native path.
func (a *App) SaveSameFormat(name, ext string, reviewed []exportfmt.MetaField, filename string) error {
	var data []byte
	var err error
	if ext == "pdf" {
		// EXPERIMENTAL regenerated PDF (BUILD-02 Phase 13): built from
		// the anonymised working text with the reviewed metadata; the
		// exporter runs a leak self-check before returning bytes.
		rd, ferr := a.findResultDoc(name)
		if ferr != nil {
			return ferr
		}
		cfg, _, cerr := a.sameFormatConfig(name)
		if cerr != nil {
			return cerr
		}
		data, err = exportfmt.ExportPDF(rd.Anonymised, reviewed, cfg)
	} else {
		data, err = a.sameFormatBytes(name, ext)
		if err == nil && len(reviewed) > 0 {
			// Apply the reviewed properties; RewriteMetadata leaves
			// everything not listed byte-identical.
			data, err = exportfmt.RewriteMetadata(data, reviewed)
		}
	}
	if err != nil {
		return err
	}
	if filename == "" {
		filename = engine.ExportFileName(name, ext)
	}
	return a.saveWithDialog(filename, "."+ext, "*."+ext, data)
}

// sameFormatConfig assembles the exportfmt.Config from the last run's
// inputs (shared by the body rewrite and the metadata proposals).
func (a *App) sameFormatConfig(name string) (exportfmt.Config, *engine.Document, error) {
	a.mu.Lock()
	var src *engine.Document
	for i := range a.docs {
		if a.docs[i].Name == name {
			src = &a.docs[i]
			break
		}
	}
	req := a.lastReq
	reg := a.registry
	settings := a.settings
	a.mu.Unlock()

	if src == nil {
		return exportfmt.Config{}, nil, fmt.Errorf("document %q is no longer imported; re-import it and re-run the pipeline", name)
	}
	if req == nil || reg == nil {
		return exportfmt.Config{}, nil, fmt.Errorf("no run inputs available yet; run the pipeline first, then export the same-format copy")
	}
	allow := engine.NewEmptyAllowlist()
	for _, t := range req.AllowTerms {
		allow.Add(t)
	}
	categories := req.Categories
	if categories == nil {
		categories = settings.Categories
	}
	return exportfmt.Config{
		Entities:   req.Entities,
		Patterns:   req.Patterns,
		Categories: categories,
		Level:      engine.Level(settings.Level),
		Allowlist:  allow,
		Registry:   reg,
	}, src, nil
}

// appendReportWarning adds a warning to the latest report (results view
// and report export both show it).
func (a *App) appendReportWarning(msg string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.results != nil {
		a.results.Report.Warnings = append(a.results.Report.Warnings, msg)
	}
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

// ChooseExportFolder opens the native directory picker and returns the folder
// the user chose, or "" when they cancelled (BUILD-05 Phase 3).
//
// It ONLY picks a folder; nothing is written here. The chosen path is
// remembered by the frontend rather than stored as a Go setting, because it is
// a convenience for one batch and does not belong in a session file next to the
// re-identification key.
//
// The folder drives the ZIP export and nothing else (decision 4). Single-file
// saves, the key, the report and the session all keep their own save dialogs, so
// no click can put something key-bearing on disk without a dialog naming the
// exact file.
//
// @return the absolute folder path, or "" when cancelled
func (a *App) ChooseExportFolder() (string, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose where to save the anonymised batch",
	})
	if err != nil {
		return "", fmt.Errorf("the folder picker could not be opened (%v), try again; "+
			"if it keeps failing, restart the application", err)
	}
	return dir, nil
}

// ExportAllZipTo writes the batch zip straight into a folder the user already
// chose, with no second dialog (BUILD-05 Phase 3).
//
// This is the ONLY method that writes without a dialog, and it is allowed to
// because the user picked the destination explicitly through
// ChooseExportFolder and the zip contains no re-identification key: it is the
// anonymised documents and nothing else.
//
// Refusing to overwrite is deliberate. A user who exports twice has almost
// certainly changed something in between, and silently replacing the first
// archive would destroy the copy they may still need. Numbering the second one
// keeps both.
//
// @param dir the folder chosen through ChooseExportFolder
// @return the full path written, so the UI can name it in the notice
func (a *App) ExportAllZipTo(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("no destination folder was chosen: pick one with Browse first, " +
			"or use a single-file save button, which asks for a location each time")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("the destination folder %q cannot be reached (%v): "+
			"check that it still exists and that you are connected to it if it is a network share",
			dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("the destination %q is a file, not a folder: "+
			"pick a folder with Browse", dir)
	}

	a.mu.Lock()
	results := a.results
	n := 0
	if results != nil {
		n = len(results.Documents)
	}
	a.mu.Unlock()

	data, err := engine.BuildExportZip(results)
	if err != nil {
		return "", err
	}

	path := filepath.Join(dir, fmt.Sprintf("%d_anonymised.zip", n))
	path, err = freePath(path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("could not write %q: %v, check that the folder is writable "+
			"(not a read-only network share) and that you have room on the disk", path, err)
	}
	return path, nil
}

// freePath returns a path that does not exist yet, appending " (2)", " (3)" and
// so on before the extension when it has to.
//
// The loop is bounded because an unbounded one on a folder full of archives
// would hang the export with no way to tell what it was doing. Fifty is far
// past any plausible number of repeat exports of one batch.
func freePath(path string) (string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path, nil
	}
	ext := filepath.Ext(path)
	stem := path[:len(path)-len(ext)]
	for i := 2; i <= 50; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%q already exists, and so do fifty numbered variants of it: "+
		"choose a different destination folder, or clear the old archives out of this one", path)
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
	smartDetect := settings.SmartDetect
	var registry []engine.MappingEntry
	var overrides map[string]string
	if a.registry != nil {
		registry = a.registry.Export()
		// The placeholders the user renamed (BUILD-05 Phase 3). The renamed
		// values are already inside `registry`; this records which of them were
		// deliberate, so re-saving a reloaded session does not demote them.
		overrides = a.registry.Overrides()
	}
	a.mu.Unlock()

	data, err := engine.SaveSession(engine.Session{
		Entities:    req.Entities,
		AllowTerms:  req.AllowTerms,
		Patterns:    req.Patterns,
		SimpleRules: req.SimpleRules,
		Settings: engine.SessionSettings{
			Level:         settings.Level,
			Categories:    settings.Categories,
			OllamaPort:    settings.OllamaPort,
			Model:         settings.Model,
			ContextSize:   settings.ContextSize,
			UseAI:         settings.UseAI,
			MinConfidence: settings.MinConfidence,
			SmartDetect:   &smartDetect,
		},
		Registry:             registry,
		PlaceholderOverrides: overrides,
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
	// Restore the registry AND which placeholders the user had renamed
	// (BUILD-05 Phase 3). An override that no longer applies is reported as a
	// report warning rather than failing the load: one stale entry must not
	// cost the user the other twenty.
	registry, overrideFailures := engine.NewRegistryFromSession(session)
	a.registry = registry
	restored := Settings{
		Level:       session.Settings.Level,
		Categories:  session.Settings.Categories,
		OllamaPort:  session.Settings.OllamaPort,
		Model:       session.Settings.Model,
		ContextSize: session.Settings.ContextSize,
		UseAI:       session.Settings.UseAI,
	}
	// An omitted optional block means "keep the current setting" rather than
	// silently resetting it. This is NOT version compatibility (a file from
	// another version is refused outright, BUILD-05 decision 1): it is a file
	// this version wrote that simply had nothing to say about these fields.
	if restored.Categories == nil {
		restored.Categories = a.settings.Categories
	}
	if restored.ContextSize == 0 {
		restored.ContextSize = a.settings.ContextSize
	}
	a.settings = restored
	a.mu.Unlock()

	for _, failure := range overrideFailures {
		a.appendReportWarning(fmt.Sprintf(
			"a saved placeholder override could not be restored: %v", failure))
	}
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
