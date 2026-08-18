/**
 * pendingExpansions(values) lists the values whose derivedSpellings still
 * need a Go expansion round-trip (derivedSpellings pending and no recorded
 * error). This is what refreshVariants iterates, so ONLY rows that were
 * just added, edited, or spelling-amended re-expand; settled rows are
 * never touched again.
 *
 * A CURATED row is settled by definition: its spellings are the ones the user
 * chose, so there is nothing for Go to derive. Asking anyway would round-trip for
 * the same list and show a pending placeholder for a list that is already final.
 */
export function pendingExpansions(values) {
  return values.filter((v) =>
    v.spellingPolicy !== "curated" &&
    (v.derivedSpellings === null || v.derivedSpellings === undefined) && !v.spellingsError);
}
