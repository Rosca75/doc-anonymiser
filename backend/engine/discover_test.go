// engine/discover_test.go —  tests for the Smart
// detection tier, English AND French fixtures (CLAUDE.md §6).
package engine

import (
	"context"
	"strings"
	"testing"
	"time"
)

// findSuggestion returns the suggestion with the given text, or nil.
func findSuggestion(suggestions []Suggestion, text string) *Suggestion {
	for i := range suggestions {
		if suggestions[i].MainText == text {
			return &suggestions[i]
		}
	}
	return nil
}

// smartDetectCountry is the test-side country-aware wrapper: it runs the
// offline pass with a document country so the country-scoped org-keyword signal
// applies. It lives here (not in discover.go) because the only non-test caller
// that needs a country, the App layer, calls HeuristicDiscoverContext directly; a
// production wrapper would be unreachable from any main package (deadcode).
func smartDetectCountry(text string, allow *Allowlist, opts HeuristicDiscoveryOptions, country string) []Suggestion {
	suggestions, _ := HeuristicDiscoverContext(context.Background(), text, allow, opts, country)
	return suggestions
}

func TestSmartDetectSuffixGazetteer(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string // expected client suggestion text
	}{
		{"sarl long form", "We audited Acme Solutions S.à r.l. in March.", "Acme Solutions S.à r.l."},
		{"sarl compact", "Contract with Bidco Sàrl was signed.", "Bidco Sàrl"},
		{"sa dotted", "The fund Alpine Trust S.A. reported growth.", "Alpine Trust S.A."},
		{"gmbh", "Payment came from Wagner GmbH yesterday.", "Wagner GmbH"},
		{"scsp", "The vehicle Borealis SCSp holds assets.", "Borealis SCSp"},
		{"nv", "Partnered with Oranje N.V. on this.", "Oranje N.V."},
		{"ltd", "Working with Brightside Ltd on delivery.", "Brightside Ltd"},
		{"plc", "Shares of Union Metals plc rose.", "Union Metals plc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HeuristicDiscoverWithOptions(tc.text, NewEmptyAllowlist(), HeuristicDiscoveryOptions{})
			c := findSuggestion(got, tc.want)
			if c == nil {
				t.Fatalf("suggestion %q missing, got %+v", tc.want, got)
			}
			if c.Category != "entity_names" {
				t.Errorf("suffix suggestion category = %s, want entity_names", c.Category)
			}
		})
	}
}

func TestHeuristicSuffixAloneIsNotASuggestion(t *testing.T) {
	// A legal form with no preceding name must not be proposed.
	got := HeuristicDiscoverWithOptions("The GmbH structure is common. The GmbH form works.", NewEmptyAllowlist(), HeuristicDiscoveryOptions{})
	if c := findSuggestion(got, "GmbH"); c != nil {
		t.Errorf("bare suffix must not be a suggestion: %+v", c)
	}
}

func TestSmartDetectCapitalisedRuns(t *testing.T) {
	text := "Yesterday we met Jean-Pierre Muller at the office. " +
		"Later, Anouk van den Berg joined the call with everyone."
	got := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), HeuristicDiscoveryOptions{})

	jp := findSuggestion(got, "Jean-Pierre Muller")
	if jp == nil {
		t.Fatalf("hyphenated multi-word name missing: %+v", got)
	}
	if jp.Category != "person_names" {
		t.Errorf("multi-word run default category = %s, want person_names", jp.Category)
	}
	if findSuggestion(got, "Anouk van den Berg") == nil {
		t.Errorf("particle name missing: %+v", got)
	}
}

func TestSmartDetectSentenceStartRule(t *testing.T) {
	// "Ensuite" opens a sentence once: sentence-case noise, dropped.
	once := HeuristicDiscoverWithOptions("Nous avons signé. Ensuite tout le monde est parti.", NewEmptyAllowlist(), HeuristicDiscoveryOptions{})
	if c := findSuggestion(once, "Ensuite"); c != nil {
		t.Errorf("single sentence-start run must be dropped: %+v", c)
	}

	// "Borealis" opens two sentences: repeated sentence-start is kept.
	twice := HeuristicDiscoverWithOptions("Borealis grew fast. Borealis hired again.", NewEmptyAllowlist(), HeuristicDiscoveryOptions{})
	if findSuggestion(twice, "Borealis") == nil {
		t.Errorf("repeated sentence-start run must be kept: %+v", twice)
	}
}

func TestSmartDetectSingleWordFrequency(t *testing.T) {
	// A single-word run appearing once mid-sentence, no suffix, no title:
	// dropped as noise.
	got := HeuristicDiscoverWithOptions("The meeting covered Zephyr briefly today.", NewEmptyAllowlist(), HeuristicDiscoveryOptions{})
	if c := findSuggestion(got, "Zephyr"); c != nil {
		t.Errorf("single-occurrence single-word run must be dropped: %+v", c)
	}
	// The same word twice qualifies.
	got = HeuristicDiscoverWithOptions("We value Zephyr highly. Everyone likes working with Zephyr daily.", NewEmptyAllowlist(), HeuristicDiscoveryOptions{})
	if findSuggestion(got, "Zephyr") == nil {
		t.Errorf("repeated run must qualify: %+v", got)
	}
}

