# CHANGE-13b — The adoption gate for aspose-pdf-foss-for-go

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0, pure Go + Wails v2, no CGo, no npm). It is **ONE
batch**, sized for one session.

It is the first batch of the change planned in `docs/change-13.md`, and that
plan is its context: the decisions (D1 to D16), the ten answered questions, the
GO/NO-GO criteria (G1 to G9) and the open owner questions (OQ1 to OQ5) all live
there and are referenced here by number rather than restated. Read the main
plan first.

**This batch wires nothing into the product and changes no user-visible
behaviour.** It adds the dependency behind a gate, builds the two guards with
teeth (the Q7 boundary guard and the Q1 whole-file leak check), creates the
fixtures, takes every measurement in Q10, and exits with a written GO/NO-GO
appended to `docs/change-13.md`. If the gate says NO-GO, this batch also
removes what it added, and the repository is left exactly as it was, plus the
recorded rejection.

**Do not start this batch until the owner has answered OQ1** (whether a pre-1.0
dependency with almost no adoption is acceptable for client documents at all).
Running the gate first and asking afterwards spends a session measuring a
library the owner may refuse on principle.

## Ground rules

The Ground rules block of `docs/change-13.md` applies in full. The ones this
batch can actually violate, restated:

- **No CGo, no `purego`, no native artefact.** The module is pure Go; anything
  that turns out not to be is an automatic NO-GO.
- **The local-only guarantee.** Nothing in this batch may construct a network
  request outside `backend/ollama/client.go`, including from a test: the gate
  tests exercise the library on bytes and never configure a copilot. The
  boundary guard (step 4) is built and green BEFORE the measurement steps run.
- **The engine stays UI-agnostic and originals are immutable.** Gate tests
  read fixtures through `os.ReadFile` in the test (tests may do I/O;
  `docs/TESTING.md` tiers govern where) and hand the library bytes via
  `OpenStream`. No production file under `backend/engine/` changes in this
  batch.
- **The production binary is unchanged.** The module may be imported ONLY by
  `_test.go` files in this batch. `wails build` compiles no test file, so the
  shipped binary cannot contain the library yet; the boundary guard makes that
  checkable.
- **A change is not finished until its tests move with it**; both suites gate:
  `go test ./...` and `node --test "frontend/**/*.test.js"` (the frontend
  suite is untouched by this batch and must simply stay green).
- **Comments explain intent in the present tense.** No "will be used by 13c",
  no batch numbers in code comments.
- **Confidentiality of the reference documents.** The owner's 2-page
  email-thread PDF and 15-slide deck live outside the repository, are read
  through an environment variable, and every measurement on them is reported
  as COUNTS. No name, company or acronym from them may reach a fixture, a
  comment, a commit message, a document in `docs/` or a memory.

### The deviation rule

If a step below is wrong, contradicted by the code, or cannot be done as
written: **stop, say so, and propose the alternative before writing it.**

---

## 1. What this batch produces

| Artefact | Where |
|---|---|
| the module, pinned and vendored | `go.mod`, `go.sum`, `vendor/github.com/aspose-pdf-foss/aspose-pdf-foss-for-go/` |
| the pinned-versions rows: the module, the OFL fonts, the go-cpp rejection | `CLAUDE.md` §7 |
| the boundary guard | `pdf_boundary_test.go` (repo root, package `main`, unit tier) |
| the whole-file leak scanner (13c's future runtime self-check, built now as the gate's instrument) | `backend/engine/exportfmt/pdfscan.go` + `pdfscan_test.go` |
| the fixture generator and the committed fixtures | `backend/engine/convert/` fixture helper + `backend/testdata/pdf_gate_*.pdf` |
| the gate tests | `backend/engine/convert/pdffoss_gate_integration_test.go`, `backend/engine/exportfmt/pdffoss_gate_integration_test.go`, `backend/engine/exportfmt/pdffoss_gate_deep_test.go` |
| the GO/NO-GO note and every measurement | appended to `docs/change-13.md` §6 and §7 |

