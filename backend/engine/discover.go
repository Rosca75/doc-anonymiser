// engine/discover.go — the Smart detection tier: a
// fully OFFLINE heuristic discovery pass that always works without
// Ollama. It proposes suggestion values from how names are written, for
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
//     followed by S.A., S.à r.l., GmbH, ... is an entity_names suggestion
//     with high confidence (suffix included in the suggestion text).
//  3. Frequency analysis: runs occurring twice or more qualify on their
//     own; a single-occurrence SINGLE-WORD run without a suffix or title
//     cue is dropped (too noisy). Single-occurrence multi-word runs are
//     kept: "Marie Duval" mid-sentence is a strong name signal even once.
//  4. Title cues (Mr, Mrs, Ms, Dr, Me, M., Mme, ...) route the following
//     name to person_names (the cue itself is not part of the suggestion).
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
// UI-agnostic and I/O-free per CLAUDE.md §4: text in, suggestions out.
package engine

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Suggestion is one UNREVIEWED potential Value, whichever discovery method
// produced it.
//
// It is one type for every route on purpose. Two shapes, one per route, is a
// mapping seam: the frontend had to convert each into its own state, and the
// conversion for Local AI dropped the folded spellings on the floor. One shape
// means the review workspace treats a signal finding, a heuristic finding and a
// model finding identically, which is also what the user is being asked to do.
//
// A Suggestion is never applied. It becomes a Value only when the user accepts
// it, and accepting carries its methods, evidence and spellings across intact.
type Suggestion struct {
	// MainText is the primary textual form of the potential Value.
	MainText string `json:"mainText"`
	// Category is one of AllValueCategories.
	Category string `json:"category"`
	// Spellings are the longer forms folded into this one (FoldValueFamilies):
	// "Coca-Cola company" under "Coca-Cola". Accepting carries them across as
	// the Value's spellings, so ONE Value with its spellings reaches the
	// pipeline instead of two rivals, the shorter of which would fire inside the
	// longer and leave the rest of the phrase in clear text.
	Spellings []string `json:"spellings,omitempty"`
	// Count is how many times the exact main text occurs.
	Count int `json:"count"`
	// Contexts holds up to maxSuggestionContexts snippets of ±60 runes around
	// occurrences, for the review workspace and for Local AI classification
	// prompts.
	Contexts []string `json:"contexts,omitempty"`
	// Confidence is the score of this suggestion, 0.0 to 1.0. It is not the same
	// kind of number as Span.Confidence: nothing is replaced on the strength of
	// it, it only ranks and filters what the review list shows. See
	// suggestionScore for the heuristic ladder.
	Confidence float32 `json:"confidence,omitempty"`
	// DiscoveryMethods is every method that found this suggestion
	// (matchclass.go). Merging two routes' output UNIONS this set rather than
	// keeping one row per route: the user is reviewing a potential Value, not a
	// detector's output, and two routes agreeing is one decision to make.
	DiscoveryMethods []string `json:"discoveryMethods,omitempty"`
	// Evidence is why the methods produced it, deduplicated across them.
	Evidence []Evidence `json:"evidence,omitempty"`
}

