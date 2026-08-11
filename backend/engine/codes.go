// engine/codes.go — the offline detector for CODE-SHAPED values: reference
// numbers, contract and invoice references, and project codes.
//
// It is a second, independent scanner rather than a rule inside the
// capitalised-run scanner in discover.go, for one reason: that scanner reads
// tokens produced by `tokenize`, which accepts only letters, hyphens and
// apostrophes as word runes. A digit ends a token there, so no code shape can
// ever surface through it. Bolting digits onto `tokenize` would change what
// every other detector sees; a separate pass over the raw text changes nothing.
//
// It emits two categories, decided by the CUE WORD next to the match:
//
//	project_names a project cue is adjacent ("projet ATLAS-2024")
//	identifier_names everything else ("Ref. INV-88213")
//
// The two share a shape because they ARE the same shape: what separates a
// project code from an invoice reference is the word beside it, not the code.
package engine

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// codeShapeRe matches the code shapes worth proposing: two to six upper-case
// letters, a SEPARATOR, then three or more digits, with an optional trailing
// block ("-A", "/2024").
//
// Matches:      PRJ-4471, INV 88213, REF/2024001, ATLAS-2024-A, DOSSIER_12345
// Deliberately not:
//
//	LU12345678 letters and digits with nothing between them is the shape of
//	             a tax or VAT number, which pass 1 owns. The separator is what
//	             keeps this detector out of pass 1's territory, and the cost is
//	             an unseparated in-house code, which is rare and which the user
//	             can declare by hand. TestCodeDetectorDoesNotOverlapPassOne
//	             holds the boundary.
//	A-123 one leading letter is too weak; "A" is a list bullet
//	PRJ-44 two digits is a page number or a quantity
//	CSSF letters alone are an acronym, which the run detector proposes
//	2024-01-15 digits first is a date, which pass 1 owns
//
// The match boundary is verified on the OFFSETS by isWordBoundary rather than
// consumed by the pattern: RE2 has no lookaround, and a consuming guard eats
// the separator between two adjacent codes so the second one is never matched.
var codeShapeRe = regexp.MustCompile(`[A-Z]{2,6}[-_ /][0-9]{3,}(?:[-/][A-Za-z0-9]{1,4})?`)

// Confidence rungs for a code shape. A code is a strong signal on its own,
// because ordinary prose does not contain them; a cue word beside it removes
// the remaining doubt about what KIND of code it is.
const (
	confidenceCodeBare float32 = 0.70
	confidenceCodeCued float32 = 0.85
	codeCueRadiusRunes         = 24 // how far either side a cue word counts
)

// projectCues route a code to project_names. Kept small and specific: a cue
// list that grows towards "any noun near a code" stops separating anything.
var projectCues = map[string]bool{
	"project": true, "projet": true, "projekt": true,
	"engagement": true, "mission": true, "workstream": true,
	"phase": true, "programme": true, "program": true,
}

// identifierCues raise the confidence of a code without changing its category.
// They are the words that introduce a reference in the documents this tool is
// aimed at, in the three languages those documents are written in.
var identifierCues = map[string]bool{
	"ref": true, "réf": true, "reference": true, "référence": true, "referenz": true,
	"dossier": true, "contrat": true, "contract": true, "vertrag": true,
	"facture": true, "invoice": true, "rechnung": true,
	"no": true, "nr": true, "num": true, "number": true, "numéro": true,
	"id": true, "case": true, "ticket": true, "order": true, "commande": true,
}

// DetectCodes proposes the code-shaped values in text.
//
// It returns Candidates, not Spans: like the rest of smart detection this is a
// SUGGESTION for the user to accept or reject on step 2, not a replacement.
// Accepting one turns it into an Entity, and the entity pass replaces it.
//
// Allowlisted codes are dropped here, before the user ever sees them, because
// the allowlist wins in every pass and offering a value that would then never
// be replaced is a review decision with no effect.
//
// @param text the document's working markdown
// @param allow the never-anonymise list, plus the session's removed values
// @return one candidate per distinct code, most frequent first
func DetectCodes(text string, allow *Allowlist) []Candidate {
	candidates, _ := detectCodes(text, allow)
	return candidates
}

