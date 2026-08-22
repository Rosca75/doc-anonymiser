# CHANGE-UI — Reframing the frontend on a token layer

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0, pure Go + Wails v2, no CGo, no npm). It is **SEVEN
BATCHES** plus one gated batch that is deliberately outside the order's own
guarantee.

The subject is not "tidy the CSS". `frontend/brand.css` is a real design system
for **colour**: 48 named tokens, each commented with the state it encodes, and a
charter rule that `style.css` may consume nothing else. The other half of a
design system, everything that is not colour, was never written down. Type
scale, spacing, radius, elevation and z-index have no tokens, so each of those
decisions is made at the call site, 2,255 lines deep, by whoever wrote that
screen.

The visible result is the reported inconsistency in headers, popups and font
sizes. The structural result is that the frontend cannot be lifted into a second
project, because components name a specific palette rather than the roles they
fill.

## Starting conditions (the census, measured on this branch)

Every number below is measured from `frontend/style.css` (2,255 lines),
`frontend/brand.css` and `frontend/ui.js`. Re-derive them at the start of batch 1
rather than trusting this list: the census is the plan's input, so a stale census
is a wrong plan.

| Property | Declarations | Distinct values | Token coverage |
|---|---:|---:|---|
| `font-size` | 146 | 17 (0.65rem to 2rem) | none |
| `padding` (all forms) | ~230 | 31 | 1 (`--workspace-pad`) |
| `gap` | ~150 | 24 | none |
| `border-radius` | 72 | 13 | 16 uses of `--card-radius` |
| `box-shadow` | 6 | 5 | none |
| `z-index` | 7 | 5 (`6`, `30`, `60`) | none |

Four specific findings drive the batch order:

- **F1. The same component renders at two sizes.** `.card-head h2` is `1.2rem`;
  `.meta-review .card-head h2` overrides it to `0.95rem`. A screen reached past
  the component and re-specified it, which is what a component exists to
  prevent. `.image-panel-head h2` is a third heading size (`1rem`) on a surface
  that is structurally the same head.

- **F2. Seven floating surfaces, built three ways.** `.modal-layer` (line 534)
  and `.image-panel-layer` (line 2206) are byte-identical fixed backdrops
  written twice. `.help-bubble` (1860) and `.warnpop-bubble` (1900) share
  `placeBubble` but re-declare the same border, radius, padding and shadow
  independently. `.mark-tooltip` (1722) and `.selection-card` (1631) position
  themselves absolutely with their own maths and their own two shadow values.
  The spellings popup reuses the confirm's shape by copy.

- **F3. `style.css` already breaks its own header rule.** The file opens with
  "No literal colour or font values may appear in this file" and contains nine:
  two `rgba(0,0,0,0.28)` scrims, five shadow literals, and `#FFFFFF` twice.
  Every one of them is an overlay value, which is exactly the class with no
  token to reach for. The rule was not ignored; it was unsatisfiable.

- **F4. One palette token carries two roles.** `--gray-5` is used 30 times as a
  border colour and 10 times as a background; `--gray-6` 10 times as a
  background and 9 times as a border. A component cannot state which it means,
  so a second project's palette cannot re-map them independently.

## Decisions

The owner settled four questions before this order was written. They are
binding, and every batch below is shaped by them.

- **D1. Scope is Option C**: the token layer, the component consolidation, AND a
  mechanical guard. The guard is not optional polish; without it the
  consolidation drifts again within three screens.
- **D2. The scale is DERIVED from existing usage**, not imported from Open Props
  or any other external system. No new dependency, no vendored stylesheet.
- **D3. Reuse across projects is COPY-PER-PROJECT.** The kit is vendored into
  each repository, as the Material Symbols and the font8x8 table already are. No
  submodule, no shared package, no registry.
- **D4. The change is LOOK-NEUTRAL.** See the operational definition below. This
  is the constraint that decides the whole batch structure, and it is the reason
  batch 8 exists and is gated.

Two further decisions follow from those, and are recorded here because they are
not obvious:

