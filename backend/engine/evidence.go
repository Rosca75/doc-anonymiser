// engine/evidence.go — WHY a discovery method produced a Suggestion.
//
// Evidence is STRUCTURED and BOUNDED on purpose. The alternative, returning a
// sentence from the engine and parsing it in the frontend, makes the copy a
// contract nobody can check and puts the explanation out of reach of a test.
// Here each piece of evidence names its kind, what it came from and where, and
// the frontend turns that into a sentence in copy.js where all the other copy
// lives.
package engine

// Evidence kinds. Each names WHAT relationship the discovery method found, not
// what it concluded, so a kind stays true even when the wording around it
// changes.
const (
	// EvidenceEmailLocalPart is the part before the "@" of a matched email
	// address, used as a person seed.
	EvidenceEmailLocalPart = "email_local_part"
	// EvidenceEmailDomain is the registrable part of a matched email address's
	// domain, used as an organisation seed.
	EvidenceEmailDomain = "email_domain"
	// EvidenceWebsiteDomain is the registrable label of a matched website URL,
	// read as an organisation's name.
	EvidenceWebsiteDomain = "website_domain"
)

// AllEvidenceKinds lists the kinds the engine can emit, so the frontend copy
// table can be checked against it rather than guessed at.
var AllEvidenceKinds = []string{EvidenceEmailLocalPart, EvidenceEmailDomain, EvidenceWebsiteDomain}

// Evidence is one reason a discovery method produced a Suggestion.
type Evidence struct {
	// Kind is one of AllEvidenceKinds.
	Kind string `json:"kind"`
	// SignalCategory is the built-in pattern category the signal came from
	// (e.g. CatEmail), so the workspace can say which signal without inferring
	// it from the kind.
	SignalCategory string `json:"signalCategory,omitempty"`
	// SignalText is the matched signal itself, verbatim. It is sensitive: it is
	// a real value out of a real document, and it travels only inside the
	// application's own process and session file.
	SignalText string `json:"signalText,omitempty"`
	// Documents names up to maxEvidenceDocuments documents the evidence was
	// found in, so the explanation can point somewhere.
	Documents []string `json:"documents,omitempty"`
}

// maxEvidenceDocuments bounds one piece of evidence's document list. The
// explanation names a few places to look; listing fifty would be a wall of text
// where a sentence was wanted, and it would grow the payload with every file.
const maxEvidenceDocuments = 3

// evidenceKey identifies one piece of evidence for deduplication, ignoring the
// document list so the same relationship found in five files is ONE piece of
// evidence naming several documents rather than five near-identical ones.
func evidenceKey(e Evidence) string {
	return e.Kind + "|" + e.SignalCategory + "|" + e.SignalText
}

// MergeEvidence unions two evidence lists, deduplicating by relationship and
// merging each one's document list up to maxEvidenceDocuments. Input order is
// preserved so the result is deterministic.
func MergeEvidence(into, from []Evidence) []Evidence {
	index := map[string]int{}
	out := make([]Evidence, 0, len(into)+len(from))
	appendOne := func(e Evidence) {
		key := evidenceKey(e)
		if at, seen := index[key]; seen {
			out[at].Documents = mergeDocuments(out[at].Documents, e.Documents)
			return
		}
		e.Documents = mergeDocuments(nil, e.Documents)
		index[key] = len(out)
		out = append(out, e)
	}
	for _, e := range into {
		appendOne(e)
	}
	for _, e := range from {
		appendOne(e)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mergeDocuments appends the names not already listed, up to the cap. The cap is
// a silent truncation of a LIST OF EXAMPLES, which is what the field is for, so
// it does not need to be reported the way a truncated set of findings would.
func mergeDocuments(into, from []string) []string {
	seen := map[string]bool{}
	for _, d := range into {
		seen[d] = true
	}
	for _, d := range from {
		if len(into) >= maxEvidenceDocuments {
			break
		}
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		into = append(into, d)
	}
	return into
}
