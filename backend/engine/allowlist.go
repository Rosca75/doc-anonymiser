// engine/allowlist.go — terms that are NEVER anonymised (CLAUDE.md §5:
// "Allowlist wins: an allowlisted term is never replaced, by any pass").
//
// The default seed covers the public, non-identifying vocabulary of the
// owner's domain: financial regulators, common methodologies/standards and
// country names — words that look like organisation or location values
// to the passes but identify nobody. Users add and remove terms in the UI;
// the seed itself can be removed for a session too (per-term delete).
package engine

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// defaultAllowlist is the documented seed.
var defaultAllowlist = []string{
	// Regulators and public institutions (public bodies, not clients).
	"CSSF", "ECB", "BCE", "EBA", "ESMA", "EIOPA", "BCL", "CNPD", "FATF", "OECD", "IMF",
	// Common methodologies, frameworks and standards.
	"Agile", "Scrum", "Kanban", "PRINCE2", "TOGAF", "ITIL", "COBIT",
	"IFRS", "GAAP", "Basel III", "Solvency II", "MiFID II", "GDPR", "RGPD",
	"ISO 27001", "ISAE 3402", "SOC 2",
	// Country names commonly present in engagement documents. Cities are
	// NOT seeded — a city can identify a client site; countries rarely do.
	"Luxembourg", "France", "Germany", "Belgium", "Netherlands", "Switzerland",
	"United Kingdom", "United States", "Ireland", "Italy", "Spain", "Portugal",
	"Austria", "Poland", "Allemagne", "Belgique", "Pays-Bas", "Suisse", "Royaume-Uni",
}

// DefaultAllowlistTerms returns a copy of the seeded default terms so the
// UI can show them at startup. The user sees every
// seeded term, can remove any, and the UI-driven list stays the single
// runtime source; nothing is applied silently.
func DefaultAllowlistTerms() []string {
	out := make([]string, len(defaultAllowlist))
	copy(out, defaultAllowlist)
	sort.Strings(out)
	return out
}

