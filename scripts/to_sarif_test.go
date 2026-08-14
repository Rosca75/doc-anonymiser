// to_sarif_test.go — tests for scripts/to_sarif.py.
//
// The converter is Python (see the script's docstring for why), but its test
// lives in Go so that it runs under `go test ./...`, which is one of the two
// suites that gate this repository (CLAUDE.md §6). A third test runner would
// be a suite nobody remembers to run.
//
// The test skips itself when no Python interpreter is on PATH rather than
// failing: the converter is only needed when running the audit, and a
// contributor building the application should not be blocked by its absence.
// CI has Python, so the coverage is not optional where it counts.
//
// What is asserted is the set of things that fail SILENTLY on upload: GitHub
// accepts a malformed-enough SARIF file and then shows no alerts, so every
// property below is one that produced no error message when it was wrong.
package scripts

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// pythonExe finds an interpreter, preferring the names that exist on the
// owner's Windows laptop, where "python3" is not one of them.
func pythonExe(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python", "py"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	t.Skip("no Python interpreter on PATH; skipping the SARIF converter tests")
	return ""
}

// runConverter writes input as JSON, converts it, and returns the parsed SARIF.
func runConverter(t *testing.T, tool string, input any) map[string]any {
	t.Helper()
	py := pythonExe(t)

	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.json")
	outPath := filepath.Join(dir, "out.sarif")

	blob, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("could not encode the test input: %v", err)
	}
	if err := os.WriteFile(inPath, blob, 0o644); err != nil {
		t.Fatalf("could not write %s: %v", inPath, err)
	}

	cmd := exec.Command(py, "to_sarif.py",
		"--tool", tool, "--input", inPath, "--output", outPath)
	// Run from scripts/, which is this test's own directory.
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("to_sarif.py --tool %s failed: %v\n%s", tool, err, out)
	}

	return readSARIF(t, outPath)
}

func readSARIF(t *testing.T, path string) map[string]any {
	t.Helper()
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("converter wrote no output at %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(blob, &doc); err != nil {
		t.Fatalf("converter output is not valid JSON: %v\n%s", err, blob)
	}
	return doc
}

// firstRun returns runs[0], failing the test if the document has no run at all.
// An upload with zero runs is accepted by GitHub and closes every alert in the
// category, so "no runs" must never be mistaken for "no findings".
func firstRun(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	runs, ok := doc["runs"].([]any)
	if !ok || len(runs) == 0 {
		t.Fatalf("SARIF document has no runs: %v", doc)
	}
	run, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatalf("runs[0] is not an object: %v", runs[0])
	}
	return run
}

func results(t *testing.T, run map[string]any) []any {
	t.Helper()
	res, ok := run["results"].([]any)
	if !ok {
		t.Fatalf("run has no results array: %v", run)
	}
	return res
}

func rules(t *testing.T, run map[string]any) []any {
	t.Helper()
	tool := run["tool"].(map[string]any)
	driver := tool["driver"].(map[string]any)
	r, ok := driver["rules"].([]any)
	if !ok {
		t.Fatalf("driver has no rules array: %v", driver)
	}
	return r
}

// sampleDeadcode is one package with one dead function.
func sampleDeadcode() any {
	return []any{
		map[string]any{
			"Name": "backend",
			"Path": "doc-anonymiser/backend",
			"Funcs": []any{
				map[string]any{
					"Name":      "App.documentInfos",
					"Position":  map[string]any{"File": "backend/app.go", "Line": 408, "Col": 15},
					"Generated": false,
				},
			},
		},
	}
}

// sampleDeadexports is one finding of each frontend rule.
func sampleDeadexports() any {
	return map[string]any{
		"tool": "deadexports",
		"findings": []any{
			map[string]any{
				"rule": "unused-export", "file": "frontend/nav.js",
				"line": 69, "symbol": "advance", "message": "advance is exported but unused.",
			},
			map[string]any{
				"rule": "test-only-export", "file": "frontend/ui.js",
				"line": 12, "symbol": "countBadge", "message": "countBadge is test-only.",
			},
			map[string]any{
				"rule": "unused-module", "file": "frontend/orphan.js",
				"line": 1, "symbol": "", "message": "Nothing imports orphan.js.",
			},
		},
		"stats": map[string]any{"filesScanned": 3, "exports": 3, "imports": 0},
	}
}

func TestConverterProducesValidSARIF(t *testing.T) {
	tests := []struct {
		tool        string
		input       any
		wantResults int
		wantRules   int
	}{
		{"deadcode", sampleDeadcode(), 1, 1},
		{"deadexports", sampleDeadexports(), 3, 3},
	}

	for _, tc := range tests {
		t.Run(tc.tool, func(t *testing.T) {
			doc := runConverter(t, tc.tool, tc.input)

			if doc["version"] != "2.1.0" {
				t.Errorf("version = %v, want 2.1.0 (GitHub rejects other versions)", doc["version"])
			}
			if _, ok := doc["$schema"].(string); !ok {
				t.Error("no $schema; GitHub rejects an upload whose schema it cannot identify")
			}

			run := firstRun(t, doc)
			if got := len(results(t, run)); got != tc.wantResults {
				t.Errorf("got %d results, want %d", got, tc.wantResults)
			}
			if got := len(rules(t, run)); got != tc.wantRules {
				t.Errorf("got %d rules, want %d", got, tc.wantRules)
			}
		})
	}
}

