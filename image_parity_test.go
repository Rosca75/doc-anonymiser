// image_parity_test.go — the JavaScript to Go parity guards for the PICTURE
// vocabularies the two sides SHARE.
//
// The picture review is the one surface where the frontend has to MIRROR an
// engine rule rather than merely render an engine answer: the treatment panel
// disables a control that imaging.Decision.Validate would refuse, so the user
// never presses a button whose refusal arrives at export time. A mirror that
// drifts fails in one of two silent ways, and both are worse than an error:
//
//	TREATMENTS  a treatment only the frontend knows is a control the user picks
//	            and SetImageDecision then rejects; one only Go knows is a
//	            redaction nobody can ask for. The ORDER is display order, and it
//	            is also the order the panel picks a default from.
//	FORMATS     the format decides which treatments are on offer, so a format
//	            the frontend cannot name renders a row whose disable rules it
//	            guesses at.
//	KINDS       what encloses a picture decides what removing it means, and the
//	            review says so; a kind with no word renders nothing at all,
//	            which reads as "this is a plain picture".
//
// Every guard also checks that copy.js has a WORD for each identifier, for the
// reason detection_parity_test.go does: a cell with no label renders its raw
// identifier, or renders empty, and both are the unexplained jargon the copy
// rules forbid.
//
// The reason and warning CODES are held to the same standard. Go answers with a
// code rather than a sentence (copy lives in copy.js), so a code the frontend has
// no sentence for is a document that reports nothing about why it has no image
// review, and the user is left with an empty tab and no answer.
//
// It reads frontend/state.js and frontend/copy.js through the helpers
// detection_parity_test.go already owns (frontendList, assertLabelled), because
// two parsers for one file is one parser nobody updates.
package main

import (
	"strings"
	"testing"

	"doc-anonymiser/backend/engine/imaging"
)

// treatmentIdentifiers is imaging.AllTreatments as plain strings, which is what
// the shared helpers compare against.
func treatmentIdentifiers() []string {
	out := make([]string, 0, len(imaging.AllTreatments))
	for _, t := range imaging.AllTreatments {
		out = append(out, string(t))
	}
	return out
}

// formatIdentifiers is imaging.AllFormats as plain strings.
func formatIdentifiers() []string {
	out := make([]string, 0, len(imaging.AllFormats))
	for _, f := range imaging.AllFormats {
		out = append(out, string(f))
	}
	return out
}

// kindIdentifiers is imaging.AllKinds as plain strings.
func kindIdentifiers() []string {
	out := make([]string, 0, len(imaging.AllKinds))
	for _, k := range imaging.AllKinds {
		out = append(out, string(k))
	}
	return out
}

// TestImageTreatmentsAgreeAcrossTheBridge: the same treatments in the same
// order on both sides.
func TestImageTreatmentsAgreeAcrossTheBridge(t *testing.T) {
	js := frontendList(t, "IMAGE_TREATMENTS")
	want := treatmentIdentifiers()
	if strings.Join(js, ",") != strings.Join(want, ",") {
		t.Errorf("IMAGE_TREATMENTS in frontend/state.js is %v, imaging.AllTreatments is %v.\n"+
			"A treatment only the frontend offers is refused by SetImageDecision the moment it is\n"+
			"applied; one only Go applies is a redaction the user cannot ask for. The order is the\n"+
			"order the treatment panel offers them in, and the order it picks a default from, so it\n"+
			"has to match too.", js, want)
	}
}

// TestImageFormatsAgreeAcrossTheBridge: the same formats in the same order.
func TestImageFormatsAgreeAcrossTheBridge(t *testing.T) {
	js := frontendList(t, "IMAGE_FORMATS")
	want := formatIdentifiers()
	if strings.Join(js, ",") != strings.Join(want, ",") {
		t.Errorf("IMAGE_FORMATS in frontend/state.js is %v, imaging.AllFormats is %v.\n"+
			"The format decides which treatments a picture can carry, so a format the frontend\n"+
			"cannot name is a row whose disabled controls it is guessing at.", js, want)
	}
}

