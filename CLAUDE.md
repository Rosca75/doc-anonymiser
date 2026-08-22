# CLAUDE.md — doc-anonymiser

This file is the single source of truth for this repository. It overrides any
conflicting instruction found elsewhere. Re-read it before every work session.

## 1. Project overview

doc-anonymiser is a Windows-first desktop application (Go + Wails v2, pattern
P0 — pure Go, no CGo, no npm) that anonymises text-based client documents
(.txt, .csv, .md) entirely on the local machine. It replaces two internal
Python notebooks. The anonymisation pipeline is DETERMINISTIC end to end
(built-in pattern matching + the accepted Values), and discovery is a separate,
earlier step: heuristic and signal-based discovery run offline, and a local LLM
served by Ollama over localhost HTTP can be switched on beside them. Every
discovery method produces Suggestions the user accepts or rejects, so
anonymisation itself runs no discovery method and reaches no model: the only
Values a run can apply are ones the user accepted on Identify or declared
while reviewing the result on Anonymise.
Fallback decision recorded: if local-LLM quality proves insufficient for NER,
the fallback is pattern P4 (a small ONNX NER model running via ONNX Runtime
Web inside the WebView) — do NOT introduce CGo bindings under any circumstance.

## 2. Owner profile

The owner orchestrates LLM coding agents and is not an expert in Go. All code
must be heavily commented, explaining intent, not just mechanics. Never assume
the owner will debug at the language level. Error messages must be actionable:
what failed, what was expected, how to fix it.

## 3. Repository structure

The repo is split into two self-documenting top-level folders, `frontend/`
and `backend/`, so GUI-focused and engine-focused work can be prompted
independently. Each folder has its OWN `CLAUDE.md` charter that Claude Code
auto-loads when editing files inside it; this root file stays authoritative
for cross-cutting rules (§4-§8), and the charters own their subtree detail:

- `frontend/CLAUDE.md` — frontend charter (module map, discipline, typography,
  copy rules).
- `frontend/BRIDGE.md` — the Go ↔ JS method/event contract (the design → code
  handoff surface).
- `backend/CLAUDE.md` — backend charter (engine passes, converters, Ollama
  boundary, binding namespace).

The module anchor (`main.go`, the `//go:embed all:frontend` directive and
`wails.json`) stays at the ROOT because `go:embed` cannot reference a parent
directory: the embedding file must sit at or above `frontend/`.

```
doc-anonymiser/
├── CLAUDE.md                  # this file — authoritative for cross-cutting rules
├── README.md                  # user-facing documentation
├── LICENSE                    # MIT, Oscar Liber
├── .gitignore / .gitattributes
├── go.mod / go.sum            # module root (module path: doc-anonymiser)
├── wails.json                 # Wails config; assetdir: "frontend"
├── main.go                    # Wails bootstrap ONLY: //go:embed all:frontend; backend.NewApp()
├── embed_test.go              # asserts the frontend is embedded (package main)
├── backend/app_e2e_test.go    # headless end-to-end through the bound app layer
├── category_parity_test.go    # JS↔Go category parity guard (package main)
├── preset_parity_test.go      # JS↔Go PRESET table parity: the same rows, in the
│                              #   same order, filling the same categories, and a
│                              #   copy.js title for every family (package main)
├── detection_parity_test.go   # JS↔Go discovery-method, match-class,
│                              #   signal-source and detection-PHASE parity guards
│                              #   (package main)
├── dataset_parity_test.go     # no camel-case data attribute is rendered: a browser
│                              #   lower-cases attribute NAMES, so dataset.x is dead
├── icon_parity_test.go        # every icon name used exists in ICONS and every ICONS
│                              #   entry is drawn: icon() fails soft to ""
├── value_shape_test.go        # the Value wire shape: no retired key comes back,
│                              #   and every current field is present
├── result_shape_test.go       # every doc.<field> the Anonymise view reads IS a key
│                              #   engine.ResultDocument emits: JS reads a missing
│                              #   key as undefined and says nothing about it
├── image_parity_test.go       # JS↔Go picture parity guards: the treatments, the
│                              #   sniffed formats, the occurrence kinds, and a
│                              #   sentence in copy.js for every reason, warning
│                              #   and blocked-treatment CODE (package main)
├── copy_guard_test.go         # no em dashes, and no retired ROUTE NAME, in a Go
│                              #   user-facing string (package main)
├── vocabulary_guard_test.go   # no retired IDENTIFIER survives anywhere outside
│                              #   docs/: every one is a string at a boundary, so a
│                              #   survivor is not a compile error (package main)
├── uitest_parity_test.go      # keeps the two UI harnesses on ONE probes.js (package main)
├── frontend/                  # THE GUI — vanilla ES modules, embedded via go:embed
│   ├── CLAUDE.md              # frontend charter (see above)
│   ├── BRIDGE.md              # Go↔JS contract (see above)
│   ├── index.html
│   ├── brand.css / style.css  # brand tokens (single source) + styling
│   ├── api.js                 # THE ONLY file that calls Go bound methods
│   ├── state.js               # single source of truth for frontend state
│   ├── main.js / shell.js / ui.js / html.js / icons.js / copy.js / scroll.js
│   ├── nav.js                 # THE one place the wizard moves (per-screen footers + step bar)
│   ├── toast.js / modal.js    # state-backed notice strip + in-app confirm (no native dialogs)
│   ├── highlight.js / panesearch.js / valuespans.js / valuemodel.js
│   ├── suggestionmodel.js
│   ├── countries.js
│   ├── views/                 # one JS module per wizard step + shared panels:
│   │                          #   home.js, import.js, export.js,
│   │                          #   anonymise.js (step 3's tabs, footer and TEXT half)
│   │                          #   + anonymiseimages.js (its IMAGE half),
│   │                          #   identify.js (layout) + identifyrail.js (choices)
│   │                          #   + identifyworkspace.js (values)
│   │                          #   + detectionrun.js (the ONE Run detection control
│   │                          #     and the ONE call: the button is drawn in the
│   │                          #     rail's head and the findings land in the
│   │                          #     workspace, so neither half may own it),
│   │                          #   allowlist.js
│   ├── docs/                  # bundled offline user docs (SECOND window, embedded only)
│   ├── assets/icons/          # vendored Material Symbols SVGs + LICENSE
│   ├── testhtml.js            # dev-time HTML query helper for the render tests
│   ├── testdom.js             # dev-time minimal DOM for the WIRING tests: its parser
│   │                          #   lower-cases attribute names, as a browser does
│   └── *.test.js              # node --test "frontend/**/*.test.js" (zero npm deps)
├── backend/                   # ALL Go business logic + the Wails bound-app layer (package backend)
│   ├── CLAUDE.md              # backend charter (see above)
│   ├── app.go                 # Wails bound struct: thin adapters to engine/* and ollama/*
│   ├── app_values.go / app_detect.go / app_export.go / app_run.go  # method groups
│   ├── app_images.go          # method group: the picture inventory and previews
│   ├── engine/                # UI-agnostic anonymisation engine
│   │   ├── framework_agreement_test.go # the fixture pair as a regression suite:
│   │   │                      #   reproduction against the reference, plus the
│   │   │                      #   precision and recall numbers
│   │   ├── document.go        # Document model, txt/csv/md ingestion
│   │   ├── csvmd.go           # CSV ⇄ markdown-table conversion (round-trip)
│   │   ├── convert/           # binary-format → markdown converters (pure Go, one-way)
│   │   │   ├── docx.go / pptx.go / xlsx.go / pdf.go
│   │   │   ├── pdflayout.go   # the PDF fragment line model: rectangles per
│   │   │                      #   fragment, a split rule (fragments merely
│   │   │                      #   sharing a baseline are not one line) and a
│   │   │                      #   geometry-gated wrapped-line join; the working
│   │   │                      #   markdown is DERIVED from it, and the in-place
│   │   │                      #   export locates against the same model
│   │   ├── presets.go        # presets as scoped DATA: one row per scope, so a
│   │   │                      #   preset FAMILY is a chip row and adding one is a
│   │   │                      #   table row rather than a rewrite
│   │   ├── matchclass.go      # discovery methods (provenance) and match classes
│   │   │                      #   (precedence), kept as separate concepts
│   │   ├── signals.go         # which built-in signals may DERIVE Suggestions
│   │   ├── signaldiscovery.go # signal-based discovery: a match used as evidence
│   │   ├── evidence.go        # WHY a discovery method produced a Suggestion
│   │   ├── pii.go             # Pass 1: built-in pattern matching
│   │   ├── patternpreview.go # what pass 1 WOULD match, read-only, so Identify
│   │                          #   can SHOW it: a direct match has no review gate,
│   │                          #   which left the category switches uncheckable
│   │   ├── country.go         # Document-country model; which regex categories apply where
│   │   ├── conflicts.go       # ValidateValues: blocking conflicts + warnings, before pass 1
│   │   ├── intersections.go   # what two routes both claim, answered BEFORE a run
│   │   ├── families.go        # one Value, its spellings; the shorter form is main
│   │   ├── removals.go        # Removed Values: the session exclusion list
│   │   ├── values.go          # Value model, categories, spelling derivation
│   │   ├── discover.go        # heuristic discovery, and the unified Suggestion
│   │   ├── registry.go        # Placeholder registry (consistent pseudonyms)
│   │   ├── pipeline.go        # Pass orchestration over the category selection
│   │   ├── allowlist.go       # Terms never anonymised
│   │   ├── report.go          # Per-file / per-category / per-VALUE statistics
│   │   ├── session.go         # Save/load session state (JSON, schema migrations)
│   │   ├── imaging/           # pictures: the OOXML picture scanner, the format
│   │   │                      #   sniffer and the thumbnailer. Its own package
│   │   │                      #   because convert/ and exportfmt/ both need it
│   │   │                      #   and neither may own it
│   │   └── exportfmt/         # same-format export: rewrite of original bytes.
│   │                          #   docx/pptx/xlsx splice the archive; the pdf
│   │                          #   export (EXPERIMENTAL) replaces text IN PLACE
│   │                          #   through a location ladder (pdfladder.go,
│   │                          #   pdfinplace.go), refuses on an unlocatable
│   │                          #   occurrence, and runs the whole-file leak scan
│   │                          #   (pdfscan.go) as a blocking self-check
│   ├── ollama/
│   │   └── client.go          # THE ONLY FILE that talks to Ollama (net/http)
│   └── testdata/              # fixture documents for unit tests (lives with the engine that uses it).
│                              #   framework_agreement.docx + _anon.docx are a real
│                              #   contract and the same document as a human
│                              #   anonymiser produced it; their ground truth is
│                              #   framework_agreement_expected.json, as DATA rather
│                              #   than a golden blob (docs/TESTING.md)
├── Taskfile.yml               # local entry point for the audit layer (no make: `task audit`)
├── .golangci.yml              # the machine-readable half of §6's coding rules
├── tools/                     # SEPARATE Go module holding the audit tool deps.
│                              #   Separate so tool dependencies cannot take part
│                              #   in the application module's version resolution
│                              #   and move what `wails build` compiles.
├── scripts/
│   ├── genicon.go             # standalone icon generator (//go:build ignore)
│   ├── to_sarif.py            # deadcode/deadexports JSON -> SARIF 2.1.0 (stdlib only)
│   ├── to_sarif_integration_test.go  # its tests, in Go; //go:build integration (spawns python)
│   ├── audit_summary.py       # per-tool finding counts, read from the SARIF
│   ├── deadexports/           # frontend dead-export scanner (knip's job, no npm)
│   └── uitest/                # the real-rendering test layer (docs/UITESTING.md)
│       ├── probes.js          # THE ONE definition of the browser-side probes and
│       │                      #   the state they seed; BOTH harnesses read it
│       ├── renderharness/     # Linux, Chromium, Go + stdlib only (no new
│       │                      #   dependency: ws.go is a minimal RFC 6455
│       │                      #   client). Runs in CI as a BLOCKING step
│       └── Invoke-UITest.ps1  # Windows additional platform check (PowerShell +
│                              #   .NET, no packages): the real WebView2 engine
│                              #   plus a UI Automation smoke test of the
│                              #   packaged .exe. Never yet executed
├── .github/workflows/
│   ├── audit.yml              # deterministic static analysis -> code scanning
│   ├── ci.yml                 # build + test on push/PR
│   └── release.yml            # on tag: build, zip, attach to Release
└── docs/                      # phased build plans and change orders
    ├── UITESTING.md           # the three test layers and how to run each
    ├── audit.md               # the audit layer: running it, dismissing, adding a tool
    ├── audit-baseline.md      # first full run: counts, genuine vs noise
    └── brand/color-palette.json  # vendored brand palette (source for frontend/brand.css)
```

