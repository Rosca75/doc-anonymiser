# CHANGE-09 — Local AI recall: slice the document, not the context window

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0, pure Go + Wails v2, no CGo, no npm). It holds **one
self-contained implementation section per change request (CR1 to CR7)**,
followed by the **decisions taken**, a **conflict analysis**, the **recommended
execution sequence** and the **acceptance criteria**.

Every CR below comes from a measured investigation of a real failure on the
owner's own laptop: a 15-slide client deck, scanned with Local AI on and the
scope set to **"Entire document"**, reported **no values at all** on a document
full of names. The investigation began where CHANGE-08 left off (the request
settings) and ended somewhere else: the request settings are fine, **the amount
of text one request carries is not**. A whole document handed over in one call
returns nothing on every model measured; the same document, one slide per call,
returns dozens of values.

CR1 fixes the slicing, CR2 turns the remaining speed-versus-recall trade-off into
a user setting instead of a hidden constant, CR3 does the same for the reply
format, CR4 stops a silent or truncated model reading as a clean document, CR5
fixes the PPTX converter defect the investigation exposed, CR6 fixes a model
default that does not exist on a machine, and CR7 documents the Ollama
environment setting that is worth more than any code change in this order.

Ground rules for this change order (unchanged from `CLAUDE.md`):

- `CLAUDE.md` and the two subtree charters remain authoritative. Nothing here
  weakens the local-only guarantee, the one-file Ollama boundary, or the
  zero-CGo rule. No new dependency: every change is standard library.
- **A change is not finished until its tests move with it** (`CLAUDE.md` §6).
  Each CR names the tests to add, update and delete. Both suites gate:
  `go test ./...` and `node --test "frontend/**/*.test.js"`.
- User-visible copy never contains em dashes (`copy_guard_test.go`,
  `frontend/copy.test.js`). This order adds several user-visible strings on both
  sides of the bridge, so that guard will be exercised.
- The parity guards are load-bearing (`category_parity_test.go`,
  `detection_parity_test.go`, `value_shape_test.go`, `result_shape_test.go`,
  `dataset_parity_test.go`, and for this order especially
  `TestPromptsAndParserAgreeOnTheCategoryKeys`).
- Comments explain intent in the present tense. Do not write "used to be" or
  "CR1 changed this" into the code.
- **This order changes authoritative rules in `CLAUDE.md` itself**: §8's
  JSON-Schema constant becomes per-call (CR3), §8 gains the measured Ollama
  runtime recommendation (CR7), and §5's local-AI chunking description is
  replaced by unit-aligned slicing (CR1). Those edits are part of the CRs and are
  not optional: leaving them stale would make the charter describe a contract the
  code no longer honours.
- **No session-file shape change.** `SessionVersion` stays **8**. See decision 9
  for why the two new settings do not force a bump.

---

## 0. Cold-start context for the implementing session

Read this section first if you are picking this document up with no
conversation history. It is everything the diagnosing session established, and
it is all measured rather than reasoned.

### Where the work stands

| Fact | Value |
|---|---|
| Repository | `Rosca75/doc-anonymiser`, module path `doc-anonymiser` |
| Branch to develop and push on | a fresh branch off `main`, e.g. `claude/change-09-implementation-<suffix>` |
| `main`'s head | the merge commit that added this document, and nothing else. No code has changed yet. |
| Suites | `go test ./...` and `node --test "frontend/**/*.test.js"`, both must be green |
| Integration tier | `go test -tags=integration ./...` (the mock-Ollama detection flow lives here) |
| Deep tier | `go test -tags=deep ./...` |
| Task runner | `task test`, `task test:integration`, `task test:all`, `task audit` (go-task, no make) |
| Owner's Ollama | 0.32.14 on Windows |
| Owner's hardware | Intel Core Ultra 7 268V, 8 cores / 8 threads, 31.5 GB RAM, **Intel Arc 140V iGPU** |
| Owner's installed models | `Qwen3.5-0.8B-Q8_0:latest` and `Qwen3.5-4B-Q4_K_M:latest`, both Unsloth GGUF quantisations imported locally. **Neither is named `qwen3.5:0.8b`**, which is what §7 pins and what `DefaultModel` still is. |

### The owner's report, in substance

Local AI on, scope "Entire document", on
`LUCCS-TAT result final report-v1.01.pptx` (15 slides, 15,152 bytes once
converted to markdown): **no values found**. The offline Smart route, on the same
document, returns 29 suggestions in 4 milliseconds, so the document is plainly
not empty of candidates.

### The owner's latency targets

Stated during the investigation, and they are the acceptance bar for CR2:

| Scope | Acceptable | Absolute maximum |
|---|---|---|
| 1 to 5 pages / slides | 20 s | 30 s |
| An entire document the size of the two reference documents | about 1 min | to be discussed if exceeded |

### The reference documents

Both live outside the repository (real client and internal documents, never
committed). Ask the owner for the paths; they were:

- `LUCCS-TAT result final report-v1.01.pptx` — 15 slides, 15,152 bytes of
  converted markdown. The DENSE OOXML case: acronym tables, bullet fragments,
  very few full sentences. This is the document that fails today.
- `End_of_Claude_PoC_Jul2026.pdf` — 2 pages, 4,360 bytes. The email-thread PDF
  CHANGE-08's model and schema measurements were taken on, so it is the
  regression reference for any change to the discovery request.

**Use both.** They disagree, and a change measured on only one of them looks
like a win and is not.

### How to read the value counts in this document

Every "values" number below is the count of **raw strings the model returned that
occur verbatim in the source text** (that is, after the hallucination filter and
before `engine.MergeSuggestions` and `engine.FoldValueFamilies`). It therefore
overstates the number of distinct Values a user would review: it counts "Anne
Humbert" and "Anne Humbert (LU)" separately. It is used because it is comparable
across rows, every row being counted the same way. Do not quote these as Value
counts in code comments or in copy.

### What was established by measurement (do not re-derive)

A throwaway probe harness was written for this investigation (stdlib-only Go,
driving the real prompts through the real client against the real local Ollama).
It lives in the diagnosing session's scratchpad; **it is not part of this change
order** and does not need to be committed. Appendix C says how to rebuild it in
about twenty minutes if you need to re-measure.

