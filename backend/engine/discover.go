// engine/discover.go — the Smart detection tier: a
// fully OFFLINE heuristic discovery pass that always works without
// Ollama. It proposes candidate entities from how names are written, for
// the review screen; nothing it finds is ever replaced without explicit
// user acceptance.
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
//  5. Email-derived names: the local-part of an address in the document
//     ("johannes.borch@pwc.lu") names a person, so a matching capitalised run
//     is routed to person_names with high confidence. Needs no name list; it
//     is the strongest offline signal on real correspondence.
//
// Precision is governed by a NEGATIVE gazetteer (smartCommonWords): a run
// whose every significant word is ordinary business/role/document vocabulary
// ("Revenue Management", "General Terms of Sale") is dropped. Growing that
// table is the preferred way to cut noise, never loosening a numeric threshold.
// A run that only ever appears in a postal-address context (a street cue like
// "rue"/"place" beside or inside it) is likewise dropped as a street or venue
// name, since there is no location category to route it to (CLAUDE.md §5).
//
// The allowlist veto is applied LAST — allowlist wins, as everywhere
// (CLAUDE.md §5).
//
// UI-agnostic and I/O-free per CLAUDE.md §4: text in, candidates out.
package engine

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Candidate is one Smart-detection proposal for the review UI (and for
// LLM span classification).
type Candidate struct {
	Text     string `json:"text"`
	Category string `json:"category"`
	// Count is how many times the exact text occurs in the document.
	Count int `json:"count"`
	// Contexts holds up to 3 snippets of ±60 runes around occurrences,
	// for the review UI and for LLM classification prompts.
	Contexts []string `json:"contexts,omitempty"`
	// Confidence is the HEURISTIC score of this proposal, 0.0 to 1.0
	// It is not the same kind of number as Span
	// .Confidence: nothing is replaced on the strength of it, it only
	// ranks and filters what the review list shows. See candidateScore
	// for the exact ladder.
	Confidence float32 `json:"confidence,omitempty"`
	// Variants are the longer spellings folded into this one
	// (FoldValueFamilies): "Coca-Cola company" under "Coca-Cola". Accepting the
	// candidate carries them across as the value's manual variants, so one
	// value with its spellings reaches the pipeline instead of two rivals, the
	// shorter of which would fire inside the longer.
	Variants []string `json:"variants,omitempty"`
}

// SmartDetectOptions tunes how eagerly SmartDetect proposes candidates
// The owner's report was that Smart detection surfaces
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
	// Strictness selects HOW MANY detectors a candidate must satisfy, a
	// lever orthogonal to MinConfidence (which is about how HIGH they must
	// score). "" and "balanced" are the default behaviour; "strict" emits
	// only structurally-anchored candidates (a legal suffix, a title cue, a
	// trademark, a matching email name or a code), trading recall for
	// precision; "lenient" additionally keeps the rare single-word single-
	// occurrence runs the frequency rule drops. Unknown values read as
	// balanced, so an older UI that never sets it is unaffected.
	Strictness string `json:"strictness,omitempty"`
}

// Strictness levels for SmartDetectOptions.Strictness. Kept as string
// constants (not an enum type) so they cross the Wails JSON boundary and a
// session file unchanged, and an empty string keeps meaning "balanced".
const (
	StrictnessLenient  = "lenient"
	StrictnessBalanced = "balanced"
	StrictnessStrict   = "strict"
)

