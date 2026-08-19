# CHANGE-08 — Local AI latency: fix the request, not the model

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0, pure Go + Wails v2, no CGo, no npm). It holds **one
self-contained implementation section per change request (CR1 to CR7)**,
followed by the **decisions taken**, a **conflict analysis**, the **recommended
execution sequence** and the **acceptance criteria**.

Every CR below comes from a measured investigation of the Local AI discovery
route on the owner's own corporate laptop. The route took **about two minutes**
to scan ONE page of a document, against a target of **under 20 seconds**. The
investigation began as a model question ("is there a lighter model, already
fine-tuned for finding the values to anonymise?") and ended somewhere else: the
model was never the main problem, **the request the application sends to Ollama
was**. CR1 to CR4 fix the request, CR5 fixes a scope leak that doubles the work, CR6
fixes a confidence field that was declared and never assigned, and CR7 stops the
model load being paid on every cold call.

Ground rules for this change order (unchanged from `CLAUDE.md`):

- `CLAUDE.md` and the two subtree charters remain authoritative. Nothing here
  weakens the local-only guarantee, the one-file Ollama boundary, or the
  zero-CGo rule. No new dependency: every change is standard library.
- **A change is not finished until its tests move with it** (`CLAUDE.md` §6).
  Each CR names the tests to add, update and delete. Both suites gate:
  `go test ./...` and `node --test "frontend/**/*.test.js"`.
- User-visible copy never contains em dashes (`copy_guard_test.go`,
  `frontend/copy.test.js`). That guard walks `backend/` and `.`, so it covers
  the PROMPT strings this order touches.
- The parity guards are load-bearing (`category_parity_test.go`,
  `detection_parity_test.go`, `value_shape_test.go`, and in this order
  especially `TestPromptsAndParserAgreeOnTheCategoryKeys`).
- Comments explain intent in the present tense. Do not write "used to be" or
  "CR2 changed this" into the code.
- **This order changes two authoritative rules in `CLAUDE.md` itself** (§7's
  pinned model row and §8's `"format":"json"` constant). Those edits are part of
  CR2 and CR4 and are not optional: leaving them stale would make the charter
  describe a contract the code no longer honours.
- **No session-file shape change.** `SessionVersion` stays **8**.

---

## 0. Cold-start context for the implementing session

Read this section first if you are picking this document up with no
conversation history. It is everything the diagnosing session established, and
it is all measured rather than reasoned.

### Where the work stands

| Fact | Value |
|---|---|
| Repository | `Rosca75/doc-anonymiser`, module path `doc-anonymiser` |
| Branch to develop and push on | `claude/lightweight-doc-anonymization-model-ept22z` |
| Its current head | this document, and nothing else. No code has changed yet. |
| Suites | `go test ./...` and `node --test "frontend/**/*.test.js"`, both must be green |
| Integration tier | `go test -tags=integration ./...` (the mock-Ollama detection flow lives here) |
| Audit | `task audit` (go-task, no make) |
| Owner's Ollama | 0.32.14 on Windows, CPU-only corporate laptop |

### The owner's report, in substance

Scoping the Local AI to **page 1** of a two-page PDF (an Outlook email thread)
took about two minutes. The owner tried three models looking for a faster one
and found no improvement, which is what prompted the investigation.

### What was established by measurement (do not re-derive)

A standalone benchmark was written for this investigation (stdlib-only Go,
reproducing the real discovery prompt and reading back the timing fields Ollama
returns). It lives outside the repository, in the diagnosing session's
scratchpad; **it is not part of this change order** and does not need to be
committed. Its findings are:

- **The input is small.** Page 1 of the reference document is **3,577 bytes,
  about 950 tokens**, measured through the app's own `convert.PDFWithPages`.
  Nothing about the input justifies two minutes.
- **Thinking left on is the catastrophe.** Qwen3.5 is a reasoning model and the
  application never sends a `think` field, so thinking is ON (Ollama's docs:
  "Thinking is enabled by default in the CLI and API for supported models").
  Critically, **`format` does NOT suppress it**: per ollama/ollama PR #15901,
  format application "occurs after the thinking-to-content transition
  completes, preserving normal thinking generation". Every reasoning token is
  generated free-running, at full cost, and lands in `message.thinking` while
  the parser reads `message.content`.

  Measured on `qwen3.5:0.8b` (Q8_0), one page, warm model:

  | request | total | generated tokens | hidden reasoning | names found | stop reason |
  |---|---|---|---|---|---|
  | as the app sends it today | **258.7s** | 7,214 | 27,228 bytes | **0** | truncated |
  | + `think:false` | 2.8s | 73 | none | 2 | stop |
  | + greedy sampling | 2.3s | 67 | none | 6 | stop |
  | + JSON schema | 5.4s | 121 | none | **16** | stop |

  On `qwen3.5:4b` (Q4_K_M) the as-shipped call did not finish inside a 90
  second cap at all. **This is the whole latency problem in one row.**
- **`think:false` must be TOP LEVEL, not inside `options`.** Ollama's server-side
  `Options` is a `map[string]any`, so an unknown key there is silently dropped;
  that dropped key is ollama/ollama issue #14793, where thinking consumed the
  entire `num_predict` budget and returned empty content.
- **A JSON Schema in `format` is a RECALL win, not only a safety one.** Adding it
  took the 0.8B from 6 names to 16 on the same text, because it forces every
  category array to be present. It costs effectively nothing in latency: the
  llguidance token-mask measurement is ~50 microseconds average against a CPU
  decode step of tens of milliseconds, and a stricter grammar lets more tokens be
  fast-forwarded rather than sampled.
- **The shipped sampling defaults fight the task.** The `qwen3.5` tags ship
  `temperature 1`, `top_k 20`, `top_p 0.95` and **`presence_penalty 1.5`**. A
  presence penalty punishes re-emitting a token that already appeared, and
  copying names verbatim out of the source IS re-emitting tokens that already
  appeared. The application sends no sampling options at all (`chatOptions`
  carries only `num_ctx`), so it inherits every one of them.
- **Model choice, on the numbers.** Both Apache-2.0:

  | model | warm per call | generation rate | names (English sample) | est. real page-1 run (two calls) |
  |---|---|---|---|---|
  | `qwen3.5:0.8b` (Q8_0) | **5.3s** | ~30 tok/s | 16 | **~10s** |
  | `qwen3.5:4b` (Q4_K_M) | 12.2s | ~10 tok/s | 17 | ~24s |

  The 4B is 2.3x slower for one extra name and busts the 20s target once the
  second call is counted. **Default to `qwen3.5:0.8b`.**
- **`num_ctx: 8192` is NOT the bottleneck.** llama.cpp does pre-allocate the KV
  cache for the whole window, but at this model size that is tens of megabytes.
  Measured cold prefill was 223 tok/s (0.8B) and 3,262 tok/s (4B). Do not shrink
  it: below the worst-case prompt it would truncate, which is a correctness bug.
- **Grammar overhead and quantisation, for the record.** BF16 has no fast CPU
  dot-product kernel without AVX512-BF16 (ggml upconverts every weight to FP32
  mid-loop; see ggml-org/llama.cpp issue #7182), so a `-bf16` tag runs several
  times slower than a K-quant on this hardware. An early BF16-vs-K-quant
  comparison in this investigation was later found to be **mislabelled** (both
  Ollama tags resolved to Qwen2.5-3B-Q5_K_M), so those numbers are VOID and are
  recorded here only so nobody re-reads them as evidence. The principle stands
  and CR2 writes it into `CLAUDE.md` §7 as a pinning rule.

### The two structural bugs the investigation surfaced

Both were read out of the code, not guessed.

- **The page scope leaks (CR5).** `App.refineCategories`
  (`backend/app_detect.go:255`, called from `RunDetection` at
  `backend/app_detect.go:214`) makes a **second** Ollama call
  (`Client.ClassifySuggestions`) that re-files everything Smart detection found,
  and it takes **no `aiScope`**. So scoping to page 1 narrows the discovery call
  and leaves this one reading the whole batch. Measured on the reference
  document, reproducing the real payloads offline:

  ```
  CALL 1  discovery, scoped to page 1
          user msg   3577 bytes  (~966 tokens)
          + system   1157 bytes  (~312 tokens)

  CALL 2  classification, NOT scoped: every Smart suggestion in the batch
          smart suggestions: 25
          user msg   6984 bytes  (~1887 tokens)
          + system   1269 bytes  (~342 tokens)

  TOTAL prompt tokens for a "page 1" run: ~3510
    of which the page the user selected: ~966 (28%)
  ```

  The unscoped call is the LARGER of the two. The UI already promises otherwise:
  `frontend/copy.js:333` reads "The local AI reads only what you point it at.
  Scanning one document, or a few pages of one, keeps a small model focused and
  the pass quick."

  The same measurement showed two compounding wastes in that payload: **279
  bytes of prompt per name classified** (each of the 25 suggestions carries up to
  `maxSuggestionContexts` = 3 snippets of +/-60 runes, largely re-quoting the
  same email header), and **9 of the 25 are spellings of another**, because
  `engine.FoldValueFamilies` runs at `backend/app_detect.go:226`, AFTER
  `refineCategories` at :214. Measured effect of each remedy:

  | variant | ~tokens | change |
  |---|---:|---:|
  | as shipped today | 3510 | - |
  | fold families before classify | 2867 | -18% |
  | + 1 context snippet each | 2302 | -34% |
  | + snippet trimmed to 40 runes | 1916 | -45% |
  | classify scoped to page 1 too | 1769 | -50% |
  | no classify pass at all | 1279 | -64% |

- **`ConfidenceLLMDefault` is declared and never assigned (CR6).**
  `engine.ConfidenceLLMDefault` = 0.8 (`backend/engine/pii.go:487`) is
  documented at `backend/engine/values.go:81` as what a Local AI Value carries.
  Nothing outside `backend/engine/confidence_test.go` ever assigns it:
  `parseSuggestionJSON` sets only `Category`, `MainText`, `Count: 1`
  (`backend/ollama/client.go:580`), and `valueFromSuggestion`
  (`frontend/state.js:1941`) copies `category`, `mainText`, `spellings`,
  `discoveryMethods`, `evidence` and not `confidence`. `engine.valueConfidence`
  (`backend/engine/values.go:313`) then reads 0 as `ConfidenceManualDefault`
  (0.95). So an accepted AI suggestion is scored as if the user had typed it,
  and raising **Minimum confidence** does not do what `values.go:81`,
  `frontend/copy.js:197` and `confidenceEffect()`
  (`frontend/views/identifyrail.js:580`, "every detection scores at least 80")
  all promise.

### Decisions the owner has already approved

1. **Default to `qwen3.5:0.8b`**, on the measured 5.3s / 16 names result, with a
   **French-quality spot-check before shipping** (see the acceptance criteria).
   Both candidates are Apache-2.0; the 4B is not worth 2.3x the latency for one
   extra name on English text.
2. **Ollama only.** No second runtime. The encoder-NER options
   (GLiNER, `openai/privacy-filter`) cannot be served by Ollama at all, because
   GGUF is a format for causal generative models; they would need the P4
   ONNX-in-WebView path, which is explicitly out of scope for this order and
   stays the recorded fallback.
3. **Commercial-safe licences only.** This rules out a family of otherwise
   strong PII models (CC-BY-NC and CC-BY-NC-ND).
4. **No session-shape change.** `SessionVersion` stays 8.

### A licence problem this order also closes

`CLAUDE.md` §7 currently pins `qwen2.5:3b-instruct` as the default model.
**Qwen2.5-3B is released under the Qwen Research License, not Apache-2.0** — it
is the exception in the Qwen2.5 family, whose other sizes are Apache-2.0. For a
tool that processes client documents at a professional-services firm that is a
compliance problem independent of speed. CR2 replaces it with an Apache-2.0
model, which resolves it as a side effect. The implementer should **verify the
licence on the model page** rather than take this paragraph on faith, and say so
if it turns out otherwise.

### Recommended session setup

- Model **Opus** (or the latest available), reasoning effort **high**. CR1 to
  CR4 all edit `backend/ollama/client.go` and its test, so they are one unit of
  work; CR5 and CR6 reach into `backend/app_detect.go` and across the bridge
  into `frontend/state.js`.
- Do NOT split CR1 to CR4 across parallel sessions: they touch the same two
  files and the same `chatReplyServer` assertion helper.
- Commit per CR (or CR1-CR4 as one commit if that reads better in the diff),
  with the CR number in the message body but never in a code comment
  (`CLAUDE.md` §6).

### Paste-ready opening prompt for the fresh session

> Read `CLAUDE.md`, `backend/CLAUDE.md`, `frontend/CLAUDE.md`,
> `frontend/BRIDGE.md` and `docs/TESTING.md`, then `docs/CHANGE-08.md` in full.
> You are on branch `claude/lightweight-doc-anonymization-model-ept22z`, which
> currently holds only that plan. Implement CHANGE-08 in the order its
> "Recommended order" section gives. For each CR: write or update the tests
> named in the CR in the SAME commit as the behaviour, then run
> `go test ./...`, `go test -tags=integration ./...` and
> `node --test "frontend/**/*.test.js"`. Push to
> `claude/lightweight-doc-anonymization-model-ept22z`. Note that CR2 and CR4
> edit authoritative rules in `CLAUDE.md` itself (§7 and §8); those edits are
> required, not optional. If a decision in the plan turns out to be wrong once
> you are in the code, stop and tell me rather than inventing a third option.

---

## CR1 — Stop paying for hidden reasoning: `think:false`, top level

This is the single highest-value change in the order: 258.7s to 2.8s on the
measured case, and the difference between zero usable names and a working pass.

### Current behaviour

`chatRequest` (`backend/ollama/client.go:200`) carries `model`, `messages`,
`format`, `stream` and an optional `options`. There is no `think` field, so
Ollama applies its default, which for a reasoning-capable model is thinking ON.
`Chat` (`backend/ollama/client.go:229`) builds every request, for both the
discovery call (`client.go:353`) and the classification call (`client.go:465`).

### Change

`backend/ollama/client.go`:

- Add to `chatRequest`, at the TOP LEVEL of the struct, beside `Format` and not
  inside `chatOptions`:

  ```go
  // Think disables a reasoning model's hidden reasoning. It is a TOP-LEVEL
  // field because Ollama's own options object is a map: a key it does not
  // recognise there is dropped in silence, so a think flag nested in options
  // reads as "set" on this side and never arrives.
  //
  // It is load-bearing rather than a tuning knob. Setting format does NOT stop
  // a reasoning model reasoning: the grammar is applied only once the model
  // transitions from thinking to content, so with thinking on the whole
  // reasoning trace is generated first, unconstrained, at full cost, and
  // returned in a field this client does not even read. On a small model asked
  // to extract names from one page that trace ran to thousands of tokens and
  // exhausted the reply budget, so the JSON never arrived.
  Think *bool `json:"think,omitempty"`
  ```

  A pointer, so "unset" and "deliberately false" stay distinct and an omitted
  field means "let the model decide", consistent with how `Options` already
  works.
- Set it to `false` in `Chat`, so BOTH callers inherit it and neither has to
  remember. Extraction never wants reasoning: the answer is a transcription of
  text already in the prompt.
- Add `Thinking string` with `json:"thinking,omitempty"` to `chatMessage`
  (`client.go:216`) so a reply that still carries reasoning can be SEEN rather
  than silently discarded. `omitempty` matters because the same struct is used
  for the messages sent; without it every outgoing message carries an empty
  `thinking` key.

> Implementer note on Ollama versions: `think:false` combined with `format` only
> behaves correctly on an Ollama carrying ollama/ollama PR #15901 (merged 7 July
> 2026). On older builds the pair could return unformatted text. The owner's
> machine reports 0.32.14, which is well past it. Record the floor in the §7
> table (CR2) rather than adding a runtime version gate: the existing
> `ErrTooOld` path already covers the "Ollama too old" class of failure, and a
> second version check would be a second thing to keep true.

### Tests

`backend/ollama/client_test.go`:

- UPDATE `chatReplyServer` (`client_test.go:32`), which currently asserts only
  `format:json` and `stream:false`. Add: **`think` must be present at the top
  level of the decoded body and be `false`**, and it must NOT appear inside
  `options`. Because this helper backs most of the file's tests, that one
  assertion covers every call path at once, which is exactly why it belongs
  there rather than in a new test.
- ADD `TestChatThinkIsTopLevelAndFalse`: decode the body, assert
  `req["think"] == false`, and assert `req["options"]` has no `think` key. State
  the reason in the test name and a comment: nested in options it is dropped in
  silence, so this test is the guard against a plausible-looking regression that
  no other test would catch.
- ADD `TestDiscoverIgnoresAThinkingReply`: a mock `/api/chat` returns a message
  with BOTH a non-empty `thinking` field and valid JSON in `content`; the parsed
  suggestions must come from `content` only, proving the parser reads the right
  field.

---

## CR2 — The default model: `qwen3.5:0.8b`, and the pinning rules around it

### Current behaviour

`DefaultModel = "qwen2.5:3b-instruct"` (`backend/ollama/client.go:38`). The
constant is read in exactly three places, all of which already do the right
thing: `New()` seeds `Client.Model` (`client.go:106`), the "no models installed"
detail builds an `ollama pull %s` hint (`client.go` ~179), and
`defaultSettings()` seeds `Settings.Model` (`backend/app.go:237`). The frontend
never hardcodes a name: `frontend/state.js:117` is `model: ""`, and
`ApplySettings` overwrites only when non-empty (`backend/app.go:612`), so an
empty frontend value correctly leaves the Go default in place.

### Change

- `backend/ollama/client.go:38`: change the constant to `qwen3.5:0.8b`. Nothing
  else in Go needs touching, and that is the point of the existing shape.
- Update the prose references, which are not code paths but are read by users
  and by CI release notes:
  - `CLAUDE.md:568` (§7 "Default Ollama model" row)
  - `backend/CLAUDE.md:275`
  - `README.md:115` (the `ollama pull` line)
  - `.github/workflows/release.yml:85`, `:152`, `:204`
- Add two rows / notes to the `CLAUDE.md` §7 pinned table, because both are
  facts a future session would otherwise have to rediscover the hard way:
  - **A minimum Ollama version**, with the reason: `think:false` combined with
    `format` needs ollama/ollama PR #15901.
  - **A quantisation pinning rule**: model tags must be a K-quant or `Q8_0`,
    never `-bf16` or `-f16`. Reason, in one sentence: BF16 has no fast CPU
    dot-product kernel without AVX512-BF16, so ggml converts every weight to
    FP32 as it goes and the model runs several times slower on the target
    laptop. Note that `qwen3.5:0.8b` ships as `Q8_0` and `qwen3.5:4b` as
    `Q4_K_M`, so the plain tags are already correct and the rule exists to stop
    someone "upgrading" to a BF16 build for quality.
- `frontend/docs/index.html` describes the Local AI route without naming a
  model, so the bundled user documentation needs **no** change. Verify that
  before concluding it (`grep -i qwen frontend/docs/`).

### Tests

- UPDATE the pinned model-name literals. They are in three files and they are
  assertions, not incidental strings, so update them deliberately rather than
  with a blind find-and-replace:
  - `backend/ollama/client_test.go:36`, `:78`, `:81` (`TestProbeHappyPath`
    asserts the first returned model name, and `chatReplyServer`'s `/api/tags`
    stub names it)
  - `backend/app_validation_integration_test.go:35`
  - `backend/engine/session_test.go:113`, `:284`
- No new test for the constant itself: a test asserting
  `DefaultModel == "qwen3.5:0.8b"` would restate the line it guards and would
  have to be edited every time the default legitimately moves. What deserves a
  guard is the shape that makes the default swappable, and `ApplySettings`
  already has that coverage.

---

## CR3 — Sampling built for extraction, not for chat

### Current behaviour

`chatOptions` (`backend/ollama/client.go:212`) carries `num_ctx` and nothing
else, so every other sampling parameter comes from whatever the model tag
shipped with. For the `qwen3.5` tags that is `temperature 1`, `top_k 20`,
`top_p 0.95` and `presence_penalty 1.5`.

### Change

`backend/ollama/client.go`:

- Extend `chatOptions` with pointer fields, and say in the comment why they are
  pointers:

  ```go
  // chatOptions mirrors the Ollama options object.
  //
  // Every field is a pointer because "unset" and "deliberately zero" are
  // different requests and a plain float with omitempty cannot tell them
  // apart: temperature 0 is exactly the value extraction wants, and it would
  // marshal away.
  type chatOptions struct {
      NumCtx     int `json:"num_ctx,omitempty"`
      NumPredict int `json:"num_predict,omitempty"`
      // Extraction is a transcription task, not a creative one: the answer is
      // text already present in the prompt. Greedy decoding makes two runs of
      // one document agree, which is what lets the review gate mean something.
      Temperature *float64 `json:"temperature,omitempty"`
      TopK        *int     `json:"top_k,omitempty"`
      TopP        *float64 `json:"top_p,omitempty"`
      Seed        *int     `json:"seed,omitempty"`
      // A presence penalty punishes re-emitting a token that already appeared.
      // Copying a name verbatim out of the document IS re-emitting tokens that
      // already appeared, so the model tags' shipped default of 1.5 works
      // against the task and lengthens the reply. Both penalties are pinned
      // neutral rather than left to the tag.
      PresencePenalty *float64 `json:"presence_penalty,omitempty"`
      RepeatPenalty   *float64 `json:"repeat_penalty,omitempty"`
  }
  ```

- Send `temperature 0`, `top_k 1`, `top_p 1`, `presence_penalty 0`,
  `repeat_penalty 1` and a fixed `seed` on every call, set in `Chat` alongside
  CR1's `Think` so both callers inherit them.
- Add a `num_predict` ceiling (512 is the measured-safe value: the schema-
  constrained replies ran 88 to 121 tokens) so a degenerate reply cannot hold
  the 120 second `chatClient` timeout open. Guard it with a comment: this cap is
  only safe BECAUSE `think:false` is set, since with thinking on the reasoning
  trace spends the budget first and the JSON comes back empty or truncated.
- Because `Chat` now has a settled request shape rather than four literals,
  extract a small `buildChatRequest(model, systemPrompt, userPrompt string,
  format any) chatRequest` helper if that reads more clearly than growing
  `Chat`. Either is acceptable; what matters is that the discovery and
  classification calls cannot drift apart.

Two things NOT to change, both deliberately:

- **`ContextSize` / `DefaultContextSize` stays 8192.** Measured cold prefill was
  223 tok/s (0.8B) and 3,262 tok/s (4B); the window is not the bottleneck.
  Shrinking it below the worst-case prompt would truncate, which is a
  correctness bug, not a speed-up.
- **`chatClient`'s 120 second timeout stays.** With `num_predict` capped it
  becomes a backstop rather than the thing the user waits on.

### Tests

`backend/ollama/client_test.go`:

- RENAME and EXTEND `TestChatNumCtx` (`client_test.go:308`) into
  `TestChatOptionsContract`: it already proves `num_ctx` travels when set and
  the `options` object is absent when `ContextSize == 0`. Add assertions that
  `temperature` is present **and equal to 0** (the pointer regression this test
  exists to catch), that `top_k`, `top_p`, `presence_penalty`, `repeat_penalty`
  and `seed` carry the pinned values, and that `num_predict` is set.
- ADD `TestChatTemperatureZeroIsNotDroppedByOmitempty` if the assertion above
  reads awkwardly inside the bigger test. State the failure it prevents: a
  plain `float64` with `omitempty` marshals 0 away, so the model would silently
  keep sampling at the tag's `temperature 1`.
- KEEP `TestChat400ContextOverflow` (`client_test.go:263`) and
  `TestChunkCap` (`:398`) unchanged: `num_ctx` and the chunk budget are
  untouched by this CR, and these tests are what proves it.

---

## CR4 — Constrain the reply with a JSON Schema built from `promptCategories`

Measured as a **recall** win as much as a robustness one: 6 names to 16 on the
same text, because the schema forces every category array to be present.

### Current behaviour

`chatRequest.Format` is a `string` and `Chat` sets it to `"json"`
unconditionally (`backend/ollama/client.go:239`). That is Ollama's loose JSON
mode: the reply is valid JSON but its SHAPE is unconstrained. The parser absorbs
the consequences: `parseSuggestionJSON` (`client.go:542`) strips accidental code
fences (`client.go` ~545), tolerates a bare string where a list was asked for
(`client.go` ~566), and silently ignores unknown keys. Each of those tolerances
is a shape the model got wrong.

### Change

`backend/ollama/client.go`:

- Change `chatRequest.Format` from `string` to `any`, so it carries either the
  string `"json"` or a schema object. Document why the type widened: the field
  is one of two things, and a schema is the one the callers now send.
- Add a `suggestionSchema()` helper that BUILDS the schema from
  `promptCategories` (`client.go:523`) rather than writing it out as a literal:

  ```go
  // suggestionSchema is the reply shape the model is CONSTRAINED to, rather than
  // merely asked for: an object whose every property is an array of strings,
  // with all seven required.
  //
  // It is derived from promptCategories rather than written out, so the schema
  // cannot fall out of step with the parser and the prompts. That keeps
  // TestPromptsAndParserAgreeOnTheCategoryKeys covering it: a new engine
  // category added to the prompts but not here would otherwise be a category
  // the model is forbidden to fill, which no other test would notice.
  //
  // The schema is FLAT on purpose. Sub-4B models degrade badly on schemas
  // containing $defs or $ref, echoing the schema's own structure back in place
  // of the extracted values, so there are no reusable definitions here even
  // though the seven properties are identical.
  ```

  Shape: `{"type":"object","properties":{<cat>:{"type":"array","items":
  {"type":"string"}}, ...},"required":[all seven]}`.
- Pass it as `Format` from both the discovery call (`client.go:353`) and the
  classification call (`client.go:465`); both expect the identical seven-key
  shape.
- **Keep** `parseSuggestionJSON`'s tolerances. They cost nothing, they still
  protect the `format:"json"` fallback path, and deleting them in the same
  change that adds the schema would confuse "the model no longer does this" with
  "we no longer cope with it". Keep the fence-stripping too.
- Keep the key list spelled out in the prompt text. Ollama's own guidance is to
  ground the model with the schema in the prompt as well as in `format`, and
  both prompts already do exactly that (`client.go:322`, `client.go:425`), so
  this is a no-op that should not be "tidied away".

**`CLAUDE.md` §8 must change with this.** Its second bullet currently reads
`set "format":"json" in the request body` (`CLAUDE.md:586`), and the §7 Ollama
HTTP API row repeats it (`CLAUDE.md:567`). Update both to say the request sends
a **JSON Schema** in `format`, derived from the category list, and keep the
existing sentence about the three lists being held to each other, which is
still exactly why the guard test matters.

### Tests

`backend/ollama/client_test.go`:

- UPDATE `chatReplyServer` (`client_test.go:32`): its current assertion
  `req["format"] != "json"` will now FAIL, because `format` is an object. Change
  it to decode `format` and assert the schema's shape: `type == "object"`, the
  seven `promptCategories` keys present as `array`-of-`string` properties, all
  seven listed in `required`, and **no `$defs` or `$ref` anywhere**. That last
  assertion is the regression guard the CR exists for, so give it its own
  failure message naming the reason (small models echo `$defs` schemas back
  instead of filling them).
- EXTEND `TestPromptsAndParserAgreeOnTheCategoryKeys` (`client_test.go:587`)
  with a THIRD direction. It currently holds the prompts and
  `promptCategories` to `engine.AllValueCategories`; add: every
  `promptCategories` key appears as a property in `suggestionSchema()` and in
  its `required` list. This is what makes the schema un-forgettable when a
  category is added.
- KEEP `TestDiscoverStripsCodeFences` (`client_test.go:156`) and
  `TestParseEntityJSONAcceptsEveryKeyAndIgnoresUnknownOnes` (`:616`): the
  tolerances stay, so their coverage stays.

---

## CR5 — The page scope must narrow BOTH model calls

The user's page selection currently narrows the smaller of the two calls. This
CR is what makes "scope to page 1" mean what `frontend/copy.js:333` says it
means, and it is worth about half the prompt tokens of a scoped run.

### Current behaviour

`RunDetection` (`backend/app_detect.go:126`) runs the Smart phase over every
document, then the Local AI phase through `runLocalAIPhase`
(`backend/app_detect.go:375`), which DOES honour `aiScope`: it builds `scanUnit`s
from `doc.PagesMarkdown(scope.Pages)` (`app_detect.go:398`). Then, at
`app_detect.go:214`, it calls `refineCategories` (`app_detect.go:255`), which
collects every `smartDiscovered` suggestion in the whole result and hands them to
`llm.ClassifySuggestions`. `refineCategories` has no scope parameter and no
knowledge that one exists.

Two compounding wastes in the same payload, both measured:

- `engine.FoldValueFamilies` runs at `app_detect.go:226`, AFTER the classify
  call, so the model pays to classify "Borch" and "Johannes Borch", "Liber" and
  "Oscar Liber", "Oscar", "Johannes" separately. 9 of 25 rows on the reference
  document are spellings of another.
- Each row carries up to `maxSuggestionContexts` = 3 snippets of +/-60 runes
  (`backend/engine/discover.go:147`), joined by `ClassifySuggestions` at
  `client.go:482`. That is 279 bytes of prompt per name classified, and on an
  email thread the snippets largely re-quote the same header block.

### Change

`backend/app_detect.go`:

- Give `refineCategories` the scope. Thread `aiScope` from `RunDetection`
  (`app_detect.go:214`) into `refineCategories` (`app_detect.go:255`), and when
  the scope is active, send only suggestions that actually occur in the scoped
  text. The scoped text is already computable exactly the way
  `runLocalAIPhase` computes it (`doc.PagesMarkdown(scope.Pages)`,
  `app_detect.go:398`), so **reuse that path rather than writing a second
  notion of what a scoped page is**. A suggestion whose `MainText` does not
  occur in the scoped text is left with its offline category, which is the same
  graceful outcome the existing failure path already produces
  (`app_detect.go:217`, "the offline guesses were kept").

  Extract the scanUnit-building loop from `runLocalAIPhase` into a helper both
  it and `refineCategories` call, so there is ONE definition of "the text this
  run's scope selects". Two implementations of that would be the same class of
  bug this CR is fixing.
- Fold families BEFORE classifying. Move `engine.FoldValueFamilies`
  (`app_detect.go:226`) above the `refineCategories` call at :214, or fold a
  copy for the classify payload. Prefer moving it: folding once, early, over the
  unified list is what `CLAUDE.md` §5 already asks for ("`FoldValueFamilies`
  folds detection's whole output once, across every route"), and the current
  order means the model sees an unfolded list nothing else in the system ever
  sees.

  > Implementer note, and the thing to verify first: `refineCategories` writes
  > refined categories back by matching on `MainText` (`app_detect.go:277`).
  > Folding first changes which rows exist and which text is the main text
  > (the SHORTER form wins, so "Borch" not "Johannes Borch"). Check that the
  > write-back still lands on the right rows after the reorder, and that a
  > refined category cannot split a family that was just folded. If it can,
  > STOP and report it rather than working around it: silently re-categorising
  > one spelling of a family would break the one-value-one-placeholder
  > invariant, which is a correctness rule, not a performance one.

`backend/ollama/client.go`:

- Trim the classification payload in `ClassifySuggestions`
  (`client.go:446`; the row is built at `:482` and the snippets are joined at `:484`): send at most ONE context snippet
  per suggestion, trimmed to about 40 runes. Measured at -45% prompt tokens
  combined with the fold, with no observed loss of classification quality on the
  reference document. Say why in a comment: the snippet exists to disambiguate a
  name, and the second and third snippets on a document of one kind are usually
  the same sentence again.

### Tests

`backend/app_detect_integration_test.go` (integration tier: it already runs a
mock `/api/tags` + `/api/chat`, see `:195`, `:280`, `:381`):

- ADD `TestClassifyRespectsTheAIScope`: import two documents, or one document of
  two pages where each page carries a distinct name; run detection with an
  `AIScope` naming page 1; assert the classification request body contains the
  page-1 name and **does NOT contain the page-2 name**. This is the scope-leak
  guard and it is the test whose absence let the leak exist.

  > The harness for this already exists: `scopeChatServer`
  > (`backend/app_detect_integration_test.go:383`) records the user content of
  > EVERY `/api/chat` call, which includes the classification call, so the new
  > test can read the second call's payload straight out of the recorded slice.
  > Reuse it rather than writing a second recording server.
- ADD `TestClassifyPayloadIsFoldedAndBounded`: assert the classify body contains
  the folded main text and not the longer spelling folded into it, and that each
  line carries at most one context snippet. Assert on SHAPE (row count, snippet
  count), not on a byte total: a byte assertion would be a wall-clock proxy that
  breaks when the fixture text changes.
- KEEP the existing scope test at `app_detect_integration_test.go:381`, which
  proves which document text the Local AI receives. It covers CALL 1; the new
  test covers CALL 2.

`backend/ollama/client_test.go`:

- UPDATE `TestClassifySuggestionsBatching` (`client_test.go:530`): it asserts
  each user message stays within `ContextSize*3*3/4 + 256`. With one trimmed
  snippet per row the messages get smaller, so the bound still holds, but the
  test should now also assert the per-row snippet count so the trim is pinned
  rather than incidental.
- KEEP `TestClassifySuggestions` (`:497`) unchanged: the verbatim-input filter
  and the allowlist veto are untouched.

---

## CR6 — Assign `ConfidenceLLMDefault`, so the confidence floor stops lying

Not a performance fix. It is here because it is the lever the user reaches for
when the model's findings are weaker than the rules, and it belongs with any
change to the Local AI path.

### Current behaviour

Established above: the constant exists (`backend/engine/pii.go:487`), the
documentation promises it (`backend/engine/values.go:81`), the UI describes its
effect (`frontend/views/identifyrail.js:580`), and no production line assigns
it. An accepted AI suggestion ends up at `ConfidenceManualDefault` (0.95) via
`engine.valueConfidence` (`backend/engine/values.go:313`) reading 0 as
"not stated".

### Change

- `backend/ollama/client.go:580`: `parseSuggestionJSON` sets
  `Confidence: engine.ConfidenceLLMDefault` on every Suggestion it emits, beside
  the existing `WithMethod(engine.MethodLocalAI)` stamp. That line is already
  the ONE place Local AI provenance is applied at the boundary, which makes it
  the right place for the score too.
- `frontend/state.js:1941`: `valueFromSuggestion` carries `confidence` across.
  `engine.MergeSuggestions` already takes the strongest confidence when routes
  agree (`backend/engine/discover.go` ~132), so a row that heuristics also found
  correctly keeps the higher score rather than being demoted by the AI's 0.8.
- `backend/engine/values.go:81`: the comment now describes what the code does.
  No logic change there.

Do NOT change `ConfidenceLLMDefault`'s value, and do NOT change
`valueConfidence`'s "0 means not stated" reading: manual declarations still
legitimately arrive without a score, and that default is what serves them.

### Tests

- `backend/ollama/client_test.go`: fold the assertion into
  `TestDiscoverHappyPath` (`client_test.go:127`), which already checks the
  parsed suggestion field by field. Assert
  `Confidence == engine.ConfidenceLLMDefault`.
- `frontend/state.test.js` (or the existing suite covering `state.js`): ADD a
  test that accepting a `local_ai` suggestion yields a Value with
  `confidence === 0.8`. Assert against the number the bridge carries, and note
  in the test why: this is a cross-bridge contract, and the Go constant is the
  source of truth.
- `backend/engine/confidence_test.go`: it already asserts the intended
  behaviour with hand-built values (`:29`, `:95`, `:143`). Leave it alone. Its
  passing while the app misbehaved is precisely the gap CR6 closes: the
  behaviour was tested, the WIRING was not.
- `value_shape_test.go` must stay green: `confidence` is an existing field of
  the Value wire shape, so nothing there changes.

---

## CR7 — Pay the model load once, not on every cold call

Smaller than CR1 but not negligible: the measured cold load on `qwen3.5:0.8b`
was **4.6 seconds**, which is roughly a third of the whole post-fix budget for a
scoped run. It is also the one cost that is pure waiting, with no work in it.

### Current behaviour

Ollama's default `keep_alive` is 5 minutes, and the application never sends the
field, so a model that has been idle longer than that is unloaded and the next
detection run pays the full load. Nothing pre-warms it either:
`App.Startup` (`backend/app.go:327`) wires the file-drop handler and nothing
else, and `App.ProbeOllama` (`backend/app.go:346`) only calls `GET /api/tags`,
which reports that a model EXISTS without loading it.

So the first Local AI run of a session, and any run after a few minutes of the
user reading their documents, starts with a multi-second stall that looks to the
user exactly like the slowness this whole order is about.

### Change

`backend/ollama/client.go`:

- Add `KeepAlive string` with `json:"keep_alive,omitempty"` to `chatRequest`,
  at the TOP LEVEL beside `Think` and for the same reason: it is not an entry in
  Ollama's options map. Send a generous value (`"30m"`) on detection calls, so a
  model does not fall out of memory while the user reviews suggestions between
  runs. Do NOT send `-1` (load forever): this is a desktop application on a work
  laptop, and pinning a model in RAM until the machine reboots is not ours to
  decide.
- Add a `Warm(ctx context.Context) error` method. It sends one bounded request
  that loads the weights without doing real work, and it belongs on the client
  because it is an Ollama HTTP call and this is the only file allowed to make
  one. An **empty `messages` array** is the documented way to load a model
  without generating; `num_predict: 0` is not, so do not reach for that instead.
  Warm must be cheap to call and must never be able to hang the caller: give it
  its own short timeout, and treat a failure as nothing worth reporting, because
  a warm-up that did not happen costs latency and not correctness.

`backend/app.go`:

- Call `Warm` from `Startup` (`backend/app.go:327`), after the existing wiring,
  **in a goroutine**, so a slow or absent Ollama cannot delay the window
  appearing. Guard it: only warm when the stored settings have Local AI on, so a
  user who never uses the route never pays RAM for a model they did not ask for.
  A failure is swallowed on purpose, and the comment must say so: the probe
  already owns telling the user Ollama is missing, and a second, louder error
  path for a performance optimisation would report a problem the user cannot act
  on.

> Implementer note: `Startup` currently takes the Wails context and stores it.
> Do not pass that context to `Warm` if it would tie the warm-up's lifetime to
> the window's, and do not block on it. The point of the change is that nobody
> waits.

### Tests

`backend/ollama/client_test.go`:

- ADD `TestWarmLoadsWithoutGenerating`: assert `Warm` posts to `/api/chat` with
  an EMPTY `messages` array and a `keep_alive` value, and that it returns no
  error on a normal reply.
- ADD `TestWarmFailureIsNotFatal`: with the server refusing connections, `Warm`
  returns an error and does not panic. The caller ignores it, and this test
  documents that the error is safe to ignore.
- UPDATE `chatReplyServer` (`client_test.go:32`) to assert `keep_alive` is
  present at the top level on real chat calls, and absent from `options`. Fold
  this into the same helper edit CR1 and CR4 make rather than touching it a
  third time.

`backend/app.go`'s side needs no new test: a goroutine that swallows errors and
whose only effect is timing has nothing deterministic to assert. What is worth
asserting lives in the client, and it is asserted above.

## Decisions taken

1. **`think:false` at the top level of the request, always.** Extraction never
   wants reasoning, `format` does not suppress it, and inside `options` the flag
   is silently dropped. Set in `Chat` so both call sites inherit it.
2. **The default model becomes `qwen3.5:0.8b`** (Apache-2.0, ships `Q8_0`),
   chosen on measured 5.3s / 16 names against the 4B's 12.2s / 17. It also
   resolves the Qwen2.5-3B Research-License problem in the current pin.
3. **Model tags must be K-quant or `Q8_0`, never `-bf16`/`-f16`**, written into
   §7 with its reason, because "BF16 means best quality" is the exact wrong
   intuition on a CPU laptop.
4. **Sampling is pinned for extraction**, not left to the model tag:
   `temperature 0`, `top_k 1`, `top_p 1`, `presence_penalty 0`,
   `repeat_penalty 1`, fixed seed. The tags' shipped `presence_penalty 1.5`
   actively fights verbatim name-copying.
5. **`num_predict` is capped at 512, and only because `think:false` is set.**
   With thinking on, the cap makes things worse: the trace spends the budget and
   the JSON never arrives.
6. **`format` carries a JSON Schema, derived from `promptCategories`, flat.**
   Derived so the parity guard covers it; flat because sub-4B models echo
   `$defs` schemas back instead of filling them. `CLAUDE.md` §7 and §8 change
   with it.
7. **The parser's tolerances stay.** They cost nothing and they still guard the
   loose-JSON fallback; removing them in the same change would conflate "the
   model stopped doing this" with "we stopped coping".
8. **`num_ctx` stays 8192 and the 120s client timeout stays.** Neither is the
   bottleneck; shrinking the window would truncate.
9. **The page scope narrows the classification call too**, reusing
   `runLocalAIPhase`'s existing notion of scoped text through a shared helper,
   never a second implementation.
10. **Families are folded BEFORE classification**, which is what `CLAUDE.md` §5
    already asks for, subject to the write-back check flagged in CR5.
11. **One context snippet per suggestion, trimmed**, in the classify payload.
12. **`ConfidenceLLMDefault` is assigned at the boundary and carried across the
    bridge.** The constant, its value and `valueConfidence`'s "0 means not
    stated" reading are all unchanged.
13. **The model is pre-warmed at startup and held with `keep_alive: "30m"`,
    not `-1`.** The measured 4.6s cold load is a third of the post-fix budget
    and is pure waiting, but pinning a model in RAM until reboot is the user's
    machine to decide, not ours. The warm-up runs in a goroutine and its failure
    is swallowed: the probe already owns telling the user Ollama is missing.
14. **No encoder NER model, no second runtime.** GGUF is generative-only, so
    Ollama cannot serve GLiNER or `openai/privacy-filter` at all. P4
    (ONNX-in-WebView) remains the recorded fallback if quality is still short.
15. **No session-shape change.** `SessionVersion` stays 8.

## Conflict analysis

### Files touched by more than one CR

| File | CRs | Note |
|---|---|---|
| `backend/ollama/client.go` | CR1 to CR7 | the centre of gravity, and the one file allowed to build Ollama requests. CR1/CR3/CR4/CR7 all add fields to the same two structs and all set them in `Chat`: do them together. |
| `backend/ollama/client_test.go` | CR1 to CR7 | and specifically `chatReplyServer` (`:32`), which CR1, CR4 and CR7 all change. See the hotspot below. |
| `CLAUDE.md` | CR2 (§7 rows), CR4 (§7 API row + §8 bullet) | both edit §7; make the §7 edits in one pass so the table is not rewritten twice |
| `backend/app_detect.go` | CR5 | isolated, but the largest behavioural change outside the client |
| `frontend/state.js` | CR6 | one function |
| `backend/app.go` | CR2 (`defaultSettings` prose only), CR7 (`Startup` warm-up) | CR2 does not actually edit it; the constant it reads lives in the client |

### Hotspots

- **`chatReplyServer` (`client_test.go:32`) is the single riskiest edit in the
  order.** It backs most tests in the file, and CR4 INVALIDATES its existing
  assertion: `req["format"] != "json"` becomes true for every call once `format`
  is a schema object, so **every test using this helper fails at once**. That is
  correct behaviour from the guard, and it is also the most confusing possible
  first symptom. Change the helper in the SAME commit as CR4, and expect the
  whole file to go red until you do. CR1's `think` and CR7's `keep_alive`
  assertions belong in that same edit: three separate passes over one helper is
  how one of the three assertions ends up quietly dropped.
- **`Chat` is where CR1, CR3 and CR4 converge.** Three CRs adding fields set in
  one function is why the recommended order lands them as one commit. If a
  `buildChatRequest` helper is extracted (CR3), extract it once, first, then add
  fields to it.
- **CR5's reorder can break a correctness invariant, not just a test.** Folding
  before classifying changes which rows exist and which text is the main text,
  and `refineCategories` writes back by `MainText`. The CR5 implementer note is
  a genuine stop-and-report point, not a caution to read past.
- **`copy_guard_test.go` walks `backend/` and `.`**, so it covers the prompt
  strings. Neither prompt changes in this order, but if a new error message is
  added for a capped or truncated reply, it must carry no em dash.
- **`detection_parity_test.go` and `category_parity_test.go` are untouched** by
  design: no discovery method, match class, signal source or category changes
  here. If a CR appears to need one, that is a signal the CR has drifted.

## Recommended order

1. **CR1 + CR3 + CR4 + CR7 as ONE commit**, in that internal order:
   `think:false`, then the sampling options, then the schema, then `keep_alive`
   and `Warm`. They edit the same two structs and the same `Chat` function, and
   CR4's `chatReplyServer` change is what makes the file green again. This commit
   is the bulk of the latency win, and it is independently verifiable against a
   real Ollama before anything else lands. CR7's `Startup` hook is the only part
   that reaches outside the client; keep it last inside the commit so a failure
   there is easy to isolate.
2. **CR2** next, alone: the default model plus the `CLAUDE.md` §7 rows and the
   prose references. Small, mechanical, and it wants its own commit because it
   is the one change a reviewer will want to see in isolation (a model swap and a
   licence fix).
3. **CR5**: the scope leak and the payload trim. Do it after 1 and 2 so the
   measured improvement is attributable, and treat the fold-order note as a gate.
4. **CR6** last: the confidence wiring, which crosses the bridge and is the only
   CR touching `frontend/state.js`.

Between step 1 and step 2, run the French spot-check in the acceptance criteria.
It is the one open decision in this order and it is cheapest to answer while the
model is still a one-line change.

## Acceptance criteria

- `go test ./...`, `go test -tags=integration ./...` and
  `node --test "frontend/**/*.test.js"` all green.
- `task audit` reports no new finding.
- With a real Ollama on the target laptop, on step 2 Identify with Local AI on:
  1. `qwen3.5:0.8b` is preselected in the model dropdown, and Local AI is
     switched off by default as before (detecting Ollama enables the switch, it
     never flips it).
  2. **Scoping to one page of the reference document completes well under 20
     seconds**, against about two minutes before this order. Both model calls
     are now scoped, so the improvement is not only the request settings.
  3. Running the SAME page twice produces the SAME suggestion list. This is the
     observable proof the greedy-sampling change landed.
  4. Accepting one AI-only suggestion, then raising **Minimum confidence** past
     80 on the Configure rail and running Anonymise, leaves that Value alone
     while a manually declared Value is still replaced. This is the observable
     proof CR6 landed.
  5. No reply arrives truncated: the JSON parses on the first attempt, with no
     code-fence stripping needed and nothing dropped by the hallucination filter
     for a formatting reason.
  6. **The French spot-check.** Run detection on a French page from the real
     corpus with both `qwen3.5:0.8b` and `qwen3.5:4b` and compare what each
     finds. Keep the 0.8B default unless the 4B is clearly better on French, in
     which case report that and let the owner decide rather than changing the
     pin unilaterally. This is the ONE open decision in this order: the 16-vs-17
     name result behind decision 2 was measured on ENGLISH text, and a name
     count is not an accuracy measure.
  7. The FIRST Local AI run of a session is not visibly slower than the second.
     The pre-warm has paid the model load before the user asks for anything, so
     the measured 4.6 second load no longer sits inside a user-visible wait. With
     Local AI switched off at startup, no model is loaded at all: confirm with
     `ollama ps` that nothing is resident until the route is enabled.
  8. Stopping Ollama and re-probing leaves the deterministic pipeline fully
     usable end to end, with the Local AI controls disabled and their tooltip
     shown. Graceful degradation is untouched by this order and must be
     confirmed, not assumed.

## First actions for the implementation coordinator

1. Read `CLAUDE.md`, `backend/CLAUDE.md`, `frontend/CLAUDE.md`,
   `frontend/BRIDGE.md` and `docs/TESTING.md`.
2. Confirm the two claims this plan makes about the world before building on
   them: that the installed Ollama is recent enough for `think:false` plus
   `format` (0.32.14 or newer), and that `qwen3.5:0.8b` is Apache-2.0 while
   `qwen2.5:3b` is Qwen-Research-licensed. Report if either is wrong.
3. Implement CR1 + CR3 + CR4 + CR7 as one commit, changing `chatReplyServer`
   once for all of them, and run all three suites.
4. Measure a scoped page-1 run against a real Ollama and record the number, so
   step 3's effect is known before CR5 changes the payload as well.
5. Implement CR2, run the French spot-check, then CR5 (honouring its fold-order
   gate) and CR6.

---

## Appendix A — The raw measurements

Kept verbatim so the implementing session can check its own numbers against a
baseline rather than against a summary. Machine: the owner's Windows corporate
laptop, CPU only, Ollama 0.32.14. Input: 2,809 bytes of business-email prose
plus the 1,163-byte discovery system prompt, which is the same size class as
page 1 of the reference document (3,577 bytes).

Read the tables with two cautions. **`names` is a COUNT, not an accuracy
measure**, and both runs were on ENGLISH text, which is why the French
spot-check is an acceptance criterion. And the `prefill` column in the warm
tables is a prompt-cache hit, not work: only the cold line's prefill is real.

### `qwen3.5:0.8b` (Q8_0)

```
COLD COST (first call after the model was unloaded):
  total 266.3s = load 4.6s + prefill 4.4s (223 tok/s for 978 tokens) + generate 257.3s

variant                                total    load   prefill   ptok  generate   gtok   think  names stop
as shipped today                      258.7s    0.3s 17419 tok/s    978  28 tok/s   7214  27228B      0 length
                                       reply did not parse: reply was not a JSON object
                                       TRUNCATED: num_predict cut the reply off, so the JSON is incomplete.
+ think:false (top level)               2.8s    0.4s 10119 tok/s    980  33 tok/s     73       -      2 stop
+ greedy, no presence penalty           2.3s    0.3s 16361 tok/s    980  35 tok/s     67       -      6 stop
+ JSON schema instead of "json"         5.4s    0.3s 16889 tok/s    980  24 tok/s    121       -     16 stop
+ num_batch 1024 (faster prefill)       5.3s    0.3s 16367 tok/s    980  25 tok/s    121       -     16 stop
+ num_predict 512 (runaway guard)       5.3s    0.3s 18036 tok/s    980  25 tok/s    121       -     16 stop
```

### `qwen3.5:4b` (Q4_K_M)

```
COLD COST with the fixes applied:
  total 8.1s = load 0.1s + prefill 0.3s (3262 tok/s for 980 tokens) + generate 7.5s

variant                                total    load   prefill   ptok  generate   gtok   think  names stop
as shipped today                      RUNAWAY: still generating at the 1m30s cap, abandoned
+ think:false (top level)              11.7s    0.4s 4687 tok/s    980  11 tok/s    118       -     16 stop
+ greedy, no presence penalty          11.4s    0.3s 4958 tok/s    980  10 tok/s    104       -     13 stop
+ JSON schema instead of "json"        12.7s    0.4s 4859 tok/s    980  10 tok/s    118       -     17 stop
+ num_batch 1024 (faster prefill)      12.2s    0.3s 4746 tok/s    980  10 tok/s    118       -     17 stop
+ num_predict 512 (runaway guard)      11.8s    0.3s 4837 tok/s    980  10 tok/s    118       -     17 stop
```

### What each row is evidence for

| Observation | Supports |
|---|---|
| 258.7s / 7,214 generated tokens / 27 KB of reasoning / 0 names / truncated | CR1. Thinking is the whole latency problem, and it does not merely slow the call, it destroys the result. |
| the 4B "as shipped" row never finishing inside 90 seconds | CR1. The bigger the model, the worse thinking-on gets. |
| 6 names to 16 when the schema is added (0.8B) | CR4. The schema is a recall win, not only a format guarantee. |
| 0.8B ~30 tok/s vs 4B ~10 tok/s generation | CR2. Size behaves normally on these K-quant builds, and the 0.8B is the faster pick. |
| 0.8B 5.3s / 16 names vs 4B 12.2s / 17 names | CR2. 2.3x the latency for one extra name on English text. |
| cold load 4.6s on the 0.8B, a third of the post-fix budget | CR7. Worth pre-warming out of the user's wait. |
| cold prefill 223 tok/s (0.8B) and 3,262 tok/s (4B) | The `num_ctx` decision. The window is not the bottleneck; do not shrink it. |
| `num_batch 1024` moving nothing outside noise | Why no CR proposes it. Prefill was never the problem once thinking was off. |

### A note on `num_batch`, which is deliberately NOT in this order

The benchmark tested it and it changed nothing measurable (5.3s vs 5.3s on the
0.8B; 12.2s vs 12.7s on the 4B, which is noise). It is listed here so a later
session does not rediscover it as an untried idea: it was tried, on this
hardware, and it is not worth a request field.

## Appendix B — Voided measurements, and why they are recorded

An earlier round of this investigation compared what were believed to be
`qwen3.5-4B-BF16` and `qwen3.5-0.8b-BF16` against `Qwen2.5-3B-Instruct-Q5_K_M`,
and concluded that BF16 quantisation was the primary cause. **Both Ollama tags
in that round actually resolved to Qwen2.5-3B-Instruct-Q5_K_M**, so the
comparison measured one model against itself. Those numbers are void and must
not be cited.

Two things are worth carrying forward from that dead end, because both cost real
time to establish:

1. **The BF16 principle is still true, it just was not what happened here.** BF16
   has no fast CPU dot-product kernel without AVX512-BF16; ggml zero-extends and
   shifts every weight to FP32 inside the dot product
   (ggml-org/llama.cpp issue #7182 has the disassembly, with the conversion
   instructions accounting for about half of execution time). That is why CR2
   writes the "K-quant or Q8_0, never BF16" rule into §7. The rule is
   preventative, not a fix for something observed.
2. **A benchmark that shares one prompt across variants must warm up with the
   real prompt.** The first version of the benchmark warmed with an empty
   `messages` array, which neither loaded the weights nor primed the prompt KV
   cache. So row 1 paid the cold load and the only real prefill while rows 2 to 6
   hit a warm cache, and prefill appeared to jump from 25 tok/s to over 11,000.
   That artefact read exactly like a 7x win for the setting on row 2, and it was
   nothing of the kind. Any future measurement of this route must separate the
   cold cost from the warm per-call cost, which is why Appendix A reports them
   apart.
