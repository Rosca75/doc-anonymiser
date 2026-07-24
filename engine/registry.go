// engine/registry.go — the placeholder registry (CLAUDE.md §5).
//
// The registry guarantees CONSISTENT pseudonyms: the same real-world value
// always maps to the same placeholder within a session, across every
// document and every pass ("marie.duval@example.com" is [EMAIL_1]
// everywhere, in doc A and doc B alike). Lookups are case-insensitive so
// "Alpine Trust" and "ALPINE TRUST" share one placeholder.
//
// Placeholder format: [CATEGORY_N] — e.g. [CLIENT_1], [PERSON_3], [EMAIL_2].
// The registry is exportable as the re-identification key (CSV/JSON export
// in Phase 9), which is why it also tracks occurrence counts.
package engine

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// placeholderLabels maps engine category identifiers to the label used
// inside placeholders. Categories without an entry fall back to the
// upper-cased identifier, so a future category degrades gracefully instead
// of breaking.
var placeholderLabels = map[string]string{
	// Entity categories (CLAUDE.md §5).
	"client_names":       "CLIENT",
	"project_names":      "PROJECT",
	"internal_names": "INTERNAL",
	"person_names":       "PERSON",
	"custom_patterns":    "CUSTOM",
	"organisation_names": "ORG",
	"location_names":     "LOCATION",
	// PII categories (pass 1).
	CatEmail:     "EMAIL",
	CatPhone:     "PHONE",
	CatIBAN:      "IBAN",
	CatVAT:       "VAT",
	CatMatricule: "NATIONAL_ID",
	CatURL:       "URL",
	CatAmount:    "AMOUNT",
	CatDate:      "DATE",
	// BUILD-03 Phase B — extended recognizers.
	CatCreditCard:  "CREDIT_CARD",
	CatNHS:         "NHS",
	CatIPAddress:   "IP",
	CatMACAddress:  "MAC",
	CatCrypto:      "CRYPTO",
	CatDatabaseURI: "DB_URI",
	CatDESteuerID:  "DE_TAX_ID",
	CatESNIF:       "ES_NIF",
}

// MappingEntry is one row of the exported re-identification key.
type MappingEntry struct {
	Original    string `json:"original"`
	Placeholder string `json:"placeholder"`
	Category    string `json:"category"`
	Count       int    `json:"count"` // how many occurrences were replaced
}

// Registry assigns and remembers placeholders. Safe for concurrent use —
// the pipeline runs in a goroutine (Phase 8) while the UI may export the
// mapping.
type Registry struct {
	mu sync.Mutex
	// counters tracks the next N per placeholder label (EMAIL → 3 means
	// [EMAIL_3] was the last one handed out).
	counters map[string]int
	// entries is keyed by category + lower-cased original, giving the
	// case-insensitive stable lookup.
	entries map[string]*MappingEntry
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		counters: map[string]int{},
		entries:  map[string]*MappingEntry{},
	}
}

// Assign returns the stable placeholder for (category, original), creating
// a new one on first sight and bumping the occurrence count every time.
// This is the function handed to ApplySpans by the pipeline.
func (r *Registry) Assign(category, original string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := category + "|" + strings.ToLower(original)
	if e, ok := r.entries[key]; ok {
		e.Count++
		return e.Placeholder
	}

	label, ok := placeholderLabels[category]
	if !ok {
		// Unknown category: degrade to its upper-cased identifier rather
		// than failing — the placeholder stays readable either way.
		label = strings.ToUpper(category)
	}
	r.counters[label]++
	e := &MappingEntry{
		Original:    original,
		Placeholder: fmt.Sprintf("[%s_%d]", label, r.counters[label]),
		Category:    category,
		Count:       1,
	}
	r.entries[key] = e
	return e.Placeholder
}

// Lookup returns the placeholder already assigned to (category, original)
// without creating one; ok is false when it was never seen. Used by the
// post-pass (Phase 4) to re-apply known mappings across all documents.
func (r *Registry) Lookup(category, original string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[category+"|"+strings.ToLower(original)]
	if !ok {
		return "", false
	}
	return e.Placeholder, true
}

// Export returns the full mapping sorted by category then placeholder
// number — the deterministic order used for the CSV/JSON key export and
// for golden tests.
func (r *Registry) Export() []MappingEntry {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]MappingEntry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		// Placeholders of one category share a label, so string length
		// then value sorts [X_2] before [X_10] correctly.
		if len(out[i].Placeholder) != len(out[j].Placeholder) {
			return len(out[i].Placeholder) < len(out[j].Placeholder)
		}
		return out[i].Placeholder < out[j].Placeholder
	})
	return out
}

// Entries returns every known (original → placeholder) pair, longest
// original first — the order the post-pass wants for safe re-application
// (longer strings must be replaced before their substrings).
func (r *Registry) Entries() []MappingEntry {
	out := r.Export()
	sort.Slice(out, func(i, j int) bool {
		return len(out[i].Original) > len(out[j].Original)
	})
	return out
}
