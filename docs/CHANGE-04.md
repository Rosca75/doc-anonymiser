# CHANGE-04 - Detection terminology, signal-derived suggestions and Identify UX

You are implementing a cross-cutting change against the existing
**doc-anonymiser** repository. The application is a Windows-first Wails v2
desktop application written in pure Go with a vanilla JavaScript frontend. It
has no npm dependencies, performs no network I/O except to a local Ollama
instance, and must never introduce CGo.

This document is the implementation handoff for a fresh GPT-5.6 Sol session.
Read the root `CLAUDE.md`, `backend/CLAUDE.md`, `frontend/CLAUDE.md` and
`frontend/BRIDGE.md` before editing. Those files remain authoritative for
repository-wide constraints. Update them as part of this change so they
describe the final implementation rather than the current one.

The change intentionally breaks internal and persisted contracts.
**Backward compatibility is not required.** Delete obsolete types, fields,
functions, comments, tests and compatibility aliases instead of preserving
them. Session files from the previous schema must be refused, never migrated.

## 1. Why this change is needed

The application currently uses several overlapping vocabularies:

- `Entity`, `Value`, `Candidate` and `ProposedEntity` describe different
  lifecycle stages of substantially the same domain object.
- `Canonical`, `Variant` and `ManualVariants` describe what the interface calls
  a Value and its spellings.
- `Trigger`, `source`, `route` and `Origin` are used for both provenance and
  overlap precedence.
- "Smart detection" contains two differently behaving features called Native
  detection and Auto detection.
- Smart and Local AI results travel through separate `candidates` and
  `proposals` arrays even though the interface presents both as Suggestions.

This is not only a naming problem. The split response shape already loses data:
the backend preserves folded spellings on Local AI proposals, but
`frontend/views/identifyworkspace.js` currently maps Local AI proposals to
`{text, category}` and discards those spellings before adding them to frontend
state.

There is also a product gap around built-in signals. An email address such as
`pierre.dupont@tpps.com` is correctly matched and anonymised directly, but the
address can also provide deterministic evidence for names and organisations
written elsewhere in the documents. Those inferred strings must become
reviewable Suggestions. They must not be silently created or replaced as if
they were direct regex matches.

Finally, step 3 retains a general literal Find and replace facility inherited
from the notebooks. It bypasses the Value model, category reporting,
placeholder ownership and the re-identification key. Its legitimate
anonymisation use cases are covered by Values and custom patterns, so it should
be removed.

## 2. Non-negotiable decisions

1. Built-in patterns continue to match and replace direct signals such as
   emails, phone numbers, VAT numbers and IBANs during anonymisation.
2. A built-in pattern may additionally provide evidence for
   **signal-based discovery**.
3. Signal-based discovery produces Suggestions, never accepted Values.
4. Every Suggestion must be accepted or rejected on Identify before it can
   become a Value.
5. **No new Value should be created invisibly during Anonymise. The current
   Local AI residual scan must be removed, so every AI-discovered Value passes
   through Suggestions.**
6. Smart detection and Local AI remain the two user-operable detection routes.
7. The user can independently enable or disable each signal source that may
   derive Suggestions. Disabling signal-derived Suggestions from email must
   not disable direct email anonymisation.
8. Find and replace and the Compare action "Replace the text only" are removed.
9. "Something missed?" is renamed **Add missed Value**. It remains a manual
   Value declaration followed by a fast deterministic rerun.
10. Cloud AI is not implemented and should leave no placeholder UI, state,
    documentation or tests.
11. Provenance and overlap priority are separate concepts and must not share
    one field.
12. Session schema version 7 is the only accepted schema after this change.
    Version 6 files are refused with an actionable message.

## 3. Authoritative terminology

Use the following terminology consistently in Go names, JavaScript names, JSON,
comments, frontend copy and documentation.

### 3.1 Detection terms

| Term | Definition | Output |
|---|---|---|
| **Detection route** | A switchable user-facing feature group | Smart detection or Local AI |
| **Smart detection** | Built-in, non-AI detection route | Direct pattern matches and Suggestions |
| **Built-in pattern matching** | Application-provided patterns for structured signals | Direct matches |
| **Signal-based discovery** | Uses a direct signal match as evidence to find related text | Suggestions |
| **Heuristic discovery** | Uses spelling, context, frequency and deterministic gazetteers | Suggestions |
| **Local AI** | Ollama-backed detection route used during Identify | Suggestions |
| **Local AI discovery** | The Local AI method that extracts potential Values | Suggestions |
| **Custom pattern matching** | User-authored regular expressions | Direct matches |
| **Manual Value declaration** | A Value explicitly entered by the user | Accepted Value |

