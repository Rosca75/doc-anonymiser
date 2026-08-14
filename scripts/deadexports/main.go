// Command deadexports reports ES-module exports that nothing imports.
//
// # WHY THIS EXISTS AND NOT knip
//
// The usual tool for this job is knip, which needs npm. This project has no
// npm: frontend/package.json declares zero dependencies and exists only to set
// "type": "module" so `node --test` can run (CLAUDE.md §6, "no npm, no
// bundler"). Installing one for the audit layer would put a node_modules tree
// and a lockfile into a codebase whose charter forbids both, and it would fail
// on the TLS-inspecting proxy the owner's laptop sits behind. So the audit
// layer brings its own detector: Go, standard library only, no install step.
//
// # WHAT IT DOES
//
// It builds the module graph of frontend/ by reading `import` and `export`
// statements, then reports three things:
//
//	unused-export     an export no other file imports, from anywhere
//	test-only-export  an export only *.test.js files import, i.e. code kept
//	                  alive by its own test and by nothing else
//	unused-module     a file nothing imports at all
//
// HTML files are parsed with the same import scanner, so frontend/index.html's
// inline `import { boot } from "./main.js"` marks boot as used. That is why
// there is no hardcoded list of entry points to keep in sync: the entry point
// is whatever the HTML actually loads.
//
// # WHAT IT DELIBERATELY DOES NOT DO
//
// This is a line-oriented scanner, not a JavaScript parser. It reads import
// and export statements, which in this codebase are always at the start of a
// line, and it understands nothing else about the language. Three consequences,
// all resolved in the direction of NOT reporting:
//
//   - A module imported as `import * as ns from "./m.js"` has every one of its
//     exports marked used. Working out which members of ns are read would need
//     real scope analysis, and guessing would produce false positives.
//   - A dynamic `await import("./m.js")` marks the whole module used, same
//     reasoning. The test suites use this form.
//   - Anything re-exported (`export { x } from "./m.js"`) counts as both an
//     import of m and an export of the re-exporting file.
//
// A false positive here costs more than a false negative: the finding lands in
// GitHub code scanning, and a reviewer who dismisses two bogus alerts stops
// reading the rest.
//
// # OUTPUT
//
// JSON on stdout, converted to SARIF by scripts/to_sarif.py. The schema is
// defined by the report/finding types below and is private to this pair of
// programs; nothing else reads it.
//
// USAGE
//
//	go run ./scripts/deadexports -root frontend -o .audit/deadexports.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ── Report schema (the contract with scripts/to_sarif.py) ────────────────────

// report is the whole output document.
type report struct {
	Tool     string    `json:"tool"`
	Findings []finding `json:"findings"`
	Stats    stats     `json:"stats"`
}

// finding is one reported symbol or module.
//
// Symbol is empty for module-level findings (unused-module). to_sarif.py builds
// the partial fingerprint from Rule + File + Symbol and never from Line, so
// that moving a function down a file does not create a new alert.
type finding struct {
	Rule    string `json:"rule"`
	File    string `json:"file"`   // slash-separated, relative to the repo root
	Line    int    `json:"line"`   // 1-based; for humans only, never fingerprinted
	Symbol  string `json:"symbol"` // exported identifier, or "" for a whole module
	Message string `json:"message"`
}

// stats let the Taskfile print a one-line summary without re-reading findings,
// and make "the scan ran but found nothing" distinguishable from "the scan
// found no files", which look identical in an empty findings list.
type stats struct {
	FilesScanned int `json:"filesScanned"`
	Exports      int `json:"exports"`
	Imports      int `json:"imports"`
}

// ── Module graph ─────────────────────────────────────────────────────────────

// exportedSymbol is one export site.
type exportedSymbol struct {
	name string
	line int
}

// module is one .js file in the graph.
type module struct {
	rel     string // repo-relative, slash-separated
	exports []exportedSymbol

	// importedBy records who imports which name. The key is the exported
	// identifier; the value is the set of repo-relative importer paths. A "*"
	// key means the whole module was pulled in wholesale (namespace import,
	// dynamic import, or side-effect import), which marks every export used.
	importedBy map[string]map[string]bool

	// anyImporter is every file that imports this module in any way, used for
	// the unused-module rule. Kept separate from importedBy because a
	// side-effect import has no names at all.
	anyImporter map[string]bool
}

func newModule(rel string) *module {
	return &module{
		rel:         rel,
		importedBy:  map[string]map[string]bool{},
		anyImporter: map[string]bool{},
	}
}

// ── Statement patterns ───────────────────────────────────────────────────────
//
// Compiled once at init (CLAUDE.md §6). Each is documented with what it matches
// and what it deliberately does not.

