# CHANGE-12 — One switch, one mechanism: splitting the detection routes in the rail

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0, pure Go + Wails v2, no CGo, no npm). It is **ONE batch**,
sized for one session, followed by the **decisions taken**, the **execution
order** and the **acceptance criteria**.

It is the first of four: **12** is this UI split, **12b** renames the identifiers
behind the retired labels, **12c** makes presets scoped data, and **12d**
replaces the confidence percentage with the checkbox it was really asking about.
Each later order changes behaviour or a persisted schema, which is why they are
not folded into this one.

This order is a **pure UI and copy change**. It moves no detection logic, adds
no producer, changes no placeholder, and touches no persisted identifier. When
it is finished, every existing session file and profile file still loads, and
`SessionVersion` is still **10**.

## The complaint this answers

The rail presents one section, "Smart detection", whose header switch is a master
over three mechanisms that have nothing in common except being offline:

- **built-in pattern matching**, which produces DIRECT MATCHES applied without
  review;
- **heuristic discovery**, which produces SUGGESTIONS that must be reviewed;
- **signal-based discovery**, which produces suggestions from pattern evidence.

The review gate is the difference between the first and the other two
(`CLAUDE.md` §5), and the rail hides it: a user turning "Smart detection" off is
turning off two unrelated things, and a user turning it on cannot tell which of
them found what. "Smart detection" also does not say what it does. Neither does
"Local AI".

Ground rules (unchanged from `CLAUDE.md`):

- `CLAUDE.md` and the two subtree charters remain authoritative. Nothing here
  weakens the local-only guarantee, the one-file Ollama boundary, the zero-CGo
  rule, or "originals are immutable".
- **No engine behaviour change.** `backend/engine/**` gains no logic in this
  batch. Its only permitted edits are user-facing STRINGS and comments that name
  a retired label.
- **No persisted identifier changes.** `local_ai`, `smart_discovered`,
  `useLocalAI`, `useBuiltInPatterns`, `useHeuristicDiscovery`, `PhaseSmart` and
  every JSON key keep their spelling. `CLAUDE.md` §5: "a label is a display
  string, an identifier is a contract." The identifier rename is
  `docs/change-12b.md`, a separate order with its own session bump.
- **A change is not finished until its tests move with it** (`CLAUDE.md` §6;
  `docs/TESTING.md` owns the tiers and the scoping procedure). Both suites gate:
  `go test ./...` and `node --test "frontend/**/*.test.js"`. The rendering
  harness (`docs/UITESTING.md`) is part of this batch, because this change is
  measured in the rail's layout.
- **Render tests over substring matches; wiring tests when the question is what a
  control DOES** (`docs/TESTING.md`).
- User-visible copy never contains em dashes (`copy_guard_test.go`,
  `frontend/copy.test.js`). The prose of THIS document is not copy; the strings
  it quotes are.
- Comments explain intent in the present tense. Do not write "used to be", "this
  order changed it", or a change-request number, into the code.
- **This order changes authoritative rules**: `CLAUDE.md` §5's vocabulary table
  and its rail paragraph, `frontend/CLAUDE.md`, `frontend/BRIDGE.md`,
  `backend/CLAUDE.md`, `README.md` and `frontend/docs/index.html`. Those edits
  are part of the batch and are not optional.

### The deviation rule

If a step below is wrong, contradicted by the code, or cannot be done as
written: **stop, say so, and propose the alternative before writing it.**

---

## 1. Decisions taken