Smart detection is a **route** containing:

1. built-in pattern matching;
2. signal-based discovery;
3. heuristic discovery.

Local AI is a **route** containing Local AI discovery. It must have no
Anonymise-time residual discovery operation.

Not every detection method produces Suggestions. Built-in pattern matching and
custom pattern matching produce direct matches. Signal-based discovery,
heuristic discovery and Local AI discovery produce Suggestions. Manual Value
declaration creates a Value directly.

### 3.2 Value terms

| Term | Definition |
|---|---|
| **Suggestion** | An unreviewed potential Value produced by a discovery method |
| **Value** | An accepted replacement unit with one placeholder |
| **Main text** | The primary textual form naming a Value |
| **Spelling** | An alternative textual form of the same Value |
| **Value family** | A Value's main text together with all its spellings |
| **Spelling policy** | Whether spellings are automatically derived or user-curated |
| **Evidence** | Why a discovery method produced a Suggestion |
| **Related Values** | Suggestions or Values that may represent the same subject |
| **Intersection** | Two matches that claim overlapping character ranges |
| **Placeholder** | The replacement assigned to one Value |
| **Re-identification key** | The mapping from placeholders back to Values |

The authoritative model is:

```text
Value
├── category
├── mainText
├── spellings[]          // excludes mainText
├── spellingPolicy       // "automatic" or "curated"
├── discoveryMethods[]
└── evidence[]
```

`mainText` is always matched but is not duplicated in `spellings`.

When `spellingPolicy` is `automatic`, the engine may derive additional
spellings according to the category. When the user adds, deletes, renames or
moves a spelling, the policy becomes `curated`. From that point, `mainText`
plus the displayed `spellings` array is the complete replacement set.

### 3.3 Provenance and precedence

`Origin` currently answers both "how was this found?" and "which overlapping
claim wins?". Remove it and model those questions separately.

`discoveryMethods` is provenance. It is a set because several methods may find
the same Suggestion:

```json
["signal", "heuristic", "local_ai"]
```

Accepting a Suggestion preserves all its methods and evidence. A manually
declared Value uses `["manual"]`.

`matchClass` is an engine-internal precedence input:

| Match class | Rank | Produced by |
|---|---:|---|
| `built_in_pattern` | 1 | Built-in structured pattern match or an established registry entry |
| `user_defined` | 2 | Manual Value or custom pattern |
| `smart_discovered` | 3 | Signal-based or heuristic discovery |
| `local_ai_discovered` | 4 | Local AI discovery |

Lower rank wins. If one Value has several discovery methods, use the strongest
applicable class. This preserves deterministic overlap behavior while allowing
provenance to remain complete.

`matchClass` should not be exposed as user-editable state. User-facing
intersection copy names the winning method or route, not an internal rank.

### 3.4 Frontend terms

Use these names in future change requests and frontend documentation:

| Term | Meaning |
|---|---|
| **Identify step** | Wizard step 2 |
| **Configure panel** or **left rail** | Left side of Identify |
| **Detection section** | Smart detection or Local AI collapsible section |
| **Review workspace** | Right side of Identify |
| **Suggestions tab** | Unreviewed Suggestions |
| **My Values tab** | Accepted Values |
| **Suggestion row** | One reviewable Suggestion |
| **Value card** | One accepted Value |
| **Spelling chip** | One alternative spelling |
| **Help tooltip** | Explanatory text shown on hover or keyboard focus |
| **Compare pane** | Original or anonymised document pane |
| **Add missed Value** | Manual Value declaration on Anonymise |

## 4. Contract and identifier migration

Rename the domain consistently rather than wrapping the existing names.

| Current name | New name |
|---|---|
| `Entity`, `entities` | `Value`, `values` |
| `Canonical` | `MainText` |
| `Variant`, `variants` | `Spelling`, `spellings` |
| `ManualVariants` | `Spellings` |
| `AutoExpand` | `SpellingPolicy` |
| `ExpandVariants` | `ExpandSpellings` |
| `FoldValueFamilies` | keep, but operate on Suggestions and spellings |
| `Candidate` | `Suggestion` |
| `ProposedEntity` | `Suggestion` |
| `Candidates`, `Proposals` | `Suggestions` |
| `Origin` | remove |
| `source` | `discoveryMethods` and `evidence` |
| `UseNativeDetect` | `UseBuiltInPatterns` |
| `UseAutoDetect` | `UseHeuristicDiscovery` |
| `UseAI` | `UseLocalAI` |
| `SmartDetectOptions` | `HeuristicDiscoveryOptions` |
| `Something missed?` | `Add missed Value` |