var (
	// `export function foo(`, `export async function foo(`, `export class Foo`,
	// `export const foo =`, `export let foo`, `export var foo`.
	// Does NOT match `exports.foo = ...` (CommonJS, absent from this codebase)
	// or an indented export inside a block (not legal ES anyway).
	reExportDecl = regexp.MustCompile(
		`^export\s+(?:async\s+)?(?:function\s*\*?|class|const|let|var)\s+([A-Za-z_$][\w$]*)`)

	// `export default ...`. Recorded under the name "default", which is what an
	// importing file's default binding resolves to.
	reExportDefault = regexp.MustCompile(`^export\s+default\b`)

	// `export { a, b as c }` and `export { a } from "./m.js"`. The brace body is
	// captured whole and split by splitSpecifiers; the optional `from` clause is
	// captured so a re-export also counts as an import of the source module.
	// Multi-line brace bodies are joined into one logical line by statements()
	// before this runs.
	reExportBrace = regexp.MustCompile(`^export\s*\{([^}]*)\}\s*(?:from\s*["']([^"']+)["'])?`)

	// `export * from "./m.js"` and `export * as ns from "./m.js"`. Treated as a
	// wholesale import of the source module: which names it forwards depends on
	// the source, and following that chain is more machinery than this codebase
	// (which has no star re-exports at all) justifies.
	reExportStar = regexp.MustCompile(`^export\s*\*\s*(?:as\s+[A-Za-z_$][\w$]*\s+)?from\s*["']([^"']+)["']`)

	// `import ... from "./m.js"` in all its shapes. Group 1 is everything
	// between `import` and `from`; group 2 is the specifier. splitImportClause
	// takes group 1 apart.
	reImportFrom = regexp.MustCompile(`^import\s+([^"']*?)\s*from\s*["']([^"']+)["']`)

	// `import "./m.js"` — side effect only, no bindings.
	reImportBare = regexp.MustCompile(`^import\s*["']([^"']+)["']`)

	// A dynamic `import("./m.js")` anywhere on a line, including
	// `await import(...)`, which the frontend test suites use to re-import a
	// module with a fresh state. Marks the target wholesale.
	reImportDynamic = regexp.MustCompile(`\bimport\s*\(\s*["']([^"']+)["']\s*\)`)

	// The inline `import { boot } from "./main.js"` inside index.html's
	// <script type="module">. Same shapes as reImportFrom but not anchored to
	// the start of a line, because HTML indents the script body.
	reImportInHTML = regexp.MustCompile(`import\s+([^"']*?)\s*from\s*["']([^"']+)["']`)

	// `a as b` inside a brace list. Only the LEFT name matters: that is the name
	// the exporting module published.
	reAsAlias = regexp.MustCompile(`^([A-Za-z_$][\w$]*)\s+as\s+`)
)

func main() {
	var (
		root    = flag.String("root", "frontend", "directory to scan, relative to the repo root")
		outPath = flag.String("o", "", "write JSON here instead of stdout")
		quiet   = flag.Bool("quiet", false, "suppress the human-readable summary on stderr")
		exclude = flag.String("exclude", defaultExcludes,
			"comma-separated repo-relative paths whose exports are never reported")
	)
	flag.Parse()

	rep, err := scan(*root, strings.Split(*exclude, ","))
	if err != nil {
		// Actionable error (CLAUDE.md §2): name the directory, not just the
		// syscall, because "root" is a flag the caller chose.
		fmt.Fprintf(os.Stderr, "deadexports: could not scan %q: %v\n"+
			"Run this from the repository root, or pass -root with a directory that exists.\n",
			*root, err)
		os.Exit(2)
	}

	blob, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "deadexports: could not encode the report: %v\n", err)
		os.Exit(2)
	}
	blob = append(blob, '\n')

	if *outPath != "" {
		if err := os.WriteFile(*outPath, blob, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "deadexports: could not write %q: %v\n"+
				"Check the directory exists and is writable.\n", *outPath, err)
			os.Exit(2)
		}
	} else if _, err := os.Stdout.Write(blob); err != nil {
		fmt.Fprintf(os.Stderr, "deadexports: could not write to stdout: %v\n", err)
		os.Exit(2)
	}

	if !*quiet {
		fmt.Fprintf(os.Stderr, "deadexports: %d files, %d exports, %d imports, %d findings\n",
			rep.Stats.FilesScanned, rep.Stats.Exports, rep.Stats.Imports, len(rep.Findings))
	}

	// Exit 0 even with findings. This is a reporter: the SARIF upload decides
	// what blocks, and a non-zero exit here would make `task audit` stop before
	// the remaining tools run.
}

