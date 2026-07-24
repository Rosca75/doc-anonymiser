# CHANGE-01 — Extend supported inputs to .docx, .pptx, .xlsx, .pdf

You are executing a change order against the existing **doc-anonymiser** repository (bootstrapped from INITIALISATION.md, pattern P0 — pure Go + Wails v2). The original scope limited inputs to `.txt`, `.csv`, `.md`; that was a specification error. The application must additionally import **`.docx`, `.pptx`, `.xlsx`, `.pdf`**, converting them to markdown on import (the nb1 notebook capability), so the fixed process — 1) convert to markdown, 2) anonymise, 3) export — applies to all seven formats.

Ground rules for this change order:

- `CLAUDE.md` remains the single source of truth; this prompt tells you exactly how to amend it. Apply the edits verbatim.
- The zero-CGo rule is untouched. All converters are pure Go.
- Conversion is **one-way**: binary formats are imported and converted to markdown; export formats remain `.md` / `.txt` / `.csv` / zip / clipboard. The app never writes a `.docx`, `.pptx`, `.xlsx` or `.pdf`.
- Execute Steps 1–3 in order, then stop. Do NOT start implementing the converters — that happens in the (new) Phase 1B of BUILD.md, in its normal turn.

## Step 1 — Amend CLAUDE.md

### 1a. §3 Repository structure

Inside the `engine/` block of the tree, add a `convert/` sub-package immediately after `csvmd.go`:

```
│   ├── convert/               # binary-format → markdown converters (pure Go, one-way)
│   │   ├── docx.go            # zip + XML extraction of word/document.xml
│   │   ├── pptx.go            # one H2 per slide; body, tables, speaker notes
│   │   ├── xlsx.go            # excelize; smart per-sheet routing (flat → Grid, complex → JSON)
│   │   └── pdf.go             # text extraction + spacing repair (EXPERIMENTAL)
```

### 1b. §4 Architecture rules — add one rule

```
- **Converters are pure Go and one-way:** `engine/convert/*` may use only the
  Go standard library, excelize, and ledongthuc/pdf (pinned in §7). No CGo,
  ever. Binary formats convert TO markdown on import; the app never exports
  back to docx/pptx/xlsx/pdf. If pure-Go PDF extraction quality proves
  unacceptable, the recorded fallback is a wazero-embedded WASM extractor
  (P3 pattern) — not a CGo binding.
```

### 1c. §5 Domain rules — replace the "Supported inputs" bullet with:

```
- **Supported inputs:** `.txt`, `.csv`, `.md`, `.docx`, `.pptx`, `.xlsx`,
  `.pdf`. Reject anything else in the file dialog filter AND on drop, with a
  clear message. Conversion rules per format:
  - `.txt` → markdown as-is (line-ending normalisation).
  - `.md`  → passthrough.
  - `.csv` → Grid model + markdown-table preview; round-trips to CSV on export.
  - `.docx` → headings (paragraph styles Heading 1–6 → #..######), bold/italic
    runs, ordered/unordered lists (numPr), tables → markdown tables,
    hyperlinks → markdown links. Images dropped with an inline placeholder
    `*[image omitted]*`. Headers/footers/footnotes dropped (pagination noise).
  - `.pptx` → one `## Slide N — <title>` section per slide; body text with
    bullet indentation; tables → markdown tables; speaker notes under a
    `**Notes:**` sub-block. Slide-master/branding shapes skipped.
  - `.xlsx` → one Document per sheet, named `<workbook>.xlsx#<sheet>`. Smart
    routing per sheet (nb1 rules): FLAT (no merged cells, contiguous data
    bounds, header-like first row) → Grid model, same behaviour as a CSV
    import including CSV round-trip export; COMPLEX → structured JSON
    rendered in a fenced code block, anonymised as text. Trailing empty
    rows/columns trimmed via data-bounds detection.
  - `.pdf` → per-page text extraction with the spacing-repair heuristic
    (collapse runs of single uppercase characters split by kerning; collapse
    doubled spaces). PDF support is EXPERIMENTAL and labelled as such in the
    UI. A PDF yielding no extractable text is rejected with: "No text layer
    found — this PDF is likely scanned. OCR is not supported; convert it
    externally first."
```

### 1d. §7 Pinned versions — add two rows to the table

```
| github.com/xuri/excelize/v2 | v2.9.x | XLSX reading; pure Go, MIT licence |
| github.com/ledongthuc/pdf | pin the latest tagged/commit version at implementation time and record it here | pure-Go PDF text extraction (BSD-3); limited by design — see §5 PDF rules |
```

## Step 2 — Amend BUILD.md

### 2a. Dependency table — add:

```
| github.com/xuri/excelize/v2 | v2.9.x | Phase 1B | XLSX parsing incl. merged-cell detection; pure Go, MIT |
| github.com/ledongthuc/pdf | pinned at implementation (record in CLAUDE.md §7) | Phase 1B | pure-Go PDF text extraction; only permitted PDF dependency |
```

### 2b. Performance budgets — add:

```
| Binary-format conversion (docx/pptx/xlsx/pdf), typical office file ≤ 20 MB | ≤ 5 s per file | Phase 1B |
```

### 2c. Insert a new phase between Phase 1 and Phase 2, titled **Phase 1B — Binary-format converters (docx, pptx, xlsx, pdf)**. Do not renumber the other phases. Full phase text:

```
## Phase 1B — Binary-format converters (docx, pptx, xlsx, pdf)

### Goal
Pure-Go, one-way conversion of the four Office/PDF formats into the Document
model, replicating the nb1 notebook converters within the zero-CGo rule.

### Activities
1. engine/convert/docx.go — open with archive/zip; parse word/document.xml
   with encoding/xml; map paragraph styles Heading1–6 to markdown headings;
   runs with b/i properties to **bold**/*italic*; numPr lists to -/1. items
   with indentation; tables (w:tbl) to markdown tables; hyperlinks resolved
   via word/_rels/document.xml.rels; images replaced by *[image omitted]*.