// DefaultSmartDetectOptions are the options the APPLICATION starts with
// They are deliberately stricter than the legacy
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
		Strictness:         StrictnessBalanced,
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
	// Business / consulting process nouns. These are the dominant offline
	// noise class: a document like an engagement deck is full of Title-Case
	// phrases ("Revenue Management", "Financial Due Diligence") that read like
	// names but are ordinary vocabulary. isCommonWordRun drops a run only when
	// EVERY significant word is here, so a real name that merely CONTAINS one
	// ("March Consulting") still survives, and this list is safe to grow.
	// English then French.
	"development": true, "management": true, "strategy": true, "strategies": true,
	"assessment": true, "review": true, "transformation": true, "transformations": true,
	"transaction": true, "advisory": true, "advice": true, "marketing": true,
	"sales": true, "performance": true, "diagnostic": true, "logistics": true,
	"pricing": true, "revenue": true, "innovation": true, "capability": true,
	"valuation": true, "integration": true, "reporting": true, "audit": true,
	"assurance": true, "safety": true, "feasibility": true, "forecasting": true,
	"financing": true, "loyalty": true, "governance": true, "restructuring": true,
	"regulation": true, "economics": true, "stakeholders": true, "roadmap": true,
	"planning": true, "diligence": true, "contracting": true, "branding": true,
	"modelling": true, "scenario": true, "analysis": true, "benchmarks": true,
	"standards": true, "compliance": true, "procurement": true, "finance": true,
	"financial": true, "commercial": true, "operational": true, "corporate": true,
	"digital": true, "supply": true, "chain": true, "impact": true,
	"développement": true, "gestion": true, "stratégie": true, "évaluation": true,
	"revue": true, "conformité": true, "gouvernance": true, "réglementation": true,
	"financement": true, "faisabilité": true, "achats": true, "ventes": true,
	// Role / title nouns. A job title in Title Case ("Senior Manager",
	// "Director") is not a person's name; the person beside it is.
	"director": true, "directeur": true, "manager": true, "senior": true,
	"junior": true, "partner": true, "partners": true, "associate": true,
	"consultant": true, "analyst": true, "officer": true, "executive": true,
	"coordinator": true, "coordinateur": true, "administrator": true,
	"assistant": true, "specialist": true, "spécialiste": true, "supervisor": true,
	"intern": true, "trainee": true, "head": true, "lead": true, "chief": true,
	"architecture": true,
	// Email / letter / invoice furniture. Header labels and sign-offs are
	// capitalised at the start of a line and otherwise read like names.
	"from": true, "sent": true, "received": true, "regards": true,
	"sincerely": true, "best": true, "dear": true, "hello": true,
	"envoyé": true, "reçu": true, "cordialement": true, "salutations": true,
	"bonjour": true, "cher": true, "chère": true, "mobile": true,
	"phone": true, "fax": true, "email": true, "courriel": true,
	"tel": true, "attn": true, "invoice": true, "facture": true,
	"order": true, "commande": true, "reminder": true, "control": true,
	"terms": true, "conditions": true, "sale": true, "general": true,
	"edition": true, "notice": true, "price": true, "total": true,
	"amount": true, "quantity": true, "description": true, "name": true,
	"address": true, "adresse": true, "contact": true, "questions": true,
	"event": true, "events": true, "liability": true,
	"fraud": true, "organize": true,
	// Everyday HR / administrative nouns that appear Title-Cased in internal
	// mail ("Extra Holiday Buying", "Holiday Savings Account").
	"holiday": true, "holidays": true, "buying": true, "extra": true,
	"savings": true, "account": true, "overtime": true, "leave": true,
	"vacation": true, "request": true, "approval": true, "congé": true,
}

// smartConnectors are the tiny function words ("of", "the", "and") that can
// sit INSIDE a common-noun phrase ("General Terms of Sale", "Terms and
// Conditions"). isCommonWordRun skips them exactly like particles, so a phrase
// made only of connectors and common nouns still counts as all-common. They
// are NOT added to smartCommonWords because on their own they are not names
// either, and keeping them separate documents why they are skipped.
var smartConnectors = map[string]bool{
	"of": true, "the": true, "and": true, "for": true, "to": true,
	"in": true, "on": true, "vs": true, "et": true, "ou": true, "aux": true,
}

// streetCues are address words that mark a nearby capitalised run as part of a
// POSTAL ADDRESS rather than a person or company: a street name, a square, a
// town. Checked as the whole token immediately before a run ("rue Gerhard
// Mercator") or as a token INSIDE it ("Place de l'Hôtel de Ville"). English
// then French. Deliberately excludes ambiguous abbreviations (st, av, dr) that
// collide with "Saint", a title, or an initial. Compared accent-folded and
// lower-cased.
var streetCues = map[string]bool{
	"street": true, "road": true, "avenue": true, "boulevard": true,
	"lane": true, "square": true, "plaza": true, "place": true,
	"drive": true, "court": true, "terrace": true, "way": true,
	"rue": true, "impasse": true, "chemin": true, "route": true,
	"quai": true, "cours": true, "esplanade": true, "allee": true,
	"ville": true, "city": true, "town": true, "hotel": true, "hôtel": true,
}

