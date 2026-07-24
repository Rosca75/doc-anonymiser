// engine/exportfmt/pptx.go — same-format export for PowerPoint files
// (BUILD-02 Phase 11d): every slide and notes slide goes through the
// text-run replacement; everything else (masters, layouts, media,
// themes) survives bit-for-bit.
package exportfmt

import (
	"path"
	"strings"
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
// bytes) and returns the new file plus the total replacement count.
func ExportPptx(raw []byte, cfg Config) ([]byte, int, error) {
	total := 0
	out, err := rewriteZip(raw, func(name string) RewriteFunc {
		if !isPptxTextPart(name) {
			return nil // bit-for-bit passthrough
		}
		return func(data []byte) ([]byte, error) {
			rewritten, n, err := rewriteTextPart(data, cfg)
			total += n
			return rewritten, err
		}
	})
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}
