// uitest_parity_test.go, the guard that keeps the two layer-3 UI harnesses
// sharing ONE set of browser-side probes (docs/UITESTING.md).
//
// There are two harnesses, and there have to be: the Linux one
// (scripts/uitest/renderharness) gates in CI, and the Windows one
// (scripts/uitest/Invoke-UITest.ps1) is the only thing that can see the real
// WebView2 engine and the packaged .exe. What there must NOT be is two copies of
// the assertions. Both read scripts/uitest/probes.js and call the same
// __uiProbes.* functions; the harnesses own only the plumbing.
//
// That arrangement decays silently: someone in a hurry adds a querySelector to
// one harness, the other never learns about it, and because the Windows script
// has still never been executed, its copy is the one that rots unnoticed. So the
// arrangement is a test rather than a comment.
//
// This is a source-inspection test on purpose. It needs no browser, so it runs
// inside plain `go test ./...` on every push, which is exactly where a guard has
// to be to be worth anything.
package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	probesPath   = "scripts/uitest/probes.js"
	linuxHarness = "scripts/uitest/renderharness/checks.go"
	linuxMain    = "scripts/uitest/renderharness/main.go"
	winHarness   = "scripts/uitest/Invoke-UITest.ps1"
)

// probeCall finds every `__uiProbes.<name>` reference, in either harness or in
// probes.js itself.
var probeCall = regexp.MustCompile(`__uiProbes\.([A-Za-z][A-Za-z0-9]*)`)

func readFileOrFail(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read %s: %v\n"+
			"This file is part of the layer 3 UI test strategy (docs/UITESTING.md). "+
			"If it moved, update the paths at the top of uitest_parity_test.go.", path, err)
	}
	return string(data)
}

// TestProbesAreTheOnlyPlaceTheDOMIsTouched is the anti-duplication rule itself.
//
// A harness that queries the DOM or imports the store on its own has started a
// second copy of the assertions. The only file allowed to do either is probes.js.
func TestProbesAreTheOnlyPlaceTheDOMIsTouched(t *testing.T) {
	// Each needle is something only a browser-side PROBE has any business doing.
	// They are matched against the harness sources, where a hit means an
	// assertion has been written locally instead of in the shared file.
	needles := []string{
		"document.querySelector",
		"document.querySelectorAll",
		"getBoundingClientRect",
		"dispatchEvent",
		"getComputedStyle",
		`import("/state.js")`,
		"import('/state.js')",
		"setState(",
	}

	for _, harness := range []string{linuxHarness, linuxMain, winHarness} {
		source := readFileOrFail(t, harness)
		for _, needle := range needles {
			if strings.Contains(source, needle) {
				t.Errorf("%s contains %q, so it is probing the page itself.\n"+
					"Both harnesses must share ONE set of probes: move that JavaScript into %s as a "+
					"__uiProbes.<name> function and have the harness read the result. Two copies of an "+
					"assertion is one copy nobody updates, and the Windows script has never run, so its "+
					"copy is the one that would rot.",
					harness, needle, probesPath)
			}
		}
	}
}

// TestBothHarnessesCallOnlyDefinedProbes catches the other direction of drift: a
// harness calling a probe that does not exist, or one harness quietly falling
// behind the other.
func TestBothHarnessesCallOnlyDefinedProbes(t *testing.T) {
	probes := readFileOrFail(t, probesPath)

	// The definitions are the keys of the __uiProbes object literal. Matching
	// `name(` / `async name(` at the object's indentation is enough and avoids
	// pulling in a JavaScript parser for four lines of gain.
	defined := map[string]bool{}
	definitionLine := regexp.MustCompile(`^\s{4}(?:async\s+)?([A-Za-z][A-Za-z0-9]*)\s*[:(]`)
	for _, line := range strings.Split(probes, "\n") {
		if m := definitionLine.FindStringSubmatch(line); m != nil {
			defined[m[1]] = true
		}
	}
	if len(defined) == 0 {
		t.Fatalf("found no probe definitions in %s. The file must expose __uiProbes as an object "+
			"literal whose members are the probes.", probesPath)
	}

	calledBy := map[string][]string{}
	for _, harness := range []string{linuxHarness, linuxMain, winHarness} {
		source := readFileOrFail(t, harness)
		for _, m := range probeCall.FindAllStringSubmatch(source, -1) {
			name := m[1]
			if !defined[name] {
				t.Errorf("%s calls __uiProbes.%s, which %s does not define.\n"+
					"Define the probe there (it is the one place both harnesses read) or fix the name. "+
					"Probes that do exist: %s",
					harness, name, probesPath, strings.Join(sortedKeys(defined), ", "))
				continue
			}
			calledBy[name] = append(calledBy[name], harness)
		}
	}

	// Every probe the Linux harness asserts on must also be asserted on Windows.
	// The reverse is not required: -Packaged has checks Linux cannot make at all.
	linuxCalls := map[string]bool{}
	for name, harnesses := range calledBy {
		for _, h := range harnesses {
			if h == linuxHarness || h == linuxMain {
				linuxCalls[name] = true
			}
		}
	}
	winSource := readFileOrFail(t, winHarness)
	for name := range linuxCalls {
		if !strings.Contains(winSource, "__uiProbes."+name) {
			t.Errorf("the Linux harness asserts on __uiProbes.%s but %s never calls it.\n"+
				"A check that gates in CI on Linux should also run on the platform this application "+
				"actually ships on. Add the corresponding Test-* assertion to the PowerShell script.",
				name, winHarness)
		}
	}
}

// TestProbesFileIsOneExpression pins the contract both harnesses rely on: the
// whole file is a single expression that installs __uiProbes and evaluates to the
// string "installed". A file that returns anything else would install silently and
// then fail somewhere far away.
func TestProbesFileIsOneExpression(t *testing.T) {
	probes := strings.TrimSpace(readFileOrFail(t, probesPath))

	if !strings.Contains(probes, `return "installed";`) {
		t.Errorf("%s must end its expression by returning the string \"installed\", which is what both "+
			"harnesses check to know the probes really ran.", probesPath)
	}
	if !strings.HasSuffix(probes, "})()") {
		t.Errorf("%s must be ONE immediately-invoked expression, so Runtime.evaluate can install it in a "+
			"single call. It currently ends with: %q", probesPath, tail(probes, 40))
	}
	if !strings.Contains(probes, "globalThis.__uiProbes") {
		t.Errorf("%s must assign globalThis.__uiProbes: that is the handle both harnesses call through.",
			probesPath)
	}
}

// TestProbesAreNotEmbeddedInTheBinary keeps the test scaffolding out of the
// shipped application.
//
// The frontend is compiled in with `//go:embed all:frontend` (root CLAUDE.md
// section 3). probes.js seeds application state and pokes at the DOM, so it has no
// business inside the binary a client runs; it lives under scripts/ precisely so
// the embed directive cannot see it.
func TestProbesAreNotEmbeddedInTheBinary(t *testing.T) {
	if !strings.HasPrefix(probesPath, "scripts/") {
		t.Fatalf("the shared probes are at %s, which is not under scripts/. Anything under frontend/ is "+
			"embedded into the shipped binary by //go:embed all:frontend, and test scaffolding must not "+
			"ship.", probesPath)
	}
	if _, err := os.Stat("frontend/probes.js"); err == nil {
		t.Errorf("frontend/probes.js exists, so the probes would be embedded into the shipped binary. " +
			"The one copy belongs at scripts/uitest/probes.js.")
	}
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