// ParseAllowlistCSV parses a user-supplied CSV of never-anonymise terms
// Tolerant by design:
//   - a UTF-8 BOM is stripped, CRLF line endings are fine,
//   - if a header row is present, the column named "term" is used
//     (case-insensitive); otherwise column 1,
//   - whitespace is trimmed, empty cells dropped,
//   - duplicates collapse case-insensitively (first spelling wins).
//
// Malformed CSV returns an actionable error naming the first bad line.
func ParseAllowlistCSV(data []byte) ([]string, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM

	// A semicolon-delimited file (common Excel export in Europe) parses as
	// one column containing semicolons; detect and reject it with a fix.
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1 // rows may be ragged; we only read one column
	r.Comment = '#'        // the template ships commented example lines

	var rows [][]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			if perr, ok := err.(*csv.ParseError); ok {
				return nil, fmt.Errorf(
					"the allowlist CSV could not be parsed at line %d (%v), fix that line or re-export the file as plain CSV", perr.Line, perr.Err)
			}
			return nil, fmt.Errorf("the allowlist CSV could not be parsed (%v), re-export the file as plain CSV", err)
		}
		rows = append(rows, rec)
	}
	if len(rows) == 0 {
		return []string{}, nil // an empty file is an empty list, not an error
	}

	// Column selection: header row "term" if present, else column 1.
	col := 0
	start := 0
	for i, name := range rows[0] {
		if strings.EqualFold(strings.TrimSpace(name), "term") {
			col, start = i, 1
			break
		}
	}

	var out []string
	seen := map[string]bool{}
	for i := start; i < len(rows); i++ {
		if col >= len(rows[i]) {
			continue
		}
		term := strings.TrimSpace(rows[i][col])
		if term == "" || seen[strings.ToLower(term)] {
			continue
		}
		if strings.Contains(term, ";") && len(rows[i]) == 1 {
			return nil, fmt.Errorf(
				"line %d looks semicolon-delimited (%q), the allowlist import expects comma-separated CSV; re-export the file with commas or use the downloadable template", i+1, term)
		}
		seen[strings.ToLower(term)] = true
		out = append(out, term)
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// AllowlistTemplateCSV returns the downloadable template
// Phase 4a): a commented explanation, the "term" header and three example
// rows. The template parses through ParseAllowlistCSV (round-trip test).
func AllowlistTemplateCSV() []byte {
	return []byte(`# Allowlist template for doc-anonymiser.
# One term per row in the "term" column. These terms are never anonymised.
# Lines starting with # are ignored.
# A term prefixed with "regex:" is compiled as a case-insensitive pattern
# (e.g. "regex:^CVE-\d{4}-\d+$" keeps every CVE identifier untouched).
term
CSSF
IFRS 17
Luxembourg
`)
}

// AllowlistRegexPrefix marks a regex allowlist entry:
// a term starting with "regex:" is compiled and matched with (?i) prepended
// so authors do not have to remember to add it. Compiling is done once at
// Add time; broken patterns are dropped with a note kept in RegexErrors.
const AllowlistRegexPrefix = "regex:"

// Allowlist is a case-insensitive term set (plus optional regex entries),
// safe for concurrent use (the pipeline goroutine reads it while the UI
// may edit it).
type Allowlist struct {
	mu sync.RWMutex
	// terms maps the lower-cased term to its display spelling, so the UI
	// lists "CSSF" (as seeded/typed) rather than "cssf".
	terms map[string]string
	// regexes carries compiled regex entries. Keyed by
	// the original "regex:..." display spelling so Terms() can re-emit it.
	regexes map[string]*regexp.Regexp
	// RegexErrors reports the display spelling of every regex entry that
	// failed to compile — surfaced in the UI so the user can fix it.
	RegexErrors []string
}

// NewAllowlist returns an allowlist pre-seeded with the defaults.
func NewAllowlist() *Allowlist {
	a := NewEmptyAllowlist()
	for _, t := range defaultAllowlist {
		a.Add(t)
	}
	return a
}

// NewEmptyAllowlist returns an allowlist with no terms (used by tests and
// by session loading, which restores the user's exact term set).
func NewEmptyAllowlist() *Allowlist {
	return &Allowlist{terms: map[string]string{}, regexes: map[string]*regexp.Regexp{}}
}

// Add inserts a term (case-insensitive; re-adding updates the display
// spelling). Empty strings are ignored. A term beginning with
// AllowlistRegexPrefix ("regex:") is compiled as a case-insensitive regex
// and stored separately; compilation errors are collected in RegexErrors.
func (a *Allowlist) Add(term string) {
	term = strings.TrimSpace(term)
	if term == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if rest, ok := strings.CutPrefix(term, AllowlistRegexPrefix); ok {
		pattern := strings.TrimSpace(rest)
		if pattern == "" {
			a.RegexErrors = append(a.RegexErrors,
				fmt.Sprintf("allowlist regex is empty (%q), remove it or supply a pattern", term))
			return
		}
		re, err := regexp.Compile(`(?i)` + pattern)
		if err != nil {
			a.RegexErrors = append(a.RegexErrors,
				fmt.Sprintf("allowlist regex %q did not compile (%v), fix the pattern or remove it", term, err))
			return
		}
		a.regexes[term] = re
		return
	}
	a.terms[strings.ToLower(term)] = term
}

// Remove deletes a term (case-insensitive; regex entries match on display
// spelling since two spellings compile to different regexes). Removing an
// unknown term is a no-op.
func (a *Allowlist) Remove(term string) {
	term = strings.TrimSpace(term)
	a.mu.Lock()
	defer a.mu.Unlock()
	if strings.HasPrefix(term, AllowlistRegexPrefix) {
		delete(a.regexes, term)
		return
	}
	delete(a.terms, strings.ToLower(term))
}

// Contains reports whether the term is allowlisted (literal OR regex),
// ignoring case and surrounding whitespace. A nil allowlist contains
// nothing.
func (a *Allowlist) Contains(term string) bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if _, ok := a.terms[strings.ToLower(strings.TrimSpace(term))]; ok {
		return true
	}
	for _, re := range a.regexes {
		if re.MatchString(term) {
			return true
		}
	}
	return false
}

// Terms returns the display spellings (literal + "regex:..." entries),
// alphabetically sorted — stable UI listing and deterministic session files.
func (a *Allowlist) Terms() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]string, 0, len(a.terms)+len(a.regexes))
	for _, display := range a.terms {
		out = append(out, display)
	}
	for display := range a.regexes {
		out = append(out, display)
	}
	sort.Strings(out)
	return out
}

