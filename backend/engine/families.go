// engine/families.go — folding the spellings of one real-world thing into ONE
// Value family.
//
// Discovery routinely finds "Coca-Cola" and "Coca-Cola company" and offers them
// as two Suggestions. They are one Value with two spellings, and the difference
// is not cosmetic: DetectValues matches a Value's spellings longest first, so
// with "Coca-Cola" as the main text and "Coca-Cola company" as one of its
// spellings the whole phrase collapses into one placeholder. Left as two Values,
// the shorter one fires inside the longer one, the text reads "[BRAND_1]
// company", the legal form leaks, and two numbers are spent on one company.
//
// So the SHORTER form is the main text and the longer forms are its spellings.
// Every rule below exists to stop one specific wrong fold. Folding happens ONCE
// over the unified Suggestion list, across every discovery method, so a family
// cannot be split by which route happened to find which spelling.
package engine

import (
	"sort"
	"strings"
)

// maxFoldedContexts bounds the snippets a folded family carries. The review row
// shows a few examples; carrying every member's three would grow the payload
// without telling the user anything the first few do not.
const maxFoldedContexts = 3

// FoldValueFamilies groups Suggestions that are spellings of the same thing and
// returns one Suggestion per family, the shortest form as main text with the
// longer ones as its spellings. Suggestions in no family come back untouched, in
// their original order.
//
// @param suggestions the merged output of every discovery method, in any order
// @param allow the never-anonymise list; an allowlisted member is not folded
// @return one row per Value family, Spellings carrying the folded forms
func FoldValueFamilies(suggestions []Suggestion, allow *Allowlist) []Suggestion {
	if len(suggestions) < 2 {
		return suggestions
	}

	// Union-find over the "is a word-boundary substring of" relation, which is
	// transitive by construction: "Coca", "Coca-Cola" and "Coca-Cola Ltd." land
	// in one family even though the first and last are only linked through the
	// middle one.
	parent := make([]int, len(suggestions))
	for i := range parent {
		parent[i] = i
	}
	// Iterative with path halving rather than recursive: a family of any size
	// resolves in a loop, so there is no stack depth to think about.
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	union := func(a, b int) { parent[find(a)] = find(b) }

	for i := range suggestions {
		for j := range suggestions {
			if i == j || !canJoinFamily(suggestions[i], suggestions[j], allow) {
				continue
			}
			union(i, j)
		}
	}

	// Members per family root, in input order so the output is deterministic.
	members := map[int][]int{}
	var roots []int
	for i := range suggestions {
		r := find(i)
		if _, seen := members[r]; !seen {
			roots = append(roots, r)
		}
		members[r] = append(members[r], i)
	}

	out := make([]Suggestion, 0, len(suggestions))
	for _, root := range roots {
		group := members[root]
		if len(group) == 1 {
			out = append(out, suggestions[group[0]])
			continue
		}
		out = append(out, foldFamily(suggestions, group))
	}
	return out
}

// canJoinFamily reports whether longer should fold into shorter.
//
// Each condition is a fold that would be wrong without it:
//
//   - Same category. A person "Delta" and an organisation "Delta Industries"
//     are a cross-category INTERSECTION, not a family; folding them would file
//     a human being under an organisation.
//   - Strictly shorter. Equal-length strings are not spellings of each other.
//   - At WORD BOUNDARIES. "Alten" occurs inside "Altenberg", and they are two
//     different names. This is the same boundary rule the Value pass matches
//     with, so the fold agrees with what will actually be replaced.
//   - Long enough. Promoting a two-character stem to a main value would shred
//     ordinary text everywhere it appeared.
//   - Neither is allowlisted. An allowlisted term is replaced by nothing, so
//     folding one into a family would make the family's placeholder depend on a
//     value that never applies.
func canJoinFamily(shorter, longer Suggestion, allow *Allowlist) bool {
	if shorter.Category != longer.Category {
		return false
	}
	s, l := strings.TrimSpace(shorter.MainText), strings.TrimSpace(longer.MainText)
	if len([]rune(s)) >= len([]rune(l)) {
		return false
	}
	if len([]rune(s)) < minSpellingLen {
		return false
	}
	if allow.Contains(s) || allow.Contains(l) {
		return false
	}
	return containsAtWordBoundary(l, s)
}

