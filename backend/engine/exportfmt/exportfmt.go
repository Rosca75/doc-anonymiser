// Package exportfmt implements SAME-FORMAT export (BUILD-02 Phases 11-13):
// writing a NEW anonymised copy of a docx/pptx/xlsx file with the source
// formatting preserved.
//
// Non-negotiables (CLAUDE.md §4):
//   - Pure Go only: archive/zip, encoding/xml and the already-pinned
//     excelize. No new dependencies, no CGo.
//   - The ORIGINAL FILE ON DISK IS NEVER TOUCHED. Everything here reads
//     Document.Raw (the bytes captured once at import) and returns a new
//     byte slice that app.go writes through the usual save dialog.
//   - Every archive entry without a registered rewriter is copied
//     BIT-FOR-BIT (zip raw copy, no recompression), so styles, themes,
//     images and metadata survive untouched.
//
// The text replacement runs the SAME span machinery as the body pipeline
// (deterministic passes plus the registry mapping), with the session
// registry passed in, so placeholders match the markdown export exactly.
package exportfmt

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"sort"

	"doc-anonymiser/backend/engine"
)

// Config bundles everything the rewriters need to reproduce the body
// pipeline's replacements. Populate it from the last run's inputs.
type Config struct {
	Entities   []engine.Entity
	Patterns   []engine.CustomPattern
	Categories engine.CategorySelection
	Level      engine.Level
	Country    string
	Allowlist  *engine.Allowlist
	// Registry is the SESSION registry: passing the same instance the
	// pipeline used guarantees identical placeholders here.
	Registry *engine.Registry
}

// selection resolves the effective category switches (nil = level preset,
// mirroring engine.Run).
func (c Config) selection() engine.CategorySelection {
	if c.Categories != nil {
		return c.Categories
	}
	level := c.Level
	if level == "" {
		level = engine.LevelMedium
	}
	return engine.PresetSelection(level)
}

// Replacement is one span to splice: text[Start:End) becomes Text.
type Replacement struct {
	Start, End int
	Text       string
}

// Replacements computes the anonymisation spans for one piece of decoded
// text: pass 1 (PII), pass 2 (entities + custom patterns) and the
// registry post-pass (known originals from other passes/documents), all
// through ResolveOverlaps, then materialised into placeholders via the
// session registry.
func (c Config) Replacements(text string) []Replacement {
	if text == "" || c.Registry == nil {
		return nil
	}
	sel := c.selection()

	var entities []engine.Entity
	for _, e := range c.Entities {
		if sel[e.Category] {
			entities = append(entities, e)
		}
	}

	country := c.Country
	if country == "" {
		country = engine.CountryLU
	}
	spans := engine.FilterAllowed(engine.DetectPIISelected(text, sel, country), c.Allowlist)
	spans = append(spans, engine.DetectEntities(text, entities, c.Allowlist)...)
	if sel["custom_patterns"] {
		spans = append(spans, engine.DetectCustomPatterns(text, c.Patterns, c.Allowlist)...)
	}
	// Registry post-pass equivalent: values that earned a placeholder in
	// ANY document (including LLM deep-scan finds that never became
	// session entities) are replaced here too, so the same-format copy
	// can never leak something the markdown export already hides.
	spans = append(spans, filterAllowedKnown(engine.DetectKnownOriginals(text, c.Registry.Entries()), c.Allowlist)...)

	resolved := engine.ResolveOverlaps(spans)
	out := make([]Replacement, 0, len(resolved))
	for _, s := range resolved {
		out = append(out, Replacement{
			Start: s.Start,
			End:   s.End,
			Text:  c.Registry.Assign(s.Category, s.CanonicalOrOriginal()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// AnonymiseText applies Replacements to a plain string (used by the xlsx
// cell rewriter and the metadata pass).
func (c Config) AnonymiseText(text string) (string, int) {
	repls := c.Replacements(text)
	if len(repls) == 0 {
		return text, 0
	}
	var b bytes.Buffer
	last := 0
	for _, r := range repls {
		b.WriteString(text[last:r.Start])
		b.WriteString(r.Text)
		last = r.End
	}
	b.WriteString(text[last:])
	return b.String(), len(repls)
}

// filterAllowedKnown drops known-original spans whose text is allowlisted
// (allowlist wins over the registry too).
func filterAllowedKnown(spans []engine.Span, allow *engine.Allowlist) []engine.Span {
	if allow == nil {
		return spans
	}
	out := spans[:0]
	for _, s := range spans {
		if !allow.Contains(s.Original) && !allow.Contains(s.Canonical) {
			out = append(out, s)
		}
	}
	return out
}

// RewriteFunc transforms one zip entry's content.
type RewriteFunc func([]byte) ([]byte, error)

// rewriteZip copies the archive, rewriting only the entries for which
// selector returns a non-nil RewriteFunc. Every other entry is copied
// RAW (same compressed bytes, same header), guaranteeing bit-identical
// passthrough of styles, images, themes and anything else untouched.
func rewriteZip(raw []byte, selector func(name string) RewriteFunc) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("the original file could not be opened as a zip archive (%v); re-import the document and try again", err)
	}

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, entry := range reader.File {
		rewrite := selector(entry.Name)
		if rewrite == nil {
			// Bit-for-bit passthrough: raw copy without recompression.
			src, err := entry.OpenRaw()
			if err != nil {
				return nil, fmt.Errorf("could not read %q from the archive: %w", entry.Name, err)
			}
			header := entry.FileHeader
			dst, err := writer.CreateRaw(&header)
			if err != nil {
				return nil, fmt.Errorf("could not copy %q into the new archive: %w", entry.Name, err)
			}
			if _, err := io.Copy(dst, src); err != nil {
				return nil, fmt.Errorf("could not copy %q into the new archive: %w", entry.Name, err)
			}
			continue
		}

		src, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("could not read %q from the archive: %w", entry.Name, err)
		}
		content, err := io.ReadAll(src)
		src.Close()
		if err != nil {
			return nil, fmt.Errorf("could not read %q from the archive: %w", entry.Name, err)
		}
		rewritten, err := rewrite(content)
		if err != nil {
			return nil, fmt.Errorf("could not rewrite %q: %w", entry.Name, err)
		}
		header := entry.FileHeader
		header.CompressedSize64 = 0
		header.UncompressedSize64 = uint64(len(rewritten))
		header.CRC32 = 0
		dst, err := writer.CreateHeader(&header)
		if err != nil {
			return nil, fmt.Errorf("could not write %q into the new archive: %w", entry.Name, err)
		}
		if _, err := dst.Write(rewritten); err != nil {
			return nil, fmt.Errorf("could not write %q into the new archive: %w", entry.Name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("could not finalise the new archive: %w", err)
	}
	return buf.Bytes(), nil
}
