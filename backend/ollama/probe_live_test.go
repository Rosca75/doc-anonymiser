//go:build live

// ollama/probe_live_test.go — the measurement instrument for the local model route.
//
// TIER: none of the three in docs/TESTING.md, and that is deliberate. It needs a
// real Ollama, a real model and a document that is not in this repository, so it
// is neither unit, integration nor deep; the `live` build tag keeps it out of
// every suite, including `-tags=integration,deep`. Nothing runs it by accident,
// and `go vet -tags=integration,deep ./...` does not even compile it.
//
// It is committed rather than rebuilt each time because the questions it answers
// are questions about the SHIPPED code that no mock can answer: how many requests
// a real document becomes, how long each one takes on this machine, whether the
// model went silent, and whether its reply was cut off. Two sessions have now
// written this harness and deleted it, and each time the next session had to
// choose between re-writing it and guessing.
//
// It PRINTS rather than asserts. There is no pass condition for "how fast is a
// 4B model on this laptop": the output is evidence for a human, so a run that
// completes is a successful run whatever the numbers say.
//
// It drives the production path deliberately: engine.ScanChunks decides the
// slices, buildChatRequest shapes the body and postChat sends it, so a number it
// prints is a number the application would produce. The one thing it does NOT do
// is go through DiscoverSlices, because that merges the slices together and the
// per-slice detail is the whole point.
//
// Usage (PowerShell, from the repository root):
//
//	$env:PROBE_DOC   = "C:\path\to\document.pptx"   # required
//	$env:PROBE_MODEL = "Qwen3.5-4B-Q4_K_M:latest"   # required
//	$env:PROBE_FORMAT = "json"                      # "json" (default) or "schema"
//	$env:PROBE_LEVEL  = "thorough"                  # "thorough" (default) or "faster"
//	go test -tags=live -count=1 -v -timeout 60m ./backend/ollama/ -run TestLiveProbe
//
// -count=1 is not optional. Go caches a test result by its inputs, and this
// test's inputs are environment variables the cache does not hash, so without it
// a second run replays the first run's numbers, which has already caused one
// wrong conclusion in this project.
//
// The reference documents are confidential. Print COUNTS and SHAPES only: this
// file must never grow a line that logs a returned string, and no measurement
// taken with it may be quoted anywhere as a list of names.
package ollama

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"doc-anonymiser/backend/engine"
)

// TestLiveProbe scans one real document with one real model and reports, per
// slice, what the request cost and what came back.
func TestLiveProbe(t *testing.T) {
	path := os.Getenv("PROBE_DOC")
	model := os.Getenv("PROBE_MODEL")
	if path == "" || model == "" {
		t.Skip("set PROBE_DOC to a document path and PROBE_MODEL to an installed model name; see this file's header")
	}

	client := New(DefaultBaseURL)
	client.Model = model
	if status := client.Probe(); !status.Available {
		t.Skipf("ollama is not reachable on %s: %s", client.BaseURL, status.Detail)
	}

	// The format and the level are the two axes worth varying, so they are the
	// two the environment sets. Both fall back to what the application ships.
	format := strings.ToLower(strings.TrimSpace(os.Getenv("PROBE_FORMAT")))
	client.StrictFormat = format == "schema"
	level := strings.ToLower(strings.TrimSpace(os.Getenv("PROBE_LEVEL")))
	if level == "" {
		level = engine.DetailThorough
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read PROBE_DOC %q: %v", path, err)
	}
	docs, err := engine.LoadAll(filepath.Base(path), raw)
	if err != nil {
		t.Fatalf("could not import PROBE_DOC %q: %v", path, err)
	}

	t.Logf("model=%s format=%s level=%s target=%dB ceiling=%dB documents=%d",
		model, client.discoveryFormatName(), level,
		engine.ScanTargetBytes(level), client.PromptBudgetBytes(), len(docs))

	// Totals across every document of the file (an XLSX workbook is several).
	var totalRequests, totalSilent, totalTruncated, totalKept int
	runStarted := time.Now()

	for _, doc := range docs {
		slices, err := engine.ScanChunks(doc, nil, level, client.PromptBudgetBytes())
		if err != nil {
			t.Fatalf("could not slice %q: %v", doc.Name, err)
		}
		// The hallucination filter reads the whole scanned text, exactly as
		// DiscoverSlices does, so a name read in slice 3 is not dropped for
		// being absent from slice 1.
		var texts []string
		for _, s := range slices {
			texts = append(texts, s.Text)
		}
		source := strings.Join(texts, "\n")

		t.Logf("%s: %d %s(s), %d bytes of markdown, %d request(s)",
			doc.Name, doc.PageCount(), doc.Unit, len(doc.Markdown), len(slices))

		for i, slice := range slices {
			req := client.buildChatRequest(model, []chatMessage{
				{Role: "system", Content: discoverSystemPrompt},
				{Role: "user", Content: slice.Text},
			}, client.discoveryFormat())

			started := time.Now()
			resp, err := client.postChat(context.Background(), client.chatClient, model, req)
			elapsed := time.Since(started)
			totalRequests++

			// A cut-off reply is measured, not fatal: it is one of the four
			// outcomes this probe exists to distinguish. Its salvaged content is
			// counted like any other reply's.
			content := resp.Message.Content
			truncated := resp.DoneReason == "length"
			var kept int
			switch {
			case truncated:
				totalTruncated++
				kept = len(client.filterSuggestions(salvageSuggestionJSON(content), source))
			case err != nil:
				t.Logf("  slice %d/%d units %d-%d %5dB %6.1fs FAILED: %v",
					i+1, len(slices), slice.FromUnit, slice.ToUnit, len(slice.Text), elapsed.Seconds(), err)
				continue
			default:
				parsed, perr := parseSuggestionJSON(content)
				if perr != nil {
					t.Logf("  slice %d/%d units %d-%d %5dB %6.1fs eval=%d UNPARSEABLE: %v",
						i+1, len(slices), slice.FromUnit, slice.ToUnit, len(slice.Text),
						elapsed.Seconds(), resp.EvalCount, perr)
					continue
				}
				kept = len(client.filterSuggestions(parsed, source))
			}
			if kept == 0 && !truncated {
				totalSilent++
			}
			totalKept += kept

			// done_reason, eval_count, seconds and the kept count are the four
			// numbers that separate "empty document" from "silent model" from
			// "reply cut off", which is the difference between a diagnosis and
			// an hour of confusion.
			t.Logf("  slice %d/%d units %d-%d %5dB %6.1fs done=%-6s eval=%4d kept=%d",
				i+1, len(slices), slice.FromUnit, slice.ToUnit, len(slice.Text),
				elapsed.Seconds(), reasonOrOK(resp.DoneReason), resp.EvalCount, kept)
		}
	}

	wall := time.Since(runStarted)
	perRequest := 0.0
	if totalRequests > 0 {
		perRequest = wall.Seconds() / float64(totalRequests)
	}
	t.Logf("TOTAL requests=%d values=%d silent=%d truncated=%d wall=%s (%.1fs each)",
		totalRequests, totalKept, totalSilent, totalTruncated,
		wall.Round(time.Second), perRequest)
}

// discoveryFormatName names the format the run used, for the log line. The
// format ITSELF is whatever discoveryFormat returns; this only labels it.
func (c *Client) discoveryFormatName() string {
	if c.StrictFormat {
		return "schema"
	}
	return "json"
}

// reasonOrOK keeps the per-slice line aligned when Ollama reports the ordinary
// "stop" reason, so the eye finds the "length" rows.
func reasonOrOK(reason string) string {
	if reason == "" {
		return "-"
	}
	return reason
}
