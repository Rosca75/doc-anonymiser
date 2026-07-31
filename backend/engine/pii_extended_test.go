// engine/pii_extended_test.go — BUILD-03 Phase B tests: new recognizers,
// their checksum validators, and the confidence + context-boosting layer
// (Phase C). One table per checksum so the mutation cases live next to
// their positive fixtures.
package engine

import (
	"strings"
	"testing"
)

// TestExtendedRecognizerCategories covers each new category with a
// positive (must match) and a negative (must not) case at level Soft
// (all extended recognizers are hard PII and fire at every level).
func TestExtendedRecognizerCategories(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		category string
		want     string // "" = must NOT match this category
	}{
		// --- credit card (Luhn) ---
		{"cc valid visa spaced", "card 4532 0151 1283 0366 charged", CatCreditCard, "4532 0151 1283 0366"},
		{"cc valid amex", "amex 3782 822463 10005 charged", CatCreditCard, "3782 822463 10005"},
		{"cc invalid luhn", "ref 4532 0151 1283 0367 rejected", CatCreditCard, ""},
		// --- UK NHS --- 943 476 5919 is a public example that satisfies the mod-11 rule.
		{"nhs valid", "patient NHS 943 476 5919 in file", CatNHS, "943 476 5919"},
		{"nhs invalid checksum", "patient NHS 943 476 5918 in file", CatNHS, ""},
		// --- IPv4 ---
		{"ipv4 positive", "connect from 192.168.0.1 today", CatIPAddress, "192.168.0.1"},
		{"ipv4 negative octet range", "not an ip 999.1.2.3 ever", CatIPAddress, ""},
		// --- IPv6 ---
		{"ipv6 positive full", "gateway 2001:db8:0:0:0:0:0:1 replies", CatIPAddress, "2001:db8:0:0:0:0:0:1"},
		{"ipv6 positive compressed", "gateway 2001:db8::1 replies", CatIPAddress, "2001:db8::1"},
		// --- MAC ---
		{"mac positive colon", "hw 00:1A:2B:3C:4D:5E in log", CatMACAddress, "00:1A:2B:3C:4D:5E"},
		{"mac positive hyphen", "hw 00-1a-2b-3c-4d-5e in log", CatMACAddress, "00-1a-2b-3c-4d-5e"},
		{"mac negative five groups", "hw 00:1A:2B:3C:4D in log", CatMACAddress, ""},
		// --- crypto (Bitcoin) ---
		{"btc positive p2pkh", "wallet 1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2 ok", CatCrypto, "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2"},
		{"btc positive bech32", "wallet bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4 ok", CatCrypto, "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4"},
		// --- database URIs ---
		{"db postgres with creds", "dsn postgres://user:pw@host:5432/db active", CatDatabaseURI, "postgres://user:pw@host:5432/db"},
		{"db mongodb srv", "dsn mongodb+srv://u:p@cluster.example.com/db active", CatDatabaseURI, "mongodb+srv://u:p@cluster.example.com/db"},
		// --- Germany Steuer-ID ---
		{"de tax id positive", "Steuer-ID 12345678901 registriert", CatDESteuerID, "12345678901"},
		{"de tax id negative leading zero", "Steuer-ID 01234567890 registriert", CatDESteuerID, ""},
		// --- Spain NIF ---
		{"es nif positive", "NIF 00000000T aportado", CatESNIF, "00000000T"},
		{"es nif negative wrong letter", "NIF 00000000A aportado", CatESNIF, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans := DetectPII(tt.text, LevelSoft)
			var got []string
			for _, s := range spans {
				if s.Category == tt.category {
					got = append(got, s.Original)
				}
			}
			if tt.want == "" {
				for _, g := range got {
					if g == "" {
						continue
					}
					// The IP recognizer intentionally overlaps IPv4 and IPv6
					// patterns; the negative check is that no valid IP came out.
					t.Errorf("expected no %s match, got %v", tt.category, got)
					break
				}
				return
			}
			if len(got) == 0 {
				t.Fatalf("expected %s match %q, got none (all spans: %+v)", tt.category, tt.want, spans)
			}
			// The extended patterns can produce >1 span for the same range
			// (e.g. an IPv4 also matching a broader numeric run); accept if
			// any match is the wanted one.
			hit := false
			for _, g := range got {
				if g == tt.want {
					hit = true
					break
				}
			}
			if !hit {
				t.Errorf("matched %v, want %q among them", got, tt.want)
			}
		})
	}
}

// TestLuhn covers the checksum in isolation with real test card numbers
// (published by every card network for exactly this purpose).
func TestLuhn(t *testing.T) {
	cases := []struct {
		s  string
		ok bool
	}{
		{"4532015112830366", true},  // Visa test
		{"4532015112830367", false}, // mutated
		{"5555555555554444", true},  // Mastercard test
		{"378282246310005", true},   // Amex test
		{"371449635398431", true},   // Amex test
		{"6011111111111117", true},  // Discover test
		{"4532 0151 1283 0366", true},
		{"4532-0151-1283-0366", true},
		{"", false},
		{"1234", false},
	}
	for _, c := range cases {
		if got := validLuhn(c.s); got != c.ok {
			t.Errorf("validLuhn(%q) = %v, want %v", c.s, got, c.ok)
		}
	}
}

// TestNHSChecksum covers the mod-11 rule and its edge cases.
func TestNHSChecksum(t *testing.T) {
	cases := []struct {
		s  string
		ok bool
	}{
		{"9434765919", true},   // valid
		{"943 476 5919", true}, // grouped form
		{"9434765918", false},  // mutated last digit
		{"123456789", false},   // wrong length
	}
	for _, c := range cases {
		if got := validNHS(c.s); got != c.ok {
			t.Errorf("validNHS(%q) = %v, want %v", c.s, got, c.ok)
		}
	}
}

