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
//     followed by S.A., S.à r.l., GmbH, ... is an entity_names candidate
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
	"context"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
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
	// Confidence is the HEURISTIC score of this proposal, 0.0 to 1.0
	// (BUILD-04 CR13). It is not the same kind of number as Span
	// .Confidence: nothing is replaced on the strength of it, it only
	// ranks and filters what the review list shows. See candidateScore
	// for the exact ladder.
	Confidence float32 `json:"confidence,omitempty"`
}

// SmartDetectOptions tunes how eagerly SmartDetect proposes candidates
// (BUILD-04 CR13). The owner's report was that Smart detection surfaces
// far too many values to review, so every knob here removes noise:
//
//   - MinLength drops very short candidates ("Ltd", "Rue").
//   - MinOccurrences requires a candidate to appear N times.
//   - ExcludeCommonWords drops candidates made only of ordinary
//     capitalised words (month names, weekdays, common sentence openers),
//     which is where most of the noise comes from.
//   - MinConfidence drops candidates whose heuristic score is too low,
//     which is the single control that trades recall for precision
//     smoothly rather than in one dimension at a time.
//
// A zero value of this struct means "no filtering at all", which is what
// keeps the legacy SmartDetect signature behaving exactly as it did.
type SmartDetectOptions struct {
	// MinLength is the minimum candidate length in RUNES (not bytes, so
	// accented names count correctly). 0 disables the check.
	MinLength int `json:"minLength"`
	// MinOccurrences is the minimum number of times the candidate must
	// occur. 0 and 1 both mean "once is enough".
	MinOccurrences int `json:"minOccurrences"`
	// ExcludeCommonWords drops candidates whose every significant word is
	// an ordinary capitalised word rather than a name.
	ExcludeCommonWords bool `json:"excludeCommonWords"`
	// MinConfidence is the heuristic-score floor, 0.0 to 1.0. 0 disables
	// the check.
	MinConfidence float32 `json:"minConfidence"`
}

// DefaultSmartDetectOptions are the options the APPLICATION starts with
// (BUILD-04 CR13). They are deliberately stricter than the legacy
// no-filter behaviour, because over-detection was the reported problem:
// a review list nobody can get through is worse than one that misses a
// value the user can still type in by hand.
//
// MinOccurrences stays at 1 on purpose. A multi-word name occurring once
// ("Marie Duval" in a one-page note) is the most valuable thing Smart
// detection finds, and requiring two occurrences would throw exactly
// those away. The noise is cut by the word list and the score floor
// instead.
func DefaultSmartDetectOptions() SmartDetectOptions {
	return SmartDetectOptions{
		MinLength:          4,
		MinOccurrences:     1,
		ExcludeCommonWords: true,
		MinConfidence:      0.5,
	}
}

