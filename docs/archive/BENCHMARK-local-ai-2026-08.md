# BENCHMARK — the local AI route, August 2026

Archived measurement record for the Local AI discovery route, gathered while
implementing CHANGE-09 (`docs/CHANGE-09.md`). It exists so the numbers behind
that order's decisions can be **questioned or reused** rather than re-derived,
and so a future session can tell which figures are still comparable to a fresh
run and which are not.

It is a record, not a plan. Nothing here is authoritative over `CLAUDE.md`; where
a number in this file drove a rule, that rule lives in `CLAUDE.md` §7 or §8 and
this file is the evidence beneath it.

**Confidentiality.** Both reference documents are real client and internal
documents and are NOT in this repository. They are identified in
`docs/CHANGE-09.md` section 0; their paths are not recorded anywhere in the repo.
Every figure below is a COUNT or a SHAPE. No string any model returned appears in
this file, and none may be added to it.

---

## 1. The test bench

Every measurement in this file was taken on ONE machine. That is a limitation of
the whole record: the wall-clock figures are properties of this laptop, and the
value counts are properties of this laptop's backends. Nothing here has been
reproduced on second hardware.

### Machine

| Property | Value |
|---|---|
| Model | LENOVO 21NTS3UW01 |
| CPU | Intel(R) Core(TM) Ultra 7 268V |
| Cores / threads | 8 / 8 (no SMT) |
| Base clock | 2.20 GHz |
| RAM | 33,810,075,648 bytes (31.5 GiB) |
| GPU | Intel(R) Arc(TM) 140V, integrated, 16 GB shared, driver 32.0.101.6913 |
| GPU as Ollama sees it | `library=Vulkan name=Vulkan0 type=iGPU total="17.9 GiB" available="17.2 GiB"` |
| OS | Windows 11 Enterprise, build 22631 (10.0.22631) |

Two virtual display adapters are also present (MirrorOp, DameWare); they are
remote-access artefacts and take no part in inference.

The GPU is INTEGRATED, and that is the single most important fact about this
bench: Ollama detects an integrated GPU and then **drops it** unless
`OLLAMA_IGPU_ENABLE=1` is set for the service. So "CPU" below is not a machine
without a GPU, it is this machine with its GPU deliberately idle, which was the
DEFAULT state until 2026-08-20.

### Software

| Component | Version |
|---|---|
| Ollama | 0.32.14 |
| Go | go1.26.5 windows/amd64 |
| Node | v24.18.1 |
| Application | this repository at the CHANGE-09 branch head |

### Ollama server configuration of record

Read from the server's own startup log, not assumed:

```
OLLAMA_VULKAN:true            OLLAMA_NUM_PARALLEL:1
OLLAMA_IGPU_ENABLE:1          OLLAMA_MAX_LOADED_MODELS:0
OLLAMA_FLASH_ATTENTION:false  OLLAMA_CONTEXT_LENGTH:0
OLLAMA_KEEP_ALIVE:5m0s
```

`OLLAMA_IGPU_ENABLE` was **unset** for every row marked CPU and **1** for every
row marked GPU. `OLLAMA_NUM_PARALLEL:1` is the server default and is why the
concurrency experiment (section 7) measured almost nothing: concurrent requests
are serialised unless the user also changes this.

`OLLAMA_CONTEXT_LENGTH:0` means the server imposes no context length; the
application sends its own `num_ctx` of 8192 per request.

### What the application pins per request

Set by `backend/ollama/client.go` and unchanged across every row unless the row
says otherwise: `temperature 0`, `top_k 1`, `top_p 1`, `presence_penalty 0`,
`repeat_penalty 1`, `seed 7`, `num_ctx 8192`, `think:false`,
`keep_alive 30m`, `stream:false`. Greedy decoding, so a repeat on one backend is
byte-identical; ACROSS backends it is not, which section 5 is about.

`repeat_penalty` is pinned neutral on purpose (CHANGE-08 decision 4) so a name
can be copied verbatim. That is also why a degenerate reply loops rather than
being penalised out of it.

### Models installed

| Tag | Size | Parameters | Quantisation | Family |
|---|---|---|---|---|
| `Qwen3.5-4B-Q4_K_M:latest` | 2.74 GB | 4.2B | Q4_K_M | qwen35 |
| `Qwen3.5-0.8B-Q8_0:latest` | 0.81 GB | 752M | Q8_0 | qwen35 |
| `mistral0.3:7b-instruct` | 4.37 GB | 7.2B | Q4_K_M | llama |
| `qwen2.5:3b-instruct` | 2.22 GB | 3.1B | Q5_K_M | qwen2 |

