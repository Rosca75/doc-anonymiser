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
| D4 | Surfaces the text pass cannot reach are anonymised through the existing metadata review (Info, XMP) or scrubbed as text at export (annotations, form values, outlines), and the rest are **dropped from the produced copy** (page thumbnails, embedded attachments, JavaScript actions), each drop reported | Q1, Q3 | mine; the attachment/JS drop is flagged to the owner as OQ4 |
| D5 | Binding is **string-driven**, registry original to placeholder, with a fixed fallback ladder: `ReplaceText`, else redact-and-redraw (`NewRedactAnnotation` + `ApplyRedactions` + `Page.AddText` fitted to the original rectangle down to a floor), else a solid redaction box with no caption; every rung is counted and reported | Q2 | mine |
| D6 | An occurrence that cannot be **located** at all blocks the export with an actionable refusal naming the placeholder and the page; a half-anonymised PDF that looks finished is worse than a refusal. Whether a regenerated-layout fallback stays available behind that refusal is the owner's call (OQ5), and it decides whether `fpdf` actually leaves | Q2 | owner (OQ5); refusal itself: mine |
| D7 | Extraction stays **page-shaped** (`Document.Pages`, so `PageCount`, `engine.ScanChunks` and `pagescope.go` are untouched); the working markdown remains the body text in reading order; annotations, form field values and outline titles are NOT added to the markdown and are scrubbed at export through the same span machinery, following the existing docx header/footer precedent, with the extra hits reported as the docx `document_extras` warning is | Q3 | mine |
| D8 | The spacing-repair heuristic's fate is a 13b measurement: if the library's layout-aware extraction no longer produces the kerning defect, the repair retires for PDF in 13c; if it does, the repair keeps running over the new extractor's output | Q3 | 13b |
| D9 | A PDF image asset's ID is a **content hash** (`pdf:sha256:<16 hex>`); an occurrence is `Part: "page/<n>"` plus `Ordinal` among that page's image placements. The inventory lists raster image XObjects; vector drawings are content, not assets, and are never offered a control that cannot anonymise them | Q4 | mine |
| D10 | Treatments reuse `imaging.Treat` unchanged; the treated PNG/JPEG bytes go back through `ImageInfo.ReplaceFromStream`. The PDF format table row changes to full image review, `ReasonPDFImagesRemoved` and `copy.js` `pdf_images_removed` retire (with a `vocabulary_guard_test.go` entry), and `image_parity_test.go` moves with them, all in 13d | Q4 | mine |
| D11 | The copy promises what is measured, never "almost identical": replaced words are redrawn in a substitute font and may shrink to fit. PDF **keeps its EXPERIMENTAL label** through 13c and 13d; dropping it is the owner's call afterwards (OQ3) | Q5 | mine + owner |
| D12 | Dependency arithmetic is **+1 and −2**, staged: `ledongthuc/pdf` and `go-pdf/fpdf` leave at the END of 13c, after its acceptance criteria pass and never before; the module is vendored so its exact source is auditable in-tree; a version bump is never automatic and re-runs the 13b gate checks | Q6 | mine |
| D13 | The local-only guarantee gets a **boundary guard test** in the idiom of `vocabulary_guard_test.go`: a forbidden-symbol scan over `backend/`, `frontend/` and `scripts/`, plus a committed inventory of the vendored files that import `net/http`, held unchanged so a version bump cannot widen the network surface unnoticed. Built in 13b as a first-class step, before any measurement that exercises the library | Q7 | mine |
| D14 | Scanned PDFs: the refusal and its exact message stay word for word. The library's OCR runs through the copilot endpoint, which D13 forbids, and returns no coordinates, which in-place replacement needs, so OCR has not arrived and the plan says so once | Q8 | mine |
| D15 | The **adoption gate** (Q10) runs in 13b before any production wiring: measurable GO/NO-GO criteria, counts only, on committed synthetic fixtures plus the owner's two confidential reference documents. NO-GO leaves today's pipeline untouched and records the measurement here as a rejected option, pdfcpu-style | Q10 | 13b; acceptability of a pre-1.0 dependency at all is the owner's (OQ1) |
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
`.md`, or per OQ5 the regenerated layout). It cannot be a warning: the Q1
whole-file check would fail on the surviving original anyway, and a
half-anonymised PDF that looks finished is worse than a refusal. Whether the
old regenerated export remains available as the explicitly-labelled fallback
behind that refusal is the owner's question OQ5, because keeping it keeps
`fpdf` and changes the Q6 arithmetic.

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
  regeneration, **unless** the owner keeps regeneration as the explicit
  fallback behind D6's refusal (OQ5), in which case `fpdf` stays and the
  arithmetic is +1 and −1. The recommendation is to remove it: the refusal
  names the `.md` export as the way out, and a second PDF writer kept for a
  rare failure path is a maintenance cost with no reviewer.
