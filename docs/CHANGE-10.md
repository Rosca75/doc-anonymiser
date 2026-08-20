# CHANGE-10 — Image anonymisation: the second half of the Anonymise step

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0, pure Go + Wails v2, no CGo, no npm). It holds **four
self-contained implementation batches (B1 to B4)**, each sized for ONE Opus 5
session, followed by the **decisions taken**, a **conflict analysis**, the
**recommended execution sequence** and the **acceptance criteria**.

The feature: the Anonymise step (step 3) gains a second half. Today that step is
about text only. After this order it carries two tabs, **TEXT** (the Compare
card exactly as it is today) and **IMAGE** (every picture found in the selected
document, with a decision per picture: keep it, or anonymise it by replacing it
with a box carrying the user's own text, by blurring it, or by removing it
entirely).

The order also closes a **leak that exists today** and that nothing in the
repository currently says out loud. `exportfmt.rewriteZip` copies every archive
entry it has no rewriter for BIT-FOR-BIT (`backend/engine/exportfmt/exportfmt.go`),
and no rewriter is registered for `word/media/*` or `ppt/media/*`. So the
"anonymised" .docx or .pptx a user exports today still contains **every original
image**: the client logo, the screenshot of the client's own system, the photo of
the team. The text is anonymised and the pictures are not. That is the real
reason this feature is not cosmetic.

Ground rules for this change order (unchanged from `CLAUDE.md`):

- `CLAUDE.md` and the two subtree charters remain authoritative. Nothing here
  weakens the local-only guarantee, the one-file Ollama boundary, the zero-CGo
  rule, or "originals are immutable": every treatment described below reads
  `Document.Raw` (the bytes captured once at import) and produces NEW bytes that
  `app.go` writes through the usual save dialog.
- **No new Go module.** The owner decided this explicitly (decision 2). Every
  image operation in this order is `image`, `image/png`, `image/jpeg`,
  `encoding/xml`, `archive/zip` and arithmetic. The one non-stdlib thing added is
  a **vendored data table** (an 8x8 bitmap font), which is an asset like the
  Material Symbols SVGs already are, not a module.
- **A change is not finished until its tests move with it** (`CLAUDE.md` §6, and
  `docs/TESTING.md` owns the tiers, the category prefixes and the scoping
  procedure). Each batch names the tests to add and update. Both suites gate:
  `go test ./...` and `node --test "frontend/**/*.test.js"`.
- User-visible copy never contains em dashes (`copy_guard_test.go`,
  `frontend/copy.test.js`). This order adds a lot of copy on both sides of the
  bridge, so that guard will be exercised. The prose of THIS document is not
  copy; the strings it quotes are.
- The parity guards are load-bearing. This order adds one
  (`image_parity_test.go`) and touches `icon_parity_test.go` and
  `copy_guard_test.go`.
- Comments explain intent in the present tense. Do not write "used to be", "B2
  changed this" or "CHANGE-10 added this" into the code.
- **This order changes authoritative rules in `CLAUDE.md` itself**: §5 gains the
  image rules and the per-format applicability matrix, §5's session paragraph
  gains the new `SessionVersion`, §7 gains the vendored-font asset row, and
  `backend/CLAUDE.md` and `frontend/CLAUDE.md` gain their subtree detail. Those
  edits are part of the batches and are not optional: leaving them stale would
  make the charter describe a contract the code no longer honours.

---

## 0. Cold-start context for the implementing session

Read this section and YOUR batch. You do not need to read the other batches,
except the "what B<n> hands you" paragraph at the top of yours.

### Where the work stands

| Fact | Value |
|---|---|
| Repository | `Rosca75/doc-anonymiser`, module path `doc-anonymiser` |
| Branch to develop and push on | a fresh branch off `main`, e.g. `claude/change-10-b1-<suffix>`, one per batch |
| Suites | `go test ./...` and `node --test "frontend/**/*.test.js"`, both must be green |
| Integration tier | `go test -tags=integration ./...` |
| Deep tier | `go test -tags=deep ./...` (includes the CDP render harness) |
| Task runner | `task test`, `task test:integration`, `task test:all`, `task audit` (go-task, no make) |
| Scoping rule | `docs/TESTING.md` §"Scoping tests for a change". Do not run the whole suite per edit. |

### The deviation rule (this is why the batches are in one file)

The batches are ordered and each one builds on the last. If your batch **has to
deviate** from what is written here (a contract turns out to be wrong, a name
collides, an approach does not survive contact with a real file), then before
you finish:

1. Edit the LATER batch sections of this document so they describe what you
   actually left behind, not what was planned.
2. Say so in your commit message and in the pull request body, in one sentence
   per deviation.

A later session reads this file as its brief. A stale brief costs it a whole
session.

### What the repository does with images TODAY (established by reading it)

| Fact | Where |
|---|---|
| The docx converter DROPS images from the working text and writes `*[image omitted]*`, counting them into an import warning | `backend/engine/convert/docx.go:394` |
| The pptx converter ignores pictures entirely (it walks `a:t`, `a:tbl` and nothing else), so a deck's images are invisible to the app end to end | `backend/engine/convert/pptx.go walkShapes` |
| The same-format export copies every entry it has no rewriter for bit-for-bit, and `word/media/*` / `ppt/media/*` have none | `backend/engine/exportfmt/exportfmt.go rewriteZip` |
| So the exported .docx / .pptx today ships every original image untouched | the two rows above, together |
| The PDF export does NOT rewrite the original file: it REGENERATES a new PDF from the anonymised text with `go-pdf/fpdf` | `backend/engine/exportfmt/pdf.go ExportPDF` |
| Therefore every image in a source PDF is ALREADY absent from the exported PDF | same |
| The xlsx export rewrites cells through excelize and passes media through | `backend/engine/exportfmt/xlsx.go` |
| Word's cached page breaks are already parsed (`w:lastRenderedPageBreak`, hard `w:br` page breaks) and drive `Document.Pages` | `backend/engine/convert/docx.go:384-393` |
| Binary fixtures are GENERATED from code with the stdlib, then committed | `backend/engine/convert/fixtures_test.go` |
| There is a reusable tab bar | `frontend/ui.js tabbar()`, used by `views/identifyworkspace.js` |
| There is an in-app confirm and a notice strip; native dialogs are banned under `frontend/` | `frontend/modal.js`, `frontend/toast.js` |

### The applicability matrix (fixed, do not widen it)

| Format | Image review | Why |
|---|---|---|
| `.pptx` | **full** | pictures are `p:pic` elements in `ppt/slides/slideN.xml`, layouts and masters, with their bytes in `ppt/media/*` |
| `.docx` | **full** | pictures are `w:drawing` (or legacy `w:pict`) in `word/document.xml`, headers and footers, with their bytes in `word/media/*` |
| `.pdf` | **not offered**, one explanatory line | the export regenerates the file from text, so every image is already gone. See Appendix C for the full investigation |
| `.xlsx` | **not offered** | the owner's decision: a spreadsheet's pictures are not worth the complexity |
| `.csv` `.txt` `.md` | **not offered** | there are no images in them |

### The vocabulary this order adds (it is the contract, exactly like §5's)

| Term | Definition |
|---|---|
| **Image asset** | one picture FILE inside the document archive (`ppt/media/image3.png`). It is what carries bytes, and it is what a decision is attached to. |
| **Image occurrence** | one PLACE that asset is used (slide 4, the header of page 2, the slide master). One asset can have many occurrences. |
| **Image treatment** | what happens to an asset on export: `keep`, `box`, `blur` or `remove`. |
| **Image status** | what the review banner filters on: **Kept** (`keep`) or **Anonymised** (any of the other three). There is no third status: every asset starts `keep`, so nothing is ever "undecided". |
| **Location** | where an occurrence sits, in the document's own words: "Slide 4", "Slide master", "Page 2", "Header", "Hidden slide 7". |

A treatment is attached to the ASSET, never to the occurrence (decision 3). A
logo used on five slides is one row's worth of decision, and the row says so.

### The invariants every batch must hold

1. **The original pixels always leave the archive.** All three anonymising
   treatments overwrite the asset's bytes in the produced file. A `remove` that
   only deleted the drawing element and left `ppt/media/image3.png` in the zip
   would be a leak that LOOKS like a redaction, which is worse than no feature.
   `TestExportedArchiveKeepsNoAnonymisedOriginalBytes` is the permanent guard.
2. **Blur must destroy information, not hide it.** A Gaussian blur is partially
   invertible and a light blur over text is simply readable. The implementation
   is mosaic-then-smooth (Appendix A), which throws the samples away.
3. **A control that does not anonymise is never labelled "anonymise".** This is
   why SVG has no blur (decision 4): blurring a vector leaves every original
   shape and every original text string in the file.
4. **The engine stays UI-agnostic.** `backend/engine/imaging` takes bytes and
   returns bytes. It never sees a path, a Wails context or a filename the user
   chose.
5. **The original file on disk is read once at import and never written.**
   Unchanged, and it constrains where the bytes come from: `Document.Raw`.

### Decisions the owner has already approved

These four were put to the owner during planning and answered. Do not re-litigate
them; if you believe one is wrong, say so in the pull request and leave the code
as specified.

1. **PDF is dropped**, with one explanatory line in the IMAGE tab. Full reasoning
   in Appendix C.
2. **No new Go module.** The box text is drawn with a vendored 8x8 bitmap font
   table (an asset, not a module). The owner declined `golang.org/x/image`.
3. **One decision per image asset**, applied to every place it appears, with a
   visible "appears in N places" note. Per-occurrence decisions would need
   picture-part cloning and relationship rewriting in the exporter, which is the
   riskiest code the feature could contain.
4. **No blur for SVG.** SVG assets offer "Replace with a box" and "Remove" only;
   the blur control renders disabled with a tooltip that says why.

### The four batches at a glance

| Batch | Owns | Touches the UI? | Ends with |
|---|---|---|---|
| **B1** | `backend/engine/imaging`: the scanner, the model, the thumbnailer. Two bound methods. | no | the app can LIST the images in a docx/pptx and show a thumbnail. Nothing is changed in any export. |
| **B2** | the treatments, the export rewriting, the decision store, the session field. Three bound methods. | no | the app can APPLY a treatment. Fully testable headless; no user can reach it yet. |
| **B3** | the TEXT / IMAGE tabs and the whole IMAGE review surface. | yes | the feature works end to end. |
| **B4** | report, export-screen wiring, the leak audit, the parity guards, the charters, the user docs. | small | the feature is documented, guarded and honest about what it did. |

The order is strictly forward: B2 needs B1's model, B3 needs B2's methods, B4
needs B3's screen. **Nothing user-visible promises anything the engine cannot
already do**, which is why the two backend batches come first.

---

## B1 — The inventory: what pictures are in this document

**What B0 hands you:** nothing. This is the first batch; `main` carries this
document and no code for it.

**Goal:** given an imported .docx or .pptx, produce the list of its image assets
with everything the review screen's seven columns need, plus a thumbnail. No
treatment, no export change, no UI.

### New package: `backend/engine/imaging`

A new package under the engine, for the reason `convert` and `exportfmt` are
separate packages: it is used by BOTH (the scan reads the import bytes, the
treatments write the export bytes) and it must not import either. It imports
`archive/zip`, `encoding/xml`, `image`, `image/png`, `image/jpeg`, `strings`,
`sort`, `fmt`, `path` and nothing else from the repository.

Files created in B1:

