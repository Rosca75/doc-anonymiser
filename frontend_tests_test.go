// frontend_tests_test.go, the guard that keeps the frontend unit tests HONEST.
//
// A failing test is useful. A test that passes while asserting nothing, or while
// asserting a contract the application retired, is worse than no test at all: it
// reports safety that is not there, and it is the reason a green suite sat beside
// seven reported defects.
//
// This file guards the three false-green modes that can be detected
// mechanically, so nobody has to remember them:
//
//  1. THE TEST NEVER RAN. `node --test frontend/*.test.js` silently ignored
//     everything under frontend/views/, so a test file added next to a view
//     module never executed and CI stayed green. Verified: a deliberately
//     failing frontend/views/probe.test.js was not picked up and the suite
//     still reported 350 passing. This test expands the pattern CI actually
//     runs and fails if any test file on disk would be missed.
//
//  2. THE TEST WAS SILENCED. A skipped or todo test reads as a pass in the exit
//     code, and `.only` plus --test-only hides every other test in the file.
//
//  3. THE MODULE HAS NO TEST AT ALL. A new view or view-model arriving without
//     one is the commonest way coverage quietly stops tracking the code. The
//     exemptions are enumerated below WITH REASONS, so the list is a decision
//     somebody made rather than a silence.
//
// What it deliberately does NOT do: scan the test files for retired identifiers
// (client_names, the old step tokens). Those legitimately appear in the very
// tests that PROVE the retirement, such as copy.test.js asserting
// `!("client_names" in CATEGORY_LABELS)`, and in the store test that checks an
// unknown step token falls back to Import. A guard there would fail on the tests
// doing the right thing. Retirement is already guarded where it belongs, in
// category_parity_test.go and step_parity_test.go, against the SOURCE.
//
// Source inspection only, so this needs no browser and no node: it runs inside
// plain `go test ./...` on every push, which is where a guard has to be to be
// worth anything.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const ciWorkflow = ".github/workflows/ci.yml"

// nodeTestStepRe pulls the `node --test <patterns>` command out of ci.yml, so
// this test checks the command CI ACTUALLY RUNS rather than a copy of it that
// could drift.
var nodeTestStepRe = regexp.MustCompile(`run:\s*node --test\s+(.+)`)

// frontendTestFiles walks frontend/ for every test file that exists on disk,
// wherever it sits.
func frontendTestFiles(t *testing.T) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir("frontend", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".test.js") {
			found = append(found, filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not walk frontend/ for test files: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("found no *.test.js under frontend/. Either the frontend unit tests have been " +
			"deleted or this test is looking in the wrong place.")
	}
	sort.Strings(found)
	return found
}

