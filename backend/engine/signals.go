// engine/signals.go — which built-in signals may DERIVE Suggestions.
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
package engine

// SignalSource identifiers. Only a signal category that ACTUALLY implements
// discovery belongs here: an identifier with no discovery behind it is a control
// that appears to do something and does not.
const (
	// SignalSourceEmail derives person and organisation Suggestions from a
	// matched email address's local part and domain.
	SignalSourceEmail = "email"
)

// AllSignalSources lists the sources the user can switch, mirrored by frontend
// state.js SIGNAL_SOURCES and checked by ../../signal_parity_test.go.
var AllSignalSources = []string{SignalSourceEmail}

// SignalSourceSelection is which sources may derive Suggestions, keyed by the
// identifiers above.
//
// A map rather than a struct of booleans, and deliberately DATA-DRIVEN: the
// control that renders it is a checklist built from AllSignalSources, so a new
// source is one constant and one implementation rather than a new field, a new
// row in the rail and a new persisted flag.
//
// There is no master boolean over it. "Every source off" is already expressible,
// so a second way of saying it would be a second thing to keep in agreement.
type SignalSourceSelection map[string]bool

// DefaultSignalSources are the sources a fresh session starts with: email on,
// because the evidence is deterministic and it is the reason the feature exists.
func DefaultSignalSources() SignalSourceSelection {
	return SignalSourceSelection{SignalSourceEmail: true}
}

// SignalSourceEnabled reports whether a source may derive Suggestions.
//
// A NIL selection means "nothing was supplied", which reads as the defaults
// rather than as "everything off": a caller that has not reached the settings
// yet must get the shipped behaviour, not silence. An explicitly present false
// is obeyed.
//
// @param sel the selection, possibly nil
// @param source one of AllSignalSources
// @return whether signal-based discovery may use this source
func SignalSourceEnabled(sel SignalSourceSelection, source string) bool {
	if sel == nil {
		return DefaultSignalSources()[source]
	}
	return sel[source]
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

// NormaliseSignalSources returns a selection containing exactly the known
// sources, filling any the caller omitted from the defaults and dropping any it
// does not implement.
//
// This is what keeps a session file, a profile and a live settings push agreeing
// on the same set: each of the three can be missing a key for a different
// reason, and all three must end up meaning the same thing.
func NormaliseSignalSources(sel SignalSourceSelection) SignalSourceSelection {
	out := SignalSourceSelection{}
	defaults := DefaultSignalSources()
	for _, source := range AllSignalSources {
		if sel == nil {
			out[source] = defaults[source]
			continue
		}
		if on, present := sel[source]; present {
			out[source] = on
		} else {
			out[source] = defaults[source]
		}
	}
	return out
}
