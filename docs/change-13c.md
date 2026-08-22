# CHANGE-13c — PDF text in place

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0, pure Go + Wails v2, no CGo, no npm). It is **ONE
batch**, and it is the largest of the change-13 family: it is the batch that
makes the feature real.

It is the second batch of the change planned in `docs/change-13.md`, and that
plan is its context: the decisions (D1 to D16), the ten answered questions, the
adoption-gate criteria (G1 to G9), the answered owner questions (OQ1 to OQ5)
and the findings log (F1 to F22) all live there and are referenced here by
number rather than restated. **Read the main plan first, and read §11's
GO/NO-GO note including its 2026-08-22 reference-document section.**

Where 13b proved the library can do the job on fixtures, the owner's deep run
proved what it costs on real documents. Two of that run's findings are the
reason this order exists in the shape it does, and neither was in the original
13c scope:

- **F19.** On a real 15-slide deck, 10 of 48 occurrences could not be located
  at all. The cause is not the library's quality, it is that both extraction
  and search work on text FRAGMENTS while the pipeline works on strings.
- **F20.** A value the pipeline holds can be one that no extraction contains
  verbatim, because the reassembly and the spacing repair rewrite the text
  detection runs on.

So this batch's first job is not the exporter. It is to make extraction and
location speak the same language: fragments with rectangles.

## Starting conditions

- **Met:** 13b's gate is resolved (`docs/change-13.md` §11). G1, G2, G3, G9,
  G5 and G8 pass; G4 passes on both reference documents; G7 fails and this
  order is the answer to it. The owner accepted the fragment-aware enlargement
  on 2026-08-22.
- **Resolved after the batch:** F22. `main` did not build on Windows from a
  clean clone, because `.gitignore`'s `*.exe` rule stopped git tracking a file
  `go mod vendor` wrote into the Wails tree. The owner chose the negation
  (`!vendor/**/*.exe`) over dropping `vendor/` from git, so the 1.8 MB embedded
  installer is committed, `.gitattributes` marks `*.exe binary`, and a
  `GOOS=windows` cross-compile step on the Linux runner now fails the build for
  the whole class rather than leaving it to the next tag.
- **Binding for the last step:** OQ3's decommissioning gate. The old PDF path
  is removed only after the owner explicitly confirms the tests are successful,
  against their tag and release as the rollback point. Until that confirmation
  the new path ships BESIDE the old code.

## Ground rules

The Ground rules block of `docs/change-13.md` applies in full. The ones this
batch can actually violate, restated:

- **No CGo, no `purego`, no native artefact.**
- **The local-only guarantee.** Only `backend/ollama/client.go` may construct a
  network request. `pdf_boundary_test.go` already forbids the library's
  copilot symbols, its file-path APIs under `backend/`, and its timestamp
  lever; this batch must keep that guard GREEN while adding the first
  production caller the library has ever had.
- **The engine stays UI-agnostic and originals are immutable.** The library is
  reached through `OpenStream` on bytes the App already holds. Nothing under
  `backend/engine/` learns a filesystem path.
- **One decision per occurrence is not a thing.** Unlike pictures, text
  replacement is decided by the pipeline; this batch adds no new user decision
  to the Anonymise step beyond what the refusal (step 6) must say.
- **A change is not finished until its tests move with it**; both suites gate:
  `go test ./...` and `node --test "frontend/**/*.test.js"`.
- **Comments explain intent in the present tense.** No batch numbers, no "this
  used to", no tombstones.
- **Confidentiality of the reference documents.** Every measurement on the
  owner's 2-page PDF and 15-slide deck is reported as COUNTS. No name, company
  or acronym from them may reach a fixture, a comment, a commit message, a
  document in `docs/` or a memory.

### The deviation rule

If a step below is wrong, contradicted by the code, or cannot be done as
written: **stop, say so, and propose the alternative before writing it.** Two
steps below (1 and 2) carry MEASURED thresholds rather than decreed ones; if a
measurement contradicts the threshold, the measurement wins and the finding is
recorded.

---

## 1. What this batch produces

1. A fragment-aware PDF extraction that feeds the pipeline text no worse than
   today's and no longer manufactures values out of fragments that merely share
   a baseline.
2. An in-place PDF export: the produced file is the original's bytes with the
   pipeline's replacements applied, not a regenerated layout.
