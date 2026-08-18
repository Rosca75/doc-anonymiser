// engine/signals_test.go — the signal-source selection's reading rules.
//
// Unit tier: pure map logic, no I/O (docs/TESTING.md). Every case here is about
// how SILENCE is read, because that is where this file can do damage: a missing
// key read as "off" silently disables a feature the user never switched off, and
// the user finds out from a run that suggests nothing.
//
// The discovery behaviour these switches gate lives in signaldiscovery_test.go;
// this file is only about the selection itself.
package engine

import "testing"

func TestSignalDerivationEnabledDefaultsOn(t *testing.T) {
	t.Run("config/absent_reads_as_the_default", func(t *testing.T) {
		cases := []struct {
			name string
			sel  SignalSourceSelection
		}{
			{"nil selection", nil},
			{"empty selection", SignalSourceSelection{}},
			{"source present with no readings named", SignalSourceSelection{SignalSourceEmail: {}}},
			{"source present but nil", SignalSourceSelection{SignalSourceEmail: nil}},
			{
				"one reading named, the other silent",
				SignalSourceSelection{SignalSourceEmail: {DerivationEmailPerson: true}},
			},
		}
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				if !SignalDerivationEnabled(tt.sel, SignalSourceEmail, DerivationEmailOrganisation) {
					t.Errorf("with %s, the organisation reading must read as its default (on), got off "+
						"(selection: %+v)", tt.name, tt.sel)
				}
			})
		}
	})
}

func TestSignalDerivationEnabledObeysAnExplicitFalse(t *testing.T) {
	t.Run("config/explicit_false_is_obeyed", func(t *testing.T) {
		sel := SignalSourceSelection{SignalSourceEmail: {
			DerivationEmailPerson:       false,
			DerivationEmailOrganisation: true,
		}}
		if SignalDerivationEnabled(sel, SignalSourceEmail, DerivationEmailPerson) {
			t.Error("an explicitly cleared reading must read as off, not as its default")
		}
		if !SignalDerivationEnabled(sel, SignalSourceEmail, DerivationEmailOrganisation) {
			t.Error("the reading left on must stay on: the two are independent")
		}
	})
}

func TestSignalDerivationEnabledRefusesAnUnknownPair(t *testing.T) {
	t.Run("config/unknown_pair_is_never_enabled", func(t *testing.T) {
		// A derivation identifier is only meaningful UNDER its source, so a valid
		// reading filed under the wrong source is as dead as an invented one. Both
		// must read as off whatever a stored map says, or a typo becomes a
		// producer nothing gates.
		cases := []struct{ source, derivation string }{
			{SignalSourceEmail, "email.telepathy"},
			{"telepathy", DerivationEmailPerson},
			{"", ""},
		}
		for _, tt := range cases {
			sel := SignalSourceSelection{tt.source: {tt.derivation: true}}
			if SignalDerivationEnabled(sel, tt.source, tt.derivation) {
				t.Errorf("(%q, %q) is not implemented and must never read as enabled, "+
					"even stored as true", tt.source, tt.derivation)
			}
		}
	})
}

func TestSignalSourceEnabledIsTheDerivedMaster(t *testing.T) {
	t.Run("config/source_master_is_derived", func(t *testing.T) {
		// On when ANY reading is on, off only when all of them are. Derived rather
		// than stored, because a flag beside the set it summarises can disagree
		// with it, and a signal reading "on" over readings that are all off lies
		// about what a run does.
		cases := []struct {
			name string
			sel  SignalSourceSelection
			want bool
		}{
			{"nil is the defaults, which are all on", nil, true},
			{
				"one reading on",
				SignalSourceSelection{SignalSourceEmail: {
					DerivationEmailPerson:       true,
					DerivationEmailOrganisation: false,
				}},
				true,
			},
			{
				"the other reading on",
				SignalSourceSelection{SignalSourceEmail: {
					DerivationEmailPerson:       false,
					DerivationEmailOrganisation: true,
				}},
				true,
			},
			{
				"every reading off",
				SignalSourceSelection{SignalSourceEmail: {
					DerivationEmailPerson:       false,
					DerivationEmailOrganisation: false,
				}},
				false,
			},
		}
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				if got := SignalSourceEnabled(tt.sel, SignalSourceEmail); got != tt.want {
					t.Errorf("SignalSourceEnabled with %s = %v, want %v (selection: %+v)",
						tt.name, got, tt.want, tt.sel)
				}
			})
		}
	})
}

