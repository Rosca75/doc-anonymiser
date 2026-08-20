// app_export.go — bound methods for the Export screen: per-file
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
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/engine/exportfmt"
	"doc-anonymiser/backend/engine/imaging"
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
// same-format rewriter; text extensions through
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
		data, extras, _, _, err := exportfmt.ExportDocx(src.Raw, cfg)
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
		data, _, _, err := exportfmt.ExportPptx(src.Raw, cfg)
		return data, err
	case "xlsx":
		data, _, err := exportfmt.ExportXlsx(src.Raw, cfg)
		return data, err
	default:
		return nil, fmt.Errorf("same-format export does not support .%s", ext)
	}
}

// SameFormatMeta is the review payload for one document's same-format
// export: every document property with its proposed
// replacement, plus the proposed anonymised filename.
type SameFormatMeta struct {
	Fields   []exportfmt.MetaProposal `json:"fields"`
	Filename string                   `json:"filename"`
	// Images is what this save will do to the document's pictures, and it is
	// ABSENT for a format with no image review or a document with no pictures,
	// so the review panel says nothing at all rather than "0 images".
	//
	// It travels with the properties because this is the call the review panel
	// already makes, and the panel is the last surface before the file is
	// written: a user who never opened the IMAGE tab is otherwise never told
	// that the pictures are going out exactly as they came in.
	Images *imaging.Summary `json:"images,omitempty"`
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
	meta := &SameFormatMeta{
		Fields:   exportfmt.ProposeMetadata(fields, cfg),
		Filename: engine.SameFormatFileName(name, ext, entries, docIndex),
	}
	if summary, ok := a.imageSummaryFor(name); ok {
		meta.Images = &summary
	}
	return meta, nil
}

