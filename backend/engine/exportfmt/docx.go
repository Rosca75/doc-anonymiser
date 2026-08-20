// engine/exportfmt/docx.go — same-format export for Word documents
// a NEW .docx whose body, headers, footers,
// footnotes and endnotes went through the pipeline's replacements while
// every other archive entry survives bit-for-bit.
//
// Headers/footers/footnotes were DROPPED at import (pagination noise,
// CLAUDE.md §5), so their text never met entity discovery; they still go
// through the deterministic PII pass and the registry mapping here, and
// hits are counted per part in Extras so the caller can warn that text
// outside the preview was changed.
package exportfmt

import (
	"path"
	"strings"

	"doc-anonymiser/backend/engine/imaging"
)

// Extras reports replacements made in parts the user never previewed
// (headers, footers, footnotes, endnotes), keyed by part name.
type Extras map[string]int

// Total sums the extra replacements across parts.
func (e Extras) Total() int {
	n := 0
	for _, c := range e {
		n += c
	}
	return n
}

// isDocxBodyPart / isDocxExtraPart classify the rewritable docx parts.
func isDocxBodyPart(name string) bool {
	return name == "word/document.xml"
}

func isDocxExtraPart(name string) bool {
	if !strings.HasPrefix(name, "word/") || !strings.HasSuffix(name, ".xml") {
		return false
	}
	base := path.Base(name)
	return strings.HasPrefix(base, "header") || strings.HasPrefix(base, "footer") ||
		base == "footnotes.xml" || base == "endnotes.xml"
}

// ExportDocx rewrites a Word document held in raw (the import-time
// bytes). It returns the new file, the per-part extras counts, the total number
// of body replacements, and what happened to the document's pictures.
//
// Each rewritable part goes through the text pass FIRST and the picture pass
// SECOND, on the bytes the text pass produced (engine/exportfmt/images.go says
// why the two are sequential rather than merged). A part that carries pictures
// but no rewritable text still gets the picture pass, so a decision cannot be
// honoured in the body and forgotten in a header.
func ExportDocx(raw []byte, cfg Config) ([]byte, Extras, int, imaging.Summary, error) {
	extras := Extras{}
	bodyCount := 0
	plan := cfg.Images
	media := plan.mediaRewrites()

	out, err := rewriteZip(raw, func(name string) RewriteFunc {
		if pair, ok := media[name]; ok {
			// A media entry the plan changes: its bytes are REPLACED, which is
			// what keeps the original pixels out of the produced file.
			return treatMediaPart(name, pair)
		}
		partName := name
		switch {
		case isDocxBodyPart(name):
			return func(data []byte) ([]byte, error) {
				rewritten, n, err := rewriteTextPart(data, cfg)
				if err != nil {
					return nil, err
				}
				bodyCount += n
				return applyImagePass(rewritten, partName, plan)
			}
		case isDocxExtraPart(name):
			return func(data []byte) ([]byte, error) {
				rewritten, n, err := rewriteTextPart(data, cfg)
				if err != nil {
					return nil, err
				}
				if n > 0 {
					extras[partName] += n
				}
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
		return nil, nil, 0, imaging.Summary{}, err
	}
	return out, extras, bodyCount, plan.Summary(), nil
}