// FilterAllowed drops every span whose matched text is allowlisted. It is
// the shared guard applied to built-in pattern, Value and custom-pattern spans
// alike — the single place enforcing "allowlist wins" for span producers
// that do not check the allowlist themselves.
func FilterAllowed(spans []Span, allow *Allowlist) []Span {
	if allow == nil {
		return spans
	}
	out := make([]Span, 0, len(spans))
	for _, s := range spans {
		if !allow.Contains(s.Original) {
			out = append(out, s)
		}
	}
	return out
}

// --- defined terms: the vocabulary a document declares about itself ----------

// Definition idioms, the drafting shapes that introduce a defined term. They are
// string constants so a term's provenance survives the Wails JSON boundary and a
// session file, and so the UI can name the idiom in the never-anonymise list
// instead of showing an unexplained entry.
const (
	// DefinitionIdiomMeans is the dictionary form: `"Work Order" means ...`.
	DefinitionIdiomMeans = "means"
	// DefinitionIdiomParenthetical is the inline form: `(the "Dedicated
	// Advisors")`, `(together the "Experts")`.
	DefinitionIdiomParenthetical = "parenthetical"
)

// DefinedTerm is one phrase a DOCUMENT declares as its own vocabulary.
//
// A defined term is not merely noise in a review list: it is the strongest "do
// not anonymise" signal a contract can offer, because the document itself says
// the phrase is part of its machinery. A phrase introduced as `"Work Order"
// means ...` or `(the "Dedicated Advisors")` is definitionally not a client
// identity, and in the measured fixture nineteen such phrases accounted for the
// largest single class of false positives in the review list.
//
// It carries its provenance so the never-anonymise list can show WHY the entry
// is there and the user can delete it, exactly as a session exclusion is
// visible and reversible. A suppressor the user cannot see is the mistake this
// shape exists to avoid.
type DefinedTerm struct {
	// Term is the phrase as the document spells it, emphasis markers removed.
	Term string `json:"term"`
	// Idiom is which drafting shape introduced it (the constants above).
	Idiom string `json:"idiom"`
	// Document is the file the definition was read from, for the UI.
	Document string `json:"document,omitempty"`
}

// definedTermMeansRe matches the dictionary idiom: a quoted phrase followed by
// "means" or "shall mean".
//
// Matches:      “**Work Order**” means, "Confidential Information" means,
//
//	“Loss” shall mean
//
// Does not match: hereinafter referred to as “Contoso” (no "means", and no
//
//	parentheses either, which is what keeps a party's short name
//	out of the suppressor: the short name is exactly what has to be
//	anonymised).
//
// The capture tolerates the markdown emphasis markers the working form carries
// inside the quotes, because the converter wraps the bold term the drafter
// used; stripMarkdownEmphasis removes what is left.
var definedTermMeansRe = regexp.MustCompile(
	`["\x{201c}]([^"\x{201c}\x{201d}\n]{2,60}?)["\x{201d}][ ]*(?:shall[ ]+mean|means)\b`)

// definedTermParentheticalRe matches the inline idiom: a quoted phrase inside
// parentheses, introduced by an article.
//
// Matches:      (the “**Dedicated Advisors**”), (together the “***Experts***”),
//
//	(each a "Party")
//
// Does not match: (“AC Process”) — no article, so the shape is not certain
//
//	enough; and any unparenthesised quotation, so
//	`referred to as “Contoso”` is untouched.
//
// The ARTICLE is required deliberately. It is what separates a definition from
// an ordinary aside, and dropping it would let a document that writes
// (“Contoso”) suppress its own party name.
var definedTermParentheticalRe = regexp.MustCompile(
	`\((?:[ ]*(?:together|collectively)[ ]+)?(?:the|a|an|each[ ]+an?|each)[ ]+["\x{201c}]([^"\x{201c}\x{201d}\n]{2,60}?)["\x{201d}][ ]*\)`)

// markdownEmphasisRe matches a run of markdown emphasis markers, so a term read
// out of the working form is stored as the drafter's words rather than with the
// converter's bold markers around it.
var markdownEmphasisRe = regexp.MustCompile(`[*_]{1,3}`)