- **D5. A look-neutral token layer is a CENSUS, not a scale.** If every token's
  value equals the literal it replaces, the type layer has 17 entries and the
  spacing layer has 31. That is a rename, and it is worth doing anyway: it makes
  every value countable, makes the guard possible, and makes the later collapse
  a one-line edit per token instead of a hunt. The *target* ladder is declared
  in the same file, new code may use only the ladder, and the census entries
  become a burn-down list. Collapsing them changes pixels, so it is batch 8.
- **D6. The guard is a Go test at the repository root**, not a node test. Its
  siblings are `icon_parity_test.go` and `dataset_parity_test.go`, which already
  read `frontend/` from Go, and it therefore runs inside the unqualified
  `go test ./...` that gates every push. A new `frontend/*.test.js` module would
  also work, but it would sit under `frontend_tests_test.go`'s
  module-needs-a-test rule for no gain.

## Ground rules

- **No npm, no bundler, no build step, no CDN, no remote font.** Every file
  added here is a plain `.css` or `.js` file under `frontend/`, embedded by the
  existing `//go:embed all:frontend`. `embed_test.go` must stay green.
- **`brand.css` stays the single source of truth for colour and font family.**
  This order gives it a sibling for the non-colour primitives; it does not give
  colour a second home. The `:root` block currently sitting in `style.css`
  (`--card-radius`, `--workspace-pad`, the chrome heights) moves out of it,
  which resolves the contradiction rather than adding to it.
- **The fixed-height layout contract is untouched.** `body` and `#app` stay
  `100vh`, the chrome heights stay fixed, scrolling stays inside a card body,
  and every `min-height: 0` in the chain stays where it is. A batch that changes
  a scroll owner has gone wrong.
- **No user-visible copy changes.** This order moves no string into or out of
  `copy.js`, and adds none. If a step appears to need one, that is a finding.
- **A change is not finished until its tests move with it.** Both suites gate:
  `go test ./...` and `node --test "frontend/**/*.test.js"`. Read
  `docs/TESTING.md` before writing a test.
- **Comments explain intent in the present tense.** No batch numbers, no "this
  used to be a literal", no tombstone for a deleted rule. Where a past mistake
  explains a token, state the rule and the failure it prevents. F3 is the model:
  the comment on the elevation tokens says why a scrim needs a token, not that
  nine literals used to exist.

### What look-neutral means here (D4, operational)

Not "looks the same to the eye". The definition is mechanical, because an
eyeball cannot certify 146 declarations:

> For batches 1 to 7, the rendering harness reports **identical computed
> values** for `font-size`, `line-height`, `padding`, `margin`, `gap`,
> `border-radius`, `box-shadow`, `color` and `background-color` on every probed
> element, before and after the batch.

`scripts/uitest/probes.js` already calls `getComputedStyle` and
`getBoundingClientRect` throughout, so this is an addition to an existing
capability rather than a new one. Batch 1 adds the snapshot probe FIRST, before
any token exists, and captures the baseline. Every later batch is checked
against that baseline byte for byte.

A batch that cannot meet it is not permitted to proceed by judgement. It is
deferred to batch 8, which is gated on the owner.

### The deviation rule

If a step is wrong, contradicted by the code, or cannot be done as written:
**stop, say so, and propose the alternative before writing it.** In particular,
if a token substitution changes a computed value anywhere, that is a finding
about the census, not a rounding error to absorb.

---

## The layer model

Four layers, split by what changes per project. This split is the deliverable;
the tidiness is a side effect.

| Layer | File | Contents | Travels to a new project? |
|---|---|---|---|
| 0 | `frontend/tokens.css` | type ladder, spacing ladder, radii, elevation, z-index, motion. No colour, no font family. | **unchanged** |
| 1 | `frontend/brand.css` | the palette, the font families, and the semantic aliases mapping them onto roles | **rewritten** (the only per-project file) |
| 2 | `frontend/components.css` + `frontend/ui.js` | button, card, chip, badge, field, tab bar, overlay, tooltip, popover | **unchanged** |
| 3 | `frontend/style.css` + `frontend/views/*` | screen layout, the fixed-height workspace, the wizard | **stays** |

