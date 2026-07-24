// engine/discover.go — the Smart detection tier (BUILD-02 Phase 8a): a
// fully OFFLINE heuristic discovery pass that always works without
// Ollama. It proposes candidate entities from how names are written, for
// the review screen; nothing it finds is ever replaced without explicit
// user acceptance (the Phase 9 review gate).
//
// Detectors, in order:
//  1. Capitalised-run extraction: unicode-aware sequences of capitalised
//     words, tolerating French/Dutch surname particles (de, du, des, van,
//     von, le, la, den, der; table below) and hyphenated names
//     ("Jean-Pierre Muller"). A run whose ONLY occurrence sits at a
//     sentence start is dropped (sentence-case noise); repeated
//     sentence-start runs are kept.
//  2. Legal-suffix gazetteer (Luxembourg-aware): a capitalised run
//     followed by S.A., S.à r.l., GmbH, ... is a client_names candidate
//     with high confidence (suffix included in the candidate text).
//  3. Frequency analysis: runs occurring twice or more qualify on their
//     own; a single-occurrence SINGLE-WORD run without a suffix or title
//     cue is dropped (too noisy). Single-occurrence multi-word runs are
//     kept: "Marie Duval" mid-sentence is a strong name signal even once.
//  4. Title cues (Mr, Mrs, Ms, Dr, Me, M., Mme, ...) route the following
//     name to person_names (the cue itself is not part of the candidate).
//
// The allowlist veto is applied LAST — allowlist wins, as everywhere
// (CLAUDE.md §5).
//
// UI-agnostic and I/O-free per CLAUDE.md §4: text in, candidates out.
package engine

import (
	"sort"
	"strings"
	"unicode"
)

// Candidate is one Smart-detection proposal for the review UI (and for
// LLM span classification, BUILD-02 Phase 8b).
type Candidate struct {
	Text     string `json:"text"`
	Category string `json:"category"`
	// Count is how many times the exact text occurs in the document.
	Count int `json:"count"`
	// Contexts holds up to 3 snippets of ±60 runes around occurrences,
	// for the review UI and for LLM classification prompts.
	Contexts []string `json:"contexts,omitempty"`
}

// smartParticles are the lowercase name particles tolerated INSIDE a
// capitalised run ("Anouk van den Berg"). Table-driven so extending the
// list is a one-line change.
var smartParticles = map[string]bool{
	"de": true, "du": true, "des": true, "van": true, "von": true,
	"le": true, "la": true, "den": true, "der": true, "ten": true, "ter": true,
}

// smartTitles are the person-title cues (detector 4). Checked against the
// first token of a run AND the token right before it ("M." is a single
// letter and never joins a run itself).
var smartTitles = map[string]bool{
	"Mr": true, "Mrs": true, "Ms": true, "Dr": true, "Me": true,
	"M": true, "Mme": true, "Mlle": true, "Prof": true, "Herr": true, "Frau": true,
}

// smartLeadingStopwords are articles/pronouns that must not OPEN a run:
// "The CSSF" is the term "CSSF", not an entity called "The CSSF".
// Table-driven; extend freely.
var smartLeadingStopwords = map[string]bool{
	"The": true, "A": true, "An": true, "This": true, "That": true,
	"These": true, "Those": true, "Our": true, "We": true, "They": true,
	"Le": true, "La": true, "Les": true, "Un": true, "Une": true,
	"Ce": true, "Cette": true, "Ces": true, "Nos": true, "Notre": true,
}

// smartLegalSuffixes is the Luxembourg-aware legal-form gazetteer
// (detector 2), matched longest-first against the text following a run.
// Table-driven; extend freely.
var smartLegalSuffixes = []string{
	"S.à r.l.", "S.à.r.l.", "S.àr.l.", "Sàrl", "SARL",
	"S.C.A.", "S.C.S.", "SCSp", "S.p.A.", "S.A.", "SA", "ASBL", "SE",
	"GmbH", "AG", "N.V.", "B.V.", "Ltd", "LLC", "plc", "SAS", "Inc.", "Inc",
}

