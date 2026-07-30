// step_parity_test.go — the wizard-step parity guard (BUILD-05 Phase 0).
//
// The wizard's step list is declared in FIVE places, in three different
// files, and nothing used to hold them together:
//
//	frontend/state.js   WIZARD_STEPS   the ordered tokens, the source of order
//	frontend/state.js   STEP_RESETS    what each step owns and therefore clears
//	frontend/main.js    STEP_LABELS    the visible label on each step-bar circle
//	frontend/main.js    VIEWS          the renderer for each step
//	frontend/copy.js    NAV.stepNames  the step name used in prose
//
// BUILD-04 CR3 renamed one step and had to touch four of the five. BUILD-05
// removes a step entirely and renames two more. A rename that reaches four of
// the five places produces a step with no renderer (a blank screen), or a
// reset that silently stops running, or a confirmation sentence naming a step
// that no longer exists. None of those fail a build; they just misbehave.
//
// So this test reads the three files, extracts the five token sets, and fails
// naming the exact offender when any of them disagrees with WIZARD_STEPS.
//
// It is deliberately a text parser, in the same spirit as
// category_parity_test.go: the alternative is a JavaScript runtime in the Go
// test suite, and the declarations it reads are plain literals by convention.
// If it cannot find a declaration it FAILS rather than passing on an empty
// set, so a refactor that changes the declaration style breaks this test
// loudly instead of quietly disabling it.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// wizardStepsRe matches `export const WIZARD_STEPS = ["a", "b", ...]`,
// including a multi-line array literal.
var wizardStepsRe = regexp.MustCompile(`(?s)export const WIZARD_STEPS = \[(.*?)\]`)

// stepTokenRe pulls the double-quoted tokens out of that array body.
var stepTokenRe = regexp.MustCompile(`"([a-z_]+)"`)

// objectKeyRe matches an object-literal key at the point the scanner has
// reached: `import:`, `export :`, and so on. Quoted keys are not matched on
// purpose; none of the five declarations uses them, and accepting them would
// hide a declaration style this parser has not been taught.
var objectKeyRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*:`)

// readFrontend reads one frontend file, failing the test with an actionable
// message rather than returning an error nobody handles.
func readFrontend(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("could not read %s: %v\nThis guard compares the wizard step "+
			"declarations across state.js, main.js and copy.js; it cannot run "+
			"without all three.", name, err)
	}
	return string(raw)
}

// objectKeys returns the TOP-LEVEL keys of the object literal that starts at
// the first occurrence of prefix (which must end with the opening brace).
//
// The scan is a small hand-written lexer rather than a regexp because the
// bodies contain nested objects, arrow functions and template strings:
// STEP_RESETS entries look like `import: () => ({ importErrors: [] }),`, so a
// regexp over lines would either miss keys or collect the nested ones. Depth
// tracking, with strings and line comments skipped, is the smallest thing
// that reads all five declarations correctly.
//
// what names the declaration in failure messages.
func objectKeys(t *testing.T, src, prefix, what string) []string {
	t.Helper()
	if !strings.HasSuffix(prefix, "{") {
		t.Fatalf("objectKeys: prefix for %s must end with the opening brace, got %q", what, prefix)
	}
	idx := strings.Index(src, prefix)
	if idx < 0 {
		t.Fatalf("could not find %s (looked for %q).\nThis guard reads that "+
			"declaration verbatim; if you changed its shape, teach the parser "+
			"the new shape rather than deleting the guard.", what, prefix)
	}

	// Start ON the opening brace so the depth counter sees it.
	i := idx + len(prefix) - 1
	depth := 0
	// lastSig is the last significant (non-whitespace) byte consumed. A key
	// can only follow the opening brace or a comma, which is how the scanner
	// tells `import:` in `{ import: ... }` from a `?:` conditional or a
	// labelled statement inside a value.
	var lastSig byte
	var keys []string

	for i < len(src) {
		c := src[i]

		// Line comment: skip to the newline. STEP_RESETS documents every
		// entry with one.
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		// Block comment.
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				t.Fatalf("unterminated block comment while scanning %s", what)
			}
			i += 2 + end + 2
			continue
		}
		// String or template literal: skip it whole, honouring escapes, so a
		// brace or a comma inside copy text cannot move the depth counter.
		if c == '"' || c == '\'' || c == '`' {
			quote := c
			i++
			for i < len(src) && src[i] != quote {
				if src[i] == '\\' {
					i++
				}
				i++
			}
			i++
			lastSig = quote
			continue
		}
		if c == '{' {
			depth++
			i++
			lastSig = c
			continue
		}
		if c == '}' {
			depth--
			if depth == 0 {
				return keys // the literal is closed; we have every key
			}
			i++
			lastSig = c
			continue
		}
		if depth == 1 && (lastSig == '{' || lastSig == ',') {
			if m := objectKeyRe.FindStringSubmatch(src[i:]); m != nil {
				keys = append(keys, m[1])
				i += len(m[1])
				lastSig = ':' // not '{' or ',', so the value cannot be read as a key
				continue
			}
		}
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			lastSig = c
		}
		i++
	}

	t.Fatalf("unterminated object literal for %s (never found its closing brace)", what)
	return nil
}

