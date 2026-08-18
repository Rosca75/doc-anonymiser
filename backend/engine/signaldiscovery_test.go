// engine/signaldiscovery_test.go — signal-based discovery.
//
// The rules being pinned here are the ones that decide whether the feature is
// useful or noise. Each is a table case, and each names the failure it prevents.
package engine

import (
	"strings"
	"testing"
)

// batch builds a document batch from name/markdown pairs.
func batch(pairs ...string) []Document {
	var docs []Document
	for i := 0; i+1 < len(pairs); i += 2 {
		docs = append(docs, Document{Name: pairs[i], Format: FormatTXT, Markdown: pairs[i+1]})
	}
	return docs
}

// discover runs signal-based discovery with email on and an empty allowlist.
func discover(t *testing.T, docs []Document) []Suggestion {
	t.Helper()
	return DiscoverFromSignals(SignalDiscoveryInput{
		Documents: docs,
		Sources:   SignalSourceSelection{SignalSourceEmail: true},
		Allow:     NewEmptyAllowlist(),
	})
}

// find returns the suggestion whose main text matches, or nil.
func find(suggestions []Suggestion, text string) *Suggestion {
	for i := range suggestions {
		if strings.EqualFold(suggestions[i].MainText, text) {
			return &suggestions[i]
		}
	}
	return nil
}

// --- The two seeds -------------------------------------------------------

// TestEmailLocalPartFindsThePerson: the address names the person, so the person
// written in prose somewhere else is a Suggestion with the address as evidence.
func TestEmailLocalPartFindsThePerson(t *testing.T) {
	docs := batch(
		"mail.md", "Write to pierre.dupont@tpps.com about the fee.\n",
		"engagement.md", "Contact Pierre Dupont for approval.\n")

	got := find(discover(t, docs), "Pierre Dupont")
	if got == nil {
		t.Fatalf("the local part must find the person written elsewhere, got %+v", discover(t, docs))
	}
	if got.Category != CatPersonNames {
		t.Errorf("a local-part seed files under person_names, got %q", got.Category)
	}
	if len(got.DiscoveryMethods) != 1 || got.DiscoveryMethods[0] != MethodSignal {
		t.Errorf("the method must be signal, got %v", got.DiscoveryMethods)
	}
	if len(got.Evidence) != 1 || got.Evidence[0].Kind != EvidenceEmailLocalPart {
		t.Fatalf("the evidence must be the local part, got %+v", got.Evidence)
	}
	if got.Evidence[0].SignalText != "pierre.dupont@tpps.com" {
		t.Errorf("the evidence must name the address it came from, got %q", got.Evidence[0].SignalText)
	}
	if got.Evidence[0].SignalCategory != CatEmail {
		t.Errorf("the evidence must name the signal category, got %q", got.Evidence[0].SignalCategory)
	}
}

// TestEmailDomainFindsTheOrganisation: the domain's registrable label is the
// START of the organisation's name, so the Suggestion is the whole capitalised
// name as the document writes it, not the bare label. Suggesting "Tpps" alone
// would offer to replace a stem and leave the rest of the name in clear text.
func TestEmailDomainFindsTheOrganisation(t *testing.T) {
	docs := batch(
		"mail.md", "From pierre.dupont@tpps.com.\n",
		"engagement.md", "The counterparty is Tpps France, a subsidiary.\n")

	got := find(discover(t, docs), "Tpps France")
	if got == nil {
		t.Fatalf("the domain must find the organisation name, got %+v", discover(t, docs))
	}
	if got.Category != CatEntityNames {
		t.Errorf("a domain seed files under entity_names, got %q", got.Category)
	}
	if len(got.Evidence) != 1 || got.Evidence[0].Kind != EvidenceEmailDomain {
		t.Errorf("the evidence must be the domain, got %+v", got.Evidence)
	}
}

// TestDiscoveryReadsTheWholeBatch: the address is in one file and the name is in
// another, which is the normal case and the reason this method is batch-level.
// Per document it would find nothing at all here.
func TestDiscoveryReadsTheWholeBatch(t *testing.T) {
	docs := batch(
		"a.md", "pierre.dupont@tpps.com\n",
		"b.md", "no names here\n",
		"c.md", "Pierre Dupont signed.\n")

	got := find(discover(t, docs), "Pierre Dupont")
	if got == nil {
		t.Fatal("a name in a different file from the address must still be found")
	}
	if len(got.Evidence) == 0 || len(got.Evidence[0].Documents) == 0 {
		t.Errorf("the evidence must name the document the signal was in, got %+v", got.Evidence)
	}
	if got.Evidence[0].Documents[0] != "a.md" {
		t.Errorf("the evidence document is the one holding the SIGNAL, got %v",
			got.Evidence[0].Documents)
	}
}