// globToRegexp translates the small glob dialect these patterns use into a
// regexp: `**/` matches any number of directories (including none) and `*`
// matches within one path segment.
//
// It is written out rather than reached for in a library because the whole point
// is to reproduce what node does with the pattern, and a dependency for six lines
// of translation would have to go in the pinned-versions table (root CLAUDE.md
// section 7).
func globToRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch {
		case strings.HasPrefix(pattern[i:], "**/"):
			b.WriteString("(?:[^/]+/)*")
			i += 2
		case pattern[i] == '*':
			b.WriteString("[^/]*")
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// TestEveryFrontendTestFileIsActuallyRun is mode 1: the test that never ran.
func TestEveryFrontendTestFileIsActuallyRun(t *testing.T) {
	workflow, err := os.ReadFile(ciWorkflow)
	if err != nil {
		t.Fatalf("could not read %s: %v", ciWorkflow, err)
	}

	match := nodeTestStepRe.FindSubmatch(workflow)
	if match == nil {
		t.Fatalf("found no `run: node --test ...` step in %s.\n"+
			"The frontend unit tests are layer 2 of the test strategy (docs/UITESTING.md) and must run "+
			"in the blocking job. If the step was renamed, update nodeTestStepRe here.", ciWorkflow)
	}

	// The patterns as CI passes them, minus the quoting. The quotes are not
	// decoration: they hand the pattern to node instead of to bash, whose globstar
	// is off by default and would expand `**` as a plain `*`, which is exactly the
	// hole this test exists to close.
	raw := strings.TrimSpace(string(match[1]))
	if !strings.Contains(raw, `"`) && strings.Contains(raw, "**") {
		t.Errorf("the node --test pattern in %s uses ** but is NOT quoted: %s\n"+
			"Unquoted, bash expands it with globstar off, so `frontend/**/*.test.js` collapses to "+
			"`frontend/*/*.test.js` and the tests directly under frontend/ stop running. Quote it.",
			ciWorkflow, raw)
	}

	var patterns []*regexp.Regexp
	for _, field := range strings.Fields(raw) {
		field = strings.Trim(field, `"'`)
		if field == "" {
			continue
		}
		re, err := globToRegexp(field)
		if err != nil {
			t.Fatalf("could not understand the pattern %q from %s: %v", field, ciWorkflow, err)
		}
		patterns = append(patterns, re)
	}
	if len(patterns) == 0 {
		t.Fatalf("the node --test step in %s passes no pattern at all", ciWorkflow)
	}

	var missed []string
	for _, file := range frontendTestFiles(t) {
		covered := false
		for _, re := range patterns {
			if re.MatchString(file) {
				covered = true
				break
			}
		}
		if !covered {
			missed = append(missed, file)
		}
	}

	if len(missed) > 0 {
		t.Errorf("these frontend test files exist but the command CI runs (node --test %s) would "+
			"never execute them:\n  %s\n"+
			"They would pass silently by not running at all, which is the worst kind of green. "+
			"Fix: use a recursive, QUOTED pattern in %s, currently `node --test \"frontend/**/*.test.js\"`.",
			raw, strings.Join(missed, "\n  "), ciWorkflow)
	}
}

// silencedRe finds a test that cannot fail because it never runs, and `.only`,
// which hides its siblings.
var silencedRe = regexp.MustCompile(`\b(?:test|it|describe|suite)\.(skip|todo|only)\b|[{,]\s*(?:skip|todo|only)\s*:\s*true`)

// TestNoFrontendTestIsSilenced is mode 2: the test that was switched off.
//
// A skipped test still exits 0, so the suite reports success for a check nobody
// is making. `.only` is worse: with --test-only it runs that test and no other,
// and a suite of one passing test also exits 0.
//
// If a test genuinely has to be parked, delete it and open an issue. A skip is a
// note to nobody.
func TestNoFrontendTestIsSilenced(t *testing.T) {
	for _, file := range frontendTestFiles(t) {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("could not read %s: %v", file, err)
		}
		for i, line := range strings.Split(string(source), "\n") {
			// A comment explaining that skipping is forbidden is not itself a skip.
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
				continue
			}
			if m := silencedRe.FindString(line); m != "" {
				t.Errorf("%s:%d uses %q, so that test does not run but the suite still exits 0:\n  %s\n"+
					"A silenced test reports safety that is not there. Either fix the test and let it run, "+
					"or delete it: a skip is a note to nobody.",
					file, i+1, m, trimmed)
			}
		}
	}
}

// frontendModulesWithoutTests lists the modules deliberately NOT covered by a
// unit test, each with the reason. This is mode 3's exemption table, and it is
// the point of the check: a new module gets a test or gets a line here, and
// either way somebody decided.
//
// Every entry is a module with no branching worth asserting, or one whose logic
// lives somewhere already covered. When this check first ran it also named
// nav.js, modal.js and toast.js, and those three got tests (nav.test.js,
// notices.test.js) rather than entries: an exemption is for a module where a test
// would assert nothing, not for one nobody has got round to.
var frontendModulesWithoutTests = map[string]string{
	"frontend/html.js":            "one function, escapeHTML, exercised by every render test that escapes anything",
	"frontend/icons.js":           "a map of vendored SVG strings, no logic to assert",
	"frontend/main.js":            "pure wiring: it attaches listeners to shell.js markup, which shell.test.js covers. What it renders is asserted by the layer 3 rendering harness",
	"frontend/views/home.js":      "a static landing page with no branching",
	"frontend/views/identify.js":  "layout and footer only; the halves that carry logic are identifyrail.js and identifyworkspace.js, both tested",
	"frontend/views/allowlist.js": "chip markup over a plain array; the allowlist rules live in the engine and are tested there",
}

