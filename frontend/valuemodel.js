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

/**
 * repend(v) is what a value looks like after a gesture that changed what it
 * MATCHES: the row is handed back to whoever will settle its spellings.
 *
 * The sentinel it writes depends on the spelling POLICY, and that is the whole
 * point of the helper:
 *
 *   automatic  ->  null, the PENDING sentinel. pendingExpansions above picks the
 *                  row up, Go re-derives the list, and the card shows the hint
 *                  while it is in flight.
 *   curated    ->  [], the SETTLED sentinel, which is exactly what state.js
 *                  curate() writes. A curated row's chips ARE its list, so there
 *                  is nothing to derive and the row is finished the moment it is
 *                  amended.
 *
 * Writing null onto a curated row is the bug this exists to make impossible.
 * pendingExpansions deliberately skips curated rows, so no expansion is ever
 * requested and nothing clears the sentinel: the card renders "working out the
 * other spellings..." for the rest of the session, over chips that are already
 * correct. The fix is NOT to derive for curated rows, because that would let a
 * deleted spelling come straight back, which is the whole reason curation
 * exists.
 *
 * @param {object} v the value the caller is about to write into the store
 * @returns {object} a new value with the right sentinel and no stale error
 */
export function repend(v) {
  return {
    ...v,
    derivedSpellings: v?.spellingPolicy === "curated" ? [] : null,
    spellingsError: null,
  };
}
