// app_images.go — the bound methods behind the Anonymise step's IMAGE half, and
// the two places the rest of the application says what happened to the pictures.
//
// Method group, following the convention of app_values.go, app_detect.go,
// app_export.go and app_run.go: thin adapters over engine/imaging, no business
// logic. What a picture IS, where it sits and what its preview looks like are
// all decided in the engine; this file picks the document the user named and
// hands its bytes over.
//
// It also owns the run report's picture section and the export screen's count,
// because both are read off the decision store and the cached inventories that
// live here, and because the ENGINE cannot own either: engine.Run produces the
// report and knows nothing about pictures, which is the invariant that keeps it
// UI-agnostic and testable headless.
package backend

import (
	"fmt"
	"strings"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/engine/exportfmt"
	"doc-anonymiser/backend/engine/imaging"
)

// DefaultThumbnailPx is the preview size the review screen asks for when it
// states none. It is a default and not a limit: the caller passes what its
// layout needs, and the engine clamps it.
const DefaultThumbnailPx = 320

// ImageThumb is one asset's preview, ready for an <img src>.
type ImageThumb struct {
	// DataURL is "data:image/png;base64,..." for a raster asset and
	// "data:image/svg+xml;base64,..." for an SVG one. An SVG is rendered
	// through an <img> tag and never inlined into the page: an <img> context
	// executes no script and an inlined <svg> element does.
	DataURL string `json:"dataUrl"`
	// Width and Height are what the preview will draw at, so the layout can
	// reserve the space before the picture arrives.
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ListDocumentImages returns the image inventory of one IMPORTED document.
//
// It reads the imported document rather than a result document, because the
// pictures live in the bytes captured at import and the user reviews them
// before as well as after a run. Nothing here needs a run to have happened.
//
// A format with no image review is not an error: the answer says so, with a
// reason code the interface turns into its own copy.
//
// @param name the imported document's name, as the import list shows it
// @return the inventory; an error only for an unknown document or a damaged file
func (a *App) ListDocumentImages(name string) (imaging.Inventory, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if inv, ok := a.imageScans[name]; ok {
		return a.withDecisionsLocked(name, inv), nil
	}

	doc, ok := a.docByNameLocked(name)
	if !ok {
		return imaging.Inventory{}, fmt.Errorf(
			"the document %q is not imported, so its pictures cannot be listed; import it again "+
				"or pick another document from the list", name)
	}

	var (
		inv imaging.Inventory
		err error
	)
	switch doc.Format {
	case engine.FormatDOCX:
		inv, err = imaging.ScanDocx(doc.Raw)
	case engine.FormatPPTX:
		inv, err = imaging.ScanPptx(doc.Raw)
	case engine.FormatPDF:
		// The PDF export regenerates the file from the anonymised text with a
		// PDF writer, so a source PDF's pictures are already absent from
		// everything this application produces. Offering a review of them would
		// be a control with nothing behind it.
		inv = imaging.NotApplicable(imaging.ReasonPDFImagesRemoved)
	default:
		inv = imaging.NotApplicable(imaging.ReasonFormatNotSupported)
	}
	if err != nil {
		return imaging.Inventory{}, err
	}

	// The screen asks on every repaint and a sixty-slide deck is not free to
	// re-scan, so the answer is cached per document. The cache is dropped
	// wherever the bytes behind it can change: a new import, a removal, and both
	// resets.
	if a.imageScans == nil {
		a.imageScans = map[string]imaging.Inventory{}
	}
	a.imageScans[name] = inv
	return a.withDecisionsLocked(name, inv), nil
}

// withDecisionsLocked returns the inventory with each asset's current decision
// attached. Caller holds a.mu.
//
// The CACHE holds the scan alone, because the scan describes bytes that do not
// change while the decisions do: caching them together would hand the screen a
// decision the user has since changed. The copy is shallow on purpose, so only
// the asset list is duplicated; nothing here mutates an occurrence.
func (a *App) withDecisionsLocked(name string, inv imaging.Inventory) imaging.Inventory {
	decisions := a.imageDecisions[name]
	out := inv
	out.Assets = make([]imaging.Asset, len(inv.Assets))
	copy(out.Assets, inv.Assets)
	for i := range out.Assets {
		// An absent decision is keep, which is the zero Decision, so an asset
		// nobody has touched needs nothing written into it.
		out.Assets[i].Decision = decisions[out.Assets[i].ID]
	}
	return out
}

// SetImageDecision records one asset's treatment.
//
// It validates against the ASSET it names rather than against the treatment
// alone, so a refusal ("an SVG image cannot be blurred") reaches the user beside
// the control that caused it instead of at export time, when the only thing left
// to do about it is start again.
//
// A keep is recorded as the ABSENCE of a decision, so the store holds only what
// the user changed.
//
// @param docName the imported document's name
// @param assetID the asset ID the inventory listed (the archive part path)
// @param d the decision to record
// @return an error for an unknown document, an unknown picture, or a decision
//
//	this picture cannot carry
func (a *App) SetImageDecision(docName, assetID string, d imaging.Decision) error {
	asset, err := a.imageAsset(docName, assetID)
	if err != nil {
		return err
	}
	if err := d.Validate(asset); err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if !d.Anonymises() {
		delete(a.imageDecisions[docName], assetID)
		if len(a.imageDecisions[docName]) == 0 {
			delete(a.imageDecisions, docName)
		}
		return nil
	}
	if a.imageDecisions == nil {
		a.imageDecisions = map[string]map[string]imaging.Decision{}
	}
	if a.imageDecisions[docName] == nil {
		a.imageDecisions[docName] = map[string]imaging.Decision{}
	}
	a.imageDecisions[docName][assetID] = d
	return nil
}

// ResetImageDecisions drops every decision for one document: the "keep them
// all" bulk action.
//
// It checks the document exists first, so a stale name is an error the caller
// can act on rather than a silent success that leaves the decisions in place.
//
// @param docName the imported document's name
// @return an error only for a document that is not imported
func (a *App) ResetImageDecisions(docName string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.docByNameLocked(docName); !ok {
		return fmt.Errorf(
			"the document %q is not imported, so its picture decisions cannot be cleared; "+
				"pick another document from the list", docName)
	}
	delete(a.imageDecisions, docName)
	return nil
}

// PreviewImageTreatment renders what the export WILL produce for one asset,
// scaled to a preview.
//
// It runs the REAL treatment and the real thumbnailer, so the preview cannot
// promise something the export does not do. A keep previews the picture as it
// is, which is exactly what keeping it produces.
//
// @param docName the imported document's name
// @param assetID the asset ID the inventory listed
// @param d the decision to preview, which is NOT recorded
// @param maxPx the longest side wanted; 0 asks for DefaultThumbnailPx
// @return the data URL and the size it draws at
func (a *App) PreviewImageTreatment(docName, assetID string, d imaging.Decision, maxPx int) (ImageThumb, error) {
	if maxPx <= 0 {
		maxPx = DefaultThumbnailPx
	}
	asset, err := a.imageAsset(docName, assetID)
	if err != nil {
		return ImageThumb{}, err
	}
	if err := d.Validate(asset); err != nil {
		return ImageThumb{}, err
	}

	a.mu.Lock()
	doc, ok := a.docByNameLocked(docName)
	a.mu.Unlock()
	if !ok {
		return ImageThumb{}, fmt.Errorf(
			"the document %q is not imported, so its pictures cannot be previewed; import it "+
				"again or pick another document from the list", docName)
	}

	url, w, h, err := imaging.PreviewTreated(doc.Raw, asset, d, maxPx)
	if err != nil {
		return ImageThumb{}, err
	}
	return ImageThumb{DataURL: url, Width: w, Height: h}, nil
}

// imageAsset finds one asset of one document, so the three methods that need it
// ask the same question the same way and report the same two failures.
func (a *App) imageAsset(docName, assetID string) (imaging.Asset, error) {
	inv, err := a.ListDocumentImages(docName)
	if err != nil {
		return imaging.Asset{}, err
	}
	for _, asset := range inv.Assets {
		if asset.ID == assetID {
			return asset, nil
		}
	}
	return imaging.Asset{}, fmt.Errorf(
		"the picture %q is not part of %q; the document may have been re-imported since the "+
			"list was drawn, reopen the image list to refresh it", assetID, docName)
}

// imagePlanFor assembles what the exporter needs for one document: the cached
// inventory and the decisions taken against it.
//
// An empty plan is the normal answer and means "change no picture", so a
// document nobody reviewed exports exactly as it did before this pass existed.
// The scan is skipped entirely when there are no decisions, because walking a
// sixty-slide archive to find no work is a cost the user feels on every save.
func (a *App) imagePlanFor(docName string) exportfmt.ImagePlan {
	decisions := a.imageDecisionsFor(docName)
	if len(decisions) == 0 {
		return exportfmt.ImagePlan{}
	}
	inv, err := a.ListDocumentImages(docName)
	if err != nil {
		// The document cannot be scanned, so no decision can be applied to it.
		// The export still runs and still anonymises the text: refusing it here
		// would turn an unreadable picture into a document the user cannot save
		// at all.
		return exportfmt.ImagePlan{}
	}
	return exportfmt.ImagePlan{Inventory: inv, Decisions: decisions}
}

// imageDecisionsSnapshot copies the whole store, for the session file.
func (a *App) imageDecisionsSnapshot() map[string]map[string]imaging.Decision {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.imageDecisions) == 0 {
		return nil
	}
	out := make(map[string]map[string]imaging.Decision, len(a.imageDecisions))
	for doc, byAsset := range a.imageDecisions {
		copied := make(map[string]imaging.Decision, len(byAsset))
		for id, d := range byAsset {
			copied[id] = d
		}
		out[doc] = copied
	}
	return out
}