```
backend/engine/imaging/
├── imaging.go      # package header, the model, the format sniffer
├── scan.go         # the OOXML picture scanner (docx + pptx share it)
├── locate.go       # occurrence -> a human location string
├── thumbnail.go    # decode + box-filter downscale + PNG encode
├── imaging_test.go
├── scan_test.go
└── scan_integration_test.go
```

### The model (`imaging.go`)

```go
// Asset is ONE picture file inside the document archive. It is the unit a
// decision is attached to: a logo used on five slides is one Asset with five
// Occurrences, because a user reviewing "the logo" is answering one question.
type Asset struct {
    // ID is the archive part path, e.g. "ppt/media/image3.png". It is stable
    // across re-imports of the same file, which is what lets a decision taken
    // in one session be re-applied in the next.
    ID string
    // Name is what the picture calls itself: the first occurrence's cNvPr
    // name or descr attribute, falling back to the part's base name. A user
    // recognises "Acme group logo"; nobody recognises "image7.png".
    Name string
    // Format is sniffed from the BYTES, never from the extension: a .png part
    // holding JPEG bytes is common enough to matter.
    Format Format // FormatPNG | FormatJPEG | FormatSVG | FormatOther
    // Bytes is the part's decompressed size.
    Bytes int
    // Width and Height are pixels for raster assets, and the declared or
    // viewBox size for SVG. Zero for a format we cannot read.
    Width, Height int
    // Companion is the SVG part of an SVG picture, whose primary part is the
    // PNG fallback Office writes beside it. Empty for everything else. A
    // treatment must reach BOTH parts or Office shows the untreated one.
    Companion string
    // Linked marks an image referenced by r:link rather than r:embed: its
    // bytes are NOT in the archive. It can be removed and nothing else.
    Linked bool
    // Occurrences are every place this asset is used, in document order.
    Occurrences []Occurrence
}

// Occurrence is one PLACE an asset is used.
type Occurrence struct {
    // Part is the XML part the picture element sits in
    // ("ppt/slides/slide4.xml", "word/header2.xml").
    Part string
    // Ordinal is this occurrence's index among the picture elements of that
    // part, from 0. Part plus Ordinal identifies the element without carrying
    // a byte offset, which is what lets the export re-scan its own rewritten
    // bytes and still find the same picture (B2 depends on this).
    Ordinal int
    // Kind is what encloses the blip, and it decides what "remove" can do.
    Kind Kind // KindPicture | KindFill | KindBackground
    // Location is the ready-to-print place, e.g. "Slide 4", "Page 2",
    // "Header", "Slide master", "Hidden slide 7".
    Location string
    // DisplayEMU is the frame the picture is drawn in, in English Metric
    // Units (914400 per inch). Zero when the source does not state it.
    DisplayCX, DisplayCY int
}
```

`Format`, `Kind` and the location kinds are string constants with an
`All…` slice each, because B3 mirrors them in `state.js` and
`image_parity_test.go` will hold the two sides together.

`Inventory` is what crosses the bridge:

```go
type Inventory struct {
    // Applicable is false for every format that has no image review, and
    // Reason is then a CODE the frontend maps to copy ("format_not_supported",
    // "pdf_images_removed"). A code rather than a sentence because copy.js is
    // the single home for user-visible strings (frontend/CLAUDE.md).
    Applicable bool     `json:"applicable"`
    Reason     string   `json:"reason,omitempty"`
    Assets     []Asset  `json:"assets"`
    // Warnings are per-document notes worth showing above the list, as codes:
    // "unreadable_part" when a picture part could not be decoded,
    // "linked_images" when at least one picture lives outside the file.
    Warnings   []string `json:"warnings,omitempty"`
}
```

### The scanner (`scan.go`)

One function, two entry points:

```go
func ScanDocx(raw []byte) (Inventory, error)
func ScanPptx(raw []byte) (Inventory, error)
```

Both delegate to a shared `scanOOXML(raw []byte, profile profile) (Inventory, error)`
where `profile` names the parts to walk, the removable element names and the
location resolver. Everything else is identical between the two formats, and
keeping it identical is the point: the export in B2 walks the same parts with the
same identity rule.

**Parts walked**

| Format | Parts | Location prefix |
|---|---|---|
| pptx | `ppt/slides/slideN.xml` | "Slide N" (see hidden slides below) |
| pptx | `ppt/slideLayouts/slideLayoutN.xml` | "Slide layout" |
| pptx | `ppt/slideMasters/slideMasterN.xml` | "Slide master" |
| pptx | `ppt/notesSlides/notesSlideN.xml` | "Notes on slide N" |
| docx | `word/document.xml` | "Page N" when Word cached the breaks, "Body" otherwise |
| docx | `word/headerN.xml` | "Header" |
| docx | `word/footerN.xml` | "Footer" |
| docx | `word/footnotes.xml`, `word/endnotes.xml` | "Footnotes" / "Endnotes" |

Slide numbers come from the entry name (`slide12.xml` is slide 12), exactly as
`convert/pptx.go` already does with `slideEntryRe`. Notes slides resolve their
slide through `ppt/slides/_rels/slideN.xml.rels`, mirroring
`convert/pptx.go slideNotes`, so a reordered deck still labels them correctly.

**What is a picture**

Scan each part with `xml.Decoder.RawToken()` (never `Unmarshal`: the parts carry
namespaces and mixed content, and B2 needs the same walk to splice bytes). A
picture occurrence is recorded when either is seen:

- `<a:blip r:embed="rIdN">` or `<a:blip r:link="rIdN">` (DrawingML, both formats)
- `<v:imagedata r:id="rIdN">` (legacy VML, inside `<w:pict>`; old Word documents
  and pasted content still produce it)

The `Kind` comes from the nearest enclosing element that was open when the blip
was seen:

| Enclosing element | Kind | What "remove" will mean in B2 |
|---|---|---|
| `p:pic` (pptx), `w:drawing` or `w:pict` (docx) | `KindPicture` | delete that whole element |
| `a:blipFill` inside a shape or a table cell property | `KindFill` | no element to delete: the asset's bytes become a 1x1 transparent PNG |
| `p:bg` / `p:bgPr` | `KindBackground` | same as `KindFill` |

**Resolving the bytes.** `r:embed` is a relationship id, resolved against the
part's own rels file (`ppt/slides/_rels/slide4.xml.rels`,
`word/_rels/document.xml.rels`) and then against the part's directory:
a pptx target is `../media/image3.png` relative to `ppt/slides/`, a docx target
is `media/image3.png` relative to `word/`. Use `path.Join(path.Dir(part), target)`
and `path.Clean`; do not string-trim `../`. `r:link` targets are external:
record the occurrence with `Linked: true` and no asset bytes.

**SVG pairs.** Office writes an SVG picture as a PNG fallback in `r:embed` plus
the SVG itself in an extension:

```xml
<a:blip r:embed="rId3">
  <a:extLst><a:ext uri="{96DAC541-7B7A-43D3-8B79-37D633B846F1}">
    <asvg:svgBlip r:embed="rId4"/>
  </a:ext></a:extLst>
</a:blip>
```

When an `svgBlip` is found inside a blip, the asset's `ID` stays the PNG
fallback (that is what the relationship points at and what B2 must keep valid),
`Companion` becomes the resolved SVG part, and `Format` becomes `FormatSVG`,
because SVG is what the user sees and what decides which treatments are offered.

**Grouping.** Occurrences are grouped by resolved part path into one `Asset`.
Sort assets by first occurrence in document order (part order, then ordinal), so
the list reads like the document rather than like a zip directory.

**Hidden.** A pptx slide whose root element carries `show="0"` is hidden: its
location becomes "Hidden slide N". A shape whose `p:cNvPr` carries `hidden="1"`
gets " (hidden)" appended. Hidden pictures are the ones a user most needs told
about, so they are never filtered out.

**Page numbers in docx.** Reuse the rule `convert/docx.go` already applies: count
`<w:lastRenderedPageBreak/>` and `<w:br w:type="page"/>` elements seen so far in
`word/document.xml`; the occurrence's page is that count plus one. When the
document contains none of either (Word never rendered it), every body occurrence
is located "Body". Do not invent a page number: a wrong page is worse than no
page, and the user is looking at the file.

### Format sniffing and dimensions (`imaging.go`)

- PNG: the 8-byte signature, then `image.DecodeConfig` for the size.
- JPEG: `0xFF 0xD8 0xFF`, then `image.DecodeConfig`.
- SVG: the first non-whitespace bytes are `<?xml` or `<svg`, and an `<svg`
  element appears in the first 1024 bytes. Size from `width`/`height` when they
  are plain numbers or `px`, otherwise from `viewBox`'s third and fourth values,
  otherwise zero.
- Anything else (`emf`, `wmf`, `gif`, `tiff`, `bmp`): `FormatOther`, size zero.
  These are listed and can be REMOVED, and nothing else. They are not hidden: an
  image the user cannot see in the review is an image they believe was reviewed.
- A part that cannot be decoded at all still produces an `Asset` with its size in
  bytes and a `"unreadable_part"` warning. Never drop it.

Decoding is by SNIFFING and by `image.DecodeConfig` only: never decode full
pixels here. A 40-megapixel PNG must not be materialised to answer "what size is
it".

### The thumbnailer (`thumbnail.go`)

```go
// Thumbnail decodes a raster asset, scales it to fit maxPx on its longest
// side with a box filter, and returns PNG bytes. It never scales UP: a 40x40
// icon comes back 40x40, because a blurry enlargement tells the reviewer less
// than the real thing.
func Thumbnail(raw []byte, maxPx int) (png []byte, w, h int, err error)
```

The box filter is about forty lines: for each destination pixel, average the
source pixels of its footprint in linear-ish space (plain 8-bit averaging is
acceptable and is what the tests assert). Do not use `image/draw`: the standard
library's `draw` does not resample, so it would give you nearest-neighbour
aliasing.

SVG assets are NOT thumbnailed: their bytes are handed to the frontend as they
are and rendered by the WebView. A hard rule comes with that, and B3 repeats it:
**an SVG is only ever rendered through `<img src="data:image/svg+xml;base64,…">`,
never inlined into the page**, because an `<img>` context executes no script and
an inlined `<svg>` element does.

Cap: `Thumbnail` refuses a source above 40 megapixels with an actionable error
rather than allocating it. `maxPx` is clamped to `[16, 2048]`.

### The bound methods (`backend/app_images.go`, new file)

Following the existing method-group convention (`app_values.go`, `app_detect.go`,
`app_export.go`, `app_run.go`).

```go
// ListDocumentImages returns the image inventory of one IMPORTED document
// (not one result document: the pictures live in the original bytes, and the
// user reviews them before as well as after a run).
func (a *App) ListDocumentImages(name string) (imaging.Inventory, error)

// ImageThumbnail returns one asset's preview as a data URL, ready for an
// <img src>. maxPx is the longest side.
func (a *App) ImageThumbnail(docName, assetID string, maxPx int) (ImageThumb, error)

type ImageThumb struct {
    DataURL string `json:"dataUrl"` // "data:image/png;base64,…" or image/svg+xml
    Width   int    `json:"width"`
    Height  int    `json:"height"`
}
```

`ListDocumentImages` dispatches on `Document.Format`:

| Format | Answer |
|---|---|
| `docx` | `imaging.ScanDocx(doc.Raw)` |
| `pptx` | `imaging.ScanPptx(doc.Raw)` |
| `pdf` | `{Applicable: false, Reason: "pdf_images_removed"}` |
| everything else | `{Applicable: false, Reason: "format_not_supported"}` |

