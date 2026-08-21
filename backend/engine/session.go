// engine/session.go — save/load of session state (CLAUDE.md §3/§5).
//
// A session file contains the accepted Values + the never-anonymise list +
// custom patterns + settings + the placeholder REGISTRY. The registry is the
// re-identification key: whoever holds the file can map placeholders back to
// real values. That is why saving is an explicit user action behind a warning
// (CLAUDE.md §5, "sensitive state stays in memory"). This module only does the
// (de)serialisation; the warning lives in the UI.
package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	"doc-anonymiser/backend/engine/imaging"
)

// SessionVersion is the session file format. A file carrying any other version
// is REFUSED, never migrated.
//
// That is the whole policy, and it is the strict one on purpose: a session file
// holds the re-identification key, and a half-migrated one silently reassigns
// placeholders. The user finds out when two batches of the same engagement no
// longer agree with each other, which is far past the point of noticing. There
// is therefore no migration table and no compatibility alias anywhere in this
// file: the only accepted version is the one this build writes.
//
// Bump it whenever a change makes a file this build writes unreadable by the
// previous one, or the other way round: an added field the loader can ignore is
// not a bump, but a renamed field, a retired field and a new field the pipeline
// depends on all are.
//
// Version history (reason for each bump, newest last):
//
//	v5: added the two offline route sub-switches. A v4 file has neither, and a
//	    v4 reader would not know the built-in patterns can be off, so the
//	    versions are not interchangeable.
//	v6: the Value shape lost the per-value list of spellings the expansion had to
//	    suppress and gained a curated-spellings flag instead. A v5 file's
//	    exclusions have no meaning under the curated model: a v6 reader would
//	    drop them and silently start replacing spellings the user had removed.
//	v7: the whole domain vocabulary is the contract now, so nearly every field a
//	    v6 file carries is either renamed or gone. Values replace entities and
//	    are keyed on mainText and spellings; spellingPolicy replaces the
//	    autoExpand flag; discoveryMethods and evidence replace the single origin
//	    field, which had been answering both "how was this found" and "which
//	    claim wins" with one string; simpleRules and reservedPlaceholders are
//	    gone with the find-and-replace facility; and signalSuggestionSources is
//	    new and load-bearing, because a v6 file cannot say whether the user had
//	    switched signal-derived Suggestions off. Reading a v6 file field by field
//	    would mean guessing at every one of those, and each guess changes what
//	    the next run replaces.
//	v8: signalSuggestionSources is keyed by source AND derivation, because one
//	    signal supports several readings and each is switched on its own. A v7
//	    file holds one boolean per source, which no longer describes what runs: a
//	    v7 `{"email": true}` cannot say whether the user wanted people from local
//	    parts, organisations from domains, or both, and a v8 reader guessing
//	    "both" would produce Suggestions the user had switched off.
//	v9: the session carries the image treatments. A v8 file has none, and a v8
//	    READER silently ignores the field: it would load a session in which the
//	    user had boxed the client logo, export the .docx, and ship the logo. The
//	    strict-version rule exists for exactly this shape of failure, where the
//	    file loads, nothing errors, and the output is wrong. This is the
//	    borderline case the paragraph above describes: an added field the loader
//	    can ignore is normally not a bump, and this one IS one, because what the
//	    old reader ignores is a redaction.
//	v10: three value categories were added (country_names, nationality_names,
//	    business_sector_names) along with three pattern categories. This is a
//	    bump for a REVERSE-compatibility reason rather than a forward one:
//	    Registry.Assign PANICS on a category with no placeholderLabels row, so a
//	    v9 file written by this build carrying a country_names Value would be
//	    accepted by an older v9 binary and crash it on the next run. The bump
//	    turns that crash into the clear "written by a different version" refusal.
//	    The file also carries definedTerms, the vocabulary the imported documents
//	    declare about themselves, which is enforced through the allowlist: a v9
//	    reader ignoring it would restore a session and start suggesting every
//	    defined term again.
//	v11: the detection vocabulary's IDENTIFIERS moved with the labels, so one
//	    discovery method, two match classes and the three local-model settings
//	    keys are all spelled differently. Every one of them is written into a
//	    session file, so a v10 file states a Value's provenance and the user's
//	    route settings in words this build does not read: the methods come back
//	    EMPTY, which makes MatchClassForMethods rank the Value as user-defined and
//	    hands it a precedence it never had, and the three settings read as their
//	    zero values, silently switching the local route and its two options off.
//	    Both failures load cleanly and change what the next run replaces, which is
//	    exactly the shape the strict-version rule exists for. The old spellings are
//	    not written here: the current ones are the contract, and
//	    ../../vocabulary_guard_test.go is what keeps the retired ones out of the
//	    tree.
//	v12: `level` LEAVES the schema and `presets` enters it. A preset is scoped
//	    data now (presets.go): a single level string cannot say that the
//	    built-in pattern categories are at Soft while the name categories are at
//	    Thorough, which is a selection the scoped chips make in two clicks, and
//	    it has no room at all for a second preset family. The two are not
//	    readable as each other in either direction: a v11 reader finds no level
//	    and falls back to its default, silently moving the selection the user
//	    saved, and a v11 file's level names presets ("medium", "advanced") that
//	    no row in this build's table holds. The per-category selection is what a
//	    run obeys either way, so the failure is not in what the file replaces
//	    but in what the rail then SAYS it will replace, which is the shape of
//	    silent disagreement the strict-version rule exists for.
const SessionVersion = 12