Only the two Qwen3.5 builds were benchmarked. **Both are locally imported Unsloth
GGUF quantisations, not Ollama library tags**, and the machine does NOT have
`qwen3.5:0.8b`, which is what `CLAUDE.md` §7 pins. Section 8 is about why that
matters and why it is still unresolved.

When the 4B is loaded with the GPU enabled the server reports
`offloaded 33/33 layers to GPU` and `Vulkan0 model buffer size = 2603.50 MiB`.
That line, not the absence of an error, is the confirmation that the GPU is in
use.

### Reference documents

| Reference | Shape | Character |
|---|---|---|
| deck | 15 slides, 15,182 B of converted markdown | dense OOXML: acronym tables, bullet fragments, very few full sentences |
| PDF | 2 pages, 4,360 B of converted markdown | an email thread; page 1 is 3,577 B and one unit, page 2 is 780 B |

The deck is 15,152 B before the PPTX soft-break fix and 15,182 B after it. Rows
taken before that fix are marked.

They disagree with each other, which is the reason both are used: a change
measured on only one of them looks like a win and is not. The PDF is also the
CONTROL for timing noise, because its page 1 is a single unit and therefore
becomes a byte-identical request at every detail level.

### How values are counted

Every "values" figure is the count of **raw strings the model returned that occur
verbatim in the source text**: after the hallucination filter, before
`engine.MergeSuggestions` and `engine.FoldValueFamilies`. It therefore OVERSTATES
the number of distinct Values a user would review, counting a name and the same
name with a suffix separately. It is used because it is comparable across rows,
every row being counted the same way. **Do not quote these as Value counts** in
code comments or in user-facing copy.

### Instruments

Two, and they are not interchangeable:

- **A throwaway harness** (sections 3, 4, 6, 7). Stdlib-only Go driving the real
  prompts through the real client, but setting its **own `num_predict`** rather
  than the application's reply cap. A row from it can report values for a slice
  the application would have reported as cut off. Read those rows as evidence
  about SLICE SIZE, model and reply format, not as a prediction of a run.
- **`backend/ollama/probe_live_test.go`** (sections 5 and 9), committed behind
  `//go:build live`, which drives `engine.ScanChunks`, `buildChatRequest` and
  `postChat`, so every number it prints is one the application would produce.
  Always run with `go test -count=1`: its inputs are environment variables the
  test cache does not hash, and a cached replay has already produced one wrong
  conclusion in this project.

---

## 2. What the benchmark established, in one page

1. **One request over a whole document returns nothing.** Four configurations,
   two models, two reply formats: all zero. This was a correctness defect, not a
   tuning problem, and it is what CHANGE-09 CR1 fixed.
2. **Recall depends on slice size, and the cliff is around one kilobyte on a
   small model.** Above about 2 KB the 0.8B returns nothing at all.
3. **Unit-aligned slicing beats byte chunking and beats one-slide-per-request.**
   The shipped code returns 118 to 156 values on the deck in 10 requests, against
   76 for 15 byte-agnostic per-slide requests and 0 for one request.
4. **The JSON Schema is not free and not a villain.** It wins on documents under
   about 500 B and on one dense page of prose, costs 2.4x the wall clock on the
   deck, and returns nothing at all on the 0.8B. Hence a per-call decision and a
   user setting, not a constant.
5. **The integrated GPU is worth about 1.2x, not the 2.2x first reported.** What
   it reliably changed was recall, not the clock.
6. **The detail level controls the REQUEST COUNT, not the wall clock.** On the
   model that finds anything, the two levels overlap in time.
7. **Parallel requests are not worth having here.** 1.01x against a default
   server configuration.
8. **The 0.8B build on this machine is not usable on the deck.** 2 values from 10
   requests, 9 silent. Whether that is the model or the local GGUF build is still
   unresolved.
9. **This machine's timing noise floor is large.** A byte-identical request took
   37.9 s and then 76.2 s. Any timing difference smaller than about 1.5x on a
   single run means nothing here.

---

## 3. One request over a whole document (the reported failure)

Throwaway harness, deck at 15,152 B (pre-PPTX-fix), CPU.

