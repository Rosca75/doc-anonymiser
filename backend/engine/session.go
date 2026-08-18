// engine/session.go — save/load of session state (CLAUDE.md §3/§5).
//
// A session file contains entities + allowlist + custom patterns +
// settings + the placeholder REGISTRY. The registry
// is the re-identification key: whoever holds the file can map
// placeholders back to real names. That is why saving is an explicit user
// action behind a warning (CLAUDE.md §5, "sensitive state stays in
// memory") — this module only does the (de)serialisation; the warning
// lives in the UI.
package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
)

// SessionVersion is the session file format. A file carrying any other version
// is REFUSED, never migrated.
//
// That is the whole policy, and it is the strict one on purpose: a session file
// holds the re-identification key, and a half-migrated one silently reassigns
// placeholders. The user finds out when two batches of the same engagement no
// longer agree with each other, which is far past the point of noticing.
//
// Bump it whenever a change makes a file this build writes unreadable by the
// previous one, or the other way round: an added field the loader can ignore is
// not a bump, but a renamed category, a retired category and a new field the
// pipeline depends on all are.
//
// Version history (reason for each bump, newest last):
//
//	v5: added the useNativeDetect/useAutoDetect settings (the "Smart detection"
//	    route split into a Native-detection master over the regex signals and an
//	    Auto-detection word-frequency pass). A v4 file has neither flag, and a v4
//	    reader would not know Native detection can be off, so the versions are not
//	    interchangeable.
//	v6: the entity shape changed twice. It lost excludedVariants, the per-value
//	    list of spellings the expansion had to suppress, and gained autoExpand,
//	    which freezes a value's spellings to exactly the ones shown instead. A v5
//	    file's exclusions have no meaning under the curated model: a v6 reader
//	    would drop them and silently start replacing spellings the user had
//	    removed. It also gained origin, the route that produced the value, which
//	    decides precedence when two routes claim the same text; a v5 file states
//	    none, and reading it as "declared" would promote every AI proposal in it.
const SessionVersion = 6

// SessionSettings mirrors the app settings worth persisting. The engine
// does not interpret them — they round-trip for app.go. The
// fields (categories, contextSize, useAI) are absent in v1 files; app.go
// treats zero values as "keep the current defaults".
type SessionSettings struct {
	Level       string            `json:"level"`
	Categories  CategorySelection `json:"categories,omitempty"`
	OllamaPort  int               `json:"ollamaPort"`
	Model       string            `json:"model"`
	ContextSize int               `json:"contextSize,omitempty"`
	Country     string            `json:"country,omitempty"`
	UseAI       bool              `json:"useAI,omitempty"`
	// UseSmartDetect is the offline detection route switch. It is
	// a POINTER because its default is TRUE: with a plain bool, "absent" and
	// "the user switched it off" are the same value, and the wrong reading of
	// the two silently changes what a restored session detects.
	UseSmartDetect *bool `json:"useSmartDetect,omitempty"`
	// UseNativeDetect and UseAutoDetect are the two halves the "Smart detection"
	// route split into. UseNativeDetect is the master over the regex signal
	// categories (pass 1); UseAutoDetect is the offline word-frequency pass.
	// Both are POINTERS for the same reason as UseSmartDetect: their default is
	// TRUE, so "absent" must be distinguishable from "the user switched it off".
	UseNativeDetect *bool `json:"useNativeDetect,omitempty"`
	UseAutoDetect   *bool `json:"useAutoDetect,omitempty"`
	// MinConfidence is the detection-confidence floor. Absent
	// in every session file written before, where it loads as 0,
	// which is exactly the "keep every detection" default: an older
	// session therefore reproduces its original behaviour.
	MinConfidence float32 `json:"minConfidence,omitempty"`
	// SmartDetect is the smart-detection tuning. A pointer
	// so "absent" (an older file) is distinguishable from "present and
	// all zeroes" (a user who deliberately turned every filter off): the
	// first fills the defaults, the second must be obeyed.
	SmartDetect *SmartDetectOptions `json:"smartDetect,omitempty"`
}

// Session is the complete persistable session state.
type Session struct {
	Version     int             `json:"version"`
	Entities    []Entity        `json:"entities"`
	AllowTerms  []string        `json:"allowTerms"`
	Patterns    []CustomPattern `json:"patterns"`
	Settings    SessionSettings `json:"settings"`
	// Registry is the exported mapping — the re-identification key.
	Registry []MappingEntry `json:"registry"`
	// PlaceholderOverrides holds the placeholders the USER renamed
	// keyed "category|lower-cased original" exactly as
	// Registry.Overrides produces them.
	//
	// It is additive and has NO migration path: a file without it
	// would be a version 1 file, and the loader refuses those. The renamed
	// placeholders themselves are already in Registry above; this field is what
	// tells a reloaded session which of them were deliberate, so saving again
	// does not quietly demote them to automatic assignments.
	PlaceholderOverrides map[string]string `json:"placeholderOverrides,omitempty"`
	// RemovedValues tracks values the user deleted from the session.
	// They must not appear in any run without explicit restoration.
	RemovedValues []RemovedValue `json:"removedValues,omitempty"`
	// RetiredPlaceholders tracks placeholders whose entries were
	// forgotten but whose numbers were never freed.
	RetiredPlaceholders []string `json:"retiredPlaceholders,omitempty"`
}

// SaveSession serialises a session to pretty-printed JSON (stable key
// order via struct fields — good for diffs and the equality test).
func SaveSession(s Session) ([]byte, error) {
	s.Version = SessionVersion
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("could not serialise the session: %w", err)
	}
	return out, nil
}

