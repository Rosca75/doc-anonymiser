//go:build integration

// main_integration_test.go — the scanner tests that need a real filesystem.
//
// TIER: integration (docs/TESTING.md). These lay out a fake frontend tree in a
// temp directory (writeTree, t.TempDir, t.Chdir) and run the whole scanner over
// it. That is real file I/O, which the integration tier owns. The pure
// clause-splitting and specifier-resolution tests, which need no tree, are in
// main_test.go.
//
// The scanner is a line-oriented reader, not a JavaScript parser, so the value
// of these tests is in the shapes it must NOT get wrong: multi-line import
// lists, `as` aliases, namespace imports that suppress reporting, and the HTML
// entry point. A false positive here becomes a code scanning alert against
// working code, so several cases exist purely to assert that nothing is
// reported.
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTree lays out a fake frontend in a temp dir and chdirs into its parent,
// because scan() reports repo-relative paths and resolves specifiers from them.
func writeTree(t *testing.T, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("could not create %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("could not write %s: %v", full, err)
		}
	}
	t.Chdir(dir)
}

// findingSet reduces a report to rule/file/symbol triples for comparison.
func findingSet(t *testing.T, root string, excludes []string) map[string]bool {
	t.Helper()
	rep, err := scan(root, excludes)
	if err != nil {
		t.Fatalf("scan(%q) failed: %v", root, err)
	}
	out := map[string]bool{}
	for _, f := range rep.Findings {
		out[f.Rule+" "+f.File+" "+f.Symbol] = true
	}
	return out
}

func TestScanRules(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		excludes []string
		want     []string // findings that MUST be present
		notWant  []string // findings that must NOT be present
	}{
		{
			name: "an export nobody imports is reported",
			files: map[string]string{
				"frontend/index.html": `<script type="module">import { boot } from "./main.js";</script>`,
				"frontend/main.js":    "export function boot() {}\nexport function orphan() {}\n",
			},
			want:    []string{"unused-export frontend/main.js orphan"},
			notWant: []string{"unused-export frontend/main.js boot"},
		},
		{
			name: "the HTML entry point keeps its import alive",
			files: map[string]string{
				// If HTML were not scanned, boot would be the only export of an
				// unimported module and main.js would come back unused-module.
				"frontend/index.html": "<script type=\"module\">\n  import { boot } from \"./main.js\";\n  boot(document.body);\n</script>",
				"frontend/main.js":    "export function boot() {}\n",
			},
			notWant: []string{
				"unused-export frontend/main.js boot",
				"unused-module frontend/main.js ",
			},
		},
		{
			name: "a multi-line import list is read whole",
			files: map[string]string{
				"frontend/index.html": `<script type="module">import { boot } from "./main.js";</script>`,
				"frontend/main.js":    "import {\n  alpha,\n  beta,\n} from \"./lib.js\";\nexport function boot() { alpha(); beta(); }\n",
				"frontend/lib.js":     "export function alpha() {}\nexport function beta() {}\n",
			},
			notWant: []string{
				"unused-export frontend/lib.js alpha",
				"unused-export frontend/lib.js beta",
			},
		},
		{
			name: "an as-alias matches the exported name, not the local one",
			files: map[string]string{
				"frontend/index.html": `<script type="module">import { boot } from "./main.js";</script>`,
				"frontend/main.js":    "import { alpha as renamed } from \"./lib.js\";\nexport function boot() { renamed(); }\n",
				"frontend/lib.js":     "export function alpha() {}\n",
			},
			notWant: []string{"unused-export frontend/lib.js alpha"},
		},
		{
			name: "a namespace import suppresses every finding in the target",
			files: map[string]string{
				"frontend/index.html": `<script type="module">import { boot } from "./main.js";</script>`,
				"frontend/main.js":    "import * as lib from \"./lib.js\";\nexport function boot() { lib.alpha(); }\n",
				"frontend/lib.js":     "export function alpha() {}\nexport function beta() {}\n",
			},
			// beta is genuinely unreferenced, but proving it would need scope
			// analysis of `lib.`; reporting it would be a guess.
			notWant: []string{
				"unused-export frontend/lib.js alpha",
				"unused-export frontend/lib.js beta",
			},
		},
		{
			name: "a dynamic import suppresses findings in the target",
			files: map[string]string{
				"frontend/index.html": `<script type="module">import { boot } from "./main.js";</script>`,
				"frontend/main.js":    "export async function boot() {\n  const lib = await import(\"./lib.js\");\n  return lib;\n}\n",
				"frontend/lib.js":     "export function alpha() {}\n",
			},
			notWant: []string{
				"unused-export frontend/lib.js alpha",
				"unused-module frontend/lib.js ",
			},
		},
		{
			name: "an export only tests import is test-only, not unused",
			files: map[string]string{
				"frontend/index.html":  `<script type="module">import { boot } from "./main.js";</script>`,
				"frontend/main.js":     "export function boot() {}\n",
				"frontend/lib.js":      "export function helper() {}\n",
				"frontend/lib.test.js": "import { helper } from \"./lib.js\";\nhelper();\n",
			},
			want: []string{"test-only-export frontend/lib.js helper"},
			notWant: []string{
				"unused-export frontend/lib.js helper",
				"unused-module frontend/lib.js ",
			},
		},
		{
			name: "a test file's own exports are never reported",
			files: map[string]string{
				"frontend/index.html":  `<script type="module">import { boot } from "./main.js";</script>`,
				"frontend/main.js":     "export function boot() {}\n",
				"frontend/lib.test.js": "export function fixture() {}\n",
			},
			notWant: []string{
				"unused-export frontend/lib.test.js fixture",
				"unused-module frontend/lib.test.js ",
			},
		},
		{
			name: "a module nobody imports is one finding, not one per export",
			files: map[string]string{
				"frontend/index.html": `<script type="module">import { boot } from "./main.js";</script>`,
				"frontend/main.js":    "export function boot() {}\n",
				"frontend/orphan.js":  "export function a() {}\nexport function b() {}\nexport function c() {}\n",
			},
			want: []string{"unused-module frontend/orphan.js "},
			notWant: []string{
				"unused-export frontend/orphan.js a",
				"unused-export frontend/orphan.js b",
				"unused-export frontend/orphan.js c",
			},
		},
		{
			name: "an excluded file produces no findings",
			files: map[string]string{
				"frontend/index.html":       `<script type="module">import { boot } from "./main.js";</script>`,
				"frontend/main.js":          "export function boot() {}\n",
				"frontend/testhtml.js":      "export function one() {}\n",
				"frontend/testhtml.test.js": "import { one } from \"./testhtml.js\";\none();\n",
			},
			excludes: []string{"frontend/testhtml.js"},
			notWant:  []string{"test-only-export frontend/testhtml.js one"},
		},
		{
			name: "an import in a comment is not an import",
			files: map[string]string{
				"frontend/index.html": `<script type="module">import { boot } from "./main.js";</script>`,
				"frontend/main.js":    "export function boot() {}\n",
				// Both comment forms mention lib.js. If either were read as a
				// real edge, alpha would look used and the finding would vanish.
				"frontend/other.js": "// import { alpha } from \"./lib.js\";\n/*\nimport { alpha } from \"./lib.js\";\n*/\nimport { boot } from \"./main.js\";\nboot();\n",
				"frontend/lib.js":   "export function alpha() {}\n",
			},
			want: []string{"unused-module frontend/lib.js "},
		},
		{
			name: "subdirectories resolve through ../",
			files: map[string]string{
				"frontend/index.html":     `<script type="module">import { boot } from "./main.js";</script>`,
				"frontend/main.js":        "import { render } from \"./views/home.js\";\nexport function boot() { render(); }\n",
				"frontend/views/home.js":  "import { helper } from \"../lib.js\";\nexport function render() { helper(); }\n",
				"frontend/lib.js":         "export function helper() {}\nexport function stray() {}\n",
				"frontend/views/dead.js":  "export function nobody() {}\n",
				"frontend/views/note.txt": "not javascript",
			},
			want: []string{
				"unused-export frontend/lib.js stray",
				"unused-module frontend/views/dead.js ",
			},
			notWant: []string{
				"unused-export frontend/lib.js helper",
				"unused-export frontend/views/home.js render",
			},
		},
		{
			name: "const and async function exports are both seen",
			files: map[string]string{
				"frontend/index.html": `<script type="module">import { boot } from "./main.js";</script>`,
				"frontend/main.js":    "export function boot() {}\nexport const TABLE = [1, 2];\nexport async function later() {}\n",
			},
			want: []string{
				"unused-export frontend/main.js TABLE",
				"unused-export frontend/main.js later",
			},
		},
		{
			name: "a node: builtin specifier is ignored, not resolved",
			files: map[string]string{
				"frontend/index.html": `<script type="module">import { boot } from "./main.js";</script>`,
				// If "node:test" were resolved, scan would look for a module
				// that cannot exist and the walk would have to cope with it.
				"frontend/main.js": "import test from \"node:test\";\nexport function boot() { test; }\n",
			},
			notWant: []string{"unused-export frontend/main.js boot"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			writeTree(t, tc.files)
			got := findingSet(t, "frontend", tc.excludes)

			for _, want := range tc.want {
				if !got[want] {
					t.Errorf("expected finding %q, got %v", want, keys(got))
				}
			}
			for _, bad := range tc.notWant {
				if got[bad] {
					t.Errorf("unexpected finding %q (false positive), got %v", bad, keys(got))
				}
			}
		})
	}
}