Delete the derived `UseSmartDetect` setting. The Smart detection section is on
when any of its methods is on. Its section-level switch remains a UI master
that changes its child settings in one action; it is not persisted as a fourth
independent boolean.

Engine category identifiers such as `entity_names`, `person_names` and
`custom_patterns` are stable category contracts and are not mechanically
renamed by this change.

Rename files such as `entitymodel.js` or `entities.go` only when doing so
clarifies the final structure and does not create artificial package
fragmentation. There must be no compatibility aliases using retired domain
names.

## 5. Unified Suggestion contract

Replace the split detection result:

```json
{
  "candidates": [],
  "proposals": []
}
```

with one route-independent result:

```json
{
  "suggestions": [
    {
      "mainText": "Pierre Dupont",
      "category": "person_names",
      "spellings": [],
      "count": 3,
      "contexts": ["Contact Pierre Dupont for approval."],
      "discoveryMethods": ["signal", "heuristic"],
      "evidence": [
        {
          "kind": "email_local_part",
          "signalCategory": "email",
          "signalText": "pierre.dupont@tpps.com",
          "documents": ["engagement.md"]
        }
      ]
    }
  ],
  "phases": ["smart", "local_ai"],
  "skipped": [],
  "errors": [],
  "cancelled": false,
  "status": "..."
}
```

The exact evidence fields may be tightened during implementation, but evidence
must be structured, deterministic and bounded. Do not return arbitrary prose
from the engine and then parse it in the frontend.

Merging Suggestions must:

- deduplicate `mainText` case-insensitively within a category;
- combine occurrence counts and contexts without unbounded growth;
- union discovery methods;
- deduplicate evidence;
- preserve spellings from every contributing method;
- fold Value families once over the unified result;
- retain the shorter form as main text when the existing family rule applies.

This one response shape removes the mapping seam that currently drops Local AI
spellings.

## 6. Signal-based discovery

### 6.1 Email behavior

An email produces two independent effects:

1. Built-in pattern matching identifies the complete email as a direct match.
2. When email-derived Suggestions are enabled, signal-based discovery extracts
   conservative person and organisation seeds and searches the imported batch.

Example:

```text
pierre.dupont@tpps.com
    ├── local-part evidence
    │   └── Suggestion: Pierre Dupont
    └── domain evidence
        ├── Suggestion: TppS France
        └── Suggestion: Tpps S.A.
```

The discovery pass must:

- use the entire imported batch, not only the document containing the email;
- preserve each matched document's actual spelling and casing;
- ignore text located inside the source email span;
- emit no Suggestion unless corresponding text occurs outside the signal;
- reject role mailboxes such as `info`, `support`, `billing` and `noreply`;
- reject public or generic email providers such as Gmail, Outlook and Yahoo as
  organisation seeds;
- reject infrastructure-only domain components and country/TLD fragments;
- use word boundaries and minimum-length safeguards;
- respect the allowlist and session removals;
- attach bounded evidence and contexts;
- merge with heuristic and Local AI findings rather than creating duplicate
  Suggestions.

Do not automatically group `TppS France` and `Tpps S.A.`. Shared domain
evidence makes them **Related Values**, but legal entities or country branches
may genuinely differ. The user confirms grouping.

### 6.2 Compact source controls

The user must control which built-in signals may derive Suggestions without
confusing this with direct signal anonymisation.

Inside the Smart detection section, add one compact summary control:

```text
Signal-based suggestions        Email addresses ▾
```

Activating it opens a small checklist:

```text
Suggestion sources
☑ Email addresses               Names and organisations
```

Only signal categories that actually implement discovery appear here. The
control is deliberately data-driven so future sources do not add permanent
rows to the rail.

Behavior:

- clearing Email addresses disables only email-derived Suggestions;
- direct email pattern matching remains governed by Built-in patterns and the
  Email category checkbox;
- no redundant master boolean is stored;
- the closed summary reads `Off`, the sole enabled source name, or
  `N sources`;
