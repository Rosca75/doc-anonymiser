# CHANGE-12d — Replacing the confidence percentage with the two questions it was really asking

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0, pure Go + Wails v2, no CGo, no npm). It is **ONE batch**.

It comes AFTER `docs/change-12.md`, `docs/change-12b.md` and `docs/change-12c.md`.
Unlike the first two,
this order **changes behaviour**, so it is executed on its own and its result is
checked against a real document, not only against tests.

## 1. The finding

The rail's "Match confidence" slider (`settings.minConfidence`, engine
`FilterByMinConfidence`) presents itself as a general quality dial. It is not.
It is passed to exactly two places, and in both it does something narrow:

- at Identify time, only to the pattern-match preview
  (`app_detect.go` -> `PreviewPatternMatches`). It never filters the suggestion
  list, which is filtered by the heuristic block's own floor.
- at run time, to every span in the pipeline (`app_run.go` -> `PipelineInput`).

The scores it sorts between are fixed (`engine/pii.go`):

| Span at run time | Score |
|---|---|
| pattern match | 1.0 |
| pattern match whose CORROBORATING checksum failed | 0.7 |
| custom pattern | 1.0 |
| accepted Value the user declared | 0.95 |
| accepted Value that came from a Suggestion | the Suggestion's own score (0.9 signal, 0.8 model, whatever heuristic scored it) |

So the slider does two unrelated things, one of them wrong:

**(a) It decides whether a checksum-failed pattern match is replaced.** That is a
real question, and today it is the only genuine use of the control. It deserves
to be asked in words, not as a percentage the user has to reverse-engineer from a
read-out.