// orgKeywordsCommon are organisation-indicating words that hold in ANY country,
// because English is the lingua franca of company naming: "Delta Group",
// "Helios Holdings", "Meridian Partners". They VOUCH a capitalised run as an
// entity_names candidate the same way a legal suffix does, but a hair less
// certainly (a legal form is registered, a keyword is convention), so they
// score just below one. Compared accent-folded and lower-cased.
//
// This is broader than the legalSuffixes gazetteer (entities.go), which lists
// only registered legal FORMS (Sàrl, GmbH): a keyword names the KIND of
// organisation, a suffix names its legal shell. Both mark a run as a company.
var orgKeywordsCommon = map[string]bool{
	"group": true, "holdings": true, "holding": true, "partners": true,
	"partnership": true, "ventures": true, "associates": true,
	"corporation": true, "incorporated": true, "enterprises": true,
	"enterprise": true, "industries": true, "technologies": true,
	"laboratories": true, "pharmaceuticals": true, "systems": true,
	"solutions": true, "consulting": true, "international": true,
	"worldwide": true, "trust": true, "bank": true, "capital": true,
}

// orgKeywordsByLanguage adds the organisation words specific to a language, so
// "PwC Société coopérative" reads as a company when the document country uses
// French. Keyed by a language tag, mapped to countries by countryLanguages.
var orgKeywordsByLanguage = map[string]map[string]bool{
	"fr": {
		"groupe": true, "société": true, "societe": true, "sociétés": true,
		"societes": true, "coopérative": true, "cooperative": true,
		"compagnie": true, "banque": true, "assurances": true, "assurance": true,
		"conseil": true, "conseils": true, "associés": true, "associes": true,
		"entreprise": true, "entreprises": true, "établissements": true,
		"etablissements": true, "mutuelle": true, "fédération": true,
		"federation": true, "fiduciaire": true,
	},
	"de": {
		"gesellschaft": true, "genossenschaft": true, "handelsgesellschaft": true,
		"verein": true, "versicherung": true, "versicherungen": true,
		"stiftung": true, "werke": true, "industrie": true, "beteiligungen": true,
		"gruppe": true, "unternehmen": true, "bank": true,
	},
	"es": {
		"sociedad": true, "compañía": true, "compania": true, "empresa": true,
		"banco": true, "seguros": true, "cooperativa": true, "grupo": true,
		"fundación": true, "fundacion": true, "asociación": true, "asociacion": true,
	},
}

// countryLanguages maps a document country to the languages whose organisation
// keywords apply. Luxembourg is trilingual (French, German, plus English as the
// common set), which is why a Luxembourg document must recognise both "Société
// coopérative" and "Gesellschaft". "" (no country chosen) falls back to the
// common English set only.
var countryLanguages = map[string][]string{
	CountryLU: {"fr", "de"},
	CountryFR: {"fr"},
	CountryDE: {"de"},
	CountryES: {"es"},
	CountryUK: {},
}

// orgKeywordApplies reports whether a folded, lower-cased word is an
// organisation keyword for the given document country. The common set always
// applies; the language sets apply per countryLanguages.
func orgKeywordApplies(word, country string) bool {
	if orgKeywordsCommon[word] {
		return true
	}
	for _, lang := range countryLanguages[country] {
		if orgKeywordsByLanguage[lang][word] {
			return true
		}
	}
	return false
}