2. engine/convert/pptx.go — enumerate ppt/slides/slideN.xml in order; title
   placeholder to "## Slide N — <title>"; body text frames with bullet
   levels; a:tbl to markdown tables; notesSlide text under **Notes:**.
3. engine/convert/xlsx.go — excelize v2.9.x; per-sheet flat detection
   (merged-cell check via GetMergeCells, data-bounds trim, header-row
   heuristic: first non-empty row all non-numeric strings); flat sheet →
   Grid Document (identical downstream behaviour to CSV import); complex
   sheet → JSON Document (cell map with addresses) in a fenced code block.
   One Document per sheet, named <workbook>.xlsx#<sheet>.
4. engine/convert/pdf.go — ledongthuc/pdf per-page plain-text extraction;
   port the nb1 repair heuristic (collapse single-uppercase-char token runs;
   collapse double spaces) as RepairPDFText with the nb1 examples as test
   cases; empty-extraction detection returning the actionable scanned-PDF
   error from CLAUDE.md §5.
5. Wire the four converters into engine/document.go Load() dispatch; extend
   the Document model so one imported file may yield multiple Documents
   (xlsx sheets); collect per-file conversion warnings (dropped images,
   complex-sheet routing, PDF repair applied).
6. Test fixtures: generate minimal valid .docx and .pptx in a test helper by
   assembling the OOXML zip structure directly (stdlib only); generate the
   .xlsx fixture with excelize; hand-construct a minimal single-page text
   .pdf fixture (raw PDF syntax) plus an image-only .pdf for the scanned
   rejection path. Commit fixtures under testdata/. All fixture content
   obviously fictional, English + French.
7. Measure the ≤ 5 s conversion budget on the largest fixture; record the
   measurement in a test comment.

### Unit tests
- Golden-file tests per converter (fixture → expected markdown).
- xlsx routing: flat fixture becomes Grid; merged-cell fixture becomes JSON;
  trailing empty rows/cols trimmed.
- RepairPDFText table-driven cases including the nb1 'B R IDDING ULES' →
  'BIDDING RULES' class of defect and a no-op lowercase case.
- Scanned-PDF fixture returns the exact actionable error message.
- Unsupported extension still rejected.

### Definition of done
- Build and tests pass; budget measured.
- Commit: feat(convert): pure-Go docx/pptx/xlsx/pdf converters
```

### 2d. Phase 6 (UI shell, import screen) — amend activity 3

Extend the file dialog filter and drop validation to all seven extensions; per-file format badge covers the new formats; xlsx imports render one list entry per sheet Document; PDF entries carry an "experimental" badge; conversion warnings from Phase 1B surface in the per-file warnings display.

### 2e. Phase 9 (Export) — amend activity 1

Documents originating from docx/pptx/pdf export as `.md` (or `.txt`); flat xlsx-sheet Documents behave like CSV-origin documents (`.csv` or `.md`); complex-sheet Documents export as `.md` or `.json`. No export path produces a binary Office/PDF format.

### 2f. Manual test matrix — add rows

```
| 10 | Real Office documents | Import a genuine real-world docx (headings, table, image) and pptx (titles, notes) | Faithful markdown; image placeholder present; notes captured | Windows 11 |
| 11 | Workbook routing | Import an xlsx with one flat sheet and one merged-cell sheet | Two Documents: Grid + JSON; flat sheet exports back to valid CSV after anonymisation | Windows 11 |
| 12 | PDF paths | Import a text-layer PDF and a scanned PDF | First converts with repair; second rejected with the scanned-PDF message; experimental badge visible | Windows 11 |
```

### 2g. Deferred to v2 — edit the list

Remove the line deferring DOCX/PDF/PPTX/XLSX input conversion. Add:
- Scanned-PDF OCR.
- Export back to original binary formats (docx/pptx/xlsx/pdf reconstruction).
- DOCX/PPTX embedded-image content description (alt-text or vision-model based).
- Per-workbook `_manifest.md` overview generation (nb1 feature; superseded in-app by the per-sheet Document list).

## Step 3 — Amend README.md, verify, commit

1. README.md: update the supported-formats list to all seven; add one sentence marking PDF as experimental (text-layer only, no OCR); note that Office/PDF files are converted to markdown and exported as text formats.
2. Verify: `go vet ./...` and `go test ./...` still pass (no converter code is written yet — this change order only amends documents and README); CLAUDE.md contains the §1a–§1d edits exactly; BUILD.md contains §2a–§2g exactly.
3. Exactly **one** commit, message: `docs: extend supported inputs to docx/pptx/xlsx/pdf (CHANGE-01)`.