## 4. Architecture rules

- **Local-only guarantee (non-negotiable):** the application performs no
  network I/O except HTTP to `127.0.0.1:11434` (Ollama). No telemetry, no
  update checks, no remote fonts/CDNs. All frontend assets are vendored in
  `frontend/` and embedded in the binary.
- **One-file external boundary:** only `backend/ollama/client.go` may
  construct HTTP requests to Ollama. `backend/engine/*` receives an interface,
  never a concrete client — this keeps the P4 fallback a contained refactor.
- **Engine is UI-agnostic:** nothing under `backend/engine/` imports Wails or
  reads the filesystem paths chosen by the user; documents arrive as `[]byte`
  + filename via `backend/app.go`. This keeps the engine unit-testable
  headless.
- **Frontend discipline** (detail in `frontend/CLAUDE.md`): `api.js` is the
  only bridge caller; `state.js` is the only state holder; view modules render
  from state and dispatch actions. The Wails binding namespace is
  `window.go.backend.App` (App lives in package `backend`); the full method
  contract is `frontend/BRIDGE.md`.
- **Documentation opens in a second window (BUILD-04 CR6):** the
  "Documentation" menu entry opens a SEPARATE window whose content comes from
  `frontend/docs/*`, embedded by the same `go:embed` directive. It may load
  NOTHING but embedded assets (no `http(s)://`, no CDN, no system-browser
  hand-off): Wails v2 drives one native window per process, so Go owns the path
  (`backend/app.go DocumentationURL`) and the frontend opens it with
  `window.open` (`api.js openDocumentation`). Do NOT convert it to
  `runtime.BrowserOpenURL`. Full mechanism in `frontend/CLAUDE.md`.
- **Originals are immutable:** imported files are read once and never written
  back to their source path. All output goes through explicit save dialogs.
