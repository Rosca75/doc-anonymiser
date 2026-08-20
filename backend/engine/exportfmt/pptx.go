// engine/exportfmt/pptx.go — same-format export for PowerPoint files
// every slide and notes slide goes through the
// text-run replacement; everything else (masters, layouts, media,
// themes) survives bit-for-bit.
package exportfmt

import (
	"path"
	"strings"

	"doc-anonymiser/backend/engine/imaging"
)

// isPptxTextPart selects the rewritable pptx parts: slides and speaker
// notes. Slide masters and layouts carry branding, not engagement text,
// and are deliberately left untouched (mirroring the import converter).
func isPptxTextPart(name string) bool {
	if !strings.HasSuffix(name, ".xml") {
		return false
	}
	dir := path.Dir(name)
	base := path.Base(name)
	return (dir == "ppt/slides" && strings.HasPrefix(base, "slide")) ||
		(dir == "ppt/notesSlides" && strings.HasPrefix(base, "notesSlide"))
}

// ExportPptx rewrites a PowerPoint file held in raw (the import-time
// bytes) and returns the new file, the total replacement count, and what
// happened to the deck's pictures.
//
// A slide goes through the text pass FIRST and the picture pass SECOND, on the
// bytes the text pass produced. Slide layouts and masters carry no rewritable
// text, and they DO carry pictures (a watermark, a client logo behind every
// slide), so they get the picture pass on its own: a logo the user boxed must
// not survive because it lives on the master.
func ExportPptx(raw []byte, cfg Config) ([]byte, int, imaging.Summary, error) {
	total := 0
	plan := cfg.Images
	media := plan.mediaRewrites()

	out, err := rewriteZip(raw, func(name string) RewriteFunc {
		if pair, ok := media[name]; ok {
			return treatMediaPart(name, pair)
		}
		partName := name
		switch {
		case isPptxTextPart(name):
			return func(data []byte) ([]byte, error) {
				rewritten, n, err := rewriteTextPart(data, cfg)
				if err != nil {
					return nil, err
				}
				total += n
				return applyImagePass(rewritten, partName, plan)
			}
		case plan.touchesPart(name):
			return func(data []byte) ([]byte, error) {
				return applyImagePass(data, partName, plan)
			}
		default:
			return nil // bit-for-bit passthrough
		}
	})
	if err != nil {
		return nil, 0, imaging.Summary{}, err
	}
	return out, total, plan.Summary(), nil
}