Naming note: the gate test files carry the tier in their name per
`docs/TESTING.md` (`_integration_test.go`, `_deep_test.go`), open with their
`//go:build` line and a header justifying the tier, and use the
`extraction` / `roundtrip` / `redaction` subtest prefixes so they can be
scoped with `-run`.

---

## 2. Execution order

### Step 1 — Baseline first, with the incumbent

Before the module exists in the tree, measure what today's pipeline does, so
every later number has a denominator:

1. Import wall clock and extraction output for the two reference documents
   through the CURRENT `convert.PDFWithPages` (a throwaway deep-tier test or a
   `go run` scratch program is fine; the numbers are what is kept, in the
   findings log).
2. Detection counts over that extraction: run the offline routes exactly as a
   session would and record values found per category, as counts.
3. Note how often `RepairPDFText` changed a line (the `repaired` flag): the
   D8 measurement's baseline.

Record all of it in `docs/change-13.md` §7 (findings log) as counts, and set
the G8 wall-clock budgets from these numbers (recommendation: import within 3x
the incumbent, export within 30 seconds per reference document; adjust from
what the baseline actually says and record the chosen budgets beside the
numbers).

### Step 2 — Add, pin, vendor

1. `go get github.com/aspose-pdf-foss/aspose-pdf-foss-for-go@v0.7.0`, then
   `go mod vendor`. Confirm the vendored `go.mod` has no `require` block and
   `go 1.24` (re-verifying the zero-dependency claim against what actually
   landed, not the README).
2. `CLAUDE.md` §7 gains three rows, per D12: the module (pinned v0.7.0, the
   reason for the exact pin, the never-automatic bump rule naming this gate),
   the bundled OFL 1.1 fonts (Arimo, Tinos, Cousine, Carlito; data with a
   licence, in the shape of the Material Symbols and font8x8 rows), and the
   `aspose-pdf-go-cpp` rejection row (commercial licence, evaluation
   watermark, native blob; `purego` rejected with it), in the shape of the
   pdfcpu row.
3. Confirm the audit layer ignores `vendor/`: run `task audit` (or its
   individual targets) and check `golangci-lint`, `deadcode`, `deadexports`
   and `govulncheck` neither scan the vendor tree for findings nor fail on
   it. If any tool walks it, scope that tool's configuration and record the
   edit in the findings log.

### Step 3 — The fixtures and their generator

Extend the `convert` package's committed-fixture mechanism (`fixture(...)`,
integration tier) to generate and commit `backend/testdata/pdf_gate_*.pdf`:

- `pdf_gate_text.pdf`: three pages of English and French prose carrying
  invented names (never from the reference documents), one value wrapped
  across a line break, one value in a second, smaller font size.
- `pdf_gate_surfaces.pdf`: an Info dictionary and an XMP packet carrying an
  invented name, a text annotation carrying one, an outline title carrying
  one, and a page thumbnail (`/Thumb`).
- `pdf_gate_images.pdf`: a JPEG placed on two pages (one XObject or two, as
  the generator produces; record which, it matters to D9), one PNG, and if
  the generator can produce them, one inline image (`BI..EI`).

The generator may use the library itself (it is test-side code); what it may
not be is an uncheckable blob. If a surface cannot be generated pure-Go
(the thumbnail, the inline image), construct it by byte-level assembly in the
generator with a comment explaining the object being written; a hand-built
minimal PDF object is still checkable source.

### Step 4 — The boundary guard (before any measurement runs)

`pdf_boundary_test.go` at the repo root, package `main`, unit tier, in
`vocabulary_guard_test.go`'s idiom (whole-token scan, tiny named exemptions,
failure messages that name the fix). Three assertions, per D13:

