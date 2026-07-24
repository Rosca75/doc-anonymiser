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
	"strings"
)

// SessionVersion is bumped on breaking format changes so an old app can
// refuse a newer file with a clear message (and vice versa).
const SessionVersion = 1

// SessionSchema tracks CONTENT migrations within version 1 files
// (BUILD-02 Phase 3d). Schema 2 renamed the entity category
// `pwc_internal_names` to `internal_names` and the placeholder label
// `PWC_INTERNAL` to `INTERNAL`. Files without a schema field (v1 saves)
// are migrated on load; loading a schema-2 file is a no-op migration.
const SessionSchema = 2

// SessionSettings mirrors the app settings worth persisting. The engine
// does not interpret them — they round-trip for app.go.
type SessionSettings struct {
	Level      string `json:"level"`
	OllamaPort int    `json:"ollamaPort"`
	Model      string `json:"model"`
}

// Session is the complete persistable session state.
type Session struct {
	Version int `json:"version"`
	// Schema is the content-migration marker (see SessionSchema). Old
	// files omit it (0), which triggers the rename migration on load.
	Schema      int             `json:"schema"`
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
	s.Schema = SessionSchema
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
	migrateSession(&s)
	return s, nil
}

// migrateSession upgrades older session content in place (BUILD-02
// Phase 3d). Schema < 2: the `pwc_internal_names` category becomes
// `internal_names` in entities and registry rows, and registry
// placeholders `[PWC_INTERNAL_N]` become `[INTERNAL_N]` with the counter
// N preserved, so a re-run reproduces identical numbering. User data is
// NEVER dropped; unknown future schemas load as-is (the version gate
// above already rejected incompatible files).
func migrateSession(s *Session) {
	if s.Schema >= SessionSchema {
		return // already current: no-op migration
	}
	for i := range s.Entities {
		if s.Entities[i].Category == "pwc_internal_names" {
			s.Entities[i].Category = "internal_names"
		}
	}
	for i := range s.Registry {
		if s.Registry[i].Category == "pwc_internal_names" {
			s.Registry[i].Category = "internal_names"
		}
		if rest, ok := strings.CutPrefix(s.Registry[i].Placeholder, "[PWC_INTERNAL_"); ok {
			s.Registry[i].Placeholder = "[INTERNAL_" + rest
		}
	}
	s.Schema = SessionSchema
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