// SaveSameFormat writes the same-format copy with the REVIEWED metadata
// values applied (reject = the original value comes back unchanged) and
// the reviewed filename as the dialog default. The body rewrite is
// identical to SaveDocument's native path.
func (a *App) SaveSameFormat(name, ext string, reviewed []exportfmt.MetaField, filename string) error {
	var data []byte
	var err error
	if ext == "pdf" {
		// EXPERIMENTAL regenerated PDF: built from
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
	// Through the shared builder, so a value the user removed does not reappear
	// in a same-format copy after the pipeline stopped replacing it
	allow := a.allowlistFor(req.AllowTerms)
	categories := req.Categories
	if categories == nil {
		categories = settings.Categories
	}
	return exportfmt.Config{
		Values:     req.Values,
		Patterns:   req.Patterns,
		Categories: categories,
		Level:      engine.Level(settings.Level),
		Country:    settings.Country,
		Allowlist:  allow,
		Registry:   reg,
		// The picture decisions come from the App and not from the request, for
		// the same reason the removals do: this config is what BOTH export paths
		// build from, and a decision carried in a request would be honoured by
		// one of them and forgotten by the other.
		Images: a.imagePlanFor(name),
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
// the user chose, or "" when they cancelled.
//
// It ONLY picks a folder; nothing is written here. The chosen path is
// remembered by the frontend rather than stored as a Go setting, because it is
// a convenience for one batch and does not belong in a session file next to the
// re-identification key.
//
// The folder drives the ZIP export and nothing else. Single-file
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
// chose, with no second dialog.
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

// maxCopyTextBytes caps CopyText. It is a mis-drag guard, not a product limit:
// the selection panel copies a VALUE out of a preview, and a drag that ran away
// down the pane would otherwise push a whole document through the clipboard.
// Generous for any name, address or account number.
const maxCopyTextBytes = 4096

// CopyText puts an arbitrary short string on the system clipboard.
//
// It exists for the Compare pane's selection panel, where the user copies a
// value out of the preview. Clipboard access goes through Go, as CopyDocument
// already does: the WebView's own clipboard API is not reliably available
// without a user-gesture context the panel's button does not always carry.
//
// @param text the selected text
// @return an actionable error when the selection is empty or too long
func (a *App) CopyText(text string) error {
	if err := validateCopyText(text); err != nil {
		return err
	}
	if err := runtime.ClipboardSetText(a.ctx, text); err != nil {
		return fmt.Errorf("could not write to the clipboard (%v), try again", err)
	}
	return nil
}

// validateCopyText is the guard in front of the clipboard write, split out so
// it can be tested: the Wails runtime refuses a context it was not given by a
// lifecycle hook, and there is no such context in a headless test.
func validateCopyText(text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("there is nothing to copy, select some text in one of the previews first")
	}
	if len(text) > maxCopyTextBytes {
		return fmt.Errorf(
			"that selection is %d characters, which is too long to copy: select a single value rather than a passage",
			len(text))
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

// reportPayload is what the JSON report export writes: the engine's own report
// with the picture sections beside it.
//
// The engine's Report is EMBEDDED rather than nested under a key, so every field
// a reader of an earlier report knows stays exactly where it was and the picture
// sections are simply one more key at the top level.
type reportPayload struct {
	*engine.Report
	// Images is one section per document that has pictures, absent for a batch
	// of text files.
	Images []DocumentImageReport `json:"images,omitempty"`
}

// reportBytes composes the exported report and names the file it belongs in.
//
// It is separate from ExportReport for the reason validateCopyText is separate
// from CopyText: the save goes through the Wails dialog, which refuses a context
// no lifecycle hook gave it, so the part worth testing has to be reachable
// without one.
//
// @param format "json" or "md"
// @return the file's bytes, the default filename, and an actionable error
func (a *App) reportBytes(format string) ([]byte, string, error) {
	a.mu.Lock()
	results := a.results
	a.mu.Unlock()
	if results == nil {
		return nil, "", fmt.Errorf("no report yet, run the pipeline first")
	}

	// In the run's own document order, so the picture sections read down the
	// report in the same order as the per-document table above them.
	names := make([]string, 0, len(results.Documents))
	for _, rd := range results.Documents {
		names = append(names, rd.Name)
	}
	images := a.imageReports(names)

	switch format {
	case "json":
		if len(images) == 0 {
			// No document had a picture, so the file is the engine's report and
			// nothing else, byte for byte as it always was. The engine's own
			// marshaller stays the one that writes it, rather than a wrapper
			// that would have to be trusted to produce the same bytes.
			data, err := results.Report.ToJSON()
			if err != nil {
				return nil, "", err
			}
			return data, "anonymisation_report.json", nil
		}
		data, err := jsonMarshalIndent(reportPayload{Report: &results.Report, Images: images})
		if err != nil {
			return nil, "", err
		}
		return data, "anonymisation_report.json", nil
	case "md":
		return []byte(results.Report.ToMarkdown() + imageReportMarkdown(images)),
			"anonymisation_report.md", nil
	default:
		return nil, "", fmt.Errorf("unknown report format %q, expected json or md", format)
	}
}

// ExportReport saves the run report as JSON or human-readable markdown,
// INCLUDING what the picture decisions do on export: a report that described a
// document as anonymised without mentioning its pictures would be read as
// covering them.
func (a *App) ExportReport(format string) error {
	data, filename, err := a.reportBytes(format)
	if err != nil {
		return err
	}
	if format == "json" {
		return a.saveWithDialog(filename, "JSON", "*.json", data)
	}
	return a.saveWithDialog(filename, "Markdown", "*.md", data)
}

// SaveSessionToFile persists entities + allowlist + patterns + rules +
// settings + registry to a .anonsession.json. The UI shows the
// re-identification-key warning before calling (CLAUDE.md §5: sensitive
// state leaves memory only on explicit user action).
func (a *App) SaveSessionToFile(req RunRequest) error {
	a.mu.Lock()
	settings := a.settings
	heuristicDiscovery := settings.HeuristicDiscovery
	removed := make([]engine.RemovedValue, len(a.removed))
	copy(removed, a.removed)
	var registry []engine.MappingEntry
	var overrides map[string]string
	// retired holds numbers this session has spent that no entry holds, so they
	// cannot be recovered from `registry` on load. Losing them hands the same
	// number out twice, and the re-identification key stops being reversible
	// with nothing able to notice.
	var retired []string
	if a.registry != nil {
		registry = a.registry.Export()
		// The placeholders the user renamed. The renamed
		// values are already inside `registry`; this records which of them were
		// deliberate, so re-saving a reloaded session does not demote them.
		overrides = a.registry.Overrides()
		retired = a.registry.Retired()
	}
	a.mu.Unlock()

	data, err := engine.SaveSession(engine.Session{
		Values:     req.Values,
		AllowTerms: req.AllowTerms,
		Patterns:   req.Patterns,
		Settings: engine.SessionSettings{
			Level:                   settings.Level,
			Categories:              settings.Categories,
			OllamaPort:              settings.OllamaPort,
			Model:                   settings.Model,
			ContextSize:             settings.ContextSize,
			Country:                 settings.Country,
			UseLocalAI:              settings.UseLocalAI,
			AIStrictFormat:          settings.AIStrictFormat,
			AIDetailLevel:           settings.AIDetailLevel,
			UseBuiltInPatterns:      &settings.UseBuiltInPatterns,
			UseHeuristicDiscovery:   &settings.UseHeuristicDiscovery,
			SignalSuggestionSources: engine.NormaliseSignalSources(settings.SignalSuggestionSources),
			MinConfidence:           settings.MinConfidence,
			HeuristicDiscovery:      &heuristicDiscovery,
		},
		Registry:             registry,
		PlaceholderOverrides: overrides,
		RemovedValues:        removed,
		RetiredPlaceholders:  retired,
		// The picture decisions travel with the session, or a restored session
		// would export the client logo the user had boxed.
		ImageDecisions: a.imageDecisionsSnapshot(),
		// The defined terms travel too, because the user may have deleted
		// individual entries: re-deriving them on load would bring them back.
		DefinedTerms: a.definedTermsSnapshot(),
	})
	if err != nil {
		return err
	}
	return a.saveWithDialog("session.anonsession.json", "Session", "*.anonsession.json;*.json", data)
}

// LoadSessionFromFile opens a session file, restores the Go-side state
// (registry + settings) and returns the session so the frontend can
// restore its own state (entities, allowlist, patterns).
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

	overrideFailures, err := a.applyRestoredSession(session)
	if err != nil {
		return nil, err
	}

	for _, failure := range overrideFailures {
		a.appendReportWarning(fmt.Sprintf(
			"a saved placeholder override could not be restored: %v", failure))
	}
	return &session, nil
}

// applyRestoredSession installs a loaded session's registry, removals and
// settings, and reports the placeholder overrides that could not be restored.
//
// VALIDATION HAPPENS FIRST, and nothing is installed until all of it passes
// The previous order swapped the registry and the settings
// in and validated afterwards, so a file this application refused still left the
// App holding that file's registry, behind an error message the UI reads as
// "nothing was loaded". The next export would then have written the rejected
// file's re-identification key.
//
// EVERY persisted setting is restored. The confidence floor and the smart
// detection tuning used to be missing from this literal, so loading a session
// silently reset the floor to 0 and the tuning to the shipped defaults: two
// settings that decide what gets replaced, quietly changed by an action the
// user reads as "restore what I saved".
//
// @param session a session LoadSession has already accepted
// @return one error per override that did not apply (warnings, not failures),
//
//	and a fatal error that leaves the App exactly as it was
func (a *App) applyRestoredSession(session engine.Session) ([]error, error) {
	// 1. Build, do not install. Restore the registry AND which placeholders the
	//    user had renamed. An override that no longer applies
	//    is reported as a report warning rather than failing the load: one stale
	//    entry must not cost the user the other twenty. A corrupt key is fatal.
	registry, overrideFailures, err := engine.NewRegistryFromSession(session)
	if err != nil {
		return nil, err
	}

	restored := a.restoredSettings(session)

	// 2. Validate. ApplySettings checks every field and only then stores, so a
	//    rejected file stops here with the App untouched.
	if _, err := a.ApplySettings(restored); err != nil {
		return nil, fmt.Errorf(
			"this session file could not be loaded: %v. Nothing was changed, "+
				"the session you had open is still open", err)
	}

	// 3. Install the rest.
	a.mu.Lock()
	a.registry = registry
	// The values the user removed. They restore with the session, or
	// every one of them silently comes back on the next run.
	a.removed = append([]engine.RemovedValue(nil), session.RemovedValues...)
	a.definedTerms = append([]engine.DefinedTerm(nil), session.DefinedTerms...)
	// The picture decisions, restored for the same reason: without them a
	// reloaded session exports every picture as it came in, silently, while the
	// screen the user saved from said they were anonymised.
	a.imageDecisions = restoredImageDecisions(session.ImageDecisions)
	a.mu.Unlock()

	return overrideFailures, nil
}

// restoredSettings computes the Settings a loaded session asks for, without
// installing them. Pure apart from reading the current settings for the fields
// the file had nothing to say about.
func (a *App) restoredSettings(session engine.Session) Settings {
	a.mu.Lock()
	defer a.mu.Unlock()

	restored := Settings{
		Level:                 session.Settings.Level,
		Categories:            session.Settings.Categories,
		OllamaPort:            session.Settings.OllamaPort,
		Model:                 session.Settings.Model,
		ContextSize:           session.Settings.ContextSize,
		Country:               session.Settings.Country,
		UseLocalAI:            session.Settings.UseLocalAI,
		AIStrictFormat:        session.Settings.AIStrictFormat, // absent means "off": that is the default
		AIDetailLevel:         session.Settings.AIDetailLevel,  // absent means thorough, filled in below
		UseBuiltInPatterns:    true,                            // absent means "on": that is the default
		UseHeuristicDiscovery: true,                            // absent means "on": that is the default
		// A missing key falls back to the default rather than to "off", so a file
		// that says nothing about a source cannot silently disable it.
		SignalSuggestionSources: engine.NormaliseSignalSources(session.Settings.SignalSuggestionSources),
		MinConfidence:           session.Settings.MinConfidence,
		HeuristicDiscovery:      a.settings.HeuristicDiscovery,
	}
	if session.Settings.UseBuiltInPatterns != nil {
		restored.UseBuiltInPatterns = *session.Settings.UseBuiltInPatterns
	}
	if session.Settings.UseHeuristicDiscovery != nil {
		restored.UseHeuristicDiscovery = *session.Settings.UseHeuristicDiscovery
	}
	if session.Settings.HeuristicDiscovery != nil {
		restored.HeuristicDiscovery = *session.Settings.HeuristicDiscovery
	}
	// An omitted optional block means "keep the current setting" rather than
	// silently resetting it. This is NOT version compatibility (a file from
	// another version is refused outright): it is a file
	// this version wrote that simply had nothing to say about these fields.
	if restored.Categories == nil {
		restored.Categories = a.settings.Categories
	}
	if restored.ContextSize == 0 {
		restored.ContextSize = a.settings.ContextSize
	}
	if restored.Country == "" {
		restored.Country = a.settings.Country
	}
	// The detail level is the exception to "keep the current setting": a file
	// that does not name one was written under THOROUGH, so thorough is what it
	// describes. Carrying the live choice over instead would let loading an old
	// session restore a scan the file never recorded.
	if restored.AIDetailLevel == "" {
		restored.AIDetailLevel = engine.DetailThorough
	}
	return restored
}

// jsonMarshalIndent is a tiny wrapper so the import list stays tidy above.
func jsonMarshalIndent(v interface{}) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("could not serialise to JSON: %w", err)
	}
	return data, nil
}
