// engine/origin.go — which ROUTE produced a detection, and the order in which
// routes supersede each other.
//
// Four routes can claim the same characters: the deterministic regex signals,
// the values and patterns the user declared, the offline Smart detection, and
// the local AI. When two of them claim one string, exactly one has to win, and
// the rule has to be the same everywhere: in overlap resolution, in the
// pre-run intersection check, and in the ownership unification that decides
// which category a string is filed under for the whole session. Origin is that
// rule's input.
package engine

// Origin names WHICH ROUTE produced a detection. It is deliberately separate
// from Confidence: confidence answers "how much is this trusted" and feeds
// MinConfidence, origin answers "who found it" and feeds precedence. With one
// number doing both jobs, raising the confidence floor silently reordered
// precedence, and two routes that scored the same (a regex signal and a custom
// pattern, both 1.0) were separated by whichever match happened to be longer.
const (
	OriginNative   = "native"   // pass 1 regex signal, and the code detector
	OriginDeclared = "declared" // the user typed it: custom patterns and manual values
	OriginAuto     = "auto"     // offline Smart detection
	OriginAI       = "ai"       // the local model
)

// AllOrigins lists the routes in precedence order, mirrored by frontend
// state.js ORIGINS and checked by ../../origin_parity_test.go.
var AllOrigins = []string{OriginNative, OriginDeclared, OriginAuto, OriginAI}

// originRanks is the superseding order as a lookup. Native beats everything
// because pass 1 already runs before pass 2; a value the user typed and a
// pattern the user wrote are the same act, so they share one rank.
var originRanks = map[string]int{
	OriginNative:   1,
	OriginDeclared: 2,
	OriginAuto:     3,
	OriginAI:       4,
}

// OriginRank is the superseding order. LOWER WINS.
//
// An unknown or empty origin ranks with declared values rather than last, so a
// producer that states none is trusted rather than silently demoted. Ranking it
// last would mean any detection path that forgot to stamp an origin quietly
// lost every contest, which is a bug that shows up as a missing replacement
// rather than as an error.
//
// @param origin one of the Origin constants, or "" from a producer that
//
//	states none
//
// @return 1 (native) through 4 (ai); 2 for anything unrecognised
func OriginRank(origin string) int {
	if rank, ok := originRanks[origin]; ok {
		return rank
	}
	return originRanks[OriginDeclared]
}
