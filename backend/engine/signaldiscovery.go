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
	"unicode"
)

// SignalDiscoveryInput is everything signal-based discovery reads.
type SignalDiscoveryInput struct {
	// Documents is the WHOLE imported batch. Signal-based discovery is a
	// batch-level method by nature, so this is not optional.
	Documents []Document
	// Sources says which DERIVATIONS of which signals may derive Suggestions.
	// Nil, or a missing key at either level, means the defaults (signals.go),
	// never "none".
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
	// Nothing to do when no reading of any signal is on. Asked source by source
	// rather than as one flag, so adding a source cannot leave this behind.
	any := false
	for _, source := range AllSignalSources {
		if SignalSourceEnabled(in.Sources, source) {
			any = true
			break
		}
	}
	if !any {
		return nil
	}
	// One batch per source, merged through the shared rule, so two sources
	// agreeing on a string is one reviewable row carrying both pieces of
	// evidence rather than two rivals.
	return MergeSuggestions(discoverFromEmails(in), discoverFromWebsites(in))
}

// discoverFromWebsites is the website source: a URL's registrable domain label
// seeds an organisation, and the seed is then SEARCHED FOR in the whole batch,
// exactly as an email domain's is.
//
// It answers a case email evidence cannot. A contract between two companies
// routinely contains no address at all while printing each party's website, and
// the domain label is often the SHORT form of the name ("nstar" for
// "Northstar") that no spelling-derivation rule could produce from the long one.
//
// Everything an email domain is filtered by applies here for the same reasons:
// public-suffix labels, public mail providers, infrastructure labels and a
// minimum length. A URL PATH is deliberately ignored: a page is not an
// organisation.
func discoverFromWebsites(in SignalDiscoveryInput) []Suggestion {
	if !SignalDerivationEnabled(in.Sources, SignalSourceWebsite, DerivationWebsiteOrganisation) {
		return nil
	}
	seeds := map[string]*emailSeed{}
	spans := map[string][][2]int{}

	for _, doc := range in.Documents {
		for _, m := range websitePatternRe.FindAllStringIndex(doc.Markdown, -1) {
			url := doc.Markdown[m[0]:m[1]]
			spans[doc.Name] = append(spans[doc.Name], [2]int{m[0], m[1]})
			for _, seed := range websiteSeeds(url, doc.Name) {
				addSeed(seeds, seed)
			}
		}
	}
	if len(seeds) == 0 {
		return nil
	}
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

// websitePatternRe is the URL shape used for EVIDENCE. It deliberately mirrors
// pii.go's URL rule, so a URL that is anonymised is a URL that can be read as
// evidence: two shapes drifting apart would make a signal appear or vanish for
// reasons neither the user nor a test could see.
var websitePatternRe = regexp.MustCompile(`https?://[^\s<>"')\]]+` +
	`|\bwww\.[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)+(?:/[^\s<>"')\],]*)?`)

// websiteSeeds derives an organisation seed from a URL's registrable domain
// label, or nothing when the host names no organisation.
func websiteSeeds(url, docName string) []emailSeed {
	host := urlHost(url)
	if host == "" {
		return nil
	}
	labels := strings.Split(host, ".")
	// Walk in from the right past the public suffix, exactly as an email domain
	// is walked: two-level suffixes ("co.uk") are why this is a loop.
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
			Kind:           EvidenceWebsiteDomain,
			SignalCategory: CatURL,
			SignalText:     url,
			Documents:      []string{docName},
		},
	}}
}

// urlHost returns the lower-cased host of a URL, with the scheme, any
// credentials, the port and the path removed. It is a few string cuts rather
// than net/url because the input is already a regex match of a known shape, and
// a parse error on a matched string would be a silent miss.
func urlHost(url string) string {
	host := strings.ToLower(url)
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+len("://"):]
	}
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:] // credentials
	}
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i] // port
	}
	return strings.Trim(host, ".")
}

