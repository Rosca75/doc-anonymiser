// report_test.go — the per-value report.
//
// "Many values were replaced within the document loaded, but I couldn't find
// any summary of the values replaced within the Report area." The report
// carried per-CATEGORY totals only; the values existed nowhere in Go, so the
// exported report could not contain them either and the screen had to recount
// them from the anonymised text on every repaint.
package engine

import (
	"context"
	"strings"
	"testing"
)

// runForReport anonymises two documents with values that hit both of them,
// so the per-run and per-document value lists differ.
func runForReport(t *testing.T) *Results {
	t.Helper()
	res, err := Run(context.Background(), PipelineInput{
		Documents: []Document{
			{
				Name: "a.txt", Format: FormatTXT,
				Markdown: "Marie Duval met Marie Duval again at Meridian Consulting.",
			},
			{
				Name: "b.txt", Format: FormatTXT,
				Markdown: "Thomas Berger wrote to marie.duval@example.com.",
			},
		},
		Values: []Value{
			{Category: "person_names", MainText: "Marie Duval"},
			{Category: "person_names", MainText: "Thomas Berger"},
			{Category: "entity_names", MainText: "Meridian Consulting"},
		},
		Categories: DepthSelection(PresetStandard, CountryLU),
		Allowlist:  NewEmptyAllowlist(),
		Registry:   NewRegistry(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

func TestReportListsEveryReplacedValue(t *testing.T) {
	res := runForReport(t)
	if len(res.Report.Values) == 0 {
		t.Fatal("the report must list what was replaced, not only how much")
	}

	byPlaceholder := map[string]ValueReport{}
	for _, v := range res.Report.Values {
		byPlaceholder[v.Placeholder] = v
	}
	marie, ok := byPlaceholder["[PERSON_1]"]
	if !ok {
		t.Fatalf("Marie Duval is missing from the value list: %+v", res.Report.Values)
	}
	if marie.Original != "Marie Duval" {
		t.Errorf("the value list must carry the ORIGINAL, got %q", marie.Original)
	}
	if marie.Category != "person_names" {
		t.Errorf("category is %q, want person_names", marie.Category)
	}
	if marie.Count != 2 {
		t.Errorf("Marie Duval occurs twice in a.txt, the report says %d", marie.Count)
	}
}

func TestReportValuesAreSortedByFrequency(t *testing.T) {
	// Most frequent first: the value that matters most is the one that was
	// replaced most, and a stable order stops rows swapping between renders.
	values := runForReport(t).Report.Values
	for i := 1; i < len(values); i++ {
		if values[i-1].Count < values[i].Count {
			t.Errorf("value %d (%d) sorts before %d (%d)",
				i-1, values[i-1].Count, i, values[i].Count)
		}
	}
}

func TestReportValuesAddUpToTheCategoryTotals(t *testing.T) {
	// The two figures on the same screen must agree, or one of them is wrong
	// and the user cannot tell which.
	res := runForReport(t)
	perCategory := map[string]int{}
	for _, v := range res.Report.Values {
		perCategory[v.Category] += v.Count
	}
	for category, total := range perCategory {
		if res.Report.ByCategory[category] != total {
			t.Errorf("category %q: the values sum to %d but ByCategory says %d",
				category, total, res.Report.ByCategory[category])
		}
	}
}

func TestPerDocumentValuesAreScopedToThatDocument(t *testing.T) {
	// This is what makes the report's scope selector mean something.
	res := runForReport(t)
	for _, doc := range res.Report.Documents {
		for _, v := range doc.Values {
			if v.Count == 0 {
				t.Errorf("%s lists %s with a count of zero", doc.Name, v.Placeholder)
			}
		}
	}
	var a, b DocumentReport
	for _, doc := range res.Report.Documents {
		if doc.Name == "a.txt" {
			a = doc
		} else {
			b = doc
		}
	}
	has := func(values []ValueReport, placeholder string) bool {
		for _, v := range values {
			if v.Placeholder == placeholder {
				return true
			}
		}
		return false
	}
	if !has(a.Values, "[PERSON_1]") {
		t.Error("a.txt replaced Marie Duval, so its value list must say so")
	}
	if has(b.Values, "[PERSON_1]") {
		t.Error("Marie Duval does not appear in b.txt; a scoped list must not claim she does")
	}
}

func TestDetectedCategoriesFollowActualReplacements(t *testing.T) {
	res := runForReport(t)
	if strings.Join(res.Report.DetectedCategories, ",") != "email,entity_names,person_names" {
		t.Fatalf("run detected categories = %v, want [email entity_names person_names]", res.Report.DetectedCategories)
	}

	var a, b DocumentReport
	for _, doc := range res.Report.Documents {
		if doc.Name == "a.txt" {
			a = doc
		}
		if doc.Name == "b.txt" {
			b = doc
		}
	}
	if strings.Join(a.DetectedCategories, ",") != "entity_names,person_names" {
		t.Errorf("a.txt detected categories = %v", a.DetectedCategories)
	}
	if strings.Join(b.DetectedCategories, ",") != "email,person_names" {
		t.Errorf("b.txt detected categories = %v", b.DetectedCategories)
	}
}

func TestReportMarkdownContainsTheValueTable(t *testing.T) {
	// The exported report had no per-value data at all, so it could not be used
	// to check what a batch replaced without opening the application again.
	md := runForReport(t).Report.ToMarkdown()
	if !strings.Contains(md, "## Replaced values") {
		t.Fatal("the markdown report must contain the value table")
	}
	if !strings.Contains(md, "Marie Duval") || !strings.Contains(md, "[PERSON_1]") {
		t.Errorf("the value table must map originals to placeholders:\n%s", md)
	}
	if !strings.Contains(md, "re-identification key") {
		t.Error("a table of real values must say what it is")
	}
}

func TestReportMarkdownEscapesAPipeInAValue(t *testing.T) {
	// Document text can contain anything, and one pipe would silently break
	// the table it is printed in.
	r := Report{Values: []ValueReport{
		{Original: "Ace | Co", Placeholder: "[ENTITY_1]", Category: "entity_names", Count: 1},
	}}
	if !strings.Contains(r.ToMarkdown(), `Ace \| Co`) {
		t.Errorf("a pipe in a value must be escaped:\n%s", r.ToMarkdown())
	}
}
