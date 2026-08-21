# CHANGE-12b — Renaming the identifiers behind the labels: `local_ai` and `smart_discovered`

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0, pure Go + Wails v2, no CGo, no npm). It is **ONE batch**.

It is the companion to `docs/change-12.md` and **must be executed after it**.
Change-12 retired "Smart detection" and "Local AI" from every user-facing string
while leaving every identifier alone, on the rule in `CLAUDE.md` §5: a label is a
display string, an identifier is a contract. This order changes the contract.

## Why it is a separate order

The identifiers are PERSISTED. `local_ai` is a `DiscoveryMethods` member and
`local_ai_discovered` a match class, both written into every session file;
`useLocalAI`, `aiStrictFormat` and `aiDetailLevel` are settings keys in session
files and in profile files. `CLAUDE.md` §5 is explicit that a session file of the
wrong version is **refused, never migrated**, because a half-migrated file
silently reassigns placeholders and a placeholder that has left the machine can
never be re-numbered.

So this batch costs the owner every saved session and every saved profile on
disk. That is an acceptable price for a vocabulary that stays true as the model
routes multiply, and an unacceptable one to pay by accident inside a UI change.
Hence two orders.

Ground rules, in addition to the Ground rules block of `docs/change-12.md`:

- **No behaviour change of any kind.** Every rename is mechanical. If a rename
  requires a logic edit to compile, stop and report it: that is a finding about
  coupling, not a step to improvise.
- `SessionVersion` goes **10 to 11**, with the reason recorded beside the
  constant in `backend/engine/session.go` exactly as the previous bumps are.
  There is no migration table and no compatibility alias anywhere in the loader.
- The parity guards are load-bearing and move with the constants:
  `detection_parity_test.go`, `category_parity_test.go`, `value_shape_test.go`,
  `result_shape_test.go`.
- Comments explain intent in the present tense. No comment may say what an
  identifier used to be spelled.

### The deviation rule

If a step is wrong, contradicted by the code, or cannot be done as written:
**stop, say so, and propose the alternative before writing it.**

---

## 1. The rename table

Every row is the whole rename: the Go constant or field, the JSON or wire
spelling, and the JS mirror. Nothing else may change spelling.

| Kind | Now | Becomes | Lives in |
|---|---|---|---|
| discovery method value | `local_ai` | `local_llm` | `engine/matchclass.go`, `state.js DISCOVERY_METHODS`, session files |
| discovery method constant | `MethodLocalAI` | `MethodLocalLLM` | `engine/matchclass.go` and its callers |
| match class value | `local_ai_discovered` | `local_llm_discovered` | `engine/matchclass.go`, `state.js MATCH_CLASSES`, session files |
| match class constant | `MatchClassLocalAIDiscovered` | `MatchClassLocalLLMDiscovered` | same |
| match class value | `smart_discovered` | `rules_discovered` | same. The class is ONE precedence rank shared by heuristic discovery and signal-based discovery, and the name says what both are: a rule over the text rather than a model reading it. It stays true if signal-based discovery is ever given its own rail section, which `heuristic_discovered` would not |
| match class constant | `MatchClassSmartDiscovered` | `MatchClassRulesDiscovered` | same |
| detection phase value | `smart` | `rules` | `app_detect.go PhaseSmart`, the `detection:progress` event payload, `copy.js` phase label switch. The phase runs heuristic AND signal discovery, so it takes the same word as the match class rather than naming one of its two halves |
| detection phase constant | `PhaseSmart` | `PhaseRules` | `app_detect.go` |
| detection phase constant | `PhaseLocalAI` | `PhaseLocalLLM` (value `local_llm`) | `app_detect.go` |
| settings field and key | `UseLocalAI` / `useLocalAI` | `UseLocalLLM` / `useLocalLLM` | `app.go`, `engine/session.go`, `state.js`, `BRIDGE.md` |
| settings field and key | `AIStrictFormat` / `aiStrictFormat` | `LLMStrictFormat` / `llmStrictFormat` | same |
| settings field and key | `AIDetailLevel` / `aiDetailLevel` | `LLMDetailLevel` / `llmDetailLevel` | same |
| Go function names | `runLocalAIPhase`, `localAISection`, `aiScope`, `estimateAIRequests` and the rest of the `*AI*` family | the `LocalLLM` / `llm` spelling | `backend/*`, `frontend/*`. `llmEnabled` and `App.llm` already read correctly and stay |
| session constant | `SessionVersion = 10` | `= 11` | `engine/session.go`, with the reason line |

