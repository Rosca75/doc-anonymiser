// engine/presets.go — presets as scoped DATA rather than as a branch in a
// function (CLAUDE.md §5).
//
// A preset is a SHORTCUT and nothing more. The pipeline obeys CategorySelection
// and reads no preset at run time: a chip WRITES that map, and the map is the
// authority from then on. That is what makes this file safe to grow.
//
// Four rules carry the model, and each one is why a piece of this is shaped the
// way it is.
//
//  1. SCOPE is what keeps the rail's sections separate. Applying a preset writes
//     only the categories in its own scope and leaves every category outside it
//     exactly as it was, so a chip under Built-in patterns can no longer reach a
//     checkbox under Heuristic discovery. A Categories entry outside its own
//     scope is a defect, and presets_test.go fails the build on it rather than
//     letting ApplyPreset ignore the key in silence.
//
//  2. A FAMILY is a chip row. Each rail section renders one row per family that
//     has entries in that section's scope, so adding a family adds a row and
//     introduces no new concept in the interface.
//
//  3. A preset spanning both mechanisms is declared TWICE, once per scope,
//     sharing an ID and a Label. Sharing the ID across scopes is deliberate: it
//     is what lets the two rows be recognised as the same regulation while each
//     instance fills only its own section's categories.
//
//  4. There is NO preset algebra. A chip is a write, not a layer. Each row
//     derives independently whether the current selection still matches one of
//     its presets (MatchingPreset) and reads as Custom when it does not.
//     Layering two families would need conflict rules for a category two presets
//     disagree about, and would make the active chip unrecoverable from the
//     selection, so the rail could no longer tell the user which preset they are
//     on.
//
// Adding a preset is one table row PER SCOPE and the mirrored row in
// frontend/state.js. No view changes. A "GDPR" preset in a regulatory family,
// for example, is exactly these two rows:
//
//	{ID: "gdpr", Family: "regulatory", Scope: ScopePatterns, Label: "GDPR",
//	 Categories: []string{...the pattern categories the regulation requires...}},
//	{ID: "gdpr", Family: "regulatory", Scope: ScopeNames, Label: "GDPR",
//	 Categories: []string{...the name categories it requires...}},
package engine

import (
	"fmt"
	"sort"
	"strings"
)

// The SCOPES: which half of the rail a preset may write. A scope is a set of
// categories, and applying a preset rewrites exactly that set.
//
// ScopePatterns is the built-in pattern categories, which render under Built-in
// patterns because that is what matches them. ScopeNames is the name categories
// a discovery method can emit, which render under Heuristic discovery because
// that is what discovers them offline.
const (
	ScopePatterns = "patterns"
	ScopeNames    = "names"
)

// AllPresetScopes lists the scopes in rail order, and is mirrored by
// frontend/state.js PRESET_SCOPES.
var AllPresetScopes = []string{ScopePatterns, ScopeNames}

// FamilyDepth is the depth family: how much ordinary text the selection risks
// catching, from Soft through Thorough. It is the only family today; a
// regulatory family beside it is a table row, not a rewrite.
const FamilyDepth = "depth"

// The depth family's preset IDs. They are IDENTIFIERS, persisted in the session
// file and the profile file, so they are never renamed to follow a label.
const (
	PresetSoft     = "soft"
	PresetStandard = "standard" // the default
	PresetThorough = "thorough"
)

// Preset is one chip: a named set of categories, inside one scope, belonging to
// one family.
type Preset struct {
	// ID identifies the preset within its family, and is SHARED by the
	// instances of one preset across scopes (rule 3 above).
	ID string `json:"id"`
	// Family is the chip row this preset belongs to.
	Family string `json:"family"`
	// Scope is the only set of categories this preset may write.
	Scope string `json:"scope"`
	// Label is the chip's visible text. A display string, never an identifier.
	Label string `json:"label"`
	// Categories are the keys this preset switches ON. Every other category in
	// its scope is switched OFF; every category outside its scope is untouched.
	Categories []string `json:"categories"`
}

// AlwaysOnCategories have no switch anywhere in the interface, so no preset may
// set or clear them: they are forced on by DefaultSelection and left alone by
// ApplyPreset. Mirrored by frontend/state.js ALWAYS_ON_CATEGORIES.
//
// custom_patterns is the only member. Its editor is the Custom patterns tab, so
// a stored false would be a pattern editor whose patterns never run with nothing
// on screen saying why.
var AlwaysOnCategories = []string{CatCustomPatterns}