`index.html` loads them in order: `tokens.css`, `brand.css`, `components.css`,
`style.css`. The order is load-bearing, and a comment in `index.html` says so,
as the existing `brand.css` comment already does.

**The rule that makes layer 2 portable:** a component may reference a
**semantic alias** (`--surface-raised`, `--border-subtle`, `--text-secondary`)
and never a palette token (`--gray-4`, `--ink-2`, `--key-fg`,
`--tint-orange-3`). There are currently 250+ direct palette references, so this
is the largest single edit in the order. The functional pairs already in
`brand.css` (`--notice-warn-*`, `--valid-*`, `--src-pattern-*`) are already
written this way and are the model; the neutrals are where the discipline was
skipped.

---

## Batch 1 — The census and the baseline

**Nothing renders differently. Nothing consumes the new file yet.**

1. **Add the computed-style snapshot probe** to `scripts/uitest/probes.js` (the
   ONE definition both harnesses read) and capture the baseline for every screen
   the harness already drives. This runs before any token exists, so the
   baseline is the current application. Record it as a fixture the later batches
   diff against.

2. **Write `frontend/tokens.css`.** Two blocks per property family, and the
   distinction between them is the whole design:

   - **the ladder**, which new code may use;
   - **the census**, one entry per existing value that is not on the ladder,
     each carrying its use count and the ladder step it will collapse onto.

   The type ladder is derived, not invented. The existing values form a near
   perfect arithmetic ladder at 0.05rem in the UI band, and the four
   highest-count values sit exactly on it:

   | Step | Value | Current uses |
   |---|---:|---:|
   | `--text-3xs` | 0.65rem | 4 |
   | `--text-2xs` | 0.70rem | 10 |
   | `--text-xs` | 0.75rem | 14 |
   | `--text-sm` | 0.80rem | **44** |
   | `--text-md` | 0.85rem | 16 |
   | `--text-lg` | 0.90rem | 4 |
   | `--text-xl` | 0.95rem | 7 |
   | `--text-2xl` | 1.00rem | 4 |
   | `--display-sm` | 1.20rem | 1 |
   | `--display-md` | 1.60rem | 1 |
   | `--display-lg` | 2.00rem | 1 |

   The census is then five values, 36 declarations, each within 0.02rem of a
   ladder step: `0.72rem` (11 uses, to `--text-2xs`), `0.76rem` (1, to
   `--text-xs`), `0.78rem` (18, to `--text-sm`), `0.82rem` (4, to `--text-sm`),
   `0.875rem` (2, to `--text-lg`), plus `1.15rem` (2, to `--display-sm`).

   Spacing follows the same method: a 0.1rem ladder below 1rem and a 0.25rem
   ladder above it, with the eleven highest-count values landing on steps, and
   the half-steps (`0.05`, `0.15`, `0.25`, `0.35`, `0.45`, `0.55`, `0.65`,
   `0.85`, `1.1`, `1.15`, `1.4`) in the census.

   Radius: four steps at `3px`, `4px`, `6px` and `8px` (the existing
   `--card-radius`), with `2px` and `5px` in the census. `50%` and `999px` are
   **not radii**; they are shapes (a circle, a pill) and get their own two
   tokens so the guard does not have to special-case them.

   Elevation: four tokens at the exact current literals, which answers F3.
   `--elev-1` `0 6px 18px rgba(0,0,0,0.12)`, `--elev-2`
   `0 10px 26px rgba(0,0,0,0.16)`, `--elev-3` `0 6px 18px rgba(0,0,0,0.22)`,
   `--elev-4` `0 18px 44px rgba(0,0,0,0.22)`, plus `--scrim`
   `rgba(0,0,0,0.28)`. The alpha values are the one place a colour literal
   legitimately lives outside `brand.css`, because they are opacities of black
   rather than brand values; say so in the comment.

   Z-index: five named layers derived from the existing `6`, `30`, `60`. Naming
   them is what stops the sixth surface picking `61`.

3. **Move the layout tokens** out of `style.css`'s `:root` into `tokens.css`:
   `--card-radius` (kept as an alias of the `8px` step so no call site changes),
   `--workspace-pad`, `--chrome-header`, `--chrome-stepbar`, `--chrome-footer`.

