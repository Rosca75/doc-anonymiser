// engine/pii_extended_test.go — BUILD-03 Phase B tests: new recognizers,
// their checksum validators, and the confidence layer. One table per checksum
// so the mutation cases live next to their positive fixtures.
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
		country  string
		category string
		want     string // "" = must NOT match this category
	}{
		// --- credit card (Luhn) ---
		{"cc valid visa spaced", "card 4532 0151 1283 0366 charged", CountryLU, CatCreditCard, "4532 0151 1283 0366"},
		{"cc valid amex", "amex 3782 822463 10005 charged", CountryLU, CatCreditCard, "3782 822463 10005"},
		{"cc invalid luhn", "ref 4532 0151 1283 0367 rejected", CountryLU, CatCreditCard, ""},
		// --- UK NHS --- 943 476 5919 is a public example that satisfies the mod-11 rule.
		{"nhs valid", "patient NHS 943 476 5919 in file", CountryUK, CatNHS, "943 476 5919"},
		{"nhs invalid checksum", "patient NHS 943 476 5918 in file", CountryUK, CatNHS, ""},
		// --- IPv4 ---
		{"ipv4 positive", "connect from 192.168.0.1 today", CountryLU, CatIPAddress, "192.168.0.1"},
		{"ipv4 negative octet range", "not an ip 999.1.2.3 ever", CountryLU, CatIPAddress, ""},
		// --- IPv6 ---
		{"ipv6 positive full", "gateway 2001:db8:0:0:0:0:0:1 replies", CountryLU, CatIPAddress, "2001:db8:0:0:0:0:0:1"},
		{"ipv6 positive compressed", "gateway 2001:db8::1 replies", CountryLU, CatIPAddress, "2001:db8::1"},
		// --- MAC ---
		{"mac positive colon", "hw 00:1A:2B:3C:4D:5E in log", CountryLU, CatMACAddress, "00:1A:2B:3C:4D:5E"},
		{"mac positive hyphen", "hw 00-1a-2b-3c-4d-5e in log", CountryLU, CatMACAddress, "00-1a-2b-3c-4d-5e"},
		{"mac negative five groups", "hw 00:1A:2B:3C:4D in log", CountryLU, CatMACAddress, ""},
		// --- crypto (Bitcoin) ---
		{"btc positive p2pkh", "wallet 1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2 ok", CountryLU, CatCrypto, "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2"},
		{"btc positive bech32", "wallet bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4 ok", CountryLU, CatCrypto, "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4"},
		// --- database URIs ---
		{"db postgres with creds", "dsn postgres://alice:secret@host:5432/db active", CountryLU, CatDatabaseURI, "postgres://alice:secret@host:5432/db"},
		{"db mongodb srv", "dsn mongodb+srv://u:p@cluster.example.com/db active", CountryLU, CatDatabaseURI, "mongodb+srv://u:p@cluster.example.com/db"},
		// --- Germany Steuer-ID ---
		{"de tax id positive", "Steuer-ID 12345678901 registriert", CountryDE, CatDESteuerID, "12345678901"},
		{"de tax id negative leading zero", "Steuer-ID 01234567890 registriert", CountryDE, CatDESteuerID, ""},
		// --- Spain NIF ---
		{"es nif positive", "NIF 00000000T aportado", CountryES, CatESNIF, "00000000T"},
		{"es nif negative wrong letter", "NIF 00000000A aportado", CountryES, CatESNIF, ""},
		// --- country scoping (BUILD-06 Phase 1) --- a national category is
		// off entirely when another country is selected, so neither the
		// German tax ID nor the Spanish NIF fires under a Luxembourg run.
		{"de tax id negative under LU selection", "Steuer-ID 12345678901 registriert", CountryLU, CatDESteuerID, ""},
		{"es nif negative under LU selection", "NIF 00000000T aportado", CountryLU, CatESNIF, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans := DetectPIISelected(tt.text, PresetSelection(LevelSoft), tt.country)
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
		spans := DetectPIISelected(text, PresetSelection(level), CountryLU)
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
	for _, s := range DetectPIISelected(text, PresetSelection(LevelSoft), CountryLU) {
		if s.Confidence < ConfidenceDeterministic || s.Confidence > 1.0 {
			t.Errorf("span %+v has out-of-range confidence %v", s, s.Confidence)
		}
	}
	// Entities and custom patterns also carry confidence.
	spans := DetectEntities("Alpine Trust S.A. filed a report", []Entity{
		{Category: CatEntityNames, Canonical: "Alpine Trust S.A."},
	}, nil)
	if len(spans) == 0 || spans[0].Confidence != ConfidenceManualDefault {
		t.Errorf("entity span confidence = %v, want %v", spans, ConfidenceManualDefault)
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