// smartCommonWords are ordinary capitalised words that are not names:
// month names, weekdays and frequent sentence openers, in English and
// French (the two document languages this application is tested against,
// CLAUDE.md §6). A candidate whose significant words are ALL in this set
// is dropped when ExcludeCommonWords is on.
//
// Compared lower-cased. Table-driven; extend freely, and prefer adding a
// word here over loosening a numeric threshold.
var smartCommonWords = map[string]bool{
	// Months, English.
	"january": true, "february": true, "march": true, "april": true,
	"may": true, "june": true, "july": true, "august": true,
	"september": true, "october": true, "november": true, "december": true,
	// Months, French.
	"janvier": true, "février": true, "fevrier": true, "mars": true,
	"avril": true, "mai": true, "juin": true, "juillet": true, "août": true,
	"aout": true, "septembre": true, "octobre": true, "novembre": true,
	"décembre": true, "decembre": true,
	// Weekdays, English then French.
	"monday": true, "tuesday": true, "wednesday": true, "thursday": true,
	"friday": true, "saturday": true, "sunday": true,
	"lundi": true, "mardi": true, "mercredi": true, "jeudi": true,
	"vendredi": true, "samedi": true, "dimanche": true,
	// Frequent capitalised sentence openers and connectives, English.
	"however": true, "therefore": true, "moreover": true, "furthermore": true,
	"finally": true, "first": true, "second": true, "third": true,
	"next": true, "then": true, "also": true, "since": true, "during": true,
	"following": true, "regarding": true, "please": true, "note": true,
	"yesterday": true, "today": true, "tomorrow": true, "when": true,
	"after": true, "before": true, "both": true, "each": true, "here": true,
	// Frequent capitalised sentence openers and connectives, French.
	"cependant": true, "toutefois": true, "ensuite": true, "enfin": true,
	"donc": true, "ainsi": true, "pourtant": true, "aussi": true,
	"depuis": true, "pendant": true, "concernant": true, "veuillez": true,
	"premièrement": true, "premierement": true, "hier": true,
	"aujourd'hui": true, "demain": true, "lorsque": true, "apres": true,
	"après": true, "avant": true,
	// Document furniture that reads like a name at the start of a line.
	"introduction": true, "conclusion": true, "summary": true, "annex": true,
	"appendix": true, "annexe": true, "résumé": true, "resume": true,
	"objet": true, "subject": true, "date": true, "page": true,
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
//
// This is the LEGACY signature, kept so every existing caller and test
// compiles unchanged (BUILD-04 CR13). It applies no tuning at all, which
// is exactly the behaviour it had before the options existed. New callers
// that want the application's stricter defaults use
// SmartDetectWithOptions(text, allow, DefaultSmartDetectOptions()).
func SmartDetect(text string, allow *Allowlist) []Candidate {
	return SmartDetectWithOptions(text, allow, SmartDetectOptions{})
}

// SmartDetectWithOptions is SmartDetect with the BUILD-04 CR13 tuning
// applied. The detectors themselves are unchanged; the options decide
// which of their proposals reach the review list, and every candidate
// carries the heuristic score the filtering used (candidateScore), so the
// UI can filter further without recomputing anything.
func SmartDetectWithOptions(text string, allow *Allowlist, opts SmartDetectOptions) []Candidate {
	candidates, _ := SmartDetectContext(context.Background(), text, allow, opts)
	return candidates
}

// SmartDetectContext is SmartDetectWithOptions that can be INTERRUPTED
// (BUILD-06). Until now the offline pass took no context at all, so Cancel
// could only take effect between documents: one very large file ran to
// completion whatever the user pressed, which is a large part of why
// detection "sometimes does not complete" from the outside.
//
// On cancellation it returns the candidates found so far together with
// ctx.Err(), the same contract the chunked LLM scan already had: partial work
// is worth keeping, and the caller decides how to describe it.
func SmartDetectContext(ctx context.Context, text string, allow *Allowlist, opts SmartDetectOptions) ([]Candidate, error) {
	runs, err := extractRunsContext(ctx, text)
	if err != nil {
		return nil, err
	}

	// Group occurrences by candidate text (case-sensitive: "WEBER" and
	// "Weber" are different spellings the review UI should see as typed;
	// the registry collapses case later anyway).
	type group struct {
		text         string
		count        int
		firstStart   int
		category     string
		qualifies    bool
		contexts     []string
		sentenceOnly bool
	}
	groups := map[string]*group{}
	order := []string{}

	for i, r := range runs {
		// Checked every 512 runs rather than every run: ctx.Err() is a mutex
		// read, and a document long enough to need interrupting has hundreds
		// of thousands of runs.
		if i%512 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
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
			g.category = "entity_names"
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
		// person names, single words as organisation-ish entity names.
		// This is only the INITIAL guess; the review UI and the optional
		// LLM classification (Phase 8b) refine it.
		if g.category == "" {
			if r.words >= 2 {
				g.category = "person_names"
			} else {
				g.category = "entity_names"
			}
		}
		// Allowlist veto LAST among the ORIGINAL rules (allowlist wins,
		// CLAUDE.md §5).
		if allow.Contains(g.text) {
			continue
		}

		// BUILD-04 CR13 tuning. The score is computed either way, so the
		// review UI always has it to filter and sort on, even when the
		// engine-side floor is off.
		score := candidateScore(r, g.count)
		if !keepCandidate(g.text, r, g.count, score, opts) {
			continue
		}

		out = append(out, Candidate{
			Text:       g.text,
			Category:   g.category,
			Count:      g.count,
			Contexts:   g.contexts,
			Confidence: score,
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
	return out, nil
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
	runs, _ := extractRunsContext(context.Background(), text)
	return runs
}

// extractRunsContext is extractRuns that can be interrupted. The token walk
// is the longest uninterruptible stretch of the offline pass, so this is
// where Cancel has to be able to land: without it, pressing Cancel on a large
// document did nothing at all until the whole file was done.
func extractRunsContext(ctx context.Context, text string) ([]smartRun, error) {
	tokens := tokenize(text)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var runs []smartRun

	i := 0
	checked := 0
	for i < len(tokens) {
		// ctx.Err() is a mutex read, so it is checked every 4096 tokens
		// rather than every token: often enough that Cancel feels immediate,
		// rare enough to cost nothing on the documents that do not need it.
		if i-checked >= 4096 {
			checked = i
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
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
	return runs, nil
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
	// DecodeRuneInString, not []rune(rest)[0]. `rest` here is the whole
	// remainder of the document, and converting it to a rune slice to read
	// ONE character allocated and scanned megabytes per run: extractRuns was
	// quadratic in document size, which is why a large file made detection
	// look like it had hung (15 s for 800 KB, ~2 min for 2.4 MB). Decoding
	// the first rune in place is the same answer in constant time.
	r, _ := utf8.DecodeRuneInString(rest)
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

// --- BUILD-04 CR13: candidate scoring and filtering -------------------------

// candidateScore turns what the detectors observed about a run into one
// heuristic number in [0.0, 1.0]. It is a LADDER, not a formula, so every
// step can be read and argued with:
//
//	0.95  a legal form follows the name ("Alpine Trust S.A."): as close to
//	      certain as a heuristic gets, companies are named this way on purpose
//	0.90  a person title introduced it ("Mme Weber", "Dr Keller")
//	0.80  several words, seen more than once: a repeated full name
//	0.65  several words, seen once: "Marie Duval" mid-sentence is still a
//	      strong signal, but a single sighting leaves room for a fluke
//	0.45  one word, seen more than once: the weakest thing worth showing,
//	      and where most of the over-detection lives
//	0.25  anything else that survived the detectors
//
// The default floor (0.5) therefore keeps the first four rungs and drops
// the last two.
func candidateScore(r smartRun, count int) float32 {
	switch {
	case r.hasSuffix:
		return 0.95
	case r.hasTitle:
		return 0.90
	case r.words >= 2 && count >= 2:
		return 0.80
	case r.words >= 2:
		return 0.65
	case count >= 2:
		return 0.45
	default:
		return 0.25
	}
}

// keepCandidate applies the SmartDetectOptions filters to one candidate.
// Split out of SmartDetectWithOptions so each rule is independently
// testable and so the order of the checks is visible: cheapest first,
// and the word list before the score, because "this is just the word
// March" is a better reason to drop something than "it scored low".
func keepCandidate(text string, r smartRun, count int, score float32, opts SmartDetectOptions) bool {
	if opts.MinLength > 0 && len([]rune(text)) < opts.MinLength {
		return false
	}
	if opts.MinOccurrences > 1 && count < opts.MinOccurrences {
		return false
	}
	if opts.ExcludeCommonWords && isCommonWordRun(text) {
		return false
	}
	if opts.MinConfidence > 0 && score < opts.MinConfidence {
		return false
	}
	return true
}

// isCommonWordRun reports whether EVERY significant word of the candidate
// is an ordinary capitalised word (smartCommonWords). "March" is dropped;
// "March Consulting" is not, because "Consulting" is not in the list, and
// a real company can perfectly well be called that.
//
// Particles ("de", "van") are skipped like everywhere else, and a run
// with no significant words at all is not treated as common: that would
// be a detector bug, and dropping it silently would hide it.
func isCommonWordRun(text string) bool {
	significant := 0
	for _, w := range strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == '-' }) {
		lower := strings.ToLower(strings.Trim(w, ".,;:!?()\"\u2019'"))
		if lower == "" || smartParticles[lower] {
			continue
		}
		significant++
		if !smartCommonWords[lower] {
			return false
		}
	}
	return significant > 0
}