func TestSmartDetectTitleCues(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"Le dossier de Mme Weber est prêt.", "Weber"},
		{"Please ask Dr Keller about it.", "Keller"},
		{"Selon M. Dupont, tout va bien.", "Dupont"},
	}
	for _, tc := range cases {
		got := HeuristicDiscoverWithOptions(tc.text, NewEmptyAllowlist(), HeuristicDiscoveryOptions{})
		c := findSuggestion(got, tc.want)
		if c == nil {
			t.Errorf("title-cued name %q missing in %q: %+v", tc.want, tc.text, got)
			continue
		}
		if c.Category != "person_names" {
			t.Errorf("title-cued suggestion %q category = %s, want person_names", tc.want, c.Category)
		}
	}
}

func TestSmartDetectFrequencyAndContexts(t *testing.T) {
	text := "Alpine Trust leads. We audit Alpine Trust yearly. " +
		"The Alpine Trust burden grows. Alpine Trust again. Alpine Trust once more."
	got := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), HeuristicDiscoveryOptions{})
	c := findSuggestion(got, "Alpine Trust")
	if c == nil {
		t.Fatalf("frequent suggestion missing: %+v", got)
	}
	if c.Count < 3 {
		t.Errorf("count = %d, want at least 3", c.Count)
	}
	if len(c.Contexts) == 0 || len(c.Contexts) > 3 {
		t.Errorf("contexts must be 1..3 snippets, got %d", len(c.Contexts))
	}
	for _, ctx := range c.Contexts {
		if !strings.Contains(ctx, "Alpine Trust") {
			t.Errorf("context snippet must contain the suggestion: %q", ctx)
		}
	}
	// Ranking: the most frequent suggestion comes first.
	if len(got) > 1 && got[0].Count < got[1].Count {
		t.Errorf("suggestions not ranked by count: %+v", got)
	}
}

func TestSmartDetectAllowlistWins(t *testing.T) {
	allow := NewEmptyAllowlist()
	allow.Add("CSSF")
	allow.Add("Luxembourg")
	text := "The CSSF reviewed our Luxembourg filing. The CSSF asked again. " +
		"Luxembourg rules apply. Alpine Trust S.A. responded."
	got := HeuristicDiscoverWithOptions(text, allow, HeuristicDiscoveryOptions{})
	if findSuggestion(got, "CSSF") != nil || findSuggestion(got, "Luxembourg") != nil {
		t.Errorf("allowlisted terms must never be emitted: %+v", got)
	}
	if findSuggestion(got, "Alpine Trust S.A.") == nil {
		t.Errorf("non-allowlisted suggestion must survive: %+v", got)
	}
}

func TestSmartDetectFrenchFixture(t *testing.T) {
	text := "Réunion avec Mme Amélie Lefèvre et la société Lumière Conseil Sàrl. " +
		"Le projet Lumière Conseil Sàrl continue à Esch-sur-Alzette avec Amélie Lefèvre."
	got := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), HeuristicDiscoveryOptions{})
	if c := findSuggestion(got, "Amélie Lefèvre"); c == nil || c.Category != "person_names" {
		t.Errorf("accented person name missing or misrouted: %+v", got)
	}
	if c := findSuggestion(got, "Lumière Conseil Sàrl"); c == nil || c.Category != "entity_names" {
		t.Errorf("French company with Sàrl suffix missing or misrouted: %+v", got)
	}
}

// --- HeuristicDiscoveryOptions --------------------------------------------------

// TestHeuristicSuggestionsCarryAScore: every suggestion must carry the
// heuristic score, whether or not filtering is on, because the review UI
// sorts and filters on it without re-running detection.
func TestHeuristicSuggestionsCarryAScore(t *testing.T) {
	got := HeuristicDiscoverWithOptions("Alpine Trust S.A. signed. Marie Duval signed too.\n", NewEmptyAllowlist(), HeuristicDiscoveryOptions{})
	if len(got) == 0 {
		t.Fatal("expected suggestions")
	}
	for _, c := range got {
		if c.Confidence <= 0 || c.Confidence > 1 {
			t.Errorf("suggestion %q scored %v, want a score in (0, 1]", c.MainText, c.Confidence)
		}
	}
}

// TestSuggestionScoreLadder pins the score each detector signal earns, in
// English and French. The ladder is the thing a future tuning change has
// to argue with, so it is asserted directly.
func TestSuggestionScoreLadder(t *testing.T) {
	cases := []struct {
		name string
		text string
		want map[string]float32
	}{
		{
			name: "legal form outranks everything (English)",
			text: "Alpine Trust S.A. signed the mandate today.\n",
			want: map[string]float32{"Alpine Trust S.A.": 0.95},
		},
		{
			name: "a title routes and scores a person (French)",
			text: "Le rapport a ete valide par Mme Weber hier soir.\n",
			want: map[string]float32{"Weber": 0.90},
		},
		{
			name: "a repeated full name beats a single sighting",
			text: "Marie Duval called. Later Marie Duval wrote to Anouk Berger.\n",
			want: map[string]float32{"Marie Duval": 0.80, "Anouk Berger": 0.65},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HeuristicDiscoverWithOptions(tc.text, NewEmptyAllowlist(), HeuristicDiscoveryOptions{})
			scores := map[string]float32{}
			for _, c := range got {
				scores[c.MainText] = c.Confidence
			}
			for text, want := range tc.want {
				if scores[text] != want {
					t.Errorf("%q scored %v, want %v (all: %+v)", text, scores[text], want, scores)
				}
			}
		})
	}
}