| # | Decision | Source |
|---|---|---|
| D1 | The rail lists **three** route sections, each switch bound to a REAL settings key: Built-in patterns (`useBuiltInPatterns`), Heuristic discovery (`useHeuristicDiscovery`), Local LLM discovery (`useLocalAI`). The `"derived"` sentinel and the master switch are deleted. | owner |
| D2 | Document country and the preset chips render under **Built-in patterns**. | owner |
| D3 | "Auto detected values" (the name categories) and the discovery-strictness block render under **Heuristic discovery**. | owner |
| D4 | The **match-confidence floor** is the one genuinely cross-route control left, so it moves to a switch-less panel of its own, "Detection quality", between the routes and Load profile. Placing a floor that governs three routes inside one of them is the same mislabelling this order removes. Superseded by `docs/change-12d.md`, which deletes the percentage entirely and replaces it with one checkbox; this order only gives it an honest home in the meantime. | mine, flagged |
| D5 | The **"Your own patterns"** group leaves the rail. `custom_patterns` becomes permanently on; its editor is already the workspace's Custom patterns tab. | owner |
| D6 | The built-in pattern categories are regrouped into **eight benchmark-derived sub-sections** (§3), chosen so enrichment lands in an existing group instead of forcing a re-grouping. | owner |
| D7 | **Signal-based discovery keeps its drill-downs on the email and website category rows**, inside Built-in patterns, because that is where the evidence is produced (`CLAUDE.md` §5). | owner |
| D8 | **"Smart detection" and "Local AI" are retired from every user-facing string**, in `frontend/copy.js`, in the Go strings that reach the user, in `README.md` and in the bundled docs. "Local AI" becomes **"Local LLM discovery"**. | owner |
| D9 | The name is LLM-specific rather than model-generic because the four encoder models the owner intends to add next (GLiNER2-PII, GLiNER2 base, Knowledgator gliner-pii, Piiranha-v1) get **their own section**, not a backend selector inside this one: different dependency (a bundled ONNX file, pattern P4, versus an installed Ollama service), different settings, different failure modes. | owner |
| D10 | Engine and persisted identifiers are untouched; the rename is `docs/change-12b.md`. | owner |

---

## 2. The rail, before and after

Before:

```
> Smart detection                        [On]   <- master over three mechanisms
      [x] Built-in patterns   [x] Heuristic discovery
      Document country
      Preset
      Categories                 (3 groups, signal drill-downs on email/url)
      Auto detected values       (11 name categories)
      Your own patterns          (custom_patterns)
      Match confidence
      > Discovery strictness
> Local AI                               [Off]
> Load profile
```

After:

```
> Built-in patterns                      [On]   useBuiltInPatterns
      Document country
      Preset
      > Contact details
      > Locations and addresses
      > Financial accounts
      > Government and tax identifiers
      > Health identifiers
      > Network and device identifiers
      > Credentials and secrets
      > Dates and monetary amounts
> Heuristic discovery                    [On]   useHeuristicDiscovery
      Auto detected values       (11 name categories)
      > Discovery strictness
> Local LLM discovery                    [Off]  useLocalAI
      Ollama port, model, context, detail level, what to scan
> Detection quality              (no switch)    0%
      Match confidence
> Load profile                   (no switch)
```

Fold defaults: Built-in patterns and Heuristic discovery **open**, every category
sub-group **folded**, Local LLM discovery / Detection quality / Load profile
**folded**. Unchanged in spirit from today: the rail opens on what a user changes.

### 2.1 The two things a reviewer will want to check

**The "Categories" label row disappears.** Under a section titled "Built-in
patterns", a block labelled "Categories" says nothing, and the panel's height is
its scarcest resource. The eight sub-groups sit directly in the section body. The
explanation currently in `RAIL.categoriesHelp` moves to the section header's help
tooltip.

**Switching Built-in patterns off must NOT disable the signal drill-downs.**
Signal-based discovery is gated only by `signalSuggestionSources`
(`backend/app_detect.go runSmartPhase`, `engine.DiscoverFromSignals` matches its
own evidence); `UseBuiltInPatterns` governs only whether the signal itself is
replaced. So with the section off, the category checkboxes render disabled as
they do today (`blockDisabled`) while each `.signal-drill` and every reading
inside it stays live. That asymmetry is the whole reason the separate setting
exists, and it needs a wiring test.

### 2.2 The two confidence settings are not the same setting

They are easy to read as one, and D4 only makes sense once they are apart.

| Control | Store | Engine | Scope |
|---|---|---|---|
| the "Discovery strictness" block: min length, min occurrences, its own min confidence (default 0.5), common words, lenient / balanced / strict | `settings.heuristicDiscovery` | `SmartDetectOptions`, read by `HeuristicDiscoverContext` | **heuristic discovery only**, which is why it nests inside that section (D3) |
| the "Match confidence" slider | `settings.minConfidence` | `FilterByMinConfidence` | **every producer**: pass-1 pattern spans, declared Values and custom patterns in `pipeline.go detectText`, and again over the Identify preview in `patternpreview.go` |