// TestNothingIsSuggestedFromInsideTheSignal is the rule that stops the feature
// suggesting its own input. "pierre.dupont" occurs inside the address, and the
// address is already being anonymised by built-in pattern matching, so a
// Suggestion here would be the seed reading itself back.
func TestNothingIsSuggestedFromInsideTheSignal(t *testing.T) {
	docs := batch("only.md", "Reach pierre.dupont@tpps.com for the fee note.\n")

	got := discover(t, docs)
	if len(got) != 0 {
		t.Errorf("text occurring ONLY inside the address must produce nothing, got %+v", got)
	}
}

// TestTheDocumentsSpellingWins: the seed is folded and lower-cased, so replacing
// what the seed says would leave the real text untouched. The Suggestion has to
// carry the casing and accents the document uses.
func TestTheDocumentsSpellingWins(t *testing.T) {
	docs := batch(
		"mail.md", "jose.mendonca@tpps.com wrote in.\n",
		"body.md", "José Mendonça confirmed the scope.\n")

	got := find(discover(t, docs), "José Mendonça")
	if got == nil {
		t.Fatalf("the accented spelling in the document is what gets replaced, got %+v",
			discover(t, docs))
	}
	if got.MainText != "José Mendonça" {
		t.Errorf("main text must be the document's spelling, got %q", got.MainText)
	}
}

// --- The suppression rules ----------------------------------------------

// TestRoleMailboxesSuggestNoPerson: "info@acme.com" must not invent a person
// called Info, and neither must any other functional mailbox.
func TestRoleMailboxesSuggestNoPerson(t *testing.T) {
	for _, local := range []string{"info", "support", "billing", "noreply", "no-reply"} {
		docs := batch(
			"mail.md", local+"@tpps.com\n",
			"body.md", "Please contact "+strings.ToUpper(local[:1])+local[1:]+" about it.\n")
		for _, s := range discover(t, docs) {
			if s.Category == CatPersonNames {
				t.Errorf("%s@ must suggest no person, got %+v", local, s)
			}
		}
	}
}

// TestSingleTokenLocalPartSuggestsNoPerson: "oscarl@acme.com" is a handle.
// Splitting a handle into a name is guesswork, not evidence.
func TestSingleTokenLocalPartSuggestsNoPerson(t *testing.T) {
	docs := batch(
		"mail.md", "oscarl@tpps.com\n",
		"body.md", "Oscarl was in the meeting.\n")
	for _, s := range discover(t, docs) {
		if s.Category == CatPersonNames {
			t.Errorf("a single-token local part must suggest no person, got %+v", s)
		}
	}
}

// TestPublicProvidersSuggestNoOrganisation: a consumer mail host is the user's
// provider, never a party to the engagement, and suggesting "Gmail" would offer
// to anonymise the word in a sentence about email providers.
func TestPublicProvidersSuggestNoOrganisation(t *testing.T) {
	for _, domain := range []string{"gmail.com", "outlook.com", "yahoo.fr", "hotmail.com", "proton.me"} {
		label := strings.SplitN(domain, ".", 2)[0]
		docs := batch(
			"mail.md", "marie.duval@"+domain+"\n",
			"body.md", "We migrated away from "+strings.ToUpper(label[:1])+label[1:]+" last year.\n")
		for _, s := range discover(t, docs) {
			if s.Category == CatEntityNames {
				t.Errorf("%s must suggest no organisation, got %+v", domain, s)
			}
		}
	}
}