- Removal happens at the **end of 13c**, after its acceptance criteria pass,
  never in 13b: the replacement is proven before the incumbent leaves.
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

**Gate before it runs:** 13b's GO, plus owner answers to OQ2, OQ4 and OQ5.
**Acceptance (to be sharpened by 13b's findings):** both suites green; the
framework-agreement suite untouched and green; the exported PDF of every
fixture passes the whole-file check; ladder rung counts reported in the export
review panel and the report; `go.mod` no longer names `ledongthuc/pdf` (and
`fpdf`, per OQ5); a grep for the removed imports returns nothing outside
`docs/`; `SessionVersion` still 13 and every existing session file still
loads; scanned-PDF refusal byte-identical.

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
| 13 (this plan) | done | 2026-08-21 planning session | plan and 13b order written; owner questions OQ1 to OQ5 open |
| 13b | planned | | |
| 13c | scoped, not written | | order to be written after 13b's GO |
| 13d | scoped, not written | | order to be written after 13b's GO, revised after 13c |

---

## 7. Findings log

Append-only. What the implementation discovered that the plan did not know,
with the decision each finding forced. Measurements land here, as counts.

| # | Found in | Finding | Decision forced |
|---|---|---|---|
| F1 | planning, 2026-08-21 | `frontend/BRIDGE.md`'s session-file paragraph still says "schema version 9" while `SessionVersion` is 13 (`backend/engine/session.go`, `CLAUDE.md` §5) | the correction rides with 13d's `BRIDGE.md` edits, which touch that section anyway for the version bump; no separate batch |
| F2 | planning, 2026-08-21 | `WriteTo` full-rewrite behaviour and the copilots' exact configuration symbols could not be verified from documentation | both became 13b measurements: G1 proves the save with bytes, and the Q7 symbol table is generated from the vendored source rather than from the README |

---

## 8. Reconciliation (the standing obligation)

Every batch order of this family ends with a reconciliation step, worded as an
obligation: before the batch is reported finished, update the batch status
table (§6), append every finding to the findings log (§7), and **revise the
next batch's order in place** where a finding invalidates it. If a finding
invalidates a decision in THIS plan, the decision is amended here, with the
reason, rather than contradicted quietly in a later document.

---

## 9. Open questions for the owner

Each is in the decisions table with `owner` in the Source column; none may be
answered by an implementing session on its own.

| # | Question | Recommendation |
|---|---|---|
| OQ1 | Is a pre-1.0 dependency with 3 stars acceptable for reading client documents at all, given the mitigations (vendored and pinned source, the Q7 boundary guard, bytes-only entry points, the whole-file leak check, the 13b gate)? | Yes, behind the gate: the alternative is no in-place PDF at all, and the mitigations make the risk inspectable. But this is the owner's risk to accept, before 13b runs. |
| OQ2 | Is a substituted metric-compatible font (Arimo for Helvetica/Arial, Tinos for Times) an acceptable price for layout fidelity on replaced text? | Yes; it is the only pure-Go answer, and it touches only the replaced words. 13b produces rasterised before/after images from the fixtures so the owner judges with their eyes, not a description. |
| OQ3 | Does PDF lose its EXPERIMENTAL label after 13d? | Keep it through 13c and 13d; revisit after real-document use. The label is cheap and honest while the extractor and injector are new. |
| OQ4 | May the produced PDF drop embedded file attachments and JavaScript actions outright (reported, never silent)? | Yes: an attachment is an inner document the pipeline never read, and carrying it through an "anonymised" file is a leak wearing a paperclip. |
| OQ5 | When an occurrence cannot be located and the export refuses (D6), should the old regenerated-layout export remain available as an explicitly-labelled fallback? Keeping it keeps `go-pdf/fpdf`, making the arithmetic +1/−1 instead of +1/−2. | Remove it: the refusal names the `.md` export as the way out, and a second PDF writer kept for a rare failure path is unreviewed code in the leak-critical path. |

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
6. `go.mod` gained exactly one dependency and lost two (or one, per OQ5);
   `vendor/` holds the module's exact pinned source; `CLAUDE.md` §7 carries
   the module row, the fonts row and the go-cpp rejection row.
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