// contextRadius is the snippet half-width around an occurrence (runes).
const contextRadius = 60

// maxContexts caps how many snippets one candidate carries.
const maxContexts = 3

// smartRun is one occurrence of a capitalised run during extraction.
type smartRun struct {
	text          string
	start, end    int  // byte offsets in the source text
	sentenceStart bool // the run begins a sentence
	hasSuffix     bool // a legal suffix follows (and is included)
	hasTitle      bool // a person-title cue opened the run
	words         int  // significant (non-particle) word count
}

// SmartDetect extracts entity candidates from text using the four
// detectors above, then applies the allowlist veto. Deterministic: the
// result is sorted by descending count, then first appearance.
func SmartDetect(text string, allow *Allowlist) []Candidate {
	runs := extractRuns(text)

	// Group occurrences by candidate text (case-sensitive: "WEBER" and
	// "Weber" are different spellings the review UI should see as typed;
	// the registry collapses case later anyway).
	type group struct {
		text       string
		count      int
		firstStart int
		category   string
		qualifies  bool
		contexts   []string
		sentenceOnly bool
	}
	groups := map[string]*group{}
	order := []string{}

	for _, r := range runs {
		g, ok := groups[r.text]
		if !ok {
			g = &group{text: r.text, firstStart: r.start, sentenceOnly: true}
			groups[r.text] = g
			order = append(order, r.text)
		}
		g.count++
		if !r.sentenceStart {
			g.sentenceOnly = false
		}
		// Category priority: legal suffix beats title beats the default.
		switch {
		case r.hasSuffix:
			g.category = "client_names"
		case r.hasTitle && g.category == "":
			g.category = "person_names"
		}
		if r.hasSuffix || r.hasTitle {
			g.qualifies = true
		}
		if len(g.contexts) < maxContexts {
			g.contexts = append(g.contexts, contextSnippet(text, r.start, r.end))
		}
	}

	var out []Candidate
	for _, key := range order {
		g := groups[key]
		r := firstRunFor(runs, key)

		// Sentence-start rule: a run seen ONLY at sentence starts needs a
		// second occurrence to qualify (kills "Ensuite", "Yesterday", ...)
		// unless a suffix or title cue already vouches for it (a company
		// opening a sentence is still a company).
		if g.sentenceOnly && g.count < 2 && !g.qualifies {
			continue
		}
		// Frequency rule: single-word single-occurrence runs without a
		// suffix or title cue are dropped.
		if !g.qualifies && g.count < 2 && r.words < 2 {
			continue
		}
		// Default category for unclassified runs: multi-word runs read as
		// person names, single words as organisation-ish client names.
		// This is only the INITIAL guess; the review UI and the optional
		// LLM classification (Phase 8b) refine it.
		if g.category == "" {
			if r.words >= 2 {
				g.category = "person_names"
			} else {
				g.category = "client_names"
			}
		}
		// Allowlist veto LAST (allowlist wins, CLAUDE.md §5).
		if allow.Contains(g.text) {
			continue
		}
		out = append(out, Candidate{
			Text:     g.text,
			Category: g.category,
			Count:    g.count,
			Contexts: g.contexts,
		})
	}

	// Deterministic ranking: frequent candidates first, ties by first
	// appearance in the document.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return groups[out[i].Text].firstStart < groups[out[j].Text].firstStart
	})
	return out
}

// firstRunFor returns the first occurrence of a run text (for word-count
// and offset metadata).
func firstRunFor(runs []smartRun, text string) smartRun {
	for _, r := range runs {
		if r.text == text {
			return r
		}
	}
	return smartRun{}
}