// MergeSuggestions is THE merge rule for Suggestions, wherever they come from.
//
// One implementation, used by every producer, because merging is where the data
// loss used to happen: two routes reported into two lists, the frontend mapped
// each into its own shape, and the mapping for one of them dropped the folded
// spellings on the floor. A single function that every route funnels through is
// what makes "nothing is lost" checkable.
//
// It deduplicates main text CASE-INSENSITIVELY WITHIN a category (the same
// string under two categories is an intersection, not a duplicate), keeps the
// first-seen spelling, sums occurrence counts, and UNIONS spellings, contexts,
// discovery methods and evidence. Contexts and evidence documents are capped, so
// merging a hundred files cannot grow one row without bound. Confidence takes
// the STRONGEST sighting: a name seen once in one file and beside a legal form in
// another is as good as the legal-form sighting.
//
// @param batches the per-route, per-document findings, in any order
// @return one Suggestion per (category, main text), in first-seen order
func MergeSuggestions(batches ...[]Suggestion) []Suggestion {
	at := map[string]int{}
	var out []Suggestion
	for _, batch := range batches {
		for _, s := range batch {
			text := strings.TrimSpace(s.MainText)
			if text == "" {
				continue
			}
			key := s.Category + "|" + strings.ToLower(text)
			i, seen := at[key]
			if !seen {
				s.MainText = text
				s.Contexts = MergeContexts(nil, s.Contexts)
				s.Spellings = MergeSpellings(s.Spellings, nil, text)
				s.Evidence = MergeEvidence(nil, s.Evidence)
				at[key] = len(out)
				out = append(out, s)
				continue
			}
			out[i].Count += s.Count
			if s.Confidence > out[i].Confidence {
				out[i].Confidence = s.Confidence
			}
			out[i].Spellings = MergeSpellings(out[i].Spellings, s.Spellings, out[i].MainText)
			out[i].Contexts = MergeContexts(out[i].Contexts, s.Contexts)
			out[i].DiscoveryMethods = MergeMethods(out[i].DiscoveryMethods, s.DiscoveryMethods)
			out[i].Evidence = MergeEvidence(out[i].Evidence, s.Evidence)
		}
	}
	return out
}

// maxSuggestionContexts bounds the snippets one suggestion carries. The review
// row shows a few examples; carrying every occurrence's would grow the payload
// with the document size while telling the user nothing the first few do not.
const maxSuggestionContexts = 3

// WithMethod returns the suggestion with one discovery method recorded. It is
// how each route stamps its own output, so no producer has to remember the
// field name or the dedupe rule.
func (s Suggestion) WithMethod(method string) Suggestion {
	s.DiscoveryMethods = addMethod(s.DiscoveryMethods, method)
	return s
}

// addMethod appends a method unless it is already present, keeping first-seen
// order so the result is deterministic.
func addMethod(methods []string, method string) []string {
	if method == "" {
		return methods
	}
	for _, m := range methods {
		if m == method {
			return methods
		}
	}
	return append(methods, method)
}

// MergeMethods unions two method sets, keeping first-seen order.
func MergeMethods(into, from []string) []string {
	for _, m := range from {
		into = addMethod(into, m)
	}
	return into
}

// HeuristicDiscoveryOptions tunes how eagerly heuristic discovery suggests
// values. Over-detection is the failure mode that matters here: a review list
// nobody can get through is worse than one that misses a value the user can
// still type in by hand. So every knob removes noise:
//
//   - MinLength drops very short suggestions ("Ltd", "Rue").
//   - MinOccurrences requires a suggestion to appear N times.
//   - ExcludeCommonWords drops suggestions made only of ordinary
//     capitalised words (month names, weekdays, common sentence openers),
//     which is where most of the noise comes from.
//   - MinConfidence drops suggestions whose heuristic score is too low,
//     which is the single control that trades recall for precision
//     smoothly rather than in one dimension at a time.
//
// A zero value of this struct means "no filtering at all".
type HeuristicDiscoveryOptions struct {
	// MinLength is the minimum suggestion length in RUNES (not bytes, so
	// accented names count correctly). 0 disables the check.
	MinLength int `json:"minLength"`
	// MinOccurrences is the minimum number of times the suggestion must
	// occur. 0 and 1 both mean "once is enough".
	MinOccurrences int `json:"minOccurrences"`
	// ExcludeCommonWords drops suggestions whose every significant word is
	// an ordinary capitalised word rather than a name.
	ExcludeCommonWords bool `json:"excludeCommonWords"`
	// MinConfidence is the heuristic-score floor, 0.0 to 1.0. 0 disables
	// the check.
	MinConfidence float32 `json:"minConfidence"`
	// Strictness selects HOW MANY detectors a suggestion must satisfy, a
	// lever orthogonal to MinConfidence (which is about how HIGH they must
	// score). "" and "balanced" are the default behaviour; "strict" emits
	// only structurally-anchored suggestions (a legal suffix, a title cue, a
	// trademark, a matching email name or a code), trading recall for
	// precision; "lenient" additionally keeps the rare single-word single-
	// occurrence runs the frequency rule drops. Unknown values read as
	// balanced.
	Strictness string `json:"strictness,omitempty"`
}