// TestHeuristicDiscoveryOptionsFilters walks each option independently, so a
// failure names the knob that broke rather than "fewer suggestions".
func TestHeuristicDiscoveryOptionsFilters(t *testing.T) {
	// Anouk Berger sits MID-sentence on purpose: a name whose only
	// occurrence opens a sentence is dropped by the sentence-start rule,
	// which predates these options and is not what is under test here.
	const text = "Marie Duval called. Later Marie Duval wrote. March was busy. March was long.\n" +
		"Later that week Anouk Berger replied once.\n"

	has := func(suggestions []Suggestion, want string) bool {
		for _, c := range suggestions {
			if c.MainText == want {
				return true
			}
		}
		return false
	}

	cases := []struct {
		name     string
		opts     HeuristicDiscoveryOptions
		mustKeep []string
		mustDrop []string
	}{
		{
			name:     "no options keeps the noise",
			opts:     HeuristicDiscoveryOptions{},
			mustKeep: []string{"Marie Duval", "March", "Anouk Berger"},
		},
		{
			name:     "MinLength drops short suggestions",
			opts:     HeuristicDiscoveryOptions{MinLength: 6},
			mustKeep: []string{"Marie Duval", "Anouk Berger"},
			mustDrop: []string{"March"},
		},
		{
			name:     "MinOccurrences drops the single sighting",
			opts:     HeuristicDiscoveryOptions{MinOccurrences: 2},
			mustKeep: []string{"Marie Duval", "March"},
			mustDrop: []string{"Anouk Berger"},
		},
		{
			name:     "ExcludeCommonWords drops the month, keeps the names",
			opts:     HeuristicDiscoveryOptions{ExcludeCommonWords: true},
			mustKeep: []string{"Marie Duval", "Anouk Berger"},
			mustDrop: []string{"March"},
		},
		{
			name:     "MinConfidence drops the single-word repeat",
			opts:     HeuristicDiscoveryOptions{MinConfidence: 0.5},
			mustKeep: []string{"Marie Duval", "Anouk Berger"},
			mustDrop: []string{"March"},
		},
		{
			name:     "the shipped defaults keep the names and drop the noise",
			opts:     DefaultHeuristicDiscoveryOptions(),
			mustKeep: []string{"Marie Duval", "Anouk Berger"},
			mustDrop: []string{"March"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), tc.opts)
			for _, want := range tc.mustKeep {
				if !has(got, want) {
					t.Errorf("%q must survive, got %+v", want, got)
				}
			}
			for _, unwanted := range tc.mustDrop {
				if has(got, unwanted) {
					t.Errorf("%q must be dropped, got %+v", unwanted, got)
				}
			}
		})
	}
}

// TestExcludeCommonWordsKeepsNamesContainingOne: "March Consulting" is a
// perfectly good company name and must not be dropped just because one of
// its words is a month.
func TestExcludeCommonWordsKeepsNamesContainingOne(t *testing.T) {
	got := HeuristicDiscoverWithOptions(
		"March Consulting signed. March Consulting invoiced.\n",
		NewEmptyAllowlist(),
		HeuristicDiscoveryOptions{ExcludeCommonWords: true})
	found := false
	for _, c := range got {
		if c.MainText == "March Consulting" {
			found = true
		}
	}
	if !found {
		t.Errorf("a multi-word name containing a common word must survive, got %+v", got)
	}
}

// TestExcludeCommonWordsFrench: the word list covers French too, since
// testdata carries French fixtures (CLAUDE.md section 6).
func TestExcludeCommonWordsFrench(t *testing.T) {
	got := HeuristicDiscoverWithOptions(
		"Cependant le dossier avance. Cependant rien n'est signe.\n",
		NewEmptyAllowlist(),
		HeuristicDiscoveryOptions{ExcludeCommonWords: true})
	for _, c := range got {
		if c.MainText == "Cependant" {
			t.Errorf("a French sentence opener must be dropped, got %+v", got)
		}
	}
}

// TestAllowlistStillWinsOverTuning: an allowlisted term is dropped no
// matter how strongly the heuristics vouch for it (CLAUDE.md section 5).
func TestAllowlistStillWinsOverTuning(t *testing.T) {
	allow := NewEmptyAllowlist()
	allow.Add("Alpine Trust S.A.")
	got := HeuristicDiscoverWithOptions(
		"Alpine Trust S.A. signed the mandate.\n", allow, HeuristicDiscoveryOptions{})
	for _, c := range got {
		if c.MainText == "Alpine Trust S.A." {
			t.Errorf("an allowlisted term must never be proposed, got %+v", got)
		}
	}
}

