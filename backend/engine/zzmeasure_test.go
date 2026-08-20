//go:build measure

package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func loadMD(t *testing.T, name string) string {
	raw, err := os.ReadFile("../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	docs, err := LoadAll(name, raw)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, d := range docs {
		b.WriteString(d.Markdown)
	}
	return b.String()
}

func TestMeasureBaseline(t *testing.T) {
	src := loadMD(t, "framework_agreement.docx")
	os.WriteFile("/tmp/src.md", []byte(src), 0o644)
	tgt := loadMD(t, "framework_agreement_anon.docx")
	os.WriteFile("/tmp/tgt.md", []byte(tgt), 0o644)

	sel := PresetSelection(LevelAdvanced)
	spans := DetectPIISelected(src, sel, CountryLU)
	seen := map[string]bool{}
	for _, s := range spans {
		seen[s.Category+" | "+s.Original] = true
	}
	fmt.Println("=== PASS 1 distinct matches:", len(seen))
	for k := range seen {
		fmt.Println("  ", k)
	}

	allow := NewAllowlist()
	allow.Remove("Luxembourg")
	defined := DiscoverDefinedTerms("framework_agreement.docx", src)
	fmt.Println("=== DEFINED TERMS:", len(defined))
	for _, d := range defined {
		fmt.Printf("   %-14s %s\n", d.Idiom, d.Term)
	}
	ApplyDefinedTerms(allow, defined)
	sugs, err := HeuristicDiscoverContext(context.Background(), src, allow, DefaultHeuristicDiscoveryOptions(), CountryLU)
	if err != nil {
		t.Fatal(err)
	}
	folded := FoldValueFamilies(sugs, allow)
	fmt.Println("=== FOLDED suggestions:", len(folded))
	for _, s := range folded {
		fmt.Printf("   %-22s %-18s %.2f  %v\n", s.Category, s.MainText, s.Confidence, s.Spellings)
	}
}
