// engine/signaldiscovery.go — using a built-in pattern match as EVIDENCE.
//
// An email address is personal data in its own right, and built-in pattern
// matching replaces it. It is also deterministic evidence about text written
// somewhere else: "pierre.dupont@tpps.com" says a person called Pierre Dupont
// and an organisation whose name starts "Tpps" are involved in this engagement,
// and both may appear in prose in a completely different file.
//
// That inference is worth making and must never be made silently, so this file
// produces SUGGESTIONS. Nothing here mints a placeholder, changes a category or
// touches the registry: the user accepts or rejects each finding on Identify
// like any other Suggestion.
//
// Three rules shape every decision below.
//
//	WHOLE BATCH.   The evidence is in one file and the text it points at is
//	               usually in another, so the search reads every imported
//	               document. Per document it would find almost nothing.
//	OUTSIDE THE SIGNAL.  A match inside the email address itself proves nothing:
//	               the address is already being replaced, and "Pierre Dupont"
//	               found only inside "pierre.dupont@tpps.com" is the seed reading
//	               itself back. Nothing is suggested unless the text occurs
//	               outside every occurrence of the signal that produced it.
//	THE DOCUMENT'S OWN SPELLING.  What the user sees replaced has to be what
//	               they wrote, so the Suggestion carries the casing and accents
//	               found in the document, not the flattened form of the seed.
package engine

import (
	"regexp"
	"sort"
	"strings"
)

// SignalDiscoveryInput is everything signal-based discovery reads.
type SignalDiscoveryInput struct {
	// Documents is the WHOLE imported batch. Signal-based discovery is a
	// batch-level method by nature, so this is not optional.
	Documents []Document
	// Sources says which signals may derive Suggestions. Nil means the
	// defaults (signals.go), never "none".
	Sources SignalSourceSelection
	// Country scopes nothing today, and is carried because the org-name rules a
	// future source may need are country-scoped exactly as the keyword signal is.
	Country string
	// Allow is the never-anonymise list WITH the session exclusions already
	// applied, so a removed Value cannot be re-suggested and a removal read as
	// undone the moment detection runs again.
	Allow *Allowlist
}

// DiscoverFromSignals returns the Suggestions the enabled signals support.
//
// Every Suggestion carries MethodSignal and at least one piece of Evidence, so
// the workspace can say WHY it is there and the user can judge it rather than
// take it on trust.
//
// @param in the batch, the enabled sources, and the allowlist veto
// @return merged Suggestions, empty when nothing is enabled or nothing is found
func DiscoverFromSignals(in SignalDiscoveryInput) []Suggestion {
	if !SignalSourceEnabled(in.Sources, SignalSourceEmail) {
		return nil
	}
	return discoverFromEmails(in)
}

// emailSeed is one candidate string an email address suggests, with the
// evidence that produced it.
type emailSeed struct {
	// folded is the accent-folded, lower-cased form the search matches on.
	folded string
	// category is where an accepted Value would be filed.
	category string
	// evidence explains the seed. One piece per address that produced it.
	evidence Evidence
}

