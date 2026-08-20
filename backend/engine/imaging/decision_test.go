// engine/imaging/decision_test.go — the per-format decision rules, unit tier.
//
// TIER: unit (docs/TESTING.md). Pure set arithmetic over an Asset built in the
// test; nothing is decoded and nothing is written.
//
// Every case asserts the ACTIONABLE half of the message as well as the refusal,
// because "an SVG image cannot be blurred" on its own leaves the user looking at
// a control with nothing to do about it, and the charter's rule is that an error
// says what to do next.
package imaging_test

import (
	"strings"
	"testing"

	"doc-anonymiser/backend/engine/imaging"
)

func TestDecisionValidate(t *testing.T) {
	raster := imaging.Asset{ID: "ppt/media/image1.png", Format: imaging.FormatPNG, Width: 120, Height: 80}
	vector := imaging.Asset{ID: "ppt/media/image2.png", Format: imaging.FormatSVG, Companion: "ppt/media/image2.svg"}
	other := imaging.Asset{ID: "word/media/image3.emf", Format: imaging.FormatOther}
	linked := imaging.Asset{ID: "https://example.com/logo.png", Format: imaging.FormatOther, Linked: true}

	cases := []struct {
		name string
		as   imaging.Asset
		d    imaging.Decision
		// wantOK is true when the decision must be accepted.
		wantOK bool
		// wantSays are fragments the refusal must carry: the reason, and the way
		// out of it.
		wantSays []string
	}{
		{
			name: "config/decision_validate_keep_is_always_fine",
			as:   other,
			d:    imaging.Decision{Treatment: imaging.TreatmentKeep},
			// A format nothing can redraw can still be kept: keep is the absence
			// of a change.
			wantOK: true,
		},
		{
			name:   "config/decision_validate_absent_treatment_reads_as_keep",
			as:     linked,
			d:      imaging.Decision{},
			wantOK: true,
		},
		{
			name:     "config/decision_validate_unknown_treatment",
			as:       raster,
			d:        imaging.Decision{Treatment: "redact"},
			wantSays: []string{`unknown image treatment "redact"`, "keep, box, blur, remove"},
		},
		{
			name:     "config/decision_validate_blur_on_svg",
			as:       vector,
			d:        imaging.Decision{Treatment: imaging.TreatmentBlur, BlurStrength: 5},
			wantSays: []string{"cannot be blurred", "original", `"Replace with a box"`, `"Remove"`},
		},
		{
			name:   "config/decision_validate_box_on_svg_is_offered",
			as:     vector,
			d:      imaging.Decision{Treatment: imaging.TreatmentBox, BoxText: "Client diagram"},
			wantOK: true,
		},
		{
			name:   "config/decision_validate_remove_on_svg_is_offered",
			as:     vector,
			d:      imaging.Decision{Treatment: imaging.TreatmentRemove},
			wantOK: true,
		},
		{
			name:     "config/decision_validate_box_on_unredrawable_format",
			as:       other,
			d:        imaging.Decision{Treatment: imaging.TreatmentBox},
			wantSays: []string{"cannot redraw", "removed", "kept"},
		},
		{
			name:     "config/decision_validate_blur_on_unredrawable_format",
			as:       other,
			d:        imaging.Decision{Treatment: imaging.TreatmentBlur},
			wantSays: []string{"cannot redraw", "removed", "kept"},
		},
		{
			name:   "config/decision_validate_remove_on_unredrawable_format",
			as:     other,
			d:      imaging.Decision{Treatment: imaging.TreatmentRemove},
			wantOK: true,
		},
		{
			name: "config/decision_validate_box_on_linked_asset",
			as:   linked,
			d:    imaging.Decision{Treatment: imaging.TreatmentBox},
			// The linked reason beats the format reason: "there are no bytes
			// here" is the truth, where "this format cannot be redrawn" sends
			// the user looking for a converter that cannot help.
			wantSays: []string{"linked from outside", "no bytes", "removed", "kept"},
		},
		{
			name:   "config/decision_validate_remove_on_linked_asset",
			as:     linked,
			d:      imaging.Decision{Treatment: imaging.TreatmentRemove},
			wantOK: true,
		},
		{
			name: "config/decision_validate_box_text_too_long",
			as:   raster,
			d: imaging.Decision{
				Treatment: imaging.TreatmentBox,
				BoxText:   strings.Repeat("a", imaging.MaxBoxText+1),
			},
			wantSays: []string{"121 characters", "maximum is 120", "shorten it"},
		},
		{
			name: "config/decision_validate_box_text_at_the_cap",
			as:   raster,
			d: imaging.Decision{
				Treatment: imaging.TreatmentBox,
				BoxText:   strings.Repeat("a", imaging.MaxBoxText),
			},
			wantOK: true,
		},
		{
			name: "config/decision_validate_box_text_counts_runes_not_bytes",
			as:   raster,
			// 120 accented letters are 240 bytes and 120 characters. Counting
			// bytes would refuse a French caption the user is entitled to.
			d: imaging.Decision{
				Treatment: imaging.TreatmentBox,
				BoxText:   strings.Repeat("é", imaging.MaxBoxText),
			},
			wantOK: true,
		},
		{
			name:     "config/decision_validate_blur_strength_too_high",
			as:       raster,
			d:        imaging.Decision{Treatment: imaging.TreatmentBlur, BlurStrength: 11},
			wantSays: []string{"blur strength is 11", "between 1 and 10"},
		},
		{
			name:     "config/decision_validate_blur_strength_negative",
			as:       raster,
			d:        imaging.Decision{Treatment: imaging.TreatmentBlur, BlurStrength: -2},
			wantSays: []string{"blur strength is -2", "between 1 and 10"},
		},
		{
			name: "config/decision_validate_blur_strength_absent_reads_as_default",
			as:   raster,
			// Zero is "not stated". Every absent value in this application reads
			// as its default rather than as "none", and a refusal here would make
			// the store's own omit-empty encoding invalid.
			d:      imaging.Decision{Treatment: imaging.TreatmentBlur},
			wantOK: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.d.Validate(c.as)
			if c.wantOK {
				if err != nil {
					t.Fatalf("Validate(%+v) on a %s asset: %v, want no error",
						c.d, c.as.Format, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate(%+v) on a %s asset accepted it, want a refusal",
					c.d, c.as.Format)
			}
			for _, want := range c.wantSays {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not mention %q, so the user cannot act on it:\n%v",
						want, err)
				}
			}
		})
	}
}

