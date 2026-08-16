// engine/discover_test.go —  tests for the Smart
// detection tier, English AND French fixtures (CLAUDE.md §6).
package engine

import (
	"context"
	"strings"
	"testing"
	"time"
)

// findCandidate returns the candidate with the given text, or nil.
func findCandidate(cands []Candidate, text string) *Candidate {
	for i := range cands {
		if cands[i].Text == text {
			return &cands[i]
		}
	}
	return nil
}

func TestSmartDetectSuffixGazetteer(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string // expected client candidate text
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
			got := SmartDetectWithOptions(tc.text, NewEmptyAllowlist(), SmartDetectOptions{})
			c := findCandidate(got, tc.want)
			if c == nil {
				t.Fatalf("candidate %q missing, got %+v", tc.want, got)
			}
			if c.Category != "entity_names" {
				t.Errorf("suffix candidate category = %s, want entity_names", c.Category)
			}
		})
	}
}

func TestSmartDetectSuffixAloneIsNotACandidate(t *testing.T) {
	// A legal form with no preceding name must not be proposed.
	got := SmartDetectWithOptions("The GmbH structure is common. The GmbH form works.", NewEmptyAllowlist(), SmartDetectOptions{})
	if c := findCandidate(got, "GmbH"); c != nil {
		t.Errorf("bare suffix must not be a candidate: %+v", c)
	}
}

func TestSmartDetectCapitalisedRuns(t *testing.T) {
	text := "Yesterday we met Jean-Pierre Muller at the office. " +
		"Later, Anouk van den Berg joined the call with everyone."
	got := SmartDetectWithOptions(text, NewEmptyAllowlist(), SmartDetectOptions{})

	jp := findCandidate(got, "Jean-Pierre Muller")
	if jp == nil {
		t.Fatalf("hyphenated multi-word name missing: %+v", got)
	}
	if jp.Category != "person_names" {
		t.Errorf("multi-word run default category = %s, want person_names", jp.Category)
	}
	if findCandidate(got, "Anouk van den Berg") == nil {
		t.Errorf("particle name missing: %+v", got)
	}
}

func TestSmartDetectSentenceStartRule(t *testing.T) {
	// "Ensuite" opens a sentence once: sentence-case noise, dropped.
	once := SmartDetectWithOptions("Nous avons signé. Ensuite tout le monde est parti.", NewEmptyAllowlist(), SmartDetectOptions{})
	if c := findCandidate(once, "Ensuite"); c != nil {
		t.Errorf("single sentence-start run must be dropped: %+v", c)
	}

	// "Borealis" opens two sentences: repeated sentence-start is kept.
	twice := SmartDetectWithOptions("Borealis grew fast. Borealis hired again.", NewEmptyAllowlist(), SmartDetectOptions{})
	if findCandidate(twice, "Borealis") == nil {
		t.Errorf("repeated sentence-start run must be kept: %+v", twice)
	}
}

func TestSmartDetectSingleWordFrequency(t *testing.T) {
	// A single-word run appearing once mid-sentence, no suffix, no title:
	// dropped as noise.
	got := SmartDetectWithOptions("The meeting covered Zephyr briefly today.", NewEmptyAllowlist(), SmartDetectOptions{})
	if c := findCandidate(got, "Zephyr"); c != nil {
		t.Errorf("single-occurrence single-word run must be dropped: %+v", c)
	}
	// The same word twice qualifies.
	got = SmartDetectWithOptions("We value Zephyr highly. Everyone likes working with Zephyr daily.", NewEmptyAllowlist(), SmartDetectOptions{})
	if findCandidate(got, "Zephyr") == nil {
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
		got := SmartDetectWithOptions(tc.text, NewEmptyAllowlist(), SmartDetectOptions{})
		c := findCandidate(got, tc.want)
		if c == nil {
			t.Errorf("title-cued name %q missing in %q: %+v", tc.want, tc.text, got)
			continue
		}
		if c.Category != "person_names" {
			t.Errorf("title-cued candidate %q category = %s, want person_names", tc.want, c.Category)
		}
	}
}

