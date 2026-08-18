//go:build integration

// engine/exportfmt/metadata_integration_test.go — document-property
// extraction, proposal and reviewed-rewrite round-trips.
//
// TIER: integration (docs/TESTING.md). These decode and rewrite real docx
// property parts (core/app/custom XML inside the OOXML zip), so they exercise
// real-format behaviour, not pure logic. The bespoke-fixture builders
// (buildPropsDocx) and the zip reader (extractEntry) they use are shared
// helpers in the untagged exportfmt_test.go.
package exportfmt

import (
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
)

func TestMetadataExtractAndPropose(t *testing.T) {
	raw := buildPropsDocx(t)
	fields, err := ExtractMetadata(raw)
	if err != nil {
		t.Fatalf("ExtractMetadata: %v", err)
	}
	byName := map[string]MetaField{}
	for _, f := range fields {
		byName[f.Part+"|"+f.Name] = f
	}
	if f := byName["docProps/core.xml|creator"]; f.Value != "Marie Duval" {
		t.Errorf("creator = %+v", f)
	}
	if f := byName["docProps/app.xml|Company"]; f.Value != "Alpine Trust S.A." {
		t.Errorf("Company = %+v", f)
	}
	if f := byName["docProps/custom.xml|Client"]; f.Value != "Alpine Trust" {
		t.Errorf("custom Client = %+v", f)
	}
	if f := byName["docProps/core.xml|description"]; f.Value != "Prepared for Alpine Trust & partners" {
		t.Errorf("entity-decoded description = %+v", f)
	}
	if _, ok := byName["docProps/custom.xml|Reviewed"]; ok {
		t.Error("non-string custom property must not be harvested")
	}

	// Proposals: pipeline path, allowlist wins ("CSSF" survives in the
	// subject although it looks like an organisation).
	cfg := testConfig(
		engine.Value{Category: "entity_names", MainText: "Alpine Trust"},
		engine.Value{Category: "person_names", MainText: "Marie Duval"},
	)
	cfg.Allowlist.Add("CSSF")
	props := ProposeMetadata(fields, cfg)
	byKey := map[string]MetaProposal{}
	for _, p := range props {
		byKey[p.Part+"|"+p.Name] = p
	}
	if p := byKey["docProps/core.xml|creator"]; !p.Changed || !strings.Contains(p.Proposed, "[PERSON_") {
		t.Errorf("creator proposal = %+v", p)
	}
	if p := byKey["docProps/core.xml|subject"]; strings.Contains(p.Proposed, "[") || !strings.Contains(p.Proposed, "CSSF") {
		t.Errorf("allowlisted CSSF must survive in the subject: %+v", p)
	}
	if p := byKey["docProps/app.xml|Pages"]; p.Part != "" {
		t.Errorf("Pages is not a harvested field: %+v", p)
	}
}

func TestMetadataRewriteRoundTrip(t *testing.T) {
	raw := buildPropsDocx(t)
	reviewed := []MetaField{
		{Part: "docProps/core.xml", Name: "creator", Value: "[PERSON_1]"},
		{Part: "docProps/core.xml", Name: "title", Value: "[ENTITY_1] engagement report"},
		{Part: "docProps/app.xml", Name: "Company", Value: "[ENTITY_1]"},
		{Part: "docProps/custom.xml", Name: "Client", Value: "[ENTITY_1]"},
		// A rejected field simply is not in this list: lastModifiedBy
		// keeps "Marie Duval".
	}
	out, err := RewriteMetadata(raw, reviewed)
	if err != nil {
		t.Fatalf("RewriteMetadata: %v", err)
	}
	fields, err := ExtractMetadata(out)
	if err != nil {
		t.Fatalf("re-extract: %v", err)
	}
	byName := map[string]string{}
	for _, f := range fields {
		byName[f.Part+"|"+f.Name] = f.Value
	}
	if byName["docProps/core.xml|creator"] != "[PERSON_1]" ||
		byName["docProps/core.xml|title"] != "[ENTITY_1] engagement report" ||
		byName["docProps/app.xml|Company"] != "[ENTITY_1]" ||
		byName["docProps/custom.xml|Client"] != "[ENTITY_1]" {
		t.Errorf("reviewed values did not round-trip: %v", byName)
	}
	if byName["docProps/core.xml|lastModifiedBy"] != "Marie Duval" {
		t.Errorf("rejected field must keep its original value: %v", byName)
	}

	// The boolean custom property survived verbatim.
	outStr := extractEntry(t, out, "docProps/custom.xml")
	if !strings.Contains(outStr, "<vt:bool>true</vt:bool>") {
		t.Errorf("non-string custom property was touched:\n%s", outStr)
	}
	// Word count metadata part in app.xml untouched too.
	if app := extractEntry(t, out, "docProps/app.xml"); !strings.Contains(app, "<Pages>3</Pages>") {
		t.Errorf("unharvested app field was touched:\n%s", app)
	}
}