// LoadSession parses and validates a session file.
func LoadSession(raw []byte) (Session, error) {
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		return Session{}, fmt.Errorf(
			"the file is not a valid session file (%v), pick a .anonsession.json file saved by this application", err)
	}
	if s.Version != SessionVersion {
		// Refused, not migrated. The message says which
		// direction the mismatch goes, because the fix differs: an older file
		// needs re-creating, a newer one needs a newer application.
		direction := "an older version of this application"
		fix := "start a new session in this version and save it again"
		if s.Version > SessionVersion {
			direction = "a newer version of this application"
			fix = "update the application to open it"
		}
		return Session{}, fmt.Errorf(
			"this session file is version %d and this application reads version %d, "+
				"so it was written by %s. It is refused rather than partly loaded, "+
				"because a session file holds the re-identification key and a partly "+
				"restored one would quietly hand out different placeholders. To carry on: %s",
			s.Version, SessionVersion, direction, fix)
	}
	return s, nil
}

// placeholderParseRe splits "[LABEL_N]" into label and N for counter
// reconstruction on session load.
var placeholderParseRe = regexp.MustCompile(`^\[([A-Z][A-Z0-9_]*)_([0-9]+)\]$`)

// NewRegistryFromEntries rebuilds a live registry from exported mapping
// entries (session load). Counters resume from the highest N per label so
// new assignments continue the numbering instead of colliding.
//
// builds byOriginal and byPlaceholder indexes, and treats a duplicated
// original as a corrupt-file error.
//
// It RETURNS that error rather than panicking. This runs
// inside a bound method on a file the user picked, so a panic here takes the
// whole application down on a bad file, which is the opposite of the
// refuse-and-say-why policy every other load failure follows.
//
// @param entries the mapping rows from a session file
// @return the live registry, or an actionable error naming the corruption
func NewRegistryFromEntries(entries []MappingEntry) (*Registry, error) {
	r := NewRegistry()
	for _, e := range entries {
		entry := e // copy — the map keeps a pointer
		key := e.Category + "|" + lowered(e.Original)
		lowerOriginal := lowered(e.Original)

		// A duplicated original means the registry is corrupt: two entries own
		// the same value under different categories, breaking the
		// one-value-one-placeholder invariant. Nothing this application writes
		// can produce it, so the file has been edited or truncated.
		if existingKey, ok := r.byOriginal[lowerOriginal]; ok {
			// The FIRST entry's spelling is the one named, because that is the row
			// the user would recognise; a lower-cased duplicate reads as a
			// different value and sends them looking for the wrong thing.
			return nil, fmt.Errorf(
				"this session file is corrupt: the value %q appears twice in the "+
					"re-identification key, under the categories %q and %q, so there is no "+
					"single answer to what it was replaced with. It is refused rather than "+
					"half-loaded. Use an earlier copy of the session file, or start a new session",
				r.entries[existingKey].Original, r.entries[existingKey].Category, e.Category)
		}

		r.entries[key] = &entry
		r.byOriginal[lowerOriginal] = key
		r.byPlaceholder[e.Placeholder] = key

		if m := placeholderParseRe.FindStringSubmatch(e.Placeholder); m != nil {
			if n, err := strconv.Atoi(m[2]); err == nil && n > r.counters[m[1]] {
				r.counters[m[1]] = n
			}
		}
	}
	return r, nil
}

// NewRegistryFromSession rebuilds a live registry from a loaded session,
// including which placeholders the user renamed.
//
// The renamed placeholders are already in s.Registry, so this is not restoring
// them: it is restoring the KNOWLEDGE that they were deliberate, so a later
// save writes them out as overrides again instead of demoting them to
// automatic assignments.
//
// Overrides that no longer apply (their value is not in the loaded registry)
// are returned as failures rather than aborting the load: one stale entry must
// not cost the user the other twenty.
//
// A CORRUPT key, by contrast, aborts: an override that does not apply costs one
// renamed placeholder, while two entries claiming one value means the key cannot
// be read at all.
//
// @param s a session that LoadSession has already accepted
// @return the live registry, one error per override that did not apply, and a
//
//	fatal error when the key itself is unreadable
func NewRegistryFromSession(s Session) (*Registry, []error, error) {
	r, err := NewRegistryFromEntries(s.Registry)
	if err != nil {
		return nil, nil, err
	}

	// Retired placeholders restore with the entries. They are numbers this
	// session has already spent and are not recoverable from s.Registry,
	// because a Forget freed the entry and deliberately did NOT free the
	// number: the user may already hold an export in which [PERSON_4] means one
	// person. Dropping the set on load would hand 4 straight back out and make
	// two artefacts of one session disagree, which is the exact ambiguity the
	// refusal exists to prevent, arriving one save-and-reload later.
	//
	// The counters move up too, or the set would be remembered and then skipped
	// past one number at a time on every assignment.
	for _, p := range s.RetiredPlaceholders {
		r.retired[p] = true
		r.raiseCounterFor(p)
	}

	if len(s.PlaceholderOverrides) == 0 {
		return r, nil, nil
	}
	return r, r.ApplyOverrides(s.PlaceholderOverrides), nil
}

// raiseCounterFor advances the counter for a placeholder's label so the number
// it uses is never handed out again. Callers hold no lock: it is only used
// while a registry is still being built and nothing else can see it.
func (r *Registry) raiseCounterFor(placeholder string) {
	m := placeholderParseRe.FindStringSubmatch(placeholder)
	if m == nil {
		return
	}
	if n, err := strconv.Atoi(m[2]); err == nil && n > r.counters[m[1]] {
		r.counters[m[1]] = n
	}
}