Explicitly NOT renamed, and the plan must say why in a comment where each one is
declared:

- `MethodHeuristic = "heuristic"` and `MethodSignal = "signal"`: already correct.
- `useBuiltInPatterns` and `useHeuristicDiscovery`: already correct, and they are
  the two keys change-12's section switches bind to.
- `engine.SmartDetectOptions` / `HeuristicDiscoverContext` and the
  `heuristicDiscovery` settings block: the OPTIONS type still carries the word
  smart in `SmartDetectOptions`. Rename it to `HeuristicDiscoverOptions` in this
  batch (it is not persisted under that name; the JSON key is already
  `heuristicDiscovery`), and verify that with a test rather than by reading.

---

## 2. Where the values are read as data

A rename of a persisted VALUE is only complete if every consumer of the string
moves. These are the places that compare against the literals rather than the
constants, and each must be checked by hand:

| Place | What to check |
|---|---|
| `frontend/state.js` `DISCOVERY_METHODS`, `MATCH_CLASSES` | mirrors, guarded by `detection_parity_test.go`. The guard fails first if this is missed, which is the point of it |
| `frontend/copy.js` method and match-class label maps | keyed BY the identifier; the keys move, the labels were already fixed in change-12 |
| `frontend/suggestionmodel.js`, `valuemodel.js`, `views/identifyworkspace.js` | any `=== "local_ai"` or `"smart_discovered"` comparison |
| `backend/engine/intersections.go`, `conflicts.go`, `values.go`, `registry.go` | rank tables and label switches keyed by match class |
| `backend/ollama/client.go` | the method stamped on model suggestions |
| `backend/engine/session.go` load and save | the settings keys, and the version refusal message |
| `backend/engine/framework_agreement_expected.json` and any other testdata JSON | a fixture carrying a method or class string |
| `scripts/uitest/probes.js` | seeded state and any probe reading a method name |

---

## 3. Tests this batch moves

- `detection_parity_test.go`, `category_parity_test.go`, `value_shape_test.go`,
  `result_shape_test.go`: the guards prove the two sides agree; they move with
  the constants and must be run FIRST after each rename step, because they are
  the cheapest signal that a mirror was missed.
- `backend/engine/session_test.go`: a v10 file is REFUSED with an actionable
  message naming the version it holds and the version this build wants. Add the
  test if it does not exist for the new number.
- `backend/app_e2e_test.go`, `backend/app_detect_integration_test.go`,
  `backend/engine/framework_agreement_test.go`: they name the routes and the
  methods; update the identifiers, not the assertions.
- `frontend/*.test.js`: the same treatment. No test's MEANING changes in this
  batch. A test that has to change what it asserts is a signal that a behaviour
  moved, which this order forbids.

---

## 4. Execution order

1. `engine/matchclass.go` first, plus `state.js` mirrors, then run
   `detection_parity_test.go` and the frontend suite. Nothing else until green.
2. The phase constants and the progress event payload.
3. The settings fields and keys, then `SessionVersion` 10 to 11 and its reason
   line, then the session tests.
4. The function and file-local names (`runLocalAIPhase`, `localAISection`,
   `aiScope`, `estimateAIRequests`, `SmartDetectOptions`), which are compiler
   work: `go build ./...` is the guide.
5. Fixtures and probes.
6. `CLAUDE.md` §5 tables (discovery method, match class), `frontend/BRIDGE.md`,
   `frontend/CLAUDE.md`, `backend/CLAUDE.md`, and the `SessionVersion` line in
   `CLAUDE.md` §5.

---

## 5. Acceptance criteria

1. `go test ./...` and `node --test "frontend/**/*.test.js"` green, and the
   rendering harness green.
2. `grep -rn "local_ai\|smart_discovered\|UseLocalAI\|AIStrictFormat\|AIDetailLevel\|PhaseSmart\|SmartDetectOptions"` over the
   repository returns nothing outside `docs/`.
3. `SessionVersion` is 11, the reason is recorded beside the constant, and a v10
   file is refused with a message naming both versions and telling the user what
   to do. No migration code exists anywhere.
4. No behaviour change: the framework-agreement regression suite reports the same
   precision and recall numbers as before the batch, to the digit.
5. `TestAnonymiseNeverCallsOllama` still green, untouched.
