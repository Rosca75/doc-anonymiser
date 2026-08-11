// engine/removals.go — removal of values from the session (Phase 4).
//
// When a user removes a value, it is recorded as an exclusion separate from
// the allowlist (but enforced by the same machinery), so it sticks across
// re-runs and is reversible. Removal is one action with three effects:
// prune the registry entry, drop the Entity if the value was declarative or
// accepted, and record the exclusion.
package engine

import "strings"

// RemovedValue represents a value that was removed from the session.
// The placeholder is what it used to become.
type RemovedValue struct {
	Category    string   `json:"category"`
	Canonical   string   `json:"canonical"` // lower-cased original
	Variants    []string `json:"variants"`
	Placeholder string   `json:"placeholder"` // what it used to become
}

// ApplyRemovals adds the removed values' canonical and variant strings to
// the allowlist, so they will not be replaced. It returns the count of
// strings added.
//
// Phase 4: A removed value must not show up as a term on the Allow tab
// and "undo removal" must not be the same gesture as "delete an allowlist
// term", so removals stay separate from the allowlist in state and in the
// session file, but are enforced by the same machinery (Allowlist.Contains).
func ApplyRemovals(allow *Allowlist, removed []RemovedValue) int {
	if allow == nil {
		return 0
	}
	count := 0
	for _, r := range removed {
		// Add canonical to allowlist
		allow.Add(r.Canonical)
		count++
		// Add variants to allowlist
		for _, v := range r.Variants {
			allow.Add(v)
			count++
		}
	}
	return count
}

// FilterRemoved removes entities that match any removed value.
// Used in filterEntities so the on-screen value count is honest (Phase 4).
func FilterRemoved(entities []Entity, removed []RemovedValue) []Entity {
	if len(removed) == 0 {
		return entities
	}

	// Build a set of removed canonicals for fast lookup (lowercase)
	removedSet := make(map[string]bool)
	for _, r := range removed {
		removedSet[r.Canonical] = true
		for _, v := range r.Variants {
			removedSet[strings.ToLower(v)] = true
		}
	}

	// Filter out entities that match removed values
	out := make([]Entity, 0, len(entities))
	for _, e := range entities {
		if !removedSet[strings.ToLower(e.Canonical)] {
			out = append(out, e)
		}
	}
	return out
}