// extractRuns scans the text once and returns every capitalised-run
// occurrence with its metadata.
func extractRuns(text string) []smartRun {
	tokens := tokenize(text)
	var runs []smartRun

	i := 0
	for i < len(tokens) {
		if !isCapWord(tokens[i].text) {
			i++
			continue
		}

		// Leading articles never open a run ("The CSSF" is about "CSSF").
		// The stopword itself is consumed so it cannot re-trigger.
		if smartLeadingStopwords[tokens[i].text] {
			i++
			continue
		}

		// Collect the run: SPACE-ADJACENT capitalised words, tolerated
		// particles, and hyphenated capitalised words. Punctuation between
		// tokens (commas, sentence ends) always terminates the run.
		j := i
		last := i // index of the last CAPITALISED token accepted
		for j < len(tokens) {
			t := tokens[j].text
			if j > i && !tokens[j].adjacent {
				break // ", " or ". " between words: never one name
			}
			if isCapWord(t) {
				last = j
				j++
				continue
			}
			if smartParticles[strings.ToLower(t)] && j > i {
				// Lookahead: particles must lead to another ADJACENT
				// capitalised word ("van den Berg"), else they end the run.
				k := j
				for k < len(tokens) && smartParticles[strings.ToLower(tokens[k].text)] && tokens[k].adjacent {
					k++
				}
				if k < len(tokens) && isCapWord(tokens[k].text) && tokens[k].adjacent {
					j = k
					continue
				}
			}
			break
		}
		j = last + 1

		start := tokens[i].start
		end := tokens[last].end
		runText := text[start:end]

		r := smartRun{
			text:          runText,
			start:         start,
			end:           end,
			sentenceStart: tokens[i].sentenceStart,
			words:         significantWords(text[start:end]),
		}

		// Detector 4a: a title cue as the FIRST token ("Mme Weber",
		// "Dr Keller") routes to persons and is stripped from the text.
		firstTok := tokens[i].text
		if smartTitles[firstTok] && last > i {
			r.hasTitle = true
			r.start = tokens[i+1].start
			r.text = text[r.start:end]
			r.words = significantWords(r.text)
		} else if smartTitles[firstTok] && last == i {
			i = j
			continue // a bare title with no name is not a candidate
		}

		// Detector 4b: a title BEFORE the run ("M. Dupont": the dotted
		// single letter never joins the run). The separator may be a dot
		// plus spaces.
		if !r.hasTitle && i > 0 && smartTitles[tokens[i-1].text] {
			sep := text[tokens[i-1].end:tokens[i].start]
			if trimmed := strings.TrimLeft(strings.TrimPrefix(sep, "."), " "); trimmed == "" {
				r.hasTitle = true
				// The dot of "M." is an abbreviation, not a sentence end;
				// undo the tokenizer's sentence-start flag.
				r.sentenceStart = false
			}
		}

		// Detector 2: legal suffix. Two shapes:
		//  (a) the suffix token joined the run itself ("Wagner GmbH",
		//      "Bidco Sàrl": GmbH/Sàrl are capitalised words), or
		//  (b) the suffix follows the run un-absorbed ("Alpine Trust
		//      S.A.": single dotted letters never join runs).
		for _, suffix := range smartLegalSuffixes {
			if strings.HasSuffix(r.text, " "+suffix) {
				r.hasSuffix = true
				break
			}
		}
		if !r.hasSuffix {
			rest := text[r.end:]
			trimmed := strings.TrimLeft(rest, " ")
			pad := len(rest) - len(trimmed)
			for _, suffix := range smartLegalSuffixes {
				if strings.HasPrefix(trimmed, suffix) && suffixBoundaryOK(trimmed, suffix) {
					r.end = r.end + pad + len(suffix)
					r.text = text[r.start:r.end]
					r.hasSuffix = true
					break
				}
			}
		}

		// Very short candidates are noise ("Al", "Le"), and a run that IS
		// a bare legal form ("GmbH" discussed as a concept) is not a name.
		if len([]rune(r.text)) >= 3 && !isBareSuffix(r.text) {
			runs = append(runs, r)

			// Sentence-start runs often glue a grammar-capitalised word
			// onto a real name ("Later Marie Duval called"). Emit the
			// sub-run without the first word too, so "Marie Duval" still
			// counts; the frequency and sentence rules weed out whichever
			// grouping is noise. Suffix runs are already whole company
			// names and produce no sub-run.
			if r.sentenceStart && !r.hasSuffix && !r.hasTitle && last > i {
				sub := smartRun{
					text:          text[tokens[i+1].start:r.end],
					start:         tokens[i+1].start,
					end:           r.end,
					sentenceStart: false,
					words:         significantWords(text[tokens[i+1].start:r.end]),
				}
				if len([]rune(sub.text)) >= 3 && !isBareSuffix(sub.text) {
					runs = append(runs, sub)
				}
			}
		}
		i = j
	}
	return runs
}

