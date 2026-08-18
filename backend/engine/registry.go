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
// which is why it also tracks occurrence counts.
package engine

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// placeholderLabels maps engine category identifiers to the label used
// inside placeholders. Every shipped category MUST have an explicit row:
// silently falling back to the upper-cased identifier would mint a placeholder
// shape the tests and the UI were never taught about.
var placeholderLabels = map[string]string{
	// Entity categories (CLAUDE.md §5).
	CatEntityNames:     "ENTITY",
	CatProjectNames:    "PROJECT",
	CatProductNames:    "PRODUCT",
	CatBrandNames:      "BRAND",
	CatPersonNames:     "PERSON",
	CatIdentifierNames: "ID",
	CatOtherNames:      "OTHER",
	CatCustomPatterns:  "CUSTOM",
	// PII categories (pass 1).
	CatEmail:     "EMAIL",
	CatPhone:     "PHONE",
	CatIBAN:      "IBAN",
	CatVAT:       "VAT",
	CatMatricule: "NATIONAL_ID",
	CatURL:       "URL",
	CatAmount:    "AMOUNT",
	CatDate:      "DATE",
	// extended recognizers.
	CatCreditCard:  "CREDIT_CARD",
	CatNHS:         "NHS",
	CatIPAddress:   "IP",
	CatMACAddress:  "MAC",
	CatCrypto:      "CRYPTO",
	CatDatabaseURI: "DB_URI",
	CatDESteuerID:  "DE_TAX_ID",
	CatESNIF:       "ES_NIF",
}

// PlaceholderLabel returns the label a category's placeholders carry, e.g.
// "PERSON" for person_names, or "" for a category this build does not know.
//
// It exists so callers outside the engine (the App's rule-placeholder minting)
// can build a placeholder in the SAME shape the registry assigns, rather than
// upper-casing the identifier and getting [PERSON_NAMES_1] right up until
// something compares the two.
//
// @param category an engine category identifier
// @return the placeholder label, or "" when the category is unknown
func PlaceholderLabel(category string) string {
	return placeholderLabels[category]
}

// MappingEntry is one row of the exported re-identification key.
type MappingEntry struct {
	Original    string `json:"original"`
	Placeholder string `json:"placeholder"`
	Category    string `json:"category"`
	Count       int    `json:"count"` // how many occurrences were replaced
}

// Registry assigns and remembers placeholders. Safe for concurrent use —
// the pipeline runs in a goroutine while the UI may export the
// mapping.
type Registry struct {
	mu sync.Mutex
	// counters tracks the next N per placeholder label (EMAIL → 3 means
	// [EMAIL_3] was the last one handed out).
	counters map[string]int
	// entries is keyed by category + lower-cased original, giving the
	// case-insensitive stable lookup.
	entries map[string]*MappingEntry
	// overrides records which entries the USER renamed,
	// keyed exactly like entries. It is recorded rather than inferred because
	// no inference is reliable: renaming [CLIENT_1] to [CLIENT_9] is a
	// deliberate override that looks exactly like an automatic assignment.
	// This is what the session file persists so a reloaded session keeps the
	// user's names.
	overrides map[string]string
	// byOriginal maps lower(original) to the SINGLE entry key that
	// owns it, so one real-world string can only ever have ONE placeholder,
	// whatever category a later pass thinks it belongs to. Assign checks it
	// before minting a second.
	byOriginal map[string]string
	// byPlaceholder maps placeholder to entry key (replaces the O(n)
	// scan in placeholderTakenLocked).
	byPlaceholder map[string]string
	// retired tracks placeholders whose ENTRY a Forget freed but whose
	// NUMBER it did not: the user may already have exported a document with
	// [PERSON_4] meaning one person, and handing 4 to somebody else would make
	// two artefacts of one session disagree with nothing able to detect it.
	retired map[string]bool
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		counters:      map[string]int{},
		entries:       map[string]*MappingEntry{},
		overrides:     map[string]string{},
		byOriginal:    map[string]string{},
		byPlaceholder: map[string]string{},
		retired:       map[string]bool{},
	}
}