**1. The request itself is correct.** `think:false` is accepted (HTTP 200 on the
owner's locally imported tags, which do report the `thinking` capability), the
schema is applied, the reply is valid JSON. Nothing CHANGE-08 landed has
regressed. The failure is not in the request settings.

**2. A whole document in one request returns nothing, on every model measured.**
This is the headline result and the reason for CR1.

| request shape | model | backend | wall clock | values |
|---|---|---|---|---:|
| whole deck, 1 request, schema | 0.8B | CPU | 23 s | **0** |
| whole deck, 1 request, json | 0.8B | CPU | 19 s | **0** |
| whole deck, 1 request, json | 4B | CPU | 2 m 13 s | **0** |
| whole deck, 1 request, schema | 4B | CPU | 5 m 31 s | **0** |
| whole deck, schema, app's own 512-token reply cap | 4B | CPU | 1 m 10 s | **0**, truncated |
| deck, one slide per request (15) | 4B | CPU | 3 m 59 s | **76** |
| deck, one slide per request (15) | 4B | GPU | 1 m 48 s | **80** |

The whole-document call fails in two different ways depending on the model, which
is why one bug produced two different symptoms:

- On the **0.8B** it returns a well-formed, completely empty object:
  `{"brand_names": [], "entity_names": [], ... "project_names": []}`. No error,
  nothing to report, and the run says "0 suggestions". This is what the owner saw.
- On the **4B** it starts answering, runs past `maxReplyTokens` (512) and is cut
  off mid-string, having degenerated into a repeat loop
  (`"ADA","ARHS","ADA","ARHS",...`). Ollama returns `done_reason: "length"` and
  `eval_count: 512`, and `parseSuggestionJSON` fails with "the model's reply was
  not the expected JSON object (unexpected end of JSON input)", so the run reports
  a per-file problem. `repeat_penalty` is pinned neutral (1.0) by CHANGE-08
  decision 4, correctly, so that names can be copied verbatim; the loop is a
  consequence of that plus greedy decoding plus a very large prompt, and the fix
  is the smaller prompt, not the penalty.

**3. Recall depends on slice size, and the cliff is around one kilobyte on a
small model.** Same document, same request settings, byte-sized chunks:

| chunk budget | chunks | 0.8B, schema | 0.8B, json |
|---|---:|---:|---:|
| 18,432 B (today's default) | 1 | 0 | 0 |
| 2,048 B | 12 | 0 | 0 |
| 1,024 B | 26 | 0 | **13** |
| 512 B | 50 | 0 | **21** |

A 120-byte prefix of the same text returns names; a 200-byte prefix already
returns none with the schema on. Note the last row: at 512 bytes only **4 of 50**
chunks produced anything, and those four carried all 21 values. "Most requests
return nothing" is therefore normal, and is not by itself evidence of a fault.

**4. `promptBudgetBytes` is the proximate cause.** It derives the per-chunk
budget from the context window: `ContextSize * 3 * 3 / 4`, which is
`8192 * 3 * 3/4 = 18,432` bytes (`backend/ollama/client.go:836`). The reference
deck is 15,152 bytes, so it becomes **one** request. That function answers "what
fits the model's context window", and the measurements above show that is a
different question from "what the model can still find names in". Sizing a slice
from the window is a defect, not a tuning choice.

**5. The JSON Schema is not the villain, but it is not free either.** It was
worth re-measuring because on the 0.8B it is the difference between "few values"
and "zero values". Across models, documents and backends:

| document | model | backend | schema | json |
|---|---|---|---:|---:|
| deck, per slide | 4B | GPU | 71 in 4 m 18 s | **80 in 1 m 48 s** |
| deck, per slide | 4B | CPU | 70 in 8 m 56 s | **76 in 3 m 59 s** |
| deck, 1 KB chunks | 0.8B | CPU | 0 | **13** |
| deck, 512 B chunks | 0.8B | CPU | 0 (50 of 50 silent) | **21** |
| PDF page 1 | 4B | CPU | **77** | 45 |
| PDF page 1 | 4B | GPU | 53 | 48 |
| PDF page 2 | 4B | CPU and GPU | 9 | 9 |
| PDF, whole (4,360 B) | 4B | GPU | 35 | 34 |
| PDF page 1 | 0.8B | CPU | 0 | 2 |
| the five repo fixtures (~400 B each) | 0.8B | CPU | 2, 2, 2, 2, 3 | 1, 2, 2, 1, 3 |

Read that table carefully, because it does not say one single thing:

- The schema's large recall win (77 against 45) appears in **one** row and does
  not reproduce on the same document with the same model on the other backend
  (53 against 48). Greedy decoding is deterministic per backend, not across
  backends, so that row is backend luck rather than a property of the grammar.
- On the tiny fixtures the schema is equal or slightly better (2 against 1,
  twice), which is consistent with CHANGE-08's original finding.
- The schema costs **2.2x to 2.4x the wall clock** on the deck, consistently,
  because it forces all seven arrays to be emitted whether or not they have
  content.
- On the 0.8B it is catastrophic at every slice size tested, from 120 bytes to
  18 KB.

That is a trade-off with no single right answer, which is why CR3 makes it a
setting rather than a constant, and why its default is the fast one.

**6. `OLLAMA_IGPU_ENABLE=1` is worth more than anything else in this order.** The
owner's laptop has an Intel Arc 140V iGPU. Ollama 0.32.14 ships Vulkan enabled
and **detects the GPU**, then drops it:

```
msg="dropping integrated GPU; to enable, set OLLAMA_IGPU_ENABLE=1"
     id=0 library=Vulkan name=Vulkan0 description="Intel(R) Arc(TM) Graphics"
```

With the variable set, the same server reports the Arc as inference compute
("type=iGPU total=17.9 GiB"), offloads `33/33 layers to GPU`, and the deck scan
goes from 3 m 59 s to **1 m 48 s (2.2x)** while finding slightly MORE (80 against
76). Every measurement in this document, and every measurement in CHANGE-08, was
otherwise taken on 8 CPU cores with a capable GPU sitting idle. The application
cannot detect or set this: it lives in the Ollama service's environment and
`/api/tags` does not report it. Hence CR7, which is documentation only.

**7. Concurrency is a substitute for the GPU, not an addition to it.** Measured
by sending several slices at once:

| configuration | sequential | concurrent (4) | speed-up |
|---|---|---|---:|
| 4 slides, CPU, `OLLAMA_NUM_PARALLEL=4` | 49 s | 30 s | 1.63x |
| 15 slides, GPU, `OLLAMA_NUM_PARALLEL=4` | 2 m 0 s | 1 m 46 s | 1.13x |
| 4 slides, CPU, server's default parallelism | 32 s | 32 s | 1.01x |

The third row matters most: with Ollama's default settings concurrent requests
are serialised, so a concurrent chunk loop in the application would do nothing at
all until the user also changed their server configuration. Combined with 1.13x
once the GPU is on, that is why parallelism is **rejected** in this order
(decision 8) rather than deferred as a nice-to-have.

### The four defects the investigation surfaced

All four were read out of the code, not guessed.

- **The slice size comes from the context window (CR1).** `promptBudgetBytes`
  (`backend/ollama/client.go:836`) sizes a chunk from `ContextSize`, and
  `Client.Chunks` (`:846`) cuts the document at arbitrary byte offsets with a
  512-byte overlap (`chunkText`, `:869`). Nothing consults the document's own
  units, even though the engine already models them (`Document.PageCount`,
  `PageRangeMarkdown`, `PagesMarkdown` in `backend/engine/pagescope.go`) and the
  page-scope feature is built on exactly that. The result is measurement 2:
  "Entire document" is one request, and one request over a whole document finds
  nothing.

- **A silent model is indistinguishable from a clean document (CR4).**
  `scanChunks` (`backend/ollama/client.go:533`) returns however many suggestions
  it parsed, and `runLocalAIPhase` (`backend/app_detect.go:485`) merges them.
  Neither counts how many requests produced **nothing**. When every request comes
  back empty, `detectionStatus` (`:566`) reports "scanned 1 file(s), 0
  suggestion(s)", which is the same sentence a genuinely name-free document
  produces. The user cannot tell "your model found nothing" from "there was
  nothing to find", and gets no hint that another model or a smaller slice would
  change the answer. Separately, `chatResponse` (`:329`) does not decode
  `done_reason`, so the truncation in measurement 2 surfaces as a JSON parse error
  rather than as the actionable "the reply was cut off" that it is.

- **The PPTX converter drops soft line breaks, and a title's overflow lines
  (CR5).** `walkShapes` (`backend/engine/convert/pptx.go:151`) handles `a:t`
  (`:224`) and `a:tbl` (`:243`) but has **no case for `a:br`**, so a paragraph
  built with soft breaks is concatenated with no separator at all. On the
  reference deck the entire slide-1 title is one `<a:p>` full of `<a:br/>`, and it
  converts to:

  ```
  ## Slide 1: LUCCS TAT result final reportAuthors:Vincent Gauché - PwCOscar Liber - PwCDate: 05/03/2021Version: 1.01
  ```

  `docx.go` already handles `w:br` (`backend/engine/convert/docx.go:378`), so this
  is an inconsistency between two converters rather than an unconsidered case. The
  damage is not cosmetic: on this deck heuristic discovery proposes
  `PwCOscar Liber` as a person today, and so does the local AI, because that is
  genuinely what the text says. A naive fix makes it worse: `flushShape` (`:167`)
  keeps only a title shape's **first line** and discards the rest, so emitting a
  real line break would silently drop the authors, the date and the version from
  the document instead of gluing them. Both halves have to change together.

- **The default model does not exist on the owner's machine (CR6).**
  `DefaultModel` is `qwen3.5:0.8b` (`backend/ollama/client.go:46`);
  `frontend/state.js:117` starts at `model: ""`; `ApplySettings`
  (`backend/app.go:590`) keeps `DefaultModel` when the setting is empty. The
  owner's tags are `Qwen3.5-0.8B-Q8_0:latest` and `Qwen3.5-4B-Q4_K_M:latest`, so a
  run before the model dropdown has ever been touched posts to a model Ollama does
  not have and gets "model is not installed". Worse, the `<select>` in
  `localAISection` (`frontend/views/identifyrail.js:880`) marks an option selected
  only when it equals `s.settings.model`, so with `model: ""` nothing is marked,
  the browser selects the **first** option, and `pushSettings` (`:1034`) sends
  that. Which model a fresh session actually uses is therefore decided by Ollama's
  tag ordering, which for the owner means the 4B at 2.3x the latency.

### Decisions the owner has already approved

1. **Model choice stays the end user's.** Do not pin a different default on the
   strength of these measurements, and do not add a code path that picks a model
   by document type. The owner's words: the user "knows that performance might be
   different between the two".
2. **The trade-off is exposed as parameters, with plain-language helpers.** Where
   measurement shows no single winner (slice size, reply format), the setting goes
   in the Configure rail with a help tooltip that says what it ACHIEVES for a
   regular business user, never what it does mechanically. That is the shape of
   CR2 and CR3 and it is the owner's explicit instruction.
3. **Large documents are scanned, with an honest time warning**, not skipped.
   Today a document over `MaxChunksPerDocument` is dropped from the AI phase with
   "too large for the local AI"; unit slicing makes that threshold reachable for
   ordinary decks, and refusing a scan the user asked for is worse than a slow
   scan they can cancel.
4. **The PPTX soft-break fix and the model-default fix are in scope** (the two the
   owner selected from three offered).
5. **Heuristic discovery's quality problem is OUT of scope.** On the reference
   deck the offline route files "Technical Acceptance Test", "Impact High" and
   "TTS AS" as `person_names` and misses PwC, CTIE and ADA. It is real, and it is
   its own change order.
6. **The GPU setting is documented, not automated** (CR7).
7. **Parallel requests are rejected for now** (decision 8).

### A contradiction this order must record, not hide

CHANGE-08 pinned `qwen3.5:0.8b` on a measurement of **16 names on page 1 of the
reference PDF with the schema on**. That measurement **did not reproduce**: on the
owner's installed 0.8B, the same page with the same request settings returns **0
with the schema and 2 without**, while the 4B on the same page returns 45 to 77.
Both installed models are Unsloth GGUF quantisations of `Qwen/Qwen3.5-0.8B` and
`Qwen/Qwen3.5-4B` (Apache-2.0, `Q8_0` and `Q4_K_M`, so §7's quantisation rule is
satisfied), imported locally rather than pulled from the Ollama library.

The most likely explanation is that the CHANGE-08 benchmark ran against a
different GGUF file for the same nominal model. This order does **not** change
§7's pin on that basis, because the alternative explanation (a poor local build)
is equally consistent with the evidence, and settling it needs a roughly 700 MB
pull the owner has not authorised. It is written into the acceptance criteria as a
spot-check instead. Do not quietly "correct" the §7 model row while implementing.

### Recommended session setup

- Model **Opus** (or the latest available), reasoning effort **high**. CR1 is the
  centre of gravity and touches the engine, the client and the bound app; CR2 and
  CR3 add settings that cross the bridge; CR4 touches the client and the app; CR5
  is an isolated converter fix; CR6 is small but crosses the bridge; CR7 is
  documentation.
- Do NOT split CR1 to CR4 across parallel sessions: they touch
  `backend/ollama/client.go`, `backend/app_detect.go` and the same integration
  test helpers.
- CR5 CAN be done by a parallel session: it touches only
  `backend/engine/convert/pptx.go`, one fixture builder and one expectation.
- Commit per CR, with the CR number in the message body but never in a code
  comment (`CLAUDE.md` §6).

### Paste-ready opening prompt for the fresh session

> Read `CLAUDE.md`, `backend/CLAUDE.md`, `frontend/CLAUDE.md`,
> `frontend/BRIDGE.md` and `docs/TESTING.md`, then `docs/CHANGE-09.md` in full.
> Branch off `main` as `claude/change-09-implementation-<suffix>`. Implement
> CHANGE-09 in the order its "Recommended order" section gives. For each CR:
> write or update the tests named in the CR in the SAME commit as the behaviour,
> then run `go test ./...`, `go test -tags=integration ./...` and
> `node --test "frontend/**/*.test.js"`. Note that CR1, CR3 and CR7 edit
> authoritative rules in `CLAUDE.md` itself (§5 and §8); those edits are required,
> not optional. The two reference documents named in section 0 are the acceptance
> bar: ask me for their paths and measure against BOTH before claiming a CR is
> done. If a decision in this plan turns out to be wrong once you are in the code,
> stop and tell me rather than inventing a third option.

---

## CR1 — Slice a document by its own units, never by the context window

The fix for the reported failure. Everything else in this order is either the
user's control over it (CR2, CR3), the honesty of its reporting (CR4), or a defect
it exposed (CR5, CR6).

### Current behaviour

`runLocalAIPhase` (`backend/app_detect.go:485`) builds one `scanUnit` (`:426`) per
document holding the WHOLE text to scan (`scopedUnits`, `:444`), asks
`Client.EstimateChunks` whether it is too large, and hands the text to
`DiscoverWithProgress` (`backend/ollama/client.go:523`). The client then does its
own division: `scanChunks` (`:533`) calls `Chunks` (`:846`), which calls
`chunkText(text, promptBudgetBytes(), chunkOverlapBytes)` (`:869`).

Three things follow, and all three are wrong:

1. The slice size is `ContextSize * 3 * 3 / 4` = 18,432 bytes at the default
   `num_ctx`, so any document under about 18 KB is ONE request. Measured result:
   zero values on both models (section 0, measurement 2).
2. The cut points are arbitrary byte offsets, softened by a 512-byte overlap. A
   slide's title can land in one request and its bullets in another.
3. A document over `MaxChunksPerDocument` (64) is not scanned at all: it is
   reported as `DetectionSkip` with "too large for the local AI".

The engine already knows how to divide a document properly. `Document.PageCount`
(`backend/engine/pagescope.go:32`) and `PageRangeMarkdown` (`:63`) model slides,
pages, rows and lines, and the whole page-scope feature is built on them. The AI
route uses them for the user's scope and then throws the knowledge away when it
slices.

### Change

**New file `backend/engine/aichunks.go`.** The engine owns how a document is
divided, because the engine is what knows what a unit is. It must not import
`ollama` and must not learn what a request costs.

- `type ScanChunk struct { Text string; FromUnit, ToUnit int }`. The unit numbers
  travel so progress can say "slide 7 of 15" in the words the import list already
  uses, rather than a chunk index that means nothing to a user.
- `func ScanChunks(d Document, units []int, level string, hardMaxBytes int) ([]ScanChunk, error)`:
  - `units` is a 1-based list of the document's own units as the user's scope
    selected them; nil or empty means every unit. The list may be discontiguous
    ("12,13,18-20"), and **a slice must never span a gap in it**: packing pages 13
    and 18 into one request would hand the model text the user excluded.
  - Pack CONTIGUOUS units until adding the next one would exceed the level's
    target size (CR2 owns the targets), then flush. A run that is still empty
    never overshoots, so a single unit larger than the target always gets its own
    request rather than being dropped.
  - `hardMaxBytes` is an absolute ceiling the caller derives from the context
    window. It is a backstop, not the sizing rule: it bites only when ONE unit is
    bigger than it (a dense PDF page, a complex XLSX sheet rendered as one JSON
    block), in which case that unit alone is split by bytes.
  - Validate every index against `PageCount` with the same actionable message
    style `PagesMarkdown` (`:114`) already uses.
- **Move the byte splitter into the engine** as the last-resort path above:
  `chunkText` and `chunkOverlapBytes` (`backend/ollama/client.go:869`, `:64`) move
  to `aichunks.go` as `SplitOversizedUnit(text string, maxBytes int) []string`,
  keeping the paragraph / line / space preference, the rune safety and the
  overlap. Its home is now the file that owns document division. Do not leave a
  copy behind in the client.

**`backend/ollama/client.go`:**

- Delete `Chunks` (`:846`), `EstimateChunks` (`:859`), `chunkText` (`:869`) and
  `chunkOverlapBytes` (`:64`). The client no longer divides documents.
- Export the ceiling the engine needs: `promptBudgetBytes` (`:836`) becomes
  `PromptBudgetBytes()`. Keep its derivation and its comment, and add one
  sentence saying what it is now FOR: the absolute ceiling for one request, not
  the sizing rule for a slice.
- Replace `Discover` (`:515`) and `DiscoverWithProgress` (`:523`) with one method
  that takes pre-built slices, since the boundaries are no longer the client's
  business:

  ```go
  // DiscoverSlices scans slices the ENGINE decided the boundaries of, one request
  // each, and reports what came back as well as what did not.
  func (c *Client) DiscoverSlices(ctx context.Context, slices []string, sourceText string,
      onSlice func(index, total int)) (DiscoveryOutcome, error)
  ```

  `DiscoveryOutcome` carries `Suggestions []engine.Suggestion`, `Requests int` and
  `Silent int` (requests that parsed cleanly and yielded nothing). CR4 consumes
  the last two. Keep everything else `scanChunks` does: `onSlice` fires BEFORE
  each request, `ctx` is honoured between requests, partial results survive a
  cancellation WITH the context error, per-request suggestions merge through
  `MergeSuggestions`, and the hallucination filter runs against `sourceText`, the
  whole scanned text, not the individual slice (`filterSuggestions`, `:570`).
- `MaxChunksPerDocument` (`:69`) stops being a refusal. Rename it to what it now
  means, `LargeScanRequests`, keep it at 64, and rewrite the comment: beyond this
  many requests a scan is worth WARNING about, because the user asked for it and
  refusing is worse than being slow (approved decision 3).

**`backend/app_detect.go`:**

- `scanUnit` (`:426`) stops carrying text. It becomes the document plus the unit
  indices the scope selected, and `scopedUnits` (`:444`) returns that instead of
  joined markdown. Keep its "problems rather than recording them" contract and its
  comment about being the ONE definition of what the scope selects: it is still
  the one definition, it just returns indices now.
- `runLocalAIPhase` (`:485`) builds slices with `engine.ScanChunks(doc, units,
  settings.AIDetailLevel, llm.PromptBudgetBytes())` and calls `DiscoverSlices`.
  Replace the oversize SKIP with the approved warning: when
  `len(slices) > ollama.LargeScanRequests`, append a message to `res.Errors`
  naming the request count and that it will take a while, and scan anyway.
- `DetectionSkip` (`:62`) keeps its type and its place in `DetectionResult`, but
  its only remaining producer is a document with no scannable text at all (an
  empty or whitespace-only markdown). That is a real case and it keeps the field,
  and the frontend that renders it, meaningful rather than dead.
- `DetectionProgress` (`:45`) gains `UnitFrom`, `UnitTo` and `UnitWord` so the
  caption can say "slides 4 to 6 of 15". `ChunkIndex` and `ChunkCount` keep their
  meaning (position inside one document's scan) and are now the request number and
  the request count. Update their comments to say so.
- `refineCategories` (`:275`) is unchanged in shape but note that `scopedUnits`
  now returns indices: it needs the TEXT of the scope to decide which suggestions
  the scope makes this run's business (`occursInScope`, `:341`). Build it from the
  same slices, so there is still one definition of the scoped text. Do not
  reintroduce a second one.

**`CLAUDE.md` §5 must change with this.** The "Pipeline passes" section and the
Local AI paragraphs describe handing documents to the model without saying how
they are divided; §5's `AIScope` discussion says scoping exists because a whole
document is "too much". Add the rule this CR establishes, in one short paragraph:
the local AI reads the document in slices aligned to its OWN units
(`engine.ScanChunks`), sized by the user's detail level and never by the context
window, because what fits the window and what a model can still extract from are
different questions, and one request over a whole document measured zero values on
every model tried.

### Tests

`backend/engine/aichunks_test.go` (NEW, unit tier), table-driven:

- A pptx-shaped document (slides) packs one slide per request at the thorough
  target and several at the faster one.
- A line-unit document (txt) packs MANY lines per request: the target is a size,
  not a unit count, or a 400-line text file would become 400 requests.
- A grid document (csv) keeps the header on every slice, which
  `PageRangeMarkdown` already does; assert it, because it is the reason a scoped
  row is intelligible to a model at all.
- A discontiguous `units` list never produces a slice spanning the gap.
- A single unit larger than `hardMaxBytes` is split, and the pieces carry the same
  `FromUnit`/`ToUnit`.
- Every slice is non-empty, and concatenating the slices of a full scan covers
  every unit exactly once (modulo the oversized-unit overlap).
- An out-of-range index returns the actionable error, not a panic.
- `SplitOversizedUnit` inherits the existing `chunkText` cases from
  `TestChunkText` at `backend/ollama/client_test.go:591` (empty, short, paragraph
  preference, overlap, rune safety, unbroken token). MOVE those tests; do not
  duplicate them.

`backend/ollama/client_test.go`:

- DELETE `TestChunkText` (`:591`) and `TestChunkCap` (`:644`), whose subject is
  gone. Their coverage moves to the engine.
- UPDATE every `Discover` call site (`:218`, `:254`, `:262`, `:278`, `:295`,
  `:549`, `:684`, `:726`) to `DiscoverSlices` with an explicit one-slice or
  two-slice input. This is mechanical but it is the largest test edit in the CR.
- ADD: `DiscoveryOutcome.Requests` equals the number of slices sent, and `Silent`
  counts the ones that parsed to nothing (CR4 asserts what the app does with it).
- KEEP the cancellation, partial-result, code-fence and hallucination-filter
  tests: none of those contracts change.

`backend/app_detect_integration_test.go`:

- ADD: a two-slide document scanned with Local AI on produces TWO model calls,
  not one. This is the regression guard for the whole order, so name it after the
  failure: a whole document must not be one request.
- ADD: the progress stream carries unit numbers, and `ChunkCount` equals the
  request count.
- ADD: a document whose request count exceeds `LargeScanRequests` is STILL
  scanned, and the run reports the warning. Assert it is not in `Skipped`.
- UPDATE `TestDetectionAIScopeLimitsToPageRange` (`:417`),
  `TestDetectionAIScopeOutOfRangeReportsButFinishes` (`:462`),
  `TestDetectionAIScopeDiscontiguousPages` (`:492`) and
  `TestClassifyRespectsTheAIScope` (`:538`): the scoped text now arrives as
  several requests, so assertions that inspect ONE captured prompt must gather
  them. `scopeChatServer` (`:383`) already records every prompt it sees, which is
  what makes this a small edit.
- KEEP `TestDetectionKeepsGoingWhenOneFileFails` (`:276`) and
  `TestDetectionCancellationIsHonestAboutIt` (`:327`): per-file and mid-scan
  behaviour is unchanged by design, and they are the guards that say so.

---

## CR2 — Detail level: the speed-versus-recall dial, in the user's hands

CR1 makes slicing correct. It does not decide how small a slice should be, and
the measurements say that question has no single answer: 512 bytes finds 21
values on the 0.8B in 2 m 23 s, 1 KB finds 13 in 1 m 20 s, and one slide per
request on the 4B finds 80 in 1 m 48 s on the GPU. A regular business user cannot
be asked about kilobytes, but they can be asked whether they want thorough or
quick, told what that costs, and shown what it did last time.

### Current behaviour

There is no control. The slice size is `promptBudgetBytes()`, derived from
`ContextSize`, which the rail exposes as "Context" (`RAIL.contextSize`,
`frontend/copy.js:323`) with a tooltip about how much the AI can read at once. So
the only lever a user has over slice size today is a setting about the model's
memory, which changes the slice size as a side effect. That is the opposite of
explaining a trade-off.

### Change

**`backend/engine/aichunks.go`** owns the levels, because they are the sizes
`ScanChunks` packs to:

```go
const (
    DetailThorough = "thorough"
    DetailFaster   = "faster"

    thoroughTargetBytes = 1200
    fasterTargetBytes   = 4000
)

// ScanTargetBytes is the target size of one request at the given level. An
// unknown or empty level reads as DetailThorough, so a payload that omits it
// lands on the safe end rather than the fast one: a level nobody chose must not
// be the one that silently finds less.
func ScanTargetBytes(level string) int
```

The two numbers come from the measurements: on the reference deck the recall
cliff on a small model sits between one and two kilobytes, so `thorough` targets
just under it, and `faster` is deliberately past it (the tooltip says so) because
on a larger model it is a real saving. Comment both with the measured reason, in
the present tense.

**Deliberately NOT offered: a "whole document in one request" level.** It measures
zero values on both models on both reference documents and truncates the reply on
the 4B. A setting whose measured outcome is "finds nothing" is a broken switch,
not a choice. Note this in the code comment beside the levels, so the next reader
does not add it back as an obvious third option.

**`backend/app.go`:**

- `Settings` (`:36`) gains `AIDetailLevel string` with the JSON tag
  `aiDetailLevel`, beside the other Local AI fields and documented as the user's
  speed / recall dial.
- `defaultSettings` (`:233`) sets it to `engine.DetailThorough`.
- `ApplySettings` (`:590`) validates it like `Country` and `Level` are validated:
  an unknown level is REFUSED with an actionable message naming the two valid
  ones. Follow the existing style ("unknown anonymisation level %q, expected
  soft, medium or advanced").

**`backend/engine/session.go`:** `SessionSettings` (`:63`) gains
`AIDetailLevel string` with the JSON tag `aiDetailLevel,omitempty`. **No `SessionVersion`
bump**: an absent field reads as the empty string, `ScanTargetBytes` reads that as
`thorough`, and thorough is the default a v8 file was written under. That is
exactly the "an added field the loader can ignore is not a bump" case the constant's
own comment describes. Do not bump it, and do not add a migration.

**New bound method, `backend/app_detect.go`:**

```go
// EstimateAIRequests reports how many model requests the current scope and detail
// level imply, so the rail can show the cost of a choice BEFORE the user pays it.
func (a *App) EstimateAIRequests(fileNames []string, aiScope *AIScope) (int, error)
```

It must compute this by calling `engine.ScanChunks` through the SAME helper
`runLocalAIPhase` uses, not by a parallel formula. A second implementation of the
packing rule is a number that disagrees with reality as soon as either copy
changes. Document it in `frontend/BRIDGE.md` with the other detection methods.

**`frontend/state.js`:**

- Settings default (`:117`) gains `aiDetailLevel: "thorough"`.
- Export the two level identifiers as a frozen list beside the other mirrored
  vocabularies, so the rail cannot invent a third: follow how `PRESETS` and
  `SIGNAL_SOURCES` are done.

**`frontend/views/identifyrail.js`:**

- `localAISection` (`:880`) gains the control after the model field and before
  "Context", because it belongs with the two settings about how much the model
  reads. Short visible label plus a help tooltip, per the rail's discipline: a
  `<select>` with two options, gated exactly like the model field is.
- `pushSettings` (`:1034`) sends `aiDetailLevel` the way it sends `model`: read
  the element if the tab is rendered, otherwise fall back to the store, so
  switching tabs never resets it.
- The request-count read-out is DYNAMIC information, so it stays inline. **It must
  not be a `<p class="hint">`**: `frontend/identifyrail.test.js:779` counts
  `p.hint` in the rendered rail and demands zero, and the Local AI section is in
  the DOM even when folded. Use the established shape for a live fact, a
  `<span class="hint">` inside a `.rail-status` row, which is what the Ollama
  availability line already does (`:900`). Getting this wrong turns a green suite
  red in a way that looks unrelated to the change.

**`frontend/copy.js`:** add `RAIL.detailLevel` (short, two or three words) plus
the option labels, and `CONFIGURE.detailLevelHelp`. The tooltip says what the
setting achieves, in outcome terms:

> The local AI reads your document in slices. One page or slide at a time finds
> the most values and takes the longest. Larger slices are quicker and can miss
> values completely.

No em dashes (`frontend/copy.test.js` and `copy_guard_test.go` both police this).
Do not put numbers of bytes or requests in the copy: those are dynamic and belong
in the read-out.

### Tests

`backend/engine/aichunks_test.go`: `ScanTargetBytes` maps both levels, and an
unknown or empty level maps to the thorough target. Assert the DIRECTION of the
default explicitly ("an unrecognised level must not be the fast one"), because
that is the invariant a future third level could quietly break.

`backend/app_validation_integration_test.go`: an unknown detail level is refused
by `ApplySettings` with a message naming the valid values; both valid levels are
accepted and survive a `GetSettings` round-trip.

`backend/engine/session_test.go`: a session written with a level round-trips it;
a v8 file WITHOUT the field loads as thorough and `SessionVersion` is still 8.

`backend/app_detect_integration_test.go`: `EstimateAIRequests` equals the number
of model calls a real run then makes, for the same scope and level. That
equality IS the contract: a read-out that predicts a different number from the run
is worse than no read-out.

`frontend/identifyrail.test.js`: the control renders with a help tooltip (the
existing "every strictness field explains itself through a tooltip" pattern at
`:531` is the model to copy); its label is short; the panel still contains no
`p.hint`; `pushSettings`' payload carries `aiDetailLevel`.

`frontend/state.test.js`: the default is thorough, and the exported level list has
exactly the two identifiers Go validates.

`scripts/uitest/probes.js` and `scripts/uitest/renderharness/checks.go`: the rail
probe already measures label widths and clipping; extend the seeded state so the
Local AI tab renders the new control, and assert its label is not clipped. Keep
both harnesses on the one `probes.js` (`uitest_parity_test.go` enforces it).

---

## CR3 — The reply format is the user's choice for discovery, and stays a schema for classification

### Current behaviour

`Chat` receives `format` from its callers and both pass `suggestionSchema()`
(`backend/ollama/client.go:748`): the discovery call at `:526` and the
classification call at `:651`. §8 states the request body "must carry a JSON
**Schema** in `format`", full stop, and CHANGE-08 CR4 made it so on a measured
6-to-16-name recall win.

Section 0 measurement 5 is what forces this open again. The schema is not the
villain, but on the owner's 0.8B it is the difference between 13 values and none,
and it costs 2.2x to 2.4x wall clock on the deck for recall that is equal or
slightly worse. Meanwhile the single large win behind CHANGE-08's decision does
not reproduce across backends.

### Change

**`backend/ollama/client.go`:**

- `Client` gains `StrictFormat bool`, set from settings exactly as `Model` and
  `ContextSize` are (`backend/app.go:590`).
- The DISCOVERY call sends `suggestionSchema()` when `StrictFormat` is true and
  the string `"json"` otherwise. Put that choice in one small method
  (`discoveryFormat()`), never inline at the call site, so there is one answer to
  "what shape does discovery ask for".
- The CLASSIFICATION call keeps `suggestionSchema()` unconditionally. It is a
  different task and the schema earns its keep there: the input is a bounded list
  of names the model only has to file, "every category present" is the property
  that makes a re-filing complete, and the payload is small enough that the token
  cost is noise. Say that in the comment, because a reader who has just seen
  discovery made switchable will otherwise assume this was missed.
- `parseSuggestionJSON`'s tolerances (`:774`) stay. With the schema off they are
  load-bearing again rather than belt-and-braces, which is worth one sentence in
  their comment.

**`backend/app.go`:** `Settings` gains `AIStrictFormat *bool`
with the JSON tag `aiStrictFormat`, a POINTER for the reason `UseBuiltInPatterns` is one
(`backend/engine/session.go:82`): "absent" and "the user switched it off" must stay
distinguishable across a session file. Default OFF. `ApplySettings` copies it to
`a.llm.StrictFormat`.

**`backend/engine/session.go`:** `SessionSettings` gains `AIStrictFormat *bool`
with the JSON tag `aiStrictFormat,omitempty`. No version bump, same argument as
CR2.

**`frontend/state.js`, `identifyrail.js`, `copy.js`:** a checkbox in the Local AI
section, off by default, `aiStrictFormat` in the `pushSettings` payload, and a
tooltip in outcome terms:

> Makes the model answer for every category instead of only the ones it thought
> of. Sometimes finds a little more, and usually takes about twice as long.

**`CLAUDE.md` §8 must change with this**, and this is the edit to get right. Its
current bullet says the request body "must carry a JSON **Schema** in `format`".
Rewrite it as a PER-CALL rule with both measurements recorded, because the next
person to read it needs to know the evidence points both ways:

- the classification call always carries the schema, and why;
- the discovery call carries it when the user asks for it, defaulting to
  `"format":"json"`, and why: it doubled wall clock at equal recall on a
  slide-heavy deck and returned nothing at all on a 0.8B model, while it measured
  a recall win on a short email page and on the repo's small fixtures;
- the schema itself is unchanged: still derived from `promptCategories`, still
  flat, still no `$defs` or `$ref`, and `TestPromptsAndParserAgreeOnTheCategoryKeys`
  still holds the four lists to each other.

Also update §7's "Ollama HTTP API" row (`CLAUDE.md:581`), which repeats the
always-a-schema claim in passing.

### Tests

`backend/ollama/client_test.go`:

- `chatReplyServer` (`:39`) currently asserts the schema shape on EVERY call. It
  must now assert per call: the classification prompt always gets the schema; the
  discovery prompt gets whatever `StrictFormat` selected. The system prompt in the
  captured request tells the two apart. This helper backs most tests in the file,
  so change it once, in this CR's commit, and expect the file to be red until you
  do. It is the same hotspot CHANGE-08 flagged, for the same reason.
- ADD: with `StrictFormat` false the discovery request carries `"format":"json"`;
  with it true, the schema object; the classification request carries the schema
  in both cases. That third assertion is the one that stops a later tidy-up from
  making classification switchable too.
- KEEP `TestPromptsAndParserAgreeOnTheCategoryKeys` (`:855`) exactly as it is: the
  schema is still built from `promptCategories`, so all four lists still have to
  agree.

`backend/app_validation_integration_test.go`: `AIStrictFormat` survives an
`ApplySettings` round-trip in all three states (nil, true, false), and nil is
treated as off rather than as on.

`frontend/identifyrail.test.js` and `frontend/state.test.js`: the checkbox renders
with a tooltip, defaults to off, and reaches the payload.

---

## CR4 — A silent or truncated model must not read as a clean document

The reported bug was invisible for a reason: nothing in the run distinguishes "the
model answered nothing, fifteen times" from "this document has no values in it".

### Current behaviour

- `scanChunks` (`backend/ollama/client.go:533`) counts nothing. A reply of
  `{"person_names": [], ...}` is indistinguishable downstream from a document with
  no people in it.
- `detectionStatus` (`backend/app_detect.go:566`) reports "scanned N file(s), 0
  suggestion(s)". True, and useless.
- `chatResponse` (`backend/ollama/client.go:329`) does not decode `done_reason`,
  so a reply cut off at `maxReplyTokens` (512) surfaces as
  `parseSuggestionJSON`'s "not the expected JSON object (unexpected end of JSON
  input)". Measured on the 4B with the whole deck: `done_reason: "length"`,
  `eval_count: 512`, the content a repeat loop cut mid-string. The user is told
  their model produced malformed JSON, when what happened is that they asked one
  request to describe a whole document.

### Change

**`backend/ollama/client.go`:**

- `chatResponse` (`:329`) decodes `DoneReason string` with the JSON tag `done_reason` and
  `EvalCount int` with the JSON tag `eval_count`. Both are already in every reply; nothing
  new is requested.
- `postChat` (`:418`) returns an actionable error when `DoneReason == "length"`,
  BEFORE the parser ever sees the truncated text: say the reply was cut off after
  N tokens, and name the fix the user actually has ("send less text per request by
  choosing the Thorough detail level, or scan fewer pages at a time"). No em dash:
  `copy_guard_test.go` walks `backend/`.
- `DiscoveryOutcome` (CR1) carries `Requests` and `Silent`. Count a request as
  silent when it parsed cleanly and produced zero suggestions AFTER the
  hallucination filter, because a reply of three invented names the filter dropped
  is silence from the user's point of view.

**`backend/app_detect.go`:**

- `DetectionResult` (`:73`) gains `AIRequests int`, `AISilentRequests int` and
  `AISecondsPerRequest float64`. The third is measured, not estimated: total AI
  phase wall clock divided by requests. It is what lets a user judge the CR2 dial
  on their OWN document, which no fixed guidance in a tooltip can do.
- When `AIRequests > 0 && AISilentRequests == AIRequests`, append a message to
  `res.Errors` naming the model and the request count, and pointing at the two
  things that change the answer (a larger model, a smaller detail level). Errors
  is the right home: the frontend already renders it as warnings the run finished
  with, and this IS a problem the user wants to know about. Do not invent a
  parallel "notes" channel for it.
- `detectionStatus` (`:566`) mentions the request count when the AI phase ran, so
  the one-line summary distinguishes the two cases by itself.

**`frontend/`:** the Identify view already renders `result.errors`; confirm the new
message appears there and reads well. Add the per-request timing to the same
`.rail-status` read-out CR2 introduces ("last scan: 15 requests, about 7s each"),
built from the new fields. Copy in `copy.js`, dynamic values interpolated.
`frontend/BRIDGE.md` gains the three new `DetectionResult` fields.

### Tests

`backend/ollama/client_test.go`: a mock returning `done_reason: "length"` produces
the truncation error, and the error names the detail level as the fix. A mock
returning a well-formed empty object produces `Silent == Requests` and NO error:
silence is data, not a failure, at this layer.

`backend/app_detect_integration_test.go`:

- ADD: every request silent, on a document that has text, yields a run that
  FINISHES (status set, `detection:done` emitted, exactly one terminal event) and
  carries a message naming the model. Assert the message mentions the model name,
  because "your model found nothing" is the actionable half.
- ADD: `AIRequests` equals the number of model calls the mock server saw, and
  `AISilentRequests` equals the number that returned empty objects. Wire it to the
  server's own count rather than to a constant, so the assertion cannot drift.
- ADD: a truncated reply on ONE file leaves the other files' suggestions intact.
  `TestDetectionKeepsGoingWhenOneFileFails` (`:276`) is the pattern to follow.

`result_shape_test.go` is for `engine.ResultDocument`, not `DetectionResult`, so it
is untouched. Do not add the new fields there.

---

## CR5 — PPTX soft line breaks, and the title lines that are currently discarded

Independent of the model work, and a parallel session can take it.

### Current behaviour

`walkShapes` (`backend/engine/convert/pptx.go:151`) has no `a:br` case, so every
soft line break inside a paragraph vanishes and the runs on either side are
concatenated. `flushShape` (`:167`) takes a title shape's FIRST line as the
section heading and discards the rest.

On the reference deck, whose slide 1 is a single `<a:p>` containing five `<a:br/>`
elements, the two combine into one glued heading (section 0). `docx.go:378`
already handles `w:br`, mapping a page break to the page sentinel and any other
break to a space.

### Change

`backend/engine/convert/pptx.go`:

- Add `case "br":` to the `xml.StartElement` switch (beside `"t"` at `:224`),
  inside the `inPara` guard: end the current line and start a new one at the same
  outline level, by flushing `paraText` into `shapeText` the way the `a:p` end
  element does at `:256`. Factor that flush into one closure so a paragraph end
  and a soft break cannot drift apart.
- `flushShape`: a title shape's first line stays the heading; its REMAINING lines
  go to the body instead of being dropped. Emit them as plain lines, not bullets:
  they are prose continuation (an author list, a date), and the bullet convention
  belongs to body placeholders. Without this half the CR makes the output worse,
  not better: the authors, date and version on the reference deck would disappear
  from the document entirely rather than being glued to the title.
- Comment the WHY in the present tense: a soft break is a line break in the
  source, so it is a line break in the markdown; and a title placeholder holding
  more than a title still holds document text, which anonymisation must see.

`backend/engine/convert/pptx.go` header comment (`:6`) documents the "## Slide N:
title" shape; extend it with the overflow rule.

### Tests

`backend/engine/convert/fixtures_test.go`: extend `buildPptxFixture` (`:158`) so
slide 1's title is `Quarterly Review` + `<a:br/>` + two more lines (an author and
a date, so the case matches the real document). Only ONE existing expectation
covers this fixture's full markdown,
`backend/engine/convert/convert_integration_test.go:60`, so update that `want`
string in the same commit. That is the whole blast radius; confirm with a grep for
`Quarterly Review` before starting.

`backend/engine/convert/convert_test.go` (or the integration file, matching where
the pptx cases already live):

- ADD: two runs separated by `<a:br/>` do NOT concatenate. Assert the exact
  glued string cannot appear, naming the real symptom in the failure message
  (`PwCOscar Liber`), because that string is why the test exists.
- ADD: a title shape with three lines produces a one-line heading and keeps the
  other two lines in the body. Assert both halves: the heading is short AND the
  overflow survives.
- ADD: a body placeholder with a soft break still yields bullets, one per line, so
  the fix has not turned body text into prose.

`backend/engine/pagescope_test.go:76` reads the committed `deck.pptx`, not the
built fixture, so slide COUNTING is unaffected. Re-run it anyway: it is the guard
that would catch a stray extra `## Slide` heading.

---

## CR6 — A model default that exists, and a dropdown that does not choose for you

### Current behaviour

Three things line up badly (section 0, fourth defect):

1. `DefaultModel = "qwen3.5:0.8b"` (`backend/ollama/client.go:46`) is a NAME, and
   a name is only a default if the machine has it. The owner's machine does not.
2. `frontend/state.js:117` starts at `model: ""`, and `ApplySettings`
   (`backend/app.go:590`) keeps `DefaultModel` when the setting is empty, so the
   first run of a fresh session posts to a model that may not exist. The error is
   correct and actionable ("model %q is not installed; run 'ollama pull %s' or
   pick another model in settings"), but it arrives as a per-file detection
   problem, at the end of a run the user waited for.
3. The `<select>` (`frontend/views/identifyrail.js:887`) marks an option
   `selected` only when it equals `s.settings.model`. With `model: ""` no option is
   marked, so the browser picks the FIRST, and `pushSettings` (`:1034`) sends it.
   The effective model is decided by Ollama's tag ordering.

### Change

The rule to establish: **the effective model is always one the probe just saw**,
and the user is never silently given a different one.

`backend/app.go`:

- After a successful probe (`ProbeOllama` at `:385` and the probe at the end of
  `ApplySettings`), resolve the effective model: keep `s.Model` when the probe
  lists it; otherwise prefer `ollama.DefaultModel` when the probe lists THAT;
  otherwise take the first model the probe returned. Store the resolution in
  `a.settings.Model` and on `a.llm`, so what the next run uses is what the last
  probe justified.
- Put the resolution in ONE unexported helper on `App` and call it from both
  places. Two copies of a preference order is two preference orders.
- `OllamaStatus` is what the frontend reads back
  (`backend/ollama/client.go:119`); it already carries `Models` and `Detail`. Add
  the resolved name to the App-level return rather than to `OllamaStatus` itself:
  the resolution is an APP decision (it involves stored settings), and
  `ollama.Client` must not start reading settings to make it. `ProbeOllama`
  already returns `ollama.OllamaStatus`; wrap it in a small App-level struct
  carrying the status plus `model`, and update `frontend/BRIDGE.md`.
- The `DefaultModel` constant, its comment and §7's pin are UNCHANGED. It stays
  the documented preference; this CR only stops it being used as if it were
  installed. Do not touch the §7 model row here (see the contradiction note in
  section 0 and the spot-check in the acceptance criteria).

`frontend/`:

- `api.js`: `probeOllama` and `applySettings` now return the model alongside the
  status. `api.js` is the only file allowed to call bound methods, so the shape
  change stops there.
- `state.js`: when a probe reports a resolved model, adopt it into
  `settings.model`. That is what makes the dropdown show what will actually run.
- `identifyrail.js`: the `<select>` must always have exactly one option marked
  selected when models exist. Do not rely on the browser's implicit first-option
  behaviour, which is precisely how the 4B got chosen for the owner.

### Tests

`backend/app_validation_integration_test.go` (or a new
`backend/app_model_integration_test.go` if that file is already crowded):

- A probe whose tags do NOT include `DefaultModel` and whose stored model is empty
  resolves to an installed one, and a following detection run posts to THAT name.
  Assert on the model name the mock server received: this is the bug, so the
  assertion has to be about the wire.
- A probe whose tags DO include `DefaultModel` and whose stored model is empty
  resolves to `DefaultModel`, not to the first tag. The preference order is the
  contract.
- A stored model that is still installed is left alone, even when it is not
  `DefaultModel`: the user's choice outranks the pin.
- A probe that fails changes nothing: an unreachable Ollama must not rewrite the
  user's model setting.

`frontend/identifyrail.test.js`: with models present, exactly one `<option>`
carries `selected`. `frontend/api.test.js`: the new return shape is unwrapped in
`api.js` and nothing else sees it. `frontend/state.test.js`: a probe result with a
resolved model updates `settings.model`; one without leaves it alone.

---

## CR7 — Document the Ollama environment, because it is worth more than the code

Documentation only, no code. It is in this order because it is the single largest
measured improvement available to the owner (2.2x), and because the application
cannot do it: the setting lives in the Ollama service's environment and
`/api/tags` does not report it.

### Change

**`README.md`**, in "Optional: local AI assistance with Ollama" (`:107`), after
the pull instruction, add a short subsection ("Make it faster: let Ollama use your
GPU" or similar) saying:

- Ollama ships with Vulkan enabled but **ignores integrated GPUs unless told
  otherwise**, and most business laptops have exactly that. It logs "dropping
  integrated GPU" and runs on the CPU.
- Setting `OLLAMA_IGPU_ENABLE=1` and restarting Ollama measured **2.2x faster** on
  the reference deck, with no change to quality.
- **On Windows it must be set for the Ollama service, not in a terminal**:
  `setx OLLAMA_IGPU_ENABLE 1`, then quit Ollama from the tray and start it again.
  A variable exported in a shell reaches nothing, and this is the mistake that
  makes people conclude the setting does not work.
- How to confirm it took: the server log reports the GPU as inference compute and
  "offloaded N/N layers to GPU" instead of "dropping integrated GPU".
- Discrete NVIDIA and AMD GPUs are used automatically and need none of this.

Keep the section short and copy-paste-able. It is a performance note, not a
tutorial, and it must not read as a requirement: the app works without it.

**`frontend/docs/`** carries the bundled offline documentation, which is what a
user actually reaches from the Documentation menu. Add the same guidance there, in
the same words, in whichever page covers the Local AI route. Remember the second
window may load NOTHING but embedded assets (`CLAUDE.md` §4): no link out to
ollama.com from that page, so name the variable in text rather than linking to
documentation the user cannot open.

**`frontend/copy.js`:** one sentence in the Local AI section's help tooltip
pointing at the Documentation window for speed settings. One sentence, not a
paragraph: the rail explains through tooltips and this one already carries the
route's own explanation.

**`CLAUDE.md` §8** gains a bullet recording the measured environment
recommendation and, importantly, that it is NOT a code constant and cannot be
detected: it is documented in the README and the bundled docs, and the numbers
behind it (2.2x on the reference deck, 33/33 layers offloaded, an Intel Arc 140V
on the owner's laptop) belong beside it so the next person does not have to
re-measure to trust it.

### Tests

`frontend/copy.test.js` covers the new sentence for em dashes automatically; run
it. There is nothing else to test here, and inventing a test that asserts a README
contains a string would be a test of the documentation's spelling, not of the
software.

Do NOT add a runtime check that warns when the GPU is off. The app cannot see the
server's environment, so any such check would have to infer it from timings, and a
performance warning that fires on a slow document is a false alarm the user cannot
act on.

---

## Decisions taken

1. **A slice is aligned to the document's own units and sized by the user's
   detail level, never by the context window.** `promptBudgetBytes` answers a
   different question ("what fits") from the one that matters ("what can still be
   extracted from"), and one request over a whole document measured zero values on
   every model tried.
2. **The engine owns document division** (`engine.ScanChunks`), the client owns
   request shaping. The byte splitter moves to the engine with the rest of the
   division logic; the client keeps no copy.
3. **Two detail levels, thorough (default) and faster.** Targets 1200 and 4000
   bytes, both from the measured recall cliff. A "whole document in one request"
   level is deliberately not offered: it measures zero.
4. **An unknown or absent detail level reads as thorough.** A level nobody chose
   must not be the one that finds less.
5. **The discovery reply format is the user's choice, defaulting to
   `"format":"json"`; the classification reply is always schema-constrained.** The
   schema doubled wall clock at equal recall on the deck and returned nothing at
   all on the 0.8B, while measuring a win on a short email page and on the repo's
   fixtures. `CLAUDE.md` §8 becomes per-call and records both results.
6. **A run reports what the model did not say.** Requests, silent requests and
   measured seconds per request travel on `DetectionResult`; an all-silent AI phase
   names the model in the run's problems. "0 suggestions" must stop meaning two
   different things.
7. **A truncated reply is reported as truncation**, from `done_reason`, with the
   detail level named as the fix, instead of surfacing as malformed JSON.
8. **Parallel requests are rejected.** 1.13x once the GPU is enabled, 1.01x
   against a default Ollama configuration, against the cost of a concurrent chunk
   loop: progress ordering, mid-flight cancellation, partial merges and per-request
   error attribution, all of which the sequential loop gets right today. If it is
   ever revisited it is its own change order, aimed at CPU-only machines, where it
   measured 1.63x.
9. **No session-file bump.** `AIDetailLevel` absent reads as thorough;
   `AIStrictFormat` is a `*bool` so absent and off stay distinguishable. Both are
   "an added field the loader can ignore", which the `SessionVersion` comment
   already excludes from bumping. `SessionVersion` stays 8.
10. **Large scans warn, they do not refuse.** `MaxChunksPerDocument` becomes
    `LargeScanRequests`, a warning threshold. The user asked for the scan and can
    cancel it.
11. **The effective model is always one the probe just saw**, with a documented
    preference order (stored choice, then the pinned default, then the first
    installed). §7's pin is unchanged.
12. **The §7 model pin is NOT changed in this order**, despite CHANGE-08's
    16-name measurement failing to reproduce, because a locally imported GGUF and
    a library tag are not the same artefact and the acceptance criteria settle it
    with a spot-check rather than a guess.
13. **The GPU setting is documented, never automated**, and no timing-based
    warning is added for it.
14. **Heuristic discovery's category quality stays out of scope.** Filing
    "Impact High" as a person is a real defect with its own root cause, and folding
    it in here would make this order unreviewable.

## Conflict analysis

### Files touched by more than one CR

| File | CRs | Note |
|---|---|---|
| `backend/ollama/client.go` | CR1, CR3, CR4, CR6 | the centre of gravity. CR1 removes the chunking, CR3 adds the format switch, CR4 adds `done_reason`. Sequence them in that order inside one file pass. |
| `backend/ollama/client_test.go` | CR1, CR3, CR4 | and specifically `chatReplyServer` (`:39`), which CR3 changes structurally. See the hotspot below. |
| `backend/app_detect.go` | CR1, CR2 (`EstimateAIRequests`), CR4 | CR1 restructures `scopedUnits`/`runLocalAIPhase`; do CR1 before the other two touch it. |
| `backend/app.go` | CR2 (settings + validation), CR3 (settings), CR6 (model resolution) | three edits to `Settings` and `ApplySettings`; make the `Settings` additions in one pass. |
| `backend/engine/session.go` | CR2, CR3 | two fields, one comment about why neither bumps the version. Write that comment once. |
| `backend/app_detect_integration_test.go` | CR1, CR2, CR4 | the scope tests need CR1's multi-request shape before CR2 and CR4 add to them. |
| `frontend/state.js` | CR2, CR3, CR6 | settings defaults and the probe-result handling. |
| `frontend/views/identifyrail.js` | CR2, CR3, CR6 | `localAISection` and `pushSettings` both take three small edits; do them together. |
| `frontend/copy.js` | CR2, CR3, CR7 | three tooltips and one read-out template. |
| `frontend/BRIDGE.md` | CR2, CR4, CR6 | one new method, three new result fields, one changed probe return. Update it once, at the end, from the code rather than from this plan. |
| `CLAUDE.md` | CR1 (§5), CR3 (§7 row + §8 bullet), CR7 (§8 bullet) | both CR3 and CR7 edit §8; make the §8 edits in one pass so the section is not rewritten twice. |

### Hotspots

- **`chatReplyServer` (`backend/ollama/client_test.go:39`) is the riskiest edit in
  the order**, exactly as it was in CHANGE-08. It asserts the schema shape on every
  request; CR3 makes that assertion wrong for discovery calls, so **every test
  using the helper fails at once**. That is the guard doing its job and it is also
  the most confusing possible first symptom. Change the helper in the SAME commit
  as CR3, and expect the file to be red until you do.
- **CR1 changes what `scopedUnits` returns**, and `refineCategories` (`:275`) is a
  second consumer that needs the scoped TEXT rather than indices. Build that text
  from the same slices. If you find yourself writing a second "what does the scope
  select" implementation, stop: that duplication is the bug CR5 of CHANGE-08
  existed to remove, and re-introducing it would be a regression that no test
  currently catches.
- **The `p.hint` guard (`frontend/identifyrail.test.js:779`) will bite CR2.** The
  rail must contain ZERO explanatory paragraphs, the Local AI section is in the DOM
  even when folded, and the natural way to add a read-out is a `<p class="hint">`.
  Use a `<span class="hint">` in a `.rail-status` row instead. The failure message
  will talk about explanatory paragraphs and will not mention your read-out.
- **`engine` must not import `ollama`.** CR1 puts the division rule in the engine
  and the ceiling in the client, so the ceiling travels as an `int` argument. If a
  dependency appears in that direction, the design has drifted.
- **`copy_guard_test.go` walks `backend/` and `.`**, so every new error message in
  CR1, CR2 and CR4 is covered: no em dashes in the truncation message, the
  all-silent message, the large-scan warning or the unknown-level validation error.
- **`detection_parity_test.go` and `category_parity_test.go` are untouched by
  design.** No discovery method, match class, signal source or category changes
  here. If a CR appears to need one, that is a signal the CR has drifted.
- **The two new settings are NOT in `presetCategories` or the level presets.**
  They are Local AI scan parameters, not anonymisation scope, so no preset fills
  them and `category_parity_test.go` has nothing to say about them.

## Recommended order

1. **CR1**, alone, as the first commit. It is the fix, it is the largest edit, and
   everything else in the order either reads its new shape or reports on it. Land
   it with the engine tests and the updated integration tests green, and measure a
   whole-document run on the reference deck before moving on: that number is the
   proof the order works, and it is cheapest to get while nothing else has changed.
2. **CR4** second. It is small, it depends on CR1's `DiscoveryOutcome`, and having
   it early means every later measurement tells you whether the model was silent,
   which is information you will want for the rest of the order.
3. **CR3** third, with the `chatReplyServer` rewrite in the same commit. Now that
   silence is visible, flipping the discovery format is measurable rather than a
   matter of trust.
4. **CR2** fourth: the setting, its validation, its bound estimate and the rail
   control. It comes after CR3 because both add a Local AI control and the rail
   edit is cheaper done once, with CR3's checkbox already in place.
5. **CR6** fifth: the model resolution, which crosses the bridge and is otherwise
   independent.
6. **CR5** any time, including in parallel with 1 to 5: it shares no file with
   them.
7. **CR7** last, once the numbers in it have been confirmed on the owner's machine.

After step 4, run the full acceptance measurement on BOTH reference documents
before starting step 5. That is the point where the order either meets the owner's
latency targets or does not, and it is the one place where the plan may need to
come back to the owner (see criterion 4 below).

## Acceptance criteria

- `go test ./...`, `go test -tags=integration ./...`,
  `go test -tags=deep ./...` and `node --test "frontend/**/*.test.js"` all green.
- `task audit` reports no new finding. Expect the audit to notice anything left
  behind by CR1's deletions: an unused constant or an unreferenced helper in
  `backend/ollama/client.go` is exactly what `deadcode` is there to catch.
- The render harness passes (`docs/UITESTING.md`), including the rail label
  measurements with the two new controls present.
- With a real Ollama on the target laptop, on step 2 Identify with Local AI on:
  1. **The reported failure is fixed.** Scope "Entire document" on the reference
     deck (15 slides) returns values instead of nothing. On the owner's 4B it
     measured 80 raw strings, including "Vincent Gauché", "Oscar Liber", "PwC",
     "ADA", "CTIE", "ARHS" and "SAP"; the exact count will vary with the model and
     the backend, but ZERO is now a failure.
  2. **Both reference documents are measured**, not just the deck. The PDF must not
     regress: page 1 alone and the whole document both still return the names they
     returned before this order (the 4B measured 45 to 77 raw strings on page 1
     depending on backend and format).
  3. **The same page scanned twice produces the same suggestion list.** Greedy
     sampling is unchanged by this order, and unit slicing is deterministic, so this
     must still hold. It is the cheapest signal that the slicing is stable.
  4. **The latency targets.** One page or slide, and a 5-page scope, inside 20
     seconds (30 absolute maximum). A whole document of the reference sizes at about
     1 minute. On the owner's machine, with the GPU enabled and the default
     (thorough, JSON mode), the deck measured 1 m 48 s for discovery plus the
     classification call, so roughly 2 minutes end to end: **that is over target and
     it is the one open point in this order.** Report the measured number and let the
     owner decide between accepting it with honest progress, the faster detail level,
     a different model, or a further change order. Do NOT close the gap by
     quietly skipping units or dropping the classification pass.
  5. **Silence is legible.** Switch to the 0.8B, scan the deck whole, and confirm
     the run says the model returned nothing for N requests and names the model,
     rather than reporting "0 suggestions" as though the document were clean.
  6. **Truncation is legible.** There is no easy way to force `done_reason:
     "length"` once slicing is fixed, so verify it at the unit level (CR4's mock
     test) and, if you want the real thing, temporarily set the faster detail level
     on a dense PDF page with a small `num_ctx`.
  7. **The detail level and the format switch both change the outcome**, visibly:
     switching the detail level changes the request count in the read-out and the
     values found; switching "ask for every category" on roughly doubles the scan
     time. If either control makes no observable difference, it is not wired up.
  8. **The read-out tells the truth.** The pre-run request estimate equals the
     number of requests the run then makes, and the post-run "seconds each" is
     consistent with the wall clock the user just watched.
  9. **A fresh session uses a model that exists.** With no model ever chosen, on a
     machine whose tags do not include `qwen3.5:0.8b`, the first run must NOT fail
     with "model is not installed", and the dropdown must show the model that will
     actually run.
  10. **The PPTX fix is visible in the preview.** Import the reference deck and
      read slide 1 on the Import step: the heading is the title, and the authors,
      date and version are present as body lines. `PwCOscar Liber` must not appear
      anywhere, in the preview or in any suggestion.
  11. **Graceful degradation is untouched.** Stop Ollama, re-probe: the
      deterministic pipeline stays fully usable end to end, the Local AI controls
      (including the two new ones) render disabled with their tooltip, and no run
      posts to a model.
  12. **The GPU note is accurate on the owner's machine.** Follow the README
      instructions exactly as written, from a cold start, and confirm the server log
      shows the layers offloaded. A performance note that does not reproduce is worse
      than none.
- **The model spot-check (open decision).** Pull the library tag
  `ollama pull qwen3.5:0.8b` (about 700 MB, and ASK the owner before downloading
  anything) and measure it against the locally imported
  `Qwen3.5-0.8B-Q8_0:latest` on page 1 of the reference PDF, with the schema on.
  If the library tag reproduces CHANGE-08's 16 names, §7's pin is sound and the
  README should warn that a third-party GGUF import of the same nominal model can
  behave very differently. If it does not, §7's model row needs revisiting in its
  own change order, and this order's measurements should be cited there. Report the
  result either way; do not change the pin unilaterally.

## First actions for the implementation coordinator

1. Read `CLAUDE.md`, `backend/CLAUDE.md`, `frontend/CLAUDE.md`,
   `frontend/BRIDGE.md` and `docs/TESTING.md`.
2. Ask the owner for the two reference document paths (section 0) and confirm both
   convert cleanly on the current `main` before changing anything: import them and
   read the preview. That also confirms the CR5 symptom is still present.
3. Confirm the three claims this plan makes about the world, and report if any is
   wrong: that "Entire document" on the deck today produces exactly ONE model
   request (add a temporary log line or count the mock's calls); that the owner's
   Ollama still drops the integrated GPU by default; and that
   `engine.Document.PageCount` reports 15 for the reference deck.
4. Implement CR1 with its tests, then measure a whole-document run on the deck and
   record the number before touching anything else.
5. Then CR4, CR3, CR2 in that order, re-measuring after CR2, and bring criterion 4
   (the latency gap) back to the owner with a number rather than an estimate.

---

## Appendix A — The raw measurements

All on the owner's laptop, Ollama 0.32.14, greedy sampling as CHANGE-08 pinned it
(`temperature 0`, `top_k 1`, `top_p 1`, `presence_penalty 0`, `repeat_penalty 1`,
`seed 7`), `num_ctx 8192`, `think:false`, `keep_alive 30m`. "CPU" is the machine's
default configuration; "GPU" is the same machine with `OLLAMA_IGPU_ENABLE=1`.
Values counted as described in section 0.

### The reference deck (15 slides, 15,152 bytes of markdown)

| slicing | format | model | backend | requests | wall clock | values |
|---|---|---|---|---:|---|---:|
| whole document | schema | 0.8B | CPU | 1 | 23 s | 0 |
| whole document | json | 0.8B | CPU | 1 | 19 s | 0 |
| 2,048 B chunks | schema | 0.8B | CPU | 12 | 25 s | 0 |
| 2,048 B chunks | json | 0.8B | CPU | 12 | 19 s | 0 |
| 1,024 B chunks | schema | 0.8B | CPU | 26 | 1 m 29 s | 0 |
| 1,024 B chunks | json | 0.8B | CPU | 26 | 1 m 20 s | 13 |
| 512 B chunks | schema | 0.8B | CPU | 50 | 2 m 10 s | 0 |
| 512 B chunks | json | 0.8B | CPU | 50 | 2 m 23 s | 21 |
| whole document | json | 4B | CPU | 1 | 2 m 13 s | 0 |
| whole document | schema | 4B | CPU | 1 | 5 m 31 s | 0 |
| whole document, 512-token cap | schema | 4B | CPU | 1 | 1 m 10 s | 0, truncated |
| one slide per request | schema | 4B | CPU | 15 | 8 m 56 s | 70 |
| one slide per request | json | 4B | CPU | 15 | 3 m 59 s | 76 |
| one slide per request | schema | 4B | GPU | 15 | 4 m 18 s | 71 |
| one slide per request | json | 4B | GPU | 15 | 1 m 48 s | 80 |
| one slide per request, 4 concurrent | json | 4B | GPU | 15 | 1 m 46 s | 80 |

### The reference PDF (2 pages, 4,360 bytes of markdown)

| slicing | model | backend | schema | json |
|---|---|---|---:|---:|
| page 1 (3,577 B) | 0.8B | CPU | 0 | 2 |
| page 2 (780 B) | 0.8B | CPU | 0 | 1 |
| whole document | 0.8B | CPU | 0 | 3 |
| page 1 | 4B | CPU | 77 | 45 |
| page 2 | 4B | CPU | 9 | 9 |
| whole document | 4B | CPU | 31 | 38 |
| page 1 | 4B | GPU | 53 | 48 |
| page 2 | 4B | GPU | 9 | 9 |
| whole document | 4B | GPU | 35 | 34 |

### The repository's own fixtures (0.8B, CPU)

| fixture | bytes | schema | json |
|---|---:|---:|---:|
| `sample.txt` | 420 | 2 | 1 |
| `sample.md` | 429 | 2 | 2 |
| `french.md` | 183 | 2 | 2 |
| `code_fences.md` | 142 | 2 | 1 |
| `sample.csv` | 305 | 3 | 3 |

### What each group is evidence for

- The deck's "whole document" rows are the reported bug, and they are why CR1 is a
  correctness fix rather than a performance one. Four different configurations, two
  models, two formats, all zero.
- The deck's chunk-size rows locate the recall cliff between 1 KB and 2 KB on the
  0.8B, which is where CR2's thorough target (1200 bytes) comes from.
- The 512-token-cap row is the truncation CR4 reports properly.
- The `schema` against `json` columns, read across all three groups, are why CR3
  makes the format a choice: the schema wins on tiny fixtures and one email page,
  loses on the deck, and is fatal on the 0.8B.
- The GPU rows are CR7, and the concurrent row is decision 8.
- The fixtures group is the only place the schema is consistently at least as good,
  and every one of those documents is under 500 bytes. That is a useful warning
  about what the repository's own test corpus can and cannot tell you about model
  behaviour: it is far smaller than any real document.

## Appendix B — Measured and rejected, and why it is recorded

Recorded so the next session does not spend an afternoon re-discovering them.

- **Retry a silent request without the schema.** Tested as a policy over the whole
  deck: 26 requests became 52, wall clock went from 1 m 20 s to 2 m 3 s, and the
  values found were **identical** (13). At 512 bytes, 46 of 50 requests are
  legitimately silent, so a retry-on-silence rule pays double for almost every
  request on any document. Rejected: it buys nothing that CR3's default does not
  buy for free.
- **Parallel requests.** 1.63x on CPU with `OLLAMA_NUM_PARALLEL=4`, 1.13x once the
  GPU is enabled, 1.01x against a default Ollama configuration (which serialises).
  Rejected for the reasons in decision 8. If it is ever wanted, note that it needs
  BOTH an application change and a user configuration change, so it cannot be
  delivered by documentation alone.
- **512-byte slices as the default.** Finds more on the 0.8B (21 against 13) and
  costs nearly twice the time, and on a 15-slide deck it means 50 requests instead
  of 15. Rejected as a default; the faster/thorough pair already lets a user who
  wants maximum recall reach for the thorough end, and a third "exhaustive" level
  would be a third thing to explain for a case the model choice covers better.
- **Skipping units with no candidate proper noun.** Would have saved roughly 25%
  on the deck (4 of 15 slides returned nothing). Rejected for this order: a skipped
  unit is one the model never sees, the saving is modest, and the recall risk is
  invisible in the result. If latency has to come down further, this is the first
  thing to measure properly, with the recall cost quantified on both reference
  documents.
- **Making the classification pass optional.** It is a second model call over the
  whole suggestion list and it costs real time on a large document. Not touched
  here because CHANGE-08 CR5 has just scoped it correctly and its value (better
  categories on offline findings) is not measured by anything in this
  investigation. If the latency gap in criterion 4 has to be closed, measure this
  before reaching for the pre-filter above.
- **A schema with fewer required keys.** Tried: `required: []` made the 0.8B
  produce long repetitive invented lists ("LUCCS-TAT Strategy-Deviations-ADA-ADA"),
  which is worse than silence. A single-key schema (`person_names` only) worked
  fine, which is what identifies the seven-required-arrays shape as the problem on
  that model, but seven separate calls per slice is not a trade anyone wants.
  Recorded because it explains WHY the schema fails on a small model, which the
  §8 rewrite in CR3 should reflect.

## Appendix C — Reproducing the measurements

The probe harness was two `//go:build live` test files (one in `package backend`,
one in `package ollama`) reading a document path and a model name from the
environment. It is deliberately NOT committed: `docs/TESTING.md` allows three
tiers and no env-var gating, and a test that needs a real model and a real client
document belongs to neither. Rebuilding it is about twenty minutes:

1. In `package ollama`, a `live`-tagged test that reads a document with
   `engine.LoadAll`, builds a `Client` pointed at `DefaultBaseURL`, and calls the
   real discovery path for a given slice, printing the raw reply, the parsed
   count, and which strings survive `strings.Contains(source, name)`.
2. A raw `http.Post` helper beside it, so the probe can vary what the client pins
   (`format`, `num_predict`, sampling, `num_ctx`) without editing production code.
   Print `prompt_eval_count`, `eval_count` and `done_reason` from every reply:
   those three fields are what distinguish empty from truncated from cut short,
   and they are the difference between an hour of confusion and a diagnosis.
3. In `package backend`, a `live`-tagged test that drives `App.RunDetection`
   end to end with `runtimeEventsEmit` swapped for a sink (`withRecorder` in
   `app_detect_integration_test.go:89` shows how), so the bound-app behaviour can
   be measured, not just the client's.
4. To measure the GPU without touching the owner's own Ollama, start a second
   server on another port with the environment you want to test
   (`OLLAMA_HOST=127.0.0.1:11435 OLLAMA_IGPU_ENABLE=1 ollama serve`) and point the
   probe at it. It shares the model blobs, so nothing is downloaded, and killing it
   afterwards leaves the user's own server untouched.

Delete the probes when the measurement is done, and put the NUMBERS in the change
order instead. A measurement harness that lingers becomes a test nobody runs and
nobody trusts.
