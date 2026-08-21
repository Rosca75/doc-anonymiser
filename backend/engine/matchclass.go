// engine/matchclass.go — provenance and precedence, kept apart on purpose.
//
// Two different questions get asked about every Value and every span, and one
// field cannot answer both:
//
//	HOW WAS THIS FOUND?      discovery methods. A SET, because several methods
//	                         can find the same thing, and accepting a Suggestion
//	                         has to keep all of them: that is what the review
//	                         workspace shows the user and what evidence hangs
//	                         off.
//	WHICH CLAIM WINS?        the match class. A single rank, consumed only by
//	                         overlap resolution, ownership unification and the
//	                         pre-run intersection check.
//
// Collapsing them is what a single "matchClass" field did, and it forced a choice
// between an incomplete provenance record and a non-deterministic precedence
// rule. Confidence is a third, separate thing: it answers "how much is this
// trusted" and feeds MinConfidence. With confidence doing precedence too,
// raising the floor silently reordered which route won.
package engine

// Discovery methods: HOW a Value or Suggestion was found. A Value carries every
// method that found it, so the set grows as routes agree rather than the last
// one overwriting the others.
//
// Built-in pattern matching and custom pattern matching are absent on purpose:
// they produce DIRECT MATCHES, never Suggestions, so nothing they find is ever
// a Value with provenance to record. A pattern's claim carries its match class
// on the span and nothing else.
const (
	// MethodManual is a Value the user declared: typed on Identify, or added
	// through Add missed Value or the Compare panel.
	MethodManual = "manual"
	// MethodSignal is signal-based discovery: a built-in pattern match used as
	// EVIDENCE to find related text elsewhere in the batch.
	MethodSignal = "signal"
	// MethodHeuristic is heuristic discovery: spelling, context, frequency and
	// deterministic gazetteers.
	MethodHeuristic = "heuristic"
	// MethodLocalLLM is Local LLM discovery: a language model running on this
	// machine read the text and proposed the Value.
	MethodLocalLLM = "local_llm"
)

// AllDiscoveryMethods lists the methods a Value can carry, mirrored by frontend
// state.js DISCOVERY_METHODS and checked by ../../discovery_parity_test.go.
var AllDiscoveryMethods = []string{
	MethodManual, MethodSignal, MethodHeuristic, MethodLocalLLM,
}

// Match classes: WHICH overlapping claim wins. LOWER RANK WINS.
const (
	// MatchClassBuiltInPattern is a built-in structured pattern match (pass 1
	// and the code detector), and an already-decided registry entry. It ranks
	// first because pass 1 already runs before pass 2, so the rank agrees with
	// the pass order rather than competing with it.
	MatchClassBuiltInPattern = "built_in_pattern"
	// MatchClassUserDefined is a manually declared Value or a custom pattern.
	// Both are the same act by the same person, so they share one rank.
	MatchClassUserDefined = "user_defined"
	// MatchClassRulesDiscovered is signal-based OR heuristic discovery: they share
	// ONE rank, and the name says what both are, a rule over the text rather than a
	// model reading it. It stays true if signal-based discovery is ever given its
	// own rail section, which a name after either half would not.
	MatchClassRulesDiscovered = "rules_discovered"
	// MatchClassLocalLLMDiscovered is Local LLM discovery.
	MatchClassLocalLLMDiscovered = "local_llm_discovered"
)

// AllMatchClasses lists the classes in precedence order, mirrored by frontend
// state.js MATCH_CLASSES and checked by ../../matchclass_parity_test.go.
var AllMatchClasses = []string{
	MatchClassBuiltInPattern,
	MatchClassUserDefined,
	MatchClassRulesDiscovered,
	MatchClassLocalLLMDiscovered,
}

// matchClassRanks is the superseding order as a lookup.
var matchClassRanks = map[string]int{
	MatchClassBuiltInPattern:     1,
	MatchClassUserDefined:        2,
	MatchClassRulesDiscovered:    3,
	MatchClassLocalLLMDiscovered: 4,
}

// methodClasses maps one discovery method to the match class it implies.
var methodClasses = map[string]string{
	MethodManual:    MatchClassUserDefined,
	MethodSignal:    MatchClassRulesDiscovered,
	MethodHeuristic: MatchClassRulesDiscovered,
	MethodLocalLLM:  MatchClassLocalLLMDiscovered,
}

// MatchClassRank is the superseding order. LOWER WINS.
//
// An unknown or empty class ranks with user-defined rather than last, so a
// producer that states none is TRUSTED rather than silently demoted. Ranking it
// last would mean a detection path that forgot to stamp a class quietly lost
// every contest, and that shows up as a missing replacement rather than as an
// error.
//
// @param class one of the MatchClass constants, or "" from a producer that
//
//	states none
//
// @return 1 (built-in pattern) through 4 (local LLM); 2 for anything unrecognised
func MatchClassRank(class string) int {
	if rank, ok := matchClassRanks[class]; ok {
		return rank
	}
	return matchClassRanks[MatchClassUserDefined]
}

// MatchClassForMethods reduces a Value's discovery methods to the ONE class
// precedence uses: the strongest applicable one.
//
// A Value found by both heuristic discovery and the local model is trusted as far
// as its strongest finder, because the weaker method agreeing with a stronger
// one is corroboration, not doubt. Provenance keeps both; precedence needs one
// answer, and this is where the set becomes it.
//
// @param methods the Value's discovery methods, in any order; may be empty
// @return the strongest class among them, MatchClassUserDefined when empty or
//
//	entirely unrecognised
func MatchClassForMethods(methods []string) string {
	best := ""
	bestRank := 0
	for _, m := range methods {
		class, ok := methodClasses[m]
		if !ok {
			continue
		}
		if rank := matchClassRanks[class]; best == "" || rank < bestRank {
			best, bestRank = class, rank
		}
	}
	if best == "" {
		return MatchClassUserDefined
	}
	return best
}
