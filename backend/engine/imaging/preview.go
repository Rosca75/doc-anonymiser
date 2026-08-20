// engine/imaging/preview.go — one asset's bytes, and the preview built from
// them.
//
// The bound layer must stay a thin adapter (backend/CLAUDE.md), so the two
// questions "where are this asset's bytes" and "what does the interface put in
// an <img src>" are answered here, in the engine, from bytes alone. The App
// only picks the document and passes the bytes through.
package imaging

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
)

// AssetBytes reads one picture part out of a document archive.
//
// @param raw the whole file, as captured at import (Document.Raw)
// @param partName the asset ID, which IS the archive part path
// @return the part's decompressed bytes
func AssetBytes(raw []byte, partName string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("this document could not be opened as a zip archive (%v); "+
			"re-export it from the source application and import it again", err)
	}
	return readEntry(indexEntries(zr), partName)
}

// Preview builds what the review screen puts in an <img src> for one asset.
//
// A raster asset is thumbnailed, because a full-size screenshot per row is how
// a desktop application starts swapping. An SVG asset is handed over AS IT IS,
// because it is already small and because rasterising vector graphics is not
// something the standard library can do. That comes with the hard rule this
// package's header states: the interface renders it through an <img> tag and
// never by inlining the element into the page.
//
// @param raw the whole document, as captured at import
// @param a the asset to preview, as the scan listed it
// @param maxPx the longest side wanted for a raster preview
// @return a data URL ready for an <img src>, and the size it will draw at
func Preview(raw []byte, a Asset, maxPx int) (dataURL string, w, h int, err error) {
	if a.Linked {
		return "", 0, 0, fmt.Errorf(
			"this picture is linked from outside the document, so there is nothing here to "+
				"preview; open %q in its own application to see it", a.ID)
	}

	// An SVG asset's ID is the PNG fallback Office wrote beside it, because that
	// is what the relationship points at. The SVG itself is what the user sees,
	// so it is what the preview shows.
	part := a.ID
	if a.Format == FormatSVG && a.Companion != "" {
		part = a.Companion
	}
	data, err := AssetBytes(raw, part)
	if err != nil {
		return "", 0, 0, err
	}

	if a.Format == FormatSVG {
		mime, err := MIMEType(FormatSVG)
		if err != nil {
			return "", 0, 0, err
		}
		return dataURLOf(mime, data), a.Width, a.Height, nil
	}

	// A raster preview is always PNG, whatever the source encoding was: a
	// preview re-encoded as JPEG adds compression artefacts to the very picture
	// the reviewer is being asked to judge.
	thumb, tw, th, err := Thumbnail(data, maxPx)
	if err != nil {
		return "", 0, 0, err
	}
	mime, err := MIMEType(FormatPNG)
	if err != nil {
		return "", 0, 0, err
	}
	return dataURLOf(mime, thumb), tw, th, nil
}

// PreviewTreated builds what the review screen shows for one asset under one
// decision: the REAL treatment, thumbnailed.
//
// It runs Treat and then Thumbnail rather than drawing its own approximation, so
// the preview cannot promise something the export does not do. That is the whole
// point of it being here: a second drawing of the same idea would drift, and the
// user would approve a redaction they never saw.
//
// A keep previews the picture as it is, which is exactly what keeping produces.
// A remove previews the transparent pixel the export writes, which is exactly
// what removing produces: nothing.
//
// @param raw the whole document, as captured at import
// @param a the asset to preview, as the scan listed it
// @param d the decision to preview
// @param maxPx the longest side wanted for a raster preview
// @return a data URL ready for an <img src>, and the size it will draw at
func PreviewTreated(raw []byte, a Asset, d Decision, maxPx int) (dataURL string, w, h int, err error) {
	if !d.Anonymises() {
		return Preview(raw, a, maxPx)
	}
	if a.Linked {
		return "", 0, 0, fmt.Errorf(
			"this picture is linked from outside the document, so there is nothing here to "+
				"preview; it can be removed from the document, which needs no preview, or kept as "+
				"it is (%q)", a.ID)
	}

	// An SVG asset is two parts, and the SVG is the one the user sees, so it is
	// the one previewed. The PNG fallback beside it gets the same treatment at
	// export time.
	part := a.ID
	if a.Format == FormatSVG && a.Companion != "" {
		part = a.Companion
	}
	source, err := AssetBytes(raw, part)
	if err != nil {
		return "", 0, 0, err
	}
	treated, err := Treat(source, a, d)
	if err != nil {
		return "", 0, 0, err
	}

	// The treatment answers in the SOURCE's encoding, so what the preview is
	// looking at is decided by the bytes that came back and not by the asset's
	// reported format.
	switch Sniff(treated) {
	case FormatSVG:
		mime, mimeErr := MIMEType(FormatSVG)
		if mimeErr != nil {
			return "", 0, 0, mimeErr
		}
		tw, th, ok := Measure(treated, FormatSVG)
		if !ok {
			tw, th = a.Width, a.Height
		}
		return dataURLOf(mime, treated), tw, th, nil
	default:
		thumb, tw, th, thumbErr := Thumbnail(treated, maxPx)
		if thumbErr != nil {
			return "", 0, 0, thumbErr
		}
		mime, mimeErr := MIMEType(FormatPNG)
		if mimeErr != nil {
			return "", 0, 0, mimeErr
		}
		return dataURLOf(mime, thumb), tw, th, nil
	}
}

// dataURLOf assembles the base64 data URL an <img src> takes.
func dataURLOf(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}
