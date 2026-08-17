// origin_parity_test.go — the JavaScript to Go parity guard for detection
// ORIGINS.
//
// An origin is the route that produced a value, and it is the FIRST comparator
// when two routes claim the same text. It is therefore a contract, not a
// display string: the frontend stamps it on every accepted value and sends it
// across the bridge, and Go ranks it. A route added on one side and forgotten
// on the other does not fail anywhere: the unknown origin falls back to
// "declared", so an AI proposal would quietly start outranking Smart detection
// and nothing would say so.
//
// That is the same failure category_parity_test.go exists for, so it gets the
// same guard. It also checks the LABEL, because a value carrying a route the
// copy has no word for renders its raw identifier on the card, which is the
// unexplained jargon the copy rules forbid.
package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
)

// originsListRe matches `export const ORIGINS = ["a", "b", ...];` in state.js,
// including a multi-line array literal.
var originsListRe = regexp.MustCompile(`(?s)export const ORIGINS = \[(.*?)\]`)

// originQuotedRe pulls the double-quoted values out of one array literal body.
var originQuotedRe = regexp.MustCompile(`"([a-z_]+)"`)

// frontendOrigins returns the origins state.js lists, in declaration order.
func frontendOrigins(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("frontend/state.js")
	if err != nil {
		t.Fatalf("could not read frontend/state.js: %v", err)
	}
	m := originsListRe.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("found no `export const ORIGINS = [...]` in frontend/state.js; " +
			"this guard cannot work, fix the parser or the declaration style")
	}
	var out []string
	for _, q := range originQuotedRe.FindAllStringSubmatch(m[1], -1) {
		out = append(out, q[1])
	}
	return out
}

// TestOriginsAgreeAcrossTheBridge: the same four routes, in the same
// precedence order, on both sides. Order matters as much as membership: it IS
// the superseding rule, and the frontend's list documents it for the reader of
// state.js.
func TestOriginsAgreeAcrossTheBridge(t *testing.T) {
	js := frontendOrigins(t)
	if strings.Join(js, ",") != strings.Join(engine.AllOrigins, ",") {
		t.Errorf("ORIGINS in frontend/state.js is %v, engine.AllOrigins is %v.\n"+
			"They must list the same routes in the same precedence order: the order is the\n"+
			"superseding rule, and a route only one side knows falls back to \"declared\",\n"+
			"which silently changes which route wins.", js, engine.AllOrigins)
	}
}

// TestEveryOriginHasAFrontendLabel: a value carrying an origin copy.js has no
// word for renders its raw identifier on the card.
func TestEveryOriginHasAFrontendLabel(t *testing.T) {
	raw, err := os.ReadFile("frontend/copy.js")
	if err != nil {
		t.Fatalf("could not read frontend/copy.js: %v", err)
	}
	// The originLabel table is a flat object literal of `key: "Label",` rows.
	block := regexp.MustCompile(`(?s)originLabel: \{(.*?)\}`).FindStringSubmatch(string(raw))
	if block == nil {
		t.Fatal("found no `originLabel: {...}` in frontend/copy.js")
	}
	labelled := map[string]bool{}
	for _, m := range regexp.MustCompile(`([a-z_]+):\s*"`).FindAllStringSubmatch(block[1], -1) {
		labelled[m[1]] = true
	}

	var missing []string
	for _, origin := range engine.AllOrigins {
		if !labelled[origin] {
			missing = append(missing, origin)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf(
			"WORKSPACE.originLabel in frontend/copy.js has no label for: %s\n"+
				"The origin chip renders the raw identifier without one, and the precedence\n"+
				"rule is only meaningful to a user who can read which route owns a value.",
			strings.Join(missing, ", "))
	}
}
