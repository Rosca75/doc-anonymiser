// engine/signals.go — which built-in signals may DERIVE Suggestions, and which
// readings of each.
//
// A built-in pattern does two independent things, and the user must be able to
// control the second without losing the first:
//
//  1. it MATCHES AND REPLACES the signal itself. An email address is
//     anonymised because it is personal data in its own right. That is governed
//     by Built-in patterns and the category's own switch, and nothing here
//     touches it.
//  2. it can be used as EVIDENCE to find related text elsewhere in the batch.
//     An address like pierre.dupont@tpps.com is deterministic evidence for a
//     person and an organisation written in prose somewhere else.
//
// Only the second is a source of Suggestions, and only the second is switched
// here. Conflating them is the mistake this file exists to prevent: clearing
// "Email addresses" must never stop email addresses being anonymised.
//
// The second thing is not one question but several, because one signal supports
// several readings through several mechanisms. An address's LOCAL PART is
// evidence for a person; its DOMAIN is evidence for an organisation. A user who
// wants organisations from domains but does not want "pierre.dupont" read as a
// person is asking something the engine can answer, so each reading is switched
// on its own and the signal above them is a master over the set.
package engine

// SignalSource identifiers. Only a signal category that ACTUALLY implements
// discovery belongs here: an identifier with no discovery behind it is a control
// that appears to do something and does not.
const (
	// SignalSourceEmail derives person and organisation Suggestions from a
	// matched email address's local part and domain.
	SignalSourceEmail = "email"
	// SignalSourceWebsite derives organisation Suggestions from a matched
	// website's registrable domain label.
	//
	// It exists because a document need contain no email address at all and still
	// name its own parties: a measured framework agreement between two companies
	// carried no address anywhere, so email evidence contributed nothing, while
	// "www.nstar.lu" sat in it as deterministic evidence for the organisation
	// NStar, whose spelling no derivation rule can produce from "Northstar".
	//
	// Its VALUE is the URL pattern category, as SignalSourceEmail's is the email
	// one: a signal source identifier IS a built-in pattern category, because the
	// rail renders a signal's readings on the row of the pattern that produces
	// the evidence. An identifier with no category row would be a control with
	// nowhere to render.
	SignalSourceWebsite = CatURL
)

// SignalDerivation identifiers: WHAT a signal derives, and by which mechanism.
// One per implemented mechanism in signaldiscovery.go, for the same reason as
// above: a row with no producer behind it is a control that appears to do
// something and does not.
const (
	// DerivationEmailPerson reads the local part of a matched address as a
	// person's name ("pierre.dupont@..." is evidence for Pierre Dupont).
	DerivationEmailPerson = "email.person"
	// DerivationEmailOrganisation reads the domain as an organisation's name
	// ("...@tpps.com" is evidence for Tpps).
	DerivationEmailOrganisation = "email.organisation"
	// DerivationWebsiteOrganisation reads a website's registrable domain label as
	// an organisation's name ("www.nstar.lu" is evidence for NStar).
	//
	// A website has no person reading: a domain names an organisation, and a URL
	// path is a page rather than somebody. So this source has exactly one
	// derivation, which is what the nested selection shape is for.
	DerivationWebsiteOrganisation = "url.organisation"
)

// AllSignalSources lists the sources the user can switch, mirrored by frontend
// state.js SIGNAL_SOURCES and checked by ../../detection_parity_test.go.
var AllSignalSources = []string{SignalSourceEmail, SignalSourceWebsite}

// SignalDerivations lists, per signal source, the derivations it supports, in
// display order. It is the ONE definition of the tree the rail renders, mirrored
// by frontend/state.js SIGNAL_DERIVATIONS and guarded by
// ../../detection_parity_test.go.
var SignalDerivations = map[string][]string{
	SignalSourceEmail:   {DerivationEmailPerson, DerivationEmailOrganisation},
	SignalSourceWebsite: {DerivationWebsiteOrganisation},
}

