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
			if c.Category != "client_names" {
				t.Errorf("suffix candidate category = %s, want client_names", c.Category)
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
	if c := findCandidate(got, "Lumière Conseil Sàrl"); c == nil || c.Category != "client_names" {
		t.Errorf("French company with Sàrl suffix missing or misrouted: %+v", got)
	}
}