// TestImageKindsAgreeAcrossTheBridge: the same occurrence kinds in the same
// order.
func TestImageKindsAgreeAcrossTheBridge(t *testing.T) {
	js := frontendList(t, "IMAGE_KINDS")
	want := kindIdentifiers()
	if strings.Join(js, ",") != strings.Join(want, ",") {
		t.Errorf("IMAGE_KINDS in frontend/state.js is %v, imaging.AllKinds is %v.\n"+
			"What encloses a picture decides what removing it means, and the review says so: a kind\n"+
			"only Go knows renders as nothing, which reads as \"this is a plain picture\".", js, want)
	}
}

// TestEveryImageTreatmentHasALabel: the panel's chips and the Status column both
// name a treatment, and they read two different tables to do it.
func TestEveryImageTreatmentHasALabel(t *testing.T) {
	identifiers := treatmentIdentifiers()
	assertLabelled(t, "treatmentLabel", identifiers,
		"The treatment panel builds one chip per treatment from the identifier list, so an\n"+
			"unlabelled treatment renders a chip named after a JSON key.")
	assertLabelled(t, "statusLabel", identifiers,
		"The Status column and the tile chip name the DECISION, so an unlabelled treatment\n"+
			"makes a boxed picture read as kept.")
}

// TestEveryImageFormatHasALabel: the Format column names what the bytes turned
// out to be.
func TestEveryImageFormatHasALabel(t *testing.T) {
	assertLabelled(t, "formatLabel", formatIdentifiers(),
		"The Format column reads this table, and a format with no word in it renders its own\n"+
			"identifier in a cell four characters wide.")
}

// TestEveryImageKindHasALabel: the Location tooltip names what encloses the
// picture, and only for the kinds that are not a plain picture element, so an
// unlabelled kind is silently dropped rather than shown wrong.
func TestEveryImageKindHasALabel(t *testing.T) {
	assertLabelled(t, "kindLabel", kindIdentifiers(),
		"The review names what encloses a picture, because removing a background or a shape\n"+
			"fill has no element to delete and means overwriting the bytes. An unlabelled kind is\n"+
			"dropped, so the reader is told nothing.")
}

// TestEveryImageReasonCodeHasCopy: "this document has no image review" is an
// ANSWER, and the answer is a sentence the frontend owns.
func TestEveryImageReasonCodeHasCopy(t *testing.T) {
	assertLabelled(t, "reason",
		[]string{imaging.ReasonPDFImagesRemoved, imaging.ReasonFormatNotSupported},
		"Go answers with a CODE and copy.js owns the sentence. A code with no sentence renders\n"+
			"an empty tab, which is the question the sentence exists to answer.")
}

// TestEveryImageWarningCodeHasCopy: the same for the per-document notes.
func TestEveryImageWarningCodeHasCopy(t *testing.T) {
	assertLabelled(t, "warning",
		[]string{imaging.WarnUnreadablePart, imaging.WarnLinkedImages},
		"A warning code with no sentence prints nothing, so a document with an unreadable\n"+
			"picture part looks exactly like one without.")
}

// TestImageBlockedReasonsAreLabelled holds the frontend's OWN reason codes to
// the copy table.
//
// state.js treatmentBlockedReason answers with a code so the sentence stays in
// copy.js, exactly as Go's codes do. These three are the frontend's mirror of
// imaging.Decision.Validate's three refusals, and a code with no sentence
// disables a chip with an empty tooltip, which is a control that says no and
// will not say why.
func TestImageBlockedReasonsAreLabelled(t *testing.T) {
	assertLabelled(t, "blocked", []string{"linked", "format", "svg_blur"},
		"A disabled treatment chip explains itself through this table. Without a sentence the\n"+
			"chip is greyed out and mute, and the user has no way to learn what to do instead.")
}
