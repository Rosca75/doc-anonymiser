# CHANGE-13 — PDF in place: text and pictures through the standard flow

You are executing a change against the existing **doc-anonymiser** repository
(pattern P0, pure Go + Wails v2, no CGo, no npm). This document is the **main
plan** for a change spanning **three batches**: it holds the findings, the
decisions, the invariants, the adoption gate, the batch map and the acceptance
criteria for the whole change. It contains no step-by-step edits of its own;
those live in the batch orders (`docs/change-13b.md` is written; 13c and 13d are
scoped here and written only after the gate in 13b has run).

## What the owner wants

Today a PDF is: extracted to text with `ledongthuc/pdf`
(`backend/engine/convert/pdf.go`), anonymised as text, and **regenerated** as a
new, simplified PDF with `go-pdf/fpdf` (`backend/engine/exportfmt/pdf.go`).
Every picture is dropped (`imaging.ReasonPDFImagesRemoved`), every layout
decision is lost, and the format is labelled EXPERIMENTAL.

The owner wants instead:

1. text **and images** extracted out of the PDF;
2. both anonymised through the standard flow already in the repository: the
   deterministic pipeline for text, the existing `imaging` treatments for
   pictures;
3. an anonymised PDF that stays as close to the original as the facts allow,
   produced by changing only the parts that must change inside the original
   file rather than by rebuilding a new document from text.

Two words carry the distinction, and they are used consistently in every
document of this family. **In-place replacement** is the new mechanism: the
produced file is the original file's own structure with the replaced parts
swapped (the owner's word for it is injection, and that is the intent: edit in
place). **Regeneration** is what the current code does: a new document built
from the anonymised working text. The two never appear in one sentence meaning
the same thing.

## The library, and the two products that share a name

The whole change stands on one dependency decision, and there are **two
different products with confusingly similar names**. The distinction decides
the design, so it is stated here explicitly:

| | `github.com/aspose-pdf-foss/aspose-pdf-foss-for-go` | `github.com/aspose-pdf/aspose-pdf-go-cpp` |
|---|---|---|
| What it is | 100% Go source, standard library only | a wrapper over a proprietary native shared library |
| Licence | MIT; bundled fonts under SIL OFL 1.1 | commercial; `Aspose.PDF.GoViaCPP.lic` via `SetLicense()`; evaluation watermark on every page and a four-page processing limit without one |
| Native artefacts | none | per-platform native binaries |
| `go.mod` | `go 1.24` and **no `require` block at all** | requires the native blob |

**The C++ product is REJECTED**, in the repository's habit of recording
rejected options (the pdfcpu row in `CLAUDE.md` §7). The reasons: it costs a
commercial licence and carries an evaluation watermark until one is applied; it
ships a per-platform proprietary binary beside or inside the executable; and it
replaces "pure Go, no CGo" with "no CGo compiler, but a native blob anyway",
which is the letter of the P0 rule and not its purpose. **`purego` is not
needed and must not be introduced**: it would only exist to reach the C++
product, and the FOSS module needs no FFI at all. It is an ordinary `go get`.

### What was re-verified, and when

Checked against the live repository and documentation on **2026-08-21**:

- **Releases v0.1.0 (21 May 2026) through v0.7.0 (10 Aug 2026).** The module is
  **pre-1.0**, has **3 GitHub stars**, and its published feature surface is very
  broad for a repository three months old. That mismatch is a risk this plan
  treats as one (Q10), not a detail.
- **Zero third-party dependencies**: the `go.mod` on `main` is the module line
  and `go 1.24`, with no `require` block. The repo's Go pin (1.26.5) satisfies
  it.
- **Licence**: MIT, plus SIL OFL 1.1 for the bundled metric-compatible fonts
  (Arimo, Tinos, Cousine, Carlito).
- **API surface**, as the README documents it: `OpenStream(io.Reader)` /
  `OpenStreamWithPassword`, `Document.WriteTo(io.Writer)` (and file-path
  `Open`/`Save`, which the engine may never use); `ExtractText` with raw or
  visual reading order and `Page.ExtractTextWithLayout` (per-glyph positions);
  `SearchText` (literal, case-insensitive, or RE2 regex, per-match bounding
  boxes) and `ReplaceText`; `ExtractImages` / `ImageInfos()` and on
  `ImageInfo`: `Extract()`, `Remove()`, `Replace(path)`,
  `ReplaceFromStream(io.Reader)`; `Page.AddImage` / `AddImageFromStream`;
  `NewRedactAnnotation(page, rect)`, `Document.ApplyRedactions()`
  (documented as irreversibly removing text, images and paths in the region),
  `Document.ValidateRedactions()`; `Page.AddText`; a rasteriser.
- **Documented limitations**: `SearchText` and `ReplaceText` match **within a
  single text line only**; `ReplaceText` redraws the replacement at the same
  baseline, size and colour in a metric-compatible Standard-14 face; scanned
  pages are auto-detected and the OCR path returns line-level text with **no
  coordinates**.
- **AI copilots**: the library ships `SummaryCopilot`, `OcrCopilot` (behind
  `MakeSearchable`), `ChatCopilot` and `ImageDescriptionCopilot`, which send
  extracted document text and/or rendered page images to a **configured
  OpenAI-compatible endpoint**. This is the single most dangerous fact in the
  list, and Q7 exists because of it.
- **The C++ product's licensing model** (watermark, four-page evaluation limit,
  `SetLicense`) was confirmed from its own documentation.

Two things could **not** be verified from the documentation and are therefore
**named measurements in 13b**, not assumptions:

- whether `Document.WriteTo` is a **full rewrite** (every object reserialised)
  or an incremental update that appends and leaves the old objects readable.
  Q1's leak proof answers it with bytes.
- the copilots' exact configuration surface (which symbols carry the endpoint,
  and whether anything network-shaped is reachable without explicit
  configuration). 13b builds the Q7 inventory from the vendored source itself,
  which is stronger than any documentation claim.

If 13b finds the module is not what its README claims, the gate fails, the
measurement is recorded in the findings log below, and the change stops with
today's pipeline untouched.

## Ground rules (unchanged, restated in this plan's own words)

- **No CGo, ever. No `purego`. No native artefact of any kind.** The P0 pattern
  is the product, not a preference.
- **The local-only guarantee holds and stays provable.** The application
  performs no network I/O except HTTP to `127.0.0.1:11434`, and only
  `backend/ollama/client.go` may construct such a request. The new library
  contains code that can POST document text to an endpoint; Q7's guard is a
  test with teeth, built in 13b before anything else is wired.
- **The engine stays UI-agnostic.** Documents arrive as `[]byte` plus a
  filename, and nothing under `backend/engine/` reads a user-chosen path. The
  library's `Open`/`Save` file-path APIs are therefore **unusable in the
  engine**: only `OpenStream` and `WriteTo` are permitted, and that is an
  invariant the Q7 guard also enforces.
- **Originals are immutable.** The source file is read once at import and never
  written, moved or modified. In-place replacement operates on a copy of the
  original bytes held in memory (`Document.Raw`), exactly as `exportfmt`
  already does for OOXML.
- **The pipeline is deterministic, `engine.Run` reaches no model, and picture
  decisions are export-time state that `engine.Run` knows nothing about.**
  Nothing in this change touches detection routes, the review gate, or the
  pass order.
- **A change is not finished until its tests move with it**, and both suites
  gate: `go test ./...` and `node --test "frontend/**/*.test.js"`
  (`docs/TESTING.md` owns the tiers and the scoping procedure).
- **Comments explain intent in the present tense.** No comment says what the
  code used to be, which batch changed it, or which change order asked for it.
- **No em dashes in user-visible copy, and no retired route name**
  (`copy_guard_test.go`, `frontend/copy.test.js`). The prose of this document
  is not copy; the strings it quotes are.
- **Every document edit a batch implies is part of that batch and is not
  optional**: `CLAUDE.md`, `backend/CLAUDE.md`, `frontend/CLAUDE.md`,
  `frontend/BRIDGE.md`, `README.md`, `frontend/docs/index.html`.

### The deviation rule

If a step in any order of this family is wrong, contradicted by the code, or
cannot be done as written: **stop, say so, and propose the alternative before
writing it.**

---

## 1. Decisions taken

The table is the summary; each decision's reasoning is the numbered subsection
under §2 it points at. `Source` says whose decision it is: `owner` for a call
the owner made or must make, `mine` for a call this plan makes and argues,
`13b` for a decision that is a measurement before it can be taken.