// TestEveryFrontendModuleWithLogicHasATest is mode 3: the module nothing tests.
//
// "Imported by a test" is a coarse measure and it is chosen on purpose. It cannot
// tell a thorough test from a shallow one, so it does not pretend to; what it CAN
// do is make the arrival of an untested module a build failure rather than a thing
// nobody noticed, which is where coverage actually decays.
func TestEveryFrontendModuleWithLogicHasATest(t *testing.T) {
	// Which modules does the suite import? A test file references a sibling as
	// "./state.js" and a view as "./views/import.js", so the import path maps
	// straight back onto a repository path.
	imported := map[string]bool{}
	for _, file := range frontendTestFiles(t) {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("could not read %s: %v", file, err)
		}
		for _, m := range regexp.MustCompile(`from "\./([A-Za-z0-9_/.-]+\.js)"`).FindAllStringSubmatch(string(source), -1) {
			imported["frontend/"+m[1]] = true
		}
	}

	var untested []string
	err := filepath.WalkDir("frontend", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		path = filepath.ToSlash(path)
		if d.IsDir() {
			// docs/ is the bundled user documentation and assets/ is vendored SVGs;
			// neither holds application logic.
			if d.Name() == "docs" || d.Name() == "assets" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".js") || strings.HasSuffix(d.Name(), ".test.js") {
			return nil
		}
		if imported[path] {
			return nil
		}
		if _, exempt := frontendModulesWithoutTests[path]; exempt {
			return nil
		}
		untested = append(untested, path)
		return nil
	})
	if err != nil {
		t.Fatalf("could not walk frontend/: %v", err)
	}

	if len(untested) > 0 {
		sort.Strings(untested)
		t.Errorf("no frontend test imports these modules:\n  %s\n"+
			"Every module carrying logic needs a unit test that moves with it, or an entry in "+
			"frontendModulesWithoutTests here saying why not (frontend/CLAUDE.md, Testing). "+
			"Export the pure builder and assert what a pane SHOWS with testhtml.js; see "+
			"docs/UITESTING.md layer 2.",
			strings.Join(untested, "\n  "))
	}

	// The exemption table must not outlive the modules it excuses, or it silently
	// starts exempting nothing while reading as though it exempts something.
	for path := range frontendModulesWithoutTests {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("frontendModulesWithoutTests exempts %s, which no longer exists.\n"+
				"Remove the entry: a stale exemption reads as a considered decision and is not one.", path)
		}
		if imported[path] {
			t.Errorf("frontendModulesWithoutTests exempts %s, but a test imports it now.\n"+
				"Remove the entry so the module stays covered: %s",
				path, frontendModulesWithoutTests[path])
		}
	}
}

// TestFrontendTestCommandIsDocumentedConsistently keeps the command in the
// charters and the docs identical to the one CI runs.
//
// The owner of this repository directs agents from the documentation, so a
// charter showing the flat pattern would keep sending agents to the command with
// the hole in it, however correct ci.yml became.
func TestFrontendTestCommandIsDocumentedConsistently(t *testing.T) {
	workflow, err := os.ReadFile(ciWorkflow)
	if err != nil {
		t.Fatalf("could not read %s: %v", ciWorkflow, err)
	}
	match := nodeTestStepRe.FindSubmatch(workflow)
	if match == nil {
		t.Fatalf("found no `run: node --test ...` step in %s", ciWorkflow)
	}
	want := "node --test " + strings.TrimSpace(string(match[1]))

	// Anything that looks like an invocation but is not the current one. `node
	// --test` on its own, with no pattern, is prose ("testable under node --test")
	// and not an instruction, so it is left alone.
	stale := regexp.MustCompile(`node --test\s+"?[A-Za-z0-9_*/.{}-]+\.test\.js"?`)

	for _, doc := range []string{"CLAUDE.md", "frontend/CLAUDE.md", "backend/CLAUDE.md", "docs/UITESTING.md"} {
		source, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("could not read %s: %v", doc, err)
		}
		for i, line := range strings.Split(string(source), "\n") {
			for _, found := range stale.FindAllString(line, -1) {
				// Markdown and the repo map quote the command various ways; compare the
				// commands themselves, not the punctuation around them.
				if normaliseCommand(found) != normaliseCommand(want) {
					t.Errorf("%s:%d documents `%s` but CI runs `%s`.\n"+
						"A charter is what an agent follows, so a stale command here outlives a correct "+
						"ci.yml. Make them the same.", doc, i+1, found, want)
				}
			}
		}
	}
}

func normaliseCommand(s string) string {
	return strings.ReplaceAll(strings.Join(strings.Fields(s), " "), `"`, "")
}

// used by the failure messages above; kept so a future check can reuse it.
var _ = fmt.Sprintf