// discoverFromEmails is the email source: local parts seed people, domains seed
// organisations, and each seed is then SEARCHED FOR in the whole batch.
//
// A seed on its own is never a Suggestion. "tpps" derived from a domain is a
// guess about a string; the Suggestion is the text the documents actually
// contain, and if the documents contain none, there is nothing to review.
func discoverFromEmails(in SignalDiscoveryInput) []Suggestion {
	// Seeds first, over the whole batch, so an address in one file can be
	// matched against prose in every other.
	seeds := map[string]*emailSeed{}
	// spans records where each address occurs, per document, so a match can be
	// tested for being inside the signal that produced it.
	spans := map[string][][2]int{}

	for _, doc := range in.Documents {
		for _, m := range emailPatternRe.FindAllStringSubmatchIndex(doc.Markdown, -1) {
			address := doc.Markdown[m[0]:m[1]]
			spans[doc.Name] = append(spans[doc.Name], [2]int{m[0], m[1]})

			local, domain, ok := splitEmail(address)
			if !ok {
				continue
			}
			for _, seed := range personSeeds(local, address, doc.Name) {
				addSeed(seeds, seed)
			}
			for _, seed := range organisationSeeds(domain, address, doc.Name) {
				addSeed(seeds, seed)
			}
		}
	}
	if len(seeds) == 0 {
		return nil
	}

	// Deterministic seed order, so two runs over the same batch produce the same
	// Suggestions in the same order.
	keys := make([]string, 0, len(seeds))
	for k := range seeds {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var batches [][]Suggestion
	for _, key := range keys {
		batches = append(batches, matchSeed(*seeds[key], in, spans))
	}
	return MergeSuggestions(batches...)
}

// addSeed records a seed, merging the evidence when two addresses support the
// same string. Two people at one company support the same organisation seed, and
// that is one Suggestion with two pieces of evidence, not two rows.
func addSeed(seeds map[string]*emailSeed, seed emailSeed) {
	key := seed.category + "|" + seed.folded
	if existing, ok := seeds[key]; ok {
		existing.evidence.Documents = mergeDocuments(existing.evidence.Documents, seed.evidence.Documents)
		return
	}
	copied := seed
	seeds[key] = &copied
}

// emailPatternRe is the address shape used for EVIDENCE. It is deliberately the
// same shape pass 1 matches on (pii.go), so a signal that gets anonymised is
// exactly a signal that can be used as evidence: two different shapes would mean
// an address that seeds a Suggestion but is left in clear text, or the reverse.
var emailPatternRe = regexp.MustCompile(`([A-Za-z0-9._%+-]+)@([A-Za-z0-9.-]+\.[A-Za-z]{2,})`)

// splitEmail returns the local part and the domain of an address.
func splitEmail(address string) (local, domain string, ok bool) {
	at := strings.LastIndex(address, "@")
	if at <= 0 || at == len(address)-1 {
		return "", "", false
	}
	return address[:at], address[at+1:], true
}

// minSeedLen is the shortest seed worth searching for. Two characters would put
// a common word fragment into every document; three is the same floor
// minSpellingLen applies to a spelling, and for the same reason.
const minSeedLen = 3

// personSeeds derives person seeds from an address's local part.
//
// A role mailbox derives nothing: "info@acme.com" must not invent a person
// called Info. A single token derives nothing either: "oscarl@acme.com" is a
// handle, and splitting a handle into a name is guesswork rather than evidence.
// Only a local part that is genuinely structured as several name tokens is
// treated as naming a person.
//
// The seeds are the FULL name and each part of it, because a document that
// writes the address once usually writes "Pierre Dupont" in one place and
// "Dupont" in another, and both should reach the review list as spellings of one
// Value. A part that is an ordinary word ("may", "will") is not seeded alone.
func personSeeds(local, address, docName string) []emailSeed {
	lower := strings.ToLower(local)
	if nonNameMailboxes[lower] {
		return nil
	}
	parts := strings.FieldsFunc(lower, func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == '+'
	})
	var words []string
	for _, p := range parts {
		if len([]rune(p)) >= 2 && !isAllDigits(p) && !nonNameMailboxes[p] {
			words = append(words, foldAccentsLower(p))
		}
	}
	if len(words) < 2 {
		return nil
	}

	evidence := Evidence{
		Kind:           EvidenceEmailLocalPart,
		SignalCategory: CatEmail,
		SignalText:     address,
		Documents:      []string{docName},
	}
	seeds := []emailSeed{{
		folded:   strings.Join(words, " "),
		category: CatPersonNames,
		evidence: evidence,
	}}
	for _, w := range []string{words[0], words[len(words)-1]} {
		if smartCommonWords[w] || len([]rune(w)) < minSeedLen {
			continue
		}
		seeds = append(seeds, emailSeed{folded: w, category: CatPersonNames, evidence: evidence})
	}
	return seeds
}

// organisationSeeds derives organisation seeds from an address's domain.
//
// Only the REGISTRABLE label is a seed: "tpps" out of "mail.tpps.co.uk". The
// public-suffix labels and the infrastructure labels are not names of anything,
// and seeding them would search every document for "mail", "co" and "uk".
//
// A generic mail provider derives nothing. "someone@gmail.com" says nothing
// whatever about an organisation involved in the engagement, and seeding "gmail"
// would suggest anonymising the word in a sentence about email providers.
func organisationSeeds(domain, address, docName string) []emailSeed {
	labels := strings.Split(strings.ToLower(domain), ".")
	// Walk from the right past every public-suffix-ish label to the first label
	// that names something. Two-level suffixes ("co.uk", "com.au") are why this
	// is a loop rather than "the second to last label".
	i := len(labels) - 1
	for i >= 0 && publicSuffixLabels[labels[i]] {
		i--
	}
	if i < 0 {
		return nil
	}
	label := labels[i]
	if publicMailProviders[label] || infrastructureLabels[label] || len([]rune(label)) < minSeedLen {
		return nil
	}
	return []emailSeed{{
		folded:   foldAccentsLower(label),
		category: CatEntityNames,
		evidence: Evidence{
			Kind:           EvidenceEmailDomain,
			SignalCategory: CatEmail,
			SignalText:     address,
			Documents:      []string{docName},
		},
	}}
}

