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

// SessionVersion is bumped on breaking format changes so an old app can
// refuse a newer file with a clear message (and vice versa).
const SessionVersion = 1

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
	UseAI       bool              `json:"useAI,omitempty"`
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
		return Session{}, fmt.Errorf(
			"the session file has version %d but this application expects version %d, it was saved by a different application version; re-create the session or update the application", s.Version, SessionVersion)
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