// runHasOrgKeyword reports whether a capitalised run reads as an organisation:
// it carries an org keyword AND at least one OTHER significant word (so a bare
// "Group" or "Société" on its own is not an org, but "Delta Group" and "PwC
// Société" are). Tokens are split on spaces and hyphens and compared
// accent-folded.
func runHasOrgKeyword(runText, country string) bool {
	hasKeyword := false
	hasOther := false
	for _, w := range strings.FieldsFunc(runText, func(r rune) bool { return r == ' ' || r == '-' }) {
		folded := foldAccentsLower(strings.Trim(w, ".,;:!?()\"'’"))
		if folded == "" || smartParticles[folded] || smartConnectors[folded] {
			continue
		}
		if orgKeywordApplies(folded, country) {
			hasKeyword = true
		} else {
			hasOther = true
		}
	}
	return hasKeyword && hasOther
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

// smartLeadingStopwords are articles/pronouns/salutations that must not OPEN a
// run: "The CSSF" is the term "CSSF", not an entity called "The CSSF", and
// "Hello Oscar" is the person "Oscar", not "Hello Oscar". Table-driven; extend
// freely.
var smartLeadingStopwords = map[string]bool{
	"The": true, "A": true, "An": true, "This": true, "That": true,
	"These": true, "Those": true, "Our": true, "We": true, "They": true,
	"Le": true, "La": true, "Les": true, "Un": true, "Une": true,
	"Ce": true, "Cette": true, "Ces": true, "Nos": true, "Notre": true,
	// Salutations and sign-offs, so a greeting never glues onto the name it
	// addresses ("Hello Oscar", "Cher Thierry", "Best regards Marie").
	"Hello": true, "Hi": true, "Dear": true, "Hey": true,
	"Bonjour": true, "Bonsoir": true, "Salut": true, "Cher": true,
	"Chère": true, "Chers": true, "Best": true, "Regards": true,
}

// productHeadNouns mark a capitalised run as a PRODUCT rather than a company:
// "Meridian Platform", "Helios Suite". The noun may be the run's own last word
// or the word immediately after it.
var productHeadNouns = map[string]bool{
	"platform": true, "plateforme": true, "suite": true, "edition": true,
	"sdk": true, "toolkit": true, "framework": true, "engine": true,
	"module": true, "app": true, "application": true, "software": true,
	"logiciel": true, "solution": true, "portal": true, "portail": true,
}

// trademarkMarks follow a product name and almost nothing else. A mark is the
// highest-precision offline signal in this file and costs one string compare.
var trademarkMarks = []string{"\u2122", "\u00ae", "\u2120"}

// emailPersonScore is the confidence given to a capitalised run that matches a
// name derived from an email address in the same document. It sits at the
// person-title / trademark rung (0.90): a name written next to its own address
// ("Johannes Borch <johannes.borch@pwc.lu>") is nearly as certain a person as
// a title cue, and far more certain than a bare multi-word run (0.65).
const emailPersonScore float32 = 0.90

// emailLocalRe captures the LOCAL-PART of an email address (the text before
// the @). The address itself is hard PII that pass 1 already removes; what the
// offline discovery pass wants from it is the person's NAME, which is spelt
// out in the local-part of a corporate address ("prenom.nom@societe.tld").
// Deliberately the same address shape as pii.go's email rule, with the
// local-part captured.
var emailLocalRe = regexp.MustCompile(`([A-Za-z0-9._%+\-]+)@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// nonNameMailboxes are functional/role local-parts that are NOT a person's
// name. A local-part equal to one of these injects no name, so "info@acme.com"
// and "no-reply@acme.com" do not invent a person called "Info" or "No Reply".
var nonNameMailboxes = map[string]bool{
	"info": true, "contact": true, "noreply": true, "no-reply": true,
	"admin": true, "sales": true, "support": true, "hello": true, "team": true,
	"office": true, "help": true, "service": true, "services": true,
	"billing": true, "hr": true, "jobs": true, "marketing": true, "mail": true,
	"newsletter": true, "postmaster": true, "webmaster": true, "contactus": true,
}

// deriveEmailNames builds a per-document gazetteer of person names from the
// local-part of every email address in the text. It needs no name list at all:
// the name is written next to its own address, which is why this is the single
// highest-value offline signal on real correspondence.
//
//	"johannes.borch@pwc.lu"  ->  "johannes borch", "johannes", "borch"
//
// The bare forename/surname are indexed too, so a later mention of just
// "Borch" is also recognised as a person. A forename that is itself a common
// word (a month, "May") is NOT indexed alone, so a stray "May" is not turned
// into a person; the full "May Smith" join still is. Accent-folded and
// lower-cased so it can match the accented spelling in the body ("José" vs the
// ASCII "jose" in the address).
func deriveEmailNames(text string) map[string]bool {
	out := map[string]bool{}
	for _, m := range emailLocalRe.FindAllStringSubmatch(text, -1) {
		local := strings.ToLower(m[1])
		if nonNameMailboxes[local] {
			continue
		}
		parts := strings.FieldsFunc(local, func(r rune) bool {
			return r == '.' || r == '_' || r == '-' || r == '+'
		})
		var words []string
		for _, p := range parts {
			if len([]rune(p)) >= 2 && !isAllDigits(p) && !nonNameMailboxes[p] {
				words = append(words, foldAccentsLower(p))
			}
		}
		// A single token is a handle ("oscarl"), not a forename+surname; only
		// a run of two or more reads as a real name.
		if len(words) < 2 {
			continue
		}
		out[strings.Join(words, " ")] = true
		if !smartCommonWords[words[0]] {
			out[words[0]] = true
		}
		if last := words[len(words)-1]; !smartCommonWords[last] {
			out[last] = true
		}
	}
	return out
}

// emailNameMatch reports whether a capitalised run is (or contains, or is
// contained by) a name derived from an email address in the same document.
// Whole-word comparison after accent-folding, so "Liber" matches inside the
// "oscar liber" gazetteer entry but "end" never matches inside "mendonca".
func emailNameMatch(set map[string]bool, runText string) bool {
	if len(set) == 0 {
		return false
	}
	ft := foldAccentsLower(runText)
	if set[ft] {
		return true
	}
	for key := range set {
		if wholeWordContains(key, ft) || wholeWordContains(ft, key) {
			return true
		}
	}
	return false
}

// wholeWordContains reports whether needle's whole-word token sequence occurs
// contiguously inside hay's. Both are space-separated, already folded.
func wholeWordContains(hay, needle string) bool {
	h, n := strings.Fields(hay), strings.Fields(needle)
	if len(n) == 0 || len(n) > len(h) {
		return false
	}
	for i := 0; i+len(n) <= len(h); i++ {
		ok := true
		for j := range n {
			if h[i+j] != n[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// isAllDigits reports whether the token is only ASCII digits (a numeric
// local-part fragment such as the "42" in "john42"), which is never a name.
func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// foldAccentsLower lower-cases a string and folds the Latin accents that
// appear in EN/FR/PT names to ASCII, then collapses internal whitespace. A
// table (not golang.org/x/text) keeps this dependency-free per CLAUDE.md §6;
// it covers the accented letters that occur in names, which is all the
// email-name match needs.
func foldAccentsLower(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		switch r {
		case 'á', 'à', 'â', 'ä', 'ã', 'å', 'ā':
			b.WriteRune('a')
		case 'é', 'è', 'ê', 'ë', 'ē':
			b.WriteRune('e')
		case 'í', 'ì', 'î', 'ï', 'ī':
			b.WriteRune('i')
		case 'ó', 'ò', 'ô', 'ö', 'õ', 'ō':
			b.WriteRune('o')
		case 'ú', 'ù', 'û', 'ü', 'ū':
			b.WriteRune('u')
		case 'ç':
			b.WriteRune('c')
		case 'ñ':
			b.WriteRune('n')
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// contextRadius is the snippet half-width around an occurrence (runes).
const contextRadius = 60

// maxContexts caps how many snippets one candidate carries.
const maxContexts = 3

// smartRun is one occurrence of a capitalised run during extraction.
type smartRun struct {
	text           string
	start, end     int  // byte offsets in the source text
	sentenceStart  bool // the run begins a sentence
	hasSuffix      bool // a legal suffix follows (and is included)
	hasTitle       bool // a person-title cue opened the run
	hasTrademark   bool // a trademark mark follows the run
	hasProduct     bool // a product head noun is in or beside the run
	addressContext bool // a street cue sits beside or inside the run (an address)
	orgKeyword     bool // an organisation keyword vouches the run as a company
	words          int  // significant (non-particle) word count
}

// SmartDetectWithOptions is SmartDetect with the tuning
// applied. The detectors themselves are unchanged; the options decide
// which of their proposals reach the review list, and every candidate
// carries the heuristic score the filtering used (candidateScore), so the
// UI can filter further without recomputing anything.
//
// It runs country-agnostic (the organisation-keyword signal uses only the
// common English set); the App layer, which knows the document country, calls
// SmartDetectContext directly with it.
func SmartDetectWithOptions(text string, allow *Allowlist, opts SmartDetectOptions) []Candidate {
	// context.Background() cannot be cancelled, so SmartDetectContext never
	// returns an error here; the check satisfies the no-blank-error rule and
	// keeps the wrapper honest if the contract ever changes.
	candidates, err := SmartDetectContext(context.Background(), text, allow, opts, "")
	if err != nil {
		return nil
	}
	return candidates
}

// SmartDetectContext is SmartDetectWithOptions that can be INTERRUPTED
// Until now the offline pass took no context at all, so Cancel
// could only take effect between documents: one very large file ran to
// completion whatever the user pressed, which is a large part of why
// detection "sometimes does not complete" from the outside.
//
// country scopes the organisation-keyword signal (countryLanguages); "" leaves
// only the common English keyword set active.
//
// On cancellation it returns the candidates found so far together with
// ctx.Err(), the same contract the chunked LLM scan already had: partial work
// is worth keeping, and the caller decides how to describe it.
func SmartDetectContext(ctx context.Context, text string, allow *Allowlist, opts SmartDetectOptions, country string) ([]Candidate, error) {
	runs, err := extractRunsContext(ctx, text, country)
	if err != nil {
		return nil, err
	}

	// Per-document email-name gazetteer (one regex pass). A capitalised run
	// that matches a name spelt out in an address is a person with high
	// confidence, whatever the frequency/word-count heuristics would have said.
	emailNames := deriveEmailNames(text)

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
		addressOnly  bool
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
			g = &group{text: r.text, firstStart: r.start, sentenceOnly: true, addressOnly: true}
			groups[r.text] = g
			order = append(order, r.text)
		}
		g.count++
		if !r.sentenceStart {
			g.sentenceOnly = false
		}
		if !r.addressContext {
			g.addressOnly = false
		}
		// Category priority, strongest cue first: a legal form names a
		// company, a trademark names a product, a title names a person, an
		// organisation keyword also names a company but a hair less certainly
		// than a legal form, and a product head noun is the weakest and only
		// fills a gap.
		switch {
		case r.hasSuffix:
			g.category = CatEntityNames
		case r.hasTrademark:
			g.category = CatProductNames
		case r.hasTitle && g.category == "":
			g.category = CatPersonNames
		case r.orgKeyword && g.category == "":
			g.category = CatEntityNames
		case r.hasProduct && g.category == "":
			g.category = CatProductNames
		}
		if r.hasSuffix || r.hasTitle || r.hasTrademark || r.orgKeyword {
			g.qualifies = true
		}
		if len(g.contexts) < maxContexts {
			g.contexts = append(g.contexts, contextSnippet(text, r.start, r.end))
		}
	}

	strictness := opts.Strictness
	var out []Candidate
	for _, key := range order {
		g := groups[key]
		r := firstRunFor(runs, key)

		// A run is VOUCHED when a structural signal stands behind it: a legal
		// suffix, a title cue or a trademark (g.qualifies), or a name spelt out
		// in an address in the same document (emailMatched). Vouched runs bypass
		// the noise heuristics below, exactly as a legal form always did.
		emailMatched := emailNameMatch(emailNames, g.text)
		vouched := g.qualifies || emailMatched

		// Sentence-start rule: a run seen ONLY at sentence starts needs a
		// second occurrence to qualify (kills "Ensuite", "Yesterday", ...)
		// unless a structural signal already vouches for it (a company opening
		// a sentence is still a company).
		if g.sentenceOnly && g.count < 2 && !vouched {
			continue
		}
		// Frequency rule: single-word single-occurrence runs without a
		// structural signal are dropped. Lenient strictness keeps them, for a
		// reviewer who would rather prune a long list than miss a rare mention;
		// they still carry a low score, so the confidence floor governs whether
		// they actually surface.
		if !vouched && g.count < 2 && r.words < 2 && strictness != StrictnessLenient {
			continue
		}
		// Address suppression: a run that only ever appears in a POSTAL-ADDRESS
		// context (a street cue like "rue"/"avenue"/"place" beside or inside it)
		// is a street or venue name, not a person or a company.
		// "2, rue Gerhard Mercator" must not propose "Gerhard Mercator" as
		// someone to anonymise. A vouched run overrides this: a real person can
		// sign above their own address. There is no location category among the
		// eight (CLAUDE.md §5), so an address value is dropped, not rerouted.
		if g.addressOnly && !vouched {
			continue
		}
		// Default category for unclassified runs: multi-word runs read as
		// person names, single words as organisation-ish entity names.
		// This is only the INITIAL guess; the review UI and the optional
		// LLM classification refine it.
		if g.category == "" {
			if r.words >= 2 {
				g.category = CatPersonNames
			} else {
				g.category = CatEntityNames
			}
		}
		// Allowlist veto LAST among the ORIGINAL rules (allowlist wins,
		// CLAUDE.md §5).
		if allow.Contains(g.text) {
			continue
		}

		// The score is computed either way, so the review UI always has it to
		// filter and sort on, even when the engine-side floor is off.
		score := candidateScore(r, g.count)

		// Email-name signal: a run named by an address is a person. It
		// overrides the category UNLESS a legal suffix or trademark already
		// vouched for it (a company whose name appears in an address stays a
		// company), and it lifts the score so a real person clears the
		// confidence floor instead of being filtered as a bare multi-word guess.
		if emailMatched {
			if !g.qualifies {
				g.category = CatPersonNames
			}
			if score < emailPersonScore {
				score = emailPersonScore
			}
		}

		// Strict strictness: emit ONLY structurally-vouched candidates, so a
		// bare capitalised run or a lone product head noun is dropped however
		// it scored. This is the high-precision end of the lever, orthogonal to
		// the numeric confidence floor: strictness is about WHICH detectors are
		// trusted, the floor is about how high they must score.
		if strictness == StrictnessStrict && !vouched {
			continue
		}
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

	sortCandidates(out, func(text string) int { return groups[text].firstStart })

	// The code detector is a second scanner over the same text (codes.go). It
	// runs here rather than at the call site so every caller of the offline
	// route gets the same set, and so the tuning options apply to both scanners
	// through one filter.
	codes, codeStarts := detectCodes(text, allow)
	for _, c := range codes {
		if keepCandidate(c.Text, smartRun{words: 1}, c.Count, c.Confidence, opts) {
			out = append(out, c)
		}
	}
	sortCandidates(out, func(text string) int {
		if g, ok := groups[text]; ok {
			return g.firstStart
		}
		return codeStarts[text]
	})
	return out, nil
}

// sortCandidates imposes the review list's deterministic ranking: the most
// frequent value first, ties broken by where it first appears in the document.
// Shared by every offline detector so two lists cannot be ordered differently.
//
// @param candidates the list, sorted in place
// @param firstStart the byte offset of a value's first occurrence
func sortCandidates(candidates []Candidate, firstStart func(text string) int) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Count != candidates[j].Count {
			return candidates[i].Count > candidates[j].Count
		}
		return firstStart(candidates[i].Text) < firstStart(candidates[j].Text)
	})
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

// extractRunsContext scans the text once and returns every capitalised-run
// occurrence with its metadata. The token walk is the longest uninterruptible
// stretch of the offline pass, so it is where cancellation has to be able to
// land: on a large document nothing else would notice for minutes.
func extractRunsContext(ctx context.Context, text, country string) ([]smartRun, error) {
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
		for _, suffix := range legalSuffixes {
			if strings.HasSuffix(r.text, " "+suffix) {
				r.hasSuffix = true
				break
			}
		}
		if !r.hasSuffix {
			rest := text[r.end:]
			trimmed := strings.TrimLeft(rest, " ")
			pad := len(rest) - len(trimmed)
			for _, suffix := range legalSuffixes {
				if strings.HasPrefix(trimmed, suffix) && suffixBoundaryOK(trimmed, suffix) {
					r.end = r.end + pad + len(suffix)
					r.text = text[r.start:r.end]
					r.hasSuffix = true
					break
				}
			}
		}

		// Detector 3: product. A trademark mark is nearly free and nearly
		// certain; a head noun is weaker and says only "this reads like a
		// product". Everything else about a product name is world knowledge,
		// which is what the AI route is for, and the frontend label says so.
		r.hasTrademark = followedByTrademark(text, r.end)
		r.hasProduct = r.hasTrademark || hasProductHeadNoun(text, r)

		// Address context: a street cue immediately before the run
		// ("rue Gerhard Mercator") or as a token inside it ("Place de la
		// Gare"). Marks the run as part of a postal address so it is not
		// proposed as a person or company (unless a stronger cue vouches).
		r.addressContext = runHasStreetCue(r.text)
		if !r.addressContext && i > 0 {
			prev := foldAccentsLower(strings.Trim(tokens[i-1].text, ".,;:"))
			if streetCues[prev] {
				r.addressContext = true
			}
		}

		// Organisation keyword (country-scoped): a capitalised word inside the
		// run names the KIND of organisation ("Delta Group", "PwC Société"),
		// vouching it as a company. A legal suffix already covers registered
		// forms, so this only fires when there was no suffix. When the run is an
		// organisation, a single lowercase org keyword immediately after it is
		// absorbed to complete the name boundary ("PwC Société" + "coopérative"
		// → "PwC Société coopérative"); the address heuristic is deliberately
		// skipped for a company (a company legitimately sits at an address).
		if !r.hasSuffix && runHasOrgKeyword(r.text, country) {
			r.orgKeyword = true
			r.addressContext = false
			if trailing := trailingOrgKeyword(text, r.end, country); trailing > 0 {
				r.end = trailing
				r.text = text[r.start:r.end]
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
			// grouping is noise. Suffix and organisation runs are already whole
			// company names and produce no sub-run.
			if r.sentenceStart && !r.hasSuffix && !r.hasTitle && !r.orgKeyword && last > i {
				sub := smartRun{
					text:           text[tokens[i+1].start:r.end],
					start:          tokens[i+1].start,
					end:            r.end,
					sentenceStart:  false,
					addressContext: r.addressContext,
					words:          significantWords(text[tokens[i+1].start:r.end]),
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

// runHasStreetCue reports whether any whole token of the run is a street cue
// ("Place de la Gare" contains "Place"). Tokens are split on spaces, hyphens
// and apostrophes so "l'Hôtel" contributes "hotel", and compared accent-folded.
func runHasStreetCue(runText string) bool {
	for _, w := range strings.FieldsFunc(runText, func(r rune) bool {
		return r == ' ' || r == '-' || r == '\'' || r == '\u2019'
	}) {
		if streetCues[foldAccentsLower(w)] {
			return true
		}
	}
	return false
}

// trailingOrgKeyword reports the byte offset just past a single LOWERCASE
// organisation keyword sitting immediately after a run ("PwC Société " +
// "coopérative"), or 0 if there is none. It completes an organisation name
// whose legal-ish tail is not capitalised, mirroring how the legal-suffix
// detector absorbs an un-capitalised suffix. Only ONE trailing word is taken,
// so it cannot swallow the rest of a sentence.
func trailingOrgKeyword(text string, end int, country string) int {
	rest := text[end:]
	trimmed := strings.TrimLeft(rest, " ")
	pad := len(rest) - len(trimmed)
	if pad == 0 || trimmed == "" {
		return 0 // must be separated by a space, and something must follow
	}
	// Take the leading run of word runes (letters, apostrophes, hyphens).
	word := ""
	for i, r := range trimmed {
		if unicode.IsLetter(r) || r == '\'' || r == '\u2019' || r == '-' {
			continue
		}
		word = trimmed[:i]
		break
	}
	if word == "" {
		word = trimmed
	}
	// Lowercase only: a capitalised keyword would already be part of the run.
	first, _ := utf8.DecodeRuneInString(word)
	if unicode.IsUpper(first) {
		return 0
	}
	if !orgKeywordApplies(foldAccentsLower(word), country) {
		return 0
	}
	return end + pad + len(word)
}

// isBareSuffix reports whether the whole run text is just a legal form
// from the gazetteer (never a candidate on its own).
func isBareSuffix(s string) bool {
	for _, suffix := range legalSuffixes {
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

// --- candidate scoring and filtering -------------------------

// candidateScore turns what the detectors observed about a run into one
// heuristic number in [0.0, 1.0]. It is a LADDER, not a formula, so every
// step can be read and argued with:
//
//	0.95 a legal form follows the name ("Alpine Trust S.A."): as close to
//	      certain as a heuristic gets, companies are named this way on purpose
//	0.90 a trademark mark follows it, or a person title introduced it
//	      ("Meridian Suite™", "Mme Weber", "Dr Keller")
//	0.85 an organisation keyword is in the run ("Delta Group", "PwC Société"):
//	      nearly a legal form, but a keyword is convention not registration
//	0.60 a product head noun is in or beside the run ("Helios Platform"):
//	      it reads like a product, which is weaker than being marked as one
//	0.80 several words, seen more than once: a repeated full name
//	0.65 several words, seen once: "Marie Duval" mid-sentence is still a
//	      strong signal, but a single sighting leaves room for a fluke
//	0.45 one word, seen more than once: the weakest thing worth showing,
//	      and where most of the over-detection lives
//	0.25 anything else that survived the detectors
//
// The default floor (0.5) therefore keeps the first five rungs and drops
// the last two.
func candidateScore(r smartRun, count int) float32 {
	switch {
	case r.hasSuffix:
		return 0.95
	case r.hasTrademark, r.hasTitle:
		return 0.90
	case r.orgKeyword:
		return 0.85
	case r.hasProduct:
		return 0.60
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
		if lower == "" || smartParticles[lower] || smartConnectors[lower] {
			continue
		}
		significant++
		if !smartCommonWords[lower] {
			return false
		}
	}
	return significant > 0
}

// followedByTrademark reports whether a trademark mark sits immediately after
// the run, tolerating a single space ("Meridian Suite ™").
func followedByTrademark(text string, end int) bool {
	rest := strings.TrimPrefix(text[end:], " ")
	for _, mark := range trademarkMarks {
		if strings.HasPrefix(rest, mark) {
			return true
		}
	}
	return false
}

// hasProductHeadNoun reports whether a product head noun is the run's own last
// word ("Helios Platform") or the word straight after it ("Helios platform").
// The second form matters because writers capitalise the name and not the noun
// at least as often as they capitalise both.
func hasProductHeadNoun(text string, r smartRun) bool {
	fields := strings.Fields(r.text)
	if len(fields) >= 2 && productHeadNouns[strings.ToLower(fields[len(fields)-1])] {
		return true
	}
	after := strings.Fields(runesAfter(text, r.end, 24))
	if len(after) == 0 {
		return false
	}
	return productHeadNouns[strings.ToLower(strings.Trim(after[0], ".,;:!?()"))]
}