An unknown document name is an error with the shape the other methods use
("document %q is not imported, ..."). Results are **cached on the App**
(`a.imageScans map[string]imaging.Inventory`, guarded by `a.mu`) because the
screen calls this on every repaint and a 60-slide deck is not free to re-scan;
the cache is dropped by `removeDocument`, `resetSession` and any new import.

`ImageThumbnail` reads the asset's bytes out of `Document.Raw` on demand. It does
not cache: the thumbnails are the biggest thing in this feature and holding all
of them for a 200-image deck is how a desktop app starts swapping.

### Documentation to update in B1

- `frontend/BRIDGE.md`: a new "## Anonymise: images" section with the two
  wrappers, in the table style the rest of the file uses. Say plainly that
  `ListDocumentImages` reads the IMPORTED document and needs no run.
- `backend/CLAUDE.md`: `engine/imaging` in the module map, with one paragraph on
  why it is a package of its own.
- `frontend/api.js`: the two wrappers. Nothing else in the frontend.

### Tests (B1)

Tier and category per `docs/TESTING.md`. Prefix every subtest.

**Unit** (`backend/engine/imaging/*_test.go`, category `extraction`):

- `extraction/sniff_*`: format sniffing over a table of hand-built byte
  prefixes, including a `.png`-named part holding JPEG bytes and an SVG with
  only a `viewBox`.
- `extraction/scan_pptx_*`: a generated deck with a picture on slide 1, the SAME
  picture again on slide 3, a different picture on the master, an SVG pair, and
  a hidden slide carrying one. Asserts: four assets, the shared one carrying two
  occurrences in slide order, locations "Slide 1"/"Slide 3"/"Slide master"/
  "Hidden slide 4", the SVG asset reporting `FormatSVG` with a non-empty
  `Companion`.
- `extraction/scan_docx_*`: a generated document with an inline picture, a
  floating (`wp:anchor`) picture, a legacy `w:pict` VML picture, one in a header,
  and a `w:lastRenderedPageBreak` before the last one. Asserts the locations
  including "Page 2" and "Header".
- `extraction/scan_rels_*`: relationship resolution, including a pptx
  `../media/x.png` target and a docx `media/x.png` target reaching the same
  cleaned path, and an `r:link` occurrence coming back `Linked: true` with no
  bytes.
- `extraction/scan_empty_*`: a deck with no pictures gives `Applicable: true`
  and zero assets, which is an empty list and not an error.
- `errors/scan_corrupt_*`: a part that is not well-formed XML produces an
  actionable error naming the part, and a picture part that cannot be decoded
  produces an asset plus the `unreadable_part` warning rather than a failure.
- `extraction/thumbnail_*`: scaling maths (a 1000x400 source at maxPx 200 comes
  back 200x80), the never-scale-up rule, the megapixel refusal, and that the
  output decodes as PNG.

**Integration** (`scan_integration_test.go`, `//go:build integration`): one pass
per format over the committed fixtures below, asserting the inventory end to end
through `App.ListDocumentImages` (the bound layer, so the format dispatch and the
cache are covered once).

**Fixtures.** Extend `backend/engine/convert/fixtures_test.go`'s generator with
`images.docx` and `images.pptx` (the helper writes them into `backend/testdata/`
on first run and they are then committed). The pictures they embed are generated
in code with `image/png` and `image/jpeg`: a 120x80 red PNG, a 200x200 blue
JPEG, and a literal SVG string. Content stays obviously fictional and in English
and French, per `docs/TESTING.md`'s fixture rules.

Note the sequencing consequence: `fixtures_test.go` lives in package `convert`,
and `imaging`'s tests cannot import it. Move the two new generators into a small
`backend/testdata/gen` helper OR duplicate the twenty lines that build a zip. The
second is cheaper and has no import-cycle risk; if you duplicate, say so in the
file header so nobody "fixes" it later.

### Done when

- `go test ./...` and `node --test "frontend/**/*.test.js"` are green.
- `go test -tags=integration ./backend/...` is green.
- `App.ListDocumentImages("images.pptx")` returns the four assets, and
  `App.ImageThumbnail` returns a data URL that decodes.
- No export, no pipeline and no UI behaviour has changed. `git diff --stat`
  touches nothing under `frontend/views/`.

---

## B2 — The treatments: making a decision change the exported file

**What B1 hands you:** `backend/engine/imaging` with the model
(`Asset`, `Occurrence`, `Inventory`), the OOXML scanner, the format sniffer and
the thumbnailer; `App.ListDocumentImages` and `App.ImageThumbnail`; the
`images.docx` / `images.pptx` fixtures. Nothing applies a treatment yet.

B1 left four things that differ from what this section assumed, and they change
what you write:

1. **The data-URL building lives in the engine, not in the bound method.**
   `imaging/preview.go` holds `AssetBytes(raw, partName)` and
   `Preview(raw, asset, maxPx)`; `App.ImageThumbnail` is the thin adapter over
   them that `backend/CLAUDE.md` requires. Put `PreviewImageTreatment`'s own
   bytes-to-data-URL step there too rather than in `app_images.go`.
2. **`Asset` and `Occurrence` carry lowerCamel JSON tags** (`id`, `name`,
   `format`, `bytes`, `width`, `height`, `companion`, `linked`, `occurrences`;
   `part`, `ordinal`, `kind`, `location`, `displayCX`, `displayCY`), because
   Wails would otherwise send Go field names across the bridge. `Decision` and
   its fields must be tagged the same way. The wire shape is written out in
   `frontend/BRIDGE.md`.
3. **`Occurrence.Ordinal` counts EVERY picture occurrence in its part**, in the
   order the walk finishes each one (a DrawingML blip counts when its element
   closes, so the SVG extension inside it is already seen; a legacy VML
   `v:imagedata` counts where it appears). Backgrounds and shape fills are
   counted too. The export must re-scan with `walkPart` itself, not a private
   walk, or the ordinals will not line up.
4. **The bound layer's integration test lives in `backend/`, not in
   `engine/imaging/`.** Injecting imported documents into the App needs
   `app.docs`, which is unexported, so a test in package `imaging_test` cannot
   reach it: `backend/app_images_integration_test.go` covers the bound layer
   (format dispatch, the cache, a decoding preview) and
   `backend/engine/imaging/scan_integration_test.go` covers the committed
   fixtures through the engine. Add B2's export tests the same way.
5. **A legacy VML picture has no display size.** VML states its size in a CSS
   `style` attribute the scanner does not parse, so `DisplayCX`/`DisplayCY` are
   0 there and the box treatment falls through to its 640x480 default. That is
   the case to keep in mind when you write the box fallback chain.

**Goal:** a decision recorded against an asset changes the .docx / .pptx the user
exports, and can be previewed exactly as it will come out. Still no UI.

### The decision model (`backend/engine/imaging/decision.go`)

```go
// Treatment is what happens to an asset on export.
type Treatment string

const (
    TreatmentKeep   Treatment = "keep"   // the default for every asset
    TreatmentBox    Treatment = "box"    // a filled rectangle of the same pixel size, carrying the user's text
    TreatmentBlur   Treatment = "blur"   // mosaic then smooth: the samples are thrown away
    TreatmentRemove Treatment = "remove" // the picture goes, and so do its bytes
)

// AllTreatments is the ordered list state.js mirrors (image_parity_test.go).
var AllTreatments = []Treatment{TreatmentKeep, TreatmentBox, TreatmentBlur, TreatmentRemove}

// Decision is one asset's answer.
type Decision struct {
    Treatment Treatment `json:"treatment"`
    // BoxText is drawn into the rectangle, centred, wrapped. Empty is allowed
    // and gives a plain rectangle. Capped at MaxBoxText runes.
    BoxText string `json:"boxText,omitempty"`
    // BlurStrength is 1 to 10, and it is RELATIVE to the image's own size: a
    // fixed pixel radius does nothing to a 4000px screenshot and destroys a
    // 60px icon. See mosaicFactor.
    BlurStrength int `json:"blurStrength,omitempty"`
}

const MaxBoxText = 120
const DefaultBlurStrength = 5
```

`Decision.Validate(a Asset) error` is the ONE place the per-format rules live, so
the frontend, the bound method and the exporter cannot disagree about them:

| Rule | Message (actionable, no em dash) |
|---|---|
| unknown treatment | `unknown image treatment %q, expected one of: keep, box, blur, remove` |
| `blur` on `FormatSVG` | `an SVG image cannot be blurred: a blur filter leaves the original shapes and text inside the file. Use "Replace with a box" or "Remove" instead.` |
| `blur` or `box` on `FormatOther` | `this picture is a %s file, which this application cannot redraw. It can be removed, or kept as it is.` |
| `blur` or `box` on a `Linked` asset | `this picture is linked from outside the document, so there are no bytes here to change. It can be removed from the document, or kept.` |
| `BoxText` over `MaxBoxText` | `the box text is %d characters, the maximum is 120. Shorten it.` |
| `BlurStrength` outside 1..10 | `the blur strength is %d, it must be between 1 and 10.` |

### The treatments (`backend/engine/imaging/treat.go`)

One entry point, so nothing can apply a treatment through a side door:

```go
// Treat produces the REPLACEMENT bytes for one asset under one decision, in
// the SAME encoding as the source: a JPEG part comes back JPEG, a PNG part
// PNG, an SVG part SVG. The encoding is not a detail: the archive's
// [Content_Types].xml maps the part's extension to a MIME type, so a PNG
// written into image2.jpeg is a file Word may refuse to draw.
//
// It returns nil bytes for TreatmentKeep: nothing to write.
func Treat(src []byte, a Asset, d Decision) (replacement []byte, err error)
```

**`box`.** Decode the source to get its pixel size (SVG: its declared or viewBox
size; `FormatOther` or an undecodable part: fall back to the occurrence's display
size converted from EMU at 96 dpi, and to 640x480 if even that is absent). Then:

- raster: build an `image.RGBA` of exactly that size, fill it with `boxFill`
  (`#E5E7EB`), stroke a 1px `boxStroke` (`#9CA3AF`) border, draw the text in
  `boxInk` (`#2D2D2D`) centred on both axes (see the font below), and encode in
  the source's format. JPEG at quality 90; PNG at default compression.
