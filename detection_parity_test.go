// detection_parity_test.go — the JavaScript to Go parity guards for the
// detection vocabularies the two sides SHARE.
//
// Each of these is a contract, not a display string, and each fails silently
// when the two sides drift:
//
//	DISCOVERY METHODS  provenance. The frontend renders which methods found a
//	                   Value and sends them back on the accepted Value; Go
//	                   reduces them to a match class. A method only one side
//	                   knows is not recognised by MatchClassForMethods, so it
//	                   falls back to user-defined and quietly starts outranking
//	                   the routes it should lose to.
//	MATCH CLASSES      precedence, in ORDER. The order IS the superseding rule,
//	                   and the frontend's list is what documents it for a reader
//	                   of state.js. It is also what the intersection copy reads
//	                   to name the winning method.
//	SIGNAL SOURCES     which built-in signals may derive Suggestions. A source
//	                   the frontend cannot render is a control the user cannot
//	                   reach; one Go does not implement is a control that appears
//	                   to do something and does not.
//
// Every guard also checks that copy.js has a WORD for each identifier, because a
// chip with no label renders the raw identifier, which is the unexplained jargon
// the copy rules forbid.
package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
)

// quotedTokenRe pulls the double-quoted identifiers out of one array literal.
var quotedTokenRe = regexp.MustCompile(`"([a-z_]+)"`)

// frontendList returns the identifiers a `export const NAME = [...]` literal in
// state.js declares, in declaration order.
func frontendList(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile("frontend/state.js")
	if err != nil {
		t.Fatalf("could not read frontend/state.js: %v", err)
	}
	re := regexp.MustCompile(`(?s)export const ` + name + ` = \[(.*?)\]`)
	m := re.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatalf("found no `export const %s = [...]` in frontend/state.js; this guard "+
			"cannot work, so fix the parser or the declaration style rather than deleting it", name)
	}
	var out []string
	for _, q := range quotedTokenRe.FindAllStringSubmatch(m[1], -1) {
		out = append(out, q[1])
	}
	return out
}

// frontendLabelKeys returns the keys of a flat `name: { key: "Label", ... }`
// object literal in copy.js.
func frontendLabelKeys(t *testing.T, table string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("frontend/copy.js")
	if err != nil {
		t.Fatalf("could not read frontend/copy.js: %v", err)
	}
	block := regexp.MustCompile(`(?s)` + table + `: \{(.*?)\n  \}`).FindStringSubmatch(string(raw))
	if block == nil {
		t.Fatalf("found no `%s: {...}` in frontend/copy.js", table)
	}
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`([a-z_]+):\s*"`).FindAllStringSubmatch(block[1], -1) {
		out[m[1]] = true
	}
	return out
}

// assertLabelled reports every identifier the copy table has no word for.
func assertLabelled(t *testing.T, table string, identifiers []string, why string) {
	t.Helper()
	labelled := frontendLabelKeys(t, table)
	var missing []string
	for _, id := range identifiers {
		if !labelled[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%s in frontend/copy.js has no label for: %s\n%s",
			table, strings.Join(missing, ", "), why)
	}
}

// TestDiscoveryMethodsAgreeAcrossTheBridge: the same methods on both sides.
func TestDiscoveryMethodsAgreeAcrossTheBridge(t *testing.T) {
	js := frontendList(t, "DISCOVERY_METHODS")
	if strings.Join(js, ",") != strings.Join(engine.AllDiscoveryMethods, ",") {
		t.Errorf("DISCOVERY_METHODS in frontend/state.js is %v, engine.AllDiscoveryMethods is %v.\n"+
			"A method only one side knows is not recognised when the set is reduced to a\n"+
			"match class, so it falls back to user-defined and quietly outranks the routes\n"+
			"it should lose to.", js, engine.AllDiscoveryMethods)
	}
}

// TestEveryDiscoveryMethodHasALabel: the workspace renders which methods found a
// Value, so every one needs a word.
func TestEveryDiscoveryMethodHasALabel(t *testing.T) {
	assertLabelled(t, "methodLabel", engine.AllDiscoveryMethods,
		"The workspace names which methods found a Value, and an unlabelled one renders\n"+
			"its raw identifier on the card.")
}

// TestMatchClassesAgreeAcrossTheBridge: the same classes in the same PRECEDENCE
// order. Order matters as much as membership, because the order is the rule.
func TestMatchClassesAgreeAcrossTheBridge(t *testing.T) {
	js := frontendList(t, "MATCH_CLASSES")
	if strings.Join(js, ",") != strings.Join(engine.AllMatchClasses, ",") {
		t.Errorf("MATCH_CLASSES in frontend/state.js is %v, engine.AllMatchClasses is %v.\n"+
			"They must list the same classes in the same precedence order: the order IS the\n"+
			"superseding rule, and a class only one side knows falls back to user-defined,\n"+
			"which silently changes which claim wins.", js, engine.AllMatchClasses)
	}
}

// TestEveryMatchClassHasALabel: the intersection warning names the WINNING
// method, and it reads the match class to do it.
func TestEveryMatchClassHasALabel(t *testing.T) {
	assertLabelled(t, "matchClassLabel", engine.AllMatchClasses,
		"The intersection warning names the winning method, never an internal rank, and it\n"+
			"reads this table to do it.")
}

// TestSignalSourcesAgreeAcrossTheBridge: the same sources on both sides. The
// checklist is built from the frontend list, so a source Go implements and the
// frontend does not is a feature with no control at all.
func TestSignalSourcesAgreeAcrossTheBridge(t *testing.T) {
	js := frontendList(t, "SIGNAL_SOURCES")
	if strings.Join(js, ",") != strings.Join(engine.AllSignalSources, ",") {
		t.Errorf("SIGNAL_SOURCES in frontend/state.js is %v, engine.AllSignalSources is %v.\n"+
			"A source only Go knows has no control the user can reach; one only the frontend\n"+
			"knows is a control that appears to do something and does not.",
			js, engine.AllSignalSources)
	}
}

// TestEverySignalSourceHasALabel: the checklist is data-driven, so its rows come
// from the identifier list and their words from copy.js.
func TestEverySignalSourceHasALabel(t *testing.T) {
	assertLabelled(t, "signalSourceLabel", engine.AllSignalSources,
		"The suggestion-source checklist is built from the identifier list, so an\n"+
			"unlabelled source renders as a checkbox named after a JSON key.")
}

// TestEveryEvidenceKindHasALabel: evidence is structured precisely so the
// frontend can turn it into a sentence from copy.js rather than the engine
// returning prose. A kind with no copy is a Suggestion that cannot explain
// itself, which is the whole point of carrying evidence.
func TestEveryEvidenceKindHasALabel(t *testing.T) {
	assertLabelled(t, "evidenceKindLabel", engine.AllEvidenceKinds,
		"Evidence is structured so the frontend can render a sentence from copy.js; a kind\n"+
			"with no copy leaves a Suggestion unable to say why it is there.")
}