// ImageThumbnail returns one asset's preview as a data URL.
//
// It is deliberately NOT cached. The previews are the largest thing this
// feature holds, and keeping every one of them for a two-hundred-picture deck
// is how a desktop application starts swapping. The inventory beside it IS
// cached, because it is small and is what the repaint reads.
//
// @param docName the imported document's name
// @param assetID the asset ID the inventory listed (the archive part path)
// @param maxPx the longest side wanted; 0 asks for DefaultThumbnailPx
// @return the data URL and the size it draws at
func (a *App) ImageThumbnail(docName, assetID string, maxPx int) (ImageThumb, error) {
	if maxPx <= 0 {
		maxPx = DefaultThumbnailPx
	}

	asset, err := a.imageAsset(docName, assetID)
	if err != nil {
		return ImageThumb{}, err
	}

	a.mu.Lock()
	doc, ok := a.docByNameLocked(docName)
	a.mu.Unlock()
	if !ok {
		return ImageThumb{}, fmt.Errorf(
			"the document %q is not imported, so its pictures cannot be previewed; import it "+
				"again or pick another document from the list", docName)
	}

	url, w, h, err := imaging.Preview(doc.Raw, asset, maxPx)
	if err != nil {
		return ImageThumb{}, err
	}
	return ImageThumb{DataURL: url, Width: w, Height: h}, nil
}