// The pattern categories every depth preset switches on: hard PII, in the sense
// that the string itself identifies a person or an account whatever else the
// document says. Named once rather than repeated per row, because Soft and
// Standard are IDENTICAL in this scope and a second copy of the list is how the
// two silently stop being identical.
//
// A BIC belongs here for the same reason an IBAN does: it identifies the
// institution holding the account it travels beside.
var depthHardPatternCategories = []string{
	CatEmail, CatURL, CatIBAN, CatVAT, CatMatricule, CatPhone,
	CatCreditCard, CatNHS, CatIPAddress, CatMACAddress, CatCrypto,
	CatDatabaseURI, CatDESteuerID, CatESNIF, CatBIC,
}

// The pattern categories only Thorough adds: the two contextual ones and the two
// LOCATION shapes a pattern can anchor. A location is contextual in the same
// sense a date is, identifying in combination with the rest.
var depthThoroughPatternCategories = []string{
	CatAmount, CatDate, CatAddress, CatPostalCode,
}

// The name categories, by the depth preset that first switches each group on.
// The tiers are ordered by how much ordinary text each risks catching:
// identifier_names is code-shaped and near-PII so it sits with the first group;
// product and brand names can catch a PUBLIC name, which is a per-document
// allowlist decision rather than a mistake, so they wait for Standard; and the
// last group is the noisiest, either by definition (other_names) or because its
// members are ordinary words that happen to sit inside a legal name.
var (
	depthSoftNameCategories     = []string{CatEntityNames, CatProjectNames, CatIdentifierNames}
	depthStandardNameCategories = []string{CatPersonNames, CatProductNames, CatBrandNames}
	depthThoroughNameCategories = []string{
		CatOtherNames, CatCountryNames, CatNationalityNames, CatBusinessSectorNames,
	}
)

// AllPresets is THE preset table, in display order within each scope and family.
// Order is load-bearing: where several rows match one selection MatchingPreset
// prefers the DEFAULT depth and falls back to the FIRST matching row, so the
// answer is stable rather than flickering between Soft and Standard when the two
// are identical in a scope.
//
// Mirrored by frontend/state.js PRESETS and guarded by
// ../../preset_parity_test.go.
var AllPresets = []Preset{
	// The depth family, patterns scope. Soft and Standard are deliberately the
	// same set: the difference between those two depths is entirely in the name
	// categories, which is a fact about the depths and not a bug here.
	{
		ID: PresetSoft, Family: FamilyDepth, Scope: ScopePatterns, Label: "Soft",
		Categories: depthHardPatternCategories,
	},
	{
		ID: PresetStandard, Family: FamilyDepth, Scope: ScopePatterns, Label: "Standard",
		Categories: depthHardPatternCategories,
	},
	{
		ID: PresetThorough, Family: FamilyDepth, Scope: ScopePatterns, Label: "Thorough",
		Categories: concat(depthHardPatternCategories, depthThoroughPatternCategories),
	},

	// The depth family, names scope. Each depth adds to the one before it.
	{
		ID: PresetSoft, Family: FamilyDepth, Scope: ScopeNames, Label: "Soft",
		Categories: depthSoftNameCategories,
	},
	{
		ID: PresetStandard, Family: FamilyDepth, Scope: ScopeNames, Label: "Standard",
		Categories: concat(depthSoftNameCategories, depthStandardNameCategories),
	},
	{
		ID: PresetThorough, Family: FamilyDepth, Scope: ScopeNames, Label: "Thorough",
		Categories: concat(depthSoftNameCategories, depthStandardNameCategories,
			depthThoroughNameCategories),
	},
}

// concat joins category lists into a fresh slice. The table's rows share their
// building blocks, and appending to a shared slice would let one row's tail
// overwrite another's.
func concat(lists ...[]string) []string {
	var out []string
	for _, list := range lists {
		out = append(out, list...)
	}
	return out
}

// PresetScopeCategories is the set of categories a scope OWNS: the only keys a
// preset in that scope may name, and the only ones ApplyPreset writes.
//
// The pattern categories and the name categories are the two halves of
// CategorySelection, minus AlwaysOnCategories, which belong to no scope because
// they have no switch. An unknown scope owns nothing, so a preset filed under one
// writes nothing rather than writing everything.
func PresetScopeCategories(scope string) []string {
	switch scope {
	case ScopePatterns:
		return append([]string(nil), AllPIICategories...)
	case ScopeNames:
		var out []string
		for _, category := range AllValueCategories {
			if !contains(AlwaysOnCategories, category) {
				out = append(out, category)
			}
		}
		return out
	default:
		return nil
	}
}

// contains is the small membership check the functions below share. The lists are
// a dozen entries at most, read once per chip press, so a linear scan is the
// right shape and a package-level index would be state to keep true.
func contains(list []string, want string) bool {
	for _, got := range list {
		if got == want {
			return true
		}
	}
	return false
}