4. **Load `tokens.css` first in `index.html`**, before `brand.css`.

**Acceptance:** the snapshot diff is empty. `go test ./...` and the node suite
green. `embed_test.go` green.

**Gate G1 (owner):** the two ladders are a proposal until the owner reads them.
The open question is deliberately NOT answered here: whether the UI band should
eventually tighten from eight steps at 0.05rem to five at a wider ratio. Eight
steps is what the current application uses and is therefore the look-neutral
answer; five would read as more deliberate and is a batch 8 question. Do not
resolve it inside this batch.

---

## Batch 2 — Semantic aliases

**Nothing renders differently.** Every alias resolves to the hex its palette
token already resolves to.

1. **Add the alias block to `brand.css`**, below the palette and clearly
   separated by a comment stating the rule: the palette is referenced by this
   file alone, and a component may name only an alias.

2. **Split the dual-role tokens (F4).** This is a disambiguation, not a rename,
   and it is the reason the batch exists:

   | Alias | Resolves to | Role |
   |---|---|---|
   | `--surface` | `--bg` | the page and card ground |
   | `--surface-raised` | `--gray-6` | a card's inner banding |
   | `--surface-sunk` | `--surface-subtle` | table heads, pane captions |
   | `--border-subtle` | `--gray-5` | a hairline inside a card |
   | `--border` | `--gray-4` | a card edge |
   | `--border-strong` | `--gray-3` | an input edge, a bubble edge |
   | `--text-primary` | `--text` | sentences and values |
   | `--text-secondary` | `--ink-2` | secondary rows |
   | `--text-label` | `--ink-3` | uppercase section labels |
   | `--text-muted` | `--muted` | hints and empty states |
   | `--action-primary` | `--accent` | the one loud element per view |

   `--gray-5` and `--gray-6` each acquire two aliases pointing at one hex today.
   That is correct and is the point: the second project re-points
   `--border-subtle` and `--surface-raised` independently.

3. **Leave the functional pairs alone.** `--notice-*`, `--valid-*`,
   `--invalid-*`, `--src-*`, `--key-*`, `--origin-*`, `--selected-*`,
   `--caution-*`, `--experimental-*`, `--more-chip-*` are already role-named.
   They move into the alias block unchanged so the block is the complete list of
   what a component may say.

**Acceptance:** snapshot diff empty. No call site edited yet.

---

## Batch 3 — The guard, and the ratchet

1. **Write `css_token_test.go`** at the repository root, `package main`. It
   reads the CSS files as text and fails on:

   - a raw `font-size`, `border-radius`, `box-shadow`, `z-index`, `padding`,
     `margin` or `gap` value that is not `var(--...)`, `0`, `inherit`, `auto` or
     a percentage;
   - a literal colour (`#rgb`, `#rrggbb`, `rgb()`, `rgba()`, `hsl()`) anywhere
     outside `tokens.css`'s elevation block, which is the rule `style.css`'s own
     header already states;
   - a component-file rule referencing a palette token instead of an alias,
     which is the portability rule made mechanical;
   - a census token used anywhere outside the baseline list.

2. **Land it green, with the current violations as an explicit baseline.** The
   baseline is a sorted list of `file:selector:property` entries in the test
   file itself, with a count. Follow the audit layer's idiom
   (`docs/audit-baseline.md`): a dismissal is a decision somebody made, written
   down, not a silence.

3. **Make it a ratchet.** The test fails if the baseline count goes UP, and
   fails if an entry in the list no longer exists (a stale dismissal is a lie
   about the state of the code). Batches 4 to 6 drive it down; batch 8 empties
   it.

**Acceptance:** the guard is green on the current tree and red if you add a raw
`font-size: 0.81rem` to any component file. Prove both.

---

## Batch 4 — One overlay primitive, two variants

The highest-value single batch, and the one the reported "popup" inconsistency
is actually about. All seven surfaces in F2 are one of two things.