// isBareSuffix reports whether the whole run text is just a legal form
// from the gazetteer (never a candidate on its own).
func isBareSuffix(s string) bool {
	for _, suffix := range smartLegalSuffixes {
		if s == suffix {
			return true
		}
	}
	return false
}

// suffixBoundaryOK verifies the legal suffix is not a prefix of a longer
// word ("SAS" must not match inside "SASU"... which is itself a form, but
// the boundary check keeps the table honest).
func suffixBoundaryOK(s, suffix string) bool {
	rest := s[len(suffix):]
	if rest == "" {
		return true
	}
	r := []rune(rest)[0]
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// token is one word with position metadata.
type token struct {
	text          string
	start, end    int
	sentenceStart bool
	adjacent      bool // separated from the previous token by spaces only
}

// tokenize splits text into letter/hyphen/apostrophe words with byte
// offsets and sentence-start flags. Unicode-aware (French accents).
func tokenize(text string) []token {
	var tokens []token
	sentenceStart := true
	wordStart := -1
	lastEnd := 0

	flush := func(end int) {
		if wordStart < 0 {
			return
		}
		adjacent := true
		for _, r := range text[lastEnd:wordStart] {
			if r != ' ' {
				adjacent = false
				break
			}
		}
		tokens = append(tokens, token{
			text:          text[wordStart:end],
			start:         wordStart,
			end:           end,
			sentenceStart: sentenceStart,
			adjacent:      adjacent,
		})
		sentenceStart = false
		lastEnd = end
		wordStart = -1
	}

	for i, r := range text {
		isWord := unicode.IsLetter(r) || r == '-' || r == '\'' || r == '’'
		if isWord {
			if wordStart < 0 {
				wordStart = i
			}
			continue
		}
		flush(i)
		// Sentence boundaries: ., !, ?, newline. A '.' only ends a
		// sentence when followed by whitespace (keeps "S.A." inside runs).
		switch r {
		case '\n', '!', '?':
			sentenceStart = true
		case '.', ':':
			sentenceStart = true
		}
	}
	flush(len(text))
	return tokens
}

// isCapWord reports whether a token looks like a capitalised name word:
// first rune uppercase, at least 2 runes, and any hyphenated part also
// capitalised ("Jean-Pierre" yes, "Jean-pierre" still yes — people type
// that; "JEAN" yes for shouting headers).
func isCapWord(s string) bool {
	rs := []rune(strings.Trim(s, "-'’"))
	if len(rs) < 2 {
		return false
	}
	return unicode.IsUpper(rs[0])
}

// significantWords counts the non-particle words of a run (used by the
// multi-word qualification rule).
func significantWords(s string) int {
	n := 0
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '-' }) {
		if w != "" && !smartParticles[strings.ToLower(w)] {
			n++
		}
	}
	return n
}

// contextSnippet returns ±contextRadius runes around [start,end),
// whitespace-normalised, for the review UI.
func contextSnippet(text string, start, end int) string {
	from := start
	for i := 0; i < contextRadius && from > 0; i++ {
		from--
		for from > 0 && (text[from]&0xC0) == 0x80 {
			from--
		}
	}
	to := end
	for i := 0; i < contextRadius && to < len(text); i++ {
		to++
		for to < len(text) && (text[to]&0xC0) == 0x80 {
			to++
		}
	}
	return strings.Join(strings.Fields(text[from:to]), " ")
}