// Assign returns the stable placeholder for (category, original), creating
// a new one on first sight and bumping the occurrence count every time.
// This is the function handed to ApplySpans by the pipeline.
//
// The invariant: if byOriginal already owns the string under another
// category, return the existing placeholder and bump its count rather than
// minting a second. A string that is both an email and a declared entity gets
// ONE placeholder.
//
// WHICH category owns it is decided UPSTREAM, by ownership unification
// (pipeline.go unifyOwnership), which picks the winning route by OriginRank
// before a single placeholder is minted. This function is deliberately not
// precedence-aware: changing an entry's category after the fact would change
// its placeholder text, and a placeholder that has already left the machine,
// in an export, a mapping CSV or a session file, can never be re-numbered
// (CLAUDE.md section 5). So the first claimant keeps the entry, and it is
// unification's job to make sure the first claimant is the right one.
func (r *Registry) Assign(category, original string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	lowerOriginal := strings.ToLower(original)
	// Check if this string already has a placeholder under another category
	if existingKey, ok := r.byOriginal[lowerOriginal]; ok {
		e := r.entries[existingKey]
		e.Count++
		return e.Placeholder
	}

	key := category + "|" + lowerOriginal
	if e, ok := r.entries[key]; ok {
		e.Count++
		return e.Placeholder
	}

	label, ok := placeholderLabels[category]
	if !ok {
		panic("engine placeholder label missing for category " + category)
	}
	// Hand out the next number for this label, SKIPPING any that a user
	// override has already taken, or that a retired placeholder has used. Renaming [CLIENT_1] to [CLIENT_5] means
	// the fifth automatic client must not also become [CLIENT_5]: two
	// originals sharing one placeholder makes the re-identification key
	// ambiguous, which silently destroys the ability to undo the
	// anonymisation. Advancing the counter is the fix, so the numbers simply
	// have a gap.
	var placeholder string
	for {
		r.counters[label]++
		placeholder = fmt.Sprintf("[%s_%d]", label, r.counters[label])
		if !r.placeholderTakenLocked(placeholder) {
			break
		}
	}
	e := &MappingEntry{
		Original:    original,
		Placeholder: placeholder,
		Category:    category,
		Count:       1,
	}
	r.entries[key] = e
	r.byOriginal[lowerOriginal] = key
	r.byPlaceholder[placeholder] = key
	return e.Placeholder
}

// placeholderTakenLocked reports whether a placeholder is taken by an entry
// or retired. Caller holds r.mu.
func (r *Registry) placeholderTakenLocked(placeholder string) bool {
	_, inEntries := r.byPlaceholder[placeholder]
	_, inRetired := r.retired[placeholder]
	return inEntries || inRetired
}

