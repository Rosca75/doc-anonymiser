// engine/exportfmt/images.go — the picture half of the same-format export.
//
// It closes a leak that has nothing to do with images being a new feature:
// rewriteZip copies every archive entry it has no rewriter for BIT-FOR-BIT, and
// nothing was ever registered for word/media/* or ppt/media/*. So an
// "anonymised" .docx or .pptx carried every original picture out with it, the
// client logo and the screenshot of the client's own system included, while its
// text was replaced.
//
// TWO PASSES OVER ONE PART, text first and pictures second, deliberately
// sequential rather than merged. A merged splice set would have to reconcile a
// text replacement that falls INSIDE a picture element being deleted (a Word
// text box lives inside w:drawing), and "apply the text, then re-scan the result
// and apply the pictures" has no such case: the second pass reads bytes the
// first pass has already finished with.
//
// That is also why an occurrence is identified by PART plus ORDINAL and never by
// a byte offset: every offset the import-time scan recorded is stale once the
// text pass has run, so the picture pass asks the same walker again, against the
// bytes it now holds.
package exportfmt

import (
	"fmt"

	"doc-anonymiser/backend/engine/imaging"
)

// ImagePlan is what the App hands the exporter: the decisions, keyed by asset
// ID, plus the inventory they were taken against.
//
// An EMPTY plan means "change no picture", which is exactly the behaviour every
// export had before this pass existed, and a test holds it to that byte for
// byte.
type ImagePlan struct {
	Inventory imaging.Inventory
	Decisions map[string]imaging.Decision
}

// assetDecision pairs one asset with the decision taken about it, because
// applying a treatment needs both: the decision says what to draw and the asset
// says what size and what format to draw it in.
type assetDecision struct {
	asset    imaging.Asset
	decision imaging.Decision
}

// Empty reports a plan that changes nothing, so the callers can skip the whole
// picture pass rather than walking every part to find no work.
func (p ImagePlan) Empty() bool {
	for _, asset := range p.Inventory.Assets {
		if p.Decisions[asset.ID].Anonymises() {
			return false
		}
	}
	return true
}

// mediaRewrites maps every media part this plan changes to the asset and
// decision that change it.
//
// An asset's COMPANION is listed under the same decision: an SVG picture is a
// PNG fallback plus the SVG itself, both parts carry the picture, and treating
// only one of them means a "blurred" logo comes back sharp on the machine that
// renders the other.
//
// A LINKED asset contributes nothing here: its ID names a file outside the
// archive, so there is no entry to rewrite. Its removal is served by deleting
// the picture element alone, which is all the document itself holds.
func (p ImagePlan) mediaRewrites() map[string]assetDecision {
	out := map[string]assetDecision{}
	for _, asset := range p.Inventory.Assets {
		d := p.Decisions[asset.ID]
		if !d.Anonymises() || asset.Linked {
			continue
		}
		pair := assetDecision{asset: asset, decision: d}
		out[asset.ID] = pair
		if asset.Companion != "" {
			out[asset.Companion] = pair
		}
	}
	return out
}

// treatmentsIn lists what happens to each picture occurrence of one XML part,
// keyed by the occurrence's ordinal within that part.
func (p ImagePlan) treatmentsIn(part string) map[int]imaging.Treatment {
	out := map[int]imaging.Treatment{}
	for _, asset := range p.Inventory.Assets {
		d := p.Decisions[asset.ID]
		if !d.Anonymises() {
			continue
		}
		for _, occ := range asset.Occurrences {
			if occ.Part == part {
				out[occ.Ordinal] = d.Treatment
			}
		}
	}
	return out
}

// touchesPart reports whether any picture in this part is changed by the plan,
// so a part with no text to rewrite (a slide layout, a slide master) still gets
// a rewriter when one of its pictures is going.
func (p ImagePlan) touchesPart(part string) bool {
	return len(p.treatmentsIn(part)) > 0
}

// applyImagePass is the picture half of one part's rewrite, shaped for the
// rewriters that also run the text pass and therefore have no use for the edit
// count.
func applyImagePass(data []byte, part string, plan ImagePlan) ([]byte, error) {
	rewritten, _, err := rewriteImagePart(data, part, plan)
	return rewritten, err
}

// Summary counts what this plan does, per ASSET, so the application can say it
// out loud instead of reporting a document as anonymised without mentioning its
// pictures.
func (p ImagePlan) Summary() imaging.Summary {
	var s imaging.Summary
	for _, asset := range p.Inventory.Assets {
		switch p.Decisions[asset.ID].Treatment {
		case imaging.TreatmentBox:
			s.Boxed++
		case imaging.TreatmentBlur:
			s.Blurred++
		case imaging.TreatmentRemove:
			s.Removed++
		default:
			s.Kept++
		}
	}
	return s
}

// rewriteImagePart applies one part's picture decisions to its bytes.
//
// It does exactly two things, and nothing else in the XML is touched:
//
//   - a `remove` occurrence's picture ELEMENT is deleted. An occurrence with no
//     element of its own (a shape fill, a slide background) is not deleted here:
//     its removal is the transparent bytes written over its media part, which
//     leaves the layout intact and leaks nothing.
//   - a `box` or `blur` occurrence's <a:srcRect/> is dropped. A source rectangle
//     CROPS the picture inside its frame, and a crop of a replacement box would
//     show one corner of the rectangle with the text outside the frame. Dropping
//     it shows the whole replacement in the same frame, which is what the user
//     asked for.
//
// @param data the part's bytes, after the text pass
// @param part the archive entry name, so the walker's error can name it
// @param plan the decisions and the inventory they were taken against
// @return the new bytes and how many edits were applied
func rewriteImagePart(data []byte, part string, plan ImagePlan) ([]byte, int, error) {
	wanted := plan.treatmentsIn(part)
	if len(wanted) == 0 {
		return data, 0, nil
	}
	places, err := imaging.PicturePlaces(part, data)
	if err != nil {
		return nil, 0, err
	}

	var edits []splice
	for _, place := range places {
		treatment, ok := wanted[place.Ordinal]
		if !ok {
			continue
		}
		switch treatment {
		case imaging.TreatmentRemove:
			if place.HasElement() {
				edits = append(edits, splice{rawStart: place.ElementStart, rawEnd: place.ElementEnd})
			}
		case imaging.TreatmentBox, imaging.TreatmentBlur:
			if place.HasSrcRect() {
				edits = append(edits, splice{rawStart: place.SrcRectStart, rawEnd: place.SrcRectEnd})
			}
		case imaging.TreatmentKeep:
			// Never reaches here: treatmentsIn lists only what changes.
		}
	}
	if len(edits) == 0 {
		return data, 0, nil
	}
	return applySplices(data, edits), len(edits), nil
}

// treatMediaPart is the rewriter registered for a media entry the plan changes.
//
// It exists as a named function so the two exporters register the SAME thing:
// two copies of "call Treat and check the result" is two places for one of them
// to start writing the original bytes through.
func treatMediaPart(part string, pair assetDecision) RewriteFunc {
	return func(data []byte) ([]byte, error) {
		replacement, err := imaging.Treat(data, pair.asset, pair.decision)
		if err != nil {
			return nil, fmt.Errorf("the %q treatment could not be applied to the picture %q: %w",
				pair.decision.Treatment, part, err)
		}
		if replacement == nil {
			// Treat answers nil only for keep, and keep never reaches here. The
			// original bytes are still returned rather than nil, because a
			// rewriter returning nothing would write an empty part.
			return data, nil
		}
		return replacement, nil
	}
}
