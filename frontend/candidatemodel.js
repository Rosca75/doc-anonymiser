// candidatemodel.js, the PURE view-model for the "Suggestions to review"
// table.
//
// The table gained a text search on the value, a category selector, and a
// sort on the occurrence count. Putting that logic in the view would make
// it untestable and would mix it with the wiring; putting it in state.js
// would make a VIEW preference (which column is sorted) part of the
// application's business state, which it is not.
//
// So it lives here, as one pure function over (candidates, view filter),
// in the same spirit as entitymodel.js. The view holds the filter object,
// passes it through this function, and renders the result 1:1.

/**
 * DEFAULT_CANDIDATE_FILTER is the neutral filter: everything visible,
 * most frequent first. A view starts from a copy of this.
 *
 *   search free text matched against the candidate value
 *   category one engine category key, or "" for all of them
 *   sort      "count-desc" | "count-asc" | "value-asc" | "value-desc"
 */
export const DEFAULT_CANDIDATE_FILTER = {
  search: "",
  category: "",
  sort: "count-desc",
};

/**
 * visibleCandidates(candidates, filter) applies the search, the category
 * selector and the sort, and returns a NEW array. The input is never
 * mutated: the store's candidate list keeps its own order, which is the
 * order Go returned and the order bulk actions fall back on.
 *
 * Matching rules, chosen so the control behaves the way a user expects
 * rather than the way a regex would:
 *   - search is case-insensitive and matches ANYWHERE in the value, so
 *     typing "duv" finds "Marie Duval";
 *   - leading and trailing spaces in the search are ignored, because they
 *     are almost always accidental;
 *   - an unknown sort key falls back to the default rather than throwing,
 *     so a stale saved preference cannot break the screen.
 *
 * @param {Array} candidates state.candidates
 * @param {object} [filter] see DEFAULT_CANDIDATE_FILTER
 * @returns {Array} the rows to render, in display order
 */
export function visibleCandidates(candidates, filter = DEFAULT_CANDIDATE_FILTER) {
  const search = (filter?.search ?? "").trim().toLowerCase();
  const category = filter?.category ?? "";

  const rows = (candidates ?? []).filter((c) => {
    if (category && c.category !== category) return false;
    if (search && !(c.text ?? "").toLowerCase().includes(search)) return false;
    return true;
  });

  // A STABLE sort with an explicit tie-break, so two candidates with the
  // same count never swap places between renders (which would make rows
  // jump under the cursor while the user works down the list).
  const byValue = (a, b) => (a.text ?? "").localeCompare(b.text ?? "");
  const sorters = {
    "count-desc": (a, b) => (b.count ?? 0) - (a.count ?? 0) || byValue(a, b),
    "count-asc": (a, b) => (a.count ?? 0) - (b.count ?? 0) || byValue(a, b),
    "value-asc": byValue,
    "value-desc": (a, b) => byValue(b, a),
  };
  const sorter = sorters[filter?.sort] ?? sorters["count-desc"];
  return rows.sort(sorter);
}


/**
 * toggleCountSort(sort) flips the occurrence sort between descending and
 * ascending, and adopts descending from any other starting point (a
 * value sort, or an unknown key). One function so the header button and
 * a keyboard shortcut can never disagree.
 * @param {string} sort the current sort key
 * @returns {string} the next sort key
 */
export function toggleCountSort(sort) {
  return sort === "count-desc" ? "count-asc" : "count-desc";
}

/**
 * toggleValueSort(sort) is the same flip for the value column.
 * @param {string} sort the current sort key
 * @returns {string} the next sort key
 */
export function toggleValueSort(sort) {
  return sort === "value-asc" ? "value-desc" : "value-asc";
}