// ValidPresetScope reports whether scope is one this file knows. It is what
// settings validation refuses an invented scope with: a stored key no reader
// resolves is a control that appears to do something and does not.
func ValidPresetScope(scope string) bool {
	return contains(AllPresetScopes, scope)
}

// PresetFamilies lists the families that have at least one preset in scope, in
// table order. The rail renders one chip row per entry, so a family with nothing
// behind it in this scope gets no row rather than an empty one.
func PresetFamilies(scope string) []string {
	var out []string
	for _, preset := range AllPresets {
		if preset.Scope == scope && !contains(out, preset.Family) {
			out = append(out, preset.Family)
		}
	}
	return out
}

// PresetsFor returns the presets of one row (one scope, one family) in table
// order, which is the row's display order and MatchingPreset's tie-break.
func PresetsFor(scope, family string) []Preset {
	var out []Preset
	for _, preset := range AllPresets {
		if preset.Scope == scope && preset.Family == family {
			out = append(out, preset)
		}
	}
	return out
}

// FindPreset resolves one chip. The bool is false for a combination the table
// does not hold, so a caller refuses rather than applying an empty preset, which
// would clear the whole scope.
func FindPreset(scope, family, id string) (Preset, bool) {
	for _, preset := range AllPresets {
		if preset.Scope == scope && preset.Family == family && preset.ID == id {
			return preset, true
		}
	}
	return Preset{}, false
}

// ApplyPreset is the ONLY writer of a preset into a selection. A caller that
// filled a category map itself is how the two sides of the bridge drift.
//
// It returns a FRESH map: every category in the preset's own scope set to whether
// the preset names it, and every category outside that scope copied across
// untouched. That last half is the whole point of the scoped model, so it is the
// behaviour presets_test.go pins in both directions.
//
// AlwaysOnCategories belong to no scope, so they survive whatever the preset
// says, and DefaultSelection is what puts them on in the first place.
func ApplyPreset(sel CategorySelection, preset Preset) CategorySelection {
	out := CategorySelection{}
	for category, on := range sel {
		out[category] = on
	}
	for _, category := range PresetScopeCategories(preset.Scope) {
		out[category] = contains(preset.Categories, category)
	}
	return out
}