| # | Decision | Where argued | Source |
|---|---|---|---|
| D1 | The dependency is `aspose-pdf-foss-for-go`, pinned exactly at v0.7.0 and **vendored**; the C++ product and `purego` are rejected and recorded as rejected | above, and Q6 | mine, gated by D15 |
| D2 | Whether `WriteTo` is a full rewrite is proved with bytes in 13b, never assumed; an incremental-update output is an automatic NO-GO | Q1 | 13b |
| D3 | Every export runs a **whole-file leak check**: every stream in the produced PDF is decompressed and scanned for every registry original (string-object encodings included), and the export FAILS naming the surface that leaked. It replaces the body-only `assertNoOriginals` | Q1 | mine |
| D4 | Surfaces the text pass cannot reach are anonymised through the existing metadata review (Info, XMP) or scrubbed as text at export (annotations, form values, outlines), and the rest are **dropped from the produced copy** (embedded attachments, JavaScript actions), each drop reported. AMENDED by F28 (2026-08-22): page thumbnails move to 13d's picture scope, because a `/Thumb` is a rendered PICTURE of the un-anonymised page, not text, and the library at v0.7.0 exposes no API over it | Q1, Q3, F28 | mine; OQ4 confirmed by the owner, 2026-08-21 |
| D5 | Binding is **string-driven**, registry original to placeholder, with a fixed fallback ladder: `ReplaceText`, else redact-and-redraw (`NewRedactAnnotation` + `ApplyRedactions` + `Page.AddText` fitted to the original rectangle down to a floor), else a solid redaction box with no caption; every rung is counted and reported | Q2 | mine |
| D6 | An occurrence that cannot be **located** at all blocks the export with an actionable refusal naming the placeholder and the page; a half-anonymised PDF that looks finished is worse than a refusal. OQ5 is answered: there is NO regenerated-layout fallback behind the refusal, which names the `.md` export as the way out, and `fpdf` leaves | Q2 | mine; OQ5 confirmed by the owner, 2026-08-21 |
| D7 | Extraction stays **page-shaped** (`Document.Pages`, so `PageCount`, `engine.ScanChunks` and `pagescope.go` are untouched); the working markdown remains the body text in reading order; annotations, form field values and outline titles are NOT added to the markdown and are scrubbed at export through the same span machinery, following the existing docx header/footer precedent, with the extra hits reported as the docx `document_extras` warning is | Q3 | mine |
| D8 | The spacing-repair heuristic's fate is a 13b measurement: if the library's layout-aware extraction no longer produces the kerning defect, the repair retires for PDF in 13c; if it does, the repair keeps running over the new extractor's output | Q3 | 13b |
| D9 | A PDF image asset's ID is a **content hash** (`pdf:sha256:<16 hex>`); an occurrence is `Part: "page/<n>"` plus `Ordinal` among that page's image placements. The inventory lists raster image XObjects; vector drawings are content, not assets, and are never offered a control that cannot anonymise them | Q4 | mine |
| D10 | Treatments reuse `imaging.Treat` unchanged; the treated PNG/JPEG bytes go back through `ImageInfo.ReplaceFromStream`. The PDF format table row changes to full image review, `ReasonPDFImagesRemoved` and `copy.js` `pdf_images_removed` retire (with a `vocabulary_guard_test.go` entry), and `image_parity_test.go` moves with them, all in 13d | Q4 | mine |
| D11 | The copy promises what is measured, never "almost identical": replaced words are redrawn in a substitute font and may shrink to fit. PDF **keeps its EXPERIMENTAL label** through 13c and 13d (OQ3 answered: the label stays; revisit after real-document use) | Q5 | mine; OQ3 answered by the owner, 2026-08-21 |
| D12 | Dependency arithmetic is **+1 and −2**, staged: `ledongthuc/pdf` and `go-pdf/fpdf` leave at the END of 13c, after its acceptance criteria pass, **and only after the owner has explicitly confirmed the tests are successful** (the OQ3 answer's decommissioning gate; the owner's tag and release of the pre-change application is the rollback point). If the confirmation has not arrived when 13c's session ends, the new path ships with the old one still present and the removals become a small follow-up commit under the 13c order, applied on confirmation. The module is vendored so its exact source is auditable in-tree; a version bump is never automatic and re-runs the 13b gate checks | Q6 | mine, amended per the owner's OQ3/OQ5 answers, 2026-08-21 |
| D13 | The local-only guarantee gets a **boundary guard test** in the idiom of `vocabulary_guard_test.go`: a forbidden-symbol scan over `backend/`, `frontend/` and `scripts/`, plus a committed inventory of the vendored files that import `net/http`, held unchanged so a version bump cannot widen the network surface unnoticed. Built in 13b as a first-class step, before any measurement that exercises the library | Q7 | mine |
| D14 | Scanned PDFs: the refusal and its exact message stay word for word. The library's OCR runs through the copilot endpoint, which D13 forbids, and returns no coordinates, which in-place replacement needs, so OCR has not arrived and the plan says so once | Q8 | mine |
| D15 | The **adoption gate** (Q10) runs in 13b before any production wiring: measurable GO/NO-GO criteria, counts only, on committed synthetic fixtures plus the owner's two confidential reference documents. NO-GO leaves today's pipeline untouched and records the measurement here as a rejected option, pdfcpu-style | Q10 | 13b; OQ1 answered by the owner, 2026-08-21: risk accepted, 13b may start |
| D16 | `SessionVersion` bumps **13 to 14 in 13d** and nowhere else, with the reason recorded beside the constant; the bump costs the owner every saved session and every saved profile on disk, and this plan says so plainly | Q9 | mine |

---

## 2. The ten questions, answered

### Q1. Where can an original string hide in a produced PDF, and how is that proved?

This is the first question because "change only what must change" pulls
straight towards the failure mode: PDF **incremental updates append new
objects and leave the old ones in the file**, so a minimally-edited PDF can
carry the pre-anonymisation content stream intact and trivially recoverable.

**Decision (D2): `WriteTo`'s behaviour is proved, not assumed.** 13b writes the
test: open a fixture whose content stream carries a sentinel, replace the
sentinel, `WriteTo`, then scan the raw output bytes (every stream inflated) for
the sentinel and for tell-tales of an incremental save (a cross-reference chain
with a `/Prev` pointer to a live original body, a superseded object still
readable). If the output is an incremental update, the gate is a NO-GO unless
the library offers a documented full-rewrite save mode that passes the same
test.

**The surfaces a value can survive in**, and what the export does with each:

| Surface | What the export does |
|---|---|
| page content streams, form XObjects | the in-place text pass (Q2) rewrites them; the leak check reads them back |
| superseded objects from an incremental save | must not exist in the output at all (D2); the leak check reads every object, live or not |
| compressed object streams | inflated and scanned like any other stream |
| Info dictionary | the existing metadata review (`ExtractPDFMetadata` and the review panel), now writing into the produced file's own Info rather than a regenerated one |
| XMP metadata packet | rewritten through the same metadata proposals; if the library cannot rewrite it field-by-field, it is replaced with a minimal clean packet, never copied through |
| annotation `/Contents` and `/RC`, AcroForm `/V`, `/DV`, appearance streams `/AP` | scrubbed as text through `Config.AnonymiseText` (the docx header/footer precedent, Q3); an appearance stream that cannot be rewritten is regenerated or dropped so the field re-renders from its value |
| outline `/Title`, named destinations, page labels, optional-content group names | scrubbed as text through `Config.AnonymiseText` |
| JavaScript actions | dropped from the produced copy and reported (D4) |
| embedded file attachments | dropped from the produced copy and reported (D4): an attachment is an arbitrary inner document the pipeline never read, and carrying it through is a leak wearing a paperclip |
| page thumbnails `/Thumb` | dropped from the produced copy: a thumbnail is a rendered picture of the un-anonymised page, and viewers rebuild thumbnails they need |
| tagged-PDF `/Alt`, `/ActualText`, `/E` | scrubbed as text through `Config.AnonymiseText` |
| pixels of embedded images | the picture review (Q4); a kept picture keeps its pixels by the user's decision, exactly as OOXML does |

**Decision (D3): the body-coverage self-check becomes a whole-file check.**
`exportfmt/pdf.go`'s `assertNoOriginals` re-extracts page text and misses every
non-content surface above. Its replacement decompresses **every stream** in the
produced file and scans the inflated bytes, plus every string and hex-string
object, for every registry original that is not allowlisted, in the encodings a
PDF can carry one (literal string with escapes, hex string, UTF-16BE). It fails
the export with an actionable message naming the **surface** that leaked (the
object type or dictionary key), using `redactTerm` so the message identifies
the culprit through its placeholder without repeating the value. It is modelled
on `TestExportedArchiveKeepsNoOriginalBytes`, which checks every entry of a
produced archive rather than the part it expects to be wrong, and like it, the
check is both the test oracle and a runtime guarantee: a leaky file is never
shipped silently. The check is written in 13b (it is the gate's own
instrument) and becomes the blocking self-check of 13c's export.

