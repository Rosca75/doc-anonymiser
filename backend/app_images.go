// app_images.go — the bound methods behind the Anonymise step's IMAGE half.
//
// Method group, following the convention of app_values.go, app_detect.go,
// app_export.go and app_run.go: thin adapters over engine/imaging, no business
// logic. What a picture IS, where it sits and what its preview looks like are
// all decided in the engine; this file picks the document the user named and
// hands its bytes over.
package backend

import (
	"fmt"

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
	a.mu.Lock()
	stored := a.imageDecisions[docName]
	decisions := make(map[string]imaging.Decision, len(stored))
	for id, d := range stored {
		decisions[id] = d
	}
	a.mu.Unlock()

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