// containsAtWordBoundary reports whether needle occurs in haystack,
// case-insensitively, delimited by non-letter/digit runes on both sides.
func containsAtWordBoundary(haystack, needle string) bool {
	lowerHay := strings.ToLower(haystack)
	lowerNeedle := strings.ToLower(needle)
	from := 0
	for {
		i := strings.Index(lowerHay[from:], lowerNeedle)
		if i < 0 {
			return false
		}
		start := from + i
		end := start + len(lowerNeedle)
		// The boundary check runs on the ORIGINAL string: lowercasing can
		// change byte lengths for some runes, and an offset from the lowered
		// copy would then land mid-rune.
		if len(lowerHay) == len(haystack) && isWordBoundary(haystack, start, end) {
			return true
		}
		from = start + 1
		if from >= len(lowerHay) {
			return false
		}
	}
}

// foldFamily collapses one group into a single suggestion.
//
// The shortest member becomes the main value. Ties break on occurrence count
// (the spelling the documents actually use is the better main value), then
// alphabetically, so the result is the same on every run.
func foldFamily(suggestions []Suggestion, group []int) Suggestion {
	ordered := append([]int(nil), group...)
	sort.SliceStable(ordered, func(a, b int) bool {
		ca, cb := suggestions[ordered[a]], suggestions[ordered[b]]
		la, lb := len([]rune(ca.MainText)), len([]rune(cb.MainText))
		if la != lb {
			return la < lb
		}
		if ca.Count != cb.Count {
			return ca.Count > cb.Count
		}
		return ca.MainText < cb.MainText
	})

	main := suggestions[ordered[0]]
	// The family's total weight: the review row should say how often the value
	// occurs in any of its spellings, not only in its shortest one.
	total := 0
	var contexts []string
	var spellings []string
	for _, i := range ordered {
		total += suggestions[i].Count
		if i != ordered[0] {
			spellings = append(spellings, suggestions[i].MainText)
		}
		for _, c := range suggestions[i].Contexts {
			if len(contexts) < maxFoldedContexts {
				contexts = append(contexts, c)
			}
		}
		// Variants a member already carried travel with it: a family can be
		// folded twice, once per route and once over the merged output.
		spellings = append(spellings, suggestions[i].Spellings...)
	}

	main.Count = total
	main.Contexts = contexts
	main.Spellings = MergeSpellings(spellings, nil, main.MainText)
	return main
}

// MergeSpellings unions two spelling lists, dropping the main text itself and
// any case-insensitive duplicate, then orders longest first, which is the order
// spelling matching wants.
//
// The main text is excluded on purpose: Value.Spellings is defined as the forms
// OTHER than the main one, so carrying it in both places would mean two records
// of one string that a later edit could contradict.
//
// @param into the spellings kept so far, in any order
// @param from the spellings to add
// @param main the Value or Suggestion's main text
// @return the merged list, longest first
func MergeSpellings(into, from []string, main string) []string {
	seen := map[string]bool{strings.ToLower(strings.TrimSpace(main)): true}
	var out []string
	for _, list := range [][]string{into, from} {
		for _, v := range list {
			v = strings.TrimSpace(v)
			key := strings.ToLower(v)
			if v == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, v)
		}
	}
	sortByLengthDesc(out)
	return out
}

// MergeContexts appends the snippets not already present, up to
// maxSuggestionContexts. The row shows a few examples, so the cap bounds the
// payload rather than losing information the user was going to read.
func MergeContexts(into, from []string) []string {
	seen := map[string]bool{}
	for _, c := range into {
		seen[c] = true
	}
	for _, c := range from {
		if len(into) >= maxSuggestionContexts {
			break
		}
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		into = append(into, c)
	}
	return into
}
