// engine/imaging/locate.go — where an occurrence sits, in the document's own
// words.
//
// The location is the only thing on the review screen that lets a user go and
// LOOK at the picture, so it is written the way the application that made the
// file talks: "Slide 4", "Page 2", "Header". It is built here rather than in
// the frontend because only the scan knows which part an occurrence came out
// of, and a location the interface reconstructed from a part path would be a
// second answer to the same question.
package imaging

import "fmt"

// The fixed vocabulary of place names. Every location string is one of these,
// optionally with a number, optionally with the hidden suffix.
const (
	locationBody      = "Body"
	locationHeader    = "Header"
	locationFooter    = "Footer"
	locationFootnotes = "Footnotes"
	locationEndnotes  = "Endnotes"
	locationLayout    = "Slide layout"
	locationMaster    = "Slide master"
	// hiddenSuffix marks a picture inside a shape the file marks hidden. A
	// hidden picture is the one a user most needs told about, because it is the
	// one they cannot see by scrolling through the document.
	hiddenSuffix = " (hidden)"
)

// locate names one occurrence's place.
//
// @param part the part it was found in, with its slide number when it has one
// @param occ the occurrence as the walk saw it
// @param pagesKnown whether the part carried any page breaks at all. With none,
// Word never rendered the document and there is no page number to give.
// @param hiddenPart whether the part itself is hidden (a hidden slide)
// @return the ready-to-print location
func locate(part partRef, occ rawOcc, pagesKnown, hiddenPart bool) string {
	base := ""
	switch part.kind {
	case partSlide:
		if hiddenPart {
			base = fmt.Sprintf("Hidden slide %d", part.slide)
		} else {
			base = fmt.Sprintf("Slide %d", part.slide)
		}
	case partLayout:
		base = locationLayout
	case partMaster:
		base = locationMaster
	case partNotes:
		if part.slide > 0 {
			base = fmt.Sprintf("Notes on slide %d", part.slide)
		} else {
			base = "Notes"
		}
	case partHeader:
		base = locationHeader
	case partFooter:
		base = locationFooter
	case partFootnotes:
		base = locationFootnotes
	case partEndnotes:
		base = locationEndnotes
	default:
		// The document body. A page number is given ONLY when Word cached its
		// page breaks; with none cached, every body occurrence is simply in the
		// body. A wrong page number is worse than no page number, because the
		// user is looking at the file while they read it.
		if pagesKnown {
			base = fmt.Sprintf("Page %d", occ.page+1)
		} else {
			base = locationBody
		}
	}
	if occ.hidden {
		base += hiddenSuffix
	}
	return base
}