1. **Forbidden symbols.** Generate the symbol table first: list every exported
   symbol declared in vendored files whose import block names `net/http`, plus
   the four copilots and `MakeSearchable` by name. Commit the table in the
   guard with a comment per entry. The scan walks `backend/`, `frontend/` and
   `scripts/` (not `vendor/`, not `docs/`) and fails on any whole-token hit.
2. **The vendored network inventory.** Enumerate every vendored file of the
   module importing `net/http` (or `net`, or constructing an HTTP client);
   compare against a committed list inside the guard. The failure message
   says: the library's network surface changed, re-review it deliberately,
   update the inventory, and record the review in `docs/change-13.md` §7.
3. **The engine path ban.** Under `backend/`, forbid the library's file-path
   entry points (`Open(`, `Save(`, `OpenWithPassword(` qualified by the
   library's package identifier): the engine takes bytes, so only
   `OpenStream`, `OpenStreamWithPassword` and `WriteTo` may appear.

Demonstrate the guard red-green: add a temporary reference to a forbidden
symbol in a scratch file, watch the guard fail naming it, delete the scratch
file. The gate tests written in later steps run under this guard from the
start, which is the point of building it first.

### Step 5 — The whole-file leak scanner

`backend/engine/exportfmt/pdfscan.go`: the Q1/D3 instrument, production code
(it becomes 13c's blocking runtime self-check, which is why it is not a test
helper), engine-idiomatic (bytes in, findings out, heavy comments, actionable
errors):

- walk every object in a PDF byte stream: inflate every stream
  (`FlateDecode` via the standard library's `compress/zlib`/`compress/flate`;
  other filters decoded where the library exposes a decoder, otherwise the
  raw bytes are scanned as-is and the filter name is reported as unscannable),
  and decode every literal string, hex string and UTF-16BE string;
- scan the decoded bytes for a set of needles (case-insensitive, and with
  whitespace collapsed, matching what `assertNoOriginals` does today);
- report each hit with the SURFACE it sits in (object number and the nearest
  dictionary context: content stream, `/Info`, XMP, annotation, outline,
  appended body after a second `%%EOF`), so the export's refusal can name it.

`pdfscan_test.go` (unit tier, table-driven) proves it finds a planted needle
in each surface of `pdf_gate_surfaces.pdf` and in a hand-appended
incremental-update body, and finds nothing in a clean file. This is the
red-green demonstration G2 requires.

### Step 6 — The save-semantics proof (G1)

Integration test: open `pdf_gate_text.pdf` via `OpenStream`, `ReplaceText` a
sentinel, `WriteTo`, then:

- run the step-5 scanner over the output for the sentinel: zero hits required;
- assert the output carries no incremental-update shape: a single body (no
  second `%%EOF` with a `/Prev`-chained cross-reference), no readable
  superseded object holding the sentinel.

If `WriteTo` appends instead of rewriting, stop measuring and record the
NO-GO per D2, unless the library documents a full-rewrite save mode that
passes this same test; the deviation rule applies.

### Step 7 — Extraction and API reality checks (fixtures)

Integration tests over the committed fixtures, each a table-driven subtest
with the `extraction` / `roundtrip` / `redaction` prefixes:

1. **Extraction parity (G4's fixture half):** per-page text via the library
   versus `convert.PDFWithPages`; assert every planted name is present in
   both; count kerning artefacts in each (the D8 measurement's fixture half).
2. **Round-trip fidelity (G5):** open, `WriteTo` with no edits; page count,
   extractable text and image inventory preserved; rasterise before and after
   and count differing pixels.
3. **ReplaceText behaviour (G6):** replace a short value with a longer
   placeholder; original absent, placeholder present; rasterise and assert
   pixels outside the replaced rectangle are unchanged; RECORD what happens
   inside it (shrink, overflow, clip) rather than asserting a hope.
4. **Redact-and-redraw (rung 2):** `NewRedactAnnotation` over a
   `SearchText` bounding box, `ApplyRedactions`, `Page.AddText` into the same
   rectangle; scanner finds no original; rasterise and eyeball-check artefact
   committed for the owner (OQ2's evidence).
5. **The wrapped value:** confirm `SearchText` does not find it (the
   documented limit), then prototype the fragment location: find the head at
   a line end and the tail at the next line start via
   `Page.ExtractTextWithLayout`, and redact both boxes. What this costs and
   whether the geometry is reliable is exactly what G7 needs to know.
6. **Images:** `ImageInfos()` on `pdf_gate_images.pdf`: does the twice-placed
   JPEG list once or twice (feeds D9's hash decision); `Extract()` gives
   decodable PNG/JPEG bytes; push those through `imaging.Treat` (box and
   blur) and back through `ReplaceFromStream`; re-open and assert the
   original image bytes are gone from the file and the treated ones are
   there; `Remove()` on the PNG and assert both the placement and the bytes
   are gone. Record whether the inline image is listed at all.
7. **Metadata:** read Info and XMP through the library; compare Info against
   `exportfmt.ExtractPDFMetadata` on the same file.

### Step 8 — Failure behaviour (G9)

Integration tests: a truncated fixture (cut `pdf_gate_text.pdf` at 60%) must
error actionably with no panic escaping (wrap with `recover` in the test to
prove the library's own behaviour; note whether 13c needs the same recover
shield `convert/pdf.go` carries today); an encrypted fixture (generate with a
password if the library can write one, else vendor a minimal hand-built
encrypted file) must be distinguishable so the refusal can say "remove the
password first"; a text-free fixture must be detectable so
`convert.ErrScannedPDF` fires byte-identically.

### Step 9 — The reference documents (deep tier, env-gated)

`pdffoss_gate_deep_test.go`, `//go:build deep`, reading paths from
`DOC_ANONYMISER_REFERENCE_PDF` and `DOC_ANONYMISER_REFERENCE_DECK` (the deck
measurement applies only if the owner supplies a reference PDF pair; skip
with an actionable message when unset):

1. **G4:** detection counts per category over the library's extraction versus
   the step-1 baseline.
2. **G7:** ladder rung counts: of every occurrence the pipeline would replace,
   how many `SearchText` finds literally, how many the tolerant pattern
   finds, how many are wrapped, how many are UNLOCATED.
3. **G8:** import and export wall clock against the step-1 budgets.
4. **G5 on real files:** open, `WriteTo`, rasterise, count differing pixels.

Everything reported as counts and milliseconds. Nothing from the documents'
content reaches the repository.

### Step 10 — The GO/NO-GO note

Append to `docs/change-13.md`: one row per criterion G1 to G9 with the
measured numbers and PASS/FAIL, a dated GO or NO-GO conclusion, and the
consequences for 13c and 13d (which ladder rungs are load-bearing, whether
`RepairPDFText` retires per D8, whether inline images need a warning code per
Q4, the confirmed asset-identity behaviour for D9).

On **NO-GO**: remove the module (`go.mod`, `go.sum`, `vendor/`), keep the
fixtures and the baseline numbers, keep `pdf_boundary_test.go` deleted (it
guards a dependency that no longer exists), convert the `CLAUDE.md` §7 module
row into a rejection row recording the failing criteria, and mark 13c and 13d
abandoned in the status table.

### Step 11 — Reconciliation (the last step, an obligation)

Before this batch is reported finished:

1. Update the batch status table in `docs/change-13.md` §6: 13b's state,
   session and one-line outcome.
2. Append every finding of steps 1 to 10 to the findings log in §7, with the
   decision each one forced.
3. **Revise `docs/change-13.md`'s scopes for 13c and 13d in place** where a
   finding invalidates them, and amend any main-plan decision a finding
   invalidates, with the reason, in the decisions table itself. A finding
   that contradicts the plan is recorded in the plan, never quietly worked
   around in a later document.
4. Confirm the owner questions OQ2, OQ4 and OQ5 are put to the owner with the
   gate's evidence attached (the rasterised before/after images for OQ2),
   because 13c cannot be written without the answers.

---

## 3. Tests this batch moves

All new; nothing existing changes behaviour, so no existing assertion is
touched. New: `pdf_boundary_test.go` (unit),
`backend/engine/exportfmt/pdfscan_test.go` (unit),
`backend/engine/convert/pdffoss_gate_integration_test.go` and
`backend/engine/exportfmt/pdffoss_gate_integration_test.go` (integration),
`backend/engine/exportfmt/pdffoss_gate_deep_test.go` (deep, env-gated skips),
plus the fixture generator additions in the `convert` fixture helper.

Scoping per `docs/TESTING.md`: `go test ./...` (the unit tier, which now
includes the boundary guard and the scanner tests), then
`go test -tags=integration ./backend/engine/...`, then the deep tier on the
machine that holds the reference documents. The frontend suite runs once to
confirm it is untouched. The compile-rot guard
(`go vet -tags=integration,deep ./...`) must pass, since this batch is mostly
tagged files.

---

## 4. Files this batch touches

```
go.mod  go.sum  vendor/github.com/aspose-pdf-foss/aspose-pdf-foss-for-go/**
CLAUDE.md                                        (§7: three new rows)
pdf_boundary_test.go                             (new)
backend/engine/exportfmt/pdfscan.go              (new)
backend/engine/exportfmt/pdfscan_test.go         (new)
backend/engine/exportfmt/pdffoss_gate_integration_test.go  (new)
backend/engine/exportfmt/pdffoss_gate_deep_test.go         (new)
backend/engine/convert/pdffoss_gate_integration_test.go    (new)
backend/engine/convert/<fixture helper>          (extended)
backend/testdata/pdf_gate_text.pdf               (generated, committed)
backend/testdata/pdf_gate_surfaces.pdf           (generated, committed)
backend/testdata/pdf_gate_images.pdf             (generated, committed)
docs/change-13.md                                (status table, findings log, GO/NO-GO)
Taskfile.yml / tools configuration               (only if step 2.3 finds a tool walking vendor/)
```

No file under `frontend/` changes. No production `.go` file changes except the
new `pdfscan.go`, which nothing in the product calls yet (13c wires it), and
which `deadcode` will therefore flag: exempt it in the audit configuration
with a comment naming `docs/change-13.md`, or keep it exercised through its
unit test if the tool counts test usage; record whichever in the findings log.

---

## 5. Acceptance criteria

1. `go test ./...`, `go test -tags=integration ./...` and
   `node --test "frontend/**/*.test.js"` all green; the deep tier green or
   skipping with actionable messages on a machine without the reference
   documents.
2. The module is pinned at exactly v0.7.0, vendored, and imported by
   `_test.go` files and `pdfscan.go` only; `wails build` (or
   `go build ./...`) succeeds and the boundary guard is green, so no
   production code path reaches a copilot or a file-path API.
3. `pdf_boundary_test.go` exists with the generated symbol table and the
   committed vendored network inventory, and its red-green demonstration is
   described in the commit message.
4. The step-5 scanner finds every planted sentinel in every surface of
   `pdf_gate_surfaces.pdf` and in an appended incremental body, and passes
   clean on a clean file.
5. Every G1 to G9 measurement is recorded in `docs/change-13.md` as counts and
   milliseconds, with a dated GO or NO-GO conclusion; no string from the
   reference documents appears anywhere in the repository.
6. `CLAUDE.md` §7 carries the module row, the fonts row and the go-cpp
   rejection row; on NO-GO, the module is removed and the row records the
   rejection instead.
7. The reconciliation step ran: the status table and findings log are updated,
   and the 13c/13d scopes in `docs/change-13.md` are revised where findings
   invalidated them.
8. No user-visible behaviour changed: a PDF imports, anonymises and exports
   exactly as before this batch, byte for byte.