// --- the offline pass must scale, and must be interruptible --------------

// TestSmartDetectContextIsInterruptible: before the offline pass took
// no context, so Cancel could only land BETWEEN documents and one large file
// ran to completion whatever the user pressed.
func TestSmartDetectContextIsInterruptible(t *testing.T) {
	text := strings.Repeat("Alpine Trust S.A. met Marie Duval in Luxembourg. ", 50000)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	got, err := HeuristicDiscoverContext(ctx, text, NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions(), "")
	if err == nil {
		t.Fatal("a cancelled context must be reported, not ignored")
	}
	if got != nil {
		t.Errorf("a cancelled scan returns no suggestions, got %d", len(got))
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("cancellation took %v, which is not a cancellation", elapsed)
	}
}

// TestSmartDetectContextMatchesTheLegacyCall: the ctx-aware entry point must
// find exactly what the old one did when nothing cancels it.
func TestSmartDetectContextMatchesTheLegacyCall(t *testing.T) {
	text := "Alpine Trust S.A. met Marie Duval. Alpine Trust signed with Borealis Fund GmbH."
	opts := DefaultHeuristicDiscoveryOptions()
	want := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), opts)
	got, err := HeuristicDiscoverContext(context.Background(), text, NewEmptyAllowlist(), opts, "")
	if err != nil {
		t.Fatalf("an uncancelled scan must not fail: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d suggestions, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i].MainText != want[i].MainText || got[i].Category != want[i].Category {
			t.Errorf("suggestion %d differs: %+v vs %+v", i, got[i], want[i])
		}
	}
}

// --- Product signals -----------------------------------------------------

func TestProductDetection(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string // the suggestion text expected under product_names
	}{
		{
			name: "a trademark mark is the high-precision signal",
			text: "We deployed Meridian Suite™ across the group. Meridian Suite™ is live.\n",
			want: "Meridian Suite",
		},
		{
			name: "a registered mark counts too",
			text: "Helios Core® shipped in March. Helios Core® shipped again.\n",
			want: "Helios Core",
		},
		{
			name: "a head noun inside the run is the weaker signal",
			text: "The Borealis Platform went live. The Borealis Platform scaled well.\n",
			want: "Borealis Platform",
		},
		{
			name: "a head noun beside the run counts as well",
			text: "The Borealis platform went live. The Borealis platform scaled well.\n",
			want: "Borealis",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HeuristicDiscoverWithOptions(tc.text, NewEmptyAllowlist(), HeuristicDiscoveryOptions{})
			for _, c := range got {
				if c.MainText == tc.want {
					if c.Category != CatProductNames {
						t.Fatalf("%q was filed as %s, want %s", c.MainText, c.Category, CatProductNames)
					}
					return
				}
			}
			t.Fatalf("no suggestion %q in %+v", tc.want, got)
		})
	}
}

func TestALegalFormBeatsAProductNoun(t *testing.T) {
	// A company that happens to sell a platform is still a company. The cue
	// ladder is ordered on purpose and this is its one ambiguous rung.
	got := HeuristicDiscoverWithOptions("Alpine Trust S.A. platform is live.\n",
		NewEmptyAllowlist(), HeuristicDiscoveryOptions{})
	if len(got) == 0 || got[0].MainText != "Alpine Trust S.A." {
		t.Fatalf("want the suffixed name first, got %+v", got)
	}
	if got[0].Category != CatEntityNames {
		t.Errorf("category = %s, want %s", got[0].Category, CatEntityNames)
	}
}

func TestCodesReachTheOfflineRoute(t *testing.T) {
	// The code detector is a second scanner, folded into the offline route so
	// every caller gets it. A detector nothing calls is a detector that does not
	// exist, which is what the retired organisation_names category was.
	got := HeuristicDiscoverWithOptions("Ref. INV-88213 covers the projet ATLAS-2024.\n",
		NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions())

	byText := map[string]Suggestion{}
	for _, c := range got {
		byText[c.MainText] = c
	}
	if c, ok := byText["INV-88213"]; !ok || c.Category != CatIdentifierNames {
		t.Errorf("want INV-88213 as an identifier, got %+v", got)
	}
	if c, ok := byText["ATLAS-2024"]; !ok || c.Category != CatProjectNames {
		t.Errorf("want ATLAS-2024 as a project, got %+v", got)
	}
}

// --- email-derived person names (BUILD benchmark, 2026-08) -----------------

// TestSmartDetectEmailDerivedNameCategorisesAndBoosts: a name spelt out in an
// address in the same document is a person with high confidence, even where
// the frequency heuristics alone would have hesitated. This is the single
// highest-value offline signal on real correspondence.
func TestSmartDetectEmailDerivedNameCategorisesAndBoosts(t *testing.T) {
	text := "Please contact Johannes Borch <johannes.borch@pwc.lu> about the file.\n"
	got := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions())
	c := findSuggestion(got, "Johannes Borch")
	if c == nil {
		t.Fatalf("email-named person missing: %+v", got)
	}
	if c.Category != CatPersonNames {
		t.Errorf("category = %s, want %s", c.Category, CatPersonNames)
	}
	if c.Confidence < emailPersonScore {
		t.Errorf("confidence = %.2f, want at least %.2f", c.Confidence, emailPersonScore)
	}
}