- SVG: emit a NEW, minimal SVG of the same size with a `<rect>` and a `<text>`.
  The text is XML-escaped, uses `font-family="Helvetica, Arial, sans-serif"`
  (the application's own stack) and a font size computed to fit. The SVG box is
  the one place real font shapes are available, so it uses them; the raster box
  cannot, and that difference is expected rather than a defect.

**`blur`.** Raster only (Validate has already refused everything else).

```
f = clamp(round(min(w,h) * strength / 200), 2, min(w,h))   // mosaic block, in pixels
```

1. Average each `f x f` block of the source and write the average back over the
   whole block. This is what destroys the information: the block's samples are
   gone, not smeared.
2. Run ONE 3x3 box blur over the mosaic, to take the hard block edges off so the
   result reads as "deliberately obscured" rather than "broken file".
3. Encode in the source's format.

At strength 5 a 600px-wide photo gets 15px blocks and a 4000px screenshot gets
100px blocks, which is the scale-invariance the relative rule buys. Strength 1
is deliberately weak; the copy in B3 says so, and the user is looking at a live
preview of the actual output.

**`remove`.** The replacement bytes are a 1x1 fully transparent PNG for every
raster source and a 1x1 empty SVG for an SVG source, and the picture ELEMENT is
deleted by the export pass below. The bytes are still overwritten even though
nothing references them any more: an orphan part inside the zip is exactly the
leak invariant 1 exists to prevent.

### The vendored font (`backend/engine/imaging/font8x8.go`)

The standard library has no font rasteriser and the owner declined a new module,
so the box text is drawn from an 8x8 bitmap table vendored as Go source, the same
way the Material Symbols SVGs are vendored as assets.

- Vendor the **font8x8 basic block** (Daniel Hepper's `font8x8`, public domain;
  originally the IBM PC 8x8 ROM font), ASCII 32 to 126, as
  `var glyphs8x8 = [95][8]byte{…}`. The file header records the source and its
  public-domain status, and `CLAUDE.md` §7 gains an assets row for it beside the
  Material Symbols one. If you cannot reach the source, author the table by
  hand: it is 95 glyphs of 8 bytes and a test asserts every one is non-empty
  except space.
- Characters outside the table are FOLDED, not dropped: a small table maps the
  Latin-1 letters a French or German document actually contains
  (`é è ê ë à â ä ç î ï ô ö ù û ü ÿ ñ ß` and their capitals, plus the typographic
  quotes and dashes) to their nearest ASCII form (`ß` becomes `ss`). Whatever
  survives neither is drawn as `?`. The copy in B3 says the box text is drawn
  with a built-in font and that accents are simplified, so the user is not
  surprised by what comes out.
- Layout: pick the largest integer scale factor at which the wrapped text fits
  the box with a one-glyph margin, down to 1; below that, truncate the last line
  with "..." (three dots, not an ellipsis character, which the table has no glyph
  for). Wrap on spaces, then hard-wrap a word longer than the line.

### The export pass (`backend/engine/exportfmt/images.go`, new file)

The export runs the existing text rewrite FIRST and the image rewrite SECOND, as
two independent passes over the same part. Sequential rather than merged, because
a merged splice set has to reconcile a text replacement that falls INSIDE a
picture element being deleted (a Word text box lives inside `w:drawing`), and
"apply text, then re-scan the result and apply images" has no such case: the
second pass reads bytes the first pass already finished with.

That is also why B1's occurrence identity is `part + ordinal` and not a byte
range. The image pass re-scans the rewritten part with the same walker and finds
the same pictures in the same order.

```go
// ImagePlan is what the App hands the exporter: the decisions, keyed by asset
// ID, plus the inventory they were taken against. An empty plan means "change
// no picture", which is exactly today's behaviour.
type ImagePlan struct {
    Inventory imaging.Inventory
    Decisions map[string]imaging.Decision
}
```

`Config` (in `exportfmt.go`) gains `Images ImagePlan`. `ExportDocx` and
`ExportPptx` gain, inside their existing `rewriteZip` selector:

- for a text part: the existing `rewriteTextPart`, then `rewriteImagePart` when
  the plan touches any picture in that part.
- for a MEDIA part named by a non-`keep` decision: a rewriter returning
  `imaging.Treat(...)`'s bytes. This is a new selector branch; every media part
  not named by a decision keeps its bit-for-bit passthrough.
- an asset's `Companion` (the SVG behind a PNG fallback) is rewritten in the same
  breath, from the same decision.

`rewriteImagePart(data []byte, part string, plan ImagePlan) ([]byte, int, error)`
deletes the removable element of every `remove` occurrence in that part, and does
one more thing for `box` and `blur`: it strips `<a:srcRect .../>` from that
picture's `blipFill`. A source rectangle crops the picture inside its frame, and
a crop of a box would show a corner of the rectangle with the text outside the
frame. Dropping it shows the whole replacement in the same frame, which is what
the user asked for. Nothing else in the XML is touched.

Deletion is by raw byte range: the walker records the element's start offset (the
`<p:pic` / `<w:drawing` / `<w:pict` token start) and its end offset (just past the
matching close tag) with `Decoder.InputOffset()`, exactly as `ooxml.go` does for
text nodes, and the splices are applied back to front. A `KindFill` or
`KindBackground` occurrence has no removable element: its `remove` is served by
the 1x1 transparent bytes alone, which is invisible in the layout and leaks
nothing.

Both exporters return an `imaging.Summary{Kept, Boxed, Blurred, Removed int}`
alongside what they already return, so B4 can report it.

### The decision store (`backend/app_images.go`, extended)

```go
// SetImageDecision records one asset's treatment. It validates against the
// asset it names, so a refusal reaches the user beside the control that caused
// it rather than at export time.
func (a *App) SetImageDecision(docName, assetID string, d imaging.Decision) error

// PreviewImageTreatment renders what the export WILL produce for one asset,
// scaled to a thumbnail. It runs the real Treat, then the real Thumbnail, so
// the preview cannot promise something the export does not do.
func (a *App) PreviewImageTreatment(docName, assetID string, d imaging.Decision, maxPx int) (ImageThumb, error)

// ResetImageDecisions drops every decision for one document (the "keep all"
// bulk action).
func (a *App) ResetImageDecisions(docName string) error
```

The store is `a.imageDecisions map[string]map[string]imaging.Decision` (document
name, then asset ID), guarded by `a.mu`, living on the App for the reason
`a.removed` does: the export builds its own config from `a.lastReq`, so a decision
carried only in a request would be honoured by one path and forgotten by the
other. A `keep` decision is stored as an ABSENT key, so the map holds only what
the user changed and "no decisions" is one empty map rather than one entry per
picture.

`ListDocumentImages` (B1) now returns each asset with its current decision
attached (`Asset.Decision Decision`), so the screen has one call rather than two
and cannot render a row whose decision it has not read.

`SaveSameFormat` / `sameFormatBytes` fill `Config.Images` from the store and the
cached inventory. A document with no decisions produces byte-identical output to
today, and a test asserts exactly that.

### Session persistence (`backend/engine/session.go`)

`Session` gains:

```go
// ImageDecisions is document name -> asset ID -> decision. Only non-keep
// decisions are stored.
ImageDecisions map[string]map[string]imaging.Decision `json:"imageDecisions,omitempty"`
```

**`SessionVersion` goes from 8 to 9**, with this reason appended to the history
comment beside the constant:

```
v9: the session carries the image treatments. A v8 file has none, and a v8
    READER silently ignores the field: it would load a session in which the
    user had boxed the client logo, export the .docx, and ship the logo. The
    strict-version rule exists for exactly this shape of failure, where the
    file loads, nothing errors, and the output is wrong.
```

Note this is the borderline case the constant's own comment describes ("an added
field the loader can ignore is not a bump"). It IS a bump here because the field
is not inert: what the old reader ignores is a redaction. Record that reasoning
in the comment; do not shorten it to "added image decisions".

### Tests (B2)

**Unit** (`backend/engine/imaging`, category `redaction` for the treatments,
`config` for validation):

- `config/decision_validate_*`: the whole table above, message by message, each
  asserted for the actionable half (it names the fix).
- `redaction/box_*`: output decodes; output is exactly the source's pixel size;
  output is the source's ENCODING (a JPEG source gives JPEG bytes); the centre
  region has ink when text was given and none when it was not; a 200-character
  text is refused by Validate before it reaches Treat.
- `redaction/blur_*`: `mosaicFactor` over a table of sizes and strengths; the
  destructiveness property, asserted as information loss rather than as
  appearance (a source of random noise and its blurred output differ in every
  block, and the blurred output has at most `ceil(w/f)*ceil(h/f)` distinct block
  averages before the smoothing pass, which is what "the samples are gone"
  means); a strength-10 output of a text image has no run of high-contrast
  pixels left at the glyph scale.
- `redaction/remove_*`: the replacement is a 1x1 of the right encoding.
- `fonts/glyphs_*`: every printable ASCII has a non-empty glyph except space;
  the folding table maps every accented character it claims; an unmappable rune
  becomes `?`; the scale-and-wrap layout picks the expected factor for a known
  box and text.

**Unit** (`backend/engine/exportfmt`, category `roundtrip`):

- `roundtrip/images_no_decisions_*`: an export with an empty plan is
  byte-identical to an export without the field. This is the regression guard
  for every existing user.
- `roundtrip/images_remove_element_*`: the picture element is gone from the
  rewritten part and the surrounding paragraph still parses.
- `roundtrip/images_srcrect_*`: `a:srcRect` is stripped for `box` and `blur`,
  and left alone for `keep`.

**Integration** (`//go:build integration`, category `roundtrip`):

- `roundtrip/export_images_docx_*` and `…_pptx_*`: take the fixture, decide one
  `box`, one `blur` and one `remove`, export, then re-open the produced archive
  and assert: it is a valid zip, every part parses as XML, the treated media
  parts decode at the expected sizes, and the removed picture's element is
  absent.
- **`roundtrip/exported_archive_keeps_no_original_bytes`**: THE leak guard of
  invariant 1. For every asset with a non-keep decision, no entry of the produced
  archive contains the asset's original bytes, and no entry's SHA-256 matches
  the original asset's. Run it over both fixtures. This test is load-bearing;
  name it in the file header as such so nobody weakens it into a spot check.
- `roundtrip/session_v9_*`: a saved session round-trips the decisions, and a
  version-8 file is refused with the existing message.

**Deep** (optional, `//go:build deep`): nothing new. There is no statistical or
visual property here that the integration tier cannot observe.

### Done when

- All three tiers are green.
- Exporting a fixture with one decision of each kind produces a file whose
  pictures are boxed, blurred and gone, and whose original picture bytes are
  nowhere in the archive.
- Exporting with no decisions produces bytes identical to `main`'s.
- `SessionVersion` is 9 with its reason recorded, and a v8 file is refused.
- Still nothing under `frontend/views/` has changed.

### What B2 left behind that differs from the above

1. **The three bound methods are wrapped in `api.js` and written into
   `frontend/BRIDGE.md` already**, following what B1 did with its own two: a
   bound method with no wrapper is unreachable, and the bridge contract is the
   design-to-code handoff surface rather than a view. Nothing under
   `frontend/views/` changed, and no view calls them yet.
2. **The error messages carry no trailing full stop.** `.golangci.yml` enables
   revive's `error-strings` rule (no capital, no trailing punctuation), so the
   table's messages are used verbatim except for the final period, with the
   clauses joined by `;` the way the rest of the repository's errors are.
3. **`Decision.Validate` accepts `BlurStrength` 0** as "not stated", which reads
   as `DefaultBlurStrength`. The `omitempty` encoding makes zero the absent
   value, and every absent value in this application reads as its default rather
   than as "none". Anything else outside 1..10 is refused with the table's
   message.
4. **The linked-asset rule is checked BEFORE the format rule**, because the scan
   reports a linked picture as `FormatOther` and "there are no bytes here" is the
   true reason: "this application cannot redraw this format" would send the user
   looking for a converter that cannot help.
5. **A box above the 40-megapixel decode limit is scaled down rather than
   refused**, and a blur above it is refused with a message naming box and remove
   (neither of which decodes the picture). The OOXML frame decides the drawn
   size, so a smaller rectangle fills the same space; a blur has no such way out.
6. **`session_v9_*` is in the UNIT tier**, in `backend/engine/session_test.go`
   beside every other session round-trip, because session save/load is pure JSON
   with no I/O and `docs/TESTING.md` decides the tier by what a test requires.