// TestPublicSuffixAndInfrastructureLabelsAreNotSeeds: only the registrable label
// names anything. Seeding "co", "uk" or "mail" would search every document for
// ordinary words.
func TestPublicSuffixAndInfrastructureLabelsAreNotSeeds(t *testing.T) {
	docs := batch(
		"mail.md", "pierre.dupont@mail.tpps.co.uk\n",
		"body.md", "Tpps handles the UK mail and the Co accounts.\n")

	got := discover(t, docs)
	// The label seeds a match; the Suggestion is the capitalised name built on it.
	if find(got, "Tpps") == nil {
		t.Errorf("the registrable label must still be a seed, got %+v", got)
	}
	for _, unwanted := range []string{"UK", "mail", "Co"} {
		if s := find(got, unwanted); s != nil && s.Category == CatEntityNames {
			t.Errorf("%q is not an organisation name, got %+v", unwanted, s)
		}
	}
}

// --- The vetoes ---------------------------------------------------------

// TestAllowlistVetoesASignalSuggestion: the never-anonymise list wins over every
// producer, and offering a Suggestion that would then never be replaced is a
// review decision with no effect.
func TestAllowlistVetoesASignalSuggestion(t *testing.T) {
	docs := batch(
		"mail.md", "pierre.dupont@tpps.com\n",
		"body.md", "Pierre Dupont signed.\n")

	allow := NewEmptyAllowlist()
	allow.Add("Pierre Dupont")
	got := DiscoverFromSignals(SignalDiscoveryInput{
		Documents: docs,
		Sources:   SignalSourceSelection{SignalSourceEmail: true},
		Allow:     allow,
	})
	if s := find(got, "Pierre Dupont"); s != nil {
		t.Errorf("an allowlisted term must not be suggested, got %+v", s)
	}
}

// TestARemovedValueIsNotSuggestedAgain: removals are enforced THROUGH the
// allowlist, so the same veto covers them. Without this a removal reads as undone
// the moment detection runs again.
func TestARemovedValueIsNotSuggestedAgain(t *testing.T) {
	docs := batch(
		"mail.md", "pierre.dupont@tpps.com\n",
		"body.md", "Pierre Dupont signed.\n")

	allow := NewEmptyAllowlist()
	ApplyRemovals(allow, []RemovedValue{{
		Category: CatPersonNames, MainText: "pierre dupont", Placeholder: "[PERSON_1]",
	}})
	got := DiscoverFromSignals(SignalDiscoveryInput{
		Documents: docs,
		Sources:   SignalSourceSelection{SignalSourceEmail: true},
		Allow:     allow,
	})
	if s := find(got, "Pierre Dupont"); s != nil {
		t.Errorf("a removed Value must not come back as a Suggestion, got %+v", s)
	}
}

// --- The source switch --------------------------------------------------

// TestDisablingTheEmailSourceStopsOnlyDiscovery is acceptance criterion 4, at the
// engine level: clearing the source stops email-DERIVED Suggestions and leaves
// email MATCHING alone, because they are two different mechanisms.
func TestDisablingTheEmailSourceStopsOnlyDiscovery(t *testing.T) {
	const text = "Write to pierre.dupont@tpps.com. Pierre Dupont signed.\n"
	docs := batch("only.md", text)

	off := DiscoverFromSignals(SignalDiscoveryInput{
		Documents: docs,
		Sources:   SignalSourceSelection{SignalSourceEmail: false},
		Allow:     NewEmptyAllowlist(),
	})
	if len(off) != 0 {
		t.Errorf("with the source off there must be no Suggestions, got %+v", off)
	}

	// The address itself is still matched and replaced, by the pass that has
	// nothing to do with this setting.
	spans := DetectPIISelected(text, PresetSelection(LevelMedium), CountryLU)
	found := false
	for _, s := range spans {
		if s.Category == CatEmail && s.Original == "pierre.dupont@tpps.com" {
			found = true
		}
	}
	if !found {
		t.Error("switching off email-derived Suggestions must not stop email anonymisation")
	}
}

// TestNilSourcesMeansTheDefaults: a caller that has not reached the settings yet
// must get the shipped behaviour, not silence.
func TestNilSourcesMeansTheDefaults(t *testing.T) {
	docs := batch(
		"mail.md", "pierre.dupont@tpps.com\n",
		"body.md", "Pierre Dupont signed.\n")

	got := DiscoverFromSignals(SignalDiscoveryInput{
		Documents: docs, Sources: nil, Allow: NewEmptyAllowlist(),
	})
	if find(got, "Pierre Dupont") == nil {
		t.Errorf("nil sources must read as the defaults, got %+v", got)
	}
}

