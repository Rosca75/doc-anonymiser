// engine/allowlist.go — terms that are NEVER anonymised (CLAUDE.md §5:
// "Allowlist wins: an allowlisted term is never replaced, by any pass").
//
// The default seed covers the public, non-identifying vocabulary of the
// owner's domain: financial regulators, common methodologies/standards and
// country names — words that look like organisation or location entities
// to the passes but identify nobody. Users add and remove terms in the UI;
// the seed itself can be removed for a session too (per-term delete).
package engine

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

// defaultAllowlist is the documented seed (BUILD.md Phase 3 activity 2).
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
// UI can show them at startup (BUILD-02 Phase 4b). The user sees every
// seeded term, can remove any, and the UI-driven list stays the single
// runtime source; nothing is applied silently.
func DefaultAllowlistTerms() []string {
	out := make([]string, len(defaultAllowlist))
	copy(out, defaultAllowlist)
	sort.Strings(out)
	return out
}

// ParseAllowlistCSV parses a user-supplied CSV of never-anonymise terms
// (BUILD-02 Phase 4a). Tolerant by design:
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

// AllowlistTemplateCSV returns the downloadable template (BUILD-02
// Phase 4a): a commented explanation, the "term" header and three example
// rows. The template parses through ParseAllowlistCSV (round-trip test).
func AllowlistTemplateCSV() []byte {
	return []byte(`# Allowlist template for doc-anonymiser.
# One term per row in the "term" column. These terms are never anonymised.
# Lines starting with # are ignored.
term
CSSF
IFRS 17
Luxembourg
`)
}

// Allowlist is a case-insensitive term set, safe for concurrent use (the
// pipeline goroutine reads it while the UI may edit it).
type Allowlist struct {
	mu sync.RWMutex
	// terms maps the lower-cased term to its display spelling, so the UI
	// lists "CSSF" (as seeded/typed) rather than "cssf".
	terms map[string]string
}

// NewAllowlist returns an allowlist pre-seeded with the defaults.
func NewAllowlist() *Allowlist {
	a := &Allowlist{terms: map[string]string{}}
	for _, t := range defaultAllowlist {
		a.Add(t)
	}
	return a
}

// NewEmptyAllowlist returns an allowlist with no terms (used by tests and
// by session loading, which restores the user's exact term set).
func NewEmptyAllowlist() *Allowlist {
	return &Allowlist{terms: map[string]string{}}
}

// Add inserts a term (case-insensitive; re-adding updates the display
// spelling). Empty strings are ignored.
func (a *Allowlist) Add(term string) {
	term = strings.TrimSpace(term)
	if term == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.terms[strings.ToLower(term)] = term
}

// Remove deletes a term (case-insensitive). Removing an unknown term is a
// no-op — the end state is what the user asked for either way.
func (a *Allowlist) Remove(term string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.terms, strings.ToLower(strings.TrimSpace(term)))
}

// Contains reports whether the term is allowlisted, ignoring case and
// surrounding whitespace. A nil allowlist contains nothing, so passes can
// call it without nil checks.
func (a *Allowlist) Contains(term string) bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	_, ok := a.terms[strings.ToLower(strings.TrimSpace(term))]
	return ok
}

// Terms returns the display spellings, alphabetically sorted (stable UI
// listing and deterministic session files).
func (a *Allowlist) Terms() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]string, 0, len(a.terms))
	for _, display := range a.terms {
		out = append(out, display)
	}
	sort.Strings(out)
	return out
}

// FilterAllowed drops every span whose matched text is allowlisted. It is
// the shared guard applied to PII, entity, custom-pattern and LLM spans
// alike — the single place enforcing "allowlist wins" for span producers
// that do not check the allowlist themselves.
func FilterAllowed(spans []Span, allow *Allowlist) []Span {
	if allow == nil {
		return spans
	}
	out := spans[:0]
	for _, s := range spans {
		if !allow.Contains(s.Original) {
			out = append(out, s)
		}
	}
	return out
}
