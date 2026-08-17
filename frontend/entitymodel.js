/**
 * pendingExpansions(entities) lists the entities whose variants still
 * need a Go expansion round-trip (variants pending and no recorded
 * error). This is what refreshVariants iterates, so ONLY rows that were
 * just added, edited, or variant-amended re-expand; settled rows are
 * never touched again.
 *
 * A CURATED row (autoExpand false) is settled by definition: its spellings are
 * the ones the user chose, so there is nothing for Go to derive. Asking anyway
 * would round-trip for the same list and show a pending placeholder for a list
 * that is already final.
 */
export function pendingExpansions(entities) {
  return entities.filter((e) =>
    e.autoExpand !== false &&
    (e.variants === null || e.variants === undefined) && !e.variantError);
}