// MatchingPreset answers, for ONE row, which preset the current selection reads
// as, or false for Custom. It is derived rather than stored, for the reason
// SignalSourceEnabled is derived: a flag beside the set it summarises can
// disagree with it, and here that would mean the rail naming a preset the run
// does not use.
//
// Where SEVERAL rows match, the DEFAULT depth wins and table order breaks any
// remaining tie. Both halves matter: Soft and Standard are identical in the
// patterns scope, so a fresh session must read "Standard" there (the depth it
// started on) rather than "Soft", and a tie between two non-default rows must
// still resolve to one stable answer rather than flickering.
//
// country is what makes the answer honest. The national identifier categories
// follow the document country (CountryIDCategories), so a Luxembourg document at
// any depth has the German and Spanish identifiers OFF even though every depth
// preset names them. Comparing against the raw table would therefore read Custom
// for every document ever loaded.
func MatchingPreset(sel CategorySelection, scope, family, country string) (Preset, bool) {
	categories := PresetScopeCategories(scope)
	var first Preset
	found := false
	for _, preset := range PresetsFor(scope, family) {
		matched := true
		for _, category := range categories {
			want := contains(preset.Categories, category)
			// A category whose switch follows the country is expected off
			// wherever it does not apply, whatever the preset names.
			if contains(CountryIDCategories, category) {
				want = want && CategoryAppliesTo(category, country)
			}
			if sel[category] != want {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		// SEVERAL presets can match one selection, because two depths can name the
		// same categories in one scope: Soft and Standard are identical in the
		// patterns scope, differing only in the names scope. The tie is broken
		// toward the DEFAULT depth, so a fresh session's row reads as the depth the
		// session actually started on rather than as whichever row happens to come
		// first in the table. Table order decides the rest, so the answer stays
		// stable instead of flickering between two rows that both match.
		if preset.ID == PresetStandard {
			return preset, true
		}
		if !found {
			first, found = preset, true
		}
	}
	return first, found
}

// PresetKey is the storage key for one row: "<scope>.<family>". Flat rather than
// nested so a family added later needs no schema change.
func PresetKey(scope, family string) string {
	return scope + "." + family
}

// SplitPresetKey reverses PresetKey. The bool is false for anything that is not
// two known halves, which is what settings validation refuses a hand-edited file
// with.
func SplitPresetKey(key string) (scope, family string, ok bool) {
	scope, family, found := strings.Cut(key, ".")
	if !found || !ValidPresetScope(scope) || !contains(PresetFamilies(scope), family) {
		return "", "", false
	}
	return scope, family, true
}

// ValidatePresets checks a stored preset map: every key is a known row and every
// value is a preset that row actually holds. The error NAMES what is valid,
// because the caller who sees it is a user with a hand-edited session file, not
// the author of this table.
//
// An EMPTY map is valid and means Custom on every row, which is exactly what a
// selection matching no preset stores.
func ValidatePresets(presets map[string]string) error {
	for key, id := range presets {
		scope, family, ok := SplitPresetKey(key)
		if !ok {
			return fmt.Errorf(
				"unknown preset row %q, expected one of %v", key, AllPresetKeys())
		}
		if _, found := FindPreset(scope, family, id); !found {
			return fmt.Errorf(
				"unknown preset %q for the %q row, expected one of %v",
				id, key, PresetIDsFor(scope, family))
		}
	}
	return nil
}

// AllPresetKeys lists every row's storage key, sorted, for the messages above and
// for the frontend's own validation.
func AllPresetKeys() []string {
	var out []string
	for _, scope := range AllPresetScopes {
		for _, family := range PresetFamilies(scope) {
			out = append(out, PresetKey(scope, family))
		}
	}
	sort.Strings(out)
	return out
}

// PresetIDsFor lists one row's preset IDs in display order.
func PresetIDsFor(scope, family string) []string {
	var out []string
	for _, preset := range PresetsFor(scope, family) {
		out = append(out, preset.ID)
	}
	return out
}

// MatchingPresets derives the whole stored shape at once: "<scope>.<family>" to
// preset ID for every row the selection matches, and NO KEY for a row that reads
// as Custom. Absence is how Custom is representable, which is why the map is
// omitempty on the wire.
//
// This is what the run REPORT names, derived from the selection the run actually
// obeyed rather than from a preset carried alongside it, so the report cannot
// claim a preset the run did not use.
func MatchingPresets(sel CategorySelection, country string) map[string]string {
	out := map[string]string{}
	for _, scope := range AllPresetScopes {
		for _, family := range PresetFamilies(scope) {
			if preset, ok := MatchingPreset(sel, scope, family, country); ok {
				out[PresetKey(scope, family)] = preset.ID
			}
		}
	}
	return out
}

// DepthSelection is one depth preset applied across BOTH scopes, then scoped to
// a document country, plus the always-on categories: the whole
// CategorySelection a fresh session at that depth has.
//
// It is deliberately NOT what a chip does. A chip is scoped, and writing both
// scopes at once is exactly the cross-section reach the scoped model removes;
// nothing in the interface calls this. It exists because a caller sometimes needs
// a complete selection rather than an edit to one: DefaultSelection is the one in
// the application, and the engine's tests are the rest.
//
// The COUNTRY is what makes the result the selection the rail would show. Every
// depth preset names all four national identifier categories, because to the
// pattern pass they are hard PII; each exists in exactly one country, so the
// switch follows the document (CountryIDCategories) and picking a depth must not
// silently re-enable the German identifier on a French document. It changes
// nothing about what a run REPLACES, since DetectPIISelected scopes the patterns
// by country as well; it is what lets MatchingPreset recognise the selection
// afterwards. An empty country falls back to Luxembourg, the application default,
// so a caller that has not settled the country yet still gets a usable selection
// rather than one with every national identifier off.
//
// An unknown ID yields a selection with every scoped category OFF, which fails
// loudly at the first assertion rather than quietly resolving to a depth nobody
// asked for.
func DepthSelection(id, country string) CategorySelection {
	if country == "" {
		country = CountryLU
	}
	sel := CategorySelection{}
	for _, scope := range AllPresetScopes {
		preset, ok := FindPreset(scope, FamilyDepth, id)
		if !ok {
			// Still write the scope, so the result is a complete map of
			// explicit falses rather than a half-filled one.
			preset = Preset{Scope: scope}
		}
		sel = ApplyPreset(sel, preset)
	}
	for _, category := range CountryIDCategories {
		sel[category] = sel[category] && CategoryAppliesTo(category, country)
	}
	for _, category := range AlwaysOnCategories {
		sel[category] = true
	}
	return sel
}

// DefaultSelection is what a run obeys when no selection is given: the depth
// Standard presets in both scopes, scoped to the document country, plus the
// always-on categories.
//
// It is the successor to the old level fallback, and it is a FUNCTION rather than
// a package variable because a CategorySelection is a map: a shared one would let
// any caller that wrote into the default change what every later run detects.
func DefaultSelection(country string) CategorySelection {
	return DepthSelection(PresetStandard, country)
}
