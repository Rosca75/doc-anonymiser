# docs/mockups — the BUILD-05 GUI design source of truth

These five files are the owner's GUI redesign, and they are the design
authority for `docs/BUILD-05.md`. Read them before changing any screen; the
plan summarises them but does not replace them.

| File | Screen | Repo target |
|---|---|---|
| `Welcome.dc.html` | Welcome / Home | `frontend/views/home.js` |
| `Import.dc.html` | Step 1, Import | `frontend/views/import.js` |
| `Identify.dc.html` | Step 2, Identify (configure rail + workspace) | `frontend/views/identify.js`, `identifyrail.js`, `allowlist.js` |
| `Anonymise.dc.html` | Step 3, Anonymise | `frontend/views/anonymise.js` |
| `Export.dc.html` | Step 4, Export | `frontend/views/export.js` |

## How to read them

They are Claude Design components, **not runnable pages**. Each file is markup
plus a `<script type="text/x-dc">` block holding a `Component` class whose
`renderVals()` returns the values the markup interpolates. The templating tags
are `<sc-for list="{{ x }}" as="i">` and `<sc-if value="{{ flag }}">`.

Each file references a `support.js` runtime that is deliberately **not**
committed: these are specifications to read, not pages to serve. Opening one in
a browser will show an unstyled skeleton, which is expected.

The `renderVals()` bodies are the most precise statement of intended
behaviour: exact copy strings, sort and filter rules, tint choices, and state
transitions. When the markup and the script disagree, the script is what the
owner exercised in the design tool.

## Known inconsistencies

The set was built over several sessions, so it is not internally consistent.
`docs/BUILD-05.md` §3 records how each conflict was resolved. The two to know
before reading:

- `Welcome.dc.html` and `Import.dc.html` still show **five** steps with a
  standalone Configure. `Identify`, `Anonymise` and `Export` show **four**,
  with Configure absorbed into Identify. Four is the target (decision 1).
- The confidence-slider copy in `Identify.dc.html` describes a source-tiered
  rule the engine does not implement. The engine's `minConfidence` floor wins
  and the copy is rewritten (decision 3).

These files are a historical design record. Do not edit them to match the
implementation; record deviations in `docs/BUILD-05.md` instead.