// TestDecisionAnonymises: an absent treatment is keep, because keep is stored as
// the absence of a decision everywhere in this feature.
func TestDecisionAnonymises(t *testing.T) {
	t.Run("config/decision_anonymises", func(t *testing.T) {
		cases := map[imaging.Treatment]bool{
			"":                      false,
			imaging.TreatmentKeep:   false,
			imaging.TreatmentBox:    true,
			imaging.TreatmentBlur:   true,
			imaging.TreatmentRemove: true,
		}
		for treatment, want := range cases {
			if got := (imaging.Decision{Treatment: treatment}).Anonymises(); got != want {
				t.Errorf("Decision{%q}.Anonymises() = %v, want %v", treatment, got, want)
			}
		}
	})
}

// TestAllTreatmentsIsTheWholeSet: the ordered list the frontend mirrors has to
// hold every treatment the model defines, or a mirror built from it would offer
// the user fewer choices than the engine accepts.
func TestAllTreatmentsIsTheWholeSet(t *testing.T) {
	t.Run("config/all_treatments_complete", func(t *testing.T) {
		want := []imaging.Treatment{
			imaging.TreatmentKeep, imaging.TreatmentBox,
			imaging.TreatmentBlur, imaging.TreatmentRemove,
		}
		if len(imaging.AllTreatments) != len(want) {
			t.Fatalf("AllTreatments has %d entries, want %d: %v",
				len(imaging.AllTreatments), len(want), imaging.AllTreatments)
		}
		for i, treatment := range want {
			if imaging.AllTreatments[i] != treatment {
				t.Errorf("AllTreatments[%d] = %q, want %q; the order is the order the "+
					"interface offers them in", i, imaging.AllTreatments[i], treatment)
			}
		}
	})
}

// TestSummaryCounts: the summary's two derived numbers, because the export
// screen's sentence is built from them and an off-by-one there tells the user a
// picture was changed that was not.
func TestSummaryCounts(t *testing.T) {
	t.Run("config/summary_counts", func(t *testing.T) {
		s := imaging.Summary{Kept: 7, Boxed: 1, Blurred: 2, Removed: 3}
		if got := s.Anonymised(); got != 6 {
			t.Errorf("Anonymised() = %d, want 6 (1 boxed + 2 blurred + 3 removed)", got)
		}
		if got := s.Total(); got != 13 {
			t.Errorf("Total() = %d, want 13 (7 kept + 6 anonymised)", got)
		}
	})
}