// Strictness levels for HeuristicDiscoveryOptions.Strictness. Kept as string
// constants (not an enum type) so they cross the Wails JSON boundary and a
// session file unchanged, and an empty string keeps meaning "balanced".
const (
	StrictnessLenient  = "lenient"
	StrictnessBalanced = "balanced"
	StrictnessStrict   = "strict"
)

// DefaultHeuristicDiscoveryOptions are the options the APPLICATION starts with
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
func DefaultHeuristicDiscoveryOptions() HeuristicDiscoveryOptions {
	return HeuristicDiscoveryOptions{
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
// CLAUDE.md §6). A suggestion whose significant words are ALL in this set
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
	// Contract and legal-document furniture. A contract's Title-Case and
	// ALL-CAPS vocabulary is its own machinery, not the client's identity, and it
	// is the largest noise class a legal document contributes. Same safety
	// property as every other row here: a run is only dropped when EVERY
	// significant word is listed, so "Framework Industries" still gets through.
	// English then French.
	"agreement": true, "agreements": true, "framework": true, "clause": true,
	"party": true, "parties": true, "term": true, "duchy": true,
	"grand": true, "register": true, "registry": true, "consultancy": true,
	"process": true, "information": true, "property": true, "proprietary": true,
	"intellectual": true, "data": true, "protection": true, "jurisdiction": true,
	"governing": true, "obligations": true, "provisions": true, "warranty": true,
	"indemnity": true, "disclosure": true, "confidential": true,
	"confidentiality": true, "deliverable": true, "deliverables": true,
	"purpose": true, "purposes": true, "scope": true, "schedule": true,
	"exhibit": true, "recitals": true, "witness": true, "signature": true,
	"signatures": true, "effective": true, "european": true, "parliament": true,
	"directive": true, "eu": true, "regulations": true,
	"companies": true, "company": true, "service": true, "services": true,
	"societes": true, "sociétés": true,
	"contrat": true, "accord": true, "convention": true, "duché": true,
	"duche": true, "registre": true, "clauses": true, "partie": true,
	"portée": true, "portee": true,
	"annexes": true, "signataire": true, "signataires": true,
	"confidentialité": true, "confidentialite": true, "propriété": true,
	"propriete": true, "intellectuelle": true, "données": true, "donnees": true,
	"européen": true, "europeen": true, "parlement": true,
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
// entity_names suggestion the same way a legal suffix does, but a hair less
// certainly (a legal form is registered, a keyword is convention), so they
// score just below one. Compared accent-folded and lower-cased.
//
// This is broader than the legalSuffixes gazetteer (values.go), which lists
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
// run: "The CSSF" is the term "CSSF", not a Value called "The CSSF", and
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
	// Quantifiers and determiners. "Neither Party" is about the term "Party",
	// exactly as "The CSSF" is about "CSSF", and a contract is full of them.
	"Neither": true, "Either": true, "Each": true, "Every": true, "Any": true,
	"All": true, "Both": true, "Such": true, "No": true, "Some": true,
	"Chaque": true, "Aucun": true, "Aucune": true, "Tout": true, "Tous": true,
	"Toute": true, "Toutes": true,
}

// smartLeadingConjunctions are the function words a Value's name can never
// BEGIN with. They are checked accent-folded and lower-cased, so the rule bites
// on the ALL-CAPS spellings that are the reason it exists: a heading is one
// adjacent stretch of capitalised words to the tokenizer, and without this rule
// a run starts at the "AND" it crosses and harvests the fragment behind it
// ("AND BETWEEN", "AND EXPENSES", "AND INDEMNITY", "AND TERMINATION").
//
// It stays a separate table from smartConnectors, which answers a different
// question: a connector may sit INSIDE a common-noun phrase ("General Terms of
// Sale") and is skipped when counting significant words, while these may not
// OPEN a name at all. Prefer adding a word here over loosening a threshold.
var smartLeadingConjunctions = map[string]bool{
	"and": true, "or": true, "but": true, "nor": true, "so": true, "yet": true,
	"for": true, "to": true, "of": true, "in": true, "on": true, "at": true,
	"by": true, "with": true, "without": true, "as": true, "if": true,
	"et": true, "ou": true, "mais": true, "ni": true, "car": true, "donc": true,
	"aux": true, "avec": true, "sans": true, "pour": true, "par": true,
}

// smartRoleTerminators are the job-title and role words that TERMINATE a
// capitalised run instead of joining it. A signature block reads
// "PIERRE LAVENTURE Partner": the person is the name, the title is what they do,
// and absorbing the title produces a Value ("LAVENTURE Partner") that matches
// the document in one place and the person nowhere else. A terminator opening a
// run means there is no name there at all, so no run is emitted
// ("Chief Information Officer" is a title, not somebody).
//
// The list is deliberately CLOSED and deliberately DISJOINT from
// orgKeywordsCommon: "Partners" and "Associates" name a firm ("Meridian
// Partners"), while the singular "Partner" and "Associate" name a job, so only
// the singular forms terminate. TestRoleTerminatorsDoNotShadowOrgKeywords holds
// that separation, because a word in both tables would silently stop every
// company named after its partners from being found.
//
// Compared lower-cased, accent-folded, with surrounding punctuation and a
// trailing possessive removed, so "Partner," and "Consultant's" are recognised.
var smartRoleTerminators = map[string]bool{
	// English job titles, singular: the form that follows a person's name.
	"partner": true, "associate": true, "director": true, "manager": true,
	"officer": true, "chief": true, "president": true, "chairman": true,
	"chairwoman": true, "chairperson": true, "secretary": true,
	"treasurer": true, "consultant": true, "analyst": true, "executive": true,
	"supervisor": true, "coordinator": true, "administrator": true,
	"specialist": true, "intern": true, "trainee": true, "auditor": true,
	"controller": true, "engineer": true, "advisor": true, "adviser": true,
	"counsel": true, "attorney": true, "notary": true,
	// C-level abbreviations, which sit beside a name exactly as a title does.
	"ceo": true, "cfo": true, "cto": true, "coo": true, "cio": true,
	"cmo": true, "chro": true, "cdo": true,
	// French job titles.
	"associe": true, "associee": true, "gerant": true, "gerante": true,
	"directeur": true, "directrice": true, "presidente": true,
	"secretaire": true, "tresorier": true, "tresoriere": true,
	"responsable": true, "conseiller": true, "conseillere": true,
	"avocat": true, "avocate": true, "notaire": true, "mandataire": true,
	"expert": true, "reviseur": true,
}

// isRoleTerminator reports whether a token is one of the job-title words that
// end a run. The token is folded, stripped of the punctuation a sentence puts
// around it, and stripped of an English possessive, so "Partner," and
// "Consultant's" both answer yes.
func isRoleTerminator(tok string) bool {
	folded := foldAccentsLower(strings.Trim(tok, ".,;:!?()[]\"'’-"))
	folded = strings.TrimSuffix(strings.TrimSuffix(folded, "'s"), "’s")
	return smartRoleTerminators[folded]
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

// legalNameScore is the confidence given to the NAME half of a continental legal
// name ("Contoso" in "Contoso, Société Française de Transport S.A."). It sits at
// the legal-form rung (0.95): the evidence is the same registered legal form,
// and the name is the part of it worth replacing.
const legalNameScore float32 = 0.95

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
	// legalName marks the NAME half of a continental legal name, the part in
	// front of the comma in "Contoso, Société Française de Transport S.A.". It
	// is the form that recurs through the document, so it is the form the user
	// needs as a Value; the full legal name folds into it as a spelling.
	legalName bool
	words     int // significant (non-particle) word count
}

// HeuristicDiscoverWithOptions is SmartDetect with the tuning
// applied. The detectors themselves are unchanged; the options decide
// which of their proposals reach the review list, and every suggestion
// carries the heuristic score the filtering used (suggestionScore), so the
// UI can filter further without recomputing anything.
//
// It runs country-agnostic (the organisation-keyword signal uses only the
// common English set); the App layer, which knows the document country, calls
// HeuristicDiscoverContext directly with it.
func HeuristicDiscoverWithOptions(text string, allow *Allowlist, opts HeuristicDiscoveryOptions) []Suggestion {
	// context.Background() cannot be cancelled, so HeuristicDiscoverContext never
	// returns an error here; the check satisfies the no-blank-error rule and
	// keeps the wrapper honest if the contract ever changes.
	suggestions, err := HeuristicDiscoverContext(context.Background(), text, allow, opts, "")
	if err != nil {
		return nil
	}
	return suggestions
}

// HeuristicDiscoverContext is HeuristicDiscoverWithOptions that can be INTERRUPTED
// Until now the offline pass took no context at all, so Cancel
// could only take effect between documents: one very large file ran to
// completion whatever the user pressed, which is a large part of why
// detection "sometimes does not complete" from the outside.
//
// country scopes the organisation-keyword signal (countryLanguages); "" leaves
// only the common English keyword set active.
//
// On cancellation it returns the suggestions found so far together with
// ctx.Err(), the same contract the chunked LLM scan already had: partial work
// is worth keeping, and the caller decides how to describe it.
func HeuristicDiscoverContext(ctx context.Context, text string, allow *Allowlist, opts HeuristicDiscoveryOptions, country string) ([]Suggestion, error) {
	runs, err := extractRunsContext(ctx, text, country)
	if err != nil {
		return nil, err
	}

	// Per-document email-name gazetteer (one regex pass). A capitalised run
	// that matches a name spelt out in an address is a person with high
	// confidence, whatever the frequency/word-count heuristics would have said.
	emailNames := deriveEmailNames(text)

	// Group occurrences by suggestion text (case-sensitive: "WEBER" and
	// "Weber" are different spellings the review UI should see as typed;
	// the registry collapses case later anyway).
	type group struct {
		text         string
		count        int
		firstStart   int
		category     string
		qualifies    bool
		legalName    bool
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
		case r.hasSuffix, r.legalName:
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
		if r.hasSuffix || r.hasTitle || r.hasTrademark || r.orgKeyword || r.legalName {
			g.qualifies = true
		}
		if r.legalName {
			g.legalName = true
		}
		if len(g.contexts) < maxSuggestionContexts {
			g.contexts = append(g.contexts, contextSnippet(text, r.start, r.end))
		}
	}

	strictness := opts.Strictness
	var out []Suggestion
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
		// person names, single words as organisation-ish names.
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
		score := suggestionScore(r, g.count)

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

		// Legal-name signal: the group was seen as the name half of a legal name
		// at least once. The score is lifted for the same reason the email signal
		// lifts it, and it is done here rather than in suggestionScore because
		// only ONE of a company's many occurrences carries its legal form, while
		// suggestionScore only ever sees the first.
		if g.legalName && score < legalNameScore {
			score = legalNameScore
		}

		// Strict strictness: emit ONLY structurally-vouched suggestions, so a
		// bare capitalised run or a lone product head noun is dropped however
		// it scored. This is the high-precision end of the lever, orthogonal to
		// the numeric confidence floor: strictness is about WHICH detectors are
		// trusted, the floor is about how high they must score.
		if strictness == StrictnessStrict && !vouched {
			continue
		}
		if !keepSuggestion(g.text, g.count, score, opts) {
			continue
		}

		out = append(out, Suggestion{
			MainText:   g.text,
			Category:   g.category,
			Count:      g.count,
			Contexts:   g.contexts,
			Confidence: score,
		})
	}

	sortSuggestions(out, func(text string) int { return groups[text].firstStart })

	// The code detector is a second scanner over the same text (codes.go). It
	// runs here rather than at the call site so every caller of the offline
	// route gets the same set, and so the tuning options apply to both scanners
	// through one filter.
	codes, codeStarts := detectCodes(text, allow)
	for _, c := range codes {
		if keepSuggestion(c.MainText, c.Count, c.Confidence, opts) {
			out = append(out, c)
		}
	}
	sortSuggestions(out, func(text string) int {
		if g, ok := groups[text]; ok {
			return g.firstStart
		}
		return codeStarts[text]
	})
	return out, nil
}