// docByNameLocked finds one imported document by name. Caller holds a.mu.
func (a *App) docByNameLocked(name string) (engine.Document, bool) {
	for _, doc := range a.docs {
		if doc.Name == name {
			return doc, true
		}
	}
	return engine.Document{}, false
}

// forgetImageScansLocked drops the cached inventories. Caller holds a.mu.
//
// It is called wherever the bytes a scan was made from can change, which is
// every path that adds, replaces or drops a document. A cache that outlives its
// document would answer with the pictures of a file the user has replaced, and
// a decision taken against it would be applied to the wrong picture.
func (a *App) forgetImageScansLocked() {
	a.imageScans = nil
}

// restoredImageDecisions copies a loaded session's decisions into the store's
// own maps, so the App never holds a map the session struct also holds.
//
// A nil or empty field restores as nil, which is "no decisions": exactly what a
// session saved before anyone reviewed a picture meant.
func restoredImageDecisions(saved map[string]map[string]imaging.Decision) map[string]map[string]imaging.Decision {
	if len(saved) == 0 {
		return nil
	}
	out := make(map[string]map[string]imaging.Decision, len(saved))
	for doc, byAsset := range saved {
		if len(byAsset) == 0 {
			continue
		}
		copied := make(map[string]imaging.Decision, len(byAsset))
		for id, d := range byAsset {
			copied[id] = d
		}
		out[doc] = copied
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// --- What the export will do to the pictures, said out loud ----------------
//
// Two surfaces ask that question and both are answered here, from the decision
// store and the cached inventories this file already owns.
//
// The ENGINE cannot answer it. engine.Run produces the report and knows nothing
// about pictures, and it must keep knowing nothing: the engine is UI-agnostic
// and a picture decision is an export-time concern rather than a pipeline pass.
// So the application composes the picture half of the report itself, rather than
// teaching the pipeline about a step it does not perform.

// ImageAssetReport is one picture an export changes: what it is, where it
// appears, and what happens to it.
//
// It names no original VALUE, so a report carrying these is not a
// re-identification key and needs no warning of its own. The box text is
// reported because it is the only part of a treatment that cannot be read off
// the treatment's name: it is the user's own sentence, and a reader checking
// what left the machine needs to see it.
type ImageAssetReport struct {
	// Asset is the archive part path, which is what the decision was keyed on.
	Asset string `json:"asset"`
	// Name is what the picture calls itself, which is what the user recognises.
	Name string `json:"name"`
	// Locations are the places the picture appears, in the document's own words.
	// One decision covers every one of them, so they are all listed.
	Locations []string `json:"locations"`
	// Treatment is box, blur or remove. A kept picture is never listed here.
	Treatment string `json:"treatment"`
	// BoxText is the text drawn into the replacement rectangle, for a box.
	BoxText string `json:"boxText,omitempty"`
}

// DocumentImageReport is one document's picture section.
//
// The anonymised pictures are LISTED and the kept ones are only COUNTED: a list
// of everything left alone is noise, while the count is what lets a reader tell
// "this document had no pictures" from "it had pictures and they all went out as
// they came in".
type DocumentImageReport struct {
	Document   string             `json:"document"`
	Kept       int                `json:"kept"`
	Anonymised []ImageAssetReport `json:"anonymised,omitempty"`
}

// imageDecisionsFor copies one document's decisions out of the store.
//
// The copy is what keeps the callers off the App's own map after the lock is
// released: the exporter and the report both walk what they are given while the
// user may still be changing decisions on screen.
func (a *App) imageDecisionsFor(docName string) map[string]imaging.Decision {
	a.mu.Lock()
	defer a.mu.Unlock()
	stored := a.imageDecisions[docName]
	out := make(map[string]imaging.Decision, len(stored))
	for id, d := range stored {
		out[id] = d
	}
	return out
}

// imageSummaryFor counts what an export of this document would do to its
// pictures.
//
// It is built from the inventory rather than from the ImagePlan, because a plan
// with no decisions is deliberately EMPTY and skips the scan, and "this copy
// keeps all seven of the document's images" is exactly the sentence a document
// with no decisions needs. The counting itself goes through ImagePlan.Summary,
// so the sentence on the screen and the pictures the export actually changes
// cannot disagree.
//
// @param docName the imported document's name
// @return the counts, and false for a format with no image review or a document
//
//	with no pictures, so the caller says nothing at all rather than "0 images"
func (a *App) imageSummaryFor(docName string) (imaging.Summary, bool) {
	inv, err := a.ListDocumentImages(docName)
	if err != nil || !inv.Applicable || len(inv.Assets) == 0 {
		return imaging.Summary{}, false
	}
	plan := exportfmt.ImagePlan{Inventory: inv, Decisions: a.imageDecisionsFor(docName)}
	return plan.Summary(), true
}

// imageReportFor builds one document's picture section.
//
// @param name the document's name, as the run reported it
// @return the section, and false when there is nothing to report: a format with
//
//	no image review, a document with no pictures, or one that no longer scans
func (a *App) imageReportFor(name string) (DocumentImageReport, bool) {
	inv, err := a.ListDocumentImages(name)
	if err != nil || !inv.Applicable || len(inv.Assets) == 0 {
		return DocumentImageReport{}, false
	}
	out := DocumentImageReport{Document: name}
	for _, asset := range inv.Assets {
		// ListDocumentImages attaches each asset's current decision, so the
		// section describes what an export made now would do rather than what
		// some earlier state of the screen would have done.
		if !asset.Decision.Anonymises() {
			out.Kept++
			continue
		}
		row := ImageAssetReport{
			Asset:     asset.ID,
			Name:      asset.Name,
			Locations: assetLocations(asset),
			Treatment: string(asset.Decision.Treatment),
		}
		// Only a box draws the text. A decision that carries text and is then
		// switched to blur still holds the string, and reporting it would
		// describe a rectangle the export never draws.
		if asset.Decision.Treatment == imaging.TreatmentBox {
			row.BoxText = asset.Decision.BoxText
		}
		out.Anonymised = append(out.Anonymised, row)
	}
	return out, true
}

// imageReports builds the picture sections for a whole run, in the run's own
// document order.
//
// Documents with nothing to report are left out entirely rather than listed
// empty: a .txt file has no pictures, and a row saying so on every text
// document would bury the decks that do.
func (a *App) imageReports(names []string) []DocumentImageReport {
	var out []DocumentImageReport
	for _, name := range names {
		if section, ok := a.imageReportFor(name); ok {
			out = append(out, section)
		}
	}
	return out
}

// assetLocations lists where one picture appears, in document order and without
// repeats.
//
// A repeat is possible and meaningless: the same picture used twice on one slide
// is two occurrences with one location, and printing "Slide 4, Slide 4" reads as
// a fault in the report rather than as a fact about the deck.
func assetLocations(asset imaging.Asset) []string {
	out := make([]string, 0, len(asset.Occurrences))
	seen := map[string]bool{}
	for _, occ := range asset.Occurrences {
		if occ.Location == "" || seen[occ.Location] {
			continue
		}
		seen[occ.Location] = true
		out = append(out, occ.Location)
	}
	return out
}

// imageReportMarkdown renders the picture sections for the human-readable
// report.
//
// It returns the EMPTY string when no document had a picture, so a batch of text
// files produces the report it always did rather than a heading over nothing.
func imageReportMarkdown(reports []DocumentImageReport) string {
	if len(reports) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Pictures\n\n")
	b.WriteString("What the picture decisions do on export. The pictures being changed are listed; " +
		"the ones left as they are are counted, so a document with no pictures reads differently " +
		"from one whose pictures all go out untouched.\n")
	for _, doc := range reports {
		fmt.Fprintf(&b, "\n### %s\n\n", doc.Document)
		fmt.Fprintf(&b, "- Kept as they are: %d\n", doc.Kept)
		fmt.Fprintf(&b, "- Anonymised: %d\n", len(doc.Anonymised))
		if len(doc.Anonymised) == 0 {
			continue
		}
		b.WriteString("\n| Picture | Where | Treatment | Box text |\n| --- | --- | --- | --- |\n")
		for _, asset := range doc.Anonymised {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n",
				reportCell(asset.Name), reportCell(strings.Join(asset.Locations, ", ")),
				asset.Treatment, reportCell(asset.BoxText))
		}
	}
	return b.String()
}

// reportCell keeps a picture name or a box text containing a pipe from breaking
// the markdown table it is printed in. Both are strings from a document or from
// the user, so both can contain anything.
func reportCell(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
