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
	// TotalOccurrences is how many it has in all. Equal counts mean the value
	// is NEVER replaced under its own type, which is the case worth shouting
	// about; a partial overlap is a milder note, and the view says so
	// differently.
	Occurrences      int `json:"occurrences"`
	TotalOccurrences int `json:"totalOccurrences"`
	// Documents names up to maxIntersectionDocuments documents the overlap
	// occurs in, so the message can point somewhere.
	Documents []string `json:"documents,omitempty"`
}

// maxIntersectionDocuments bounds the document list on one intersection. The
// message names a few places to look; listing fifty would be a wall of text
// where a sentence was wanted.
const maxIntersectionDocuments = 3

// intersectionTally accumulates one losing claim across every document.
type intersectionTally struct {
	row  Intersection
	docs map[string]bool
}

// DetectIntersections reports every value whose text another route also claims.
//
// It runs the same detectText producers over each document and resolves with
// the same comparator, then reports what lost. Nothing is mutated: no
// placeholder is minted and the registry is untouched, so it is safe to call
// while the user is still editing values.
//
// A value is only reported when the two claims actually COVER each other in
// some text. Two values that never co-occur are not an intersection, they are
// simply two values, and warning about them would train the user to ignore the
// warning.
//
// @param docs the loaded documents, in any order
// @param scope the same detection configuration the run would use
// @return one row per losing (value, category) pair, most-covered first
func DetectIntersections(docs []Document, scope detectionScope) []Intersection {
	tallies := map[string]*intersectionTally{}

	for _, doc := range docs {
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
				addIntersection(tallies, doc.Name, loser, winner)
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
func addIntersection(tallies map[string]*intersectionTally, docName string, loser, winner Span) {
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
		}
		tallies[key] = t
	}
	t.row.Occurrences++
	t.row.TotalOccurrences++
	t.docs[docName] = true
}

// countUncovered adds the occurrences a value kept for itself. Without it a
// value covered three times out of ten would read as "every occurrence", which
// is the difference between a note and an alarm.
func countUncovered(tallies map[string]*intersectionTally, kept []Span) {
	for _, s := range kept {
		if t, ok := tallies[intersectionKey(s)]; ok {
			t.row.TotalOccurrences++
		}
	}
}

// sortedIntersections turns the tallies into rows, fully covered values first
// (they are the ones never replaced under their own type), then by count, then
// by value so the output is deterministic and the tests can pin it.
func sortedIntersections(tallies map[string]*intersectionTally) []Intersection {
	out := make([]Intersection, 0, len(tallies))
	for _, t := range tallies {
		names := make([]string, 0, len(t.docs))
		for name := range t.docs {
			names = append(names, name)
		}
		sort.Strings(names)
		if len(names) > maxIntersectionDocuments {
			names = names[:maxIntersectionDocuments]
		}
		t.row.Documents = names
		out = append(out, t.row)
	}
	sort.Slice(out, func(i, j int) bool {
		fullI := out[i].Occurrences == out[i].TotalOccurrences
		fullJ := out[j].Occurrences == out[j].TotalOccurrences
		if fullI != fullJ {
			return fullI
		}
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

// NewDetectionScope builds the detection configuration DetectIntersections
// needs. detectionScope is unexported because it is the pipeline's own shape
// and nothing outside the engine should be assembling passes; this constructor
// exists so the bound App can ask the same question the run asks, with the same
// inputs, rather than growing a second notion of what a detection is.
func NewDetectionScope(values []Value, patterns []CustomPattern,
	categories CategorySelection, minConfidence float32, country string,
	allow *Allowlist, suppressRegexPII bool) detectionScope {

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
