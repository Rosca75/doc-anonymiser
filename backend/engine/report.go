// engine/report.go — per-file and aggregate anonymisation statistics
// (CLAUDE.md §3). The Report is what the user sees on the results screen
// and can export: JSON for machines, markdown for humans.
package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ValueReport is ONE replaced value: what it was, what it became, and how
// many times it was replaced in the scope this report covers.
//
// It exists because a per-CATEGORY total answers "how much was replaced" and
// nothing else. The question a user actually has after a run is "what did you
// replace?", and until the only place that could answer it was a
// drill-down in the UI that recomputed the counts from the anonymised text on
// every repaint, and an exported report that did not contain them at all.
//
// SENSITIVITY: Original is the real value. A report carrying these IS a
// re-identification key, which is why the export path warns before writing it.
type ValueReport struct {
	Original    string `json:"original"`
	Placeholder string `json:"placeholder"`
	Category    string `json:"category"`
	Count       int    `json:"count"`
}

// DocumentReport carries the statistics of one processed document.
type DocumentReport struct {
	Name string `json:"name"`
	// Replacements is the total number of spans replaced in this document
	// (all passes combined, including the registry post-pass).
	Replacements int `json:"replacements"`
	// ByCategory splits Replacements by category ("email", "person_names", …).
	ByCategory map[string]int `json:"byCategory"`
	// Warnings are the document's ingestion + processing warnings.
	Warnings []string `json:"warnings,omitempty"`
	// Values lists what was replaced IN THIS DOCUMENT, most frequent first.
	Values []ValueReport `json:"values,omitempty"`
	// DetectedCategories lists the categories that actually produced a value in
	// this document, sorted alphabetically. It is a set-shaped summary for the
	// UI: "which kinds of thing did this file contain?" without making the
	// frontend derive it again from ByCategory.
	DetectedCategories []string `json:"detectedCategories,omitempty"`
}

// Report aggregates a whole pipeline run.
type Report struct {
	GeneratedAt       time.Time      `json:"generatedAt"`
	Level             Level          `json:"level"`
	TotalReplacements int            `json:"totalReplacements"`
	ByCategory        map[string]int `json:"byCategory"`
	// Values lists every replaced value across the whole run, most frequent
	// first. This is what the Anonymise screen's report shows and what the
	// exported report was missing.
	Values     []ValueReport    `json:"values,omitempty"`
	Documents  []DocumentReport `json:"documents"`
	DurationMS int64            `json:"durationMs"`
	// DetectedCategories is the run-level union of categories that actually
	// replaced at least one value anywhere in the batch, sorted alphabetically.
	DetectedCategories []string `json:"detectedCategories,omitempty"`
	// Warnings are run-level notes (not tied to one document).
	Warnings []string `json:"warnings,omitempty"`
}

// ToJSON serialises the report for the UI and the JSON export.
func (r *Report) ToJSON() ([]byte, error) {
	out, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		// Practically unreachable (the struct is plain data), but never
		// silently swallowed.
		return nil, fmt.Errorf("could not serialise the report to JSON: %w", err)
	}
	return out, nil
}

// ToMarkdown renders the human-readable summary used by the report export
func (r *Report) ToMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Anonymisation report\n\n")
	fmt.Fprintf(&b, "- Generated: %s\n", r.GeneratedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "- Level: %s\n", r.Level)
	fmt.Fprintf(&b, "- Documents processed: %d\n", len(r.Documents))
	fmt.Fprintf(&b, "- Total replacements: %d\n", r.TotalReplacements)
	fmt.Fprintf(&b, "- Duration: %d ms\n\n", r.DurationMS)

	if len(r.ByCategory) > 0 {
		b.WriteString("## Replacements by category\n\n| Category | Count |\n| --- | --- |\n")
		for _, cat := range sortedKeys(r.ByCategory) {
			fmt.Fprintf(&b, "| %s | %d |\n", cat, r.ByCategory[cat])
		}
		b.WriteString("\n")
	}

	if len(r.Values) > 0 {
		b.WriteString("## Replaced values\n\n")
		b.WriteString("This table maps each placeholder back to the real value, so it is a\n")
		b.WriteString("re-identification key. Store it accordingly.\n\n")
		b.WriteString("| Value | Placeholder | Category | Replacements |\n| --- | --- | --- | --- |\n")
		for _, v := range r.Values {
			fmt.Fprintf(&b, "| %s | %s | %s | %d |\n",
				escapeCell(v.Original), v.Placeholder, v.Category, v.Count)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Per document\n\n| Document | Replacements | Warnings |\n| --- | --- | --- |\n")
	for _, d := range r.Documents {
		fmt.Fprintf(&b, "| %s | %d | %s |\n", d.Name, d.Replacements, strings.Join(d.Warnings, "; "))
	}

	if len(r.Warnings) > 0 {
		b.WriteString("\n## Run warnings\n\n")
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
	}
	return b.String()
}

// escapeCell keeps a value containing a pipe from breaking the markdown
// table it is printed in. Document text can contain anything.
func escapeCell(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// sortedKeys returns map keys alphabetically — deterministic reports make
// stable golden tests and diffs.
func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
