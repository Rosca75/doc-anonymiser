// main_test.go — the pure, fixture-free scanner tests.
//
// TIER: unit (docs/TESTING.md). These exercise the scanner's two pure helpers,
// clause splitting and specifier resolution, with table cases and no
// filesystem at all. The tests that lay out a real tree and run the whole
// scanner over it are in main_integration_test.go. White-box (package main)
// because splitImportClause and resolve are unexported and are exactly the
// subject here.
package main

import "testing"

// TestSplitImportClause covers the clause shapes directly, because the table
// in the integration tests can only reach them through a whole tree.
func TestSplitImportClause(t *testing.T) {
	tests := []struct {
		clause string
		want   []string
	}{
		{"{ alpha }", []string{"alpha"}},
		{"{ alpha, beta }", []string{"alpha", "beta"}},
		{"{ alpha as a, beta }", []string{"alpha", "beta"}},
		{"Def", []string{"default"}},
		{"Def, { alpha }", []string{"alpha", "default"}},
		{"* as ns", []string{"*"}},
		{"Def, * as ns", []string{"*"}},
		{"", []string{"*"}},
	}

	for _, tc := range tests {
		t.Run(tc.clause, func(t *testing.T) {
			got := splitImportClause(tc.clause)
			if len(got) != len(tc.want) {
				t.Fatalf("splitImportClause(%q) = %v, want %v", tc.clause, got, tc.want)
			}
			seen := map[string]bool{}
			for _, g := range got {
				seen[g] = true
			}
			for _, w := range tc.want {
				if !seen[w] {
					t.Errorf("splitImportClause(%q) = %v, missing %q", tc.clause, got, w)
				}
			}
		})
	}
}

// TestResolve covers specifier resolution, including the forms that must
// resolve to nothing.
func TestResolve(t *testing.T) {
	tests := []struct {
		importer string
		spec     string
		want     string
	}{
		{"frontend/main.js", "./state.js", "frontend/state.js"},
		{"frontend/main.js", "./views/home.js", "frontend/views/home.js"},
		{"frontend/views/home.js", "../state.js", "frontend/state.js"},
		{"frontend/views/home.js", "./identify.js", "frontend/views/identify.js"},
		// Not relative: a bare package name cannot name a file in this repo.
		{"frontend/main.js", "node:test", ""},
		{"frontend/main.js", "some-package", ""},
		// CLAUDE.md §4 forbids remote assets; nothing to resolve.
		{"frontend/main.js", "https://cdn.example/x.js", ""},
		// Extensionless: the browser requires the extension, so this is a typo
		// rather than a module we should silently guess at.
		{"frontend/main.js", "./state", ""},
	}

	for _, tc := range tests {
		t.Run(tc.importer+" "+tc.spec, func(t *testing.T) {
			if got := resolve(tc.importer, tc.spec); got != tc.want {
				t.Errorf("resolve(%q, %q) = %q, want %q", tc.importer, tc.spec, got, tc.want)
			}
		})
	}
}