// --- Relatedness, not grouping ------------------------------------------

// TestSharedDomainEvidenceDoesNotGroup: "Tpps France" and "Tpps S.A." share
// domain evidence, and that makes them RELATED, not one Value. A country branch
// and a legal entity may genuinely differ, so the user confirms grouping.
func TestSharedDomainEvidenceDoesNotGroup(t *testing.T) {
	docs := batch(
		"mail.md", "pierre.dupont@tpps.com\n",
		"body.md", "Tpps France invoices; Tpps Holdings signs.\n")

	got := discover(t, docs)
	france, holdings := find(got, "Tpps France"), find(got, "Tpps Holdings")
	if france == nil || holdings == nil {
		t.Fatalf("both organisation spellings must be suggested separately, got %+v", got)
	}
	for _, s := range []*Suggestion{france, holdings} {
		for _, spelling := range s.Spellings {
			if strings.EqualFold(spelling, "Tpps France") || strings.EqualFold(spelling, "Tpps Holdings") {
				t.Errorf("shared evidence must not group two legal entities, %q carries %q",
					s.MainText, spelling)
			}
		}
	}
}

// --- Merging with the other methods -------------------------------------

// TestSignalAndHeuristicFindingsMergeIntoOneRow: two methods finding the same
// text is ONE decision for the user, carrying both methods, not two rows they
// have to notice are the same.
func TestSignalAndHeuristicFindingsMergeIntoOneRow(t *testing.T) {
	signal := Suggestion{
		MainText: "Pierre Dupont", Category: CatPersonNames, Count: 1,
		Evidence: []Evidence{{Kind: EvidenceEmailLocalPart, SignalCategory: CatEmail,
			SignalText: "pierre.dupont@tpps.com", Documents: []string{"mail.md"}}},
	}.WithMethod(MethodSignal)
	heuristic := Suggestion{
		MainText: "pierre dupont", Category: CatPersonNames, Count: 2,
		Spellings: []string{"Dupont"},
	}.WithMethod(MethodHeuristic)

	got := MergeSuggestions([]Suggestion{signal}, []Suggestion{heuristic})
	if len(got) != 1 {
		t.Fatalf("one string in one category is one row, got %+v", got)
	}
	if got[0].MainText != "Pierre Dupont" {
		t.Errorf("the first-seen spelling wins, got %q", got[0].MainText)
	}
	if got[0].Count != 3 {
		t.Errorf("counts must add up, got %d", got[0].Count)
	}
	if len(got[0].DiscoveryMethods) != 2 {
		t.Errorf("both methods must survive the merge, got %v", got[0].DiscoveryMethods)
	}
	if len(got[0].Spellings) != 1 || got[0].Spellings[0] != "Dupont" {
		t.Errorf("spellings from every contributing method must survive, got %v", got[0].Spellings)
	}
	if len(got[0].Evidence) != 1 {
		t.Errorf("the evidence must survive the merge, got %+v", got[0].Evidence)
	}
}

// TestSeveralMethodsReduceToTheStrongestClass: provenance keeps every method,
// precedence needs one answer, and corroboration by a weaker method is not doubt.
func TestSeveralMethodsReduceToTheStrongestClass(t *testing.T) {
	cases := []struct {
		name    string
		methods []string
		want    string
	}{
		{"nothing stated is trusted, not demoted", nil, MatchClassUserDefined},
		{"an unknown method is trusted, not demoted", []string{"telepathy"}, MatchClassUserDefined},
		{"manual", []string{MethodManual}, MatchClassUserDefined},
		{"signal", []string{MethodSignal}, MatchClassSmartDiscovered},
		{"heuristic", []string{MethodHeuristic}, MatchClassSmartDiscovered},
		{"local AI", []string{MethodLocalAI}, MatchClassLocalAIDiscovered},
		{"signal and AI reduce to signal", []string{MethodLocalAI, MethodSignal}, MatchClassSmartDiscovered},
		{"manual beats every discovery", []string{MethodLocalAI, MethodHeuristic, MethodManual}, MatchClassUserDefined},
	}
	for _, tc := range cases {
		if got := MatchClassForMethods(tc.methods); got != tc.want {
			t.Errorf("%s: MatchClassForMethods(%v) = %q, want %q", tc.name, tc.methods, got, tc.want)
		}
	}
}
