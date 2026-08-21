// copy_guard_test.go — the Go-side copy-style guards.
//
// Two rules, over the same set of string literals: no em dashes (U+2014), and
// no retired detection-route name. Go user-visible strings are error messages,
// report text and prompt/status strings, all of which live in string LITERALS in
// backend/app*.go, backend/engine/ and backend/ollama/. These tests parse those
// files and fail listing file:line for every offending literal, so the rules are
// enforced by CI forever, not by reviewer memory.
//
// Comments are deliberately NOT scanned: they are developer-facing, em dashes are
// fine prose there, and a comment naming an engine identifier and the label it
// renders as is exactly what should be written. Test files are skipped too (their
// literals assert on content, they are never shown to users).
//
// The matching frontend guard is frontend/copy.test.js.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// guardedDirs are the source trees whose string literals reach users.
// Paths are relative to the repo root (this test's working directory). The
// backend/ split moved the app layer and the logic packages under backend/,
// so the guard now walks there; "." still covers the root main package.
var guardedDirs = []string{
	".", "backend", "backend/engine", "backend/engine/convert",
	"backend/engine/exportfmt", "backend/engine/imaging", "backend/ollama",
}

func TestNoEmDashInUserFacingStrings(t *testing.T) {
	var hits []string
	fset := token.NewFileSet()

	for _, dir := range guardedDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // a guarded dir may not exist yet (exportfmt before Phase 11)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("could not parse %s: %v", path, err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				if strings.Contains(lit.Value, "—") {
					pos := fset.Position(lit.Pos())
					hits = append(hits, fmt.Sprintf("%s:%d: %s", pos.Filename, pos.Line, lit.Value))
				}
				return true
			})
		}
	}

	if len(hits) > 0 {
		t.Errorf("em dashes found in Go string literals; replace them with commas, periods or parentheses:\n%s",
			strings.Join(hits, "\n"))
	}
}

// retiredRouteNames are labels the interface no longer uses. "Smart detection"
// was one name over three unrelated mechanisms (built-in pattern matching, which
// acts without review, plus two discovery methods, which do not), and "Local AI"
// said nothing about what runs. Go's user-facing strings name the mechanism now:
// Built-in patterns, Heuristic discovery, Local LLM discovery.
//
// The engine IDENTIFIERS keep their spelling, because a label is a display string
// and an identifier is a contract. That is why this guard reads string LITERALS
// and skips comments: a comment naming PhaseRules or local_llm and saying what it
// renders as is exactly what should be written.
var retiredRouteNames = []string{"Smart detection", "Smart Detection", "Local AI"}

func TestNoRetiredRouteNameInUserFacingStrings(t *testing.T) {
	var hits []string
	fset := token.NewFileSet()

	for _, dir := range guardedDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("could not parse %s: %v", path, err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				for _, retired := range retiredRouteNames {
					if strings.Contains(lit.Value, retired) {
						pos := fset.Position(lit.Pos())
						hits = append(hits, fmt.Sprintf("%s:%d: %s in %s", pos.Filename, pos.Line, retired, lit.Value))
					}
				}
				return true
			})
		}
	}

	if len(hits) > 0 {
		t.Errorf("retired route names found in Go string literals; name the mechanism instead "+
			"(Built-in patterns, Heuristic discovery, Local LLM discovery):\n%s",
			strings.Join(hits, "\n"))
	}
}