| request shape | format | model | wall clock | values |
|---|---|---|---|---:|
| whole document, 1 request | schema | 0.8B | 23 s | **0** |
| whole document, 1 request | json | 0.8B | 19 s | **0** |
| whole document, 1 request | json | 4B | 2 m 13 s | **0** |
| whole document, 1 request | schema | 4B | 5 m 31 s | **0** |
| whole document, 512-token cap | schema | 4B | 1 m 10 s | **0**, truncated |

The document is 15,152 B and fits an 8192-token context window comfortably, so
this is not a context overflow. It fails in two DIFFERENT ways, which is why one
defect produced two symptoms:

- On the **0.8B** the reply is a well-formed, completely empty object. No error,
  nothing to report, and the run says "0 suggestions", which is the same sentence
  a genuinely name-free document produces. This is what the user saw.
- On the **4B** the reply starts, runs past the reply cap and is cut mid-string,
  having degenerated into a repeat loop. Ollama returns `done_reason: "length"`,
  and the parser then reports malformed JSON rather than truncation.

**What fits a context window and what a model can still extract names from are
different questions.** Sizing a slice from the window was the defect.

---

## 4. Slice size and the recall cliff

Throwaway harness, deck (pre-fix), byte-sized chunks, CPU.

| chunk budget | chunks | 0.8B schema | 0.8B json |
|---|---:|---:|---:|
| 18,432 B (the old default, = `ContextSize * 3 * 3/4`) | 1 | 0 | 0 |
| 2,048 B | 12 | 0 | 0 |
| 1,024 B | 26 | 0 | **13** |
| 512 B | 50 | 0 | **21** |

The cliff sits between 1 KB and 2 KB on this model, which is where the shipped
`thorough` target of 1,200 B comes from.

At 512 B only **4 of 50** chunks produced anything, and those four carried all 21
values. **"Most requests return nothing" is therefore normal and is not by itself
evidence of a fault** — which is exactly why the run has to report the silent
count rather than leaving the user to infer it.

A 120-byte prefix of the same text returns names; a 200-byte prefix already
returns none with the schema on.

### One slide per request

| slicing | format | model | backend | requests | wall clock | values |
|---|---|---|---|---:|---|---:|
| one slide per request | schema | 4B | CPU | 15 | 8 m 56 s | 70 |
| one slide per request | json | 4B | CPU | 15 | 3 m 59 s | 76 |
| one slide per request | schema | 4B | GPU | 15 | 4 m 18 s | 71 |
| one slide per request | json | 4B | GPU | 15 | 1 m 48 s | 80 |

Compare with section 5: unit-aligned packing sends 10 requests rather than 15 and
finds more, because a slice is packed to a size rather than to a unit boundary.

---

## 5. The shipped code (the numbers that predict a real run)

`backend/ollama/probe_live_test.go`, `-count=1`, machine otherwise idle. Deck at
15,182 B (post-PPTX-fix). These supersede sections 3, 4, 6 and 7 as a prediction
of application behaviour.

### CPU (GPU dropped), 2026-08-19, JSON reply format

Deck, 10 requests at `thorough`, 6 at `faster`:

| model | level | requests | values | silent | wall clock | per request |
|---|---|---:|---:|---:|---|---|
| 4B | thorough | 10 | 118 | 1 | 2 m 09 s | 12.9 s |
| 4B | thorough (repeat) | 10 | 118 | 1 | 2 m 33 s | 15.3 s |
| 4B | faster | 6 | 119 | 0 | 3 m 01 s | 30.2 s |
| 4B | faster (repeat) | 6 | 119 | 0 | 2 m 07 s | 21.2 s |
| 0.8B | thorough | 10 | 2 | 9 | 1 m 01 s | 6.1 s |
| 0.8B | faster | 6 | 0 | 6 | 29 s | 4.9 s |

PDF, 2 requests at both levels:

| model | level | requests | values | silent | wall clock | per request |
|---|---|---:|---:|---:|---|---|
| 4B | thorough | 2 | 54 | 0 | 53 s | 26.4 s |
| 4B | faster | 2 | 54 | 0 | 1 m 31 s | 45.6 s |
| 0.8B | thorough | 2 | 3 | 0 | 14 s | 6.9 s |
| 0.8B | faster | 2 | 3 | 0 | 14 s | 6.9 s |

No request truncated. Densest reply 331 tokens (PDF page 1), densest on the deck
255, against the shipped 1,024-token cap.

### GPU (`OLLAMA_IGPU_ENABLE=1`), 2026-08-20

Server log confirmed `inference compute ... type=iGPU total="17.9 GiB"`,
`offloaded 33/33 layers to GPU`, `Vulkan0 model buffer size = 2603.50 MiB`, and
no `dropping integrated GPU` line.