// SignalSourceSelection is which DERIVATIONS may produce Suggestions, keyed by
// source and then by derivation.
//
// Nested rather than flat, because the two questions are nested: a source is a
// signal the pattern pass matched, a derivation is one reading of it. A flat map
// of dotted keys would let a derivation exist with no source above it, and the
// rail would have nowhere to hang it.
//
// Maps rather than structs of booleans, and deliberately DATA-DRIVEN: the control
// that renders this is built from AllSignalSources and SignalDerivations, so a new
// reading is one constant and one implementation rather than a new field, a new
// row in the rail and a new persisted flag.
//
// There is no master boolean over a source. "Every derivation of this source off"
// is already expressible, so a second way of saying it would be a second thing to
// keep in agreement; the rail DERIVES the master for display instead.
type SignalSourceSelection map[string]map[string]bool

// DefaultSignalSources are the derivations a fresh session starts with: all of
// them, because the evidence is deterministic and deriving from it is the reason
// the feature exists.
func DefaultSignalSources() SignalSourceSelection {
	out := SignalSourceSelection{}
	for _, source := range AllSignalSources {
		out[source] = map[string]bool{}
		for _, derivation := range SignalDerivations[source] {
			out[source][derivation] = true
		}
	}
	return out
}

// SignalSourceEnabled reports whether a source may derive anything at all, which
// is true when ANY of its derivations is on.
//
// This is the DERIVED master the rail shows on the signal's own row. It is
// computed, never stored: a persisted fourth boolean can disagree with the set it
// summarises, and a row reading "on" while every reading under it is off lies
// about what a run does.
//
// A NIL selection means "nothing was supplied", which reads as the defaults
// rather than as "everything off": a caller that has not reached the settings yet
// must get the shipped behaviour, not silence.
//
// @param sel the selection, possibly nil
// @param source one of AllSignalSources
// @return whether any reading of this signal may produce Suggestions
func SignalSourceEnabled(sel SignalSourceSelection, source string) bool {
	for _, derivation := range SignalDerivations[source] {
		if SignalDerivationEnabled(sel, source, derivation) {
			return true
		}
	}
	return false
}

// SignalDerivationEnabled reports whether ONE reading of a signal may produce
// Suggestions. This is the question the discovery pass asks, once per seed
// producer.
//
// Absent reads as the DEFAULT, never as "off", at both levels: a session file, a
// profile and a live settings push can each be missing a key for a different
// reason, and a missing key must not silently disable a feature the user never
// switched off. An explicitly present false is obeyed.
//
// @param sel the selection, possibly nil or partial
// @param source one of AllSignalSources
// @param derivation one of SignalDerivations[source]
// @return whether that reading may produce Suggestions
func SignalDerivationEnabled(sel SignalSourceSelection, source, derivation string) bool {
	if !ValidSignalDerivation(source, derivation) {
		return false
	}
	defaults := DefaultSignalSources()
	if sel == nil {
		return defaults[source][derivation]
	}
	derivations, present := sel[source]
	if !present || derivations == nil {
		return defaults[source][derivation]
	}
	if on, present := derivations[derivation]; present {
		return on
	}
	return defaults[source][derivation]
}

// ValidSignalSource reports whether an identifier is one this build implements.
// Used to refuse an unknown key rather than store it and have it silently do
// nothing for the rest of the session.
func ValidSignalSource(source string) bool {
	for _, s := range AllSignalSources {
		if s == source {
			return true
		}
	}
	return false
}

// ValidSignalDerivation reports whether a (source, derivation) pair is one this
// build implements. A derivation identifier is only meaningful UNDER its source,
// so both halves are checked together: accepting a pair whose source is wrong
// would store a reading nothing ever reads.
func ValidSignalDerivation(source, derivation string) bool {
	if !ValidSignalSource(source) {
		return false
	}
	for _, d := range SignalDerivations[source] {
		if d == derivation {
			return true
		}
	}
	return false
}

// NormaliseSignalSources returns a selection containing exactly the known
// sources and, under each, exactly the known derivations, filling anything the
// caller omitted from the defaults and dropping anything this build does not
// implement.
//
// This is what keeps a session file, a profile and a live settings push agreeing
// on the same set: each of the three can be missing a key for a different
// reason, and all three must end up meaning the same thing.
func NormaliseSignalSources(sel SignalSourceSelection) SignalSourceSelection {
	out := SignalSourceSelection{}
	for _, source := range AllSignalSources {
		out[source] = map[string]bool{}
		for _, derivation := range SignalDerivations[source] {
			out[source][derivation] = SignalDerivationEnabled(sel, source, derivation)
		}
	}
	return out
}
