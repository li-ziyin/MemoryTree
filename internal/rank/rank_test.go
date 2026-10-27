package rank

import (
	"testing"

	"github.com/li-ziyin/memorytree/internal/index"
)

func hits(ss ...float64) []index.Hit {
	out := make([]index.Hit, len(ss))
	for i, s := range ss {
		out[i] = index.Hit{ID: string(rune('a' + i)), Score: s}
	}
	return out
}

func ids(hs []index.Hit) string {
	s := ""
	for _, h := range hs {
		s += h.ID
	}
	return s
}

// A boosted hit can overtake a marginally better-ranked cold one: bm25 is
// negative, so score×heat (heat>1) is more negative and sorts first.
func TestApplyBoostFlipsNearTies(t *testing.T) {
	in := hits(-11, -10) // cold "b" already trails cold "a"
	out := Apply(in, func(id string) float64 {
		if id == "b" {
			return 1.5
		}
		return 1
	})
	if ids(out) != "ba" {
		t.Fatalf("order = %q, want boosted b first", ids(out))
	}
}

// The boost is bounded: it refines near-ties but cannot leapfrog a clearly
// better relevance score.
func TestApplyCannotLeapfrogStrongRelevance(t *testing.T) {
	in := hits(-100, -8) // "a" is far more relevant than "b"
	out := Apply(in, func(id string) float64 {
		if id == "b" {
			return 5 // beyond any real heat multiplier
		}
		return 1
	})
	if ids(out) != "ab" {
		t.Fatalf("order = %q, want relevance to dominate", ids(out))
	}
}

// Entries without usage history are neutral, never penalized: a missing
// heat row (0) clamps to 1 and keeps its relative position.
func TestApplyMissingHeatIsNeutral(t *testing.T) {
	in := hits(-11, -10)
	out := Apply(in, func(string) float64 { return 0 })
	if ids(out) != "ab" {
		t.Fatalf("order = %q, want unchanged", ids(out))
	}
}

// Heat below 1 (none is produced today, but a bad loader could) is clamped
// to neutral so cold entries can never be pushed below their relevance.
func TestApplyClampsBelowOne(t *testing.T) {
	in := hits(-11, -10)
	out := Apply(in, func(id string) float64 {
		if id == "a" {
			return 0.1
		}
		return 1
	})
	if ids(out) != "ab" {
		t.Fatalf("order = %q, want no penalty applied", ids(out))
	}
}

// Unfiltered listings carry no score and keep their incoming order.
func TestApplyPassesListingsThrough(t *testing.T) {
	in := hits(0, 0, 0)
	out := Apply(in, func(string) float64 { return 9 })
	if ids(out) != "abc" {
		t.Fatalf("order = %q, want unchanged listing", ids(out))
	}
}

// Ties keep their incoming order: the re-rank is deterministic.
func TestApplyIsStable(t *testing.T) {
	in := hits(-10, -10, -10)
	out := Apply(in, func(string) float64 { return 3 })
	if ids(out) != "abc" {
		t.Fatalf("order = %q, want stable ties", ids(out))
	}
	if ids(in) != "abc" {
		t.Fatalf("input mutated: %q", ids(in))
	}
}

func TestApplyShortCircuits(t *testing.T) {
	called := false
	out := Apply(hits(-1), func(string) float64 { called = true; return 5 })
	if ids(out) != "a" || called {
		t.Fatalf("single hit should pass through untouched")
	}
	out = Apply(hits(-1, -2), nil)
	if ids(out) != "ab" {
		t.Fatalf("nil heat should pass through")
	}
}