- the checklist is keyboard operable and closes on Escape;
- explanatory detail is available through a help tooltip;
- the setting persists in profiles and sessions.

Persist stable identifiers:

```json
{
  "signalSuggestionSources": {
    "email": true
  }
}
```

Define the supported identifiers once in Go, mirror them in frontend state and
add a parity guard.

## 7. Remove invisible Local AI discovery

Delete the Local AI residual scan from Anonymise:

- remove the deep-scan engine interface and pipeline pass;
- remove the residual-scan Ollama prompt and response parser;
- remove its progress phase and timing/report fields;
- remove settings or request fields used only by residual scanning;
- remove related report copy, status copy, comments and tests;
- ensure same-format export never invokes discovery.

Local AI is now exclusively an Identify-time discovery route. Every Local AI
finding enters the unified Suggestions list and therefore participates in the
review gate.

## 8. Remove Find and replace

Delete the literal replacement facility completely:

- `backend/engine/simplereplace.go`;
- `SimpleRule` and its tests;
- the pipeline's final ordered replacement pass;
- `simple_replace` report rows and category counts;
- simple-rule conflict validation;
- registry reservation methods and reserved-placeholder state used only by
  rules;
- `nextRulePlaceholder` and related bound-app/API methods;
- `simpleRules` from run requests, frontend state, profiles and sessions;
- the Find and replace card on Anonymise;
- the Compare action "Replace the text only";
- related copy, CSS, comments and tests.

Before deleting generic registry reservation code, confirm with a repository
search that no remaining feature uses it.

Compare retains:

1. **Make it a spelling of an existing Value**
2. **Add it as a new Value**

Rename the Anonymise recovery card to **Add missed Value**. Values created
there use manual Value declaration, receive a category and placeholder, enter
the re-identification key, support spellings and grouping, and are applied by
the fast deterministic rerun.

## 9. Simplify the Configure panel

The current rail spends too much vertical space explaining controls. Remove
inline explanatory paragraphs including the current text beginning:

- "Finds names by how they are written..."
- "Regex signals such as emails..."
- "Finds recurring names by word frequency..."
- "The phone, VAT and national identification examples..."
- "Start from a preset..."
- "These categories are used by every detection route..."
- "Every detection carries a score..."
- "Smart detection guesses which words are names..."

Review the whole Configure panel and apply the same rule to equivalent prose.
Do not merely delete those exact literals while leaving nearby replacements.

Keep concise visible labels:

- Smart detection
- Built-in patterns
- Signal-based suggestions
- Heuristic discovery
- Document country
- Preset
- Categories
- Match confidence
- Discovery strictness
- Local AI
- Scan scope

Move explanations into a shared **help tooltip** component:

- a small information icon beside the relevant label;
- opens on pointer hover and keyboard focus;
- remains readable while the icon or tooltip has focus/hover;
- closes on pointer leave, blur and Escape;
- uses `aria-describedby`;
- does not use native dialogs;
- is positioned outside the rail's clipping scroll container;
- uses copy from `copy.js`, never hardcoded view text.

Keep essential dynamic information inline:

- validation and input errors;
- Ollama availability;
- current confidence value;
- page/range selection counts;
- active category counts;
- detection progress and status.

Update the real-rendering probes so they verify the rail fits and tooltips are
not clipped.

## 10. JSON and bridge migration

Update every JSON producer and consumer, not just type and variable names.

### 10.1 Detection

- `DetectionResult.suggestions`
- runtime `detection:done` payload
- Suggestion fields `mainText`, `spellings`, `discoveryMethods`, `evidence`
- cancellation and partial-result merge paths

### 10.2 Values and requests

- run request `values`
- Value fields `mainText`, `spellings`, `spellingPolicy`,
  `discoveryMethods`, `evidence`
- expansion API renamed around spellings
- intersection request and response fields
- removals and restored Value payloads

### 10.3 Sessions and profiles

Write only schema version 7:

```text
version: 7
values
allowTerms
patterns
settings
registry
placeholderOverrides
removedValues
retiredPlaceholders
```

Remove:

- `entities`;
- `manualVariants`;
- `autoExpand`;
- `origin`;
- `simpleRules`;
- `reservedPlaceholders`;
- old Smart detection compatibility flags.

Version 6 is refused. Do not add aliases or migration code.

### 10.4 Mapping and reports