// defaultExcludes lists files whose exports are test-only BY DESIGN, so that
// reporting them would be a permanent false positive rather than a finding.
//
// This is the equivalent of telling knip where the entry points are: a list of
// places the graph is expected to look dead. Keep it short and justify every
// entry, because each one is a hole in the scan.
//
//   - frontend/testhtml.js is a dev-time HTML query helper written FOR the
//     render tests (see the repository layout in CLAUDE.md §3). Every export
//     being imported only by *.test.js is the file working as intended.
const defaultExcludes = "frontend/testhtml.js"

// scan walks root, builds the module graph and applies the three rules.
// Files matching excludes contribute to the graph as importers but never
// produce findings of their own.
func scan(root string, excludes []string) (report, error) {
	rep := report{Tool: "deadexports", Findings: []finding{}}

	mods := map[string]*module{} // repo-relative path → module

	// Pass A: collect every .js module and its exports. Must complete before
	// pass B, because an import can name a module the walk has not reached yet.
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(p)
		if d.IsDir() {
			// Wails regenerates JS bindings into frontend/wailsjs on every
			// build. They are .gitignored and frontend/api.js calls
			// window.go.backend.App directly, so nothing there is ever
			// imported; scanning it would report the whole directory dead.
			if d.Name() == "wailsjs" || d.Name() == "node_modules" || d.Name() == "dist" {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(rel, ".js") {
			mods[rel] = newModule(rel)
		}
		return nil
	})
	if err != nil {
		return rep, err
	}

	for rel, m := range mods {
		src, err := os.ReadFile(rel)
		if err != nil {
			return rep, err
		}
		m.exports = parseExports(string(src))
		rep.Stats.Exports += len(m.exports)
	}
	rep.Stats.FilesScanned = len(mods)

	// Pass B: record every import edge. HTML files participate: index.html's
	// inline module script is the application's real entry point.
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "wailsjs" || d.Name() == "node_modules" || d.Name() == "dist" {
				return fs.SkipDir
			}
			return nil
		}
		rel := filepath.ToSlash(p)
		isJS := strings.HasSuffix(rel, ".js")
		isHTML := strings.HasSuffix(rel, ".html")
		if !isJS && !isHTML {
			return nil
		}
		src, err := os.ReadFile(rel)
		if err != nil {
			return err
		}
		for _, imp := range parseImports(string(src), isHTML) {
			target := resolve(rel, imp.spec)
			if target == "" {
				continue // bare specifier: "node:test" and friends
			}
			m, ok := mods[target]
			if !ok {
				continue // resolves outside the scanned root
			}
			rep.Stats.Imports++
			m.anyImporter[rel] = true
			for _, name := range imp.names {
				if m.importedBy[name] == nil {
					m.importedBy[name] = map[string]bool{}
				}
				m.importedBy[name][rel] = true
			}
		}
		return nil
	})
	if err != nil {
		return rep, err
	}

	skip := map[string]bool{}
	for _, e := range excludes {
		if e = strings.TrimSpace(filepath.ToSlash(e)); e != "" {
			skip[e] = true
		}
	}

	rep.Findings = applyRules(mods, skip)
	return rep, nil
}