The check's honest limits are stated where it lives: it proves the absence of
the registry's originals as text; it cannot prove anything about a picture's
pixels (that is the review's job) or about a value the user never accepted
(that was never the export's contract).

### Q2. How does in-place replacement bind to what the pipeline decided?

The engine works in offsets into the converted markdown; the injector works on
the PDF's own text. Offsets do not survive the conversion, so **the binding is
string-driven (D5)**: the same inputs the OOXML same-format export already
uses, `exportfmt.Config` (values, patterns, categories, allowlist, registry),
applied to the PDF's own extracted text per page. What to replace is decided by
running the same span machinery over the page text (`Config.Replacements`),
which is what guarantees the placeholders match the markdown export exactly,
through the same session registry.

**The fallback ladder, per occurrence:**

1. **`ReplaceText`** where the occurrence sits on one line and the replacement
   fits the line's geometry. The library redraws at the same baseline, size and
   colour in a metric-compatible face; 13b measures what "fits" means in
   practice (what the library does when `[PERSON_1]` is wider than "Jean": 13b
   watches for overflow into the neighbour with the rasteriser).
2. **Redact and redraw**: `NewRedactAnnotation` over the occurrence's bounding
   box (from `SearchText`), `ApplyRedactions`, then `Page.AddText` drawing the
   placeholder fitted to the original rectangle, shrinking down to a floor
   (6 pt, or 60% of the original size, whichever is larger; the exact floor is
   a 13b measurement of legibility on the rasterised result).
3. **A solid redaction box with no caption**, when even the floor does not fit.
   The mapping still records the placeholder; the box is the redaction. Never a
   silent overlap, never a clipped placeholder fragment: a box that says
   nothing is honest, `[PERSO` is not.

**The three breaks, and their policies:**

- **A value the spacing-repair heuristic made matchable** ("B R IDDING ULES"
  collapsed to "BIDDING RULES") does not exist as a literal run in the PDF.
  Matching is two-tier: the literal first, then a derived RE2 pattern that
  tolerates the repairs the converter applied (optional single spaces at the
  seams the repair collapsed, doubled spaces read as one). 13b measures how
  often the second tier fires on the reference documents, and whether the
  library's own layout-aware extraction makes the problem disappear (D8).
- **A value wrapped across two lines** cannot be matched by `SearchText` at
  all (documented single-line limit). The ladder gains a wrapped-match step:
  split the value at each whitespace point, find a line ending with the head
  and the next line starting with the tail (per-glyph layout gives the
  geometry), then redact both fragments and draw the placeholder over the
  first. If the library's primitives cannot support that reliably, the
  occurrence is UNLOCATED (below). 13b counts wrapped occurrences on the
  reference documents so 13c knows whether this step is load-bearing or rare.
- **A placeholder is usually longer than the original** and a PDF has no
  reflow. That is the ladder itself: fit, shrink to the floor, box. Rung
  counts are reported per document (replaced in place, redrawn smaller, boxed),
  in the export review panel and the run report's warnings, so the user knows
  what the produced file looks like before opening it.

**An occurrence that cannot be located blocks the export (D6).** The refusal
names the placeholder (via `redactTerm`), the page, and the way out (export as
`.md`). It cannot be a warning: the Q1 whole-file check would fail on the
surviving original anyway, and a half-anonymised PDF that looks finished is
worse than a refusal. OQ5 is answered (§9): the old regenerated export does
NOT remain as a fallback behind the refusal, so `fpdf` leaves and the Q6
arithmetic is +1 and −2.

### Q3. What must extraction now cover?

Anything the injector can reach but detection never read is a value the user
was never offered. The inventory of reachable surfaces is Q1's table; the
question here is what feeds **detection**.

**Decision (D7):**

- **Page text** comes from the library's per-page extraction and stays the
  working markdown, split into `Document.Pages` exactly as today, so
  `Document.PageCount`, `engine.ScanChunks` and `engine/pagescope.go` are
  untouched and the local-model slicing keeps addressing real pages. A PDF has
  no separate header/footer parts: running heads are painted on the page, so
  per-page extraction already reads them (it always did; nothing was dropped
  for PDF the way docx drops header parts). Reading order: the library's
  visual-order mode, measured in 13b against the current extractor's output.
- **Annotations, form field values, outline titles** are NOT appended to the
  working markdown. They are scrubbed at export through the same span
  machinery (`Config.AnonymiseText`: the accepted Values, the custom patterns,
  pass 1's active categories, the registry post-pass, the allowlist veto).
  This follows the repository's own precedent exactly: docx headers, footers
  and footnotes are not in the working form either, and the same-format export
  replaces text in them and reports the extra hits (`document_extras`). The
  honest limit carries over too and is stated in the docs: a value that occurs
  ONLY in an annotation and never in body text is caught by the patterns and
  the registry, not by discovery, because discovery never read it. Appending
  a pseudo-page of annotations to the markdown was considered and rejected: it
  changes what a "page" means to the scan scope, puts text in the preview that
  is not on any page, and buys detection coverage for a surface that is rare
  in this application's document population.
- **Metadata** (Info, XMP) already has its review surface
  (`GetSameFormatMetadata`); the XMP packet joins the Info dictionary in it.
- **Attachments and JavaScript** need no detection because the produced copy
  drops them (D4).
- **The spacing-repair heuristic** (D8): 13b runs both extractors over the
  fixtures and the reference documents and counts kerning artefacts. If the
  library's layout-aware extraction does not produce them, `RepairPDFText`
  retires for PDF in 13c (its tests move with it); if it does, the repair runs
  over the new extractor's output unchanged. The ligature fold stays either
  way unless 13b shows the library already folds presentation forms.

### Q4. What is a PDF image asset, and does it fit the existing model?

The four words hold (asset, occurrence, treatment, status), and so does "one
decision per ASSET, applied everywhere it appears".

**Asset identity (D9).** An OOXML asset's ID is the archive part path: the
storage address, stable across re-imports. A PDF image XObject's nearest
analogue, the object number, is stable across re-imports of the same file
(originals are immutable, so the bytes never change) but is renumbered by any
rewrite, and it names a storage slot rather than a picture: the same logo
embedded twice is two numbers. The ID is therefore a **content hash**,
`pdf:sha256:<first 16 hex>` of the XObject's decoded image bytes:

- stable across re-imports (same bytes, same hash), which is what the session
  file's persisted decisions require;
- stable across the export's own rewrite, so the picture pass can re-find its
  assets in bytes the text pass has finished with, exactly as PART plus
  ORDINAL does for OOXML;
- one ID for one picture wherever it is embedded, so "the logo" is one row and
  one question even when the producer embedded it as two XObjects, which is
  the model's own rule (a logo on five slides is one asset);
- collision-proof in practice (SHA-256 over at most a few megabytes of image
  data), and the `pdf:` prefix keeps it from ever colliding with an archive
  part path in the shared decision store.

**Occurrence.** `Part` is `"page/<n>"` (the location string stays ready to
print: "Page 4"), `Ordinal` is the occurrence's index among the image
placements of that page, re-derived by re-scanning, never a byte offset: the
same rule and the same reason as OOXML. `Kind`: a page image placement is
`picture`; if the library exposes none of the fill/background distinction, PDF
occurrences are all `picture` and the existing kinds list is untouched.

**Treatments (D10).** `imaging.Treat` is reused unchanged: the library's
`ImageInfo.Extract()` decodes to PNG or JPEG, which is exactly what `Treat`
takes and returns, and `ImageInfo.ReplaceFromStream` writes the treated bytes
back. `remove` maps to `ImageInfo.Remove()` (the placement goes) AND a
byte-overwrite where the object would otherwise survive, holding invariant 1
(the original pixels always leave the file); the Q1 whole-file check cannot
see pixels, so 13d's export test extracts the produced file's images and
asserts the original bytes are gone, in the mould of
`TestExportedArchiveKeepsNoOriginalBytes`.

**Blocked treatments.** A PDF "picture" that is really a **vector drawing** is
content-stream path operators, not an image XObject: `ImageInfos()` does not
list it, so no control is ever drawn for it, and the rule "a control that does
not anonymise is never labelled anonymise" is kept by construction rather than
by a disable. The SVG-blur analogue therefore mostly disappears for PDF. Two
edge classes are 13b measurements: **inline images** (`BI..EI` operators) and
exotic filters (JPX, CCITT) the extractor may not decode; whatever the library
cannot list or cannot replace gets a warning code on the banner (the
`unreadable_part` pattern) rather than a silent absence, and its treatments
render disabled with the reason, exactly as `FormatOther` does today.

**What moves when PDF gains the review (all 13d):** `CLAUDE.md` §5's format
table row for `.pdf` changes from "not offered, one explanatory line" to full
review of raster image XObjects; `imaging.ReasonPDFImagesRemoved` and
`copy.js` `IMAGES.reason.pdf_images_removed` retire, with
`vocabulary_guard_test.go` entries so neither can come back;
`image_parity_test.go`, `frontend/anonymiseimages.test.js`, `api.test.js` and
`state.test.js` fixtures that use the code move with it; `BRIDGE.md`'s images
section and the export review panel's `images` field start covering PDF.

### Q5. What may the copy promise?

**Decision (D11): the copy promises what 13b measured, never "almost
identical".** The honest sentence has three parts: the layout is the
original's; replaced words are redrawn in a substitute font and may shrink to
fit; a replacement that cannot fit becomes a plain box. Concretely, after 13c:

- `EXPORT.pdfCaption`: "Experimental: keeps the original layout. Replaced text
  is redrawn in a substitute font and may shrink to fit. Your source file is
  not changed." (No em dash; final wording set in 13c beside its copy tests.)
- `IMPORT.experimentalTooltip`: stays about extraction and review; reworded in
  13c only if the extractor actually changes what the user should check.
- The EXPERIMENTAL label **stays** on import and export through 13c and 13d.
  Dropping it is a product judgement on real-document experience, not a test
  result, so it is the owner's decision (OQ3) taken after the batches have run.

### Q6. Does the dependency count go down?

**Decision (D12): yes, +1 and −2, and it is real, staged behind proof.**

- `github.com/ledongthuc/pdf` leaves when 13c has replaced its three jobs:
  import extraction (`convert/pdf.go`), Info-dictionary reading
  (`exportfmt/pdf.go ExtractPDFMetadata`), and the export self-check's reader
  (subsumed by the Q1 whole-file check).
- `github.com/go-pdf/fpdf` leaves when 13c's in-place export replaces
  regeneration. OQ5 is answered (§9): regeneration keeps NO fallback role, the
  refusal names the `.md` export as the way out, and a second PDF writer kept
  for a rare failure path would be a maintenance cost with no reviewer.
- Removal happens at the **end of 13c**, after its acceptance criteria pass
  and after the owner has explicitly confirmed the tests are successful (the
  OQ3 answer's decommissioning gate, D12), never in 13b: the replacement is
  proven, and the owner's tag and release of the pre-change application stands
  as the rollback point, before the incumbent leaves.
- **The module is vendored** (`go mod vendor`): the exact source is auditable
  in-tree, cannot move underneath the build, and is what the Q7 inventory is
  computed from. The audit layer keeps working: `govulncheck` analyses the
  module list and source regardless of vendoring; `golangci-lint` and
  `deadcode` skip `vendor/` by default, and `Taskfile.yml`'s audit targets are
  checked in 13b to confirm none of them walks `vendor/` by accident.
- **`CLAUDE.md` §7 gains rows** (written in 13b): the module, pinned at
  v0.7.0, with the reason for the pin (pre-1.0, moving fast, gate-verified at
  exactly this version) and the rule that **a version bump is never
  automatic**: a bump re-runs the 13b gate (the boundary inventory, the leak
  proof, the extraction counts) before it lands. A separate row for the
  bundled OFL 1.1 fonts (Arimo, Tinos, Cousine, Carlito), in the shape of the
  Material Symbols and font8x8 rows: data with a licence, not code with a
  dependency. And the pdfcpu-style rejection row for `aspose-pdf-go-cpp`.

### Q7. How is the local-only guarantee kept provable?

The library brings code into the module that can POST document text and
rendered pages to a configured endpoint. That is not a reason to reject it; it
is the reason the guard must be a **test**, not a promise in a comment.

**Decision (D13): a root guard, `pdf_boundary_test.go`, in
`vocabulary_guard_test.go`'s idiom, built in 13b as a first-class step, with
three assertions:**

1. **Forbidden symbols.** A whole-token source scan over `backend/`,
   `frontend/` and `scripts/` that fails on any reference to the library's
   network-capable surface: `SummaryCopilot`, `OcrCopilot`, `ChatCopilot`,
   `ImageDescriptionCopilot`, `MakeSearchable`, and every other exported
   symbol declared in a vendored file that imports `net/http` (the exact list
   is generated from the vendored source in 13b and committed as the guard's
   own table, with each entry's reason). A symbol never referenced is code the
   Go linker drops, so this also keeps the copilot machinery out of the
   binary.
2. **The vendored network inventory.** The test enumerates every file under
   `vendor/github.com/aspose-pdf-foss/aspose-pdf-foss-for-go/` whose import
   block names `net/http` (or `net`, or `net/url` beside an HTTP client
   construction) and compares the list against a committed inventory. A
   version bump that widens the network surface fails the build until the
   inventory is deliberately re-reviewed and re-committed, with the review
   recorded in the findings log.
3. **The engine path ban.** The same scan forbids the library's file-path
   entry points (`.Open(`, `.Save(`, `OpenWithPassword(` on the library's
   package) anywhere under `backend/`: the engine takes bytes and returns
   bytes, so only `OpenStream`, `OpenStreamWithPassword` and `WriteTo` may
   appear.

The one-file Ollama boundary is untouched: `backend/ollama/client.go` remains
the only file constructing an HTTP request, and the guard above is what makes
that statement checkable now that a second HTTP-capable package is in the
module.

### Q8. What happens to a scanned PDF?

**Decision (D14): nothing changes.** The rejection message stays word for word
(`convert.ErrScannedPDF`, asserted by test): the library's OCR path runs
through the copilot endpoint, which Q7 forbids, and returns line-level text
with no coordinates, which in-place replacement cannot use, so OCR has not
arrived and nobody reading the library's feature list should think otherwise.
The refusal fires exactly as today: extraction yielding no text on any page.

### Q9. What does this cost the user's saved files?

**Decision (D16): `SessionVersion` bumps 13 to 14 in 13d, and only there.**
13b persists nothing. 13c changes no persisted schema: in-place replacement
adds no setting and no new session field. 13d makes a PDF picture decision
storable, and that is the v8-to-v9 failure shape all over again: a v14 file
carrying a PDF asset decision, read by a pre-13d build, loads cleanly and
silently never applies the decision, so the exported document ships a picture
the user had redacted. The reason is recorded beside the constant in
`backend/engine/session.go` exactly as the previous bumps are. Said plainly:
**the bump costs the owner every saved session and every saved profile on
disk**, there is no migration table and no compatibility alias, and that price
is paid once, in 13d, not dribbled across batches.

### Q10. What is the adoption gate, and what happens if it fails?

A pre-1.0 library with 3 stars, reading confidential client documents, is a
real risk. The mitigations are structural (vendored and pinned source, the Q7
guard, `OpenStream` on bytes the App already holds, the Q1 whole-file check on
everything it writes) and procedural (this gate). Whether that is enough for
client documents at all is the owner's question OQ1, asked before 13b runs.

**Decision (D15): GO/NO-GO criteria, measured in 13b, before any production
wiring.** All measurements on the owner's reference documents are reported as
counts, never as the strings found.

| # | Criterion | GO threshold |
|---|---|---|
| G1 | `WriteTo` is a full rewrite: the leak probe finds no superseded object and no pre-edit bytes in the output | hard requirement |
| G2 | The Q1 whole-file leak check exists, finds every planted sentinel (content stream, Info, XMP, annotation, appended incremental body), and passes clean on a real replaced output | hard requirement |
| G3 | The Q7 boundary guard exists, is red-green demonstrated, and the vendored network inventory is committed | hard requirement |
| G4 | Extraction parity: detection over the library's extraction finds at least as many values as over `ledongthuc/pdf`'s, on both reference documents and on the committed fixtures (counts per category) | no regression |
| G5 | Round-trip fidelity: open then `WriteTo` with no edits preserves page count, extractable text and image inventory; the rasterised before/after differs by zero pixels, or the differences are enumerated and judged cosmetic | measured, judged in the GO/NO-GO note |
| G6 | `ReplaceText` on the fixtures: original absent, placeholder present, rasterised neighbours untouched outside the replaced rectangle; the overflow behaviour for a longer replacement is observed and recorded | measured; silent overlap of a neighbour is a NO-GO for rung 1 (the ladder then starts at rung 2) |
| G7 | Ladder coverage on the reference documents: counts per rung, and the count of UNLOCATED occurrences | UNLOCATED must be rare enough that D6's refusal is an edge case, judged in the note |
| G8 | Wall clock: import and export of the reference documents, against the current baseline measured first | budgets set from the baseline in 13b and recorded in the findings log; deep tier |
| G9 | A damaged file errors actionably (no panic escapes), an encrypted file is refused with the remove-the-password message, a scanned file gets the exact `ErrScannedPDF` message | hard requirement |

**Rollback:** today's pipeline stays in place and untouched until the gate
passes; 13b wires nothing into the product and changes no user-visible
behaviour. If the gate fails, the measurement is recorded in the findings log
below, the module and its vendor tree are removed again, and the rejection is
recorded in `CLAUDE.md` §7 exactly as the pdfcpu row records one, with the
failing criteria as the reasons.

---

## 3. Before and after

Before (today):

```
import   .pdf --ledongthuc/pdf--> per-page text --RepairPDFText--> markdown + Pages
                                                     (pictures: not extracted)
anonymise  markdown through the pipeline; IMAGE tab: "already removed", one line
export   .pdf --fpdf--> REGENERATED simplified PDF from anonymised text
                        (pictures dropped, layout lost, Info reviewed,
                         body-only self-check via ledongthuc)
```

After (13c + 13d):

```
import   .pdf --aspose-pdf-foss OpenStream--> per-page text --> markdown + Pages
              --ImageInfos/Extract--> picture inventory (13d), assets by content hash
anonymise  markdown through the pipeline, unchanged;
           IMAGE tab: one decision per asset, imaging.Treat previews (13d)
export   copy of original bytes --in-place replacement-->
           text: ReplaceText / redact-and-redraw / box, per the ladder
           surfaces: Info+XMP reviewed; annotations, forms, outlines scrubbed;
                     thumbnails, attachments, JavaScript dropped and reported
           pictures: treated bytes via ReplaceFromStream (13d)
           then the WHOLE-FILE leak check, blocking --> .pdf out
```

---

## 4. Fixtures and measurement rules

- There is no PDF fixture with pictures in the repository today. 13b creates
  synthetic fixtures under `backend/testdata/`, produced by a **committed
  generator** (integration tier, the `convert` package's `fixture(...)`
  precedent), so their provenance is checkable: multi-page prose with names in
  English and French, a value wrapped across a line break, a JPEG placed on
  two pages, a PNG, an Info dictionary and XMP packet carrying a name, an
  annotation carrying a name, an outline title carrying a name, and a page
  thumbnail. The generator may use the library itself (it is test-side code);
  what it may not be is a binary blob nobody can regenerate.
- The owner's real reference documents (the 2-page email-thread PDF and the
  15-slide deck) are **confidential, live outside the repository and are never
  committed**. The deep-tier gate tests read their paths from an environment
  variable and `t.Skip` with an actionable message when it is unset.
  Measurements taken on them are reported as **counts, never as the strings
  found**. No name, company or acronym from them may reach a fixture, a
  comment, a commit message, a document in `docs/` or a memory.
- Deep-tier work carries wall-clock budgets (`docs/TESTING.md`); the numbers
  are set from the 13b baseline and live in the findings log.

---

## 5. The batch map

Three batches, each sized to one session. 13b's order is written
(`docs/change-13b.md`); 13c and 13d are **scoped here and written only after
13b's GO**, because a detailed order written before the gate is a document
that will be rewritten rather than followed.

### 13b — the adoption gate (order: `docs/change-13b.md`)

**Scope:** add, pin and vendor the module; build the Q7 boundary guard and the
Q1 whole-file leak check; create the fixture generator; measure everything in
Q10 against the fixtures and the reference documents; wire **nothing** into
the product and change **no** user-visible behaviour; exit with a written
GO/NO-GO appended to this document.

**Gate:** the GO/NO-GO table of Q10. **Acceptance:** both suites green, the
production binary provably unchanged (the module is imported only by test
files and the guard proves the boundary), the GO/NO-GO note appended here with
every measurement as counts, and on NO-GO the module removed and the rejection
recorded.

### 13c — text in place

**Scope:** import extraction moves to the library (D7, D8); the regenerated
PDF export is replaced by in-place replacement with the fallback ladder (D5,
D6) and the whole-file leak check as a blocking self-check (D3); the
non-content surfaces are scrubbed or dropped per Q1's table (D4); the copy and
the EXPERIMENTAL labels updated per D11; the dependency removals earned under
D12 (`ledongthuc/pdf`, and `fpdf` subject to OQ5); every implied document edit
(`CLAUDE.md` §5's PDF rules and §7's table, both charters, `BRIDGE.md`,
`README.md`, `frontend/docs/index.html`).

**Revisions forced by 13b's findings (2026-08-21):** the export's save is
`RemoveUnusedObjects()` then `WriteTo`, never `WriteTo` alone, and the
naked-`WriteTo` pin stays in the suite (F5); rung 2 constructs the redact
annotation, `Add`s it to the page's annotation collection and draws the
placeholder through `SetOverlayText` with an explicit white colour, all one
gesture (F6, F7); rung 1 accepts an occurrence only after measuring the
replacement's grown width against the line's other fragments, else falls to
rung 2 (F10); `RepairPDFText` stays and runs over the new extractor's output
(F11, D8 resolved); the import distinguishes a 0-page salvage of a damaged
file from a scanned one so `ErrScannedPDF`'s wording is never shown for
truncation damage, and keeps a cheap recover shield (F8).

**Revisions forced by the owner's deep run (2026-08-22):** the ladder is
FRAGMENT-AWARE or it does not work on a real deck. Two rules, and F19 and F20
are why each exists.

First, **extraction must not manufacture a value.** Reading-order grouping puts
fragments that merely share a baseline on one extracted line, and the detector
then reads text from opposite ends of a slide as one name. 13c splits an
extracted line where the horizontal gap between two fragments exceeds a
plausible word space, using the rectangles `ExtractTextWithLayout` already
returns, so a value offered to detection is text that was actually adjacent.

Second, **an occurrence is located by fragments, never by one string.**
`SearchText` matches inside ONE text-showing operation, so a value split across
two draw operations is invisible to it even on a single line and even with no
wrap involved. The ladder gains a rung beneath the wrapped one: walk the page's
fragments, match the value across consecutive fragments, and redact the union
of their rectangles. That rung also covers F20's case, where the pipeline holds
a spelling that no extraction contains verbatim, because it matches fragment by
fragment rather than against the pipeline's string.

Third, **D6's refusal counts only what the fragment walk cannot find.** A
refusal that fires on a manufactured value refuses an export for a leak that
does not exist, which is worse than not offering the feature.

**Gate before it runs:** MET. The owner's deep run is done (2026-08-22) and its
numbers are in the findings log: G4, G5 and G8 pass on the reference documents
and G7 does not (§11, F17 to F21). The owner accepted the fragment-aware
enlargement above on 2026-08-22, which was the remaining entry condition, and
the order is written as `docs/change-13c.md`. OQ2, OQ4 and OQ5 are already
answered (§9). F22's Windows build break is NOT an entry condition for writing
the order, but it must be fixed before this batch's acceptance can be checked
on the target platform.
The removals carry their own second gate per D12: `ledongthuc/pdf`, `fpdf` and
the regenerated-export code leave only after the owner has explicitly
confirmed the tests are successful, with the owner's tag and release of the
pre-change application as the rollback point; until that confirmation the new
path ships beside the old code and the removals wait as a follow-up commit
under this order.
**Acceptance (to be sharpened by 13b's findings):** both suites green; the
framework-agreement suite untouched and green; the exported PDF of every
fixture passes the whole-file check; ladder rung counts reported in the export
review panel and the report; after the owner's confirmation, `go.mod` no
longer names `ledongthuc/pdf` or `fpdf` and a grep for the removed imports
returns nothing outside `docs/`; `SessionVersion` still 13 and every existing
session file still loads; scanned-PDF refusal byte-identical.

### 13d — pictures in the PDF

**Scope:** the PDF image scanner behind `ListDocumentImages` (D9); treatments
through `imaging.Treat` and `ReplaceFromStream`, with remove holding invariant
1 and a pixels-gone export test (D10); previews through the existing
`imaging.Preview` path; the format-table row change, the retirement of
`ReasonPDFImagesRemoved` and `pdf_images_removed` with their
`vocabulary_guard_test.go` entries; `image_parity_test.go` and the frontend
image tests moving with it; the export review panel's `images` field and the
report's picture section covering PDF; `SessionVersion` 13 to 14 with its
reason line (D16); every implied document edit.

**Revisions forced by 13c's findings (2026-08-22):** page thumbnails join this
batch's scope (F28, amending D4): a `/Thumb` is a rendered picture of the
un-anonymised page, the library at v0.7.0 exposes no API over it, and the text
export cannot reach it, so 13d either drops it through the picture plumbing it
builds or records the library gap with its mitigation; a produced PDF carrying
a thumbnail of the un-anonymised page fails the batch. And the retirement of
`ReasonPDFImagesRemoved` / `pdf_images_removed` (D10) inherits a changed
sentence: since 13c the copy behind the code states that pictures PASS THROUGH
unchanged (F29), so the retirement replaces that sentence with the real
review, not the old already-removed claim.

**Revisions forced by 13b's findings (2026-08-21):** the twice-placed asset
question is settled with evidence: `ImageInfos()` lists per PLACEMENT and the
shared object decodes identically from both, so the content-hash identity (D9)
carries unchanged. Inline images ARE listed (`Inline: true`), so 13d must
either verify `ReplaceFromStream`/`Remove` semantics on an inline image or
give inline occurrences a warning code with disabled treatments, per Q4's
`unreadable_part` pattern (F9).

**Gate before it runs:** 13c finished and reconciled; 13b's measurements on
`ImageInfos` coverage (inline images, exotic filters) folded into its steps.
**Acceptance (to be sharpened):** both suites green; the parity guards moved;
a produced PDF whose every asset was treated contains none of the original
image bytes (the archive-test mould); a v13 session file is refused with the
actionable both-versions message; the IMAGE tab reviews a PDF's pictures with
one decision per asset.

---

## 6. Batch status

One row per batch; updated by every batch's reconciliation step, which is an
obligation and not a suggestion.

| Batch | State | Session | Outcome |
|---|---|---|---|
| 13 (this plan) | done | 2026-08-21 planning session | plan and 13b order written; owner answered OQ1 to OQ5 the same day (§9), 13b cleared to start |
| 13b | done, CONDITIONAL GO; reference halves measured 2026-08-22 | 2026-08-21 implementation session, 2026-08-22 owner's deep run | every criterion measurable without the reference documents PASSES (G1, G2, G3, G9 hard requirements; G4, G5, G6 fixture halves; G7 prototyped). On the reference documents G4, G5 and G8 PASS and G7 does NOT: 10 of 48 occurrences on the 15-slide deck are UNLOCATED, so the gate reopens on that criterion and 13c's ladder scope grows (§11, findings F17 to F21) |
| 13c | implemented, steps 1 to 10 and 12; step 11 GATED and not run; acceptance criterion 3 (the G7 census) packaged for the owner | 2026-08-22 implementation session | the fragment line model, the fragment-aware ladder, the in-place export with its refusal and blocking leak scan, the non-content surfaces, the copy, the panel counts and every implied document edit are in (findings F23 to F29). The dependency removals wait for the owner's tests-successful confirmation (D12), so `ledongthuc/pdf`, `fpdf` and the regenerated exporter ship beside the new path with no production caller. The G7 census is an ASSERTION in `pdffoss_gate_deep_test.go` (UNLOCATED must be 0); the implementing environment holds no reference documents (F4), so the owner's `task test:deep` run is the verification |
| 13d | scoped, not written; scope revised by 13c (F28, F29) | | order to be written after 13c's owner verification; scope revised by 13b's findings (see §5) and by 13c's: page thumbnails join the picture scope (F28), and the `pdf_images_removed` retirement inherits a changed sentence (F29) |

---

## 7. Findings log

Append-only. What the implementation discovered that the plan did not know,
with the decision each finding forced. Measurements land here, as counts.

| # | Found in | Finding | Decision forced |
|---|---|---|---|
| F1 | planning, 2026-08-21 | `frontend/BRIDGE.md`'s session-file paragraph still says "schema version 9" while `SessionVersion` is 13 (`backend/engine/session.go`, `CLAUDE.md` §5) | the correction rides with 13d's `BRIDGE.md` edits, which touch that section anyway for the version bump; no separate batch |
| F2 | planning, 2026-08-21 | `WriteTo` full-rewrite behaviour and the copilots' exact configuration symbols could not be verified from documentation | both became 13b measurements: G1 proves the save with bytes, and the Q7 symbol table is generated from the vendored source rather than from the README |
| F3 | owner review, 2026-08-21 | the owner answered OQ1 to OQ5 (§9) and added a constraint the plan did not have: a tag and a release of the pre-change application exist as the rollback point, and the old PDF path may be decommissioned only after the owner explicitly confirms the tests are successful | D12 and the 13c scope amended: the dependency removals and the deletion of the regenerated export are gated on that confirmation, shipping the new path beside the old code until it arrives |
| F4 | 13b, 2026-08-21 | the implementing session's environment does not hold the reference documents (the env variables are unset there), so step 1's baseline and step 9's measurements could not run in-session | the deep test measures the INCUMBENT's baseline in the same run as the library's numbers, so the G4/G8 comparison is same-machine by construction; the G8 budgets are encoded in the test (import within 3x the incumbent; export within 30 s per document); the owner runs `task test:deep` with `DOC_ANONYMISER_REFERENCE_PDF` and `DOC_ANONYMISER_REFERENCE_DECK` set, and that run is 13c's entry gate |
| F5 | 13b, 2026-08-21 | `WriteTo` is a full rewrite of a SINGLE body (no second `%%EOF`, no `/Prev` chain), but it serialises every object in the document's table, INCLUDING one an edit orphaned: after `ReplaceText` the old content stream survives unreferenced and readable. `RemoveUnusedObjects()` (a documented library API; it removed 1 to 3 objects per edited fixture) clears it, and the leak scanner then finds nothing | the save discipline is `RemoveUnusedObjects()` before every `WriteTo`; D2's escape clause is satisfied (a documented save path that passes the leak test). BOTH halves are pinned by test: the discipline's output is scanned clean, and a naked `WriteTo` is asserted to still retain the orphan, so neither the discipline nor the library behaviour can drift unnoticed. Recorded in the `CLAUDE.md` §7 pin row |
| F6 | 13b, 2026-08-21 | `NewRedactAnnotation` builds an UNBOUND annotation; `ApplyRedactions` silently does nothing for one that was only constructed. It applies only after `page.Annotations().Add(...)` | 13c's rung 2 must keep construct-and-Add as one gesture; the gate tests do |
| F7 | 13b, 2026-08-21 | a placeholder drawn over the redaction box with `AddText` in the default colour is extractable but INVISIBLE (black on the black box; measured on the rasterised artefact). The annotation's own `OverlayText` is applied by `ApplyRedactions`, but the APPLY path draws it black unless the style names a colour (only the mark-mode preview defaults to a contrasting one) | rung 2 draws the placeholder through `SetOverlayText` with an EXPLICIT white `TextStyle.Color`, in the same gesture as the redaction; the committed artefacts `backend/testdata/golden/pdf_gate_redact_before.png` / `_after.png` show the result (OQ2's evidence) |
| F8 | 13b, 2026-08-21 | a truncated file never panics and never invents content: the library reconstructs the xref and salvage-opens with the pages still intact in the bytes (3/2/1/0 of 3 pages at 90/60/30/10% cuts); a non-PDF errors (`parse PDF: startxref not found`); an encrypted file errors distinguishably (`PDF is encrypted; use OpenWithPassword`) and opens with the password | G9 PASSES. Two 13c consequences: keep a cheap recover shield anyway (the incumbent's precedent), and distinguish a 0-page salvage of a DAMAGED file from a scanned one, because `ErrScannedPDF`'s wording would mislead there |
| F9 | 13b, 2026-08-21 | `ImageInfos()` lists images per PLACEMENT (the shared JPEG object appears on both pages), decodes to identical bytes from both placements, and DOES list inline images (`Inline: true`). `Extract()` output feeds `imaging.Treat` unchanged; `ReplaceFromStream` and `Remove()` leave none of the original stream bytes in the produced file (probed with a 64-byte chunk) | D9 confirmed: asset identity must be the content hash, exactly as decided. 13d gains a decision: inline images are LISTED, but `ReplaceFromStream`/`Remove` operate on XObjects, so 13d must verify the same operations on an inline image or give it a warning code and disabled treatments (the `unreadable_part` pattern) |
| F10 | 13b, 2026-08-21 | `ReplaceText` redraws the longer placeholder at the SAME size and grows RIGHTWARD (59.0 pt to 123.5 pt on the fixture); it does not shrink to fit, and nothing outside the grown rectangle changed (0 differing pixels) | rung 1 is safe only when the grown rectangle stays clear of same-line neighbours: 13c's ladder must measure the replacement's width against the line's other fragments BEFORE accepting rung 1, and fall to rung 2 otherwise. Silent overlap was not observed on the fixture, but the fixture's value sits at line end; the check is the guarantee, not the observation |
| F11 | 13b, 2026-08-21 | D8 resolved: the library's extraction reproduces the SAME interleaved-capitals artefact on the kerning fixture (2 repairable lines in both extractors), and `RepairPDFText` is idempotent and harmless over the library's output | the spacing repair does NOT retire: it keeps running over the new extractor's output in 13c, unchanged, exactly as D8's second branch says |
| F12 | 13b, 2026-08-21 | Go vendoring is all-or-nothing: `go mod vendor` vendored the WHOLE dependency graph (~32 MB: Wails, excelize and their transitive deps beside the PDF library), and builds now run in vendor mode. The library's `ai` subpackage is NOT vendored, because nothing imports it, and the boundary guard asserts that absence | accepted as the cost of D1's auditable-in-tree requirement; the audit layer is unaffected (`./...` never expands into `vendor/`, golangci-lint skips it by default, deadexports walks `frontend/` only), so step 2.3 needed no configuration edit |
| F13 | 13b, 2026-08-21 | `deadcode` (which deliberately runs without `-test`) will flag `exportfmt/pdfscan.go`'s exported functions until 13c wires them into the export path | the recorded exemption path is the audit layer's own: dismiss the code-scanning alert with a comment naming `docs/change-13.md` (the `docs/audit.md` procedure); no tool configuration is edited, and the alert disappears when 13c lands |
| F14 | 13b, 2026-08-21 | two integration tests fail on pristine `main` (`TestDetectionAlwaysEndsWithATerminalEvent`, `TestDetectionProgressNeverGoesBackwards` in `backend/app_detect_integration_test.go`), reproduced in a clean worktree of HEAD with no 13b change present. Root cause, found while triaging this PR's CI: `runSmartPhase` (`backend/app_detect.go`) calls `report(...)` only inside its heuristic loop, so with heuristic discovery OFF (the shipped default) and only signal-based discovery on, PhaseRules runs SILENTLY: zero progress events, which is exactly what both tests assert against. Proposed one-event patch: before `DiscoverFromSignals`, `report(DetectionProgress{DocIndex: max(0, len(docs)-1), DocCount: len(docs)})`, positioned at the end of the phase's document walk so the fraction never rewinds after the heuristic loop's per-file events | pre-existing and OUTSIDE this batch's original scope: the fix changes user-visible progress behaviour, which criterion 8 forbids, so it was first reported with the proposed patch instead of pushed. The owner then explicitly approved applying it inside this batch's PR ("Ok to patch integration red", 2026-08-21), so the one-event patch landed in `runSmartPhase` (reporting the phase's last document, so the fraction never rewinds), and `llmOnlyApp` moved with it: its comment promised "the offline route off" while only heuristic was off, so the silent rules phase had been passing it by accident; it now switches the signal readings off too, matching its own contract |
| F16 | 13b, 2026-08-21 | `ci.yml`'s gofmt step ran `gofmt -l .`, which walks the new `vendor/` tree, and vendored upstream files are not all gofmt-clean, so the unit job failed on files this repository does not own. This is step 2.3's tool-walking-vendor class, found by CI rather than the audit run because gofmt lives in `ci.yml`, not the audit layer | the step is scoped to first-party files (`git ls-files '*.go'` minus `vendor/`), with the reason in the workflow comment: vendor/ holds upstream code exactly as shipped, which is what makes it auditable, so reformatting it would defeat the pin |
| F15 | 13b, 2026-08-21 | the wrapped value is confirmed NOT findable whole (`SearchText`'s documented single-line limit holds: 0 matches), and the two-fragment prototype works: head and tail located by single-line searches, both redacted, placeholder drawn over the head, neighbours intact, the scanner's concatenated view clean | the wrapped-match step of D5's ladder is implementable with the library's own primitives; how often it is NEEDED is the G7 census, which runs with the reference documents (F4) |
| F17 | owner's deep run, 2026-08-22 | G4 on both reference documents shows NO regression. The 2-page PDF reaches exact per-category parity (7 categories, 33 values, every count identical). The 15-slide deck's extraction finds MORE: `person_names` 13 to 45, `identifier_names` 2 values found only by the library, `date` 1 to 1. The spacing repair changes no count on either document (33 before and after; 48 before and after) while D8's changed-line count is 12 and 24 | G4's reference half PASSES and F4's condition is met on this criterion. F11 is unchanged (the repair stays), with the refinement that the repair is not what carries the parity: it moves lines, not values |
| F18 | owner's deep run, 2026-08-22 | the gate test measured two things it did not intend. G4 compared the incumbent's REPAIRED pages against the library's RAW extraction, so it scored the absence of the repair rather than the library. And G7 drew its needles from the library's own extraction: a value that extraction split across a line break is not one value there, so it is never a needle, and the census reported zero wrapped occurrences BY CONSTRUCTION rather than by measurement | `pdffoss_gate_deep_test.go` repairs both sides before counting, and runs the census over BOTH needle sources, naming each. The pipeline-text source (the incumbent's already-reassembled text) stands in for the post-13c pipeline text until 13c's own line reassembly exists |
| F19 | owner's deep run, 2026-08-22 | G7 census with needles from the library's extraction: the 2-page PDF locates 33 of 33 literally (UNLOCATED 0), and the 15-slide deck locates 38 of 48 and leaves **10 UNLOCATED**. All 10 are `person_names`, all are present VERBATIM in the extraction yet unsearchable as one string, none is a ligature the fold rewrote, and none carries a non-ASCII character. Their longest searchable token run is 1 to 3 tokens out of 2 to 5, and the gap between their first two tokens, which share a baseline, measures 6.7, 6.7, 8.0, 8.2, 8.6, 42.9, 383.2, 384.8, 948.4 and 948.4 pt | UNLOCATED is NOT rare on a real deck (10 of 48, 21%), so §11's stated condition is NOT met and the gate REOPENS on this criterion. Two distinct causes, and 13c must answer both. `ExtractText`'s reading-order grouping concatenates fragments that merely share a baseline, so detection is offered strings that were never contiguous text (a 948 pt gap is opposite ends of a 960 pt landscape slide); and `SearchText` matches within ONE text-showing operation, so even a genuinely contiguous same-line value split across two draw operations is invisible to rungs 1 and 2 |
| F20 | owner's deep run, 2026-08-22 | the 2-page PDF's census over the PIPELINE's text leaves 1 UNLOCATED, a 2-token `person_names` value. It is absent VERBATIM from the library's raw extraction, carries no ligature the fold would have rewritten, and its two tokens do not share a baseline | a value the pipeline holds can be one that NO extraction contains verbatim, because the reflow and the spacing repair rewrite the text detection runs on. So 13c's ladder cannot locate by string equality with the pipeline's spelling alone, and the wrapped rung's geometry rule (tail above head, within 3 line heights) does not catch this occurrence |
| F22 | owner's deep run, 2026-08-22 | `main` does not build on WINDOWS from a clean clone. `go mod vendor` wrote `vendor/github.com/wailsapp/wails/v2/internal/webview2runtime/MicrosoftEdgeWebview2Setup.exe` and `.gitignore`'s `*.exe` rule stopped git tracking it, so the committed vendor tree is incomplete and `go build .` fails with `pattern MicrosoftEdgeWebview2Setup.exe: no matching files found`. Nothing catches it: the package is reached only through `internal/wv2installer`, which is Windows-only, so every Go job in `ci.yml` runs on `ubuntu-latest` and never builds it, and the one Windows job is tag-or-dispatch gated AND `continue-on-error`. `release.yml`'s `wails build -platform windows/amd64` would fail on the next tag. It is the ONLY file affected: the two other `.exe` files in the vendored modules' cached copies sit under `testdata/`, which `go mod vendor` never copies | F12's all-or-nothing vendoring has a second cost the batch did not see: the repository's own ignore rules can silently truncate the vendored tree, and the platform that catches it is the one CI does not run. The fix is one negation in `.gitignore` (`!vendor/**/*.exe`) plus committing the 1.8 MB file, and it is the OWNER's call because `*.exe` is ignored deliberately; the alternative is taking `vendor/` out of git, which contradicts D1's auditable-in-tree requirement. RESOLVED 2026-08-22: the owner chose the negation. `.gitignore` carries `!vendor/**/*.exe` after the `*.exe` rule (last matching pattern wins), the 1.8 MB file is committed, `.gitattributes` marks `*.exe binary` so `text=auto` cannot guess an executable into corruption, and `ci.yml` gained a `GOOS=windows go build` step on the existing Linux compile-rot job. That last part is the half that matters: the one-line ignore fix is cheap, and what let it ship was that no job on push compiles the Windows-only packages at all |
| F21 | owner's deep run, 2026-08-22 | G8 and G5 on the reference documents. Import: 8.6 ms incumbent against 5.8 ms library on the 2-page PDF (the library is FASTER), 3.1 ms against 23.2 ms on the 15-slide deck. No-edit `WriteTo`: 4.5 ms and 31.1 ms. Rasterised round trip: 0 differing pixels on every page rendered (2 and 3) | G8 and G5's reference halves PASS with room to spare: the largest export measured is 31 ms against a 30 s budget. The order's "within 3x the incumbent" import guidance is exceeded on the deck (7.5x) at 23 ms absolute, which is exactly why the encoded budget carries a 2 s floor beneath the ratio; no action |
| F23 | 13c, 2026-08-22 | the split threshold that separates the two measured gap populations is 3 word spaces (0.75 of the smaller adjacent font size): at every plausible size from 12 pt to 28 pt the five real word spacings (6.7 to 8.6 pt) stay under it and the five baseline-sharing gaps (42.9 to 948.4 pt) exceed it, with the nearest must-split gap still 5x the 28 pt threshold | `pdfSplitGapSpaces = 3.0` in `convert/pdflayout.go`, unit-pinned over all ten gaps at both sizes; the rule stays a multiple of the font size, never a point value, exactly as the order requires |
| F24 | 13c, 2026-08-22 | join-rule calibration on `framework_contract.pdf`: the contract's genuine wraps sit at 1.52 to 1.68 line heights (20.2 to 21.0 pt drops at a 13.3 pt line height), just OVER the order's "about 1.5", so the gate is 1.6 line heights with a 1 em right-edge slack. Detection counts, incumbent / after 1a / after 1a+1b: `framework_contract.pdf` entity_names 2/1/2 (1b recovers the wrapped two-token organisation 1a alone loses), person_names 10/10/9 (two heading half-artifacts merge into one artifact when the title block joins: one row fewer to review, no real value lost), product_names 1/0/0 (see F25); `nstar_contoso_flyer.pdf` identical in every category (9 categories, 36 values); `pdf_gate_fragments.pdf` person_names 0/1/1 and entity_names 0/0/1 (the split and the join each recover a value the incumbent never saw) | 1b SHIPS beside 1a: its one cost on the fixtures is the merge of two review-list artifacts into one, and its gain is the wrapped organisation the order names. The gate constants live beside the rules with the calibration reasoning |
| F25 | 13c, 2026-08-22 | `framework_contract.pdf` product_names 1 to 0 under 1a: the incumbent read the page header's three separate cells (a reference code, a spacer, the document-type label) as ONE line, and the heuristic proposed the label as a product name from that manufactured context. The split keeps the three cells apart; the label survives verbatim on its own line, and the heuristic's heading gate then declines it | recorded as the measured cost of 1a on the fixtures: the dropped suggestion is document furniture (the file's own type label), not a client identifier, and the string itself stays in the working text where a manual declaration can still reach it. G4's per-category letter is traded against F19's manufactured-value class, which is the whole point of the rule |
| F26 | 13c, 2026-08-22 | both body gestures repaint more than the matched rectangle: `ReplaceText` re-emits its fragment and `ApplyRedactions` repositions the line's surviving glyphs through kerning gaps, so the rest of the REPLACED LINE drifts sub-pixel (measured 380 and 53 differing pixels on the two gate-fixture pages; every drift inside the line, none outside it) | acceptance criterion 5's "pixels outside replaced regions identical" is asserted per LINE: the replaced line is the replaced region, and every other line and the rest of the page must render pixel-identical, which the integration suite pins |
| F27 | 13c, 2026-08-22 | `Document.Info()` at v0.7.0 returns Info-dictionary strings as the file's RAW bytes: a UTF-16BE value (the encoding fpdf and most writers use) comes back as byte salad with its BOM intact, so the review showed garbage and the scrub could not match a name stored two bytes per character | every Info read decodes through the leak scanner's own UTF-16BE decoder (`decodePDFInfoText`), so the review, the scrub and the scanner agree on what the text is |
| F28 | 13c, 2026-08-22 | Q1's table says page thumbnails (`/Thumb`) are dropped from the produced copy, but the library at v0.7.0 exposes no API over page thumbnail entries, and a thumbnail is not TEXT the span machinery can scrub: it is a rendered PICTURE of the un-anonymised page | the drop moves to 13d, whose subject is the PDF's pictures: D4 is amended (thumbnails are 13d's), 13d's scope gains the decision (drop `/Thumb` through whatever picture plumbing 13d builds, or record the library gap and its mitigation). Until then the leak scan reports image streams it cannot decode rather than passing them silently, and none of the committed fixtures a produced file is built from carries a thumbnail forward |
| F29 | 13c, 2026-08-22 | the IMAGE tab's PDF sentence ("every image in a PDF is already removed") became FALSE the moment the export stopped regenerating: an in-place copy carries the original's pictures through unchanged, and a reassurance that says otherwise is a leak wearing a sentence | the copy behind `pdf_images_removed` now states the pass-through and names the .md export as the way out for a picture that must not leave; the IDENTIFIER keeps its name until 13d retires it with its `vocabulary_guard_test.go` entry (D10), because a label is a display string and an identifier is a contract |
| F30 | owner's deep run, 2026-08-22 | the reference-document G4 comparison FAILS after 13c: on the 2-page PDF the production extraction finds 20 `person_names` where the retired baseline found 21 (totals 32 against 33), while on the 15-slide deck it finds 30 against 14 (`person_names` 27 against 13, plus 2 `identifier_names` the baseline never saw). G7 is UNLOCATED 0 on BOTH documents, which is acceptance criterion 3, and 13 of the deck's 36 occurrences are located by the fragment walk (10) or the wrapped rung (3), so those 13 would have refused the export before 13c | the single missing person is the split rule refusing a run of fragments that merely share a baseline: the baseline glued them, so the value it counted was never contiguous text on the page and counting it was the defect. The gate was accepted on that reading, and the consequence for the test is structural rather than cosmetic: with the baseline library removed there is no comparison left to make, so G4 becomes RECORDED FLOORS per category (`exportfmt.referenceFloors`) with a tolerance of one value (`referenceFloorTolerance`). One value is threshold noise from the split and join geometry; two is a class collapsing, which is what the criterion is about. G8's import check loses its denominator with the baseline and becomes an absolute 10s budget, generous on purpose: a tight budget on a machine-dependent number fails for reasons nobody can act on |
| F31 | 13c follow-up, 2026-08-22 | the ladder census and the per-category floors ran ONLY in the deep tier, behind two environment variables and two files that exist on one machine. An occurrence the ladder cannot locate REFUSES a user's export, and nothing on push checked that there were none. Moved to shared untagged helpers (`exportfmt/pdfcensus_test.go`) and gated on the INTEGRATION tier over the committed fixtures: 13 occurrences on `framework_contract.pdf` (literal 8, tolerant 1, wrapped 4) and 50 on `nstar_contoso_flyer.pdf` (literal 46, tolerant 4), UNLOCATED 0 on both, plus 13 category floors | the deep tier now adds SCALE rather than being the only enforcement. Two details are load-bearing: a census that examined ZERO occurrences FAILS, because a document detection finds nothing in proves nothing about the ladder (the new PowerPoint fixture tripped exactly this), and the whole rung distribution is logged on a pass, because occurrences migrating from the literal rung to the fragment walk is a statement about extraction worth seeing before it becomes a refusal |
| F32 | 13c follow-up, 2026-08-22 | `working_deck.pdf`, added as the PowerPoint print-to-PDF census fixture, extracts 7 pages and 10,363 characters of which **100 per cent are unmappable**. Its 9 fonts are all `/Type0` with `/Identity-H` encoding and embedded `/FontFile2` subsets; producer is `Microsoft: Print To PDF`. NO other committed PDF fixture uses `/Type0` at all, so no test could have caught this. Measured unmappable share across the readable fixtures: 0.0 to 0.2 per cent | this is a SILENT failure with a data-protection consequence, not a rough edge: the file has a text layer so the scanned refusal does not fire, it opens cleanly so the damaged refusal does not fire, detection then truthfully reports finding nothing, and a user is entitled to read that as nothing to anonymise in a deck full of names. Answered with a THIRD refusal (`convert.ErrUnmappablePDF`) gated on a share of unmappable characters (`maxUnmappableShare`, 0.3, sitting in the empty gap between the two measured populations), naming re-export from the authoring application as the way out. The underlying Identity-H decoding gap is NOT fixed and is the owner's next decision: it is a large class of real-world files on this market's laptops |
| F33 | 13c follow-up, 2026-08-22 | `pythonExe` in `scripts/to_sarif_integration_test.go` picked the first name `exec.LookPath` resolved. On the owner's laptop `python3` resolves ONLY to the Windows Store App Execution Alias, which resolves, refuses to run and shadows the real Python 3.12.10 earlier on the same PATH, so 13 tests failed with exit 9009 on a machine that has Python. The render harness's browser search had the same shape | a capability check must PROVE its dependency by running it, never infer it from a name resolving. Both now run the candidate and require an answer, and pass over one that cannot give it, naming what was tried. Recorded in docs/TESTING.md as a rule, because the shape recurs wherever a test shells out |
| F34 | 13c follow-up, 2026-08-22 | F32's root cause is a BUG IN THE VENDORED LIBRARY, not in the file and not in our code. `working_deck.pdf`'s ToUnicode CMaps are valid `Adobe-Identity-UCS` programs (68 `beginbfchar` entries, `<0024>` to `<0041>`, uncompressed), but each is written as ONE LINE: 1278 bytes, one newline, at the end. Upstream v0.7.0's `parseCMap` splits on lines and arms a section only on a line ENDING with `beginbfchar`, disarming only on a line that IS exactly `endbfchar`, so it never arms and returns an empty map. Both `ExtractText` and `ExtractTextWithLayout` fail identically, so no call site of ours is implicated; v0.7.0 is the newest release and nothing upstream mentions it. Proven by running upstream's own logic and a token-oriented version against the real stream: 0 mappings against 68, with `0x0024` to `'A'` | a CMap is a PostScript-style token program in which a newline is ordinary whitespace, so line-oriented reading is wrong by construction rather than incomplete. FIXED IN TREE by patching `vendor/.../cmap.go` to scan tokens, which also stops truncating multi-pair lines and dropping bfranges whose operands straddle a newline. That patch is the ONE non-upstream file in `vendor/`, a deliberate exception to the vendor-purity rule: the alternative considered and rejected was rewriting the PDF's own bytes before `OpenStream` to reformat its CMaps, which is roughly four times the code, needs `/Length` bookkeeping, and puts byte surgery in the leak-critical import path to work around a parser. It is pinned by `TestOneLineToUnicodeCMapIsDecoded`, so a `go mod vendor` that drops it fails the build with a message naming the cause, and it is removed when upstream ships the fix. The deck now extracts 11,275 characters at 0.0000 unmappable and joins the integration census (8 occurrences, UNLOCATED 0). `ErrUnmappablePDF` STAYS, with a generated `pdf_no_tounicode.pdf` behind it: a file with no CMap at all is still unreadable, and that refusal must not depend on a library bug staying unfixed |

---

## 8. Reconciliation (the standing obligation)

Every batch order of this family ends with a reconciliation step, worded as an
obligation: before the batch is reported finished, update the batch status
table (§6), append every finding to the findings log (§7), and **revise the
next batch's order in place** where a finding invalidates it. If a finding
invalidates a decision in THIS plan, the decision is amended here, with the
reason, rather than contradicted quietly in a later document.

---

## 9. Open questions for the owner — ANSWERED 2026-08-21

Each was in the decisions table with `owner` in the Source column. The owner
answered all five on 2026-08-21; the answers are recorded here verbatim in
substance and folded into the decisions they touch (D4, D6, D11, D12, D15).

| # | Question | Recommendation | Owner's answer (2026-08-21) |
|---|---|---|---|
| OQ1 | Is a pre-1.0 dependency with 3 stars acceptable for reading client documents at all, given the mitigations (vendored and pinned source, the Q7 boundary guard, bytes-only entry points, the whole-file leak check, the 13b gate)? | Yes, behind the gate: the alternative is no in-place PDF at all, and the mitigations make the risk inspectable. But this is the owner's risk to accept, before 13b runs. | **Risk accepted.** 13b may start. |
| OQ2 | Is a substituted metric-compatible font (Arimo for Helvetica/Arial, Tinos for Times) an acceptable price for layout fidelity on replaced text? | Yes; it is the only pure-Go answer, and it touches only the replaced words. 13b produces rasterised before/after images from the fixtures so the owner judges with their eyes, not a description. | **Accepted.** 13b still attaches the rasterised before/after images to the GO/NO-GO note, so the acceptance is of something seen. |
| OQ3 | Does PDF lose its EXPERIMENTAL label after 13d? | Keep it through 13c and 13d; revisit after real-document use. The label is cheap and honest while the extractor and injector are new. | **The label stays.** The owner additionally created a tag and a release of the current application as the rollback point, and set a gate this plan did not have: **the old PDF path is decommissioned only after the owner has confirmed to the implementing session that the tests are successful** (folded into D12 and the 13c scope). |
| OQ4 | May the produced PDF drop embedded file attachments and JavaScript actions outright (reported, never silent)? | Yes: an attachment is an inner document the pipeline never read, and carrying it through an "anonymised" file is a leak wearing a paperclip. | **Confirmed:** both are dropped from the anonymised file, reported, never silent. |
| OQ5 | When an occurrence cannot be located and the export refuses (D6), should the old regenerated-layout export remain available as an explicitly-labelled fallback? Keeping it keeps `go-pdf/fpdf`, making the arithmetic +1/−1 instead of +1/−2. | Remove it: the refusal names the `.md` export as the way out, and a second PDF writer kept for a rare failure path is unreviewed code in the leak-critical path. | **Confirmed:** the regenerated export goes, `go-pdf/fpdf` leaves, and the arithmetic is +1 and −2 (subject to the OQ3 decommissioning gate). |

---

## 10. Acceptance criteria for the whole change

1. Both suites green after every batch, and the rendering harness green where
   a batch touches the UI.
2. The 13b GO/NO-GO note exists in this document with every Q10 measurement
   recorded as counts; no confidential string from the reference documents
   appears anywhere in the repository.
3. A PDF imports through the pinned, vendored library; detection over its
   extraction finds at least what it finds today (G4's counts hold).
4. The PDF export is in-place replacement: the produced file is the original's
   structure, the whole-file leak check blocks every export it fails, and the
   ladder's rung counts are reported to the user.
5. The IMAGE tab reviews a PDF's pictures with one decision per asset, the
   treatments run through `imaging.Treat`, and a treated asset's original
   bytes are absent from the produced file.
6. `go.mod` gained exactly one dependency and lost two (`ledongthuc/pdf` and
   `fpdf`, removed only after the owner's tests-successful confirmation, per
   D12); `vendor/` holds the module's exact pinned source; `CLAUDE.md` §7
   carries the module row, the fonts row and the go-cpp rejection row.
7. `pdf_boundary_test.go` guards the copilot symbols, the vendored network
   inventory and the engine's bytes-only entry points, and
   `TestAnonymiseNeverCallsOllama` is untouched and green.
8. `SessionVersion` is 14, the reason is beside the constant, a v13 file is
   refused with a message naming both versions, and no migration code exists.
9. The scanned-PDF refusal message is byte-identical to today's.
10. Every authoritative document names the new behaviour: `CLAUDE.md` (§4 PDF
    fallback note, §5 PDF rules and the image format table, §7 pins),
    `backend/CLAUDE.md`, `frontend/CLAUDE.md`, `frontend/BRIDGE.md`,
    `README.md`, `frontend/docs/index.html`.

---

## 11. The 13b GO/NO-GO note — 2026-08-21

**Verdict: CONDITIONAL GO.** Every criterion measurable without the owner's
confidential reference documents PASSES, including all four hard requirements
(G1, G2, G3, G9). The reference-document halves (G4's real-document counts,
G7's ladder census, G8's wall clocks, G5 on real files) could not run in the
implementing session, because that environment does not hold the documents
(F4); they are packaged as the env-gated deep tests and the owner's
`task test:deep` run is 13c's entry gate. The condition is a measurement to
take, not a doubt to argue: if that run shows a G4 regression or a G7 census
where UNLOCATED is not rare, the gate reopens and this note is amended.

| # | Criterion | Measured | Result |
|---|---|---|---|
| G1 | `WriteTo` is a full rewrite | single body (1 `%%EOF`), no `/Prev` chain; sentinel absent after `ReplaceText` + the F5 save discipline (`RemoveUnusedObjects()` first). A NAKED `WriteTo` retains the orphaned pre-edit stream (1 to 3 objects per edited fixture), so the discipline is load-bearing and both halves are pinned by test | **PASS**, with the discipline recorded in `CLAUDE.md` §7 |
| G2 | the whole-file leak scanner exists and finds every planted sentinel | found in all 5 surfaces of `pdf_gate_surfaces.pdf` (content stream, Info, XMP, annotation, outline), in a hand-appended incremental body (named as such), and in 7 string encodings (escaped literal, octal, hex, UTF-16BE, split Tj segments, flate-compressed content, line continuation); 0 findings for an absent needle; DCTDecode streams reported unscannable by name | **PASS** |
| G3 | the boundary guard exists, red-green demonstrated, inventory committed | red on a scratch `NewOpenAIClient` reference (failure named file, line, symbol and reason), green after removal; committed network inventory = [`pkcs7_timestamp.go`]; the `ai` copilot package is asserted ABSENT from `vendor/`; the engine path ban forbids `Open`/`OpenWithPassword`/`.Save(` under `backend/` and pins the one import alias | **PASS** |
| G4 | extraction parity | fixture half: 9 of 9 planted names (English, French, an email, a 9 pt value, both wrapped-value halves) present in BOTH extractors; page shape 3 = 3 | **PASS on fixtures; reference half pending the owner's deep run (F4)** |
| G5 | round-trip fidelity | page count, per-page text and image inventory preserved on all three fixtures; rasterised difference **0 pixels on all 6 pages** | **PASS on fixtures; real files pending (F4)** |
| G6 | `ReplaceText` behaviour | original absent (scanner), placeholder extractable; redrawn at the SAME size, growing rightward 59.0 pt to 123.5 pt, no shrink; **0 pixels changed outside the replaced region**. Consequence F10: rung 1 needs a fits-check against same-line neighbours | **PASS**, with F10's condition on rung 1 |
| G7 | ladder coverage | prototyped on fixtures: single-line limit confirmed (wrapped value not found whole), two-fragment wrapped redaction works with neighbours intact (F15); rung 2 (redact + overlay placeholder) proven with the committed before/after artefacts | **prototype PASS; census pending the owner's deep run (F4)** |
| G8 | wall clock | budgets encoded in the deep test from the order's recommendation: import within 3x the incumbent, export within 30 s per document, incumbent baseline measured in the same run | **pending the owner's deep run (F4)** |
| G9 | failure behaviour | no panic at any truncation depth (salvage-opens 3/2/1/0 of 3 pages, never invents content); non-PDF errors actionably; encrypted file distinguishable (`PDF is encrypted; use OpenWithPassword`) and opens with the password; `scanned.pdf` extracts as textless through the library, so `ErrScannedPDF` fires byte-identically | **PASS**, with F8's two 13c consequences |

**The rasterised evidence (OQ2):** `backend/testdata/golden/pdf_gate_redact_before.png`
and `pdf_gate_redact_after.png`, regenerated by the rung-2 gate test: the
original name replaced by a black redaction box carrying `[PERSON_3]` in
white, neighbours pixel-identical.

**Consequences folded into the batch scopes (§5):** the save discipline and
its pin (F5), rung 2's Add-plus-OverlayText gesture (F6, F7), rung 1's
fits-check (F10), `RepairPDFText` stays (F11, resolving D8), the damaged-vs-
scanned distinction (F8), inline images listed and needing a 13d decision
(F9), and D9's content-hash identity confirmed by per-placement evidence (F9).

**What 13b did NOT change:** no production behaviour. The module is imported
by test files and `exportfmt/pdfscan.go` only, `pdfscan.go` has no production
caller yet, and the boundary guard holds the copilots, the timestamp lever and
the file-path APIs out of everything. A PDF imports, anonymises and exports
today exactly as before this batch.

It did, however, change what a clean clone can BUILD: vendoring pulled in a
file the repository's own `.gitignore` refuses to track, and the resulting
Windows build failure is invisible to every CI job that runs on push (F22).

### The reference-document halves — 2026-08-22

The owner ran the deep tier on their own machine, over the 2-page PDF and the
15-slide deck exported to PDF. The documents are confidential and stayed on
that machine; everything below is a count, a duration or a distance.

Running the test as packaged also showed it was measuring two things it did
not intend, so it was corrected first and both runs are reported (F18): G4 now
applies the spacing repair to BOTH extractions, and G7's census runs over two
needle sources because the source decides the answer.

| # | Criterion | Measured on the reference documents | Result |
|---|---|---|---|
| G4 | extraction parity | 2-page PDF: exact per-category parity, 7 categories and 33 values, every count identical. 15-slide deck: strictly MORE, `person_names` 13 to 45, `identifier_names` 2 library-only, `date` 1 to 1. The repair changes no count on either (F17) | **PASS** |
| G5 | round-trip fidelity | 0 differing pixels on every page rendered, 2 of the PDF and 3 of the deck (F21) | **PASS** |
| G7 | ladder coverage | 2-page PDF: 33 of 33 literal, UNLOCATED 0. 15-slide deck: 38 literal, **10 UNLOCATED of 48**, all `person_names`, all present verbatim in the extraction and unsearchable as one string (F19). Over the PIPELINE's text the PDF leaves 1 UNLOCATED, a value no extraction holds verbatim (F20) | **FAIL against the stated condition: UNLOCATED is not rare** |
| G8 | wall clock | import 8.6 ms against 5.8 ms (the library is faster) and 3.1 ms against 23.2 ms; no-edit `WriteTo` 4.5 ms and 31.1 ms against a 30 s budget (F21) | **PASS** |

**The condition is half met, and the note is amended as §11 said it would be.**
G4, G5 and G8 pass on the real documents, so the three questions that could
have rejected the library outright are answered in its favour: it reads a real
deck better than the incumbent does, it rewrites a real file without moving a
pixel, and it costs milliseconds.

G7 does not pass, and the gate REOPENS on that criterion alone. It is not a
verdict on the library: the 10 unlocated occurrences are a consequence of how
extraction and search each treat a text FRAGMENT, and both halves are 13c's to
answer (F19). Until they are answered, D6's refusal would fire on about a fifth
of a real deck's values, and it would fire for strings that were never in the
document, which is the worst of both outcomes: an export refused for a leak
that does not exist. **13c cannot be written against the ladder as scoped**;
the scope revision below is the entry condition now.

### The re-run G7 census — 13c, 2026-08-22

13c's answer to both halves is in: extraction splits a line where two
fragments merely share a baseline (so a manufactured string is never offered
to detection, F19's first cause), and the ladder gained the fragment-walk rung
(so a value split across draw operations, or spelt differently from any
extraction, is located by walking the line model, F19's second cause and
F20's). The census itself is now an ASSERTION rather than a log line:
`pdffoss_gate_deep_test.go` runs detection's needles page by page through the
PRODUCTION ladder over the PRODUCTION pipeline text and FAILS unless UNLOCATED
is 0 on each reference document, naming the findings log as where any survivor
must be explained.

The implementing environment does not hold the reference documents (F4), so
the numbers themselves are the owner's to take: run `task test:deep` with
`DOC_ANONYMISER_REFERENCE_PDF` and `DOC_ANONYMISER_REFERENCE_DECK` set. On the
committed fixtures the same machinery measures: the F19-geometry fixture
(`pdf_gate_fragments.pdf`, same-baseline gaps 6.7 to 948.4 pt, values whose
longest searchable run is 1 to 3 of 2 to 5 tokens) exports with its two
split-across-operations names located by the fragment rung and its wrapped
organisation by the wrapped rung, UNLOCATED 0; and a value the old extraction
would have manufactured neither reaches detection nor causes a refusal. This
note is amended with the owner's counts when that run lands.