**(b) Above roughly 0.8 it starts dropping Values the user has already
accepted**, by the score of whatever originally found them
(`engine/values.go valueConfidence` carries a Suggestion's score onto the Value).
That contradicts the review gate. `CLAUDE.md` §5 is explicit that the gate exists
so the user decides, and a run that silently discards an accepted Value because a
heuristic scored it 0.6 answers "reject" on the user's behalf after the user said
accept. It is also invisible: the value simply does not appear in the result.

## 2. What this order does

1. **Deletes the confidence percentage from the interface**, and with it the
   "Detection quality" panel that `docs/change-12.md` D4 created to hold it. The
   rail returns to three route sections plus Load profile.
2. **Adds one checkbox to Built-in patterns**: "Only replace when the checksum
   matches", **off by default**, which is exactly today's default behaviour
   (`minConfidence` 0). On, it drops pattern matches whose corroborating checksum
   failed, and nothing else.
3. **Stops the run filtering accepted Values by confidence at all.** An accepted
   Value is replaced because the user accepted it. Confidence keeps its two
   remaining jobs: it feeds the checksum question above, and it is reported.
4. Keeps `Confidence` on the Span and the Value untouched as DATA. Nothing about
   what the engine records changes; only what is filtered out changes.

Ground rules, in addition to the Ground rules block of `docs/change-12.md`:

- The three constants `ConfidenceDeterministic`, `ConfidenceChecksumFailed` and
  their siblings stay exactly as they are, with their scores. This order changes
  who READS them.
- **A checksum failure still never vetoes a span on its own** (`CLAUDE.md` §5).
  The checkbox is the user asking for the veto; `piiPattern.validate`, where a
  checksum IS the recognizer, is untouched and stays mandatory.
- `SessionVersion` bumps to **13** (11 taken by `docs/change-12b.md`, 12 by
  `docs/change-12c.md`), with the reason recorded beside the constant. The schema loses
  `minConfidence` and gains `requireChecksum`, and `CLAUDE.md` §5's session
  paragraph moves with it.

### The deviation rule

If a step is wrong, contradicted by the code, or cannot be done as written:
**stop, say so, and propose the alternative before writing it.**

---

## 3. Engine changes

| File | Change |
|---|---|
| `engine/pipeline.go` | `PipelineInput.MinConfidence` is replaced by `RequireChecksum bool`. In `detectText` the filter applies to the PATTERN spans only, before the Value and custom-pattern spans are appended, so no accepted Value can be filtered by score. The ordering comment ("the floor is applied BEFORE overlap resolution") keeps its reason and its position |
| `engine/pii.go` | `FilterByMinConfidence` is replaced by `RejectFailedChecksums(spans)`, which drops spans scoring `ConfidenceChecksumFailed`. Named after what it does, so a reader does not have to hold the score table in their head. Its doc comment carries the example the old one carried: which producers score what |
| `engine/patternpreview.go` | the preview takes the same boolean, so the preview and the run still cannot disagree. This is load-bearing: `CLAUDE.md` §5 requires the preview to promise nothing the run does not make |
| `engine/session.go` | `MinConfidence` out, `RequireChecksum bool` in, `SessionVersion` bumped with its reason line |
| `backend/app.go` | `Settings.MinConfidence` out, `Settings.RequireChecksum bool` (`json:"requireChecksum"`) in; the range validation for the old field goes with it. `Settings.HeuristicDiscovery.MinConfidence` is UNTOUCHED: that is the heuristic pass's own floor, it works where it belongs (before a Suggestion is shown), and it stays in the strictness block |
| `backend/app_run.go`, `app_detect.go`, `app_values.go` | pass the boolean instead of the float |

Nothing else in the engine may change. In particular `ResolveOverlaps`, the match
classes and the registry are untouched.

## 4. Frontend changes

| File | Change |
|---|---|
| `views/identifyrail.js` | delete `confidenceControl`, `confidenceEffect` and the `rail-quality` panel; add the checkbox row to `patternsSection`, directly under the preset, above the category groups. `collapsedGroups` loses `rail-quality` |
| `state.js` | `setMinConfidence` becomes `setRequireChecksum`; the settings default is `requireChecksum: false` |
| `copy.js` | delete `CONFIGURE.confidenceTitle`, `confidenceLabel`, `confidenceHelp` and the four `confidenceEffect` sentences, and `RAIL.qualityTitle` / `qualityHelp`. Add `RAIL.requireChecksum` = "Only replace when the checksum matches" and a help tooltip saying: some identifiers carry a check digit; when the digits do not add up the match is kept by default, because a mistyped or partly redacted bank identifier is still one, and this switch drops it instead |
| `views/export.js`, `identifyworkspace.js` | any read-out naming the confidence floor |

## 5. Tests

| Test | Asserts |
|---|---|
| an accepted Value with a low origin score is still replaced | the defect in §1(b) cannot come back: a Value carrying `Confidence` 0.6 is replaced with the checkbox both off AND on |
| the checkbox drops the checksum-failed match and nothing else | over a document holding a checksum-failed IBAN, a valid IBAN, an email and an accepted Value: on, one span fewer; the other three untouched |
| the preview and the run agree | `PreviewPatternMatches` and `engine.Run` produce the same set of pattern matches under both settings |
| `validate` is still mandatory | a bare digit run that fails Luhn is still not a credit card, with the checkbox off. This is the boundary `CLAUDE.md` §5 draws between a checksum that IS the recognizer and one that corroborates |
| the framework-agreement suite | with the checkbox off, precision and recall are UNCHANGED to the digit from before this batch. That is the proof the default is today's behaviour |
| a session file of the previous version is refused | with a message naming both versions |
| the rail has no confidence slider and no Detection quality panel | render: three `.rail-section`, one `.rail-panel` (Load profile) |
| the checkbox lives in Built-in patterns and writes only its own flag | wiring |

## 6. Execution order

1. Engine: the filter rename and the `detectText` reordering, with the two
   behaviour tests written FIRST and failing.
2. The settings field, the session field, the version bump and the refusal test.
3. Frontend: the checkbox in, the slider and the panel out, the copy sweep.
4. Harness: `probes.js`, `checks.go`, `Invoke-UITest.ps1` lose the confidence
   assertions and gain the checkbox.
5. `CLAUDE.md` §5 (the checksum paragraph now names the switch, the session
   paragraph names the new version), `frontend/BRIDGE.md`, `README.md`,
   `frontend/docs/index.html`.

## 7. Acceptance criteria

1. Both suites and the rendering harness green.
2. With the checkbox off, the framework-agreement regression numbers are
   identical to the digit. The default changes nothing for an existing user.
3. No accepted Value is ever dropped by a confidence comparison, anywhere in the
   pipeline. Grepping for the retired filter returns nothing.
4. The word "confidence" survives in the interface in exactly one place, the
   heuristic strictness block, where it governs Suggestions before they are shown.
5. **Checked by hand, not only by test:** import a document containing a
   checksum-failed IBAN, run with the checkbox off and confirm the IBAN is
   replaced and the mapping names it as an IBAN; run with it on and confirm the
   IBAN is left in clear and nothing else changed.