// TestSeverityStaysBelowFour is the rule that keeps maintenance findings out of
// the security bucket. GitHub buckets >= 4.0 as medium and above; anything in
// this layer must render as "low" (see scripts/to_sarif.py, SEVERITY).
func TestSeverityStaysBelowFour(t *testing.T) {
	for _, tool := range []struct {
		name  string
		input any
	}{
		{"deadcode", sampleDeadcode()},
		{"deadexports", sampleDeadexports()},
	} {
		t.Run(tool.name, func(t *testing.T) {
			run := firstRun(t, runConverter(t, tool.name, tool.input))
			for _, raw := range rules(t, run) {
				rule := raw.(map[string]any)
				props, ok := rule["properties"].(map[string]any)
				if !ok {
					t.Fatalf("rule %v has no properties", rule["id"])
				}
				sev, ok := props["security-severity"].(string)
				if !ok {
					t.Fatalf("rule %v: security-severity must be a JSON STRING; "+
						"a numeric literal is dropped silently by GitHub, got %T",
						rule["id"], props["security-severity"])
				}
				f, err := strconv.ParseFloat(sev, 64)
				if err != nil {
					t.Fatalf("rule %v: security-severity %q is not numeric: %v",
						rule["id"], sev, err)
				}
				if f < 1.0 || f >= 4.0 {
					t.Errorf("rule %v: security-severity %v is outside 1.0-3.9; "+
						"dead code and lint must never present as security findings",
						rule["id"], f)
				}
			}
		})
	}
}

// TestFingerprintIgnoresLineNumber is the property that preserves triage state.
// The same finding at a different line must keep its fingerprint, or every
// unrelated edit above it closes the alert and reopens a fresh one, discarding
// any dismissal the owner recorded.
func TestFingerprintIgnoresLineNumber(t *testing.T) {
	at := func(line int) string {
		input := map[string]any{
			"tool": "deadexports",
			"findings": []any{map[string]any{
				"rule": "unused-export", "file": "frontend/nav.js",
				"line": line, "symbol": "advance", "message": "unused",
			}},
		}
		run := firstRun(t, runConverter(t, "deadexports", input))
		res := results(t, run)[0].(map[string]any)
		fp := res["partialFingerprints"].(map[string]any)
		return fp["primaryLocationLineHash"].(string)
	}

	if a, b := at(69), at(412); a != b {
		t.Errorf("fingerprint changed with the line number (%q vs %q); "+
			"moving a function would discard its triage state", a, b)
	}
}

// TestFingerprintDistinguishesSymbols is the other half: two different findings
// must NOT collapse into one alert.
func TestFingerprintDistinguishesSymbols(t *testing.T) {
	input := map[string]any{
		"tool": "deadexports",
		"findings": []any{
			map[string]any{
				"rule": "unused-export", "file": "frontend/nav.js",
				"line": 69, "symbol": "advance", "message": "unused",
			},
			map[string]any{
				"rule": "unused-export", "file": "frontend/nav.js",
				"line": 79, "symbol": "goBack", "message": "unused",
			},
		},
	}
	run := firstRun(t, runConverter(t, "deadexports", input))
	res := results(t, run)
	seen := map[string]bool{}
	for _, raw := range res {
		fp := raw.(map[string]any)["partialFingerprints"].(map[string]any)
		seen[fp["primaryLocationLineHash"].(string)] = true
	}
	if len(seen) != len(res) {
		t.Errorf("%d findings collapsed into %d fingerprints; distinct symbols "+
			"must produce distinct alerts", len(res), len(seen))
	}
}

// TestLocationsAreRepoRelative guards the failure that produces alerts nobody
// can open: an absolute or backslashed path uploads cleanly and then matches
// no file in the repository.
func TestLocationsAreRepoRelative(t *testing.T) {
	run := firstRun(t, runConverter(t, "deadexports", sampleDeadexports()))
	for _, raw := range results(t, run) {
		res := raw.(map[string]any)
		loc := res["locations"].([]any)[0].(map[string]any)
		phys := loc["physicalLocation"].(map[string]any)
		uri := phys["artifactLocation"].(map[string]any)["uri"].(string)

		if filepath.IsAbs(uri) || len(uri) > 1 && uri[1] == ':' {
			t.Errorf("uri %q is absolute; GitHub cannot attach it to a file", uri)
		}
		for _, c := range uri {
			if c == '\\' {
				t.Errorf("uri %q contains a backslash; SARIF requires forward slashes", uri)
				break
			}
		}

		region := phys["region"].(map[string]any)
		if line, _ := region["startLine"].(float64); line < 1 {
			t.Errorf("uri %q has startLine %v; GitHub rejects a run containing "+
				"a region with startLine below 1", uri, line)
		}
	}
}

