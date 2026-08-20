// engine/imaging/decision.go — what the user decided about one picture, and
// the one place the per-format rules live.
//
// A decision is attached to the ASSET and never to an occurrence: a logo used
// on five slides is one question, and answering it per place would need the
// exporter to clone picture parts and rewrite relationships, which is the
// riskiest code this feature could contain.
//
// Validate is the SINGLE home of "may this treatment be applied to this
// picture". The frontend, the bound method and the exporter all ask it, so they
// cannot disagree: a control offered by the interface that the exporter would
// refuse is a control that lies, and a treatment the exporter accepts that the
// interface never offered is a redaction nobody reviewed.
package imaging

import "fmt"

// Treatment is what happens to an asset on export.
type Treatment string

const (
	// TreatmentKeep is the default for every asset: the picture goes out as it
	// came in. It is stored as an ABSENT decision rather than as a value, so
	// "nothing was changed" is one empty map and not one entry per picture.
	TreatmentKeep Treatment = "keep"
	// TreatmentBox replaces the picture with a filled rectangle of the same
	// pixel size, carrying the user's own text.
	TreatmentBox Treatment = "box"
	// TreatmentBlur is mosaic then smooth: the samples are thrown away rather
	// than smeared, because a Gaussian blur is partly invertible and a light
	// blur over text is simply readable.
	TreatmentBlur Treatment = "blur"
	// TreatmentRemove deletes the picture element AND overwrites its bytes. The
	// second half is not optional: an orphan picture part left inside the zip is
	// a leak that LOOKS like a redaction.
	TreatmentRemove Treatment = "remove"
)

// AllTreatments is the ordered list the frontend mirrors, held to the Go side by
// a parity guard exactly as the categories are.
var AllTreatments = []Treatment{TreatmentKeep, TreatmentBox, TreatmentBlur, TreatmentRemove}

// MaxBoxText is the longest box text, in runes. It is a cap and not a
// suggestion: the text is drawn into the picture's own pixel size, and a
// paragraph shrunk to fit a 60px icon is a grey smear that says nothing.
const MaxBoxText = 120

// DefaultBlurStrength is the strength an unstated blur runs at, and the value
// the interface's control starts from.
const DefaultBlurStrength = 5

// MinBlurStrength and MaxBlurStrength bound the dial. The scale is RELATIVE to
// the picture's own size (see mosaicFactor), so the same number means the same
// amount of destruction on a 60px icon and on a 4000px screenshot.
const (
	MinBlurStrength = 1
	MaxBlurStrength = 10
)

// Decision is one asset's answer.
type Decision struct {
	Treatment Treatment `json:"treatment"`
	// BoxText is drawn into the rectangle, centred and wrapped. Empty is
	// allowed and gives a plain rectangle.
	BoxText string `json:"boxText,omitempty"`
	// BlurStrength is MinBlurStrength to MaxBlurStrength. ZERO means "not
	// stated", which reads as DefaultBlurStrength, the way every other absent
	// value in this application reads as its default rather than as "none".
	BlurStrength int `json:"blurStrength,omitempty"`
}

// Anonymises reports whether this decision changes the picture at all.
//
// An EMPTY treatment counts as keep, because an absent decision is how keep is
// stored: the store holds only what the user changed.
func (d Decision) Anonymises() bool {
	return d.Treatment != "" && d.Treatment != TreatmentKeep
}

// Validate answers whether this decision can be applied to this asset.
//
// @param a the asset the decision names, as the scan listed it
// @return nil when the decision is applicable, or an error naming the fix
func (d Decision) Validate(a Asset) error {
	switch d.Treatment {
	case "", TreatmentKeep:
		// Keep carries no parameters, so nothing else can be wrong with it.
		return nil
	case TreatmentBox, TreatmentBlur, TreatmentRemove:
		// Checked below, where the per-format rules are.
	default:
		return fmt.Errorf("unknown image treatment %q, expected one of: keep, box, blur, remove",
			d.Treatment)
	}

	redraws := d.Treatment == TreatmentBox || d.Treatment == TreatmentBlur

	// The LINKED test comes before the format test even though the scan reports
	// a linked picture as FormatOther: "there are no bytes here" is the true
	// reason and the more useful one, where "this application cannot redraw this
	// format" would send the user looking for a converter that cannot help.
	if redraws && a.Linked {
		return fmt.Errorf("this picture is linked from outside the document, so there are no " +
			"bytes here to change; it can be removed from the document, or kept")
	}
	if redraws && a.Format == FormatOther {
		return fmt.Errorf("this picture is a %s file, which this application cannot redraw; "+
			"it can be removed, or kept as it is", a.Format)
	}
	// No blur for SVG, and the reason is the invariant rather than an
	// implementation limit: a blur filter over a vector leaves every original
	// shape and every original text string in the file, so a control that did
	// it would be labelled "anonymise" while anonymising nothing.
	if d.Treatment == TreatmentBlur && a.Format == FormatSVG {
		return fmt.Errorf("an SVG image cannot be blurred: a blur filter leaves the original " +
			`shapes and text inside the file; use "Replace with a box" or "Remove" instead`)
	}

	if n := len([]rune(d.BoxText)); n > MaxBoxText {
		return fmt.Errorf("the box text is %d characters, the maximum is %d; shorten it",
			n, MaxBoxText)
	}
	// Zero is "not stated" and reads as the default, so only a stated number
	// outside the dial is refused.
	if d.BlurStrength != 0 && (d.BlurStrength < MinBlurStrength || d.BlurStrength > MaxBlurStrength) {
		return fmt.Errorf("the blur strength is %d, it must be between %d and %d",
			d.BlurStrength, MinBlurStrength, MaxBlurStrength)
	}
	return nil
}

// Summary counts what one export did to a document's pictures, so the
// application can say it out loud instead of reporting a document as anonymised
// without mentioning its images.
//
// The counts are per ASSET, matching the unit a decision is taken on: a logo
// boxed on five slides is one boxed picture, because the user answered one
// question about it.
type Summary struct {
	Kept    int `json:"kept"`
	Boxed   int `json:"boxed"`
	Blurred int `json:"blurred"`
	Removed int `json:"removed"`
}

// Anonymised is how many of the document's pictures this export changed.
func (s Summary) Anonymised() int {
	return s.Boxed + s.Blurred + s.Removed
}

// Total is how many pictures the document had.
func (s Summary) Total() int {
	return s.Kept + s.Anonymised()
}
