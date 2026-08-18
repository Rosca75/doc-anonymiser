// engine/removals.go — removal of Values from the session.
//
// When a user removes a Value, it is recorded as a SESSION EXCLUSION, separate
// from the allowlist but enforced by the same machinery, so it sticks across
// re-runs and is reversible. Removal is one action with three effects that
// cannot happen separately: prune the registry entry, drop the Value, and
// record the exclusion. The exclusion is the whole mechanism for a value a
// built-in pattern matched, which has no Value behind it at all.
package engine

import "strings"

// RemovedValue is one session exclusion: a Value the user removed after a run.
type RemovedValue struct {
	Category string `json:"category"`
	// MainText is the removed Value's main text, lower-cased: the exclusion is
	// enforced through the allowlist, which matches case-insensitively.
	MainText string `json:"mainText"`
	// Spellings are the removed Value's other forms, excluded with it. Removing
	// the main text alone would leave "M. Duval" being replaced after the user
	// removed "Marie Duval".
	Spellings []string `json:"spellings,omitempty"`
	// Placeholder is what the Value used to become. The NUMBER is not freed by a
	// removal: an export, a mapping CSV or a session file in which [PERSON_4]
	// means one person may already have left the machine.
	Placeholder string `json:"placeholder"`
}

// ApplyRemovals adds the removed values' mainText and spelling strings to
// the allowlist, so they will not be replaced. It returns the count of
// strings added.
//
// A removed Value must not show up as a term on the never-anonymise list, and
// "undo the removal" must not be the same gesture as "delete an allowlist term",
// so removals stay separate from the allowlist in state and in the session file
// while being ENFORCED by the same machinery: Allowlist.Contains is the single
// veto every span producer already consults.
func ApplyRemovals(allow *Allowlist, removed []RemovedValue) int {
	if allow == nil {
		return 0
	}
	count := 0
	for _, r := range removed {
		allow.Add(r.MainText)
		count++
		for _, v := range r.Spellings {
			allow.Add(v)
			count++
		}
	}
	return count
}

// FilterRemoved drops the Values an exclusion covers, so the on-screen Value
// count is honest rather than listing Values the allowlist will silently veto.
func FilterRemoved(values []Value, removed []RemovedValue) []Value {
	if len(removed) == 0 {
		return values
	}

	excluded := make(map[string]bool)
	for _, r := range removed {
		excluded[r.MainText] = true
		for _, spelling := range r.Spellings {
			excluded[strings.ToLower(spelling)] = true
		}
	}

	out := make([]Value, 0, len(values))
	for _, v := range values {
		if !excluded[strings.ToLower(v.MainText)] {
			out = append(out, v)
		}
	}
	return out
}