// emailSeed is one string an email address suggests, with the evidence that
// produced it.
type emailSeed struct {
	// folded is the accent-folded, lower-cased form the search matches on. It is
	// the MAIN form: whatever it matches becomes a Suggestion's main text.
	folded string
	// alsoFolded are further forms to search for whose matches become SPELLINGS of
	// the main form's Suggestion rather than Suggestions of their own.
	//
	// This is the difference between one reviewable Value and several rivals. An
	// address gives a full name and its parts, and the parts are spellings of the
	// same person: emitted as their own rows they would be folded afterwards by
	// FoldValueFamilies, which promotes the SHORTEST member to main text, and the
	// row would end up named "Dupont" rather than "Pierre Dupont". Attaching them
	// here keeps the main text the form the user recognises.
	alsoFolded []string
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
			// Each reading is gated on its OWN derivation. Clearing one stops
			// only its Suggestions: the address itself is still anonymised by
			// pass 1, and the other reading still produces its own rows.
			if SignalDerivationEnabled(in.Sources, SignalSourceEmail, DerivationEmailPerson) {
				for _, seed := range personSeeds(local, address, doc.Name) {
					addSeed(seeds, seed)
				}
			}
			if SignalDerivationEnabled(in.Sources, SignalSourceEmail, DerivationEmailOrganisation) {
				for _, seed := range organisationSeeds(domain, address, doc.Name) {
					addSeed(seeds, seed)
				}
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
		for _, extra := range seed.alsoFolded {
			if !containsFolded(existing.alsoFolded, extra) {
				existing.alsoFolded = append(existing.alsoFolded, extra)
			}
		}
		return
	}
	copied := seed
	seeds[key] = &copied
}