func TestSmartDetectFrequencyAndContexts(t *testing.T) {
	text := "Alpine Trust leads. We audit Alpine Trust yearly. " +
		"The Alpine Trust burden grows. Alpine Trust again. Alpine Trust once more."
	got := SmartDetectWithOptions(text, NewEmptyAllowlist(), SmartDetectOptions{})
	c := findCandidate(got, "Alpine Trust")
	if c == nil {
		t.Fatalf("frequent candidate missing: %+v", got)
	}
	if c.Count < 3 {
		t.Errorf("count = %d, want at least 3", c.Count)
	}
	if len(c.Contexts) == 0 || len(c.Contexts) > 3 {
		t.Errorf("contexts must be 1..3 snippets, got %d", len(c.Contexts))
	}
	for _, ctx := range c.Contexts {
		if !strings.Contains(ctx, "Alpine Trust") {
			t.Errorf("context snippet must contain the candidate: %q", ctx)
		}
	}
	// Ranking: the most frequent candidate comes first.
	if len(got) > 1 && got[0].Count < got[1].Count {
		t.Errorf("candidates not ranked by count: %+v", got)
	}
}

func TestSmartDetectAllowlistWins(t *testing.T) {
	allow := NewEmptyAllowlist()
	allow.Add("CSSF")
	allow.Add("Luxembourg")
	text := "The CSSF reviewed our Luxembourg filing. The CSSF asked again. " +
		"Luxembourg rules apply. Alpine Trust S.A. responded."
	got := SmartDetectWithOptions(text, allow, SmartDetectOptions{})
	if findCandidate(got, "CSSF") != nil || findCandidate(got, "Luxembourg") != nil {
		t.Errorf("allowlisted terms must never be emitted: %+v", got)
	}
	if findCandidate(got, "Alpine Trust S.A.") == nil {
		t.Errorf("non-allowlisted candidate must survive: %+v", got)
	}
}

func TestSmartDetectFrenchFixture(t *testing.T) {
	text := "Réunion avec Mme Amélie Lefèvre et la société Lumière Conseil Sàrl. " +
		"Le projet Lumière Conseil Sàrl continue à Esch-sur-Alzette avec Amélie Lefèvre."
	got := SmartDetectWithOptions(text, NewEmptyAllowlist(), SmartDetectOptions{})
	if c := findCandidate(got, "Amélie Lefèvre"); c == nil || c.Category != "person_names" {
		t.Errorf("accented person name missing or misrouted: %+v", got)
	}
	if c := findCandidate(got, "Lumière Conseil Sàrl"); c == nil || c.Category != "entity_names" {
		t.Errorf("French company with Sàrl suffix missing or misrouted: %+v", got)
	}
}

// --- SmartDetectOptions --------------------------------------------------

// TestSmartDetectCandidatesCarryAScore: every candidate must carry the
// heuristic score, whether or not filtering is on, because the review UI
// sorts and filters on it without re-running detection.
func TestSmartDetectCandidatesCarryAScore(t *testing.T) {
	got := SmartDetectWithOptions("Alpine Trust S.A. signed. Marie Duval signed too.\n", NewEmptyAllowlist(), SmartDetectOptions{})
	if len(got) == 0 {
		t.Fatal("expected candidates")
	}
	for _, c := range got {
		if c.Confidence <= 0 || c.Confidence > 1 {
			t.Errorf("candidate %q scored %v, want a score in (0, 1]", c.Text, c.Confidence)
		}
	}
}

// TestCandidateScoreLadder pins the score each detector signal earns, in
// English and French. The ladder is the thing a future tuning change has
// to argue with, so it is asserted directly.
func TestCandidateScoreLadder(t *testing.T) {
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
			got := SmartDetectWithOptions(tc.text, NewEmptyAllowlist(), SmartDetectOptions{})
			scores := map[string]float32{}
			for _, c := range got {
				scores[c.Text] = c.Confidence
			}
			for text, want := range tc.want {
				if scores[text] != want {
					t.Errorf("%q scored %v, want %v (all: %+v)", text, scores[text], want, scores)
				}
			}
		})
	}
}

