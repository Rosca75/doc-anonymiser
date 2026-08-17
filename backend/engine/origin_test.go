package engine

import "testing"

// TestOriginRank pins the superseding order, including the two inputs that are
// not constants: an origin nobody recognises and an empty one. Both rank with
// declared values, because ranking them last would mean any producer that
// forgot to stamp an origin quietly loses every contest, and that shows up as a
// missing replacement rather than as an error.
func TestOriginRank(t *testing.T) {
	cases := []struct {
		origin string
		want   int
	}{
		{OriginNative, 1},
		{OriginDeclared, 2},
		{OriginAuto, 3},
		{OriginAI, 4},
		{"", 2},
		{"something-else", 2},
	}
	for _, tc := range cases {
		if got := OriginRank(tc.origin); got != tc.want {
			t.Errorf("OriginRank(%q) = %d, want %d", tc.origin, got, tc.want)
		}
	}
}

// TestOriginRankIsATotalOrder: the four routes must be strictly ordered, or
// two of them tie and the winner falls through to a comparator that answers a
// different question (how long the match is), which is the behaviour that read
// as random.
func TestOriginRankIsATotalOrder(t *testing.T) {
	for i := 1; i < len(AllOrigins); i++ {
		prev, cur := AllOrigins[i-1], AllOrigins[i]
		if OriginRank(prev) >= OriginRank(cur) {
			t.Errorf("%s must supersede %s, got ranks %d and %d",
				prev, cur, OriginRank(prev), OriginRank(cur))
		}
	}
}
