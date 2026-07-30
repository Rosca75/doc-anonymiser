// entitymodel.js, the PURE variant view-model (BUILD-02 Phase 7a).
//
// pendingExpansions is what makes the variants regression suite possible
// (entitymodel.test.js): the data flow that used to live in fragile
// previousElementSibling coupling is tested logic instead.
//
// variantRows() is GONE (BUILD-05 Phase 9). It turned the entity list plus the
// view's EXPANDED-ROW SET into row descriptors, for a table where a value's
// variants were hidden until its row was expanded. The value CARDS that replaced
// that table always show their variants, so there is no expanded set to model and
// nothing left for the function to compute. The four states it encoded (pending,
// error, empty, list) still exist and are still distinguished; the card renders
// them directly from the entity, which is three fewer indirections.

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