// TestSmartDetectEmailDerivedSurnameAlone: the bare surname mentioned later in
// the body is recognised as the same person, because the local-part indexes
// the individual tokens too.
func TestSmartDetectEmailDerivedSurnameAlone(t *testing.T) {
	text := "From: Thierry Kremser <thierry.kremser@pwc.lu>\n" +
		"Kremser approved the request yesterday.\n"
	got := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions())
	if c := findSuggestion(got, "Kremser"); c == nil || c.Category != CatPersonNames {
		t.Errorf("bare surname not recognised as its email-named person: %+v", got)
	}
}

// TestSmartDetectEmailAccentFold: the body spells the name with accents
// ("José") while the address is ASCII ("jose"); the fold must bridge them.
func TestSmartDetectEmailAccentFold(t *testing.T) {
	text := "Cc: José Teixeira <jose.teixeira@pwc.lu> was copied. " +
		"José Teixeira replied.\n"
	got := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions())
	if c := findSuggestion(got, "José Teixeira"); c == nil || c.Category != CatPersonNames {
		t.Errorf("accented name not matched to its ASCII address: %+v", got)
	}
}

// TestSmartDetectEmailIgnoresRoleMailbox: a functional mailbox is not a person.
// "info@acme.com" must not invent someone called "Info", and a single-token
// handle is not a forename+surname.
func TestSmartDetectEmailIgnoresRoleMailbox(t *testing.T) {
	names := deriveEmailNames("Write to info@acme.com or noreply@acme.com or bob@acme.com.\n")
	if len(names) != 0 {
		t.Errorf("role/handle mailboxes must inject no names, got %v", names)
	}
}

// TestSmartDetectEmailNameDoesNotOverrideLegalSuffix: a run carrying a legal
// suffix stays an organisation even if its words also appear in an address.
func TestSmartDetectEmailNameDoesNotOverrideLegalSuffix(t *testing.T) {
	text := "Alpine Trust S.A. <alpine.trust@example.com> signed the deed.\n"
	got := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions())
	if c := findSuggestion(got, "Alpine Trust S.A."); c == nil || c.Category != CatEntityNames {
		t.Errorf("a legal-suffix run must stay an organisation: %+v", got)
	}
}

// --- negative gazetteer (business/role/document nouns) ----------------------

// TestSmartDetectNegativeGazetteerDropsBusinessPhrases: the dominant offline
// noise class is Title-Case business vocabulary. Under the shipped defaults it
// is dropped, while a real multi-word name in the same text survives.
func TestSmartDetectNegativeGazetteerDropsBusinessPhrases(t *testing.T) {
	text := "Revenue Management improved. Revenue Management improved again. " +
		"Extra Holiday Buying was approved. General Terms of Sale apply. " +
		"The auditor Marie Duval signed the note.\n"
	got := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions())

	for _, noise := range []string{"Revenue Management", "Extra Holiday Buying", "General Terms of Sale"} {
		if c := findSuggestion(got, noise); c != nil {
			t.Errorf("business-noun phrase %q must be dropped, got it as %+v", noise, c)
		}
	}
	if findSuggestion(got, "Marie Duval") == nil {
		t.Errorf("a real name in the same text must survive: %+v", got)
	}
}

// --- organisation keyword signal (country-scoped) ---------------------------

// TestSmartDetectOrgKeywordCommon: an English organisation keyword vouches a
// run as a company in ANY country ("Delta Group"), and it beats the default
// person guess a bare multi-word run would get.
func TestSmartDetectOrgKeywordCommon(t *testing.T) {
	text := "We met Delta Group about the deal. Delta Group agreed.\n"
	got := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions())
	c := findSuggestion(got, "Delta Group")
	if c == nil {
		t.Fatalf("an org-keyword run must be proposed: %+v", got)
	}
	if c.Category != CatEntityNames {
		t.Errorf("category = %s, want %s", c.Category, CatEntityNames)
	}
	if c.Confidence < 0.85 {
		t.Errorf("confidence = %.2f, want at least 0.85", c.Confidence)
	}
}

// TestSmartDetectOrgKeywordCountryScoped: a French legal-ish keyword only
// vouches when the document country uses French. "PwC Société" is a company in
// Luxembourg (which covers French and German) but not in the UK.
func TestSmartDetectOrgKeywordCountryScoped(t *testing.T) {
	text := "Signed by PwC Société on Monday. PwC Société confirmed.\n"

	lu := smartDetectCountry(text, NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions(), CountryLU)
	if c := findSuggestion(lu, "PwC Société"); c == nil || c.Category != CatEntityNames {
		t.Errorf("Luxembourg must read \"Société\" as a company: %+v", lu)
	}

	uk := smartDetectCountry(text, NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions(), CountryUK)
	if c := findSuggestion(uk, "PwC Société"); c != nil && c.Category == CatEntityNames {
		t.Errorf("the UK has no French keyword, so \"Société\" must not vouch a company: %+v", c)
	}
}