The slider is a floor on what a run is allowed to REPLACE, not a knob on what
heuristic discovery guesses. It reaches built-in pattern matching in one concrete
case that `pii.go` documents: a shape-valid IBAN whose check digits fail scores
`ConfidenceChecksumFailed` (0.7), and this slider is the only lever that drops
it. Above 0.99 nothing but pattern matches survives at all.

So it belongs to no single route, and the panel of D4 is the honest home for it
until `docs/change-12d.md` replaces the whole control with one checkbox.
Its help copy must say both things: that it applies to every route that is on,
and that it decides what a run replaces rather than what discovery suggests.

---

## 3. The eight sub-sections, and the benchmark behind them

The current three groups ("Contact and account details", "Payment, tax and
technical identifiers", "Only for thorough anonymisation") mix mechanism with
preset tier, so a new recognizer has no obvious home and the third group is named
after a UI shorthand rather than after what is in it.

What the established tools group by (checked August 2026):

| Tool | Top-level grouping |
|---|---|
| Azure AI Language PII, entity categories | Geolocation, Personal, Financial, Organization, DateTime, Azure-related (connection strings and keys), Government (every country-scoped ID) |
| Google Cloud Sensitive Data Protection infoTypes | `typeCategory` PII / SPII / GOVERNMENT_ID / DEMOGRAPHIC, crossed with `industryCategory` FINANCE / HEALTH, plus a "credentials and secrets" family (JWT, PASSWORD, HTTP_COOKIE, weak hashes) |
| Microsoft Presidio supported entities | global recognizers versus country-scoped recognizers, the second keyed by region |

Three convergent facts drive the mapping: **financial account numbers are their
own class**, **government and tax IDs are their own class and are country-scoped**
(which is exactly what `engine.CategoryCountries` already models), and
**credentials are separated from network identifiers** even though both look
"technical".

Two deliberate departures. **Health identifiers get their own group with one
member today** (`uk_nhs`), because health data is an Article 9 special category
under the GDPR in the owner's market: the split is regulatory, not taxonomic, and
the French NIR, insurance numbers and medical record numbers land in it
unmodified. **Dates and monetary amounts get a group** that no benchmark has,
because this application treats them as contextual identifiers switched on by the
Thorough preset rather than as PII.

| # | Group label (`CONFIGURE.group*`) | Categories | Benchmark anchor | What enrichment lands here |
|---|---|---|---|---|
| 1 | Contact details | `email`, `phone`, `url` | Azure Personal, DLP PII | fax, social handles, messaging IDs |
| 2 | Locations and addresses | `address`, `postal_code` | Azure Geolocation | city, region, GPS, plot references |
| 3 | Financial accounts | `iban`, `bic`, `credit_card`, `crypto` | Azure Financial, DLP FINANCE | routing and sort codes, account numbers, IBAN-like national forms |
| 4 | Government and tax identifiers | `vat`, `matricule`, `de_steuer_id`, `es_nif` | Azure Government, DLP GOVERNMENT_ID, Presidio country-scoped | passport, driving licence, per-country tax and company numbers |
| 5 | Health identifiers | `uk_nhs` | DLP HEALTH | NIR, social security, insurance and medical record numbers |
| 6 | Network and device identifiers | `ip_address`, `mac_address` | Presidio global, Azure Personal | hostnames, IMEI, device serials, VIN |
| 7 | Credentials and secrets | `database_uri` | DLP credentials and secrets, Azure Azure-related | API keys, bearer tokens, private keys, passwords |
| 8 | Dates and monetary amounts | `date`, `amount` | Azure DateTime | ages, durations, percentages |

Every one of the nineteen pattern categories appears **exactly once**, and the
guard for that is a test (§7). Group order is broadest-first, with the contextual
group last, matching how the presets escalate.

Sources for the benchmark, to be cited in the commit message and nowhere in the
code:

- <https://learn.microsoft.com/en-us/azure/ai-services/language-service/personally-identifiable-information/concepts/entity-categories>
- <https://docs.cloud.google.com/sensitive-data-protection/docs/concepts-infotypes>
- <https://microsoft.github.io/presidio/supported_entities/>

---

## 4. Frontend changes

### 4.1 `frontend/views/identifyrail.js`

1. `RAIL_SECTIONS` becomes three entries, every one with a real settings key:

   ```js
   export const RAIL_SECTIONS = [
     ["rail-patterns",  RAIL.tabPatterns,  "useBuiltInPatterns"],
     ["rail-heuristic", RAIL.tabHeuristic, "useHeuristicDiscovery"],
     ["rail-local",     RAIL.tabLocalLLM,  "useLocalAI"],
   ];
   ```

   The `"derived"` sentinel, `smartRouteOn()` and the `key === "derived"` branch
   in `railBody` all go. `rail-local` keeps its id: the id is internal and
   nothing is gained by churning it.

2. `sectionBody(s, id)` gains the two new bodies and loses `smartSection`:
   `patternsSection(s)` = country + preset + the eight category groups;
   `heuristicSection(s)` = the name-category block + the strictness subgroup;
   `localAISection(s)` unchanged except its copy.

3. `smartMethods()` is **deleted**. The two checkboxes it drew are now the two
   section header switches, and `wireSectionSwitches` routes each id to its own
   reducer:

   ```js
   if (route === "rail-local")          setUseLocalAI(on);
   else if (route === "rail-patterns")  setUseBuiltInPatterns(on);
   else if (route === "rail-heuristic") setUseHeuristicDiscovery(on);
   ```

   `setSmartDetection` loses its only caller (§4.2).

4. `CATEGORY_GROUPS` is rewritten to the eight groups of §3 plus the name group,
   and the index arithmetic (`slice(0, 3)` / `slice(3)`) is replaced by two named
   constants, `PATTERN_GROUPS` and `NAME_GROUPS`, exported for the tests. Nothing
   may address a group by position again: that arithmetic is what makes adding a
   group a two-place edit.

5. `DECLARED_CATEGORIES` is no longer rendered anywhere in the rail (D5).

6. `confidenceControl` moves into a new switch-less `rail-panel` section,
   `rail-quality`, whose `headRightHTML` carries the live percentage so the
   folded panel still states its value. `collapsedGroups` seeds
   `["rail-local", "rail-quality", "rail-profile"]` plus every category
   sub-group id.

7. The signal drill-downs keep `signalCategoryRow` untouched. Add the one-line
   comment stating why they stay live while the section's checkboxes are
   disabled (§2.1).

### 4.2 `frontend/state.js`

- `smartDetectionOn` and `setSmartDetection` are **deleted**. After the split
  their only callers are tests, and a dead export is what
  `scripts/deadexports` exists to catch. `detectionRoutesOn` does not use them
  and is unchanged, so the run gate keeps its current meaning exactly.
- `custom_patterns` is forced on wherever a category map is adopted:
  `presetCategories` already includes it in every level, so what is added is a
  normaliser on adoption, so a v10 file carrying `custom_patterns: false` cannot
  leave the user with a pattern editor whose patterns never run. Comment it as
  the invariant it is: the category has no switch, therefore it must never be
  off.
- `toggleCategory` refuses `custom_patterns` for the same reason.
- The settings-block comment at the top (lines ~92-115) is rewritten: there are
  three routes, each with its own switch, and no section flag to explain.

### 4.3 `frontend/copy.js`

New and renamed keys:

| Key | Value |
|---|---|
| `RAIL.tabPatterns` | `"Built-in patterns"` |
| `RAIL.tabHeuristic` | `"Heuristic discovery"` |
| `RAIL.tabLocalLLM` | `"Local LLM discovery"` |
| `RAIL.tabPatternsHelp` | the current `builtInPatternsHelp` text, which is already the right explanation |
| `RAIL.tabHeuristicHelp` | the current `heuristicDiscoveryHelp` text, extended with "Its suggestions are reviewed on this step; nothing is replaced until you accept it." |
| `RAIL.tabLocalLLMHelp` | says a language model running on this machine reads the text and suggests values, that it needs Ollama on 127.0.0.1, and that nothing leaves the machine |
| `RAIL.qualityTitle` | `"Detection quality"` |
| `RAIL.qualityHelp` | says the floor applies to every route that is on |

Deleted: `tabSmart`, `tabLocalAI`, `smartHelp`, `builtInPatterns`,
`builtInPatternsHelp`, `heuristicDiscovery`, `heuristicDiscoveryHelp`,
`categories`, `categoriesHelp` (folded into `tabPatternsHelp`), and
`CONFIGURE.groupDeclared`.

Reworded, each named here so none is missed:

| Location | Now | Becomes |
|---|---|---|
| `CONFIGURE.groupContact` and its two siblings | 3 group labels | the 8 labels of §3 |
| `RAIL.valuesAutoHelp` | "used by every detection route you switch on" | names Heuristic discovery and Local LLM discovery explicitly |
| `RAIL.localValuesHelp` | "same categories chosen under Smart detection above" | "under Heuristic discovery above" |
| `RAIL.signalSuggestionsHelp` | "governed by Built-in patterns and the signal's own category" | keep, and add that these readings keep working while Built-in patterns is off |
| `runNeedsRoute` | "Turn on Smart detection or Local AI in Configure." | "Turn on Built-in patterns, Heuristic discovery or Local LLM discovery in Configure." |
| detection phase label (`phase === "smart"`) | "Smart detection" | "Heuristic discovery" |
| discovery-method label `heuristic` | "Smart detection" | "Heuristic discovery" |
| discovery-method label `local_ai` | "Local AI" | "Local LLM discovery" |
| match-class label `smart_discovered` | "Smart detection" | "Heuristic discovery" |
| match-class label `local_ai_discovered` | "Local AI" | "Local LLM discovery" |
| `intersectionOrder` | "... then Smart detection, then Local AI." | "... then heuristic discovery, then local LLM discovery." |
| `builtInHint`, `builtInSwitchedOff`, `builtInNoCategories` | "under Smart detection" | "in the Built-in patterns section" |
| `CATEGORY_LABELS` entries reading "found by the AI review" (`brand_names`, `other_names`, `country_names`, `nationality_names`) | "the AI review" | "Local LLM discovery" |
| the strictness copy in `VALUES` / `CONFIGURE` | mentions smart detection | "heuristic discovery" |
| NEW `RAIL.presetAlsoSets(n)` | absent | the live read-out under the chips, naming how many auto detected values the level also switched on (§8). Deleted again by `docs/change-12c.md` |

`custom_patterns` keeps its `CATEGORY_LABELS` row: the parity guard
(`category_parity_test.go`) matches on that table, and the category still exists.

### 4.4 `frontend/style.css`

Comments naming "Smart detection" are rewritten. `.rail-section`,
`.rail-panel`, `.route-off` and `.rail-subgroup` need no rule change; confirm in
the harness that three sections plus two panels still fit the fold-and-scroll
behaviour the panel is measured for.

---

## 5. Backend changes (strings and comments only)

| File | Change |
|---|---|
| `backend/app_detect.go:254` | status string: name the three routes |
| `backend/app_detect.go:364` | "choose categories in Configure or turn on ..." names the three routes |
| `backend/app_detect.go:730` | skip reason: "Heuristic discovery still read it." |
| `backend/engine/conflicts.go:379` | the source label returned for `smart_discovered` becomes "Heuristic discovery"; the `local_ai` one becomes "Local LLM discovery" |
| comments in `backend/app.go`, `backend/app_detect.go`, `backend/engine/session.go`, `backend/engine/discover.go`, `backend/ollama/client.go` | where they explain a rule by naming "Smart detection", name the mechanism instead. Where they name the IDENTIFIER (`PhaseSmart`, `smart_discovered`, `local_ai`) they keep it and state the label it renders as |

No signature, no constant, no JSON key, no behaviour.

---

## 6. Authoritative documentation

| File | Edit |
|---|---|
| `CLAUDE.md` §5 vocabulary table | the "Detection route" row lists Built-in patterns, Heuristic discovery, Signal-based discovery and Local LLM discovery; the "Smart detection" row is replaced by a note that the term is retired from the interface and survives only as the engine identifiers `PhaseSmart` and `smart_discovered`; the "Local AI" row is renamed with the same note about `local_ai` |
| `CLAUDE.md` §5 provenance and precedence tables | the label column names the new words; the identifier columns are untouched |
| `CLAUDE.md` §5 rail paragraph (~line 727) | rewritten to the three-section shape; the "Smart detection's own state is DERIVED" rule is **removed**, because there is no longer a section without a key. The reason the rule existed is preserved in the new text: a section switch must be the flag it claims to be |
| `CLAUDE.md` §5 category-table intro and the sub-group prose | the categories are grouped by the eight classes of §3, and `custom_patterns` has no rail switch |
| `frontend/CLAUDE.md` (~88-95, ~199) | the three-route module map; `smartDetectionOn` no longer exists |
| `frontend/BRIDGE.md` (~125-146) | the WIRE contract is unchanged and must say so explicitly: three booleans and the nested `signalSuggestionSources`, no derived section flag |
| `backend/CLAUDE.md` (~154) | same correction |
| `README.md` (~69-81, ~173) | the user-facing description of the routes |
| `frontend/docs/index.html` | the bundled offline docs, same sweep |
| `docs/UITESTING.md` (~175) | the stated rail expectation becomes three route sections |

---

## 7. Tests this batch moves

Per `docs/TESTING.md`: frontend-only edits run `node --test "frontend/**/*.test.js"`
plus `go test .`; the string edits in `backend/` pull in `go test ./...`.

**Rewritten** (`frontend/identifyrail.test.js`): every
`all(..., "section.rail-section")[0]` index disappears in favour of a helper that
selects a section BY ID, so a reordering cannot make a test silently assert about
its neighbour. Deleted: the master-switch test and "Smart detection's two plain
methods lead the section".

**New tests**:

| Test | Asserts |
|---|---|
| the rail is three routes, each with a real key | `RAIL_SECTIONS` ids, labels and keys; no `"derived"` |
| each header switch writes its OWN flag | wiring: clicking `rail-patterns` leaves `useHeuristicDiscovery` alone, and the reverse |
| Built-in patterns owns country and preset | render: both inside `#rail-patterns` |
| Heuristic discovery owns the name categories and the strictness subgroup | render: both inside `#rail-heuristic`, exactly one `.rail-subgroup` |
| every pattern category appears exactly once, in one of the eight groups | over `HARD_PII_CATEGORIES + EXTENDED_PII_CATEGORIES + ADVANCED_PII_CATEGORIES` |
| `custom_patterns` has no checkbox and is always on | render plus store: after `applyPreset` on each level, and after adopting a session that says `false` |
| the signal drill-downs stay live while Built-in patterns is off | render: `.cat-toggle` disabled, `.signal-drill` and `.signal-box` not disabled |
| Detection quality is a switch-less panel carrying the floor | render: `.rail-panel`, no `.route-toggle`, the slider inside it |
| retired vocabulary guard (`frontend/copy.test.js`) | no exported copy string contains "Smart detection", "Smart Detection" or "Local AI" |
| retired vocabulary guard, Go side (`copy_guard_test.go`) | the same two phrases absent from the Go user-facing strings it already scans for em dashes |

**Updated**: `frontend/state.test.js` (the two deleted exports, the
`custom_patterns` invariant, per-flag defaults), `frontend/export.test.js`
(session restore asserted per flag), `backend/app_validation_integration_test.go:174`
(the expected substring), any Go test asserting a reworded status string.

**Harness** (`docs/UITESTING.md` layer 3):

- `scripts/uitest/probes.js`: `configureRail()` reports three `.rail-section`
  plus two `.rail-panel`, `routes: ["rail-patterns", "rail-heuristic",
  "rail-local"]`, and `smartOn` splits into `patternsOn` and `heuristicOn`; the
  expand arithmetic covers eight groups; the `section-label` selector used by the
  indentation probe is re-anchored on the country label, which is the first
  labelled block in Built-in patterns.
- `scripts/uitest/renderharness/checks.go` and
  `scripts/uitest/Invoke-UITest.ps1`: the matching assertions and their hint
  strings (both name `RAIL_SECTIONS`, both must name the new shape). The foot
  reachability and no-static-prose checks stay as they are and must still pass
  with the taller rail: that is the measurement that says the split did not cost
  the panel its usability.

---

## 8. Out of scope

- **Engine and persisted identifier renames**: `docs/change-12b.md`.
- **The ONNX NER route** (GLiNER2-PII, GLiNER2 base, Knowledgator gliner-pii,
  Piiranha-v1): its own order. This one only makes room for it by naming the
  Ollama route after what it is (D9). Nothing here may add a model, a runtime or
  a dependency.
- **Preset semantics.** A preset still fills BOTH the pattern categories and the
  name categories (`CLAUDE.md` §5 anonymisation levels), so a chip under
  Built-in patterns also changes the selection under Heuristic discovery. That is
  a domain rule, not a UI one, and narrowing it is not a pure-UI change. It is
  made honest instead of changed: the chip row carries a live read-out naming
  what else the level switched on, which is dynamic information and therefore
  allowed inline (`CLAUDE.md` §5, Configure panel). Scoped presets, and the
  preset FAMILIES behind them, are `docs/change-12c.md`, which deletes this
  read-out again once a chip can no longer reach across sections.
- **Signal-based discovery as its own section.** Rejected in D7 and by
  `CLAUDE.md` §5: the readings render beside the pattern that produces the
  evidence.
- Any change to the Identify-to-Anonymise gate, to `detectionRoutesOn`, or to
  what a run applies.

---

## 9. Execution order

1. **Copy first** (`frontend/copy.js`): the new keys, the deletions, the sweep of
   §4.3. Add the retired-vocabulary guard to `frontend/copy.test.js` and watch it
   fail, then go green. Copy first because every later step reads these keys.
2. **State** (`frontend/state.js`): delete the two exports, add the
   `custom_patterns` invariant and its tests, rewrite the settings comment.
3. **Rail structure** (`frontend/views/identifyrail.js`): `RAIL_SECTIONS`, the
   three section bodies, the deleted `smartMethods`, the wiring, the new
   `rail-quality` panel, `PATTERN_GROUPS` and `NAME_GROUPS`.
4. **Category regrouping**: the eight groups, the fold-default ids, the
   exactly-once test.
5. **Backend strings and comments** (§5), plus the Go guard and the integration
   test expectation.
6. **Harness** (§7): probes, `checks.go`, `Invoke-UITest.ps1`. Run the Linux
   render harness; it is a blocking CI step.
7. **Documentation** (§6), last, so it describes what was actually built.

### Files this batch touches

```
frontend/copy.js
frontend/state.js
frontend/style.css
frontend/views/identifyrail.js
frontend/copy.test.js
frontend/state.test.js
frontend/export.test.js
frontend/identifyrail.test.js
backend/app.go                 (comments)
backend/app_detect.go          (3 strings + comments)
backend/engine/conflicts.go    (2 labels)
backend/engine/session.go      (comments)
backend/engine/discover.go     (comments)
backend/ollama/client.go       (comments)
backend/app_validation_integration_test.go
copy_guard_test.go
scripts/uitest/probes.js
scripts/uitest/renderharness/checks.go
scripts/uitest/Invoke-UITest.ps1
CLAUDE.md  frontend/CLAUDE.md  frontend/BRIDGE.md  backend/CLAUDE.md
README.md  frontend/docs/index.html  docs/UITESTING.md
```

---

## 10. Acceptance criteria

1. `go test ./...` and `node --test "frontend/**/*.test.js"` both green.
2. The Linux rendering harness green, including the foot-reachable and
   no-static-prose checks, with the new rail shape.
3. `git diff` shows **no logic change** under `backend/engine/` and
   `backend/ollama/`: strings and comments only. `SessionVersion` is still 10,
   and a session file saved before this batch still loads.
4. Grepping the repository for `Smart detection`, `Smart Detection` and
   `Local AI` returns hits ONLY where an engine identifier is being named and
   explained, and no hit at all in `frontend/copy.js`, `README.md` or
   `frontend/docs/index.html`.
5. Turning Built-in patterns off and running detection still produces
   signal-derived suggestions, and still previews no pattern matches. Turning
   Heuristic discovery off stops the heuristic suggestions and nothing else.
6. Every one of the nineteen pattern categories is reachable in exactly one
   sub-group, `custom_patterns` has no switch anywhere in the rail, and the
   "N of M categories on" read-out still counts it as on.
7. The three route switches each write exactly one settings flag, verified by a
   wiring test rather than by reading the code.