Deck:

| model | level | format | requests | values | silent | truncated | wall clock | per request |
|---|---|---|---:|---:|---:|---:|---|---|
| 4B | thorough | json | 10 | 156 | 1 | 0 | 2 m 24 s (cold) | 14.4 s |
| 4B | thorough | json | 10 | 156 | 1 | 0 | 1 m 55 s (warm) | 11.5 s |
| 4B | faster | json | 6 | 152 | 0 | 0 | 1 m 41 s | 16.9 s |
| 4B | thorough | schema | 10 | 361 | 1 | **2** | 4 m 31 s | 27.1 s |
| 0.8B | thorough | json | 10 | 2 | 9 | 0 | 25 s | 2.5 s |

PDF:

| model | level | format | requests | values | silent | truncated | wall clock | per request |
|---|---|---|---:|---:|---:|---:|---|---|
| 4B | thorough | json | 2 | 57 | 0 | 0 | 47 s | 23.4 s |
| 4B | thorough | json | 2 | 57 | 0 | 0 | 41 s | 20.4 s |
| 0.8B | thorough | json | 2 | 3 | 0 | 0 | 5 s | 2.3 s |

"cold" means the model load is inside the first slice's time.

### What these rows say

- **The reported failure is fixed by more than the plan predicted.** 118 to 156
  values on the deck at default settings, against zero, and against 76 for
  one-slide-per-request. Fewer requests AND more values.
- **The GPU is about 1.2x.** Deck 2 m 21 s (CPU mean) to 1 m 55 s warm; PDF 53 s
  to 41-47 s. The COLD GPU run at 2 m 24 s falls INSIDE the CPU range, so on a
  single run the difference is barely outside the noise floor. **The 2.2x first
  reported came from the throwaway harness and does not reproduce with the
  committed instrument.**
- **What the GPU reliably changed is recall, not the clock**: 156 against 118 on
  the deck, 57 against 54 on the PDF. Greedy decoding is deterministic per
  BACKEND and not across backends, so this is expected rather than a fault. Per
  output token the GPU is clearly faster: roughly a third more output in roughly
  a fifth less time.
- **Determinism holds within a backend.** Both deck repeats gave identical
  per-slice `eval_count` and the identical 156; both PDF repeats gave identical
  340 and 71 and the identical 57. Only the wall clock moved.
- **The detail level controls the request count, not the time.** 10 against 6
  requests; 156 against 152 values. On CPU the two levels overlap
  (2 m 09 s-2 m 33 s against 2 m 07 s-3 m 01 s); on GPU `faster` happened to be
  quicker, and both differences are inside the noise band. Per request the larger
  slices cost proportionally MORE, so fewer requests buy back roughly what they
  cost. Request cost tracks neither prompt size nor reply length: on the deck an
  858 B slice took 19.6 s and a 3,380 B slice took 13.0 s in the same run.
- **Where the level does buy time, it costs everything.** On the 0.8B, `faster`
  is genuinely 2x quicker on the deck (29 s against 1 m 01 s) and finds nothing
  at all (0 against 2). That is the recall cliff of section 4, on the shipped
  code.
- **The schema is the one control with a large reproducible cost**: 2.4x the wall
  clock on the deck. Its 361 is **not** 361 distinct findings: two slices ran to
  the 1,024-token cap and one alone contributed 200, which is a degenerate repeat
  loop counted string by string. **With the schema on, the deck still busts the
  shipped cap**; the "nothing truncates" note above holds in JSON mode only, and
  raw value counts are not comparable across formats when truncation is in play.
- **The noise floor is the PDF's page 1.** It is 3,577 B and ONE unit, so it is
  its own slice at BOTH levels: the two CPU rows sent byte-identical prompts and
  got byte-identical replies, and the wall clock still differed by 72 %, 37.9 s
  against 76.2 s on that one request.

### Latency against the owner's targets

Targets: 1 to 5 pages or slides inside 20 s (30 s absolute maximum); a whole
document of the reference sizes at about 1 minute.

| scope | best measured | verdict |
|---|---|---|
| one slice | 4.7 s to 28.2 s depending on density | met for ordinary slices |
| 1 to 5 slides | 11.5 s per request on the deck | met only when the slides pack into one or two requests |
| whole PDF (2 pages) | 41 s | **met** |
| whole deck (15 slides) | 1 m 55 s | **missed by roughly 2x** |