- **Graceful degradation:** Ollama availability is probed at startup and on
  demand (`GET /api/tags`). Every LLM-dependent UI control renders in a
  disabled state with a tooltip ("Requires Ollama, which was not detected
  on 127.0.0.1:11434") when unavailable. The deterministic pipeline must be
  fully usable without Ollama. User-visible copy never contains em dashes
  (enforced by copy_guard_test.go and frontend/copy.test.js), and never a retired
  route name (the same two guards).
- **Converters are pure Go and one-way:** `backend/engine/convert/*` may use
  only the Go standard library, excelize, and the vendored
  aspose-pdf-foss-for-go library (pinned in §7, and carrying one patched file,
  `cmap.go`, for the reason recorded there). There is exactly ONE PDF
  library in the module: a second parser kept to check the first is a
  dependency in the shipped binary, a second reading of every document, and a
  moving reference, and G4's recorded floors ask the part of that question
  worth asking.
  No CGo, ever. Binary formats convert TO markdown on import for preview and
  processing. The app can additionally write a NEW anonymised copy in the
  source format (docx/pptx/xlsx, and experimentally pdf) at export time; this
  copy is produced by rewriting a copy of the original bytes held in memory
  (`backend/engine/exportfmt/`); the PDF copy is the original's own structure
  with the text replaced IN PLACE, never a regenerated layout. The source file
  on disk is read once at import and never written, moved, or modified. If
  pure-Go PDF extraction quality proves unacceptable, the recorded fallback is
  a wazero-embedded WASM extractor (P3 pattern) — not a CGo binding.

## 5. Domain rules

- **Supported inputs:** `.txt`, `.csv`, `.md`, `.docx`, `.pptx`, `.xlsx`,
  `.pdf`. Reject anything else in the file dialog filter AND on drop, with a
  clear message. Conversion rules per format:
  - `.txt` → markdown as-is (line-ending normalisation).
  - `.md`  → passthrough.
  - `.csv` → Grid model + markdown-table preview; round-trips to CSV on export.
  - `.docx` → headings (paragraph styles Heading 1–6 → #..######), bold/italic
    runs, ordered/unordered lists (numPr), tables → markdown tables,
    hyperlinks → markdown links. **Adjacent runs sharing one formatting state are
    coalesced and wrapped ONCE**, so the working form is the faithful markdown of
    the document's FORMATTING and not of Word's run bookkeeping: Word splits a
    paragraph into runs for proofing state, language tagging, revision ids and
    simply which editing session typed which characters, and a wrap per `<w:r>`
    turned a bold date into `**0****1****.01.20****01**`, which is intact in the
    document and unmatchable by any multi-token pattern. It is fixed in the
    converter rather than in a pre-detection pass so every consumer (pass 1,
    discovery, preview, the local-AI slices, export) benefits and none has to
    know. Images dropped with an inline placeholder
    `*[image omitted]*`. Headers/footers/footnotes dropped (pagination noise).
  - `.pptx` → one `## Slide N: <title>` section per slide; body text with
    bullet indentation; tables → markdown tables; speaker notes under a
    `**Notes:**` sub-block. Slide-master/branding shapes skipped.
  - `.xlsx` → one Document per sheet, named `<workbook>.xlsx#<sheet>`. Smart
    routing per sheet (nb1 rules): FLAT (no merged cells, contiguous data
    bounds, header-like first row) → Grid model, same behaviour as a CSV
    import including CSV round-trip export; COMPLEX → structured JSON
    rendered in a fenced code block, anonymised as text. Trailing empty
    rows/columns trimmed via data-bounds detection.
  - `.pdf` → per-page extraction through the vendored library's layout mode:
    text FRAGMENTS with rectangles, grouped into lines by the model in
    `convert/pdflayout.go`. Two rules shape a line, each against measured
    evidence: a line SPLITS where the gap between two fragments exceeds a
    plausible word space (expressed as a multiple of the font size, never a
    point value: fragments that merely share a baseline are not one line, and
    reading them as one manufactures a value that was never contiguous text),
    and a wrapped continuation JOINS its line only when the geometry agrees
    (baselines within 1.6 line heights, a shared left margin, the first line
    reaching its block's right edge, no terminal punctuation), because joining
    on punctuation alone glues headings together and invents names. The
    spacing-repair heuristic then runs over the derived text (collapse runs of
    single uppercase characters split by kerning; collapse doubled spaces).
    PDF support is EXPERIMENTAL and labelled as such in the UI. Three refusals,
    each with its OWN message, because each is a different problem with a
    different remedy and the wrong sentence sends the user somewhere useless.
    A PDF yielding no extractable text is rejected with: "No text layer found,
    this PDF is likely scanned. OCR is not supported; convert it externally
    first." A file so damaged that no page opens gets a message naming the
    damage: the scanned sentence would send the user to an OCR tool that cannot
    help. And a file whose text layer cannot be read as CHARACTERS is rejected
    with `convert.ErrUnmappablePDF`, which names re-exporting from the
    authoring application as the way out.

    That third refusal exists because its failure is otherwise SILENT, and
    silence here is a data-protection failure rather than a rough edge. A
    producer that embeds subset fonts with no usable ToUnicode CMap (Microsoft
    Print To PDF is the common one on this market's laptops) yields a text layer
    of thousands of characters, not one of which is a letter. The file is not
    scanned and not damaged, so neither other refusal fires; detection then
    truthfully reports finding nothing, and a user is entitled to read "nothing
    found" as "nothing to anonymise" in a document full of names. The gate is a
    share of unmappable characters (`convert.maxUnmappableShare`, 0.3), chosen
    to sit in the wide empty gap between a healthy document's measured 0.0 to
    0.2 per cent and such a file's 100 per cent, so it can never refuse a
    readable document over a page of symbols.

    The PDF EXPORT is in-place replacement: the produced file is the
    original's bytes with the pipeline's replacements applied
    (`exportfmt/pdfinplace.go`), never a regenerated layout. Each replaced
    string is found through a LOCATION LADDER, each rung more expensive and
    less precise than the one above: (1) literal search, replaced in line only
    when the grown placeholder provably stays clear of its same-line
    neighbours (it redraws at the same size and grows rightward, without
    reflow); (2) a whitespace-tolerant search, redacted; (3) a FRAGMENT WALK
    over the line model, whitespace-insensitive and ligature-folded, for a
    value split across draw operations or spelt differently from any
    extraction, redacted as the union of its fragments' rectangles; (4) a
    wrapped match, head and tail redacted with the placeholder over the head.
    Every redaction draws its placeholder as the annotation's own overlay text
    in EXPLICIT white (the apply path draws it black otherwise, which is
    extractable text nobody can see). An occurrence the whole ladder cannot
    locate REFUSES the export, naming the placeholder, the page and the .md
    export as the way out: a half-anonymised PDF that looks finished is worse
    than a refusal. The save is `RemoveUnusedObjects()` then `WriteTo`, never
    a naked `WriteTo` (§7's pin row says why), and the whole-file leak scan
    runs over the produced bytes as a BLOCKING self-check. Non-content
    surfaces follow the docx precedent: annotation contents, outline titles,
    the Info dictionary and the XMP packet are rewritten through the same span
    machinery plus the metadata review; embedded file attachments and
    JavaScript actions are DROPPED from the produced copy and reported, never
    silent. The ladder's rung counts reach the export review panel and the run
    report.
- **Process order (fixed):** 1) import → convert to markdown working form,
  2) anonymise, 3) export. CSV imports are converted to a markdown table for
  preview/processing but retain their grid model so they can round-trip back
  to CSV on export.
- **The detection vocabulary is the contract.** These words mean one thing
  each, in Go names, JavaScript names, JSON, comments, copy and documentation.

  | Term | Definition | Output |
  |---|---|---|
  | **Detection route** | A switchable user-facing mechanism, one switch each | Built-in patterns, Heuristic discovery, Signal-based discovery or Local LLM discovery |
  | **Built-in pattern matching** | Application-provided patterns for structured signals. The rail names it **Built-in patterns** | Direct matches |
  | **Signal-based discovery** | Uses a direct signal match as EVIDENCE to find related text | Suggestions |
  | **Heuristic discovery** | Uses spelling, context, frequency and deterministic gazetteers | Suggestions |
  | **Local LLM discovery** | The Ollama-backed route, used during Identify | Suggestions |
  | **Custom pattern matching** | The user's own regular expressions | Direct matches |
  | **Manual Value declaration** | A Value the user typed | An accepted Value |

  Two terms are RETIRED, from the interface AND from the identifiers.
  **Smart detection** was one name over three unrelated mechanisms (pattern
  matching, which acts without review, plus two discovery methods, which do not),
  so it could never say which of them found what. **Local AI** said nothing about
  what runs; its replacement is LLM-specific rather than model-generic because an
  encoder-model route would be its own section, with a different dependency,
  different settings and different failure modes.

  The labels went first and the identifiers followed, in that order and as two
  separate changes, because a label is a display string and an identifier is a
  contract: the identifiers are persisted, so renaming them costs every saved
  session and every saved profile on disk (`SessionVersion` 11), and that is a
  price to pay deliberately rather than inside a UI change. Two guards keep both
  halves true: `copy_guard_test.go` with `frontend/copy.test.js` fail on either
  retired PHRASE in a user-facing string, and `vocabulary_guard_test.go` fails on
  any retired IDENTIFIER anywhere in the tree outside `docs/`, which is where the
  change orders keep the record of what was renamed.

  Not every method produces Suggestions, and the difference is what the review
  gate is about. Pattern matching produces DIRECT MATCHES, applied without
  review, because a pattern is a rule the user chose. Every DISCOVERY method
  produces SUGGESTIONS, which must be accepted or rejected before anything is
  replaced.

- **The Value, the unit everything else is about:** a Value is one accepted
  replacement unit. It has a category, one MAIN TEXT, none or many SPELLINGS of
  the same real-world thing, and one placeholder for the whole family, whatever
  found it. That invariant lives in the registry and not only in validation,
  because detection is what produces the violation: `Registry.Assign` keeps a
  `byOriginal` index, so a string already owned under one category is never
  given a second placeholder under another.

  A **Suggestion** is an unreviewed potential Value. Nothing becomes a Value
  without the user accepting it, and accepting carries every discovery method,
  every piece of evidence and every folded spelling across intact.

  Declarations must not conflict: `engine.ValidateValues`
  (`backend/engine/conflicts.go`) runs inside `engine.Run` BEFORE pass 1 and
  returns blocking conflicts (the same string in two active categories, a
  spelling colliding with another Value, a declared Value that is also
  allowlisted) and warnings. A blocking conflict aborts before the registry is
  mutated: a half-run that assigned placeholders for a configuration the user
  was just told is invalid is unrecoverable without a new session.
- **Provenance and precedence are separate fields.** Two different questions get
  asked about every Value, and one field cannot answer both.

  `DiscoveryMethods` answers HOW WAS THIS FOUND. It is a SET, because several
  methods can find the same thing and two routes agreeing is corroboration worth
  showing rather than a fact to overwrite. A manually declared Value carries
  exactly `["manual"]`.

  | Discovery method | Produced by |
  |---|---|
  | `manual` | the user typed it |
  | `signal` | signal-based discovery |
  | `heuristic` | heuristic discovery |
  | `local_llm` | Local LLM discovery |

  The **match class** answers WHICH CLAIM WINS. It is derived from the methods by
  `engine.MatchClassForMethods`, which takes the STRONGEST: corroboration by a
  weaker method is not doubt. It is engine-internal, never user-editable state,
  and user-facing copy names the winning METHOD rather than a rank.

  | Match class | Rank | Produced by |
  |---|---:|---|
  | `built_in_pattern` | 1 | pass 1 pattern matches, and an already-decided registry entry |
  | `user_defined` | 2 | a manual Value or a custom pattern: the same act by the same person |
  | `rules_discovered` | 3 | signal-based or heuristic discovery |
  | `local_llm_discovered` | 4 | Local LLM discovery |

  LOWER WINS. An unknown or empty class ranks with `user_defined` rather than
  last, so a producer that states none is trusted rather than silently demoted:
  ranking it last turns a forgotten stamp into a missing replacement instead of
  an error. `Confidence` is a THIRD, separate thing, and it is DATA rather than a
  lever: it orders overlaps after the match class, it feeds the heuristic pass's
  own floor before a Suggestion is shown, and it is reported. It is not what a run
  filters on, and a Value is never dropped by it: see the checksum paragraph under
  pass 1 for the one score a user can ask to have vetoed, and why that is a
  checkbox rather than a percentage.

  The constants live in `backend/engine/matchclass.go`, are mirrored by
  `frontend/state.js DISCOVERY_METHODS` and `MATCH_CLASSES`, and are guarded by
  `detection_parity_test.go`, exactly as the categories are.

  `ResolveOverlaps` compares the match class FIRST, then confidence, then length, then
  start and category. Ownership is decided ONCE for the whole batch, before a
  single placeholder is minted (`engine.unifyOwnership`, phase B of
  `engine.Run`): resolving per document let the same string be won by
  different routes in different files, and `Registry.Assign`'s `byOriginal`
  index then froze whichever claim was assigned first, which is byte offset
  within document order. `Registry.Assign` is deliberately NOT
  precedence-aware, because changing an entry's category after the fact would
  change its placeholder text, and a placeholder that has left the machine can
  never be re-numbered.
- **Signal-based discovery: a match used as evidence.** A built-in pattern does
  two independent things, and the user controls the second without losing the
  first. It MATCHES AND REPLACES the signal, because an email address is
  personal data in its own right. It can also be EVIDENCE about text written
  elsewhere: `pierre.dupont@tpps.com` is deterministic evidence for a person and
  an organisation that may appear in prose in another file.

  Only the second is switchable, through `signalSuggestionSources`
  (`backend/engine/signals.go`, mirrored by `frontend/state.js SIGNAL_SOURCES`
  and `SIGNAL_DERIVATIONS`). Clearing it stops the Suggestions and must NEVER stop
  the signal being anonymised: that is governed by Built-in patterns and the
  signal's own category. Conflating the two is the mistake the separate setting
  exists to prevent, and a test asserts it at both the engine and the bound-app
  level.

  The second thing is not ONE question but several, because one signal supports
  several readings through several mechanisms. `signalSuggestionSources` is keyed
  by SOURCE and then by DERIVATION: an address's local part is evidence for a
  person (`email.person`, `personSeeds`) and its domain is evidence for an
  organisation (`email.organisation`, `organisationSeeds`), and a user who wants
  organisations from domains but does not want "pierre.dupont" read as a person is
  asking something the engine can answer. So each reading is gated on its own
  derivation in `discoverFromEmails`, and the invariant above holds PER READING.

  A WEBSITE is the second source (`SignalSourceWebsite`, derivation
  `url.organisation`, `discoverFromWebsites`). It exists because a document need
  contain no email address at all and still name its own parties: a measured
  framework agreement between two companies carried no address anywhere, so email
  evidence contributed nothing, while `www.nstar.lu` sat in it as deterministic
  evidence for the organisation NStar, whose spelling no derivation rule can
  produce from "Northstar". It has ONE reading, because a domain names an
  organisation and a URL path is a page rather than somebody, and it is filtered
  by exactly what an email domain is filtered by.

  A source identifier IS a built-in pattern category, which is why the website
  source's value is `CatURL` rather than the word "website": the rail renders a
  signal's readings on the row of the pattern that produces the evidence, so an
  identifier with no category row would be a control with nowhere to render.

  A source has no boolean of its own. `SignalSourceEnabled` DERIVES the signal's
  state from its readings (on when any is on), for the reason `smartRouteOn`
  already states: a stored flag beside the set it summarises can disagree with it.
  The rail renders the readings ON the signal's OWN category row, because a signal
  source identifier IS a built-in pattern category: a drill-down button after the
  label ("Signal-based suggestions"), the help icon after the button, and the
  readings revealed under the row. That is where the question belongs, beside the
  pattern that produces the evidence, and it costs the panel no permanent row. The
  row's own checkbox is untouched by it, and the opened panel carries a master over
  the readings, derived for display and never stored. `SignalDerivations` is the
  ONE definition of that tree; only readings with a producer behind them appear,
  because a row with nothing behind it is a control that appears to do something
  and does not.

  Discovery reads the WHOLE imported batch, because the evidence is in one file
  and the text it points at is usually in another. It suggests nothing unless the
  text occurs OUTSIDE the source signal, keeps the document's own casing and
  accents, rejects role mailboxes, single-token handles, public mail providers,
  public-suffix labels and infrastructure labels, and respects the allowlist and
  the session exclusions like every other producer.

  Shared evidence makes two findings **Related Values**, never one Value. Two
  organisations reached through one email domain may genuinely be two legal
  entities or two country branches, and one placeholder for two companies would
  make the mapping CSV state they were the same one. The user confirms grouping.
- **Anonymise runs no discovery method and reaches no model.** `engine.Run` has
  no LLM slot: every discovery method runs at Identify time and its findings
  are Suggestions the user accepts. The only Values a run can apply are the
  ones the user accepted on Identify or DECLARED while reviewing the result on
  Anonymise, from the Compare pane selection ("Make it a spelling of an
  existing Value" or "Add it as a new Value") or the "Add missed Value" card. A
  declaration is the user acting, so it passes the review gate by definition:
  the gate exists to stop an unreviewed MACHINE finding reaching the text, not
  to stop the person reviewing the result from fixing what the machine missed.
  A Value declared on Anonymise is a first-class Value: it reaches the
  registry, the report, the Replaced values table, the mapping and the session
  file exactly like one accepted on Identify.
  `TestAnonymiseNeverCallsOllama` asserts the model call count is zero.
- **An intersection is a warning, never a refusal.** When two methods claim the
  same text the precedence rule always has an answer, so refusing the run
  would punish the user for a configuration the engine can resolve.
  `engine.DetectIntersections` answers "what covers what" BEFORE a run, using
  the same producers and the same comparator the pipeline uses, so the warning
  cannot describe a decision the run did not make. It surfaces on the value's
  own card on step 2. The step 2 to 3 gate is untouched: it exists for
  unreviewed suggestions, not for warnings.
- **One value, one placeholder: the shorter form is the main value.** When
  detection finds both "Coca-Cola" and "Coca-Cola company" they are ONE value
  with two spellings. The entity pass matches variants longest first, so with
  the shorter form as the main value the whole phrase collapses into one
  placeholder; left as two values the shorter fires inside the longer, the
  text reads `[BRAND_1] company`, the legal form leaks and two numbers are
  spent on one company. `engine.FoldValueFamilies` folds detection's whole
  output once, across every route; `state.js foldIntoFamily` does the same for
  values added one at a time. Both fold only WITHIN one category (a person
  "Delta" and an organisation "Delta Industries" are an intersection, not a
  family), only at word boundaries ("Alten" is not a spelling of
  "Altenberg"), and never below `minSpellingLen`.
- **Country association:** the country-specific built-in pattern
  categories are scoped by the DOCUMENT COUNTRY, owned by the engine
  (`backend/engine/country.go`, `CategoryCountries`, `CategoryAppliesTo`) and
  mirrored by `frontend/countries.js` exactly as `frontend/state.js PRESETS`
  mirrors `engine.AllPresets`. `Settings.Country` and `PipelineInput.Country`
  carry it;
  a category outside the selected country renders DISABLED rather than hidden,
  because an absent switch reads as "unsupported" rather than "not applicable
  here".
- **Removing a Value:** any Value can be removed after the run, from the
  Anonymise step's table. Removal is ONE action with three effects that cannot
  happen separately: the registry entry is forgotten, the Value and its
  spellings are recorded as a SESSION EXCLUSION, and the `Value` behind it (if
  any) is dropped. The exclusion is the whole mechanism for something a built-in
  pattern matched, which has no `Value` at all. Exclusions live on the App
  (`App.removed`) and in the session file, deliberately SEPARATE from the
  allowlist in state, so "undo the removal" is not the same gesture as "delete
  an allowlist term"; they are ENFORCED through the allowlist
  (`App.allowlistFor`, `engine.ApplyRemovals`), because `Allowlist.Contains` is
  the single veto every span producer already consults. Removing a value does
  NOT free its number: an export, a mapping CSV or a session file in which
  `[PERSON_4]` means one person may already have left the machine. Restoring a
  Value brings it back with a NEW number, for the same reason.
  A session exclusion is the ONLY negative rule in the model, and it is
  VISIBLE: it has its own list on the Anonymise step, with a restore action.
- **A Value's spellings are its chips, and nothing else.** Spellings are derived
  automatically until the user edits them; from that moment the list is theirs
  and the engine stops re-deriving it (`Value.SpellingPolicy`, `"automatic"` or
  `"curated"`, and an absent value reading as automatic). Deleting, renaming or
  moving a spelling CURATES the Value, so the deletion sticks without a negative
  rule. The alternative, a per-Value list of spellings the derivation must
  suppress, is a rule with no home in the interface: invisible except as the
  absence of a chip, unlisted anywhere, impossible to undo, and doing the job of
  the never-anonymise list, which is the one place a negative rule is meant to
  live and be visible. `value_shape_test.go` fails the build if the field
  returns, on either side of the bridge, and it also asserts every field the
  current shape DOES carry, so a Value that lost one is caught too.
- **Presets are SCOPED DATA, and the per-category selection stays the
  authority.** The pipeline obeys `engine.CategorySelection` and reads no preset
  at run time: a preset is a shortcut that WRITES that map. That is what makes a
  preset family addable without a rewrite.

  A preset is a ROW IN A TABLE (`backend/engine/presets.go`, mirrored by
  `frontend/state.js PRESETS` and guarded by `preset_parity_test.go`), carrying an
  ID, a family, a scope, a label and the categories it switches on. Four rules
  carry the model, and each one is why a piece of it is shaped as it is.

  **1. SCOPE is what keeps the rail's sections separate.** Applying a preset
  writes only the categories in its own scope (`patterns`, the built-in pattern
  categories; `names`, the name categories a discovery method can emit) and leaves
  every category outside it exactly as it was. A chip under Built-in patterns can
  no longer reach a checkbox under Heuristic discovery. A `Categories` entry
  outside its own scope is a defect the tests fail the build on, not a key
  `ApplyPreset` ignores in silence.

  **2. A FAMILY is a chip row.** Each section renders one row per family that has
  entries in that section's scope, so adding the intended regulatory family adds a
  row and introduces no new concept in the rail.

  **3. A preset spanning both mechanisms is declared TWICE, once per scope,**
  sharing an ID and a label. Sharing the ID across scopes is deliberate: it is
  what lets the two rows be recognised as the same regulation while each instance
  fills only its own section's categories.

  **4. There is NO preset algebra.** A chip is a write, not a layer. Each row
  derives independently whether the current selection still matches one of its
  presets (`engine.MatchingPreset`, `state.js activePreset`) and reads as Custom
  when it does not. Layering two families would need conflict rules for a category
  two presets disagree about, and would make the active chip unrecoverable from
  the selection, so the rail could no longer tell the user which preset they are
  on. A regulation that needs to say "off" as well as "on" is a new order, and it
  has to answer the reverse-derivation question first.

  The DEPTH family is the only one today, and its three presets are what the rail
  offers per section:

  | Scope | Soft | Standard (default) | Thorough |
  |---|---|---|---|
  | patterns | hard PII (emails, phones, IBANs, BICs, national IDs, VAT numbers, URLs with credentials, cards, network and credential shapes) | the same set | plus amounts, dates, street addresses and postal codes |
  | names | entity, project and identifier names | plus person, product and brand names | plus other, country, nationality and business-sector names |

  Soft and Standard being IDENTICAL in the patterns scope is a fact about the
  depths rather than a bug: Standard differs from Soft only in the name
  categories. Where SEVERAL presets match one selection the DEFAULT depth wins and
  table order breaks any remaining tie, so the patterns row reads Standard instead
  of flickering between two chips that both match, and a fresh session's row names
  the depth the session actually started on rather than the first row in the
  table. `custom_patterns` is in NO preset, because it has no switch and is
  permanently on.

  Which preset each row is on is DERIVED from the selection on both sides, never
  stored beside it, for the reason `SignalSourceEnabled` is derived: a summary
  that can disagree with the set it summarises lies about what a run does. It is
  PERSISTED, as `Settings.Presets` and `SessionSettings.Presets`, keyed
  `"<scope>.<family>"` and flat rather than nested so a family added later needs
  no schema change; a row matching no preset stores NO KEY, which is how Custom is
  representable. The run report names the presets its own selection matched
  (`engine.MatchingPresets`), so it cannot claim a preset the run did not use.

  The national identifier categories (`engine.CountryIDCategories`, mirrored by
  `frontend/countries.js COUNTRY_ID_CATEGORIES`) are masked by the document
  country on both sides of that derivation, because every depth preset names all
  four and each exists in one country: without the mask every document ever loaded
  would read as Custom.
- **Pipeline passes (fixed order):**
  1. Built-in pattern matching (`backend/engine/pii.go`).

     **A checksum failure lowers confidence; it never vetoes a span.** Where a
     checksum IS the recognizer (Luhn over a bare digit run) it must veto, or
     every long digit run becomes a card, and that is `piiPattern.validate`.
     Where the pattern already stands on its own shape (an IBAN's country code,
     check digits and grouped BBAN) a failure only scores the span
     (`piiPattern.checksum`, `ConfidenceChecksumFailed`), because "the checksum
     failed" is not evidence that the string is not an account number, only that
     it might be a bad one, and a mistyped, partly-redacted or synthetic bank
     identifier is exactly what a template document contains. Failing closed left
     the IBAN's country and check digits in clear text and let the credit-card
     recognizer claim its 16-digit interior, so the mapping asserted the document
     held a card that never existed (the card rule's own `reject` guard is what
     holds that boundary now, independently of the checksum policy).

     **The user's lever over it is one checkbox, off by default:** "Only replace
     when the checksum matches" (`Settings.RequireChecksum`,
     `engine.RejectFailedChecksums`), inside Built-in patterns, which drops those
     matches and NOTHING else. Off is the shipped default and reproduces the
     behaviour above exactly. It is a boolean and not a threshold because the
     question has two answers, and because the percentage it replaced was doing a
     second, wrong thing: above roughly 0.8 it dropped Values the user had already
     ACCEPTED, by the score of whatever originally found them, which contradicts
     the review gate and is invisible when it happens. `Confidence` is therefore
     DATA and not a filter: it orders overlaps, it feeds the heuristic pass's own
     floor before a Suggestion is shown, and it is reported. The switch reaches
     pass 1's spans alone, applied in `detectText` before the Value and
     custom-pattern spans exist, so no accepted Value can be dropped by a
     confidence comparison anywhere in the pipeline.
  2. Value pass: the accepted Values, expanded into their spellings (initials,
     surname-only, first-name-only, hyphen/space), longest-match-first
     (`backend/engine/values.go`). Derivation stops the moment the spelling
     policy goes `curated`: see the spelling-policy rule above.
  3. Post-pass: registry re-application across ALL loaded documents so the same
     real-world subject maps to the same placeholder everywhere.

  **The matches are PREVIEWED on Identify, read-only.** A direct match needs no
  review gate, which left the one thing the user does decide about pass 1
  (which signal categories are on) uncheckable until the whole batch had been
  anonymised: ticking "street addresses" changed nothing anybody could see.
  `engine.PreviewPatternMatches` answers it with pass 1's OWN detector through
  pass 1's own gates (the allowlist first, so a session exclusion suppresses a
  previewed match exactly as it suppresses a replaced one, then the checksum
  switch, then overlap resolution) over the same regions pass 1 reads, so the
  preview cannot promise a match the run does not make. `RunDetection` reports it
  beside the Suggestions (`patternMatches`, `patternCategories`,
  `builtInPatternsOn`) and it runs even when every DISCOVERY route is off, because
  the question is complete in itself. It is a READ and nothing else: it produces
  no Suggestion, mints no placeholder, is never an input to `engine.Run` (the run
  detects again for itself), and does not touch the Identify to Anonymise gate,
  which exists for unreviewed suggestions. `ActivePatternCategories` travels with
  it so the tab can say "that category never ran" rather than "it found nothing":
  a category switched off, or outside the document country, is a fact the user can
  act on and an empty result is not.

  No discovery method runs here. Discovery happens at Identify time
  (`App.RunDetection`), every finding is a Suggestion, and every local model finding
  passes a **hallucination filter** (dropped unless the exact string occurs in
  the source text) and the allowlist before the user ever sees it.
- **Image anonymisation: the second half of the Anonymise step.** The pipeline
  replaces TEXT and never touches a picture, and `exportfmt.rewriteZip` copies
  every archive entry it has no rewriter for byte for byte. So a picture leaves
  the machine exactly as it arrived unless the user decides otherwise, and step 3
  carries two tabs for that reason: **TEXT** (the Compare card) and **IMAGE**
  (every picture in the selected document, with one decision each).

  Where the review is offered is FIXED, and the table is not to be widened:

  | Format | Image review | Why |
  |---|---|---|
  | `.pptx` | full | pictures are DrawingML blips in the slides, layouts and masters, with their bytes in `ppt/media/*` |
  | `.docx` | full | pictures are `w:drawing`, or the legacy `w:pict` Word still writes, with their bytes in `word/media/*` |
  | `.pdf` | not offered yet, one explanatory line | the in-place PDF export keeps the original file with only the text replaced, so its pictures pass through EXACTLY as they arrived, and the explanatory line says so with the .md export as the way out for a picture that must not leave |
  | `.xlsx` | not offered | the owner's decision: a spreadsheet's pictures are not worth the complexity |
  | `.csv` `.txt` `.md` | not offered | there are no pictures in them |

  Four words carry the model, and keeping them apart is what makes the review one
  question per picture:

  | Term | Definition |
  |---|---|
  | **Image asset** | one picture FILE inside the archive (`ppt/media/image3.png`). It carries the bytes, and it is what a decision attaches to |
  | **Image occurrence** | one PLACE that asset is used. A logo on five slides is one asset with five occurrences |
  | **Image treatment** | what happens to the asset on export: `keep`, `box`, `blur` or `remove` |
  | **Image status** | what the review filters on: **Kept** (`keep`) or **Anonymised** (any of the other three). There is no third status, because every asset starts `keep` and nothing is ever "undecided" |

  **One decision per ASSET, applied everywhere it appears**, with a visible
  "appears in N places" note. Per-occurrence decisions would need the exporter to
  clone picture parts and rewrite relationships, which is the riskiest code this
  feature could hold, to answer a question a user reviewing "the logo" is not
  asking. The identifiers live in `backend/engine/imaging`
  (`AllTreatments`, `AllFormats`, `AllKinds`), are mirrored by `frontend/state.js`
  and are guarded by `image_parity_test.go`, exactly as the categories are.

  Three invariants, and each one is why a piece of the feature is shaped the way
  it is:

  1. **The original pixels always leave the archive.** All three anonymising
     treatments OVERWRITE the asset's bytes in the produced file, and a `remove`
     deletes the drawing element as well. A remove that deleted the element and
     left `ppt/media/image3.png` in the zip would be a leak that LOOKS like a
     redaction, which is worse than no feature.
     `TestExportedArchiveKeepsNoOriginalBytes` is the permanent guard, and it
     checks every entry of the produced archive rather than the media part alone.
  2. **Blur destroys information rather than hiding it.** A Gaussian blur is
     partly invertible and a light one over text is simply readable, so the
     implementation is mosaic then smooth: the samples are thrown away.
  3. **A control that does not anonymise is never labelled "anonymise."** That is
     why an SVG has no blur: a blur filter over a vector leaves every original
     shape and every original text string in the file. An SVG asset offers the box
     and the remove, and the blur renders disabled with the reason.

  The decisions are export-time state, not a pipeline pass: `engine.Run` knows
  nothing about pictures and must keep knowing nothing, so the run REPORT's
  picture section and the export screen's count are both composed by the App
  (`backend/app_images.go`) from the decision store and the cached inventories.
  A decision does NOT gate the step 3 to step 4 move: the gate exists for
  unreviewed suggestions, and every picture starts with an answer.
- **The local model reads a document in slices aligned to its OWN units, never in
  one request.** `engine.ScanChunks` packs contiguous units (slides, pages, rows,
  lines: the same units `Document.PageCount` addresses) up to the size the user's
  detail level asks for, and a slice never spans a gap in a discontiguous scope.
  The engine owns that division because the engine is what knows what a unit is;
  the Ollama client owns only what a request costs, and its
  `PromptBudgetBytes()` survives as an absolute CEILING for one request rather
  than as the sizing rule. What fits the context window and what a model can
  still extract names from are different questions: one request carrying a whole
  document fits an 8k window comfortably and measured ZERO values on every model
  tried, while the same document one slide per request measured dozens. A
  document needing many requests is scanned with a warning about the time, never
  refused, because the user asked for the scan and can cancel it; the only
  document skipped is one with no scannable text at all.
- **Placeholders:** stable per session, format `[CATEGORY_N]` (e.g.
  `[ENTITY_1]`, `[PERSON_3]`, `[EMAIL_2]`). The registry maps original →
  placeholder and is exportable as a re-identification key (CSV/JSON).
- **Allowlist wins:** an allowlisted term is never replaced, by any pass. It is
  also the single veto the session exclusions are enforced through, so there is
  exactly one place a producer has to consult.

  **A DEFINED TERM is enforced the same way, and it is visible.** A contract
  declares its own vocabulary: a phrase introduced as `"Work Order" means ...` or
  `(the "Dedicated Advisors")` is the document telling you the phrase is part of
  its machinery and definitionally not a client identity. That is the strongest
  "do not anonymise" signal a document can offer, and on the measured fixture
  nineteen such phrases were the largest single class of false positives in the
  review list. `engine.DiscoverDefinedTerms` reads them at detection time,
  `ApplyDefinedTerms` folds them into the allowlist, and they LIVE IN THEIR OWN
  LIST on the App and in the session file, deliberately separate from the user's
  terms, exactly as the session exclusions are: deleting a term the user typed is
  not the same gesture as dropping a definition the application read out of a
  file. They are SHOWN on the never-anonymise tab with the idiom that introduced
  each one and a per-entry remove, because a suppression the user cannot see is
  one they cannot lift.

  Two bounds are load-bearing. The suppression matches a WHOLE term, because a
  prefix rule removed "Services NStar" (`Services` is a defined term and
  `Services NStar` contains a real entity). And the parenthetical idiom REQUIRES
  an article, because that is what separates a definition from an ordinary aside:
  without it a document writing `("Contoso")` would suppress its own party name.
  A term's plural and possessive are suppressed with it, since a document that
  defines "Work Order" writes "Work Orders" in the same breath.
- **The Identify to Anonymise gate:** the wizard cannot reach
  Anonymise while a detection suggestion is still unreviewed. Detection
  produces suggestions, not decisions, and walking past one silently answers
  "reject" on the user's behalf. The rule lives in `state.js canGoTo` once, so
  the step bar and all four footers inherit it, and the Identify footer's hint
  is the refusal itself, naming the bulk "Reject all shown" so the gate is
  never a dead end.
- **Value categories:** eleven, listed in `engine.AllValueCategories` and
  mirrored by `frontend/state.js`. Every one is reachable by manual declaration
  and by the local model; several are additionally reachable OFFLINE, by heuristic
  or signal-based discovery, and the frontend label of the rest says where they
  come from, enforced by `identifyrail.test.js`.

  | Identifier | Placeholder | Also found offline by |
  |---|---|---|
  | `entity_names` | `ENTITY` | legal-suffix runs, the name half of a comma-separated legal name, country-scoped org keywords, email domains and website domains (signal-based discovery) |
  | `project_names` | `PROJECT` | codes beside a project cue |
  | `product_names` | `PRODUCT` | a trademark mark, or a product head noun |
  | `brand_names` | `BRAND` | nothing: a brand is world knowledge |
  | `person_names` | `PERSON` | title cues, multi-word runs, email local parts (signal-based discovery) |
  | `identifier_names` | `ID` | reference and contract codes |
  | `country_names` | `COUNTRY` | nothing: a gazetteer would fire in running prose |
  | `nationality_names` | `NATIONALITY` | nothing: it is an ordinary adjective |
  | `business_sector_names` | `BUSINESS_SECTOR` | nothing: it is an ordinary noun |
  | `other_names` | `OTHER` | nothing: it is defined by exclusion |
  | `custom_patterns` | `CUSTOM` | the user's own regexes |

  `entity_names` covers named organisations, companies, teams and internal
  systems. A human being is always `person_names`, which is why `entity_names`
  gets organisation-style spelling derivation and NOT the person-style one
  (initials, surname-only): deriving "Industries" from "Delta Industries" would
  replace an ordinary noun everywhere. `identifier_names`, `other_names` and the
  three CONTEXT categories are LITERAL (`engine.literalOnlyCategories`): a code
  has no name structure, and stripping a token that resembles a legal suffix off
  one would invent a spelling matching a different code.

  The three context categories exist because a missing category is worse than an
  empty one. In a two-party contract between two entities of one country the
  JURISDICTION is part of the identity, and a nationality or a line of business
  identifies in combination with the rest; without their own categories the user
  files them under `other_names` and the mapping CSV loses the distinction. None
  of the three earns a pattern or a gazetteer: "Française" and "Transport" are
  ordinary French words that happen to sit inside a legal name, and a rule for
  them would fire on running prose constantly.

  **A legal name separated by a comma is found from both sides.** `Name, Société
  anonyme` / `Name, S.A.` / `Name, Sàrl` is the standard continental legal-name
  form and the dominant one in French and Luxembourg drafting: the owner's
  market. A comma always terminates a capitalised run, so what survived was
  either the legal form with no name in front of it (worthless: the name is the
  only part worth replacing) or nothing at all, which made both parties of a
  measured contract invisible offline. `discover.go` reaches back over ONE comma
  when the run after it is a legal-form phrase, and forward over one when a
  dotted form follows (a single dotted letter never joins a run, so there is no
  run to reach back from). It then emits the NAME HALF as its own Suggestion
  beside the full legal name, because the short form is what recurs through the
  document; family folding makes the short one the main text and the full legal
  name its spelling, which is the one-value-one-placeholder rule.

  **A job title is not the person beside it, and a heading is not a name.** A
  closed list of role words terminates a person run rather than joining it
  (`engine.smartRoleTerminators`, deliberately DISJOINT from the organisation
  keywords: "Partners" names a firm, "Partner" names a job), a run of three or
  more underscores is a signature rule and not a sentence boundary, a name run
  never begins with a conjunction, and ALL-CAPS text carrying a function word is
  heading furniture. All caps ALONE is never the rule: a signature block writes
  real people in capitals, and those are the values the review list exists to
  surface.
  The code detector (`backend/engine/codes.go`) requires a separator between
  the letters and the digits. That is what keeps it out of pass 1's territory,
  which owns tax and VAT numbers, and `TestCodeDetectorDoesNotOverlapPassOne`
  holds the boundary.
- **Engine identifiers are stable, user-visible labels are not:** the wizard has
  **four** steps, and both their tokens and their visible labels are:
  1 **Import**, 2 **Identify**, 3 **Anonymise**, 4 **Export**. Identify owns two
  halves: the Configure choices are its left rail and the Values, Suggestions,
  never-anonymise list, built-in pattern matches and custom patterns are its
  Review workspace.

  The rail lists the DETECTION ROUTES as switchable sections, **one switch per
  mechanism**, each switch bound to a REAL settings flag: **Built-in patterns**
  (`useBuiltInPatterns`), THE ONE ROUTE ON BY DEFAULT and owning its own scope
  (document country, its own preset rows, the eight pattern category groups);
  **Heuristic discovery** (`useHeuristicDiscovery`), off by default and owning the
  name categories, its own preset rows and its own strictness block; and **Local
  LLM discovery** (`useLocalLLM`), off by default. Detecting Ollama ENABLES that
  switch, it never flips it. There is no cloud route.

  Built-in patterns is the default because it produces DIRECT MATCHES: a fresh
  session's first run shows what the deterministic patterns found and asks the
  user nothing. Both DISCOVERY routes produce SUGGESTIONS, which is a review list
  to work through one row at a time, so both are opted into rather than opted out
  of. The signal readings are unaffected by that: they hang off the pattern
  categories and are gated only by `signalSuggestionSources`, so a fresh session
  still derives Suggestions from an email domain.

  EVERY section of the rail, and the Profile panel with them, starts FOLDED, so
  the rail opens as a short column of route headers: the name, its help icon and
  its switch. Which routes are on is what a session starts by reading, and folding
  puts that within reach of the head's Run detection button instead of below three
  expanded panels of settings.

  The rail's HEAD carries that button (`views/detectionrun.js`), and no read-out.
  A count of active categories stated a number nobody acts on, in the place the
  user looks for something to press. The run control lives in a module of its own
  because the button is drawn in the rail and the findings land in the Review
  workspace, so neither half can own it: `views/identify.js` is the only module
  that knows about both and it does the wiring.

  The Review workspace is NOT ON SCREEN until a detection run has settled
  (`detectionRan`, reset with the rest of the step). Before that it is four empty
  tabs and a footer refusing to continue, which reads as a broken screen rather
  than as "nothing has looked yet"; while it is hidden the step footer is a
  standalone bar under the rail. The footer's sentence is the review gate's
  REFUSAL and nothing else: a count of accepted values narrated the list the user
  is already looking at, and it was all the footer ever said while the gate was
  open, so the honest empty case read as a problem.

  A section switch must be the flag it claims to be. A section whose state is
  derived from several methods can read "On" while nothing it names runs, and the
  user has no way to tell, which is why no fourth summarising boolean exists on
  either side of the bridge.

  Below the routes sits ONE switch-less panel, marked `.rail-panel` rather than
  `.rail-section` so a utility panel is never counted as a route: **Profile**,
  which offers LOAD alone. Saving a profile lives on the Anonymise step instead,
  because a profile carries the placeholder registry: only a run mints one, and
  moving back from Anonymise discards it, so a Save on Identify is a control that
  can never be used. Both screens render the one shared block
  (`frontend/views/profile.js`), so the gate on Save cannot differ between them.
  There is no cross-route quality panel, because there is no
  cross-route control left to hold: the one genuine question the confidence
  percentage was asking is the checksum checkbox, and that lives on Built-in
  patterns, which owns the check digits it is about. The only confidence control
  in the interface is Heuristic discovery's own minimum, inside that section
  because nothing else reads it and because it governs which Suggestions are
  SHOWN rather than what a run replaces.

  Signal-based discovery has no section of its own: its readings hang off the
  category row of the pattern that produces the evidence, inside Built-in
  patterns. Switching Built-in patterns off must NOT disable those drill-downs.
  Signal-based discovery is gated only by `signalSuggestionSources` and matches
  its own evidence; `UseBuiltInPatterns` governs only whether the signal itself is
  replaced. That asymmetry is the whole reason the separate setting exists, and a
  wiring test holds it.

  Within Built-in patterns the categories are grouped by CLASS, in eight groups,
  broadest first and the contextual one last: contact details; locations and
  addresses; financial accounts; government and tax identifiers; health
  identifiers; network and device identifiers; credentials and secrets; dates and
  monetary amounts. The classes are the ones the established PII tools converge
  on, plus two deliberate departures: health identifiers are their own group
  because health data is an Article 9 special category under the GDPR in this
  market, and dates and monetary amounts are their own because this application
  treats them as contextual identifiers rather than as PII. Grouping by class
  rather than by depth is what gives a new recognizer an obvious home.
  Every pattern category appears in exactly one group, guarded by
  `frontend/identifyrail.test.js`.

  `custom_patterns` has **no rail switch at all**. It is declarative, its editor
  is the workspace's Custom patterns tab, and it is therefore permanently on
  (`state.js ALWAYS_ON_CATEGORIES`, forced by `adoptCategories` on every adopted
  category map and refused by `toggleCategory`): a category with no control and a
  stored `false` is a pattern editor whose patterns never run, with nothing on
  screen saying why.

  Each section renders the preset rows of its OWN scope, and a chip writes only
  that section's categories. There is deliberately no read-out saying what a chip
  "also" switched on somewhere else: a chip cannot reach another section, so there
  is nothing to disclose. `frontend/copy.js RAIL.presetFamilyLabel` titles one
  row per family, and the chip LABELS come from the mirrored table, so a preset
  added to the engine appears in the rail with no second list to keep in step.

  The Configure panel keeps VISIBLE LABELS short and puts every explanation in a
  help tooltip. A paragraph under each control is read once and then occupies the
  panel forever, which is what put the controls at its foot out of reach. Only
  DYNAMIC information stays inline: validation errors, active counts, the live
  request estimate, Ollama availability, detection progress and status. Both the
  frontend suite and the rendering harness measure it, the harness in pixels.

  The category identifiers listed above, and the pattern category constants in
  `backend/engine/pii.go`, are NEVER renamed to follow a label change: a label is
  a display string, an identifier is a contract.
- **Sensitive state stays in memory** by default. Saving a session (registry
  + Values + settings + the removal list + the defined terms + the spent
  placeholder numbers + the
  image treatments) to disk is an explicit user action with a warning that the
  file contains the re-identification key. `SessionVersion` is **13**; a file of
  any other version
  is refused, never migrated, and the reasons for each bump are recorded beside
  the constant in `backend/engine/session.go`. There is no migration table and no
  compatibility alias anywhere in the loader: a session file holds the
  re-identification key, and a half-migrated one silently reassigns placeholders.

## 6. Coding rules

- Heavy comments everywhere; each file starts with a purpose header.
- **Comments explain intent, never change history.** Say what the code is for
  and why it is shaped that way. Do NOT write what it used to be, which phase
  changed it, or which change request asked for it: that is what `git log` and
  `docs/` are for, and in the code it decays into a story nobody can check. A
  comment naming a deleted function is a monument, not documentation; if the
  absence matters, assert it in a test. Where a past mistake explains a rule,
  state the RULE and the failure it prevents, in the present tense.
- Go standard library first. No new dependency without adding it to the
  BUILD.md dependency table AND the pinned-versions table below.
- **Testing: all conventions, tiers and commands are defined in
  `docs/TESTING.md`. Read it before writing or running any test.** It owns the
  three tiers (what a test requires and costs), the per-change scoping
  procedure, the "a change is not finished until its tests move with it" rule,
  the both-suites-gate rule, the load-bearing parity guards, the
  `backend/testdata/` fixture rules (English and French), and coverage.
- Frontend coding and typography rules live in `frontend/CLAUDE.md` (ES
  modules, no framework/build/CDN; Helvetica with Arial fallback, no Georgia,
  headings at regular weight; `--font-heading` in `brand.css` is the single
  heading-face declaration).
- All user-visible strings in English for v1 (UI i18n deferred to v2).
- Regexes are compiled once at package init and documented with examples of
  what they match and deliberately do not match.

## 7. Pinned versions

| Component | Version | Notes |
|---|---|---|
| Go | 1.26.x | toolchain in go.mod (pinned to 1.26.5); CI uses the floating 1.26.x. Moved off 1.23.x (now unsupported: Go only patches the two newest majors) to adopt Wails v2.13, which requires Go >= 1.25 |
| Wails | v2.13.x | v2 API only — do NOT use Wails v3 idioms. v2.13.0 requires Go >= 1.25 (its go.mod says `go 1.25.0`) |
| wails CLI (CI) | v2.13.x | pinned in ci.yml and release.yml — same row as the library: the CLI and go.mod versions are a coupled pair; CI must fail with an actionable message if they diverge |
| Ollama HTTP API | as of 2026: `GET /api/tags`, `POST /api/chat` with `"format"` carrying either `"json"` or a JSON **Schema** (per call, see §8), `"stream":false`, `"think":false` and `"keep_alive"` | probed at startup; if `/api/tags` succeeds but `/api/chat` returns 404 without a model-not-found body, show "Ollama too old, please update". `think` and `keep_alive` are TOP-LEVEL fields, never entries in `options`: Ollama's options object is a map, so a key it does not recognise there is dropped in silence |
| Minimum Ollama version | 0.32.0 | `"think":false` combined with `"format"` needs ollama/ollama PR #15901 (merged 7 July 2026, first shipped in 0.32.0). On an older build the pair can return unformatted text. No runtime version gate is added for it: the existing `ErrTooOld` path already covers the "Ollama too old" class of failure, and a second version check would be a second thing to keep true |
| Default Ollama model | `qwen3.5:0.8b` | Apache-2.0, ships as `Q8_0`. User-selectable from `/api/tags` results; the model name is a setting, never hardcoded outside settings defaults. Chosen on measurement: 5.3s and 16 names on the reference page against `qwen3.5:4b`'s 12.2s and 17, so the 4B costs 2.3x the latency for one extra name and busts the 20s target once the second model call is counted. The licence matters as much as the speed: Qwen2.5-3B, the previous pin, is under the Qwen **Research** licence rather than Apache-2.0 (it is the exception in its family), which is a compliance problem for a tool that reads client documents |
| Model tag quantisation | K-quant or `Q8_0`, never `-bf16` / `-f16` | BF16 has no fast CPU dot-product kernel without AVX512-BF16, so ggml converts every weight to FP32 inside the dot product and the model runs several times slower on the target laptop. The plain `qwen3.5:0.8b` (`Q8_0`) and `qwen3.5:4b` (`Q4_K_M`) tags are already correct; this rule exists to stop someone "upgrading" to a BF16 build for quality |
| Frontend | vanilla JS (ES2020), embedded via go:embed | no npm, no bundler |
| github.com/xuri/excelize/v2 | v2.9.x | XLSX reading; pure Go, MIT licence |
| github.com/ledongthuc/pdf | REMOVED (decommissioned 2026-08-22) | was the pure-Go PDF text extractor, then the deep tier's comparison baseline. Removed with the regenerated exporter once the in-place path's tests were confirmed: the production extraction is aspose-pdf-foss-for-go's layout mode, and G4's per-category floors (`exportfmt.referenceFloors`) replace the live comparison. Do NOT re-add it to answer an extraction question: a second parser in the module reads every document twice and gives the gate a reference that moves on its own |
| github.com/pdfcpu/pdfcpu | NOT ADDED (evaluated at BUILD-02 Phase 13, 2026-07-24) | in-place PDF rewriting was rejected (subset-font glyph availability), and the metadata role pdfcpu was evaluated for is covered by aspose-pdf-foss-for-go, which reads and rewrites the Info dictionary of the original file in place. The earlier Go-version incompatibility no longer applies under the Go 1.26 pin, but pdfcpu stays out for the functional reason above |
| github.com/go-pdf/fpdf | REMOVED (decommissioned 2026-08-22) | was the PDF writer behind the regenerated-PDF exporter. There is no regenerated PDF export: the PDF export is the in-place replacement, and its refusal names the .md export as the way out rather than falling back to a second writer. A second PDF writer kept for a rare failure path is unreviewed code in the leak-critical path, which is the reason not to re-add one |
| github.com/aspose-pdf-foss/aspose-pdf-foss-for-go | v0.7.0, pinned EXACTLY and **vendored**, with ONE PATCHED FILE | pure-Go PDF read/edit/write library behind the in-place PDF export (change-13). MIT; zero third-party dependencies (its go.mod is the module line and `go 1.24`). Pinned to the exact gate-verified version because the module is pre-1.0 and moving fast: **a version bump is never automatic** and re-runs the change-13b gate first (the boundary inventory in `pdf_boundary_test.go`, the save-semantics proof, the extraction counts). The library ships AI copilots that POST document text to a configured endpoint; they live in its `ai` subpackage, which is never imported, never vendored, and whose symbols `pdf_boundary_test.go` forbids repository-wide. Save discipline: `RemoveUnusedObjects()` before every `WriteTo`, because a naked `WriteTo` serialises orphaned pre-edit objects (measured at the 13b gate; both halves pinned by test). **`cmap.go` carries a LOCAL PATCH**, the one file in `vendor/` that is not upstream's: upstream reads a ToUnicode CMap line by line, but a CMap is a token program in which a newline is ordinary whitespace, so a producer may legally write the whole program on one line. Microsoft Print To PDF does, and against such a file upstream builds an EMPTY map and every glyph of a `/Type0 /Identity-H` font extracts as U+FFFD: a text layer with no letters in it, which is silent and which detection can only report as finding nothing. The patch scans tokens instead. It is pinned by `TestOneLineToUnicodeCMapIsDecoded`, which is what fails if `go mod vendor` re-copies the upstream file, and it is removed when upstream ships the fix. A version bump therefore checks this file first |
| Arimo, Tinos, Cousine, Carlito fonts (bundled inside aspose-pdf-foss-for-go, not a Go module) | as vendored at v0.7.0 | SIL OFL 1.1, metric-compatible substitutes the library redraws replaced text in. Data with a licence, like the Material Symbols and font8x8 rows, not code with a dependency |
| github.com/aspose-pdf/aspose-pdf-go-cpp | NOT ADDED (evaluated at change-13 planning, 2026-08-21) | the SAME VENDOR's other product: a wrapper over a proprietary native shared library. Rejected: commercial licence with an evaluation watermark and four-page limit until `SetLicense()`, per-platform native binaries beside the executable, and "no CGo compiler, but a native blob anyway" is the letter of the P0 rule without its purpose. `purego` was rejected with it: it would only exist to reach this product, and the FOSS module needs no FFI at all |
| golangci-lint (audit tool, `tools/go.mod`) | v2.12.2 | v2 config format (`version: "2"`) and v2 output flags (`--output.sarif.path`); the v1 flags do not exist |
| golang.org/x/tools (audit tool, `tools/go.mod`) | v0.49.0 | supplies `cmd/deadcode` |
| golang.org/x/vuln (audit tool, `tools/go.mod`) | v1.7.0 | supplies `cmd/govulncheck` |
| go-task (audit tool, `tools/go.mod`) | v3.52.0 | `Taskfile.yml` runner; `make` is unavailable on the target laptop |
| Material Symbols SVGs (assets, not a Go module) | snapshot at BUILD-02 Phase 1 | individual SVG files vendored into `frontend/assets/icons/`; Apache-2.0; licence text at `frontend/assets/icons/LICENSE` |
| font8x8 bitmap table (asset, not a Go module) | "basic" block, vendored as Go source in `backend/engine/imaging/font8x8.go` | PUBLIC DOMAIN (Daniel Hepper's font8x8, itself a transcription of the IBM PC 8x8 ROM font); ASCII 32 to 126. The standard library has no font rasteriser and the owner declined `golang.org/x/image`, so the letters drawn into a raster box treatment come from this table. It is data with a licence, like the Material Symbols row above, not code with a dependency. The visible cost is accepted: the raster box's text is blocky, and a character outside ASCII 32 to 126 is folded to its unaccented form where one exists and drawn as `?` where it does not. The SVG box does NOT read this table, because an SVG can name a real font family, so the two boxes' letterforms differ by design |

## 8. Validated constants

- Ollama base URL: `http://127.0.0.1:11434` (user-overridable port in
  settings; host is locked to loopback — do not "improve" this into a
  configurable remote host: it would break the local-only guarantee).
- The discovery and classification prompts must request STRICT JSON with the
  exact category keys from §5. **What `"format"` carries is decided PER CALL**,
  because the two calls do different work and the evidence points both ways.

  The **classification** call always carries the JSON **Schema**. Its input is a
  bounded list of names the model only has to file, so "every category present"
  is exactly the property that makes a re-filing complete, and the payload is
  small enough that the token cost is noise.

  The **discovery** call carries the schema when the user asks for it
  (`Settings.LLMStrictFormat`, off by default) and `"format":"json"` otherwise
  (`Client.discoveryFormat`). The default is the fast one on measurement: on a
  slide-heavy deck the schema cost about twice the wall clock for recall that was
  equal or slightly worse, and on a 0.8B model it returned nothing at all at every
  slice size tried, because seven REQUIRED arrays make a small model pad the
  categories it has nothing for. It stays available because it measured a real
  recall win on a short dense page of prose and on the repository's own
  sub-500-byte fixtures. Since loose JSON mode constrains no shape,
  `parseSuggestionJSON`'s tolerances (a missing key, a bare string for a list, a
  code fence) are load-bearing rather than belt-and-braces.

  The schema itself is unchanged by that choice: still DERIVED from
  `promptCategories` rather than written out, still making every category array
  REQUIRED, and still FLAT, with no `$defs` and no `$ref`, because sub-4B models
  echo a schema's own structure back in place of the extracted values.
  `backend/ollama/client_test.go` holds the four lists (each prompt's keys, the
  parser's keys, the schema's properties, the engine's categories) to each other,
  because a key in a prompt the parser does not know is dropped on parse, a key
  the parser knows that no prompt requests is a category the model is never asked
  to fill, and a category the schema omits is one the model is forbidden to fill:
  any of the three leaves the category dead and every test still passing.
- **`OLLAMA_IGPU_ENABLE=1` is a documented user recommendation, never a code
  constant.** Ollama ships Vulkan enabled and DETECTS an integrated GPU, then
  drops it (`dropping integrated GPU`) unless that variable is set for the
  SERVICE. The application cannot see or change it: it lives in the Ollama
  process's environment and `/api/tags` does not report it. So it is documented
  in `README.md` and in `frontend/docs/index.html` and nowhere in the code.

  Measured on the owner's laptop (Intel Arc 140V iGPU, Ollama 0.32.14, the 4B at
  `thorough` in JSON mode, through `backend/ollama/probe_live_test.go`): with the
  variable set the server reports the Arc as inference compute
  (`type=iGPU total="17.9 GiB"`) and `offloaded 33/33 layers to GPU`. The
  reference deck went from 2 m 09 s / 2 m 33 s on the CPU to 1 m 55 s warm
  (2 m 24 s cold), and the reference PDF from 53 s to 41 s / 47 s: about
  **1.2x**, not the 2.2x an earlier throwaway harness reported. What changed
  reliably is RECALL, not the clock: 156 values against 118 on the deck and 57
  against 54 on the PDF, with identical `eval_count` figures across repeats.
  Greedy decoding is deterministic per backend and not across backends, so the
  backend changing what a model returns is expected rather than a fault.

  Two consequences worth keeping: the copy must promise a modest improvement and
  not a transformation, and **no runtime check may infer this from timings**. The
  app cannot read the server's environment, so any such check would fire on a
  slow document and give the user a warning they cannot act on.