3. A location ladder with a fragment rung, and a refusal that fires only on
   occurrences that are genuinely unlocatable.
4. The whole-file leak scan running as a BLOCKING self-check on every produced
   PDF.
5. The non-content surfaces scrubbed or dropped per Q1's table.
6. The copy and the EXPERIMENTAL labels updated per D11.
7. After the owner's confirmation only: the dependency removals earned under
   D12.

---

## 2. Execution order

### Step 1 — The extraction line model (the fragment rule)

This is the step F19 exists for, and it comes first because everything else
reads its output.

Extraction moves from `ledongthuc/pdf` to the library, and the unit it works in
becomes the FRAGMENT. `Page.ExtractTextWithLayout()` returns lines of fragments
carrying rectangles; `Page.ExtractText()` returns strings and must not be the
production path, because a string cannot tell the ladder where anything is.

Build a new type in `backend/engine/convert/` that carries, per page, the lines
and, per line, its fragments with their rectangles. The markdown the pipeline
consumes is derived FROM it, so the two can never disagree about what a line
is.

Two rules shape a line, and each has measured evidence behind it:

**1a. SPLIT where fragments are not neighbours.** Reading-order grouping puts
fragments that merely share a baseline on one line. On the reference deck that
produced 10 strings the detector read as person names and that were never
contiguous text (F19). Split a line between two consecutive fragments when the
horizontal gap between them exceeds a plausible word space.

The threshold is expressed as a multiple of the fragment's own space width (or
its font size where a space width is unavailable), NEVER as an absolute point
value: a rule in points cannot serve a 12 pt contract and a 28 pt slide title
at once. The measured gaps to separate are 6.7, 6.7, 8.0, 8.2 and 8.6 pt (real
word spacing, must NOT split) against 42.9, 383.2, 384.8, 948.4 and 948.4 pt
(different shapes, MUST split). Calibrate on those, then verify on the
fixtures.

**1b. JOIN a wrapped continuation, carefully.** The library preserves the PDF's
visual line breaks and, on at least one fixture, emits a blank line between
them. A value wrapped across a break is then two half-values, which is how the
synthetic contract fixture loses a two-token organisation.

A naive join is NOT acceptable: joining on punctuation alone glued two headings
together and invented three spurious person names on a one-page fixture. Join
only when the geometry agrees: consecutive baselines within about 1.5 line
heights, a shared left margin, the previous line reaching near the block's right
edge, and no terminal punctuation ending it. Drop the interleaved blank line
first, or the join never fires.

**1c. `RepairPDFText` stays** and runs over the new extractor's output,
unchanged (F11). It is idempotent and harmless there, and D8 is closed.

**Measurement, recorded in the findings log as counts:** detection totals per
category over the incumbent's extraction and the new one, for both reference
documents and for `framework_contract.pdf`, before and after 1a and 1b
separately. 1a must not lose a value; 1b must not flood the review list. Where
1b's cost exceeds its gain on the reference documents, ship 1a alone and record
why.

### Step 2 — The location ladder, fragment-aware

D5's ladder gains a rung, and the order matters because each rung is more
expensive and less precise than the one above it.