func TestDefaultSignalSourcesTurnsEveryReadingOn(t *testing.T) {
	t.Run("config/defaults_are_every_reading", func(t *testing.T) {
		sel := DefaultSignalSources()
		for _, source := range AllSignalSources {
			for _, derivation := range SignalDerivations[source] {
				if !sel[source][derivation] {
					t.Errorf("%s/%s is off by default; the evidence is deterministic and "+
						"deriving from it is why the feature exists", source, derivation)
				}
			}
		}
	})
}

func TestEverySourceHasAtLeastOneDerivation(t *testing.T) {
	t.Run("config/no_source_without_a_producer", func(t *testing.T) {
		// A source with no readings would render as a master over nothing, and a
		// reading with no source above it would have nowhere to hang in the rail.
		for _, source := range AllSignalSources {
			if len(SignalDerivations[source]) == 0 {
				t.Errorf("signal source %q lists no derivations, so its control is a switch "+
					"over nothing", source)
			}
		}
		for source := range SignalDerivations {
			if !ValidSignalSource(source) {
				t.Errorf("SignalDerivations names source %q, which AllSignalSources does not; "+
					"the rail builds its groups from AllSignalSources, so those readings are "+
					"unreachable", source)
			}
		}
	})
}

func TestNormaliseSignalSourcesFillsAndDropsKeys(t *testing.T) {
	t.Run("config/normalise_is_the_complete_set", func(t *testing.T) {
		// This is what keeps a session file, a profile and a live settings push
		// agreeing: each can be missing a key for a different reason and all three
		// must end up meaning the same thing.
		got := NormaliseSignalSources(SignalSourceSelection{
			SignalSourceEmail: {DerivationEmailPerson: false, "email.telepathy": true},
			"telepathy":       {DerivationEmailPerson: true},
		})

		if len(got) != len(AllSignalSources) {
			t.Errorf("normalised selection has %d sources, want %d: %+v",
				len(got), len(AllSignalSources), got)
		}
		if _, present := got["telepathy"]; present {
			t.Error("an unimplemented source must be dropped, not stored as a key nothing reads")
		}
		email := got[SignalSourceEmail]
		if _, present := email["email.telepathy"]; present {
			t.Error("an unimplemented reading must be dropped for the same reason")
		}
		if len(email) != len(SignalDerivations[SignalSourceEmail]) {
			t.Errorf("email holds %d readings, want %d: %+v",
				len(email), len(SignalDerivations[SignalSourceEmail]), email)
		}
		if email[DerivationEmailPerson] {
			t.Error("an explicitly cleared reading must survive normalisation cleared")
		}
		if !email[DerivationEmailOrganisation] {
			t.Error("an omitted reading must be filled from the DEFAULTS, not from Go's zero value")
		}
	})
}

func TestValidSignalDerivationChecksBothHalves(t *testing.T) {
	t.Run("config/valid_pair", func(t *testing.T) {
		if !ValidSignalDerivation(SignalSourceEmail, DerivationEmailPerson) {
			t.Error("an implemented pair must be accepted")
		}
		for _, tt := range []struct{ source, derivation string }{
			{SignalSourceEmail, "email.telepathy"},
			{"telepathy", DerivationEmailPerson},
			{"", DerivationEmailPerson},
			{SignalSourceEmail, ""},
		} {
			if ValidSignalDerivation(tt.source, tt.derivation) {
				t.Errorf("(%q, %q) must be refused: storing it would be a control that "+
					"appears to do something and does not", tt.source, tt.derivation)
			}
		}
	})
}