The GPU has been tried and does not close the deck gap. What remains: accept it
with honest progress, a different model, or a further change order. Two candidate
optimisations were measured and rejected (section 7).

---

## 6. Reply format: schema against loose JSON

Throwaway harness except where section 5 supersedes it. This table is the reason
the format is a per-call decision and a user setting.

| document | model | backend | schema | json |
|---|---|---|---:|---:|
| deck, per slide | 4B | GPU | 71 in 4 m 18 s | **80 in 1 m 48 s** |
| deck, per slide | 4B | CPU | 70 in 8 m 56 s | **76 in 3 m 59 s** |
| deck, 1 KB chunks | 0.8B | CPU | 0 | **13** |
| deck, 512 B chunks | 0.8B | CPU | 0 (50 of 50 silent) | **21** |
| PDF page 1 | 4B | CPU | **77** | 45 |
| PDF page 1 | 4B | GPU | 53 | 48 |
| PDF page 2 | 4B | CPU and GPU | 9 | 9 |
| PDF whole | 4B | GPU | 35 | 34 |
| PDF page 1 | 0.8B | CPU | 0 | 2 |
| repo fixtures (~400 B each, 5 of them) | 0.8B | CPU | 2, 2, 2, 2, 3 | 1, 2, 2, 1, 3 |

Read carefully, because it does not say one single thing:

- The schema's large win (77 against 45) appears in **one** row and does not
  reproduce on the same document and model on the other backend (53 against 48).
  Greedy decoding is deterministic per backend, not across backends, so that row
  is backend luck rather than a property of the grammar.
- On documents under about 500 B the schema is equal or slightly better, twice.
- It costs 2.2x to 2.4x the wall clock on the deck, consistently, because it
  forces all seven category arrays to be emitted whether or not they have
  content.
- On the 0.8B it is catastrophic at every slice size tested, from 120 B to 18 KB.

**Why it fails on a small model**, established separately: with `required: []` the
0.8B produced long repetitive invented lists, which is worse than silence; a
single-key schema worked fine. So the problem is the seven-REQUIRED-arrays shape
making a small model pad the categories it has nothing for, not schema
constraint as such. Seven calls per slice is not a trade worth making.

The repo's own fixtures are the only corpus where the schema is consistently at
least as good, and every one of them is under 500 B. **That is a warning about
what this repository's test corpus can tell you about model behaviour: it is far
smaller than any real document.**

---

## 7. Measured and rejected

Recorded so a future session does not spend an afternoon re-discovering them.

| Idea | Measurement | Verdict |
|---|---|---|
| **Parallel requests** | 1.63x on CPU with `OLLAMA_NUM_PARALLEL=4`; 1.13x once the GPU is on; **1.01x against a default server configuration**, which serialises | Rejected. Needs BOTH an application change and a user configuration change, so documentation alone cannot deliver it, against the cost of a concurrent chunk loop: progress ordering, mid-flight cancellation, partial merges, per-request error attribution |
| **Retry a silent request without the schema** | Over the whole deck: 26 requests became 52, 1 m 20 s became 2 m 03 s, values **identical** (13) | Rejected. At 512 B, 46 of 50 requests are legitimately silent, so the rule pays double for almost every request |
| **512 B slices as the default** | Finds more on the 0.8B (21 against 13) at nearly twice the time, and 50 requests instead of 15 on a 15-slide deck | Rejected as a default |
| **A "whole document in one request" level** | Section 3: zero on both models and both formats | Rejected. A setting whose measured outcome is "finds nothing" is a broken switch |
| **Raising the reply cap further** | At 1,024 tokens two deck slices ran to the new cap (106 s, 109 s); at 2,048 both exceeded the request timeout and returned NOTHING | Rejected. A degenerate reply consumes any cap it is given, and a cap the request window cannot deliver turns a salvageable cut-off reply into a timeout |
| **Skipping units with no candidate proper noun** | Would have saved roughly 25 % on the deck (4 of 15 slides returned nothing) | Not taken. A skipped unit is one the model never sees and the recall risk is invisible in the result. If latency must come down, measure this FIRST, with the recall cost quantified on both documents |
| **Making the classification pass optional** | Not measured | Untouched. Measure before reaching for unit skipping |

---

## 8. The unresolved question: the pinned model

`CLAUDE.md` §7 pins `qwen3.5:0.8b` on a CHANGE-08 measurement of **16 names on
page 1 of the reference PDF with the schema on**.