Use Value/Spelling terminology in JSON where the field represents the domain
model. Preserve `original` only where it literally means source text rather
than a Value's main text.

Remove all `simple_replace` reporting. Update report builders and exported JSON
tests. Ensure mapping JSON and CSV remain unambiguous re-identification keys.

### 10.5 Ollama JSON

Ollama prompts continue to require strict JSON using the stable engine category
keys. Convert parsed model output immediately into unified Suggestions.

Remove residual-scan prompt JSON and parsers. Update discovery parsing,
multi-chunk merging, hallucination filtering and tests to use the new
Suggestion shape.

### 10.6 Contract documentation

Rewrite `frontend/BRIDGE.md` after the code contract settles. It must document
the actual version 7 request and response shapes, signal source settings and
runtime events. Do not describe old names parenthetically.

## 11. Documentation and comment cleanup

Update:

- root `CLAUDE.md`;
- `backend/CLAUDE.md`;
- `frontend/CLAUDE.md`;
- `frontend/BRIDGE.md`;
- `README.md`;
- bundled documentation under `frontend/docs/`;
- source comments and test descriptions.

The README currently describes obsolete wizard structure and Cloud AI. Rewrite
it from the implemented product state.

Comments must explain present intent. While touching a file, remove narratives
about previous bugs, removed functions, previous layouts, phase numbers and
superseded behavior. Git history and `docs/CHANGE-*.md` preserve that history.

Historical change-order files do not need their old terminology rewritten.
They describe the state and decisions at their own point in history.

At completion, repository searches should find no active-code use of the
retired domain terms except unavoidable third-party concepts or historical
documents.

## 12. Suggested implementation sequence

The work should remain one branch and one PR because the data model, bridge and
parity guards must move atomically. Use delegated agents for bounded work, but
only one write-owning agent at a time.

### Phase 1 - Structural deletion

Delete:

- Find and replace;
- Local AI residual scan;
- Cloud AI placeholder;
- their state, JSON, reports, copy and tests.

Run only targeted Go and frontend tests for those surfaces. Commit the coherent
deletion before continuing.

### Phase 2 - Terminology and contracts

Introduce the final Value, Suggestion, Spelling, discovery-method and
match-class models. Unify detection results and update bridge JSON. Bump the
session schema to 7.

This phase should be owned by the strongest reasoning model because it changes
the load-bearing domain contract and parity guards.

### Phase 3 - Signal-based discovery

Implement email-derived person and organisation Suggestions, whole-batch
matching, evidence, public-provider suppression, relatedness and merging.

Add engine table tests before wiring the frontend.

### Phase 4 - Frontend review and Configure UX

Wire unified Suggestions into state, preserve all spellings and evidence,
render relatedness and discovery methods, add compact source controls, and
replace Configure prose with accessible help tooltips.

### Phase 5 - Documentation and dead-term sweep

Update current-state documentation and comments. Search for retired terms,
fields, JSON keys and dead bridge methods. Delete rather than explain obsolete
code.

### Phase 6 - Integrated validation

The main coordinator runs the complete non-regression gate once after all
implementation phases and before creating the PR.

## 13. Token-efficient execution strategy

Use a fresh GPT-5.6 Sol session as the main coordinator with high reasoning and
long context. It owns architecture, sequencing, integration review and the
final PR.

Delegate bounded tasks with standalone prompts. Child agents cannot see the
coordinator conversation, so every prompt must include the glossary, owned
files, constraints and definition of done.

Recommended allocation:

| Work | Model | Effort | Context |
|---|---|---|---|
| Structural deletion | GPT-5.3-Codex | medium | default |
| Domain and JSON contract | Claude Sonnet 5 or GPT-5.6 Sol | high | long |
| Signal discovery engine | Claude Sonnet 5 | high | default |
| Frontend UX | Claude Sonnet 5 | medium | default |
| Documentation sweep | Claude Haiku 4.5 | default | default |
| Command execution | lightweight task agent | low | default |

Efficiency rules:

- Keep one branch and one PR.
- Let only one editing agent own the worktree at a time.
- Parallelise only read-only audits or independent research.
- Commit after each coherent phase so later review can focus on its delta.
- Give agents exact file ownership to avoid repeated repository exploration.
- Maintain a test-ownership matrix.
- Each implementation agent runs only the smallest tests covering its slice.
- Documentation-only work runs no application suites.
- Use task agents for verbose commands so successful logs do not consume the
  coordinator context.