1. **Literal.** `SearchText` for the value.
2. **Tolerant.** `SearchText` with the whitespace-tolerant pattern (the
   converter's repairs may have collapsed a seam).
3. **Fragment walk (NEW).** Walk the page's fragments and match the value
   across CONSECUTIVE fragments of one line; the occurrence is the union of
   their rectangles. This rung exists because `SearchText` matches inside one
   text-showing operation, so a value split across two draw operations is
   invisible to rungs 1 and 2 even with no wrap involved (F19). It also covers
   F20's case, where the pipeline holds a spelling no extraction contains
   verbatim, because it matches fragment by fragment rather than against the
   pipeline's string.
4. **Wrapped.** Head at the end of one line, tail at the start of the next,
   both redacted, the placeholder drawn over the head (F15 proved the gesture).
5. **UNLOCATED.** Nothing found. Step 6 owns what happens next.

Every rung reports its count. The counts reach the export review panel and the
run report, because "12 replaced literally, 3 across fragments, 1 wrapped" is
the honest description of what the export did.

### Step 3 — Rung 1's fits-check

`ReplaceText` redraws a longer placeholder at the same size and grows
RIGHTWARD; it does not shrink to fit (F10, measured 59.0 pt to 123.5 pt).
Accept rung 1 only after measuring the replacement's grown rectangle against
the same line's other fragments. Where it would overlap a neighbour, fall to
rung 2. Silent overlap was not observed on the fixture, and the check is the
guarantee rather than the observation.

### Step 4 — The redaction gesture

Rung 2 and below redact rather than replace, and the gesture is ONE
construct-Add-overlay unit:

- `NewRedactAnnotation` builds an UNBOUND annotation and `ApplyRedactions`
  silently does nothing for one that was only constructed. It must be
  `page.Annotations().Add(...)`ed (F6).
- The overlay placeholder is drawn through `SetOverlayText` with an EXPLICIT
  white `TextStyle.Color`. The apply path draws it black otherwise, which is
  invisible on a black box: extractable text nobody can see (F7).

The committed artefacts `backend/testdata/golden/pdf_gate_redact_before.png`
and `_after.png` are the reference for what this looks like.

### Step 5 — The save discipline

`RemoveUnusedObjects()` before EVERY `WriteTo`, never a naked `WriteTo`: the
library serialises every object in the table including one an edit orphaned, so
the pre-edit content stream survives unreferenced and readable (F5). Both
halves stay pinned by test: the discipline's output scans clean, and a naked
`WriteTo` is asserted to still retain the orphan, so neither the discipline nor
the library's behaviour can drift unnoticed.

### Step 6 — The refusal, and what may not trigger it

D6 refuses the export when an occurrence cannot be located, and the refusal
names the `.md` export as the way out (OQ5). Two revisions:

- **Only step 2's whole ladder decides.** An occurrence is UNLOCATED when every
  rung including the fragment walk has failed.
- **A manufactured value may never cause a refusal.** After step 1a the
  detector is no longer offered strings that were never contiguous text, so the
  class should be empty. Refusing an export over a leak that does not exist is
  worse than not offering the feature, so this is an acceptance measurement and
  not an assumption: re-run the G7 census on the reference deck and require
  UNLOCATED to fall from 10 to 0, or explain every survivor.

### Step 7 — The whole-file leak self-check

13b's scanner (`exportfmt/pdfscan.go`) becomes a BLOCKING self-check on every
produced PDF (D3): the export runs it over its own output bytes and fails the
export rather than handing back a file that still contains an original. It
already finds a planted sentinel in the content stream, the Info dictionary,
the XMP packet, an annotation, an outline title, an appended incremental body
and seven string encodings (G2), and it names DCTDecode streams as unscannable
rather than passing them silently.

This step also retires F13: `deadcode`'s alert on `pdfscan.go` disappears once
it has a production caller.

### Step 8 — The non-content surfaces

Per Q1's table and D4: the Info dictionary and the XMP packet are scrubbed or
dropped, annotation contents and outline titles are covered by the replacement
pass, and per OQ4 embedded file attachments and JavaScript actions are DROPPED
from the anonymised file, reported and never silent. An attachment is an inner
document the pipeline never read; carrying it through an "anonymised" file is a
leak wearing a paperclip.

### Step 9 — Damaged is not scanned

A truncated file never panics: the library reconstructs the xref and
salvage-opens with the pages still in the bytes (F8). So a 0-page result has two
causes and they need different sentences: `convert.ErrScannedPDF`'s exact
wording (`CLAUDE.md` §5) is for a document with no text layer, and a damaged
file gets its own actionable message. Keep a cheap recover shield, as the
incumbent carries.

### Step 10 — Copy and labels

Per D11. PDF keeps its EXPERIMENTAL label (OQ3). The copy may promise that the
exported PDF is the original file with the text replaced, and must not promise
that pictures are handled: that is 13d. `frontend/copy.js` owns every sentence;
Go returns CODES. No em dashes, no retired route name; `copy_guard_test.go` and
`frontend/copy.test.js` enforce both.

### Step 11 — The dependency removals, gated — DONE 2026-08-22

The gate was taken: the owner confirmed the in-place path's tests on the two
reference documents (G7 UNLOCATED 0 on both) and accepted the one G4 shortfall
as the split rule refusing text that was never contiguous, not as a regression.

`ledongthuc/pdf` and `go-pdf/fpdf` are out of `go.mod`, `go.sum` and
`vendor/`; the regenerated-layout export (`exportfmt/pdf.go`) and the baseline
extractor (`convert.PDFWithPagesLedongthuc`) are deleted; a grep for either
import returns nothing outside `docs/`. D12's arithmetic is +1 and −2.

Two things had to move with them rather than simply disappear:

- **`convert.PDFPages` was a production caller.** The unit count read the page
  count through the retired parser, which made it a second opinion nobody
  reconciled: it could report pages the extractor never opened, while the
  page-scoped model scan addresses the extractor's pages. It now reads the
  count through the same library the extraction goes through.
- **G4 lost its comparison and gained recorded floors.** See
  `exportfmt.referenceFloors` and `referenceFloorTolerance`, and F23 below.

### Step 12 — Reconciliation (the last step, an obligation)

Before this batch is reported finished:

1. Update the batch status table in `docs/change-13.md` §6.
2. Append every finding of steps 1 to 11 to the findings log in §7, with the
   decision each one forced, measurements as counts.
3. **Revise 13d's scope in place** where a finding invalidates it, and amend any
   main-plan decision a finding invalidates, in the decisions table itself.
4. Amend §11's reference-document section with the re-run G7 census.

---

## 3. Tests this batch moves

- **New, unit tier:** the split rule (1a) and the join rule (1b), table-driven
  over synthetic fragment geometries, including the ten measured gaps.
- **New, unit tier:** the ladder's rung selection, one case per rung, including
  a value split across two fragments on one line (rung 3) and a value the
  pipeline spells differently from any extraction (F20).
- **New, integration tier:** in-place export over the PDF fixtures; the produced
  file passes the whole-file scan; the original string is absent; neighbours
  are pixel-identical outside the replaced region.
- **New, integration tier:** the refusal path, asserting it does NOT fire for a
  value the old extraction would have manufactured.
- **Moved:** `framework_agreement` suite must stay green and untouched.
  `TestCodeDetectorDoesNotOverlapPassOne` and the pass-1 boundary tests are
  unaffected but must be run.
- **Moved:** `pdffoss_gate_deep_test.go` gains the re-run census of step 6 as an
  assertion rather than a log line, now that the target number is known.
- **Kept green:** `pdf_boundary_test.go` with its first production caller;
  `TestAnonymiseNeverCallsOllama`; both parity guards.
- **Frontend:** the export review panel's rung counts and the changed copy.

`docs/TESTING.md` owns the tiers and the scoping procedure; read it before
writing any of the above.

---

## 4. Files this batch touches

- `backend/engine/convert/pdf.go` — the extractor moves; `RepairPDFText` stays.
- `backend/engine/convert/` — the new fragment/line type.
- `backend/engine/exportfmt/pdf*.go` — in-place export, the ladder, the save
  discipline, the self-check wiring.
- `backend/engine/exportfmt/pdfscan.go` — gains its production caller.
- `backend/app_export.go` — the refusal and the rung counts.
- `frontend/copy.js`, the export review panel, `frontend/BRIDGE.md`.
- `CLAUDE.md` §5's PDF rules and §7's table; both charters; `README.md`;
  `frontend/docs/index.html`.
- `docs/change-13.md` — reconciliation (step 12).
- Only after the owner's confirmation: `go.mod`, `go.sum`, `vendor/`.

---

## 5. Acceptance criteria

1. Both suites green; the rendering harness green (this batch touches the UI).
2. Detection over the new extraction finds at least what it finds today, per
   category, on both reference documents and on every PDF fixture (G4 holds).
3. **The G7 census on the reference deck reports UNLOCATED 0**, or every
   survivor is explained in the findings log.
4. The exported PDF of every fixture passes the whole-file leak check, and the
   check is BLOCKING in production, not test-only.
5. A produced PDF is the original's bytes with replacements applied: page count,
   fonts and images unchanged, and pixels outside replaced regions identical.
6. Ladder rung counts appear in the export review panel and the run report.
7. The refusal never fires for a value extraction manufactured.
8. `SessionVersion` still 13; every existing session file still loads.
9. The scanned-PDF refusal is byte-identical; a damaged file gets its own
   message.
10. `pdf_boundary_test.go` green with the library's first production caller.
11. After the owner's confirmation only: `go.mod` no longer names
    `ledongthuc/pdf` or `go-pdf/fpdf`, and a grep for the removed imports
    returns nothing outside `docs/`.