7. **The committed JPEG and PNG fixture pictures now carry a gradient instead of
   a flat colour** (`texturedPNG` / `texturedJPEG` in
   `backend/engine/convert/fixtures_test.go`, and `images.docx` / `images.pptx`
   regenerated). A flat colour blurs to itself and re-encodes to the same bytes,
   so the leak guard passed on a blur that had done nothing. It found that on its
   first run, which is the guard working.
8. **`convert` gained `TestEveryCommittedFixtureIsGeneratable`.** No test in that
   package asked for `images.docx` or `images.pptx`, so the instruction every
   other package prints ("run `go test -tags=integration
   ./backend/engine/convert/` once and commit what it writes") did not actually
   write them.
9. **`copy_guard_test.go` now walks `backend/engine/imaging`.** That package
   holds user-facing error strings and was outside the guard's directory list.
10. **`CLAUDE.md` §5 and `backend/CLAUDE.md` record `SessionVersion` 9 already**,
    because leaving them saying 8 would make the charter describe a contract the
    code no longer honours. The rest of B4's documentation work is untouched.

---

## B3 — The IMAGE tab: the review surface

**What B2 hands you:** a backend that can list a document's images with their
current decision (`App.ListDocumentImages`), render a thumbnail
(`App.ImageThumbnail`), render a PREVIEW of any treatment exactly as the export
will produce it (`App.PreviewImageTreatment`), record a decision
(`App.SetImageDecision`), clear them all (`App.ResetImageDecisions`), and apply
them to the same-format export. Nothing in the interface reaches any of it yet.

**All five are already wrapped in `api.js`** (`listDocumentImages`,
`imageThumbnail`, `setImageDecision`, `previewImageTreatment`,
`resetImageDecisions`) with their contract in `frontend/BRIDGE.md`, including the
`Decision` wire shape and the per-format table of which treatments may be
offered. So this batch writes views and state, not bridge plumbing. Read the
BRIDGE.md "Anonymise: images" section before designing the treatment panel: the
disable rules the panel needs are already written down there, and
`setImageDecision` refuses what the panel must not offer.

**Goal:** step 3 gains the TEXT / IMAGE tabs and the whole image review surface.
At the end of this batch the feature works end to end for a user.

### Where the tabs sit (decision 5, flip it in review if you disagree)

The tab bar sits at the **top of the step 3 workspace**, above both columns.

- **TEXT** renders exactly what step 3 renders today: the left card column (Run,
  Replaced values, Report, Add missed Value) and the Compare card. Not one pixel
  of it changes. This matters: the TEXT tab is the whole existing screen, and a
  batch that "tidies it up on the way past" is a batch whose regression is
  invisible until a user notices.
- **IMAGE** replaces the workspace with a full-width review surface.

Full width rather than a tab inside the Compare card, because the content view
has seven columns and a preview thumbnail. Half a workspace makes it a horizontal
scroller, and the fixed-height layout contract (`frontend/CLAUDE.md`) says wide
content scrolls inside its own container, not that a screen may be built to need
it.

The **document selector is shared**: the IMAGE tab reads and writes the same
`s.resultDoc` the Compare card uses, so switching tabs keeps you on the same
file, and switching file keeps you on the same tab.

### The screen, top to bottom

1. **The tab bar** (`ui.js tabbar`, `attr: "anontab"`): `TEXT`, `IMAGE` with a
   count badge holding the number of images in the selected document (absent
   when the format has none).
2. **The banner**: the document selector on the left; the status filter as a
   `chipRow` of `All (12)`, `Kept (9)`, `Anonymised (3)` with live counts; the
   view toggle as a second `chipRow` of `Details`, `Tiles` on the right.
3. **The list**, which is the ONLY scrolling element on this screen: the content
   view or the tiles view.
4. **The footer**, `ui.js stepFooterHTML`, unchanged. Image decisions do NOT
   gate the move to step 4 (decision 6).

### The content view (the "Details" list)

A CSS grid, header row plus one row per asset, with the column template declared
ONCE as a module constant shared by the header and the rows, exactly as
`SUGGESTION_COLUMNS` is in `identifyworkspace.js` (that shared constant is why
the suggestions header and its rows cannot drift apart).

| Column | Contents |
|---|---|
| **Preview** | a 48px thumbnail. Loaded lazily, per row, and cached in state by asset ID: a 200-image deck must not fire 200 bridge calls on the first paint. Rows not yet loaded show a neutral placeholder box of the right shape. |
| **Name** | the asset's `Name`, with the part base name as the row's `title` attribute. |
| **Format** | `PNG`, `JPG`, `SVG`, or the raw extension for anything else. |
| **Dimensions** | `1200 x 800` in pixels. Blank when unknown, never `0 x 0`. The display size in centimetres goes in the `title`. |
| **Size** | `348 KB`, `1.2 MB`. One decimal above a megabyte, none below. |
| **Location** | the first occurrence's location, plus `+N more` when the asset appears in several places, whose `title` lists them all. This is where decision 3 becomes visible. |
| **Status** | `Kept`, or the treatment: `Boxed`, `Blurred`, `Removed`. |

Two direct buttons sit at the end of the row, `Keep` and `Anonymise`, so the
common answers cost one click.

### The tiles view

One card per asset: the preview at the top (fit inside a fixed box, never
stretched), the same metadata under it as a compact definition list, and the two
buttons `Keep` and `Anonymise`.

**The card is a fixed-height surface**, for the reason `frontend/CLAUDE.md`
already gives about value cards: when one card grows, every card below it moves,
the browser clamps the grid's scroll offset, and the next repaint snapshots the
clamped value, so the reader loses their place. So: the preview box is fixed, the
metadata list is fixed height with the location's `+N more` behind a `title`, and
the status is a chip rather than a wrapping sentence.

### The treatment panel

Pressing `Anonymise` (on a row or a tile) opens the treatment panel for that
asset. It is an in-app overlay through the existing mechanism, never a native
dialog (`frontend/CLAUDE.md`): render it the way `modal.js` renders the confirm,
with its markup in the view and its open/closed state in `state.js`.

It holds:

1. The **live preview**, produced by `App.PreviewImageTreatment` with the
   current draft decision. It is what the export will write, downscaled. Not a
   CSS filter: a CSS blur is not the blur the engine applies, and a preview that
   shows something other than the output is worse than no preview.
2. Three treatment choices as a `chipRow`: **Replace with a box**, **Blur**,
   **Remove**. Each renders DISABLED with an explaining `title` when
   `Decision.Validate` would refuse it for this asset (SVG cannot blur; an
   `emf`/`wmf` and a linked image can only be removed). The frontend mirrors the
   rule; the backend enforces it; `image_parity_test.go` holds the two together.
3. For **box**: a text field (max 120 characters, live counter) whose every
   change re-renders the preview, **debounced at 200 ms**. The field's hint says
   the text is drawn with a built-in font and that accents are simplified.
4. For **blur**: a 1 to 10 slider, same debounce, with the honest caption:
   `"Blur removes detail. It is not a guarantee: at a low strength, large text can still be read. Check the preview."`
5. `Apply` and `Cancel`. `Apply` calls `App.SetImageDecision` and closes;
   a rejection from Go renders ON the panel next to the field that caused it,
   the way every other per-surface error on this screen already does
   (`selectionError`, `missedError` in `anonymise.js`).

The panel keeps its draft in view-local state until Apply, so cancelling leaves
the stored decision untouched.

### The non-applicable formats

When `Inventory.Applicable` is false the tab still exists and renders one line,
mapped from the reason code in `copy.js`:

| Reason code | Copy |
|---|---|
| `pdf_images_removed` | `"PDF export rebuilds the document as text, so every image in a PDF is already removed from the exported file. There is nothing to review here."` |
| `format_not_supported` | `"Image review is available for Word and PowerPoint files. This document has no images to review."` |

Both are checked by `copy.test.js` for the em-dash rule like every other string.
The IMAGE tab is never hidden: a tab that appears and disappears as the user
changes file reads as a bug, and the sentence is the answer to the question the
missing tab would raise.

### state.js

New state, with its selectors, all in the one store:

```js
anonymiseTab: "text",              // "text" | "images"
imageFilter: "all",                // "all" | "kept" | "anonymised"
imageLayout: "details",            // "details" | "tiles"
images: {},                        // docName -> {loading, error, inventory}
imageThumbs: {},                   // assetKey -> dataUrl (assetKey = docName + " " + assetId)
imageEditor: null,                 // {docName, assetId, draft, preview, previewLoading, error} | null
```

Mirrored constants, which is what the new parity guard holds to Go:

```js
export const IMAGE_TREATMENTS = ["keep", "box", "blur", "remove"];
export const IMAGE_FORMATS = ["png", "jpeg", "svg", "other"];
export const IMAGE_STATUS_FILTERS = ["all", "kept", "anonymised"];
```

Selectors (pure, tested): `imagesFor(s, docName)`, `filteredImages(s)`,
`imageStatusCounts(s)`, `imageStatus(asset)` (the one place `keep` becomes
"Kept" and everything else becomes "Anonymised", so the banner, the column and
the tile chip cannot disagree), `treatmentAvailable(asset, treatment)` (the
frontend half of `Decision.Validate`).

### Files

| File | Change |
|---|---|
| `frontend/views/anonymise.js` | add the tab bar and dispatch on `s.anonymiseTab`. The existing render becomes the TEXT branch, moved WHOLE and otherwise untouched. |
| `frontend/views/anonymiseimages.js` | **new**: the entire IMAGE tab (banner, both views, the treatment panel, wiring). It is a sibling of `anonymise.js` for the reason `identifyrail.js` and `identifyworkspace.js` are siblings of `identify.js`: one screen, halves big enough to own a file each. |
| `frontend/state.js` | the state above, the constants, the selectors, the actions. |
| `frontend/api.js` | the three wrappers B2 added (B1's two are already there). |
| `frontend/copy.js` | a new `IMAGES` block: every string on this surface. |
| `frontend/style.css` | the grid template, the tile card, the treatment panel, the preview box. Every scrolling element gets its `min-height: 0` chain. |
| `scripts/uitest/probes.js` | a probe that opens step 3, switches to IMAGE, and reports the surface's geometry. |

**No new icons.** The filter chips, the view toggle and the two row buttons are
text (`chipRow` and `button` from `ui.js`), so `icon_parity_test.go` and the
vendored asset set are untouched. If you decide an icon is genuinely needed,
vendor BOTH the `.svg` under `frontend/assets/icons/` and its `ICONS` entry, or
the parity guard will fail the build.

### The SVG rendering rule (repeat it in the code)

Any SVG preview is rendered as `<img src="data:image/svg+xml;base64,...">` and
never inlined as markup. An `<img>` context runs no script; an inlined `<svg>`
element does, and the SVG in question came out of a client's document. Put that
sentence in the comment above the line that builds the tag.

### Tests (B3)

**Frontend, render tier** (`frontend/anonymiseimages.test.js`, using
`testhtml.js`): assert what the surface SHOWS, not that the HTML contains a
substring.

- the seven column headers, in order, in the details view.
- a shared asset's Location cell says `Slide 1 +2 more` and its `title` lists
  the three.
- the status column reads `Boxed` / `Blurred` / `Removed` / `Kept` per decision.
- the filter chips carry the live counts, and `Anonymised` selects exactly the
  non-keep assets.
- an unreadable dimension renders blank, never `0 x 0`.
- the two non-applicable reason codes render their sentence and no list.
- the treatment panel disables Blur for an SVG asset and gives the reason in the
  `title`; disables Blur AND Box for an `emf`; disables both for a linked image.

**Frontend, wiring tier** (`frontend/anonymiseimages.wiring.test.js`, using
`testdom.js`): the question here is what a control DOES, and `testdom.js` is the
DOM whose parser lower-cases attribute names the way a browser does. Every
`data-` attribute on this surface is lower-case, and `dataset_parity_test.go`
enforces it repository-wide.

- `Keep` and `Anonymise` on a row and on a tile call the right action with the
  right asset ID.
- the tab bar switches `anonymiseTab` and the TEXT branch still renders.
- the filter and view chips change only their own state.
- the slider and the text field debounce into ONE preview call, and Cancel does
  not call `setImageDecision`.

**Frontend, state tier** (`frontend/state.test.js`): the new selectors, the
status mapping, `treatmentAvailable`.

**Go, root guards:**

- `image_parity_test.go` (**new**, package `main`): `engine/imaging`'s
  `AllTreatments`, format constants and occurrence kinds are exactly what
  `state.js` mirrors, in the same order. The Go halves of the last two exist
  since B1 as `imaging.AllFormats` (`png`, `jpeg`, `svg`, `other`) and
  `imaging.AllKinds` (`picture`, `fill`, `background`). Same shape as
  `detection_parity_test.go`; read that file first and follow it, including how
  it parses the JS.
- `copy_guard_test.go` and `frontend/copy.test.js` pick up the new strings
  automatically; make sure they pass rather than adding an exemption.

**Deep, the render harness** (`scripts/uitest/renderharness`, category `layout`):
one case that switches to the IMAGE tab with a seeded inventory of forty assets
and asserts, in pixels, that the page body does not scroll, that the list is the
element that does, and that a tile card's height is the same for a card with a
one-line location and a card with `+4 more`. That last one is the fixed-height
card rule, and it is the failure the harness exists to catch.

### Done when

- Both suites are green, and `go run ./scripts/uitest/renderharness` passes.
- On a real .pptx: step 3 shows the IMAGE tab with the deck's pictures, the
  filter and both views work, boxing a picture with the text "Client logo",
  blurring another and removing a third survives an export, and the exported
  deck opens in PowerPoint with exactly those three changed and nothing else.
- Switching to TEXT shows the step 3 screen exactly as it was before this batch.

### What B3 left behind that differs from the above

1. **`state.js` mirrors FOUR lists, not three.** The Tests section asks the new
   guard to hold the occurrence KINDS as well, so `IMAGE_KINDS`
   (`picture`, `fill`, `background`) sits beside the three the state.js snippet
   named. It earns its place rather than existing for the guard: `kindNote` walks
   it so the Location tooltip lists a background or a shape fill in a stable
   order, and `IMAGES.kindLabel` gives each one a word.
2. **`ui.js card()` gained `bodyId`.** The picture list is the screen's scroll
   owner and `scroll.js` keys a preserved offset on a selector that survives the
   repaint, so the body needs an id of its own. That is one option on the ONE
   card builder, not a second builder.
3. **The disabled-treatment reasons are frontend CODES.**
   `state.js treatmentBlockedReason` answers `"linked"`, `"format"` or
   `"svg_blur"` and `copy.js IMAGES.blocked` owns the sentences, so the panel's
   tooltips live where copy is reviewed. `image_parity_test.go` holds those three
   codes to the copy table alongside Go's own reason and warning codes.
4. **The document selector renders even when `Applicable` is false.** It is in
   the banner as specified, but it is NOT gated on applicability: a selector that
   vanished on a .txt document would strand the reader on the one file with
   nothing to review. The filter and the view toggle are gated, because a filter
   over an answer with no list can do nothing.
5. **`resetImageDecisions` still has no control.** B3's screen description has no
   bulk "keep them all", so the `api.js` wrapper B2 added is written, documented
   and unreached by any view. Either give it a home or say why it has none.
6. **The details grid has EIGHT tracks.** Seven carry the documented headings; the
   eighth is the `Keep` / `Anonymise` pair and its header cell is deliberately
   empty, because the two buttons say what they do.
7. **The step 3 reset keeps the picture decisions.** `STEP_RESETS.anonymise`
   clears the open treatment panel and nothing else of this feature: the
   decisions are about the bytes captured at IMPORT, they need no run, and Go
   holds them per imported document, so discarding them because the user stepped
   back to re-read a value would throw away work the run never touched. A fresh
   import and a cleared batch DO drop the inventories, the previews and the panel.
8. **Previews are lazy by measured visibility, capped at twelve per paint.** A
   row asks for its thumbnail when it is within 200px of the list's visible box,
   and a refused render caches the empty string ("No preview") so a page with no
   bridge does not retry the same picture on every repaint.
9. **The treatment panel is its OWN overlay**, `.image-panel-layer`, not
   `.modal-layer`. It borrows the confirm's mechanism (backdrop, Escape, a click
   outside) and wires it itself, because `modal.js` is shell-level and answers a
   question, while this is a screen-level editing surface holding a draft.
   `wireModal` must keep looking only for `.modal-layer`.

---

## B4 — Telling the truth about it: report, export, guards, documentation

**What B3 hands you:** the working feature. A user can review a document's
images and their decisions reach the exported .docx / .pptx.

**Goal:** the rest of the application knows the feature exists. Nothing here is
polish for its own sake: each item closes a place where the application would
otherwise report a document as anonymised without mentioning what happened to
its pictures.

### 1. The run report says what happened to the images

The report is produced by `engine.Run`, which knows nothing about images and must
keep knowing nothing (invariant 4: the engine is UI-agnostic, and image
decisions are an export-time concern, not a pipeline pass). So the image section
is added by the APP when it writes the report, from `a.imageDecisions` and the
cached inventories, not by the engine.

`App.ExportReport` gains, per document, a section listing every asset with a
non-keep decision: its name, its locations, the treatment and (for box) the text
used. Kept images are reported as a COUNT only, because a list of everything left
alone is noise; the count is there so a reader can tell "no images" from "images,
all kept".

This is not a re-identification key (it names no original value), so it needs no
new warning; the report already carries one for the per-value table.

### 2. The export screen says it before the user saves

`SaveSameFormat` is the path with the metadata review panel. It gains one line
above the confirm: `"3 images will be changed in this copy (1 boxed, 1 blurred, 1 removed)."`
and, when the document has images and NONE is anonymised,
`"This copy keeps all 7 of the document's images."` The second sentence is the
important one: a user who never opened the IMAGE tab is told, at the moment it
matters, that the pictures are going out as they came in.

For a `.md` / `.txt` export of a docx or pptx nothing changes and nothing is
said: those formats never carried the images in the first place.

### 3. The leak audit, as a permanent test

B2 left `TestExportedArchiveKeepsNoOriginalBytes` in
`backend/engine/exportfmt/images_integration_test.go` already table-driven over
both formats with one decision of each kind, checking every entry of the produced
archive (not only the media part) against the original's bytes AND its SHA-256,
and covering an asset's SVG companion. Its file header says it is load-bearing and
must not be narrowed. The remove-beside-a-keep case is covered at the unit tier
(`roundtrip/images_remove_element_and_bytes`, two pictures in one slide).

So B4 EXTENDS rather than rebuilds: add the SVG pair as a decided asset in the
integration table (B2 boxes and blurs raster assets only), and add the
remove-beside-a-keep case to the integration table too, so the splice order is
held over a real archive as well as a built one.

Add the companion assertion that the produced archive still opens: every part
parses as XML, `[Content_Types].xml` still names every part's extension, and the
relationship ids referenced by the surviving pictures all resolve.

### 4. Charters and user documentation

| File | Edit |
|---|---|
| `CLAUDE.md` §3 | `backend/engine/imaging/` and `backend/app_images.go` in the tree; `frontend/views/anonymiseimages.js` in the frontend list; `image_parity_test.go` in the guards list |
| `CLAUDE.md` §5 | a new "Image anonymisation" block: the applicability matrix, the asset/occurrence/treatment/status vocabulary, the one-decision-per-asset rule, the three invariants (the pixels always leave the archive, blur destroys rather than hides, a control that does not anonymise is not called anonymise), and why PDF is out |
| `CLAUDE.md` §5 | DONE in B2: the session paragraph already says `SessionVersion` is **9**. Check it, do not redo it |
| `CLAUDE.md` §7 | the vendored 8x8 font row, in the assets style of the Material Symbols row |
| `backend/CLAUDE.md` | `engine/imaging` in the module map; the export's two-pass rule (text first, then images, on the same part). Its session paragraph already records version 9 (B2) |
| `frontend/CLAUDE.md` | the step 3 tabs in the file map and the discipline list; the SVG-in-`<img>` rule; `ui.js card()`'s `bodyId`; and that the treatment panel is its own overlay rather than `modal.js`'s confirm (see "What B3 left behind") |
| `frontend/BRIDGE.md` | DONE in B1 and B2: the five wrappers and the `Inventory` / `Asset` / `Decision` wire shapes are written. B4 only adds whatever B3's screen turned out to need |
| `README.md` | a short "Images" section: what the three treatments do, that blur is not a guarantee, that PDF images are always removed, that .xlsx images are not reviewed |
| `frontend/docs/index.html` | the same, in the bundled offline user docs, in the voice that file already uses |
| `docs/UITESTING.md` | the new probe and what the harness case measures |
| `docs/TESTING.md` | nothing, unless you added a category. Do not add one. |

### 5. What must NOT be done in B4

- Do not make image decisions gate the step 3 to step 4 move (decision 6).
- Do not change the markdown export's `*[image omitted]*` placeholder to carry
  the box text. It is a plausible idea and it is a different change: the
  markdown export has no images to anonymise, so the placeholder is a
  description of the source, not a redaction.
- Do not add image review to .xlsx. The owner ruled on it.

### Tests (B4)

- `backend/app_export_test.go`: the report section, over a document with a mix of
  treatments and one with none.
- `roundtrip/exported_archive_*`: the promoted table above, integration tier.
- `frontend/export.test.js`: the two same-format sentences, including the
  all-kept one.
- `copy_guard_test.go` and `frontend/copy.test.js`: green with the new strings.
- Full sweep once, at the end: `task test:all`.

### Done when

- `task test:all` is green.
- Exporting a deck whose images were reviewed produces a report naming them and
  a confirm sentence counting them.
- Every file in the table above describes the code that now exists.

### What B4 left behind that differs from the above

1. **The picture summary travels with the PROPERTIES review, not beside it.**
   `SameFormatMeta` gained `Images *imaging.Summary`, filled by
   `GetSameFormatMetadata`, because that is the call the review panel already
   makes and the panel is the last surface before the file is written. It is
   ABSENT rather than zeroed for a format with no image review, which is what
   makes the panel render no line at all: a line reading "0 images" on a PDF
   would contradict the IMAGE tab, which says a PDF export has already dropped
   every picture. The counting itself goes through `ImagePlan.Summary`, so the
   sentence and the pictures the export actually changes cannot disagree.
2. **The report export split into `reportBytes`**, for the reason
   `validateCopyText` is split out of `CopyText`: the save goes through the Wails
   dialog, which refuses a context no lifecycle hook gave it, so the part worth
   testing has to be reachable headless. The JSON shape EMBEDS `engine.Report`
   and adds one top-level `images` key, so every field a reader of an earlier
   report knows stays where it was.
3. **The picture section's builders live in `backend/app_images.go`**, not in a
   new file: they read the decision store and the cached inventories that file
   already owns, and the engine must keep knowing nothing about pictures. The
   file's header says so.
4. **The integration table grew a `wantSummary` and a `staysPart` / `staysNames`
   pair** rather than keeping the hard-coded "one of each treatment" assertion.
   The pptx case now decides FOUR assets (the SVG pair is boxed, which is what
   put the companion under the leak guard: disabling companion rewriting makes
   `TestExportedArchiveKeepsNoOriginalBytes` fail, and before this it did not).
   The size check moved from `image.DecodeConfig` to `imaging.Measure`, because
   an SVG companion is now one of the parts under test and only `Measure` can
   read a vector's size. The remove-beside-a-keep case is the DOCX one, where
   the legacy VML picture is deleted out of the middle of `word/document.xml`
   and the picture after it is one nobody decided anything about; the pptx's
   `staysNames` holds the weaker but still real rule that a box never deletes an
   element.
5. **The two archive-integrity companions are `contentTypesCoverEveryPart` and
   `pictureRelationshipsResolve`.** The second re-scans the PRODUCED archive with
   the same scanner the bound layer uses and asserts the absence of the
   `unreadable_part` warning, rather than walking the XML a second time: a second
   resolver could disagree with the first and then pass an archive the
   application cannot read.
6. **`frontend/BRIDGE.md` was NOT already correct.** It still said the session
   file was schema version 8 and listed its shape without `imageDecisions`, so
   B4 fixed both. `docs/TESTING.md` gained `image_parity_test.go` in its
   load-bearing-guards list, which is a name and not a category.
7. **`README.md`'s same-format paragraph was stale in the other direction**: it
   promised that "the layout, styles and images are preserved", which after B2
   is only true until the user decides otherwise. It now points at the Images
   section instead of promising preservation.
8. **`resetImageDecisions` still has no view**, and B3's question is answered in
   `BRIDGE.md` rather than by inventing a control: with one decision per picture
   and every picture starting on `keep`, "undo all of them" recovers from no
   failure, and a button that clears work the user did would need a confirm of
   its own. It sits in the same class as `documentationURL` and `validateValues`,
   two other wrappers `deadexports` reports as kept alive by their own test.

---

## Decisions taken

1. **PDF is out of the feature**, with one explanatory line in its place.
   Investigation in Appendix C. Owner-approved.
2. **No new Go module.** The box text is drawn from a vendored 8x8 bitmap font
   table, which is an asset like the Material Symbols SVGs, not a dependency.
   Owner-approved. The visible cost: the raster box's text is blocky and its
   accents are folded to ASCII, and the copy says so.
3. **A decision belongs to an image ASSET, not to an occurrence.** A logo used on
   five slides is one question. Owner-approved. What it buys: the exporter never
   clones a picture part and never rewrites a relationship file, which is the
   riskiest code the feature could have contained. What it costs: "blur it on
   slide 3 and keep it on slide 7" is not expressible. Nothing in the model
   forbids adding it later; it would be a new batch, not a rework.
4. **No blur for SVG.** A blur filter on a vector leaves every original shape and
   string in the file, so it would be a control labelled "anonymise" that does
   not anonymise. Owner-approved.
5. **The tabs sit above the whole step 3 workspace**, and the IMAGE tab is full
   width. Taken while planning, not put to the owner: the seven-column view needs
   the width, and the alternative (tabs inside the Compare card) makes the
   details view a horizontal scroller on a laptop screen. Flip it in review if
   the owner prefers the left column to stay visible; the change is local to
   `anonymise.js`.
6. **Image decisions do not gate the move to step 4.** Every asset starts at
   `keep`, so there is no unreviewed state for a gate to protect, which is the
   opposite of the Identify-to-Anonymise gate, where a suggestion is genuinely
   unanswered until the user answers it. The three-way filter (All / Kept /
   Anonymised) is the visible form of the same fact: there is no fourth,
   undecided bucket.
7. **`SessionVersion` goes to 9.** The constant's own rule says an added field
   the loader can ignore is not a bump, and this is the exception the rule
   describes rather than a contradiction of it: what an older reader would
   ignore is a redaction, so it would load the session, export the file, and
   ship the logo. Recorded in full beside the constant.
8. **The export runs text first, then images, as two passes over the same part.**
   Merging them would need a rule for a text replacement that falls inside a
   picture element being deleted (a Word text box lives inside `w:drawing`).
   Sequential passes have no such case, at the cost of one extra walk per part,
   which is microseconds.
9. **Occurrence identity is `part + ordinal`, never a byte offset.** The text
   pass moves every offset in the part before the image pass runs, so an
   offset-based identity would be stale exactly when it is used.
10. **Blur is mosaic-then-smooth, and its strength is relative to the image's own
    size.** A Gaussian is partially invertible and a fixed pixel radius is
    meaningless across a 60px icon and a 4000px screenshot.

---

## Conflict analysis

### Files touched by more than one batch

| File | B1 | B2 | B3 | B4 | Note |
|---|---|---|---|---|---|
| `backend/app_images.go` | creates | extends | - | - | B2 appends methods; no B1 signature changes except `Asset.Decision` |
| `backend/engine/imaging/imaging.go` | creates | extends | - | - | B2 adds `Decision` to `Asset`; keep the field last so the diff is additive |
| `frontend/api.js` | 2 wrappers | 3 wrappers | reads | - | B2 adds its wrappers when it adds its methods, so B3 finds them ready |
| `frontend/BRIDGE.md` | section | rows | - | completes | one section, appended to three times |
| `CLAUDE.md` | - | §5 session line | - | the rest | B2 must edit §5's `SessionVersion` sentence when it bumps the constant, or the charter lies for two batches |
| `backend/testdata/images.{docx,pptx}` | creates | reads | seeds the harness | reads | committed once, never regenerated casually: a regenerated fixture invalidates every golden assertion above it |
| `frontend/views/anonymise.js` | - | - | tab bar only | - | the single riskiest edit in the order, see below |

### Hotspots

- **`frontend/views/anonymise.js` is 2031 lines and is the TEXT tab.** B3's edit
  to it must be a wrapper, not a refactor: add the tab bar, move the existing
  body into a `textTab(s, doc)` branch, change nothing inside it. Every exported
  function it already has (`runCard`, `compareCard`, `selectedCard`, ...) is
  imported by `frontend/anonymise.test.js`, which is 1382 lines; if a signature
  moves, that suite is what tells you, and the right answer is to move it back.
- **`exportfmt.Config` gains a field.** Every construction site must set it or
  get the zero value, and the zero `ImagePlan` must mean "change nothing". Assert
  that with the byte-identical-export test rather than by reading the call sites.
- **`rewriteZip`'s selector is a `switch` on part name in each format's file.**
  B2 adds a media-part branch to both. The bit-for-bit passthrough for every
  OTHER entry is the property that makes the same-format export safe at all; do
  not restructure the function, add a branch to it.
- **The parity guards fail loudly and early.** If `image_parity_test.go` is red,
  the two sides of a constant disagree, and the fix is never to loosen the test.
- **`Document.Raw` must still be there.** The image scan and every treatment read
  it. Nothing in this order may add a "drop Raw after import to save memory"
  optimisation; if a future one is proposed, this feature is the reason it cannot
  happen.

---

## Recommended order

Strictly B1, B2, B3, B4, one session each, each on its own branch off `main`,
each merged before the next starts.

The dependency is real rather than administrative: B2 needs B1's model and its
walker, B3 needs B2's methods to exist or it would render controls that reject
every click, B4 needs B3's screen to document. The one thing that CAN be lifted
out and run in parallel, if the owner wants a fifth session, is B4's
documentation half (the charter and README edits), which depends only on this
document. Do not parallelise anything else: two sessions editing `state.js` and
`anonymise.js` at once will cost more in conflict resolution than they save.

If a batch runs out of room, the natural split points are:

- **B1**: the scanner (docx + pptx) and the thumbnailer are separable. Ship the
  scanner with `ImageThumbnail` returning a not-yet error, and finish the
  thumbnailer in a follow-up commit on the same branch.
- **B2**: box and remove first, blur second. Blur is the only treatment with real
  arithmetic in it and it is independently testable.
- **B3**: the details view first, tiles second. The tiles view renders the same
  data through a different template and can land in a second commit.
- **B4**: never split; it is the batch that stops the feature from being silently
  half-documented.

---

## Acceptance criteria

Written so the owner can check them on their own laptop with their own
documents, in the order they would naturally try them.

### Correctness

1. Importing a .pptx with pictures and opening step 3 shows the IMAGE tab with
   one row per distinct picture, and the count in the tab badge matches the
   number of rows under the "All" filter.
2. A picture used on several slides appears ONCE, and its Location cell says how
   many places it is in.
3. A picture on the slide master is listed with the location "Slide master".
4. A picture on a hidden slide is listed, and its location says the slide is
   hidden.
5. An SVG picture is listed as SVG, and its Blur control is disabled with a
   tooltip that explains why rather than just greying out.
6. Switching to TEXT shows step 3 exactly as it was before this order.

### The three treatments

7. `Replace with a box` with the text `Client logo` produces, in the exported
   file, a rectangle of the same size and in the same place as the original,
   carrying that text.
8. `Blur` at strength 8 on a screenshot of a client system produces an image in
   which no text is legible, at the same size and place.
9. `Remove` produces a file in which the picture is simply not there, and the
   surrounding text and layout are otherwise unchanged.
10. Opening the exported .docx in Word and the exported .pptx in PowerPoint
    produces NO repair prompt, on a file with all three treatments applied.

### The leak

11. Unzipping the exported file and searching every entry for the original
    picture's bytes finds nothing, for all three treatments. This is
    `roundtrip/exported_archive_keeps_no_original_bytes` and it is the one
    criterion that must never be waived.
12. Exporting a document on which no image decision was taken produces a file
    byte-identical to what `main` produces today.

### Honesty

13. Opening the IMAGE tab on a PDF shows the one-line explanation and no
    controls.
14. Opening it on a .csv, .txt, .md or .xlsx shows the not-supported line and no
    controls.
15. The same-format export confirm says how many images will be changed, and says
    it even when the answer is none.
16. The exported report names every anonymised image and counts the kept ones.

### Discipline

17. `task test` , `task test:integration` and `go run ./scripts/uitest/renderharness`
    are green, and `task test:all` is green at the end of B4.
18. `task audit` reports no new finding attributable to this order (see
    `docs/audit.md` for dismissing a false positive properly).
19. No em dash reaches any user-visible string, on either side of the bridge.
20. `go.mod` and `go.sum` are unchanged by the whole order.

---

## Appendix A — The OOXML reference the batches assume

Everything here was read out of the format, not remembered. Verify against the
fixtures you generate rather than trusting the snippets.

### A picture in a .pptx

```xml
<p:pic>
  <p:nvPicPr>
    <p:cNvPr id="4" name="Acme group logo" descr="the client logo"/>
    <p:cNvPicPr/><p:nvPr/>
  </p:nvPicPr>
  <p:blipFill>
    <a:blip r:embed="rId2"/>
    <a:srcRect l="10000"/>            <!-- optional crop, stripped for box/blur -->
    <a:stretch><a:fillRect/></a:stretch>
  </p:blipFill>
  <p:spPr>
    <a:xfrm><a:off x="838200" y="365125"/><a:ext cx="4572000" cy="3429000"/></a:xfrm>
    <a:prstGeom prst="rect"><a:avLst/></a:prstGeom>
  </p:spPr>
</p:pic>
```

- `p:cNvPr/@name` and `@descr` are where `Asset.Name` comes from.
- `p:cNvPr/@hidden="1"` marks a hidden shape.
- `a:ext/@cx,@cy` is the display frame in EMU.
- The slide root `<p:sld show="0">` marks a hidden slide.
- Relationships: `ppt/slides/_rels/slide4.xml.rels`, targets relative to
  `ppt/slides/` (`../media/image2.png`).

### A picture in a .docx

```xml
<w:p><w:r><w:drawing>
  <wp:inline>                          <!-- or wp:anchor for a floating picture -->
    <wp:extent cx="2857500" cy="1905000"/>
    <wp:docPr id="1" name="Figure 1" descr="the client's org chart"/>
    <a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">
      <pic:pic>
        <pic:nvPicPr><pic:cNvPr id="0" name="orgchart.png"/></pic:nvPicPr>
        <pic:blipFill><a:blip r:embed="rId4"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>
        <pic:spPr><a:xfrm><a:ext cx="2857500" cy="1905000"/></a:xfrm></pic:spPr>
      </pic:pic>
    </a:graphicData></a:graphic>
  </wp:inline>
</w:drawing></w:r></w:p>
```

Legacy, still produced by paste and by old documents:

```xml
<w:r><w:pict><v:shape style="width:200pt;height:150pt">
  <v:imagedata r:id="rId5"/>
</v:shape></w:pict></w:r>
```

Relationships: `word/_rels/document.xml.rels`, targets relative to `word/`
(`media/image4.png`). Headers and footers have their own rels files
(`word/_rels/header2.xml.rels`), which is why the resolver must always be
relative to the PART, never hardcoded.

### An SVG picture, in either format

```xml
<a:blip r:embed="rId3">
  <a:extLst>
    <a:ext uri="{96DAC541-7B7A-43D3-8B79-37D633B846F1}">
      <asvg:svgBlip xmlns:asvg="http://schemas.microsoft.com/office/drawing/2016/SVG/main"
                    r:embed="rId4"/>
    </a:ext>
  </a:extLst>
</a:blip>
```

`rId3` is a PNG fallback Office renders when it cannot draw SVG; `rId4` is the
SVG itself. **Both parts carry the picture**, so both must be treated, or a
"blurred" logo comes back sharp on the machine that renders the fallback.

### Units

`914400` EMU per inch, `360000` per centimetre. At 96 dpi, pixels =
`EMU * 96 / 914400`. Used only as a fallback size when a part cannot be decoded;
prefer the real pixel size whenever there is one.

### The blur maths

```
f      = clamp(round(min(w,h) * strength / 200), 2, min(w,h))
mosaic = for each f x f block: every pixel becomes the block's mean
result = one 3x3 box blur pass over mosaic
```

| image | strength 1 | strength 5 | strength 10 |
|---|---:|---:|---:|
| 200 x 200 icon | 2 px blocks | 5 px | 10 px |
| 1200 x 800 photo | 4 px | 20 px | 40 px |
| 3840 x 2160 screenshot | 11 px | 54 px | 108 px |

The property the test asserts is information loss, not appearance: before the
smoothing pass, the mosaic has at most `ceil(w/f) * ceil(h/f)` distinct pixel
values, whatever the source contained.

---

## Appendix B — Considered and rejected, and why it is recorded

Recorded so a later session does not spend an afternoon rediscovering them.

**Replace the picture with a native shape instead of new pixels.** In pptx,
swapping `<p:pic>` for a `<p:sp>` rectangle with a `<p:txBody>` would give real,
crisp, accented text with no font code at all, and it is genuinely easy. It was
rejected because the docx equivalent is not: a Word drawing carrying a shape
needs `mc:AlternateContent` with a `wps:wsp` fallback, and a malformed one makes
Word declare the file corrupt. One mechanism that works identically in both
formats is worth more than a nicer result in one of them, especially with a
non-expert owner maintaining it. If the box text's appearance turns out to
matter, the pptx-only shape route is the first thing to revisit.

**Blur in the WebView with a CSS filter, and only render in Go on export.**
Instant preview, no round trip. Rejected because the preview would then be a
different algorithm from the output: CSS `blur()` is a true Gaussian, the engine
does mosaic-then-smooth, and the whole point of the slider is that the user
judges the result before committing. A 200 ms debounced round trip on a
thumbnail is cheap.

**Cache every thumbnail in Go.** Rejected: thumbnails are the largest thing this
feature holds, and a 200-image deck would sit in memory for a screen the user
visits once. The frontend caches what it has actually shown, which is bounded by
what fits on screen plus scrollback.

**Scan every document's images at import.** Rejected: it costs a full zip walk
per file on a screen that is not about images, and the user may never open the
tab. The scan is lazy and cached per document.

**Let `remove` delete the media part and its relationship entry.** Rejected: a
dangling relationship id is a repair prompt in Word, and the parts are referenced
from `[Content_Types].xml` too. Overwriting the bytes with a 1x1 achieves the
only thing that matters (the pixels are gone) with no structural edit.

**Offer image review for .xlsx.** Ruled out by the owner. The machinery would
mostly work (excelize aside, the media parts sit in `xl/media/`), but a
spreadsheet's pictures are rarely the client's confidential material and the
sheet-per-document model would need an owner for images that belong to the
workbook rather than to a sheet.

---

## Appendix C — The PDF investigation, in full

The owner asked for this to be settled before any PDF work was planned. It is
settled: **PDF is out**, and here is everything that decided it.

### C1. The exported PDF has no images in it today, and never had

`backend/engine/exportfmt/pdf.go` does not rewrite the user's PDF. Its own header
says so: in-place body-text replacement inside content streams was evaluated and
NOT adopted (subset fonts frequently lack the glyphs a placeholder needs), and
the implemented fallback is a **regenerated** PDF, built from the anonymised
working text with `go-pdf/fpdf`, one page per source page, plain paragraphs, plus
the reviewed Info dictionary.

`ExportPDF(anonymised string, reviewed []MetaField, cfg Config)` takes a STRING.
There is no path by which an original page object, and therefore no path by which
an original image, reaches the output file. Every image in a source PDF is
already absent from the exported one.

So the strongest treatment this feature offers, `remove`, is already what PDF
does to every image, unconditionally. There is nothing for a review screen to
decide.

### C2. Reading the images back out is only partly possible

Even for a read-only listing with previews, the pinned reader does not cooperate:

- `github.com/ledongthuc/pdf` exposes stream contents only through
  `Value.Reader()`, which runs the stream's filters. Its `applyFilter` **panics**
  on any filter it does not implement, and it implements `FlateDecode`,
  `ASCII85Decode` and a short list beside them. `DCTDecode` (JPEG) and
  `JPXDecode` (JPEG 2000) are not among them, and DCTDecode is what a photograph
  in a PDF almost always is.
- The raw stream offset lives in the package's unexported `stream` struct, so
  the raw, still-encoded bytes cannot be reached from outside the module either.
  There is no "give me the compressed bytes" accessor.

The consequence: FlateDecode images (screenshots, exported charts) could be
extracted and previewed; JPEG images could not, without forking the module or
adding a second PDF parser. A review screen that shows some of a document's
pictures and silently omits the photographs is worse than one that shows none,
because the omission looks like an answer.

### C3. Changing the original PDF needs a writer this repository has rejected

Replacing an image XObject in place is, in principle, more tractable than
replacing text: an image is a self-contained stream with `/Width`, `/Height`,
`/ColorSpace`, `/BitsPerComponent` and `/Filter` in its dictionary, and no font
subsetting to worry about. In practice it needs the ability to write a PDF back:
rebuilding the cross-reference table, handling cross-reference STREAMS and object
streams (any PDF 1.5 or later), and preserving encryption where present.
`ledongthuc/pdf` is read-only. `pdfcpu` is recorded in `CLAUDE.md` §7 as **NOT
ADDED**, on a functional decision taken at BUILD-02 Phase 13, and the owner has
now separately declined new dependencies for this feature.

Hand-rolling an xref rewriter is a project, not a batch, and it would be a
project whose output is a file format the application already declares
EXPERIMENTAL.

### C4. What is shipped instead

One sentence in the IMAGE tab, mapped from the reason code
`pdf_images_removed`:

> PDF export rebuilds the document as text, so every image in a PDF is already
> removed from the exported file. There is nothing to review here.

It is true, it is checkable by the user in ten seconds, and it costs nothing to
maintain.

### C5. What would change the decision

If PDF image review is ever wanted for real, the honest route is not to add a
reader: it is to notice that the PDF export is already a REGENERATION, and to put
the images back into the regenerated file deliberately. `fpdf` can embed PNG and
JPEG from a reader, so a future change could extract what it can, apply the same
three treatments, and place the results into the new PDF in reading order. That
would be a new feature ("keep the pictures in the regenerated PDF"), not an
extension of this one, and it inherits C2's extraction gap. Record it here rather
than half-building it now.

---

## Appendix D — Paste-ready opening prompts

One per batch. Each is written to be pasted into a FRESH session with no
conversation history. Replace `<suffix>` with anything short.

**B1**

> Read `CLAUDE.md`, `backend/CLAUDE.md` and `docs/TESTING.md`, then read
> `docs/CHANGE-10.md` sections 0 and B1 and implement B1 exactly as written.
> Develop and push on branch `claude/change-10-b1-<suffix>`, branched from
> `main`. Do not start B2. If you must deviate from the brief, edit the later
> batch sections of `docs/CHANGE-10.md` so they describe what you actually left
> behind, and say so in the pull request.

**B2**

> Read `CLAUDE.md`, `backend/CLAUDE.md` and `docs/TESTING.md`, then read
> `docs/CHANGE-10.md` sections 0 and B2 and implement B2 exactly as written. B1
> is merged; read `backend/engine/imaging/` and `backend/app_images.go` before
> planning. Develop and push on branch `claude/change-10-b2-<suffix>`, branched
> from `main`. Do not start B3. If you must deviate, update B3 and B4 in
> `docs/CHANGE-10.md` and say so in the pull request.

**B3**

> Read `CLAUDE.md`, `frontend/CLAUDE.md`, `frontend/BRIDGE.md` and
> `docs/TESTING.md`, then read `docs/CHANGE-10.md` sections 0 and B3 and
> implement B3 exactly as written. B1 and B2 are merged; the five bound methods
> exist and work. The edit to `frontend/views/anonymise.js` must be a wrapper
> around the existing screen, not a refactor of it. Develop and push on branch
> `claude/change-10-b3-<suffix>`, branched from `main`. Do not start B4. If you
> must deviate, update B4 in `docs/CHANGE-10.md` and say so in the pull request.

**B4**

> Read `CLAUDE.md`, both subtree charters and `docs/TESTING.md`, then read
> `docs/CHANGE-10.md` sections 0 and B4 and implement B4 exactly as written.
> B1 to B3 are merged and the feature works end to end; this batch makes the
> rest of the application tell the truth about it. Finish with a full
> `task test:all` and `task audit`. Develop and push on branch
> `claude/change-10-b4-<suffix>`, branched from `main`.

### Before B1 starts, the owner should have to hand

- A real .pptx and a real .docx with pictures in them, including at least one
  logo repeated across slides and, if possible, one SVG. They stay outside the
  repository (client material is never committed); the committed fixtures are
  generated from code.
- PowerPoint and Word, to open the exported files. Acceptance criterion 10 (no
  repair prompt) cannot be checked any other way.