1. **The dialog variant**: centred, scrim, Escape, click-outside, focus trap.
   `.modal-layer` and `.image-panel-layer` collapse onto it. Their CSS is
   already byte-identical, so this is a deletion, not a redesign. The charter's
   existing constraint holds: `wireModal` keeps looking only for `.modal-layer`,
   and the treatment panel keeps wiring its own dismissal because it holds a
   draft rather than a question. **Only the surface CSS is shared; the two
   behaviours stay separate.**

2. **The bubble variant**: anchored by `placeBubble`, no scrim, `position:
   fixed` because every panel it opens inside is a clipping ancestor.
   `.help-bubble` and `.warnpop-bubble` already share the positioning model and
   duplicate the surface; the surface becomes one rule with two modifiers.
   `warningPopover` stays a distinct control for the reason the charter gives (a
   hover surface holding buttons has a different contract from one holding a
   sentence); it shares the surface, not the contract.

3. **`.mark-tooltip` and `.selection-card` keep `position: absolute`** and their
   own clamping. They are anchored inside a scrolling pane against the Compare
   card's bounds, which is a different problem from the clipping one. They adopt
   the elevation and radius tokens and nothing else. Do NOT convert them to
   `placeBubble`; that is a behaviour change dressed as a cleanup.

4. **Replace all five shadow literals** with `--elev-*`, and both scrims with
   `--scrim`. `.mark-tooltip`'s shadow is currently `0 6px 18px rgba(0,0,0,0.22)`
   where the two bubbles use `0.12`. Under D4 it keeps its own value
   (`--elev-3`). Unifying it is a batch 8 candidate, listed there.

5. **Delete what is replaced**, per the charter: a second builder for the same
   control is the next inconsistency.

**Acceptance:** snapshot diff empty. The harness's existing overlay probes
(help bubble open/closed, warning popover, treatment panel) stay green. The
guard's baseline drops by the overlay entries.

---

## Batch 5 — One header

1. **One `card` head at one size.** Delete the `.meta-review .card-head h2`
   override and the separate `.image-panel-head h2` size (F1) by making those
   two surfaces use the head they already structurally are.

   This is the one place where D4 and the fix collide: `1.2rem`, `0.95rem` and
   `1rem` cannot all become one value without changing pixels. **Resolve it by
   giving `card` an explicit `size` option** (`ui.js`), so the three call sites
   ask for the size they currently render at through one builder. The
   duplication moves from CSS to a named argument, which is countable and
   removable; the pixels do not move. Collapsing the three sizes into one is
   listed in batch 8.

2. **Every heading declaration goes through the ladder.** Nine heading sizes
   become nine ladder references, no new values.

3. **`.brand` keeps `font-weight: 700`.** It is a brand mark, not a heading, and
   the charter's regular-weight rule is about headings. Leave it, and put the
   reason in the comment so the next reader does not "fix" it. If the owner
   wants it changed, that is a brand decision and a separate order.

**Acceptance:** snapshot diff empty. `ui.test.js` covers the new `size` option.

---

## Batch 6 — Carve out `components.css`

1. **Move component rules out of `style.css` into `components.css`**, one
   component at a time, in this order: button, badge, chip, field and input,
   section label, tab bar, collapsible group, card, overlay, tooltip, popover.
   Each move converts that component's remaining palette references to aliases
   and drives the guard's baseline down.

2. **`style.css` keeps only screen layout**: the workspace grid, the chrome, the
   wizard, the rail, the Compare panes, the picture list. If a rule is hard to
   classify, the test is whether a second project would want it. A rail is this
   application's; a chip is not.

3. **Update `frontend/CLAUDE.md`'s file map** with the two new files and the
   layer rule. The charter is the contract; a new file that is not in it does
   not exist.

**Acceptance:** guard baseline empty except the census entries (which batch 8
owns). Snapshot diff empty. Both suites green.

---

## Batch 7 — Prove the portability before it is needed

The split is not proven by looking tidy. It is proven by swapping layer 1.

1. **Write `frontend/theme-probe.css`**, a second layer-1 file with a
   deliberately unrelated palette (a cool blue-green identity, not a variation
   of the orange). It defines the same alias names and nothing else.

