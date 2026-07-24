// engine/report.go — per-file and aggregate anonymisation statistics
// (CLAUDE.md §3). The Report is what the user sees on the results screen
// and can export (Phase 9): JSON for machines, markdown for humans.
package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// DocumentReport carries the statistics of one processed document.
type DocumentReport struct {
	Name string `json:"name"`
	// Replacements is the total number of spans replaced in this document
	// (all passes combined, including the registry post-pass).
	Replacements int `json:"replacements"`
	// ByCategory splits Replacements by category ("email", "person_names",
	// "simple_replace", …).
	ByCategory map[string]int `json:"byCategory"`
	// Warnings are the document's ingestion + processing warnings.
	Warnings []string `json:"warnings,omitempty"`
	// LLMDurationMS is how long the deep-scan took for this document
	// (0 when the LLM pass was skipped). Soft budget: 30 s per 50 KB.
	LLMDurationMS int64 `json:"llmDurationMs,omitempty"`
}

// Report aggregates a whole pipeline run.
type Report struct {
	GeneratedAt time.Time `json:"generatedAt"`
	Level       Level     `json:"level"`
	// LLMPass documents what happened to pass 3, e.g.
	// "skipped (Ollama not available)" or "completed" or a degradation
	// note when Ollama died mid-run (Phase 10).
	LLMPass           string           `json:"llmPass"`
	TotalReplacements int              `json:"totalReplacements"`
	ByCategory        map[string]int   `json:"byCategory"`
	Documents         []DocumentReport `json:"documents"`
	DurationMS        int64            `json:"durationMs"`
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
// (BUILD.md Phase 9 activity 5).
func (r *Report) ToMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Anonymisation report\n\n")
	fmt.Fprintf(&b, "- Generated: %s\n", r.GeneratedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "- Level: %s\n", r.Level)
	fmt.Fprintf(&b, "- LLM deep-scan: %s\n", r.LLMPass)
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

	// LLM per-file timing is surfaced here (soft budget: 30 s per 50 KB
	// document, BUILD.md performance table); "—" when the pass was skipped.
	b.WriteString("## Per document\n\n| Document | Replacements | Deep-scan | Warnings |\n| --- | --- | --- | --- |\n")
	for _, d := range r.Documents {
		llm := "-"
		if d.LLMDurationMS > 0 {
			llm = fmt.Sprintf("%d ms", d.LLMDurationMS)
		}
		fmt.Fprintf(&b, "| %s | %d | %s | %s |\n", d.Name, d.Replacements, llm, strings.Join(d.Warnings, "; "))
	}

	if len(r.Warnings) > 0 {
		b.WriteString("\n## Run warnings\n\n")
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
	}
	return b.String()
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