// SessionSettings mirrors the app settings worth persisting. The engine does not
// interpret them: they round-trip for app.go.
type SessionSettings struct {
	// Presets records which chip each preset row was on, keyed
	// "<scope>.<family>" (presets.go PresetKey). FLAT rather than nested so a
	// family added later needs no schema change, and ABSENT rather than
	// defaulted so "Custom" is representable: a row whose selection matches no
	// preset stores no key at all.
	//
	// It is a record of the rail's state, not an instruction. Categories below
	// is what a run obeys, and the presets are derived from it on both sides,
	// so the two cannot disagree about what the next run will do.
	Presets     map[string]string `json:"presets,omitempty"`
	Categories  CategorySelection `json:"categories,omitempty"`
	OllamaPort  int               `json:"ollamaPort"`
	Model       string            `json:"model"`
	ContextSize int               `json:"contextSize,omitempty"`
	Country     string            `json:"country,omitempty"`
	// UseLocalLLM is the Local LLM discovery route switch.
	UseLocalLLM bool `json:"useLocalLLM,omitempty"`
	// LLMStrictFormat is the local model's discovery reply format: schema-constrained
	// when true, Ollama's loose JSON mode otherwise. A POINTER so "absent" and
	// "the user switched it off" stay distinguishable, exactly as the two method
	// switches below are pointers.
	//
	// It does NOT bump SessionVersion, and neither does any field of this shape.
	// A reader of an older file finds it absent, absent reads as off, and off is
	// the default the file was written under, so nothing is guessed and no
	// migration is needed. A bump is for a field whose OLD meaning cannot be
	// recovered, which is what version 7's per-source booleans were.
	LLMStrictFormat *bool `json:"llmStrictFormat,omitempty"`
	// LLMDetailLevel is how much text one local-model request carries
	// (DetailThorough or DetailFaster). A plain string rather than a pointer,
	// because absence and the default are the SAME thing here: an empty value
	// reads as thorough, which is what a file written without the field was
	// written under. No version bump, for the reason given above.
	LLMDetailLevel string `json:"llmDetailLevel,omitempty"`
	// UseBuiltInPatterns and UseHeuristicDiscovery are two of the offline
	// three methods. Both are POINTERS because their default is TRUE: with a
	// plain bool, "absent" and "the user switched it off" are the same value,
	// and the wrong reading of the two silently changes what a restored session
	// detects.
	//
	// There is deliberately no persisted switch summarising the routes.
	// The section is on when any of its methods is on, so a fourth boolean would
	// be a second way of saying something the three already say, and the two
	// could disagree.
	UseBuiltInPatterns    *bool `json:"useBuiltInPatterns,omitempty"`
	UseHeuristicDiscovery *bool `json:"useHeuristicDiscovery,omitempty"`
	// SignalSuggestionSources drives signal-based discovery: which readings of
	// which built-in signals may DERIVE Suggestions (signals.go), keyed by source
	// and then by derivation. It does not govern whether those signals are matched
	// and replaced, which is what Built-in patterns and the category's own switch
	// do.
	SignalSuggestionSources SignalSourceSelection `json:"signalSuggestionSources,omitempty"`
	// MinConfidence is the detection-confidence floor. Absent loads as 0, which
	// is exactly the "keep every detection" default.
	MinConfidence float32 `json:"minConfidence,omitempty"`
	// HeuristicDiscovery is the heuristic tuning. A pointer so "absent" is
	// distinguishable from "present and all zeroes" (a user who deliberately
	// turned every filter off): the first fills the defaults, the second must be
	// obeyed.
	HeuristicDiscovery *HeuristicDiscoveryOptions `json:"heuristicDiscovery,omitempty"`
}

// Session is the complete persistable session state.
type Session struct {
	Version    int             `json:"version"`
	Values     []Value         `json:"values"`
	AllowTerms []string        `json:"allowTerms"`
	Patterns   []CustomPattern `json:"patterns"`
	Settings   SessionSettings `json:"settings"`
	// Registry is the exported mapping — the re-identification key.
	Registry []MappingEntry `json:"registry"`
	// PlaceholderOverrides holds the placeholders the USER renamed
	// keyed "category|lower-cased original" exactly as
	// Registry.Overrides produces them.
	//
	// The renamed placeholders themselves are already in Registry above; this
	// field is what tells a reloaded session which of them were deliberate, so
	// saving again does not quietly demote them to automatic assignments.
	PlaceholderOverrides map[string]string `json:"placeholderOverrides,omitempty"`
	// RemovedValues is the session exclusion list: the Values the user removed.
	// They must not appear in any run without explicit restoration.
	RemovedValues []RemovedValue `json:"removedValues,omitempty"`
	// RetiredPlaceholders tracks placeholders whose entries were
	// forgotten but whose numbers were never freed.
	RetiredPlaceholders []string `json:"retiredPlaceholders,omitempty"`
	// ImageDecisions is document name -> asset ID -> decision. Only the
	// decisions that CHANGE a picture are stored, because keep is recorded as the
	// absence of a decision everywhere else too.
	ImageDecisions map[string]map[string]imaging.Decision `json:"imageDecisions,omitempty"`
	// DefinedTerms is the vocabulary the imported documents declare about
	// themselves (allowlist.go). It is stored SEPARATELY from AllowTerms, exactly
	// as RemovedValues is, because the two are different gestures: deleting a
	// term the user typed is not the same act as telling the application to stop
	// honouring a definition it read out of a document.
	//
	// It is restored rather than re-derived on load, because the user may have
	// deleted individual entries and a re-derivation would bring them straight
	// back.
	DefinedTerms []DefinedTerm `json:"definedTerms,omitempty"`
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
