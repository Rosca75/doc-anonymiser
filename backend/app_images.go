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
		return inv, nil
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
	return inv, nil
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

	inv, err := a.ListDocumentImages(docName)
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

	for _, asset := range inv.Assets {
		if asset.ID != assetID {
			continue
		}
		url, w, h, err := imaging.Preview(doc.Raw, asset, maxPx)
		if err != nil {
			return ImageThumb{}, err
		}
		return ImageThumb{DataURL: url, Width: w, Height: h}, nil
	}
	return ImageThumb{}, fmt.Errorf(
		"the picture %q is not part of %q; the document may have been re-imported since the "+
			"list was drawn, reopen the image list to refresh it", assetID, docName)
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