// sortSuggestions imposes the review list's deterministic ranking: the most
// frequent value first, ties broken by where it first appears in the document.
// Shared by every offline detector so two lists cannot be ordered differently.
//
// @param suggestions the list, sorted in place
// @param firstStart the byte offset of a value's first occurrence
func sortSuggestions(suggestions []Suggestion, firstStart func(text string) int) {
	sort.SliceStable(suggestions, func(i, j int) bool {
		if suggestions[i].Count != suggestions[j].Count {
			return suggestions[i].Count > suggestions[j].Count
		}
		return firstStart(suggestions[i].MainText) < firstStart(suggestions[j].MainText)
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

		// A job title opening a run means there is no name at this position:
		// "Chief Information Officer" is what somebody does, not who they are.
		if isRoleTerminator(tokens[i].text) {
			i++
			continue
		}

		// A name run never begins with a conjunction. An ALL-CAPS heading is a
		// single adjacent stretch of capitalised words to the tokenizer, so a
		// run starting after the lowercase-crossing "AND" harvests the fragment
		// behind it ("AND BETWEEN", "AND TERMINATION"): ten of those were the
		// second largest noise class in the review list.
		if smartLeadingConjunctions[foldAccentsLower(tokens[i].text)] {
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
			if j > i && isRoleTerminator(t) {
				break // "PIERRE LAVENTURE Partner": the title is not the name
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
			continue // a bare title with no name is not a suggestion
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

		// Detector 2c: the comma between a name and its legal form.
		//
		// "Name, Société anonyme" / "Name, S.A." / "Name, Sàrl" is the standard
		// continental legal-name form and the dominant one in French and
		// Luxembourg drafting. A comma always terminates a run, so what survived
		// was either the legal form with no name in front of it (worthless: the
		// name is the only part worth replacing) or, when the form was one word,
		// nothing at all. Reaching back over the comma is what makes the two
		// parties of a contract like this findable offline at all.
		//
		// Bounded tightly, so the rule cannot walk a list of ordinary nouns: ONE
		// comma, no newline, and the run itself must be a legal-form phrase.
		if back := reachBackAcrossComma(text, tokens, i, r, country); back >= 0 {
			// The NAME half on its own, emitted beside the full legal name. It
			// is what recurs through the document (113 bare "Contoso" against
			// one "Contoso, Société Française de Transport S.A."), so without it
			// the user accepts a Value that matches the document once. Family
			// folding then makes the short form the main text and the full legal
			// name its spelling, which is the one-value-one-placeholder rule.
			nameText := text[tokens[back].start:tokens[i-1].end]
			if len([]rune(nameText)) >= 3 && !isBareSuffix(nameText) {
				runs = append(runs, smartRun{
					text:          nameText,
					start:         tokens[back].start,
					end:           tokens[i-1].end,
					sentenceStart: tokens[back].sentenceStart,
					legalName:     true,
					words:         significantWords(nameText),
				})
			}
			r.start = tokens[back].start
			r.text = text[r.start:r.end]
			r.words = significantWords(r.text)
			// The name in front of the comma is where the run now starts, so
			// whether it opened a sentence is the question that matters.
			r.sentenceStart = tokens[back].sentenceStart
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

		// Very short suggestions are noise ("Al", "Le"), and a run that IS
		// a bare legal form ("GmbH" discussed as a concept) is not a name.
		if len([]rune(r.text)) >= 3 && !isBareSuffix(r.text) {
			runs = append(runs, r)

			// Sentence-start runs often glue a grammar-capitalised word
			// onto a real name ("Later Marie Duval called"). Emit the
			// sub-run without the first word too, so "Marie Duval" still
			// counts; the frequency and sentence rules weed out whichever
			// grouping is noise. Suffix and organisation runs are already whole
			// company names and produce no sub-run.
			// The sub-run is where the conjunction rule earns most of its keep:
			// an ALL-CAPS heading ("COSTS AND EXPENSES") is one run, and its
			// sub-run starts at the second word, which is exactly the "AND".
			if r.sentenceStart && !r.hasSuffix && !r.hasTitle && !r.orgKeyword && last > i &&
				!smartLeadingConjunctions[foldAccentsLower(tokens[i+1].text)] {
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

// reachBackAcrossComma answers detector 2c: does a name sit in front of this
// run's legal form, separated only by a comma?
//
// It returns the token index the run should START at, or -1 to leave the run
// alone. Every guard below is one wrong extension it prevents:
//
//   - the run must BE a legal-form phrase (isLegalFormTail), so the rule cannot
//     glue two ordinary capitalised phrases together across a comma;
//   - exactly ONE comma and no newline may separate them, so it cannot walk an
//     enumeration or jump a line break;
//   - the word in front must be a plain capitalised name word, so an article, a
//     job title or a bare legal form never becomes the name;
//   - it walks further back only over SPACE-ADJACENT capitalised words, so
//     "Acme Group, S.A." keeps its whole name and a second comma stops it.
//
// @param text the document working form
// @param tokens the tokenizer's output for it
// @param at the index of the token the run currently starts at
// @param r the run as the suffix and org detectors left it
// @param country the document country, scoping the legal-form vocabulary
// @return the token index to start at, or -1
func reachBackAcrossComma(text string, tokens []token, at int, r smartRun, country string) int {
	if at == 0 || !isLegalFormTail(r, country) {
		return -1
	}
	sep := text[tokens[at-1].end:tokens[at].start]
	if strings.Count(sep, ",") != 1 || strings.TrimLeft(strings.TrimRight(sep, " "), " ") != "," {
		return -1 // not exactly one comma with only spaces around it
	}
	prev := at - 1
	if !isCapWord(tokens[prev].text) ||
		smartLeadingStopwords[tokens[prev].text] ||
		smartLeadingConjunctions[foldAccentsLower(tokens[prev].text)] ||
		isRoleTerminator(tokens[prev].text) ||
		isBareSuffix(tokens[prev].text) {
		return -1
	}
	// Walk further back over the rest of the name, space-adjacent capitalised
	// words only. tokens[k].adjacent is false at the second comma, which is what
	// keeps the single-comma bound.
	for prev > 0 && tokens[prev].adjacent && isCapWord(tokens[prev-1].text) &&
		!smartLeadingStopwords[tokens[prev-1].text] &&
		!smartLeadingConjunctions[foldAccentsLower(tokens[prev-1].text)] &&
		!isRoleTerminator(tokens[prev-1].text) {
		prev--
	}
	return prev
}

// isLegalFormTail reports whether a run reads as the LEGAL FORM half of a
// continental legal name, which is the only tail detector 2c may reach back
// from.
//
// Two shapes qualify, and nothing else:
//
//   - the run carries a registered legal suffix ("Société Française de
//     Transport S.A."), which the suffix detector has already established;
//   - the run's FIRST significant word is an organisation-form word for the
//     document country ("Société coopérative", "Gesellschaft ...",
//     "Sociedad ..."). This shape needs its own test because a one-word form
//     ("Société") carries no second significant word, so the ordinary
//     organisation-keyword rule, which requires one, cannot see it.
func isLegalFormTail(r smartRun, country string) bool {
	if r.hasSuffix {
		return true
	}
	for _, w := range strings.FieldsFunc(r.text, func(c rune) bool { return c == ' ' || c == '-' }) {
		folded := foldAccentsLower(strings.Trim(w, ".,;:!?()\"'’"))
		if folded == "" || smartParticles[folded] || smartConnectors[folded] {
			continue
		}
		return orgKeywordApplies(folded, country)
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
// from the gazetteer (never a suggestion on its own).
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

// signatureRuleUnderscores is how many consecutive '_' characters read as a
// signature rule rather than as punctuation. Three is enough to be deliberate
// and short enough to catch a hand-typed rule.
const signatureRuleUnderscores = 3

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

	// underscores counts the run of '_' characters currently being crossed. A
	// run of signatureRuleUnderscores or more is a RULED LINE, which is what a
	// signature block puts above a printed name. The word after it is a name and
	// not a grammar-capitalised sentence opener, so the rule clears the
	// sentence-start flag the preceding newline set. Without this the first word
	// of every signature is treated as sentence case and stripped as noise,
	// which loses the forename ("PIERRE LAVENTURE" becomes "LAVENTURE").
	underscores := 0

	for i, r := range text {
		isWord := unicode.IsLetter(r) || r == '-' || r == '\'' || r == '’'
		if isWord {
			if wordStart < 0 {
				wordStart = i
			}
			underscores = 0
			continue
		}
		flush(i)
		if r == '_' {
			underscores++
			if underscores >= signatureRuleUnderscores {
				sentenceStart = false
			}
			continue
		}
		// Sentence boundaries: ., !, ?, newline. A '.' only ends a
		// sentence when followed by whitespace (keeps "S.A." inside runs).
		switch r {
		case '\n', '!', '?':
			sentenceStart = true
			underscores = 0
		case '.', ':':
			sentenceStart = true
			underscores = 0
		case ' ', '\t':
			// Spaces between the rule and the name do not end it.
		default:
			underscores = 0
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

// --- suggestion scoring and filtering -------------------------

// suggestionScore turns what the detectors observed about a run into one
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
func suggestionScore(r smartRun, count int) float32 {
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

// keepSuggestion applies the HeuristicDiscoveryOptions filters to one suggestion.
// Split out of HeuristicDiscoverWithOptions so each rule is independently
// testable and so the order of the checks is visible: cheapest first,
// and the word list before the score, because "this is just the word
// March" is a better reason to drop something than "it scored low".
//
// Every filter reads the suggestion's own text, count and score, so the run
// that produced it is deliberately not a parameter: a caller with no run to
// hand over would otherwise have to invent one.
func keepSuggestion(text string, count int, score float32, opts HeuristicDiscoveryOptions) bool {
	if opts.MinLength > 0 && len([]rune(text)) < opts.MinLength {
		return false
	}
	if opts.MinOccurrences > 1 && count < opts.MinOccurrences {
		return false
	}
	if opts.ExcludeCommonWords && isCommonWordRun(text) {
		return false
	}
	if opts.ExcludeCommonWords && isAllCapsHeadingText(text) {
		return false
	}
	if opts.MinConfidence > 0 && score < opts.MinConfidence {
		return false
	}
	return true
}

// isCommonWordRun reports whether EVERY significant word of the suggestion
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

// headingFunctionWords are the function words that appear inside a heading or a
// legal formula and NEVER inside a name: "PARTIES' ROLES AND COMMITMENTS",
// "IN WITNESS WHEREOF", "GOVERNING LAW AND COMPETENT JURISDICTION". Compared
// accent-folded and lower-cased.
//
// A name particle ("de", "van") is deliberately absent: "Banque de la Place" is
// a real organisation and must survive.
var headingFunctionWords = map[string]bool{
	"and": true, "or": true, "of": true, "for": true, "to": true, "in": true,
	"into": true, "on": true, "with": true, "under": true, "by": true,
	"this": true, "these": true, "that": true, "whereof": true, "whereas": true,
	"hereby": true, "herein": true, "between": true, "among": true,
	"et": true, "ou": true, "des": true, "aux": true, "entre": true,
	"pour": true, "par": true, "sur": true, "sous": true,
}

// isAllCapsHeadingText reports whether a run is ALL-CAPS heading or legal-formula
// text rather than a name.
//
// ALL CAPS alone cannot be the rule: a signature block writes real people in
// capitals ("PIERRE DUPONT", "MARTIN DESCHAMPS"), and those are exactly the
// values the review list exists to surface. What separates furniture from a name
// is the FUNCTION WORD inside it: a heading joins clauses ("ROLES AND
// COMMITMENTS", "PARTIES ENTER INTO THIS AGREEMENT", "IN WITNESS WHEREOF") and a
// person's name never does.
//
// Both conditions are required, so a Title-Case phrase containing "and" is left
// to the common-word list, where a real "Marks and Spencer" can still get
// through: restricting the rule to capitals bounds it to the text that is
// document furniture by convention.
func isAllCapsHeadingText(text string) bool {
	words := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '-' || r == '\'' || r == '’'
	})
	hasFunctionWord := false
	significant := 0
	for _, w := range words {
		trimmed := strings.Trim(w, ".,;:!?()\"'’")
		if trimmed == "" {
			continue
		}
		folded := foldAccentsLower(trimmed)
		if headingFunctionWords[folded] {
			hasFunctionWord = true
			continue
		}
		if smartParticles[folded] {
			continue
		}
		significant++
		// One lower-case letter is enough to say this is not heading capitals.
		if strings.ToUpper(trimmed) != trimmed {
			return false
		}
	}
	return hasFunctionWord && significant > 0
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
