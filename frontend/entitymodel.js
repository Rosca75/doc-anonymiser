// entitymodel.js, the PURE variant view-model (BUILD-02 Phase 7a).
//
// variantRows turns the entity list + the view's expanded-row set into
// plain data the entities view renders 1:1. Keeping this a pure function
// (no DOM, no api calls) is what makes the variants regression suite
// possible (entitymodel.test.js): the data flow that used to live in
// fragile previousElementSibling coupling is now tested logic.

import { entityKey } from "./state.js";

/**
 * variantRows(entities, expandedKeys) returns one row descriptor per
 * EXPANDED entity:
 *   { key, category, canonical, state, variants, error }
 * where state is one of:
 *   "pending" (variants === null/undefined: expansion in flight),
 *   "error"   (variantError set: show the Go error text),
 *   "empty"   (expanded, zero variants: show the explicit empty state),
 *   "list"    (variants to show).
 *
 * @param {Array} entities state.entities
 * @param {Set<string>} expandedKeys the view's expanded entityKey set
 * @returns {Array} plain row descriptors, in entity order
 */
export function variantRows(entities, expandedKeys) {
  const rows = [];
  for (const e of entities) {
    const key = entityKey(e.category, e.canonical);
    if (!expandedKeys.has(key)) continue;
    let state = "list";
    if (e.variantError) state = "error";
    else if (e.variants === null || e.variants === undefined) state = "pending";
    else if (e.variants.length === 0) state = "empty";
    rows.push({
      key,
      category: e.category,
      canonical: e.canonical,
      state,
      variants: e.variants ?? [],
      error: e.variantError ?? null,
    });
  }
  return rows;
}

/**
 * pendingExpansions(entities) lists the entities whose variants still
 * need a Go expansion round-trip (variants pending and no recorded
 * error). This is what refreshVariants iterates, so ONLY rows that were
 * just added, edited, or variant-amended re-expand; settled rows are
 * never touched again (the Phase 7a contract).
 */
export function pendingExpansions(entities) {
  return entities.filter((e) =>
    (e.variants === null || e.variants === undefined) && !e.variantError);
}