**That measurement does not reproduce.** On this machine's locally imported 0.8B,
the same page with the same request settings returns **0 with the schema and 2
without**, while the 4B on the same page returns 45 to 77. On the deck the 0.8B
returns 2 values from 10 requests with 9 silent, on both backends, with every
silent slice returning the same 37-token empty object.

Two explanations fit the evidence equally well:

1. The CHANGE-08 benchmark ran against a **different GGUF file** for the same
   nominal model (the library tag, rather than a third-party import).
2. The locally imported build is simply **poor**.

Settling it needs the library tag, and **it could not be obtained on this
network**: `ollama pull qwen3.5:0.8b` fails with `max retries exceeded: EOF`, and
a direct request to `registry.ollama.ai` resolves the host and is then cut at the
TLS handshake, with no system proxy configured and Ollama's own `HTTP_PROXY` and
`HTTPS_PROXY` empty. That is a corporate egress filter.

**So the pin is unchanged**, and this is the outstanding action:

> Pull `qwen3.5:0.8b` from a network that permits it, or configure a proxy for
> the Ollama service, then run `backend/ollama/probe_live_test.go` with
> `-count=1` against BOTH reference documents and compare with the 0.8B rows in
> section 5. If the library tag reproduces 16 names, §7's pin is sound and the
> README should warn that a third-party GGUF import of the same nominal model can
> behave very differently. If it does not, §7's model row needs revisiting in its
> own change order, citing this file.

The deck matters more than the PDF page the original plan named: the deck is
where the local build returns 2 values against the 4B's 118 to 156.

---

## 9. Reproducing any of this

```
$env:PROBE_DOC    = "C:\path\to\document.pptx"     # required
$env:PROBE_MODEL  = "Qwen3.5-4B-Q4_K_M:latest"     # required
$env:PROBE_FORMAT = "json"                         # "json" (default) or "schema"
$env:PROBE_LEVEL  = "thorough"                     # "thorough" (default) or "faster"
go test -tags=live -count=1 -v -timeout 60m ./backend/ollama/ -run TestLiveProbe
```

Rules learned the hard way:

- **`-count=1` is not optional.** The test's inputs are environment variables the
  build cache does not hash, so without it a second run replays the first run's
  numbers. This has already produced one wrong conclusion.
- **Run nothing else on the machine.** The noise floor is large enough
  (section 5) to swallow any real difference under about 1.5x.
- **Note whether the model was already loaded.** A cold first slice carries the
  load, which was 13 s of the deck's cold run.
- **Check the server log, not the absence of an error**, to know which backend
  you measured: `offloaded N/N layers to GPU` against
  `dropping integrated GPU`.
- **Compare like with like.** The throwaway-harness rows and the committed-probe
  rows are not interchangeable (section 1, Instruments).
- The deck's byte count changed with the PPTX soft-break fix (15,152 to 15,182),
  so pre-fix and post-fix value counts differ slightly for that reason alone.

To measure a different Ollama environment without disturbing the user's own
server, start a second one on another port with the environment you want
(`OLLAMA_HOST=127.0.0.1:11435 OLLAMA_IGPU_ENABLE=1 ollama serve`) and point the
probe at it. It shares the model blobs, so nothing is downloaded.

---

## 10. Where the conclusions ended up

| Conclusion | Now lives in |
|---|---|
| Slices are unit-aligned and sized by the detail level, never by the context window | `CLAUDE.md` §5; `backend/engine/aichunks.go` |
| Two detail levels, targets 1,200 B and 4,000 B, thorough the default | `backend/engine/aichunks.go`; `CLAUDE.md` §5 |
| The reply format is per call: schema for classification, the user's choice for discovery, defaulting to loose JSON | `CLAUDE.md` §7 and §8; `backend/ollama/client.go discoveryFormat` |
| A run reports requests, silent requests, cut-off requests and measured seconds each | `frontend/BRIDGE.md`; `backend/app_detect.go` |
| The reply cap is 1,024, coupled to the request timeout | `backend/ollama/client.go` |
| `OLLAMA_IGPU_ENABLE=1` is worth about 1.2x and is documentation, never a code constant or a timing-based warning | `CLAUDE.md` §8; `README.md`; `frontend/docs/index.html` |
| Parallel requests rejected | `docs/CHANGE-09.md` decision 8 |
| The effective model is always one the probe just saw | `frontend/BRIDGE.md`; `backend/app.go resolveModel` |
| The §7 model pin is unchanged pending the spot-check | section 8 above |