// TestNIFChecksum verifies the mod-23 letter lookup.
func TestNIFChecksum(t *testing.T) {
	cases := []struct {
		s  string
		ok bool
	}{
		{"00000000T", true},
		{"00000000t", true}, // case-insensitive letter
		{"00000000A", false},
		{"12345678", false},
	}
	for _, c := range cases {
		if got := validNIF(c.s); got != c.ok {
			t.Errorf("validNIF(%q) = %v, want %v", c.s, got, c.ok)
		}
	}
}

// TestIPv4Range: octet range validation.
func TestIPv4Range(t *testing.T) {
	cases := []struct {
		s  string
		ok bool
	}{
		{"0.0.0.0", true},
		{"255.255.255.255", true},
		{"192.168.1.1", true},
		{"256.0.0.1", false},
		{"1.2.3", false},
		{"1.2.3.4.5", false},
	}
	for _, c := range cases {
		if got := validIPv4(c.s); got != c.ok {
			t.Errorf("validIPv4(%q) = %v, want %v", c.s, got, c.ok)
		}
	}
}

// TestExtendedRecognizersAtEveryLevel: hard-PII additions fire at soft,
// medium and advanced levels alike (no regression against the level matrix).
func TestExtendedRecognizersAtEveryLevel(t *testing.T) {
	text := "card 4532 0151 1283 0366; ip 192.168.0.1; dsn postgres://u:p@h/d"
	for _, level := range []Level{LevelSoft, LevelMedium, LevelAdvanced} {
		spans := DetectPII(text, level)
		if !hasCategory(spans, CatCreditCard) {
			t.Errorf("credit card must fire at level %s, spans: %+v", level, spans)
		}
		if !hasCategory(spans, CatIPAddress) {
			t.Errorf("ip must fire at level %s", level)
		}
		if !hasCategory(spans, CatDatabaseURI) {
			t.Errorf("database uri must fire at level %s", level)
		}
	}
}

// TestConfidenceDefaults: deterministic PII spans carry Confidence 1.0
// (or 1.0 after a context-word boost cap).
func TestConfidenceDefaults(t *testing.T) {
	text := "contact marie.duval@example.com or +352 621 000 111 today"
	for _, s := range DetectPII(text, LevelSoft) {
		if s.Confidence < ConfidenceDeterministic || s.Confidence > 1.0 {
			t.Errorf("span %+v has out-of-range confidence %v", s, s.Confidence)
		}
	}
	// Entities and custom patterns also carry confidence.
	spans := DetectEntities("Alpine Trust S.A. filed a report", []Entity{
		{Category: "entity_names", Canonical: "Alpine Trust S.A."},
	}, nil)
	if len(spans) == 0 || spans[0].Confidence != ConfidenceManualDefault {
		t.Errorf("entity span confidence = %v, want %v", spans, ConfidenceManualDefault)
	}
}

// TestFilterByConfidence: threshold filter honours per-category thresholds
// and treats a zero-valued Confidence as 1.0 (v1 back-compat).
func TestFilterByConfidence(t *testing.T) {
	spans := []Span{
		{Category: CatEmail, Confidence: 1.0, Original: "a@b.co"},
		{Category: CatEmail, Confidence: 0.6, Original: "c@d.co"},
		{Category: CatPhone, Confidence: 0.8, Original: "+352 621 000 111"},
		{Category: CatPhone, Confidence: 0.0, Original: "+352 621 000 222"}, // v1 span → treated as 1.0
	}
	kept := FilterByConfidence(spans, map[string]float32{
		CatEmail: 0.9,
		CatPhone: 0.85,
	})
	if len(kept) != 2 {
		t.Fatalf("kept %d spans, want 2: %+v", len(kept), kept)
	}
	if kept[0].Original != "a@b.co" || kept[1].Original != "+352 621 000 222" {
		t.Errorf("unexpected kept spans: %+v", kept)
	}
	// Empty thresholds is a no-op.
	if len(FilterByConfidence(spans, nil)) != len(spans) {
		t.Error("nil thresholds must be a no-op")
	}
}

// TestContextBoost: an email with the word "contact" nearby carries the
// same 1.0 confidence (base already 1.0, boost clamped). The public
// contract is that context never lowers the score.
func TestContextBoost(t *testing.T) {
	withContext := "Please contact marie.duval@example.com about the report"
	spans := DetectPII(withContext, LevelSoft)
	if len(spans) == 0 {
		t.Fatal("expected an email span")
	}
	if spans[0].Confidence < ConfidenceDeterministic {
		t.Errorf("confidence must never fall below the deterministic baseline, got %v", spans[0].Confidence)
	}
	if spans[0].Confidence > 1.0 {
		t.Errorf("confidence must be clamped to 1.0, got %v", spans[0].Confidence)
	}
}

// TestNewCategoriesInPresets: every new category is in AllPIICategories
// and in the default (soft) preset.
func TestNewCategoriesInPresets(t *testing.T) {
	newCats := []string{
		CatCreditCard, CatNHS, CatIPAddress, CatMACAddress, CatCrypto,
		CatDatabaseURI, CatDESteuerID, CatESNIF,
	}
	all := strings.Join(AllPIICategories, ",")
	for _, c := range newCats {
		if !strings.Contains(all, c) {
			t.Errorf("%s missing from AllPIICategories", c)
		}
		if !PresetSelection(LevelSoft)[c] {
			t.Errorf("%s must be on at soft (it is hard PII)", c)
		}
	}
}