// TestSmartDetectOrgKeywordAbsorbsLowercaseTail: an organisation name whose
// legal-ish tail is not capitalised is completed, so the VALUE is the whole
// name ("PwC Société coopérative"), not the capitalised prefix alone.
func TestSmartDetectOrgKeywordAbsorbsLowercaseTail(t *testing.T) {
	text := "Issued by PwC Société coopérative today. PwC Société coopérative billed.\n"
	got := smartDetectCountry(text, NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions(), CountryLU)
	if c := findSuggestion(got, "PwC Société coopérative"); c == nil || c.Category != CatEntityNames {
		t.Errorf("the lowercase tail must be absorbed into the company name: %+v", got)
	}
}

// TestSmartDetectOrgKeywordGermanForLuxembourg: Luxembourg covers German too,
// so a German organisation word vouches a company there.
func TestSmartDetectOrgKeywordGermanForLuxembourg(t *testing.T) {
	text := "Vertrag mit Alpen Gesellschaft unterschrieben. Alpen Gesellschaft zahlte.\n"
	got := smartDetectCountry(text, NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions(), CountryLU)
	if c := findSuggestion(got, "Alpen Gesellschaft"); c == nil || c.Category != CatEntityNames {
		t.Errorf("a German org word must vouch a company in Luxembourg: %+v", got)
	}
}

// TestSmartDetectOrgKeywordSurvivesStrict: an org-keyword run is structurally
// vouched, so strict strictness keeps it (unlike a bare capitalised run).
func TestSmartDetectOrgKeywordSurvivesStrict(t *testing.T) {
	text := "We met Delta Group once.\n"
	strict := DefaultHeuristicDiscoveryOptions()
	strict.Strictness = StrictnessStrict
	got := smartDetectCountry(text, NewEmptyAllowlist(), strict, CountryUK)
	if findSuggestion(got, "Delta Group") == nil {
		t.Errorf("strict must keep an org-keyword company: %+v", got)
	}
}

// TestSmartDetectOrgKeywordNeedsMoreThanTheKeyword: a bare keyword on its own
// is not a company ("Group" discussed as a concept), only a keyword next to
// another word is.
func TestSmartDetectOrgKeywordNeedsMoreThanTheKeyword(t *testing.T) {
	if runHasOrgKeyword("Group", "") {
		t.Errorf("a bare keyword must not read as an organisation")
	}
	if !runHasOrgKeyword("Delta Group", "") {
		t.Errorf("a keyword beside another word must read as an organisation")
	}
}

// --- strictness lever -------------------------------------------------------

// TestSmartDetectStrictRequiresAnAnchor: strict strictness emits only
// structurally-vouched suggestions. A bare multi-word run (no suffix, title or
// email name) is dropped however it scored; a legal-suffix company survives.
func TestSmartDetectStrictRequiresAnAnchor(t *testing.T) {
	text := "The auditor Marie Duval reviewed it. Alpine Trust S.A. billed us.\n"

	strict := DefaultHeuristicDiscoveryOptions()
	strict.Strictness = StrictnessStrict
	got := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), strict)
	if findSuggestion(got, "Marie Duval") != nil {
		t.Errorf("strict must drop a bare multi-word run: %+v", got)
	}
	if findSuggestion(got, "Alpine Trust S.A.") == nil {
		t.Errorf("strict must keep a legal-suffix company: %+v", got)
	}

	// Balanced (the default) keeps the same bare run: strictness is the lever
	// that changed the outcome, nothing else.
	bal := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions())
	if findSuggestion(bal, "Marie Duval") == nil {
		t.Errorf("balanced must keep the bare multi-word run: %+v", bal)
	}
}

// TestSmartDetectStrictKeepsEmailNamedPerson: an email-named person is
// structurally vouched, so strict keeps it even though it has no legal suffix.
func TestSmartDetectStrictKeepsEmailNamedPerson(t *testing.T) {
	text := "Contact Johannes Borch <johannes.borch@pwc.lu> today.\n"
	strict := DefaultHeuristicDiscoveryOptions()
	strict.Strictness = StrictnessStrict
	got := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), strict)
	if findSuggestion(got, "Johannes Borch") == nil {
		t.Errorf("strict must keep an email-named person: %+v", got)
	}
}

// TestSmartDetectLenientKeepsRareSingleWord: lenient strictness keeps the
// single-word single-occurrence runs the frequency rule drops. They carry a
// low score, so this only surfaces them once the confidence floor is lowered.
func TestSmartDetectLenientKeepsRareSingleWord(t *testing.T) {
	text := "We discussed Zephyr briefly.\n"

	lenient := DefaultHeuristicDiscoveryOptions()
	lenient.Strictness = StrictnessLenient
	lenient.MinConfidence = 0
	if findSuggestion(HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), lenient), "Zephyr") == nil {
		t.Errorf("lenient with no floor must keep a rare single-word run")
	}

	bal := DefaultHeuristicDiscoveryOptions()
	bal.MinConfidence = 0
	if findSuggestion(HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), bal), "Zephyr") != nil {
		t.Errorf("balanced must drop a single-word single-occurrence run even with no floor")
	}
}

