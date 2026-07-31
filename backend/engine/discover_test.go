// engine/discover_test.go — BUILD-02 Phase 8 tests for the Smart
// detection tier, English AND French fixtures (CLAUDE.md §6).
package engine

import (
	"strings"
	"testing"
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
			got := SmartDetect(tc.text, NewEmptyAllowlist())
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
	got := SmartDetect("The GmbH structure is common. The GmbH form works.", NewEmptyAllowlist())
	if c := findCandidate(got, "GmbH"); c != nil {
		t.Errorf("bare suffix must not be a candidate: %+v", c)
	}
}

func TestSmartDetectCapitalisedRuns(t *testing.T) {
	text := "Yesterday we met Jean-Pierre Muller at the office. " +
		"Later, Anouk van den Berg joined the call with everyone."
	got := SmartDetect(text, NewEmptyAllowlist())

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
	once := SmartDetect("Nous avons signé. Ensuite tout le monde est parti.", NewEmptyAllowlist())
	if c := findCandidate(once, "Ensuite"); c != nil {
		t.Errorf("single sentence-start run must be dropped: %+v", c)
	}

	// "Borealis" opens two sentences: repeated sentence-start is kept.
	twice := SmartDetect("Borealis grew fast. Borealis hired again.", NewEmptyAllowlist())
	if findCandidate(twice, "Borealis") == nil {
		t.Errorf("repeated sentence-start run must be kept: %+v", twice)
	}
}

func TestSmartDetectSingleWordFrequency(t *testing.T) {
	// A single-word run appearing once mid-sentence, no suffix, no title:
	// dropped as noise.
	got := SmartDetect("The meeting covered Zephyr briefly today.", NewEmptyAllowlist())
	if c := findCandidate(got, "Zephyr"); c != nil {
		t.Errorf("single-occurrence single-word run must be dropped: %+v", c)
	}
	// The same word twice qualifies.
	got = SmartDetect("We value Zephyr highly. Everyone likes working with Zephyr daily.", NewEmptyAllowlist())
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
		got := SmartDetect(tc.text, NewEmptyAllowlist())
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
	got := SmartDetect(text, NewEmptyAllowlist())
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
	got := SmartDetect(text, allow)
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
	got := SmartDetect(text, NewEmptyAllowlist())
	if c := findCandidate(got, "Amélie Lefèvre"); c == nil || c.Category != "person_names" {
		t.Errorf("accented person name missing or misrouted: %+v", got)
	}
	if c := findCandidate(got, "Lumière Conseil Sàrl"); c == nil || c.Category != "entity_names" {
		t.Errorf("French company with Sàrl suffix missing or misrouted: %+v", got)
	}
}

// --- BUILD-04 CR13: SmartDetectOptions ------------------------------------

// TestSmartDetectLegacySignatureIsUnfiltered: the two-argument SmartDetect
// must behave exactly as it did before the options existed. Every earlier
// test in this file depends on that, and so does any caller that has
// nothing to say about tuning.
func TestSmartDetectLegacySignatureIsUnfiltered(t *testing.T) {
	const text = "Ltd was mentioned. Marie Duval called. Marie Duval called again.\n"
	legacy := SmartDetect(text, NewEmptyAllowlist())
	zero := SmartDetectWithOptions(text, NewEmptyAllowlist(), SmartDetectOptions{})
	if len(legacy) != len(zero) {
		t.Fatalf("legacy SmartDetect returned %d candidates, zero options returned %d; they must agree",
			len(legacy), len(zero))
	}
	for i := range legacy {
		if legacy[i].Text != zero[i].Text || legacy[i].Count != zero[i].Count {
			t.Errorf("candidate %d differs: %+v vs %+v", i, legacy[i], zero[i])
		}
	}
}

// TestSmartDetectCandidatesCarryAScore: every candidate must carry the
// heuristic score, whether or not filtering is on, because the review UI
// sorts and filters on it without re-running detection.
func TestSmartDetectCandidatesCarryAScore(t *testing.T) {
	got := SmartDetect("Alpine Trust S.A. signed. Marie Duval signed too.\n", NewEmptyAllowlist())
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
			got := SmartDetect(tc.text, NewEmptyAllowlist())
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