2. **Add a harness case that loads it instead of `brand.css`** and asserts that
   every probed element still resolves a colour, that no element renders
   transparent-on-transparent, and that contrast holds. Anything that looks
   wrong is a component reaching past the alias layer, which is a bug in batch 2
   or 6 rather than in the probe theme.

3. **Delete `theme-probe.css` afterwards, keep the harness case** pointed at a
   generated theme. A vendored second palette is a file that rots; a generated
   one cannot.

4. **Write `docs/component-kit.md`**, the extraction manifest for D3. It lists
   exactly what a second project copies (`tokens.css`, `components.css`,
   `ui.js`, `icons.js`, `html.js`, `assets/icons/`, `css_token_test.go`), the
   one file it writes (`brand.css`, from a documented alias checklist), and the
   rule that the guard ships WITH the kit. A kit without its guard drifts from
   day one, which is how this one arrived here.

**Acceptance:** the swap case is green. The manifest is accurate enough that
someone could follow it without reading this order.

---

## Batch 8 (GATED, and NOT look-neutral)

Everything deferred by D4 collects here. **Do not start it without explicit
owner sign-off, cluster by cluster.** Each item is a pixel change, each is
small, and each needs the owner to look at a before-and-after rather than a
diff.

| # | Change | Declarations | Delta |
|---|---|---:|---|
| 1 | `0.78rem` to `--text-sm` (0.80) | 18 | +0.32px |
| 2 | `0.72rem` to `--text-2xs` (0.70) | 11 | -0.32px |
| 3 | `0.82rem` to `--text-sm` (0.80) | 4 | -0.32px |
| 4 | `0.875rem` to `--text-lg` (0.90) | 2 | +0.4px |
| 5 | `1.15rem` to `--display-sm` (1.20) | 2 | +0.8px |
| 6 | `0.76rem` to `--text-xs` (0.75) | 1 | -0.16px |
| 7 | the spacing half-steps | ~40 | up to 0.8px each |
| 8 | radius `2px` and `5px` onto the four steps | 3 | 1px |
| 9 | `.mark-tooltip` elevation `--elev-3` to `--elev-1` | 1 | shadow alpha |
| 10 | the three card-head sizes onto one | 3 | up to 4px |
| 11 | the UI band from eight steps to five (G1's open question) | ~120 | largest |

Items 1 to 8 are mechanical and, taken together, empty the guard's census. Items
9 to 11 are aesthetic and each deserves its own decision. Item 11 is the only
one that would make the type system read as designed rather than inherited, and
it is also the only one that touches most of the application; it is a separate
change order if the owner wants it.

---

## Acceptance criteria for the order

- **A1.** `go test ./...` and `node --test "frontend/**/*.test.js"` green.
- **A2.** The computed-style snapshot diff is EMPTY across batches 1 to 7. This
  is D4, and it is the criterion the order lives or dies on.
- **A3.** `css_token_test.go` green, its baseline containing only census
  entries, and demonstrably red on an added raw literal.
- **A4.** No file under `frontend/` references a remote origin
  (`embed_test.go`).
- **A5.** No component rule in `components.css` references a palette token.
- **A6.** The theme-swap harness case green.
- **A7.** `frontend/CLAUDE.md` and `docs/component-kit.md` describe the layers
  as built.
- **A8.** The fixed-height layout contract intact: the page body does not
  scroll, and every scroll owner is the one it was before.

## Open owner questions

- **OQ1 (gates batch 1's close).** The two ladders as proposed: eight UI type
  steps at 0.05rem, an eleven-step spacing ladder. Approve, or state the
  preferred shape. Tightening later is batch 8 item 11.
- **OQ2.** `.brand` at weight 700 against the regular-weight heading rule: keep
  as a brand mark (the plan's assumption) or bring it in line?
- **OQ3.** Batch 8 at all, and if so which items. The plan's assumption is that
  1 to 8 are worth doing as one signed-off pass and 9 to 11 are separate
  decisions.
- **OQ4.** Whether `docs/component-kit.md` should also carry the alias
  checklist as a fill-in-the-blanks `brand.css` template. It makes the second
  project faster and is one more file to keep true.