// TestEmptyAndMissingInputStillProduceARun covers the case that must not crash
// and must not be skipped. An empty run is what tells GitHub to CLOSE the
// alerts from the previous upload; producing no file at all would leave fixed
// findings showing as open for ever.
func TestEmptyAndMissingInputStillProduceARun(t *testing.T) {
	py := pythonExe(t)

	tests := []struct {
		name  string
		setup func(dir string) string // returns the input path
	}{
		{
			name: "empty file",
			setup: func(dir string) string {
				p := filepath.Join(dir, "empty.json")
				if err := os.WriteFile(p, nil, 0o644); err != nil {
					t.Fatalf("could not write %s: %v", p, err)
				}
				return p
			},
		},
		{
			name:  "missing file",
			setup: func(dir string) string { return filepath.Join(dir, "absent.json") },
		},
		{
			name: "empty JSON array",
			setup: func(dir string) string {
				p := filepath.Join(dir, "arr.json")
				if err := os.WriteFile(p, []byte("[]"), 0o644); err != nil {
					t.Fatalf("could not write %s: %v", p, err)
				}
				return p
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			inPath := tc.setup(dir)
			outPath := filepath.Join(dir, "out.sarif")

			cmd := exec.Command(py, "to_sarif.py",
				"--tool", "deadcode", "--input", inPath, "--output", outPath)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("converter exited non-zero on %s: %v\n%s\n"+
					"An absent or empty tool output is a normal outcome and must "+
					"not stop the audit run.", tc.name, err, out)
			}

			run := firstRun(t, readSARIF(t, outPath))
			if got := len(results(t, run)); got != 0 {
				t.Errorf("got %d results from %s, want 0", got, tc.name)
			}
		})
	}
}

// TestMalformedInputIsAnError is the deliberate exception to the tolerance
// above: a non-empty file that is not JSON means the tool crashed and its
// error text landed in the output. That must stop, not convert to zero
// findings, which would read as "clean".
func TestMalformedInputIsAnError(t *testing.T) {
	py := pythonExe(t)

	dir := t.TempDir()
	inPath := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(inPath, []byte("panic: something went wrong\n"), 0o644); err != nil {
		t.Fatalf("could not write %s: %v", inPath, err)
	}

	cmd := exec.Command(py, "to_sarif.py",
		"--tool", "deadcode", "--input", inPath, "--output", filepath.Join(dir, "out.sarif"))
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("converter accepted non-JSON input and exited 0:\n%s\n"+
			"A crashed tool would then be reported as a clean scan.", out)
	}
}

// TestDeterministicOutput keeps the SARIF diffable: the same findings must
// serialise identically every time, or every run looks like a change.
func TestDeterministicOutput(t *testing.T) {
	py := pythonExe(t)

	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.json")
	blob, err := json.Marshal(sampleDeadexports())
	if err != nil {
		t.Fatalf("could not encode the test input: %v", err)
	}
	if err := os.WriteFile(inPath, blob, 0o644); err != nil {
		t.Fatalf("could not write %s: %v", inPath, err)
	}

	var first []byte
	for i := 0; i < 3; i++ {
		outPath := filepath.Join(dir, "out.sarif")
		cmd := exec.Command(py, "to_sarif.py",
			"--tool", "deadexports", "--input", inPath, "--output", outPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("run %d failed: %v\n%s", i, err, out)
		}
		got, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatalf("run %d wrote no output: %v", i, err)
		}
		if i == 0 {
			first = got
			continue
		}
		if string(got) != string(first) {
			t.Fatalf("run %d produced different bytes from run 0; "+
				"the SARIF must be reproducible for its diff to mean anything", i)
		}
	}
}

// TestUnknownRuleIsSkippedNotFatal covers the drift case: the Go scanner grows
// a rule the converter has not been taught. The known findings must still
// convert, so that one new rule does not blank the whole category.
func TestUnknownRuleIsSkippedNotFatal(t *testing.T) {
	input := map[string]any{
		"tool": "deadexports",
		"findings": []any{
			map[string]any{
				"rule": "unused-export", "file": "frontend/a.js",
				"line": 1, "symbol": "x", "message": "unused",
			},
			map[string]any{
				"rule": "rule-from-the-future", "file": "frontend/b.js",
				"line": 1, "symbol": "y", "message": "?",
			},
		},
	}
	run := firstRun(t, runConverter(t, "deadexports", input))
	if got := len(results(t, run)); got != 1 {
		t.Errorf("got %d results, want 1: the known finding must survive an "+
			"unknown one", got)
	}
}
