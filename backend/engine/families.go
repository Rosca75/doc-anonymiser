// engine/families.go — folding the spellings of one real-world thing into ONE
// value.
//
// Detection routinely finds "Coca-Cola" and "Coca-Cola company" and offers them
// as two suggestions. They are one value with two spellings, and the difference
// is not cosmetic: DetectEntities matches a value's variants longest first, so
// with "Coca-Cola" as the main value and "Coca-Cola company" as one of its
// variants the whole phrase collapses into one placeholder. Left as two values,
// the shorter one fires inside the longer one, the text reads "[BRAND_1]
// company", the legal form leaks, and two numbers are spent on one company.
//
// So the SHORTER form is the main value and the longer forms are its variants.
// Every rule below exists to stop one specific wrong fold.
package engine

import (
	"sort"
	"strings"
)

// maxFoldedContexts bounds the snippets a folded family carries. The review row
// shows a few examples; carrying every member's three would grow the payload
// without telling the user anything the first few do not.
const maxFoldedContexts = 3

// FoldValueFamilies groups candidates that are spellings of the same thing and
// returns one candidate per family, the shortest form first with the longer
// ones as its variants. Candidates in no family come back untouched, in their
// original order.
//
// @param cands the merged output of every detection route, in any order
// @param allow the never-anonymise list; an allowlisted member is not folded
// @return one row per value, with Variants carrying the folded spellings
func FoldValueFamilies(cands []Candidate, allow *Allowlist) []Candidate {
	if len(cands) < 2 {
		return cands
	}

	// Union-find over the "is a word-boundary substring of" relation, which is
	// transitive by construction: "Coca", "Coca-Cola" and "Coca-Cola Ltd." land
	// in one family even though the first and last are only linked through the
	// middle one.
	parent := make([]int, len(cands))
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

	for i := range cands {
		for j := range cands {
			if i == j || !canJoinFamily(cands[i], cands[j], allow) {
				continue
			}
			union(i, j)
		}
	}

	// Members per family root, in input order so the output is deterministic.
	members := map[int][]int{}
	var roots []int
	for i := range cands {
		r := find(i)
		if _, seen := members[r]; !seen {
			roots = append(roots, r)
		}
		members[r] = append(members[r], i)
	}

	out := make([]Candidate, 0, len(cands))
	for _, root := range roots {
		group := members[root]
		if len(group) == 1 {
			out = append(out, cands[group[0]])
			continue
		}
		out = append(out, foldFamily(cands, group))
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
//     different names. This is the same boundary rule the entity pass matches
//     with, so the fold agrees with what will actually be replaced.
//   - Long enough. Promoting a two-character stem to a main value would shred
//     ordinary text everywhere it appeared.
//   - Neither is allowlisted. An allowlisted term is replaced by nothing, so
//     folding one into a family would make the family's placeholder depend on a
//     value that never applies.
func canJoinFamily(shorter, longer Candidate, allow *Allowlist) bool {
	if shorter.Category != longer.Category {
		return false
	}
	s, l := strings.TrimSpace(shorter.Text), strings.TrimSpace(longer.Text)
	if len([]rune(s)) >= len([]rune(l)) {
		return false
	}
	if len([]rune(s)) < minVariantLen {
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

// foldFamily collapses one group into a single candidate.
//
// The shortest member becomes the main value. Ties break on occurrence count
// (the spelling the documents actually use is the better main value), then
// alphabetically, so the result is the same on every run.
func foldFamily(cands []Candidate, group []int) Candidate {
	ordered := append([]int(nil), group...)
	sort.SliceStable(ordered, func(a, b int) bool {
		ca, cb := cands[ordered[a]], cands[ordered[b]]
		la, lb := len([]rune(ca.Text)), len([]rune(cb.Text))
		if la != lb {
			return la < lb
		}
		if ca.Count != cb.Count {
			return ca.Count > cb.Count
		}
		return ca.Text < cb.Text
	})

	main := cands[ordered[0]]
	// The family's total weight: the review row should say how often the value
	// occurs in any of its spellings, not only in its shortest one.
	total := 0
	var contexts []string
	var variants []string
	for _, i := range ordered {
		total += cands[i].Count
		if i != ordered[0] {
			variants = append(variants, cands[i].Text)
		}
		for _, c := range cands[i].Contexts {
			if len(contexts) < maxFoldedContexts {
				contexts = append(contexts, c)
			}
		}
		// Variants a member already carried travel with it: a family can be
		// folded twice, once per route and once over the merged output.
		variants = append(variants, cands[i].Variants...)
	}

	main.Count = total
	main.Contexts = contexts
	main.Variants = dedupeSpellings(variants, main.Text)
	return main
}

// dedupeSpellings removes duplicates and the main value itself, then orders
// longest first, which is the order variant matching wants.
func dedupeSpellings(variants []string, main string) []string {
	seen := map[string]bool{strings.ToLower(strings.TrimSpace(main)): true}
	var out []string
	for _, v := range variants {
		v = strings.TrimSpace(v)
		key := strings.ToLower(v)
		if v == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, v)
	}
	sortByLengthDesc(out)
	return out
}