// applyRules turns the finished graph into findings, sorted so that two runs
// over unchanged code produce byte-identical output.
func applyRules(mods map[string]*module, skip map[string]bool) []finding {
	var out []finding

	rels := make([]string, 0, len(mods))
	for rel := range mods {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	for _, rel := range rels {
		m := mods[rel]

		// A test file is a consumer of other modules, never a supplier: nothing
		// is supposed to import *.test.js, so reporting its exports would be
		// pure noise. Its own exports (helpers shared between test files) are
		// out of scope for this layer.
		if isTest(rel) || skip[rel] {
			continue
		}

		// unused-module. Reported before the per-symbol rules so a file nobody
		// imports produces ONE alert instead of one per export, which is the
		// difference between a finding a reviewer acts on and a wall they
		// dismiss. The per-symbol rules are skipped for such a file.
		if len(m.anyImporter) == 0 {
			if len(m.exports) == 0 {
				continue // no exports and no importers: not a module, skip
			}
			out = append(out, finding{
				Rule:   "unused-module",
				File:   rel,
				Line:   1,
				Symbol: "",
				Message: fmt.Sprintf(
					"No file imports %s. It declares %d export(s) and is not reachable from any HTML entry point.",
					path.Base(rel), len(m.exports)),
			})
			continue
		}

		// A wholesale import (namespace, dynamic, side effect) makes every
		// export potentially reachable. See the package comment: guessing which
		// members are read would produce false positives.
		if len(m.importedBy["*"]) > 0 {
			continue
		}

		for _, ex := range m.exports {
			importers := m.importedBy[ex.name]
			if len(importers) == 0 {
				out = append(out, finding{
					Rule:   "unused-export",
					File:   rel,
					Line:   ex.line,
					Symbol: ex.name,
					Message: fmt.Sprintf(
						"%s is exported from %s but no file imports it.",
						ex.name, path.Base(rel)),
				})
				continue
			}
			if onlyTests(importers) {
				out = append(out, finding{
					Rule:   "test-only-export",
					File:   rel,
					Line:   ex.line,
					Symbol: ex.name,
					Message: fmt.Sprintf(
						"%s is exported from %s and imported only by test files (%s). It is kept alive by its own test.",
						ex.name, path.Base(rel), strings.Join(sortedKeys(importers), ", ")),
				})
			}
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Rule != out[j].Rule {
			return out[i].Rule < out[j].Rule
		}
		return out[i].Symbol < out[j].Symbol
	})
	return out
}

func isTest(rel string) bool { return strings.HasSuffix(rel, ".test.js") }

func onlyTests(importers map[string]bool) bool {
	for rel := range importers {
		if !isTest(rel) {
			return false
		}
	}
	return len(importers) > 0
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ── Parsing ──────────────────────────────────────────────────────────────────

// importRef is one resolved import site. names holds the exported identifiers
// pulled in, or the single element "*" for a wholesale import.
type importRef struct {
	spec  string
	names []string
}

// parseExports returns every export site in src, with its 1-based line.
func parseExports(src string) []exportedSymbol {
	var out []exportedSymbol
	for _, st := range statements(src, false) {
		switch {
		case reExportDecl.MatchString(st.text):
			m := reExportDecl.FindStringSubmatch(st.text)
			out = append(out, exportedSymbol{name: m[1], line: st.line})

		case reExportStar.MatchString(st.text):
			// Forwarding export: nothing is declared here, so there is no
			// symbol of our own to report. The import edge it creates is
			// picked up by parseImports.

		case reExportBrace.MatchString(st.text):
			m := reExportBrace.FindStringSubmatch(st.text)
			for _, name := range splitSpecifiers(m[1]) {
				out = append(out, exportedSymbol{name: name, line: st.line})
			}

		case reExportDefault.MatchString(st.text):
			out = append(out, exportedSymbol{name: "default", line: st.line})
		}
	}
	return out
}

// parseImports returns every import site in src. When html is true the scanner
// also looks inside the file for un-anchored `import ... from` statements,
// which is how index.html's inline module script is found.
func parseImports(src string, html bool) []importRef {
	var out []importRef

	if html {
		for _, m := range reImportInHTML.FindAllStringSubmatch(src, -1) {
			out = append(out, importRef{spec: m[2], names: splitImportClause(m[1])})
		}
		return out
	}

	for _, st := range statements(src, true) {
		switch {
		case reImportFrom.MatchString(st.text):
			m := reImportFrom.FindStringSubmatch(st.text)
			out = append(out, importRef{spec: m[2], names: splitImportClause(m[1])})

		case reImportBare.MatchString(st.text):
			// Side-effect import: no bindings, but the module is loaded, so
			// nothing in it can be called dead on our evidence.
			m := reImportBare.FindStringSubmatch(st.text)
			out = append(out, importRef{spec: m[1], names: []string{"*"}})

		case reExportStar.MatchString(st.text):
			m := reExportStar.FindStringSubmatch(st.text)
			out = append(out, importRef{spec: m[1], names: []string{"*"}})

		case reExportBrace.MatchString(st.text):
			// `export { a } from "./m.js"` re-exports a from m: an import of m.
			m := reExportBrace.FindStringSubmatch(st.text)
			if m[2] != "" {
				out = append(out, importRef{spec: m[2], names: splitSpecifiers(m[1])})
			}
		}

		// Dynamic import can appear mid-statement, so it is checked in addition
		// to the switch above rather than as one of its arms.
		for _, m := range reImportDynamic.FindAllStringSubmatch(st.text, -1) {
			out = append(out, importRef{spec: m[1], names: []string{"*"}})
		}
	}
	return out
}

// statement is one logical line: a source line with any continuation lines of a
// multi-line brace list folded into it.
type statement struct {
	text string
	line int // 1-based line of the statement's FIRST line
}

// statements splits src into logical lines, stripping comments.
//
// It joins a line whose brace list is left open (`import {` on its own line,
// which the codebase uses for long import lists) with the lines that follow,
// up to the closing brace. `anyLine` controls anchoring: export scanning only
// cares about lines that begin at column 0, but a dynamic import can be
// indented anywhere, so import scanning keeps every line.
func statements(src string, anyLine bool) []statement {
	lines := strings.Split(src, "\n")
	var out []statement

	inBlockComment := false
	for i := 0; i < len(lines); i++ {
		raw := lines[i]

		// Block comments. Handled line-wise: a /* */ that opens and closes on
		// one line is cut out, otherwise the rest of the line is dropped and
		// the state carries to the next.
		if inBlockComment {
			if idx := strings.Index(raw, "*/"); idx >= 0 {
				raw = raw[idx+2:]
				inBlockComment = false
			} else {
				continue
			}
		}
		for {
			open := strings.Index(raw, "/*")
			if open < 0 {
				break
			}
			if close := strings.Index(raw[open+2:], "*/"); close >= 0 {
				raw = raw[:open] + raw[open+2+close+2:]
				continue
			}
			raw = raw[:open]
			inBlockComment = true
			break
		}

		// Line comments. `//` inside a string literal would be cut wrongly, but
		// no import or export statement in an ES module contains one, and the
		// scanner only ever acts on lines it recognises as such.
		if idx := strings.Index(raw, "//"); idx >= 0 {
			raw = raw[:idx]
		}

		text := raw
		if !anyLine {
			// Export scanning: only column-0 statements. Anything indented is
			// inside a block and cannot be a module-level export.
			if strings.TrimLeft(text, " \t") != text {
				continue
			}
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		// Fold a multi-line brace list into one logical statement.
		startLine := i + 1
		if strings.Count(text, "{") > strings.Count(text, "}") &&
			(strings.HasPrefix(text, "import") || strings.HasPrefix(text, "export")) {
			for i+1 < len(lines) {
				i++
				next := lines[i]
				if idx := strings.Index(next, "//"); idx >= 0 {
					next = next[:idx]
				}
				text += " " + strings.TrimSpace(next)
				if strings.Count(text, "{") <= strings.Count(text, "}") {
					break
				}
			}
		}

		out = append(out, statement{text: text, line: startLine})
	}
	return out
}

// splitImportClause turns the text between `import` and `from` into the list of
// exported names it binds.
//
//	{ a, b as c }        → [a b]
//	Def                  → [default]
//	Def, { a }           → [default a]
//	* as ns              → [*]      (wholesale: see the package comment)
func splitImportClause(clause string) []string {
	clause = strings.TrimSpace(clause)
	if clause == "" {
		return []string{"*"}
	}

	var names []string

	// Namespace import anywhere in the clause makes the whole thing wholesale.
	if strings.Contains(clause, "*") {
		return []string{"*"}
	}

	brace := strings.Index(clause, "{")
	head := clause
	if brace >= 0 {
		head = clause[:brace]
		end := strings.Index(clause[brace:], "}")
		body := clause[brace+1:]
		if end >= 0 {
			body = clause[brace+1 : brace+end]
		}
		names = append(names, splitSpecifiers(body)...)
	}

	// A default binding before the brace: `import Def, { a } from ...`.
	head = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(head), ","))
	if head != "" {
		names = append(names, "default")
	}

	if len(names) == 0 {
		return []string{"*"}
	}
	return names
}

// splitSpecifiers turns a brace body into the exported names it refers to,
// keeping the LEFT side of an `as` alias: that is the name the exporting module
// published, and the only one worth matching against its export list.
func splitSpecifiers(body string) []string {
	var out []string
	for _, part := range strings.Split(body, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if m := reAsAlias.FindStringSubmatch(part); m != nil {
			out = append(out, m[1])
			continue
		}
		// Guard against anything that is not an identifier, so a malformed line
		// contributes nothing rather than a junk name that can never match.
		if regexpIdent.MatchString(part) {
			out = append(out, part)
		}
	}
	return out
}

var regexpIdent = regexp.MustCompile(`^[A-Za-z_$][\w$]*$`)

// resolve turns a module specifier into a repo-relative, slash-separated path.
//
// Returns "" for anything that is not a relative file specifier: "node:test",
// bare package names, and absolute URLs. A bare specifier cannot name a file in
// this repo, and CLAUDE.md §4 forbids remote assets outright, so there is
// nothing to resolve.
func resolve(importer, spec string) string {
	if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
		return ""
	}
	joined := path.Join(path.Dir(importer), spec)
	if !strings.HasSuffix(joined, ".js") {
		// Extensionless specifiers need a resolver algorithm; this codebase
		// always writes the extension, as the browser requires.
		return ""
	}
	return joined
}
