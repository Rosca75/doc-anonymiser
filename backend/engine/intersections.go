// engine/intersections.go — which values are claimed by more than one route,
// answered BEFORE the pipeline runs.
//
// A Value the user declared can be covered by a built-in pattern match, a custom
// pattern can cover a declared Value, a heuristic finding can cover one the local
// AI found. The precedence rule always decides (matchclass.go), but until the run
// happens the user has no way of knowing that their Value will never be replaced
// under its own type. The warning belongs on the card that owns the Value, on the
// Identify step, which means the answer has to exist without running the
// pipeline.
//
// It is computed with the SAME producers and the SAME comparator the pipeline
// uses, and never with a parallel implementation, because a parallel check can
// disagree with the pipeline and then the warning describes something that did
// not happen.
package engine

import (
	"sort"
	"strings"
)

// Intersection is one Value whose text is claimed by more than one method.
//
// It is a WARNING, never blocking: the precedence rule always has an answer, so
// refusing the run would punish the user for a configuration the engine can
// resolve.
type Intersection struct {
	// Value, Category and MatchClass describe the claim that LOST: the text as
	// the user sees it, the category it is filed under, and the class that
	// decided the contest. MatchClass is what the frontend turns into the NAME of
	// the winning method or route: the copy says "a built-in pattern" or "the
	// local AI", never an internal rank.
	Value      string `json:"value"`
	Category   string `json:"category"`
	MatchClass string `json:"matchClass"`
	// The claim that won, in the same three terms.
	WinnerValue      string `json:"winnerValue"`
	WinnerCategory   string `json:"winnerCategory"`
	WinnerMatchClass string `json:"winnerMatchClass"`
	// Occurrences is how many of this value's hits the winner covers;
	// TotalOccurrences is how many it has in all. Only FULL coverage is
	// reported (see sortedIntersections), so the two are always equal, and they
	// are both carried because the full-coverage rule is expressed with them:
	// a row that arrives with them unequal is a bug, not a milder warning.
	Occurrences      int `json:"occurrences"`
	TotalOccurrences int `json:"totalOccurrences"`
	// Documents names up to maxIntersectionDocuments documents the overlap
	// occurs in, so the message can point somewhere.
	Documents []string `json:"documents,omitempty"`
	// MatchedTexts holds the literal text of the covered occurrences, exactly as
	// it appears in the documents (Span.Original), deduped, in document order and
	// capped at maxIntersectionMatchedTexts.
	//
	// It exists because Value is the registry KEY, which for a value-pass match is
	// the Value's canonical MainText and not what the winner actually covered. The
	// person "Pierre Dupont" is covered inside "pierre.dupont@coca.us" as the two
	// derived spellings "pierre" and "dupont", and the full name occurs nowhere in
	// that address: a message quoting the canonical form claims a string was found
	// verbatim where it was not. It is EMPTY in the common case, where the covered
	// literal is the value itself, so the message adds nothing to read.
	MatchedTexts []string `json:"matchedTexts,omitempty"`
}

// maxIntersectionDocuments bounds the document list on one intersection. The
// message names a few places to look; listing fifty would be a wall of text
// where a sentence was wanted.
const maxIntersectionDocuments = 3

// maxIntersectionMatchedTexts bounds the literal-text list for the same reason.
// A value covered under twenty different spellings is a sentence that names a
// few of them, not a list the user has to read to the end.
const maxIntersectionMatchedTexts = 3

// matchedLiteral is one covered occurrence's literal text and where it was
// found. The position is carried because resolveOverlaps hands the losers back
// in PRECEDENCE order, not document order, and the message reads as nonsense
// when it names the second half of a name before the first.
type matchedLiteral struct {
	docIndex int
	start    int
	text     string
}

// intersectionTally accumulates one losing claim across every document.
type intersectionTally struct {
	row  Intersection
	docs map[string]bool
	// The distinct literals the winner covered, and the set that dedupes them.
	matched []matchedLiteral
	seen    map[string]bool
}

// DetectIntersections reports every value that another route claims in EVERY
// place the value occurs.
//
// It runs the same detectText producers over each document and resolves with
// the same comparator, then reports what lost. Nothing is mutated: no
// placeholder is minted and the registry is untouched, so it is safe to call
// while the user is still editing values.
//
// A value is only reported when the two claims actually COVER each other in
// some text, and only when the coverage is TOTAL (sortedIntersections holds
// that rule and says why). Two values that never co-occur are not an
// intersection, they are simply two values, and warning about them would train
// the user to ignore the warning.
//
// @param docs the loaded documents; their order IS the document order the
// reported literals are listed in
// @param scope the same detection configuration the run would use
// @return one row per fully covered (value, category) pair, most-covered first
func DetectIntersections(docs []Document, scope detectionScope) []Intersection {
	tallies := map[string]*intersectionTally{}

	for docIndex, doc := range docs {
		for _, region := range detectDocument(doc, scope).regions {
			// The winners are what a run would actually replace; the losers are
			// the claims a stronger one covered. Both come out of the ONE place
			// the decision is made.
			kept, dropped := resolveOverlaps(region.spans, true)
			if len(dropped) == 0 {
				continue
			}
			// Which kept span covers a given offset range. Built once per
			// region: a region with hundreds of losers would otherwise re-scan
			// the kept list for each one.
			covererAt := func(s Span) (Span, bool) {
				for _, k := range kept {
					if s.Start < k.End && k.Start < s.End {
						return k, true
					}
				}
				return Span{}, false
			}
			for _, loser := range dropped {
				winner, ok := covererAt(loser)
				if !ok {
					continue // covered by another loser, not by a survivor
				}
				if sameClaim(loser, winner) {
					continue // the same value under the same type, just twice
				}
				addIntersection(tallies, doc.Name, docIndex, loser, winner)
			}
			// A value's TOTAL occurrences include the ones nothing covered, or
			// "3 of 3" would be reported for a value that also appears freely.
			countUncovered(tallies, kept)
		}
	}

	return sortedIntersections(tallies)
}