// TestSmartDetectOptionsFilters walks each option independently, so a
// failure names the knob that broke rather than "fewer candidates".
func TestSmartDetectOptionsFilters(t *testing.T) {
	// Anouk Berger sits MID-sentence on purpose: a name whose only
	// occurrence opens a sentence is dropped by the sentence-start rule,
	// which predates these options and is not what is under test here.
	const text = "Marie Duval called. Later Marie Duval wrote. March was busy. March was long.\n" +
		"Later that week Anouk Berger replied once.\n"

	has := func(cands []Candidate, want string) bool {
		for _, c := range cands {
			if c.Text == want {
				return true
			}
		}
		return false
	}

	cases := []struct {
		name     string
		opts     SmartDetectOptions
		mustKeep []string
		mustDrop []string
	}{
		{
			name:     "no options keeps the noise",
			opts:     SmartDetectOptions{},
			mustKeep: []string{"Marie Duval", "March", "Anouk Berger"},
		},
		{
			name:     "MinLength drops short candidates",
			opts:     SmartDetectOptions{MinLength: 6},
			mustKeep: []string{"Marie Duval", "Anouk Berger"},
			mustDrop: []string{"March"},
		},
		{
			name:     "MinOccurrences drops the single sighting",
			opts:     SmartDetectOptions{MinOccurrences: 2},
			mustKeep: []string{"Marie Duval", "March"},
			mustDrop: []string{"Anouk Berger"},
		},
		{
			name:     "ExcludeCommonWords drops the month, keeps the names",
			opts:     SmartDetectOptions{ExcludeCommonWords: true},
			mustKeep: []string{"Marie Duval", "Anouk Berger"},
			mustDrop: []string{"March"},
		},
		{
			name:     "MinConfidence drops the single-word repeat",
			opts:     SmartDetectOptions{MinConfidence: 0.5},
			mustKeep: []string{"Marie Duval", "Anouk Berger"},
			mustDrop: []string{"March"},
		},
		{
			name:     "the shipped defaults keep the names and drop the noise",
			opts:     DefaultSmartDetectOptions(),
			mustKeep: []string{"Marie Duval", "Anouk Berger"},
			mustDrop: []string{"March"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SmartDetectWithOptions(text, NewEmptyAllowlist(), tc.opts)
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
	got := SmartDetectWithOptions(
		"March Consulting signed. March Consulting invoiced.\n",
		NewEmptyAllowlist(),
		SmartDetectOptions{ExcludeCommonWords: true})
	found := false
	for _, c := range got {
		if c.Text == "March Consulting" {
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
	got := SmartDetectWithOptions(
		"Cependant le dossier avance. Cependant rien n'est signe.\n",
		NewEmptyAllowlist(),
		SmartDetectOptions{ExcludeCommonWords: true})
	for _, c := range got {
		if c.Text == "Cependant" {
			t.Errorf("a French sentence opener must be dropped, got %+v", got)
		}
	}
}

// TestAllowlistStillWinsOverTuning: an allowlisted term is dropped no
// matter how strongly the heuristics vouch for it (CLAUDE.md section 5).
func TestAllowlistStillWinsOverTuning(t *testing.T) {
	allow := NewEmptyAllowlist()
	allow.Add("Alpine Trust S.A.")
	got := SmartDetectWithOptions(
		"Alpine Trust S.A. signed the mandate.\n", allow, SmartDetectOptions{})
	for _, c := range got {
		if c.Text == "Alpine Trust S.A." {
			t.Errorf("an allowlisted term must never be proposed, got %+v", got)
		}
	}
}

// --- the offline pass must scale, and must be interruptible --------------

// TestExtractRunsScalesLinearly guards a quadratic hot spot that made
// detection look like it had hung on a real document.
//
// suffixBoundaryOK used to read its first character with []rune(rest)[0],
// where `rest` is the whole remainder of the document: one allocation and one
// scan of megabytes, per run. 800 KB took 15 seconds and 2.4 MB took about two
// minutes, which from the outside is exactly "detection sometimes does not
// complete". Decoding one rune in place made the same work linear.
//
// The assertion is a RATIO with a generous bound rather than a stopwatch
// threshold, so it fails on a return to quadratic behaviour and not on a busy
// CI runner.
func TestExtractRunsContextScalesLinearly(t *testing.T) {
	measure := func(repeats int) time.Duration {
		text := strings.Repeat("Alpine Trust S.A. met Marie Duval in Luxembourg. ", repeats)
		start := time.Now()
		extractRunsContext(context.Background(), text)
		return time.Since(start)
	}
	// Warm the code paths so the first measurement is not the one that pays
	// for lazily-initialised tables.
	measure(500)

	small := measure(4000)
	large := measure(16000)
	// Four times the input. Linear would be about 4x; quadratic is about 16x.
	// 8x leaves generous room for allocator noise while still failing loudly
	// on a reintroduced O(n^2).
	if large > 8*small {
		t.Errorf("extractRunsContext looks quadratic again: 4x the input took %v after %v (%.1fx)",
			large, small, float64(large)/float64(small))
	}
}

// TestSmartDetectContextIsInterruptible: before the offline pass took
// no context, so Cancel could only land BETWEEN documents and one large file
// ran to completion whatever the user pressed.
func TestSmartDetectContextIsInterruptible(t *testing.T) {
	text := strings.Repeat("Alpine Trust S.A. met Marie Duval in Luxembourg. ", 50000)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	got, err := SmartDetectContext(ctx, text, NewEmptyAllowlist(), DefaultSmartDetectOptions())
	if err == nil {
		t.Fatal("a cancelled context must be reported, not ignored")
	}
	if got != nil {
		t.Errorf("a cancelled scan returns no candidates, got %d", len(got))
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("cancellation took %v, which is not a cancellation", elapsed)
	}
}

// TestSmartDetectContextMatchesTheLegacyCall: the ctx-aware entry point must
// find exactly what the old one did when nothing cancels it.
func TestSmartDetectContextMatchesTheLegacyCall(t *testing.T) {
	text := "Alpine Trust S.A. met Marie Duval. Alpine Trust signed with Borealis Fund GmbH."
	opts := DefaultSmartDetectOptions()
	want := SmartDetectWithOptions(text, NewEmptyAllowlist(), opts)
	got, err := SmartDetectContext(context.Background(), text, NewEmptyAllowlist(), opts)
	if err != nil {
		t.Fatalf("an uncancelled scan must not fail: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d candidates, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i].Text != want[i].Text || got[i].Category != want[i].Category {
			t.Errorf("candidate %d differs: %+v vs %+v", i, got[i], want[i])
		}
	}
}

// --- Product signals -----------------------------------------------------

func TestProductDetection(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string // the candidate text expected under product_names
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
			got := SmartDetectWithOptions(tc.text, NewEmptyAllowlist(), SmartDetectOptions{})
			for _, c := range got {
				if c.Text == tc.want {
					if c.Category != CatProductNames {
						t.Fatalf("%q was filed as %s, want %s", c.Text, c.Category, CatProductNames)
					}
					return
				}
			}
			t.Fatalf("no candidate %q in %+v", tc.want, got)
		})
	}
}

func TestALegalFormBeatsAProductNoun(t *testing.T) {
	// A company that happens to sell a platform is still a company. The cue
	// ladder is ordered on purpose and this is its one ambiguous rung.
	got := SmartDetectWithOptions("Alpine Trust S.A. platform is live.\n",
		NewEmptyAllowlist(), SmartDetectOptions{})
	if len(got) == 0 || got[0].Text != "Alpine Trust S.A." {
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
	got := SmartDetectWithOptions("Ref. INV-88213 covers the projet ATLAS-2024.\n",
		NewEmptyAllowlist(), DefaultSmartDetectOptions())

	byText := map[string]Candidate{}
	for _, c := range got {
		byText[c.Text] = c
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
	got := SmartDetectWithOptions(text, NewEmptyAllowlist(), DefaultSmartDetectOptions())
	c := findCandidate(got, "Johannes Borch")
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
	got := SmartDetectWithOptions(text, NewEmptyAllowlist(), DefaultSmartDetectOptions())
	if c := findCandidate(got, "Kremser"); c == nil || c.Category != CatPersonNames {
		t.Errorf("bare surname not recognised as its email-named person: %+v", got)
	}
}

// TestSmartDetectEmailAccentFold: the body spells the name with accents
// ("José") while the address is ASCII ("jose"); the fold must bridge them.
func TestSmartDetectEmailAccentFold(t *testing.T) {
	text := "Cc: José Teixeira <jose.teixeira@pwc.lu> was copied. " +
		"José Teixeira replied.\n"
	got := SmartDetectWithOptions(text, NewEmptyAllowlist(), DefaultSmartDetectOptions())
	if c := findCandidate(got, "José Teixeira"); c == nil || c.Category != CatPersonNames {
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
	got := SmartDetectWithOptions(text, NewEmptyAllowlist(), DefaultSmartDetectOptions())
	if c := findCandidate(got, "Alpine Trust S.A."); c == nil || c.Category != CatEntityNames {
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
	got := SmartDetectWithOptions(text, NewEmptyAllowlist(), DefaultSmartDetectOptions())

	for _, noise := range []string{"Revenue Management", "Extra Holiday Buying", "General Terms of Sale"} {
		if c := findCandidate(got, noise); c != nil {
			t.Errorf("business-noun phrase %q must be dropped, got it as %+v", noise, c)
		}
	}
	if findCandidate(got, "Marie Duval") == nil {
		t.Errorf("a real name in the same text must survive: %+v", got)
	}
}

// --- strictness lever -------------------------------------------------------

// TestSmartDetectStrictRequiresAnAnchor: strict strictness emits only
// structurally-vouched candidates. A bare multi-word run (no suffix, title or
// email name) is dropped however it scored; a legal-suffix company survives.
func TestSmartDetectStrictRequiresAnAnchor(t *testing.T) {
	text := "The auditor Marie Duval reviewed it. Alpine Trust S.A. billed us.\n"

	strict := DefaultSmartDetectOptions()
	strict.Strictness = StrictnessStrict
	got := SmartDetectWithOptions(text, NewEmptyAllowlist(), strict)
	if findCandidate(got, "Marie Duval") != nil {
		t.Errorf("strict must drop a bare multi-word run: %+v", got)
	}
	if findCandidate(got, "Alpine Trust S.A.") == nil {
		t.Errorf("strict must keep a legal-suffix company: %+v", got)
	}

	// Balanced (the default) keeps the same bare run: strictness is the lever
	// that changed the outcome, nothing else.
	bal := SmartDetectWithOptions(text, NewEmptyAllowlist(), DefaultSmartDetectOptions())
	if findCandidate(bal, "Marie Duval") == nil {
		t.Errorf("balanced must keep the bare multi-word run: %+v", bal)
	}
}

// TestSmartDetectStrictKeepsEmailNamedPerson: an email-named person is
// structurally vouched, so strict keeps it even though it has no legal suffix.
func TestSmartDetectStrictKeepsEmailNamedPerson(t *testing.T) {
	text := "Contact Johannes Borch <johannes.borch@pwc.lu> today.\n"
	strict := DefaultSmartDetectOptions()
	strict.Strictness = StrictnessStrict
	got := SmartDetectWithOptions(text, NewEmptyAllowlist(), strict)
	if findCandidate(got, "Johannes Borch") == nil {
		t.Errorf("strict must keep an email-named person: %+v", got)
	}
}

// TestSmartDetectLenientKeepsRareSingleWord: lenient strictness keeps the
// single-word single-occurrence runs the frequency rule drops. They carry a
// low score, so this only surfaces them once the confidence floor is lowered.
func TestSmartDetectLenientKeepsRareSingleWord(t *testing.T) {
	text := "We discussed Zephyr briefly.\n"

	lenient := DefaultSmartDetectOptions()
	lenient.Strictness = StrictnessLenient
	lenient.MinConfidence = 0
	if findCandidate(SmartDetectWithOptions(text, NewEmptyAllowlist(), lenient), "Zephyr") == nil {
		t.Errorf("lenient with no floor must keep a rare single-word run")
	}

	bal := DefaultSmartDetectOptions()
	bal.MinConfidence = 0
	if findCandidate(SmartDetectWithOptions(text, NewEmptyAllowlist(), bal), "Zephyr") != nil {
		t.Errorf("balanced must drop a single-word single-occurrence run even with no floor")
	}
}

// TestSmartDetectSuppressesStreetAddress: a name that only ever appears in a
// postal-address context is a street, not a person. "rue Gerhard Mercator"
// must not propose "Gerhard Mercator" as someone to anonymise.
func TestSmartDetectSuppressesStreetAddress(t *testing.T) {
	text := "Our office is at 2, rue Gerhard Mercator in the capital. " +
		"Post to 2, rue Gerhard Mercator as well.\n"
	got := SmartDetectWithOptions(text, NewEmptyAllowlist(), DefaultSmartDetectOptions())
	if c := findCandidate(got, "Gerhard Mercator"); c != nil {
		t.Errorf("a street name must not be proposed, got %+v", c)
	}
}

// TestSmartDetectAddressCueInsideRun: the cue can be a token INSIDE the run
// ("Place de la Gare"), not only the word before it.
func TestSmartDetectAddressCueInsideRun(t *testing.T) {
	text := "The venue is Place de la Gare downtown. Place de la Gare hosts it.\n"
	got := SmartDetectWithOptions(text, NewEmptyAllowlist(), DefaultSmartDetectOptions())
	if c := findCandidate(got, "Place de la Gare"); c != nil {
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
	got := SmartDetectWithOptions(text, NewEmptyAllowlist(), DefaultSmartDetectOptions())
	if findCandidate(got, "Oscar Liber") == nil {
		t.Errorf("the signer named by an address must survive: %+v", got)
	}
	if c := findCandidate(got, "Gerhard Mercator"); c != nil {
		t.Errorf("the street on the address line must be dropped, got %+v", c)
	}
}

// TestSmartDetectStreetCueDoesNotSuppressSuffixedCompany: a legal suffix
// overrides address suppression, so a company at its own address survives.
func TestSmartDetectStreetCueDoesNotSuppressSuffixedCompany(t *testing.T) {
	text := "rue Alpine Trust S.A. is a coincidence. Alpine Trust S.A. billed us.\n"
	got := SmartDetectWithOptions(text, NewEmptyAllowlist(), DefaultSmartDetectOptions())
	if findCandidate(got, "Alpine Trust S.A.") == nil {
		t.Errorf("a legal-suffix company must survive an address cue: %+v", got)
	}
}

// TestSmartDetectConnectorsInsideCommonPhrase: "Terms of Sale" is all-common
// only if the connector "of" is skipped like a particle; without that skip the
// phrase would leak through as a candidate.
func TestSmartDetectConnectorsInsideCommonPhrase(t *testing.T) {
	if !isCommonWordRun("General Terms of Sale") {
		t.Errorf("a phrase of common nouns joined by a connector must read as all-common")
	}
	// A real name joined by a connector is NOT all-common: one token is a name.
	if isCommonWordRun("Bank of Marie") {
		t.Errorf("a run containing a genuine name token must not read as all-common")
	}
}