// TestSmartDetectSuppressesStreetAddress: a name that only ever appears in a
// postal-address context is a street, not a person. "rue Gerhard Mercator"
// must not propose "Gerhard Mercator" as someone to anonymise.
func TestSmartDetectSuppressesStreetAddress(t *testing.T) {
	text := "Our office is at 2, rue Gerhard Mercator in the capital. " +
		"Post to 2, rue Gerhard Mercator as well.\n"
	got := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions())
	if c := findSuggestion(got, "Gerhard Mercator"); c != nil {
		t.Errorf("a street name must not be proposed, got %+v", c)
	}
}

// TestSmartDetectAddressCueInsideRun: the cue can be a token INSIDE the run
// ("Place de la Gare"), not only the word before it.
func TestSmartDetectAddressCueInsideRun(t *testing.T) {
	text := "The venue is Place de la Gare downtown. Place de la Gare hosts it.\n"
	got := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions())
	if c := findSuggestion(got, "Place de la Gare"); c != nil {
		t.Errorf("an address phrase must be suppressed, got %+v", c)
	}
}

// TestSmartDetectPersonAboveOwnAddressSurvives: a real person can sign just
// above their address. The email name overrides the address suppression for
// the person, while the street on the next line is still dropped.
func TestSmartDetectPersonAboveOwnAddressSurvives(t *testing.T) {
	text := "Best regards, Oscar Liber <oscar.liber@pwc.lu>\n" +
		"2, rue Gerhard Mercator, Luxembourg.\n" +
		"Oscar Liber signed.\n"
	got := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions())
	if findSuggestion(got, "Oscar Liber") == nil {
		t.Errorf("the signer named by an address must survive: %+v", got)
	}
	if c := findSuggestion(got, "Gerhard Mercator"); c != nil {
		t.Errorf("the street on the address line must be dropped, got %+v", c)
	}
}

// TestSmartDetectStreetCueDoesNotSuppressSuffixedCompany: a legal suffix
// overrides address suppression, so a company at its own address survives.
func TestSmartDetectStreetCueDoesNotSuppressSuffixedCompany(t *testing.T) {
	text := "rue Alpine Trust S.A. is a coincidence. Alpine Trust S.A. billed us.\n"
	got := HeuristicDiscoverWithOptions(text, NewEmptyAllowlist(), DefaultHeuristicDiscoveryOptions())
	if findSuggestion(got, "Alpine Trust S.A.") == nil {
		t.Errorf("a legal-suffix company must survive an address cue: %+v", got)
	}
}

// TestSmartDetectConnectorsInsideCommonPhrase: "Terms of Sale" is all-common
// only if the connector "of" is skipped like a particle; without that skip the
// phrase would leak through as a suggestion.
func TestSmartDetectConnectorsInsideCommonPhrase(t *testing.T) {
	if !isCommonWordRun("General Terms of Sale") {
		t.Errorf("a phrase of common nouns joined by a connector must read as all-common")
	}
	// A real name joined by a connector is NOT all-common: one token is a name.
	if isCommonWordRun("Bank of Marie") {
		t.Errorf("a run containing a genuine name token must not read as all-common")
	}
}

// --- The structural rules the fixture pair exposed ---------------------------

// discoverOne is the offline route over one short string, at the shipped tuning.
func discoverOne(t *testing.T, text string) []Suggestion {
	t.Helper()
	allow := NewEmptyAllowlist()
	got, err := HeuristicDiscoverContext(context.Background(), text, allow,
		DefaultHeuristicDiscoveryOptions(), CountryLU)
	if err != nil {
		t.Fatalf("HeuristicDiscoverContext: %v", err)
	}
	return FoldValueFamilies(got, allow)
}

// hasSuggestion reports whether the list carries a row with this main text.
func hasSuggestion(list []Suggestion, mainText string) bool {
	for _, s := range list {
		if strings.EqualFold(s.MainText, mainText) {
			return true
		}
	}
	return false
}

// TestLegalFormCommaIsCrossed: "Name, Societe anonyme" is the standard
// continental legal-name form and the dominant one in French and Luxembourg
// drafting.
//
// A comma always terminates a run, so before this rule what survived was either
// the legal form with no name in front of it (worthless: the name is the only
// part worth replacing) or nothing at all. Both parties of the measured contract
// were invisible offline for exactly this reason.
func TestLegalFormCommaIsCrossed(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"suffix, no comma", "Acme S.A. signed the deal in March.", "Acme S.A."},
		{"suffix, one comma", "Acme, S.A. signed the deal in March.", "Acme"},
		{"one-word legal form", "Northstar, Societe cooperative, a consulting group.", "Northstar"},
		{"multi-word name", "Contoso Holdings, Societe anonyme, was incorporated here.", "Contoso Holdings"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := discoverOne(t, tc.text)
			if !hasSuggestion(got, tc.want) {
				t.Errorf("no suggestion %q; got %v", tc.want, suggestionTexts(got))
			}
		})
	}
}