// DiscoverDefinedTerms returns the terms a document declares as its own
// vocabulary, in first-seen order and deduplicated case-insensitively.
//
// Two idioms are recognised and no more, because the measurement that motivated
// this found the dictionary form alone caught six of nineteen while adding the
// inline parenthetical form caught all nineteen. A third, looser shape would
// start suppressing the party names the document introduces with
// `hereinafter referred to as "..."`, which are the values that most need
// replacing.
//
// The terms are enforced through the allowlist, which matches a WHOLE term
// case-insensitively. That is load-bearing rather than incidental: a
// prefix-matching suppressor removed "Services NStar", because "Services" is a
// defined term and "Services NStar" contains a real entity.
//
// @param name the document the text came from, recorded as provenance
// @param text the document's markdown working form
// @return one DefinedTerm per distinct phrase, in the order they appear
func DiscoverDefinedTerms(name, text string) []DefinedTerm {
	var out []DefinedTerm
	seen := map[string]bool{}
	collect := func(re *regexp.Regexp, idiom string) {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			term := cleanDefinedTerm(m[1])
			if term == "" || len([]rune(term)) < minSpellingLen {
				continue
			}
			key := strings.ToLower(term)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, DefinedTerm{Term: term, Idiom: idiom, Document: name})
		}
	}
	collect(definedTermMeansRe, DefinitionIdiomMeans)
	collect(definedTermParentheticalRe, DefinitionIdiomParenthetical)
	return out
}

// cleanDefinedTerm strips the working form's emphasis markers and the
// punctuation a quotation can carry, and collapses internal whitespace.
func cleanDefinedTerm(raw string) string {
	s := markdownEmphasisRe.ReplaceAllString(raw, "")
	s = strings.Trim(s, " \t.,;:")
	return strings.Join(strings.Fields(s), " ")
}

// ApplyDefinedTerms adds every discovered defined term to the allowlist and
// returns how many were added.
//
// It mirrors ApplyRemovals exactly, and for the same reason: Allowlist.Contains
// is the single veto every span producer already consults, so a second
// negative-rule mechanism would be one more thing a producer could forget to
// ask. The terms stay a SEPARATE list in state, so "stop suppressing this term"
// is not the same gesture as "delete a never-anonymise term the user typed".
func ApplyDefinedTerms(allow *Allowlist, terms []DefinedTerm) int {
	if allow == nil {
		return 0
	}
	count := 0
	for _, t := range terms {
		if strings.TrimSpace(t.Term) == "" {
			continue
		}
		for _, form := range definedTermForms(t.Term) {
			allow.Add(form)
			count++
		}
	}
	return count
}

// definedTermForms returns the term plus the inflections a drafter writes it in:
// the plural and the possessive.
//
// A document that defines "Work Order" writes "Work Orders" and "the Work
// Order's" in the same breath, and they are the same vocabulary item. Without
// the inflections the suppressor removes one review row and leaves its plural
// sitting beside it, which reads as the suppressor not working.
//
// It stays a small closed transformation rather than a stemmer: only the LAST
// word is inflected, and only by the three English rules below, so the result is
// always a form a reader can recognise as the same term. Nothing here is a
// prefix rule: every form is matched WHOLE, which is what keeps "Services NStar"
// out of the suppressor while "Services" is in it.
func definedTermForms(term string) []string {
	forms := []string{term, term + "'s", term + "\u2019s"}
	fields := strings.Fields(term)
	if len(fields) == 0 {
		return forms
	}
	last := fields[len(fields)-1]
	lower := strings.ToLower(last)
	var plural string
	switch {
	case strings.HasSuffix(lower, "s") || strings.HasSuffix(lower, "x") ||
		strings.HasSuffix(lower, "z") || strings.HasSuffix(lower, "ch") ||
		strings.HasSuffix(lower, "sh"):
		plural = last + "es"
	case strings.HasSuffix(lower, "y") && len(lower) > 1 &&
		!strings.ContainsRune("aeiou", rune(lower[len(lower)-2])):
		plural = last[:len(last)-1] + "ies"
	default:
		plural = last + "s"
	}
	fields[len(fields)-1] = plural
	return append(forms, strings.Join(fields, " "))
}