// sameClaim reports whether two spans are the same value under the same type.
// Two spans can overlap without disagreeing about anything (a name and one of
// its own variants at the same offsets), and that is not an intersection.
func sameClaim(a, b Span) bool {
	return a.Category == b.Category &&
		strings.EqualFold(a.MainTextOrOriginal(), b.MainTextOrOriginal())
}

// intersectionKey identifies one losing claim: a value under a category. The
// same loss in fifty documents is one row with a count, not fifty rows.
func intersectionKey(s Span) string {
	return s.Category + "|" + strings.ToLower(s.MainTextOrOriginal())
}

// addIntersection records one covered occurrence.
//
// It keeps the loser's Original as well as its registry key, because the two
// differ for every value-pass match: the key is the Value's canonical MainText
// and the Original is what the winner actually sat on top of. Discarding the
// Original is what let the warning quote a string the document never held.
func addIntersection(tallies map[string]*intersectionTally, docName string, docIndex int, loser, winner Span) {
	key := intersectionKey(loser)
	t, ok := tallies[key]
	if !ok {
		t = &intersectionTally{
			row: Intersection{
				Value:            loser.MainTextOrOriginal(),
				Category:         loser.Category,
				MatchClass:       loser.MatchClass,
				WinnerValue:      winner.MainTextOrOriginal(),
				WinnerCategory:   winner.Category,
				WinnerMatchClass: winner.MatchClass,
			},
			docs: map[string]bool{},
			seen: map[string]bool{},
		}
		tallies[key] = t
	}
	t.row.Occurrences++
	t.row.TotalOccurrences++
	t.docs[docName] = true
	if !t.seen[loser.Original] {
		t.seen[loser.Original] = true
		t.matched = append(t.matched,
			matchedLiteral{docIndex: docIndex, start: loser.Start, text: loser.Original})
	}
}

// countUncovered adds the occurrences a value kept for itself. It is what makes
// the full-coverage filter mean anything: without it a value covered three times
// out of ten would tally as fully covered and be reported as never replaced
// under its own type, when in fact it is replaced everywhere else.
func countUncovered(tallies map[string]*intersectionTally, kept []Span) {
	for _, s := range kept {
		if t, ok := tallies[intersectionKey(s)]; ok {
			t.row.TotalOccurrences++
		}
	}
}

// sortedIntersections turns the tallies into rows, most-covered first, then by
// value so the output is deterministic and the tests can pin it.
//
// It reports ONLY fully covered values, and that filter is the whole warning
// policy. A value covered in some places and free in others still gets its own
// placeholder where nothing covers it, and the covered occurrences are redacted
// by the winner anyway: there is no leak to report and no action to take, so the
// sentence is noise that trains the user to ignore the warning. Full coverage is
// the actionable case, because the value then gets NO placeholder of its own
// anywhere, which usually means it was declared under the wrong type.
func sortedIntersections(tallies map[string]*intersectionTally) []Intersection {
	out := make([]Intersection, 0, len(tallies))
	for _, t := range tallies {
		if t.row.Occurrences != t.row.TotalOccurrences {
			continue
		}
		names := make([]string, 0, len(t.docs))
		for name := range t.docs {
			names = append(names, name)
		}
		sort.Strings(names)
		if len(names) > maxIntersectionDocuments {
			names = names[:maxIntersectionDocuments]
		}
		t.row.Documents = names
		t.row.MatchedTexts = matchedTextsFor(t)
		out = append(out, t.row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Occurrences != out[j].Occurrences {
			return out[i].Occurrences > out[j].Occurrences
		}
		if out[i].Value != out[j].Value {
			return out[i].Value < out[j].Value
		}
		return out[i].Category < out[j].Category
	})
	return out
}

// matchedTextsFor is the literal covered text of one row, in document order and
// bounded.
//
// A literal identical to the value itself is DROPPED, which empties the list in
// the common case where the winner sat on the value's own text: the message then
// names the value once rather than repeating it as its own spelling.
func matchedTextsFor(t *intersectionTally) []string {
	sort.Slice(t.matched, func(i, j int) bool {
		if t.matched[i].docIndex != t.matched[j].docIndex {
			return t.matched[i].docIndex < t.matched[j].docIndex
		}
		return t.matched[i].start < t.matched[j].start
	})
	texts := make([]string, 0, len(t.matched))
	for _, m := range t.matched {
		if m.text == t.row.Value {
			continue
		}
		texts = append(texts, m.text)
		if len(texts) == maxIntersectionMatchedTexts {
			break
		}
	}
	if len(texts) == 0 {
		return nil
	}
	return texts
}

// NewDetectionScope builds the detection configuration DetectIntersections
// needs. detectionScope is unexported because it is the pipeline's own shape
// and nothing outside the engine should be assembling passes; this constructor
// exists so the bound App can ask the same question the run asks, with the same
// inputs, rather than growing a second notion of what a detection is.
func NewDetectionScope(values []Value, patterns []CustomPattern,
	categories CategorySelection, minConfidence float32, country string,
	allow *Allowlist, suppressRegexPII bool,
) detectionScope {
	if country == "" {
		country = CountryLU
	}
	if categories == nil {
		categories = PresetSelection(LevelMedium)
	}
	return detectionScope{
		values:           filterValues(values, categories),
		patterns:         patterns,
		categories:       categories,
		minConfidence:    minConfidence,
		country:          country,
		allow:            allow,
		suppressRegexPII: suppressRegexPII,
	}
}