// publicSuffixLabels are the domain labels that are part of a public suffix
// rather than a name. It is a SHORT list on purpose: a full public-suffix list
// is a vendored dataset that would need updating, and the failure mode of a
// missing entry here is one extra Suggestion the user rejects, not a leak.
var publicSuffixLabels = map[string]bool{
	"com": true, "net": true, "org": true, "int": true, "edu": true, "gov": true,
	"mil": true, "info": true, "biz": true, "eu": true, "io": true, "co": true,
	"lu": true, "fr": true, "de": true, "be": true, "nl": true, "uk": true,
	"ch": true, "at": true, "it": true, "es": true, "pt": true, "ie": true,
	"us": true, "ca": true, "au": true, "nz": true, "se": true, "no": true,
	"dk": true, "fi": true, "pl": true, "cz": true, "gov.uk": true,
}

// publicMailProviders are consumer and generic mail hosts. A domain that is one
// of these is the user's mail provider, never a party to the engagement.
var publicMailProviders = map[string]bool{
	"gmail": true, "googlemail": true, "google": true, "outlook": true,
	"hotmail": true, "live": true, "msn": true, "yahoo": true, "ymail": true,
	"aol": true, "icloud": true, "me": true, "mac": true, "gmx": true,
	"web": true, "mail": true, "protonmail": true, "proton": true, "pm": true,
	"zoho": true, "yandex": true, "orange": true, "free": true, "laposte": true,
	"wanadoo": true, "sfr": true, "t-online": true, "libero": true,
	"qq": true, "163": true, "126": true, "naver": true, "hanmail": true,
	"example": true, // the reserved documentation domain: never a real party
}

// infrastructureLabels are labels that describe a MACHINE rather than an
// organisation. They appear as the registrable label only in malformed
// addresses, and suggesting them would put "smtp" and "webmail" in the review
// list.
var infrastructureLabels = map[string]bool{
	"mail": true, "smtp": true, "imap": true, "pop": true, "mx": true,
	"webmail": true, "email": true, "exchange": true, "srv": true,
	"server": true, "host": true, "localhost": true, "internal": true,
	"local": true, "corp": true, "intranet": true, "www": true,
}

// matchSeed searches the whole batch for a seed and returns one Suggestion per
// distinct spelling found.
//
// The result is keyed on what the DOCUMENTS say, not on the seed: the seed is
// flattened and lower-cased, and replacing "pierre dupont" when the document
// says "Pierre Dupont" would leave the real text untouched. Each distinct
// spelling found becomes its own Suggestion; FoldValueFamilies later collapses
// the ones that are spellings of one Value, using the same rules every other
// method's output goes through.
func matchSeed(seed emailSeed, in SignalDiscoveryInput, spans map[string][][2]int) []Suggestion {
	// The seed's words, so the search can require them in order with ordinary
	// separators between: a single regexp built from the folded form cannot,
	// because folding has already removed the accents the document may carry.
	words := strings.Fields(seed.folded)
	if len(words) == 0 {
		return nil
	}

	found := map[string]*Suggestion{}
	for _, doc := range in.Documents {
		for _, hit := range findSeedRuns(doc.Markdown, words) {
			if insideAnySpan(hit.start, hit.end, spans[doc.Name]) {
				// Inside the address that produced the seed: the seed reading
				// itself back, which is evidence of nothing.
				continue
			}
			// An organisation seed is the START of a name, not the name: the
			// domain says "tpps" and the document says "Tpps France". Extending
			// over the capitalised run is what makes the Suggestion the thing the
			// user would actually want replaced, and it is also what keeps two
			// different legal entities apart, because each extends differently.
			if seed.category == CatEntityNames {
				hit.end = extendOrganisationRun(doc.Markdown, hit.end)
			}
			text := doc.Markdown[hit.start:hit.end]
			if in.Allow.Contains(text) {
				continue // the never-anonymise list, and the session exclusions
			}
			key := strings.ToLower(text)
			s, ok := found[key]
			if !ok {
				fresh := Suggestion{
					MainText: text,
					Category: seed.category,
					Count:    1,
					Contexts: []string{contextSnippet(doc.Markdown, hit.start, hit.end)},
					Evidence: []Evidence{seed.evidence},
					// Signal-based discovery is DETERMINISTIC: the evidence is a
					// regex match and the finding is a literal occurrence of text
					// derived from it. It is scored as a strong suggestion, but it
					// is still a suggestion, which is why it is scored at all.
					Confidence: ConfidenceSignalDerived,
				}
				found[key] = &fresh
				continue
			}
			s.Count++
			s.Contexts = MergeContexts(s.Contexts, []string{contextSnippet(doc.Markdown, hit.start, hit.end)})
			s.Evidence = MergeEvidence(s.Evidence, []Evidence{{
				Kind:           seed.evidence.Kind,
				SignalCategory: seed.evidence.SignalCategory,
				SignalText:     seed.evidence.SignalText,
				Documents:      []string{doc.Name},
			}})
		}
	}

	// A bare organisation label is dropped when a longer name built on it was
	// also found. Keeping both would hand FoldValueFamilies a stem that folds
	// every extension into one family, and "Tpps France" and "Tpps Holdings"
	// would silently become one Value with one placeholder. Shared domain
	// evidence makes them RELATED; only the user can say they are the same.
	if seed.category == CatEntityNames {
		dropBareStems(found, seed.folded)
	}

	out := make([]Suggestion, 0, len(found))
	for _, s := range found {
		out = append(out, s.WithMethod(MethodSignal))
	}
	// Deterministic order: most frequent first, ties by text.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].MainText < out[j].MainText
	})
	return out
}