// TestLegalFormCommaRuleStaysBounded: one comma, no newline, and the tail must
// be a legal-form phrase. Without those bounds the rule walks an enumeration of
// ordinary nouns and turns a list into a company name.
func TestLegalFormCommaRuleStaysBounded(t *testing.T) {
	cases := []struct {
		name string
		text string
		not  string
	}{
		{
			name: "the tail must be a legal form",
			text: "Acme, Wednesday Morning arrived at last.",
			not:  "Acme",
		},
		{
			name: "a second comma stops it",
			text: "Beta, Acme, Societe anonyme, was incorporated here.",
			not:  "Beta Acme",
		},
		{
			name: "an article never becomes the name",
			text: "The, Societe cooperative, filed its accounts.",
			not:  "The",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, s := range discoverOne(t, tc.text) {
				if strings.EqualFold(s.MainText, tc.not) {
					t.Errorf("the comma rule reached back too far and produced %q", s.MainText)
				}
			}
		})
	}
}

// TestJobTitleTerminatesAPersonRun: a signature block reads "PIERRE LAVENTURE
// Partner". The person is the name and the title is what they do, so absorbing
// the title produces a Value that matches the document in one place and the
// person nowhere else.
//
// A title OPENING a run means there is no name at that position at all, which is
// why "Chief Information Officer" yields nobody.
func TestJobTitleTerminatesAPersonRun(t *testing.T) {
	const text = "_________________________ PIERRE LAVENTURE Partner | " +
		"_________________________ THOMAS LAVANDOU Partner"
	got := discoverOne(t, text)

	for _, want := range []string{"PIERRE LAVENTURE", "THOMAS LAVANDOU"} {
		if !hasSuggestion(got, want) {
			t.Errorf("no suggestion %q; got %v", want, suggestionTexts(got))
		}
	}
	for _, s := range got {
		if strings.Contains(strings.ToLower(s.MainText), "partner") {
			t.Errorf("a job title joined the name beside it: %q", s.MainText)
		}
	}

	t.Run("a title opening a run is nobody", func(t *testing.T) {
		for _, s := range discoverOne(t, "Chief Information Officer, and the rest of the team.") {
			if strings.Contains(strings.ToLower(s.MainText), "chief") {
				t.Errorf("a job title was proposed as a Value: %q", s.MainText)
			}
		}
	})
}

// TestASignatureRuleIsNotASentenceStart: a run of underscores is a ruled line,
// which is what a signature block puts above a printed name.
//
// Read as a sentence boundary it makes the first word of every signature look
// grammar-capitalised, and the sub-run rule then strips it: "PIERRE LAVENTURE"
// became "LAVENTURE", which is also what stopped it folding into the
// "Pierre Laventure" written elsewhere.
func TestASignatureRuleIsNotASentenceStart(t *testing.T) {
	got := discoverOne(t, "____________________ MARTIN DESCHAMPS")
	if !hasSuggestion(got, "MARTIN DESCHAMPS") {
		t.Errorf("the forename was stripped after the signature rule; got %v", suggestionTexts(got))
	}
}

// TestANameRunNeverBeginsWithAConjunction: an ALL-CAPS heading is one adjacent
// stretch of capitalised words to the tokenizer, so without this rule a run
// starts at the "AND" it crosses and harvests the fragment behind it.
func TestANameRunNeverBeginsWithAConjunction(t *testing.T) {
	for _, heading := range []string{
		"# COSTS AND EXPENSES",
		"# TERM AND TERMINATION",
		"# LIABILITY AND INDEMNITY",
		"**BY AND BETWEEN**",
	} {
		for _, s := range discoverOne(t, heading+"\n\nSome ordinary sentence follows here.") {
			if strings.HasPrefix(strings.ToUpper(s.MainText), "AND ") {
				t.Errorf("a run began with a conjunction: %q (from %q)", s.MainText, heading)
			}
		}
	}
}

// TestAllCapsHeadingTextIsNotAName: heading capitals and legal formulae are
// document furniture.
//
// ALL CAPS alone cannot be the rule: a signature block writes real people in
// capitals, and those are exactly the values the review list exists to surface.
// What separates furniture from a name is the FUNCTION WORD inside it.
func TestAllCapsHeadingTextIsNotAName(t *testing.T) {
	furniture := []string{
		"ROLES AND COMMITMENTS", "PARTIES ENTER INTO THIS AGREEMENT",
		"IN WITNESS WHEREOF", "GOVERNING LAW AND COMPETENT JURISDICTION",
	}
	for _, phrase := range furniture {
		if !isAllCapsHeadingText(phrase) {
			t.Errorf("%q is heading furniture and was not recognised as such", phrase)
		}
	}
	people := []string{"PIERRE DUPONT", "MARTIN DESCHAMPS", "THOMAS LAVANDOU"}
	for _, name := range people {
		if isAllCapsHeadingText(name) {
			t.Errorf("%q is a real person written in capitals and must survive", name)
		}
	}
	// A particle is not a function word: a real organisation carries one.
	if isAllCapsHeadingText("BANQUE DE LA PLACE") {
		t.Error("a name particle was read as a heading function word")
	}
}

// suggestionTexts is the main texts of a list, for a readable failure.
func suggestionTexts(list []Suggestion) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		out = append(out, s.MainText)
	}
	return out
}