// wizardSteps returns the ordered step tokens from state.js WIZARD_STEPS.
func wizardSteps(t *testing.T) []string {
	t.Helper()
	src := readFrontend(t, "frontend/state.js")
	m := wizardStepsRe.FindStringSubmatch(src)
	if m == nil {
		t.Fatal("could not find `export const WIZARD_STEPS = [...]` in frontend/state.js; " +
			"this guard cannot work, fix the parser or the declaration style")
	}
	var steps []string
	for _, q := range stepTokenRe.FindAllStringSubmatch(m[1], -1) {
		steps = append(steps, q[1])
	}
	if len(steps) == 0 {
		t.Fatal("frontend/state.js WIZARD_STEPS parsed as empty; fix the parser or the declaration style")
	}
	return steps
}

// diff reports what one declaration is missing from WIZARD_STEPS and what it
// invents on top of it, as two sorted lists.
func diff(want []string, got []string) (missing, extra []string) {
	wantSet := map[string]bool{}
	for _, s := range want {
		wantSet[s] = true
	}
	gotSet := map[string]bool{}
	for _, s := range got {
		gotSet[s] = true
	}
	for _, s := range want {
		if !gotSet[s] {
			missing = append(missing, s)
		}
	}
	for s := range gotSet {
		if !wantSet[s] {
			extra = append(extra, s)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

func TestEveryWizardStepDeclarationCoversTheSameSteps(t *testing.T) {
	steps := wizardSteps(t)

	stateJS := readFrontend(t, "frontend/state.js")
	mainJS := readFrontend(t, "frontend/main.js")
	copyJS := readFrontend(t, "frontend/copy.js")

	// Each entry: the declaration, and what a mismatch actually breaks for a
	// user. The consequence is part of the failure message because "these
	// lists differ" is not enough to act on.
	cases := []struct {
		what        string
		keys        []string
		consequence string
	}{
		{
			what:        "frontend/state.js STEP_RESETS",
			keys:        objectKeys(t, stateJS, "export const STEP_RESETS = {", "frontend/state.js STEP_RESETS"),
			consequence: "a step with no entry is never reset when the user navigates back past it, so stale answers survive into a later run",
		},
		{
			what:        "frontend/main.js STEP_LABELS",
			keys:        objectKeys(t, mainJS, "const STEP_LABELS = {", "frontend/main.js STEP_LABELS"),
			consequence: "a step with no entry shows its raw token in the step bar",
		},
		{
			what:        "frontend/main.js VIEWS",
			keys:        objectKeys(t, mainJS, "const VIEWS = {", "frontend/main.js VIEWS"),
			consequence: "a step with no entry renders a BLANK screen (main.js calls VIEWS[step] unconditionally)",
		},
		{
			what:        "frontend/copy.js NAV.stepNames",
			keys:        objectKeys(t, copyJS, "stepNames: {", "frontend/copy.js NAV.stepNames"),
			consequence: "a step with no entry is named by its raw token in the navigation copy",
		},
	}

	for _, c := range cases {
		missing, extra := diff(steps, c.keys)
		if len(missing) > 0 {
			t.Errorf("%s is missing: %s\n  WIZARD_STEPS is [%s]\n  Consequence: %s",
				c.what, strings.Join(missing, ", "), strings.Join(steps, ", "), c.consequence)
		}
		if len(extra) > 0 {
			t.Errorf("%s declares steps WIZARD_STEPS does not have: %s\n  WIZARD_STEPS is [%s]\n"+
				"  Either the step was removed and this entry is dead, or the token was renamed on one side only.",
				c.what, strings.Join(extra, ", "), strings.Join(steps, ", "))
		}
	}
}

func TestWizardStepTokensAreUniqueAndOrdered(t *testing.T) {
	steps := wizardSteps(t)

	seen := map[string]int{}
	for i, s := range steps {
		if first, dup := seen[s]; dup {
			t.Errorf("frontend/state.js WIZARD_STEPS lists %q twice (positions %d and %d); "+
				"step order and the per-step guards both read this array by index",
				s, first, i)
		}
		seen[s] = i
	}

	// Import is always reachable and is the fallback for an unknown persisted
	// token (BUILD-05 decision 1), so it has to be first. Export is the last
	// step by definition: canGoTo() special-cases it as "needs results".
	if len(steps) == 0 || steps[0] != "import" {
		t.Errorf("WIZARD_STEPS must start with \"import\": it is the only always-reachable step "+
			"and the fallback for an unknown persisted token. Got [%s]", strings.Join(steps, ", "))
	}
	if len(steps) == 0 || steps[len(steps)-1] != "export" {
		t.Errorf("WIZARD_STEPS must end with \"export\": nothing follows saving the output. Got [%s]",
			strings.Join(steps, ", "))
	}
}

func TestNoNativeDialogsInTheFrontend(t *testing.T) {
	// BUILD-05 decision 10: every native confirm() goes, replaced by the in-app
	// modal. alert() and prompt() are covered by the same rule; neither was in
	// use, and this keeps them out.
	//
	// The reason is not aesthetic. A native dialog in a WebView is unstyled and
	// unbranded, it cannot carry more than one cramped line of explanation, and
	// on Windows it steals focus from the window it belongs to. The replacements
	// are modal.js askConfirm() for a question and toast.js for a statement.
	//
	// Test files are scanned too: a test that called confirm() would be asserting
	// behaviour the application must not have.
	nativeDialogRe := regexp.MustCompile(`(^|[^.\w])(confirm|alert|prompt)\s*\(`)

	var hits []string
	scan := func(dir, name string) {
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("could not read %s: %v", path, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			// Comment lines are skipped: prose ABOUT the rule, including the
			// several places that explain what was removed and why, is not a
			// violation of it.
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
				continue
			}
			// A line embedding a <script> tag is an XSS-escaping FIXTURE, not a
			// call: highlight.test.js feeds `<script>alert(1)</script>` through
			// the renderer to prove it comes back escaped. Skipping the whole
			// line is narrower than trying to tell a string literal from code,
			// and there is no reason for production markup to contain the tag.
			if strings.Contains(line, "<script") {
				continue
			}
			if nativeDialogRe.MatchString(line) {
				hits = append(hits, fmt.Sprintf("%s:%d: %s", path, i+1, trimmed))
			}
		}
	}

	for _, dir := range []string{"frontend", "frontend/views"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("could not read %s: %v", dir, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".js") {
				scan(dir, entry.Name())
			}
		}
	}

	if len(hits) > 0 {
		t.Errorf("native browser dialogs found in the frontend (BUILD-05 decision 10):\n%s\n"+
			"Use modal.js askConfirm() for a question and toast.js for a statement. A native "+
			"dialog in the WebView is unstyled, cannot explain itself in more than a line, and "+
			"on Windows steals focus from the window it belongs to.",
			strings.Join(hits, "\n"))
	}
}
