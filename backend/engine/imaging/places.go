// engine/imaging/places.go — where one part's pictures sit in its bytes.
//
// The scan answers "what pictures are in this document" and hands the interface
// an inventory whose occurrences are identified by PART plus ORDINAL, never by a
// byte offset. That is deliberate: an export rewrites the text of a part first
// and the pictures second, so any offset the scan recorded is stale by the time
// the picture pass runs.
//
// This file is how the export asks the question again against the bytes it now
// holds. It goes through the SAME walker the scan used, so the ordinals line up
// by construction rather than by hope: a private walk in the exporter could
// count pictures differently, and the user would then review one picture and
// change another.
package imaging

// PicturePlace is one picture occurrence in one part, with the byte ranges an
// export needs to change it.
type PicturePlace struct {
	// Ordinal matches Occurrence.Ordinal for the same picture in the same part.
	Ordinal int
	// Kind decides what removing this picture can mean: a picture element can be
	// deleted, a shape fill or a slide background cannot.
	Kind Kind
	// ElementStart and ElementEnd bound the whole picture element, from its
	// opening token to just past its close tag. Both are zero when there is no
	// element to delete.
	ElementStart, ElementEnd int
	// SrcRectStart and SrcRectEnd bound the crop rectangle inside this picture's
	// fill. Both are zero when the picture is not cropped.
	SrcRectStart, SrcRectEnd int
}

// HasElement reports whether this occurrence has an element an export may
// delete.
func (p PicturePlace) HasElement() bool {
	return p.Kind == KindPicture && p.ElementEnd > p.ElementStart
}

// HasSrcRect reports whether this occurrence is cropped.
func (p PicturePlace) HasSrcRect() bool {
	return p.SrcRectEnd > p.SrcRectStart
}

// PicturePlaces lists one part's picture occurrences, in the same order and with
// the same ordinals the scan gave them.
//
// @param partName the archive entry, used only in the parse error
// @param data the part's bytes, as they are NOW (after any text rewrite)
// @return one entry per picture occurrence, in ordinal order
func PicturePlaces(partName string, data []byte) ([]PicturePlace, error) {
	occs, _, _, err := walkPart(partName, data)
	if err != nil {
		return nil, err
	}
	places := make([]PicturePlace, 0, len(occs))
	for _, occ := range occs {
		places = append(places, PicturePlace{
			Ordinal:      occ.ordinal,
			Kind:         occ.kind,
			ElementStart: occ.elemStart,
			ElementEnd:   occ.elemEnd,
			SrcRectStart: occ.srcRectStart,
			SrcRectEnd:   occ.srcRectEnd,
		})
	}
	return places, nil
}