// detectCodes is DetectCodes plus the byte offset of each code's first
// occurrence, which the offline route needs to order codes and capitalised runs
// in one list without comparing an offset against a slice index.
func detectCodes(text string, allow *Allowlist) ([]Candidate, map[string]int) {
	type group struct {
		count      int
		firstStart int
		category   string
		confidence float32
		contexts   []string
	}
	groups := map[string]*group{}
	var order []string

	for _, m := range codeShapeRe.FindAllStringIndex(text, -1) {
		if !isWordBoundary(text, m[0], m[1]) {
			continue
		}
		code := text[m[0]:m[1]]
		if allow.Contains(code) {
			continue
		}

		category, confidence := classifyCode(text, m[0], m[1])
		g, ok := groups[code]
		if !ok {
			g = &group{firstStart: m[0], category: category, confidence: confidence}
			groups[code] = g
			order = append(order, code)
		}
		g.count++
		// The strongest sighting decides: one cued occurrence is enough to know
		// what the code is, and the uncued ones are the same code.
		if confidence > g.confidence {
			g.confidence = confidence
		}
		if category == CatProjectNames {
			g.category = CatProjectNames
		}
		if len(g.contexts) < maxContexts {
			g.contexts = append(g.contexts, contextSnippet(text, m[0], m[1]))
		}
	}

	out := make([]Candidate, 0, len(order))
	for _, code := range order {
		g := groups[code]
		out = append(out, Candidate{
			Text:       code,
			Category:   g.category,
			Count:      g.count,
			Contexts:   g.contexts,
			Confidence: g.confidence,
		})
	}
	firstStarts := make(map[string]int, len(groups))
	for code, g := range groups {
		firstStarts[code] = g.firstStart
	}
	sortCandidates(out, func(text string) int { return firstStarts[text] })
	return out, firstStarts
}

// classifyCode reads the words around one occurrence and decides what the code
// is.
//
// The NEAREST cue wins, not the strongest. "Ref. INV-88213 covers the projet
// ATLAS-2024" has both a reference cue and a project cue inside the window, and
// the reference cue is the one attached to INV-88213: a rule that preferred
// project cues would file every code in that sentence as a project.
func classifyCode(text string, start, end int) (category string, confidence float32) {
	nearest, distance := "", 1<<31
	for _, cue := range cueWordsAround(text, start, end) {
		if !projectCues[cue.word] && !identifierCues[cue.word] {
			continue
		}
		if cue.distance < distance {
			nearest, distance = cue.word, cue.distance
		}
	}
	switch {
	case projectCues[nearest]:
		return CatProjectNames, confidenceCodeCued
	case identifierCues[nearest]:
		return CatIdentifierNames, confidenceCodeCued
	default:
		return CatIdentifierNames, confidenceCodeBare
	}
}

// cueWord is one candidate cue with how far it sits from the code, in words.
type cueWord struct {
	word     string
	distance int
}

// cueWordsAround returns the words within codeCueRadiusRunes of the match on
// either side, lower-cased and stripped of the punctuation that normally
// attaches to a cue ("Ref." and "Réf:" both have to read as "ref"), each with
// its distance from the code so the nearest one can win.
func cueWordsAround(text string, start, end int) []cueWord {
	var out []cueWord

	before := strings.Fields(runesBefore(text, start, codeCueRadiusRunes))
	for i, field := range before {
		if word := normaliseCue(field); word != "" {
			out = append(out, cueWord{word: word, distance: len(before) - i})
		}
	}
	for i, field := range strings.Fields(runesAfter(text, end, codeCueRadiusRunes)) {
		if word := normaliseCue(field); word != "" {
			out = append(out, cueWord{word: word, distance: i + 1})
		}
	}
	return out
}

func normaliseCue(field string) string {
	return strings.ToLower(strings.Trim(field, ".,;:!?()[]\"'\u2019-"))
}

func runesBefore(text string, offset, n int) string {
	window := text[:offset]
	if len(window) > 4*n {
		window = window[len(window)-4*n:]
	}
	window = strings.TrimLeftFunc(window, func(r rune) bool { return r == utf8.RuneError })
	runes := []rune(window)
	if len(runes) > n {
		runes = runes[len(runes)-n:]
	}
	return string(runes)
}

func runesAfter(text string, offset, n int) string {
	window := text[offset:]
	if len(window) > 4*n {
		window = window[:4*n]
	}
	window = strings.TrimRightFunc(window, func(r rune) bool { return r == utf8.RuneError })
	runes := []rune(window)
	if len(runes) > n {
		runes = runes[:n]
	}
	return string(runes)
}