// containsFolded reports whether a folded form is already listed.
func containsFolded(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
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
// The main seed is the FULL name, and each part of it is an ADDITIONAL form,
// because a document that writes the address once usually writes "Pierre Dupont"
// in one place and "Dupont" in another. Both belong to one Value, and the full
// name is the main text: it is the form the user recognises, and it is what the
// placeholder ends up standing for. A part that is an ordinary word ("may",
// "will") is not searched for at all.
func personSeeds(local, address, docName string) []emailSeed {
	lower := strings.ToLower(local)
	if nonNameMailboxes[lower] {
		return nil
	}
	tokens := strings.FieldsFunc(lower, func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == '+'
	})
	var words []string
	for _, p := range tokens {
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
	var bareForms []string
	for _, w := range []string{words[0], words[len(words)-1]} {
		if smartCommonWords[w] || len([]rune(w)) < minSeedLen || containsFolded(bareForms, w) {
			continue
		}
		bareForms = append(bareForms, w)
	}
	return []emailSeed{{
		folded:     strings.Join(words, " "),
		alsoFolded: bareForms,
		category:   CatPersonNames,
		evidence:   evidence,
	}}
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
	found := map[string]*Suggestion{}
	collectSeedForm(seed, seed.folded, in, spans, found)

	if len(found) == 0 {
		// No occurrence of the main form, so there is nothing for the additional
		// forms to be spellings OF. Suggesting a bare surname on its own would be
		// a Value named after a fragment.
		return nil
	}

	// The additional forms become SPELLINGS of whatever the main form matched, so
	// one person is one reviewable row.
	spellings := map[string]bool{}
	for _, extra := range seed.alsoFolded {
		extraHits := map[string]*Suggestion{}
		collectSeedForm(seed, extra, in, spans, extraHits)
		for _, hit := range extraHits {
			spellings[hit.MainText] = true
		}
	}

	out := make([]Suggestion, 0, len(found))
	for _, s := range found {
		if len(spellings) > 0 {
			list := make([]string, 0, len(spellings))
			for text := range spellings {
				list = append(list, text)
			}
			sort.Strings(list)
			s.Spellings = MergeSpellings(s.Spellings, list, s.MainText)
		}
		out = append(out, s.WithMethod(MethodSignal))
	}
	// A bare organisation label is dropped when a longer name built on it was
	// also found. Keeping both would hand FoldValueFamilies a stem that folds
	// every extension into one family, and "Tpps France" and "Tpps Holdings"
	// would silently become one Value with one placeholder. Shared domain
	// evidence makes them RELATED; only the user can say they are the same.
	if seed.category == CatEntityNames {
		out = dropBareStems(out, seed.folded)
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

// collectSeedForm searches the whole batch for ONE folded form and accumulates a
// Suggestion per distinct spelling found, keyed on the lower-cased matched text.
func collectSeedForm(seed emailSeed, form string, in SignalDiscoveryInput,
	spans map[string][][2]int, found map[string]*Suggestion,
) {
	// The form's words, so the search can require them in order with ordinary
	// separators between: a single regexp built from the folded form cannot,
	// because folding has already removed the accents the document may carry.
	words := strings.Fields(form)
	if len(words) == 0 {
		return
	}

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
	// stop is the end of the last token that is part of the NAME. A particle or a
	// connector ("de", "of", "and") is only part of a name when a capitalised word
	// follows it, so it is accepted into `pending` and only committed when one
	// does. Without that, "Tpps France for approval" extends through "for" and the
	// Suggestion is a phrase rather than a name.
	stop := 0
	pending := 0
	for i, tok := range tokens {
		// Only an ordinary single space continues a name; a newline, a comma or a
		// double space ends it, because a name does not span a list item or a line
		// break.
		from := end + stop
		if pending > stop {
			from = end + pending
		}
		if i == 0 {
			from = end
		}
		if text[from:end+tok.start] != " " {
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
		switch {
		case isCapWord(word) || isSuffix:
			stop = tok.end
			pending = tok.end
		case smartParticles[folded] || smartConnectors[folded]:
			pending = tok.end
		default:
			return end + appendLegalSuffix(text[end:], stop)
		}
	}
	return end + appendLegalSuffix(text[end:], stop)
}

// appendLegalSuffix extends a run over a trailing legal form matched LITERALLY in
// the raw text.
//
// It cannot be done in the token walk above, because the tokenizer splits on
// non-letters and "S.à r.l." is four tokens of one or two letters each. Matching
// the suffix as a string is the only way to keep it whole, and keeping it whole
// matters: the suffix is part of the name as written, and pass 2's own derivation
// is what later matches the form without it.
//
// legalSuffixes is ordered longest first, so "S.à r.l." wins over "S.A.".
//
// @param rest the text from the end of the seed match onwards
// @param stop the offset within `rest` the token walk reached
// @return the offset within `rest` including any trailing legal form
func appendLegalSuffix(rest string, stop int) int {
	for _, suffix := range legalSuffixes {
		candidate := " " + suffix
		if len(rest) < stop+len(candidate) {
			continue
		}
		if !strings.EqualFold(rest[stop:stop+len(candidate)], candidate) {
			continue
		}
		// The suffix must END the name: a letter or digit straight after it means
		// the match landed inside a longer word.
		after := stop + len(candidate)
		if after < len(rest) && isWordChar(firstRuneAt(rest, after)) {
			continue
		}
		return after
	}
	return stop
}

// isWordChar reports whether a rune is a letter or a digit, which is the boundary
// rule the rest of the engine uses.
func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// dropBareStems removes the Suggestion whose text is exactly the seed label when
// a longer name built on it is also present.
func dropBareStems(out []Suggestion, stem string) []Suggestion {
	if len(out) < 2 {
		return out
	}
	longerExists := false
	for _, s := range out {
		key := strings.ToLower(s.MainText)
		if key != stem && containsAtWordBoundary(key, stem) {
			longerExists = true
			break
		}
	}
	if !longerExists {
		return out
	}
	kept := make([]Suggestion, 0, len(out))
	for _, s := range out {
		if strings.ToLower(s.MainText) == stem {
			continue
		}
		kept = append(kept, s)
	}
	return kept
}