- Do not repeat full suites after every phase.
- The coordinator reviews every phase diff before starting the next.
- Use exact retired-term searches as measurable completion checks.
- If an agent discovers an architectural ambiguity, return it to the
  coordinator rather than inventing a compatibility layer.

Suggested test ownership:

| Phase | Targeted validation |
|---|---|
| Structural deletion | pipeline, session, Anonymise and API tests |
| Contract migration | Value, registry, intersections, session, bridge and state tests |
| Signal discovery | discovery, family folding and detection orchestration tests |
| Frontend UX | Identify rail, Suggestions, My Values and rendering probes |
| Documentation | copy guards and retired-term searches only |

The coordinator performs the final gates:

```powershell
go test ./...
node --test "frontend/**/*.test.js"
go run ./scripts/uitest/renderharness
task audit
wails build
```

Use the commands already present in the repository. Do not install new testing
or build tools unless a declared dependency is genuinely missing.

## 14. Required tests

### Engine

- signal source setting validation and defaults;
- email local-part person discovery;
- email domain organisation discovery;
- whole-batch discovery;
- no Suggestion from text found only inside the email;
- role mailbox suppression;
- public-provider suppression;
- allowlist and removal vetoes;
- merging signal, heuristic and Local AI evidence;
- multiple discovery methods reduce to the strongest match class;
- Value family folding preserves all spellings;
- curated spelling policy prevents re-expansion;
- intersections preserve the specified precedence;
- no Local AI invocation occurs during Anonymise;
- schema 7 round trip and schema 6 refusal;
- mapping and report JSON use the new shapes;
- no simple replacement or reserved-placeholder paths remain.

Use table-driven tests for engine logic.

### Frontend

- one unified Suggestions response reaches state without losing Local AI
  spellings;
- Suggestions merge methods and evidence;
- accepting a Suggestion preserves provenance and evidence;
- My Values renders discovery methods, evidence and relatedness;
- source checklist changes signal discovery without changing email matching;
- Smart section master and child settings remain coherent;
- help tooltips work through hover and keyboard focus;
- Configure explanatory paragraphs are absent from the visible rail;
- Add missed Value creates a real Value;
- Compare exposes only spelling and new-Value actions;
- no Find and replace or Cloud AI UI remains;
- review gate behavior is unchanged.

Update rendering probes for the compact rail and unclipped tooltips.

### Parity and shape guards

- category parity remains intact;
- add discovery-method parity;
- add signal-source parity;
- replace origin parity with match-class or discovery-method guards as
  appropriate;
- update entity-shape guards to the Value schema;
- keep copy, step, frontend-test and UI-test parity guards load-bearing.

## 15. Acceptance criteria

The change is complete only when:

1. `pierre.dupont@tpps.com` remains directly anonymised as an email.
2. When enabled and present elsewhere, `Pierre Dupont`, `TppS France` and
   `Tpps S.A.` can appear as signal-derived Suggestions.
3. Signal-derived Suggestions follow the same review lifecycle as heuristic
   and Local AI Suggestions.
4. The user can disable email-derived Suggestions without disabling email
   anonymisation.
5. Related Suggestions are never grouped without user confirmation.
6. Accepted Suggestions retain all discovery methods, evidence and spellings.
7. Local AI spellings survive from backend detection through frontend state.
8. Local AI creates no Value during Anonymise.
9. Find and replace, residual scan and Cloud AI leave no dead state, bridge
   method, JSON field, report category, session field or UI.
10. Add missed Value creates a normal Value with a placeholder and
    re-identification entry.
11. Configure help is available on hover and keyboard focus without consuming
    permanent vertical space.
12. JSON, comments, function names, variables, frontend copy, README, charters
    and BRIDGE use the agreed terminology.
13. Session version 7 round-trips and every other version is refused.
14. Both test suites, rendering tests, audit and build pass.

## 16. First actions for the implementation coordinator

1. Read the four repository charters and this change order.
2. Inspect the current branch and existing tests.
3. Create a phase and test-ownership checklist outside the repository.
4. Run baseline targeted tests only where needed to distinguish existing
   failures.
5. Delegate Phase 1 with a complete prompt.
6. Review and commit each phase before beginning the next.
7. Keep the final glossary visible in every delegated prompt.
8. Stop and ask the owner only when a real product decision is not answered by
   this document. Do not preserve old behavior by default.
