// engine/session.go — save/load of session state (CLAUDE.md §3/§5).
//
// A session file contains entities + allowlist + custom patterns +
// simple-replace rules + settings + the placeholder REGISTRY. The registry
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

// SessionVersion is bumped on breaking format changes so a file this build does
// not understand is REFUSED rather than half-read.
//
// BUILD-05 decision 1 made that the whole policy: session files are read only
// by the version that wrote them. Version 2 adds PlaceholderOverrides, and
// rather than write a migration that guesses what a version 1 file meant, the
// loader refuses it and says so. Half-migrating a file that carries the
// re-identification key is the one failure mode worth being strict about: a
// partially-restored registry silently reassigns placeholders, and the user
// finds out when their two batches no longer agree.
// Version 3 (BUILD-06) merges the client_names and internal_names categories
// into entity_names. A version 2 file names categories this build no longer
// has, so it is refused rather than loaded with entities the pipeline would
// silently drop.
const SessionVersion = 3

// SessionSettings mirrors the app settings worth persisting. The engine
// does not interpret them — they round-trip for app.go. The BUILD-02
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
	// UseSmartDetect is the offline detection route switch (BUILD-06). It is
	// a POINTER because its default is TRUE: with a plain bool, "absent" and
	// "the user switched it off" are the same value, and the wrong reading of
	// the two silently changes what a restored session detects.
	UseSmartDetect *bool `json:"useSmartDetect,omitempty"`
	// MinConfidence is the BUILD-04 CR9 detection-confidence floor. Absent
	// in every session file written before BUILD-04, where it loads as 0,
	// which is exactly the "keep every detection" default: an older
	// session therefore reproduces its original behaviour.
	MinConfidence float32 `json:"minConfidence,omitempty"`
	// SmartDetect is the BUILD-04 CR13 smart-detection tuning. A pointer
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
	SimpleRules []SimpleRule    `json:"simpleRules"`
	Settings    SessionSettings `json:"settings"`
	// Registry is the exported mapping — the re-identification key.
	Registry []MappingEntry `json:"registry"`
	// PlaceholderOverrides holds the placeholders the USER renamed
	// (BUILD-05 Phase 3), keyed "category|lower-cased original" exactly as
	// Registry.Overrides produces them.
	//
	// It is additive and has NO migration path (decision 1): a file without it
	// would be a version 1 file, and the loader refuses those. The renamed
	// placeholders themselves are already in Registry above; this field is what
	// tells a reloaded session which of them were deliberate, so saving again
	// does not quietly demote them to automatic assignments.
	PlaceholderOverrides map[string]string `json:"placeholderOverrides,omitempty"`
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
		// Refused, not migrated (BUILD-05 decision 1). The message says which
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
func NewRegistryFromEntries(entries []MappingEntry) *Registry {
	r := NewRegistry()
	for _, e := range entries {
		entry := e // copy — the map keeps a pointer
		r.entries[e.Category+"|"+lowered(e.Original)] = &entry
		if m := placeholderParseRe.FindStringSubmatch(e.Placeholder); m != nil {
			if n, err := strconv.Atoi(m[2]); err == nil && n > r.counters[m[1]] {
				r.counters[m[1]] = n
			}
		}
	}
	return r
}

// NewRegistryFromSession rebuilds a live registry from a loaded session,
// including which placeholders the user renamed (BUILD-05 Phase 3).
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
// @param s a session that LoadSession has already accepted
// @return the live registry, and one error per override that did not apply
func NewRegistryFromSession(s Session) (*Registry, []error) {
	r := NewRegistryFromEntries(s.Registry)
	if len(s.PlaceholderOverrides) == 0 {
		return r, nil
	}
	return r, r.ApplyOverrides(s.PlaceholderOverrides)
}