// seedRun is one occurrence of a seed in a document, as byte offsets.
type seedRun struct{ start, end int }

// findSeedRuns locates every word-boundary-anchored occurrence of the seed's
// words, in order, separated only by single spaces, hyphens or full stops.
//
// It works on TOKENS rather than a regexp because the document may spell a name
// with accents the folded seed no longer has: comparing token by token after
// folding each one is what lets the seed "jose" match the document's "José"
// while still refusing "Josephine".
func findSeedRuns(text string, words []string) []seedRun {
	tokens := tokenize(text)
	var out []seedRun
	for i := 0; i+len(words) <= len(tokens); i++ {
		match := true
		for j, w := range words {
			if foldAccentsLower(tokens[i+j].text) != w {
				match = false
				break
			}
			// Between two words of the seed only an ordinary name separator is
			// allowed, so "Pierre said Dupont agreed" is not "Pierre Dupont".
			if j > 0 && !seedSeparator(text[tokens[i+j-1].end:tokens[i+j].start]) {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		start, end := tokens[i].start, tokens[i+len(words)-1].end
		if !isWordBoundary(text, start, end) {
			continue
		}
		out = append(out, seedRun{start: start, end: end})
	}
	return out
}

// seedSeparator reports whether the text between two seed words is an ordinary
// name separator. Anything else means the two words are not one name.
func seedSeparator(between string) bool {
	switch between {
	case " ", "-", ".", ". ", " - ", "'", "’":
		return true
	default:
		return false
	}
}

// insideAnySpan reports whether [start,end) falls inside one of the recorded
// signal spans.
func insideAnySpan(start, end int, spans [][2]int) bool {
	for _, s := range spans {
		if start >= s[0] && end <= s[1] {
			return true
		}
	}
	return false
}

// extendOrganisationRun grows a match rightwards over the rest of a capitalised
// organisation name: "Tpps" in "Tpps France Holdings S.A." becomes the whole
// phrase.
//
// It stops at the first token that is neither capitalised nor an accepted
// in-name particle or connector, so it cannot run off the end of a sentence and
// swallow the next clause. A legal suffix is included, because the suffix is part
// of the name as written, and pass 2's own expansion is what later matches the
// form without it.
//
// @param text the document
// @param end the byte offset just past the seed match
// @return the byte offset just past the extended name
func extendOrganisationRun(text string, end int) int {
	tokens := tokenize(text[end:])
	stop := 0
	for i, tok := range tokens {
		// Only ordinary single separators continue a name; a newline or a comma
		// ends it, because a name does not span a list item or a line break.
		between := text[end+stop : end+tok.start]
		if i == 0 {
			between = text[end : end+tok.start]
		}
		if !strings.HasPrefix(between, " ") || strings.TrimSpace(between) != "" || len(between) > 1 {
			break
		}
		word := tok.text
		folded := foldAccentsLower(word)
		isSuffix := false
		for _, suffix := range legalSuffixes {
			if strings.EqualFold(word, suffix) {
				isSuffix = true
				break
			}
		}
		if !isCapWord(word) && !isSuffix && !smartParticles[folded] && !smartConnectors[folded] {
			break
		}
		stop = tok.end
	}
	return end + stop
}

// dropBareStems removes the suggestion whose text is exactly the seed label when
// a longer name built on it is also present.
//
// The map is keyed on lower-cased text, so the stem is looked up directly rather
// than searched for.
func dropBareStems(found map[string]*Suggestion, stem string) {
	if len(found) < 2 {
		return
	}
	if _, ok := found[stem]; !ok {
		return
	}
	for key := range found {
		if key != stem && containsAtWordBoundary(key, stem) {
			delete(found, stem)
			return
		}
	}
}