// TestScanIsDeterministic guards the property the SARIF layer depends on:
// the same tree must produce the same findings in the same order, or
// partialFingerprints churn and GitHub reopens alerts that were dismissed.
func TestScanIsDeterministic(t *testing.T) {
	writeTree(t, map[string]string{
		"frontend/index.html": `<script type="module">import { boot } from "./main.js";</script>`,
		"frontend/main.js":    "export function boot() {}\n",
		"frontend/a.js":       "export function one() {}\nexport function two() {}\n",
		"frontend/b.js":       "export function three() {}\n",
		"frontend/c.js":       "export function four() {}\n",
	})

	first, err := scan("frontend", nil)
	if err != nil {
		t.Fatalf("first scan failed: %v", err)
	}
	for i := 0; i < 5; i++ {
		next, err := scan("frontend", nil)
		if err != nil {
			t.Fatalf("scan %d failed: %v", i, err)
		}
		if len(next.Findings) != len(first.Findings) {
			t.Fatalf("run %d returned %d findings, first run returned %d",
				i, len(next.Findings), len(first.Findings))
		}
		for j := range next.Findings {
			if next.Findings[j] != first.Findings[j] {
				t.Fatalf("run %d finding %d = %+v, first run had %+v",
					i, j, next.Findings[j], first.Findings[j])
			}
		}
	}
}

// TestScanMissingRootIsAnError checks the failure the caller is most likely to
// hit: running the tool from the wrong directory. It must fail loudly rather
// than report an empty, reassuring result.
func TestScanMissingRootIsAnError(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := scan("frontend", nil); err == nil {
		t.Fatal("scanning a directory that does not exist returned no error; " +
			"an empty report would read as 'no dead code found'")
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
