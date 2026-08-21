// patternpreview.go — what built-in pattern matching WOULD replace, answered
// before a run so the Identify step can SHOW it.
//
// Built-in pattern matching produces DIRECT MATCHES, not Suggestions: a pattern
// is a rule the user chose, so its findings need no review gate and pass 1
// applies them at anonymisation time (pipeline.go). That is deliberate, and it
// left the user with no way to check the one thing they DO decide: which signal
// categories are switched on. Ticking "street addresses" and running detection
// changed nothing anybody could see until after the whole document had been
// anonymised.
//
// So this is a READ-ONLY preview, and nothing else. It runs the SAME detector
// pass 1 runs, through the same allowlist and the same checksum switch, and it
// produces no Suggestion, no Value and no state a run consults: the run detects
// again for itself. It cannot become a review gate, because there is nothing
// here to accept.
//
// Two caveats belong to the caller's copy rather than to this code, because they
// are properties of a preview and not defects in it: an accepted Value or a
// custom pattern claiming the same characters can still win the span at run time
// (ResolveOverlaps compares match classes over the whole batch), and a category
// the user switches off after this ran is not reflected until it runs again.
package engine

import "sort"

// PatternMatch is one distinct text a built-in pattern claimed, aggregated over
// the whole imported batch.
//
// Aggregated by (category, exact text) rather than listed per occurrence,
// because the question the Identify step answers is "what did my patterns
// find", and a table with one row per occurrence of the same email address
// answers a different one. Count and Documents carry the occurrences back.
type PatternMatch struct {
	// Category is the built-in pattern category that claimed the text, one of
	// AllPIICategories. It is the SIGNAL the match belongs to.
	Category string `json:"category"`
	// Text is the matched text exactly as the document spells it. Grouping is
	// case-SENSITIVE on purpose: two spellings of one address are two things a
	// reviewer may want to see, and pass 1 replaces each occurrence as written.
	Text string `json:"text"`
	// Count is how many occurrences the whole batch holds.
	Count int `json:"count"`
	// Documents names the files the text occurs in, in import order, without
	// repeats: "which file is this in" is the first question asked about a
	// surprising match.
	Documents []string `json:"documents"`
	// Confidence is the LOWEST score any occurrence scored. Lowest rather than
	// highest, because the number is only interesting when it is below 1.0: a
	// failed corroborating checksum (ConfidenceChecksumFailed) is exactly the
	// thing a reviewer should see, and averaging or maximising hides it.
	Confidence float32 `json:"confidence"`
}

// ActivePatternCategories lists the built-in pattern categories that would
// actually fire: switched on in sel AND applicable to country.
//
// It is returned beside the matches because "no matches" and "that category
// never ran" are different facts and the user can only act on the second. The
// order is AllPIICategories', so the caller's grouping is stable.
func ActivePatternCategories(sel CategorySelection, country string) []string {
	if country == "" {
		country = CountryLU
	}
	out := make([]string, 0, len(AllPIICategories))
	for _, c := range AllPIICategories {
		if sel[c] && CategoryAppliesTo(c, country) {
			out = append(out, c)
		}
	}
	return out
}

// PreviewPatternMatches runs built-in pattern matching over docs and reports
// what it found, grouped for display.
//
// The gates are pass 1's own, in pass 1's order (detectText): the allowlist
// first (which is also how the session exclusions are enforced, so a removed
// value does not reappear here), then the checksum switch, then overlap
// resolution among the pattern spans themselves. requireChecksum is taken as an
// argument rather than read from anywhere, because the caller has to hand the
// preview the SAME value it hands the run: a preview promising a replacement the
// run does not make is the one thing this file may not do.
//
// The result is sorted by category (AllPIICategories order), then by count
// descending, then by text, so a repeated run renders identically.
func PreviewPatternMatches(docs []Document, sel CategorySelection, country string,
	requireChecksum bool, allow *Allowlist,
) []PatternMatch {
	if country == "" {
		country = CountryLU
	}
	// Keyed by category+text. The separator is a NUL, which no matched text can
	// contain, so two different pairs cannot collide into one row.
	type agg struct {
		match PatternMatch
		seen  map[string]bool // documents already credited, so Documents has no repeats
	}
	byKey := map[string]*agg{}
	var order []string

	for _, doc := range docs {
		for _, text := range previewTexts(doc) {
			spans := FilterAllowed(DetectPIISelected(text, sel, country), allow)
			if requireChecksum {
				spans = RejectFailedChecksums(spans)
			}
			spans = ResolveOverlaps(spans)
			for _, s := range spans {
				key := s.Category + "\x00" + s.Original
				entry := byKey[key]
				if entry == nil {
					entry = &agg{
						match: PatternMatch{
							Category:   s.Category,
							Text:       s.Original,
							Documents:  []string{},
							Confidence: effectiveConfidence(s),
						},
						seen: map[string]bool{},
					}
					byKey[key] = entry
					order = append(order, key)
				}
				entry.match.Count++
				if c := effectiveConfidence(s); c < entry.match.Confidence {
					entry.match.Confidence = c
				}
				if !entry.seen[doc.Name] {
					entry.seen[doc.Name] = true
					entry.match.Documents = append(entry.match.Documents, doc.Name)
				}
			}
		}
	}

	out := make([]PatternMatch, 0, len(order))
	for _, key := range order {
		out = append(out, byKey[key].match)
	}
	rank := map[string]int{}
	for i, c := range AllPIICategories {
		rank[c] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := rank[out[i].Category], rank[out[j].Category]; a != b {
			return a < b
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Text < out[j].Text
	})
	return out
}

// previewTexts is every piece of a document pass 1 reads, in the same shape
// detectDocument (pipeline.go) splits it into: a CSV's cells one by one, a
// complex sheet's JSON, or the markdown working form.
//
// It matters that the split is the SAME: a pattern anchored to a line start
// matches differently inside one cell than inside the whole rendered table, so
// previewing over the markdown of a grid document would report matches the run
// does not make.
func previewTexts(doc Document) []string {
	if doc.Grid != nil {
		var texts []string
		for _, row := range doc.Grid {
			texts = append(texts, row...)
		}
		return texts
	}
	if doc.Format == FormatXLSXJSON {
		return []string{doc.JSON}
	}
	return []string{doc.Markdown}
}