// Lookup returns the placeholder already assigned to (category, original)
// without creating one; ok is false when it was never seen. Used by the
// post-pass to re-apply known mappings across all documents.
//
// resolves through byOriginal when the exact category key misses,
// because one string can only own one placeholder across all categories.
func (r *Registry) Lookup(category, original string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	lowerOriginal := strings.ToLower(original)
	// First try the exact category key
	key := category + "|" + lowerOriginal
	if e, ok := r.entries[key]; ok {
		return e.Placeholder, true
	}
	// if not found under this category, check if byOriginal owns it
	// under another category
	if existingKey, ok := r.byOriginal[lowerOriginal]; ok {
		e := r.entries[existingKey]
		return e.Placeholder, true
	}
	return "", false
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

// placeholderShapeRe is the [NAME_N] shape a placeholder must have.
//
// It matches:  [CLIENT_1]  [PERSON_12]  [DE_TAX_ID_3]  [ACME_CORP_1]
// It does NOT match:
//
//	CLIENT_1 no brackets, so it would not stand out in the text
//	[client_1]    lower case, so it would read as ordinary prose
//	[CLIENT]      no number, so two clients could not be told apart
//	[CLIENT_0]    zero, which no automatic assignment ever hands out
//	[CLIENT 1]    a space, which would let a line break split it in a docx
//	[CLIENT_1] x trailing text, because the whole value is the placeholder
//
// The shape is enforced rather than merely encouraged because the placeholder
// has to survive a round trip through the export formats AND be findable again
// by the re-identification key. A custom placeholder that a docx rewrite splits
// across two runs is not recoverable.
var placeholderShapeRe = regexp.MustCompile(`^\[[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)*_[1-9][0-9]*\]$`)

// SetPlaceholder overrides the placeholder for one (category, original) pair,
// because "[CLIENT_1]" is sometimes less useful downstream than "[BANK_A_1]".
//
// Three things are refused, each with a message that says what to do instead:
//
//	a malformed placeholder   the shape has to survive export and re-import
//	a collision               two originals sharing one placeholder would make
//	                          the re-identification key ambiguous, which
//	                          silently destroys the ability to undo the
//	                          anonymisation
//	an unknown pair           nothing to override yet
//
// The last one is the reason this does not create an entry: an override is an
// edit to something the registry already assigned, and inventing an entry here
// would let a typo in the category quietly create a second mapping that no pass
// ever uses.
//
// The occurrence count is preserved: the override renames the placeholder, it
// does not reset what has been counted.
//
// @param category the engine category identifier
// @param original the real-world value whose placeholder is being renamed
// @param placeholder the new placeholder, in [NAME_N] form
// @return an actionable error, or nil
func (r *Registry) SetPlaceholder(category, original, placeholder string) error {
	next := strings.TrimSpace(placeholder)
	if !placeholderShapeRe.MatchString(next) {
		return fmt.Errorf(
			"%q is not a usable placeholder: it must look like [NAME_1], "+
				"square brackets around an upper-case name, an underscore and a number "+
				"from 1 upwards (for example [CLIENT_1] or [BANK_A_2]). "+
				"Lower case, spaces and a missing number are all refused because the "+
				"placeholder has to survive being written into a Word or PowerPoint file "+
				"and being found again by the re-identification key",
			placeholder)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	lowerOriginal := strings.ToLower(original)
	key := category + "|" + lowerOriginal
	entry, ok := r.entries[key]
	if !ok {
		return fmt.Errorf(
			"there is no placeholder to rename for %q in category %q yet: "+
				"run the anonymisation once so the value is assigned a placeholder, "+
				"then edit it",
			original, category)
	}
	if entry.Placeholder == next {
		// Already what was asked for, so this is not a collision with itself.
		// It still counts as an override: this is the path a session load takes,
		// where the saved registry already carries the renamed placeholder and
		// re-applying it is what repopulates the override set.
		r.recordOverrideLocked(key, next)
		return nil
	}

	// A collision check across the WHOLE registry, not just this category,
	// and against retired placeholders.
	for otherKey, other := range r.entries {
		if otherKey == key {
			continue
		}
		if other.Placeholder == next {
			return fmt.Errorf(
				"%s is already the placeholder for %q: two values cannot share one "+
					"placeholder, because the re-identification key would no longer say "+
					"which original a placeholder came from. Pick a different name or a "+
					"different number",
				next, other.Original)
		}
	}
	if r.retired[next] {
		return fmt.Errorf(
			"%s is a retired placeholder: the number was already used, and re-using it "+
				"would make earlier artefacts disagree with the re-identification key. "+
				"Pick a different number",
			next)
	}

	// Update byPlaceholder: remove old placeholder, add new one
	delete(r.byPlaceholder, entry.Placeholder)
	r.byPlaceholder[next] = key

	entry.Placeholder = next
	r.recordOverrideLocked(key, next)
	return nil
}

// recordOverrideLocked remembers that an entry's placeholder was user-chosen.
// Caller holds r.mu.
func (r *Registry) recordOverrideLocked(key, placeholder string) {
	if r.overrides == nil {
		// A registry built without NewRegistry has no map yet. Tolerating that
		// here beats a nil-map panic in a save path.
		r.overrides = map[string]string{}
	}
	r.overrides[key] = placeholder
}

// Overrides returns the placeholders the USER renamed, keyed by
// "category|original" exactly as the registry keys its entries.
//
// This is what the session file stores: the whole registry
// is already saved, so the overrides ride along as an additive field and no
// migration is written for them.
//
// The set is RECORDED as SetPlaceholder runs rather than inferred afterwards.
// Inferring it would mean asking "does this placeholder look like something
// automatic assignment would have produced?", and the answer is unreliable in
// exactly the ordinary case: renaming [CLIENT_1] to [CLIENT_9] is a deliberate
// override that any such guess would classify as automatic.
//
// @return category|original to placeholder, for user-renamed entries only
func (r *Registry) Overrides() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make(map[string]string, len(r.overrides))
	for key, placeholder := range r.overrides {
		out[key] = placeholder
	}
	return out
}

// ApplyOverrides re-applies saved placeholder overrides after a session load.
//
// Failures are COLLECTED rather than aborting on the first one: a session file
// carrying one stale override (its value no longer appears in the documents)
// should still restore the other twenty. The caller decides what to tell the
// user; returning the reasons rather than a count means it can say which ones
// did not take.
//
// @param overrides category|original to placeholder, as Overrides produced
// @return one error per override that could not be applied
func (r *Registry) ApplyOverrides(overrides map[string]string) []error {
	var failures []error
	for key, placeholder := range overrides {
		category, original, ok := strings.Cut(key, "|")
		if !ok {
			failures = append(failures, fmt.Errorf(
				"the saved placeholder override %q is malformed: it should be "+
					"\"category|original\". The session file may have been edited by hand",
				key))
			continue
		}
		// The key stores the original lower-cased, which is exactly how the
		// registry keys its entries, so SetPlaceholder finds it.
		if err := r.SetPlaceholder(category, original, placeholder); err != nil {
			failures = append(failures, err)
		}
	}
	return failures
}

// Entries returns every known (original → placeholder) pair, longest
// original first — the order the post-pass wants for safe re-application
// (longer strings must be replaced before their substrings).
func (r *Registry) Entries() []MappingEntry {
	out := r.Export()
	sort.SliceStable(out, func(i, j int) bool {
		return len(out[i].Original) > len(out[j].Original)
	})
	return out
}

// Forget drops the mapping so the key stops describing a replacement that no
// longer happens. THE NUMBER IS NOT FREED: the user may already
// have exported a document, a mapping CSV or a session in which [PERSON_4]
// means one person, and handing 4 to somebody else would make two artefacts
// of one session disagree with nothing able to detect it. Same ambiguity
// SetPlaceholder's collision refusal exists to prevent.
//
// Forget returns the removed entry (to allow RestoreValue to use it) and a
// bool indicating whether the entry was found.
func (r *Registry) Forget(category, original string) (MappingEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	lowerOriginal := strings.ToLower(original)
	key := category + "|" + lowerOriginal
	entry, ok := r.entries[key]
	if !ok {
		return MappingEntry{}, false
	}

	// Record the placeholder as retired
	r.retired[entry.Placeholder] = true

	// Clean up indexes
	delete(r.entries, key)
	delete(r.byOriginal, lowerOriginal)
	delete(r.byPlaceholder, entry.Placeholder)

	// Clean up overrides if any
	delete(r.overrides, key)

	return *entry, true
}

// Rename updates a placeholder by its current value, addressed by placeholder
// because on step 3 the user is looking at report rows and at marks in the
// Compare pane, and both carry the placeholder. This is a
// SetPlaceholder wrapper that looks up the entry by placeholder instead of by
// category and original, so the whole collision refusal lives in one place.
//
// The lookup takes the lock and gives it back BEFORE calling SetPlaceholder,
// which takes it again. It is written as an explicit unlock rather than a
// deferred one on purpose: a `defer r.mu.Unlock()` here unlocks a mutex
// SetPlaceholder has already released, and Go answers an unlock of an unlocked
// mutex with an unrecoverable fatal error, not a panic a caller could contain.
// Every rename crashed the application until, with no test
// calling this function to notice.
//
// @param current the placeholder as it stands today, in [NAME_N] form
// @param next the placeholder the user wants instead
// @return an actionable error the UI shows verbatim, or nil
func (r *Registry) Rename(current, next string) error {
	r.mu.Lock()
	key, ok := r.byPlaceholder[current]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf(
			"the placeholder %q is not one this session assigned, so there is nothing to rename. "+
				"Pick a placeholder from the replaced-values list", current)
	}
	entry := r.entries[key]
	category, original := entry.Category, entry.Original
	r.mu.Unlock()

	return r.SetPlaceholder(category, original, next)
}

// Retired returns all retired placeholders.
func (r *Registry) Retired() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]string, 0, len(r.retired))
	for p := range r.retired {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// PlaceholderOwner returns the MappingEntry that owns a placeholder
// or an empty entry and false if not found.
func (r *Registry) PlaceholderOwner(placeholder string) (MappingEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key, ok := r.byPlaceholder[placeholder]
	if !ok {
		return MappingEntry{}, false
	}
	return *r.entries[key], true
}

// OwnerCategory returns the category that owns an original string
// or empty string and false if not found.
func (r *Registry) OwnerCategory(original string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	lowerOriginal := strings.ToLower(original)
	key, ok := r.byOriginal[lowerOriginal]
	if !ok {
		return "", false
	}
	return r.entries[key].Category, true
}
