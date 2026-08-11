/**
 * pendingExpansions(entities) lists the entities whose variants still
 * need a Go expansion round-trip (variants pending and no recorded
 * error). This is what refreshVariants iterates, so ONLY rows that were
 * just added, edited, or variant-amended re-expand; settled rows are
 * never touched again.
 */
export function pendingExpansions(entities) {
  return entities.filter((e) =>
    (e.variants === null || e.variants === undefined) && !e.variantError);
}
