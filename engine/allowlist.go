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
