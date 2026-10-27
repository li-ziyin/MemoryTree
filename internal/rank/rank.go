// Package rank re-orders FTS-ranked search hits with an external usage
// signal: each hit's bm25 score (negative; more negative ranks first) is
// multiplied by the entry's usage-heat multiplier. Frequently returned
// entries float up mildly; never-returned ones keep multiplier 1, so cold is
// never a penalty and strong relevance always dominates — a hot hit can only
// overtake a marginally better-ranked cold one, never a clearly better one.
//
// The same re-ranking is shared by the MCP read tools and `memoryd bench`,
// so the retrieval regression gate exercises exactly what production serves.
package rank

import (
	"sort"

	"github.com/li-ziyin/memorytree/internal/index"
)

// HeatOf looks up one entry's usage-heat multiplier. Implementations return
// 1 for entries without usage history.
type HeatOf func(entryID string) float64

// Apply returns hits re-ordered by Score×heat. Hits without a relevance
// score (unfiltered listings) and empty heat maps pass through unchanged.
// The sort is stable, so ties keep their incoming (index) order and results
// stay deterministic.
func Apply(hits []index.Hit, heat HeatOf) []index.Hit {
	if len(hits) < 2 || heat == nil {
		return hits
	}
	scored := false
	for _, h := range hits {
		if h.Score != 0 {
			scored = true
			break
		}
	}
	if !scored {
		return hits
	}
	out := append([]index.Hit(nil), hits...)
	sort.SliceStable(out, func(i, j int) bool {
		return effective(out[i], heat) < effective(out[j], heat)
	})
	return out
}

// effective is hit.Score scaled by heat; the map-free HeatOf form keeps the
// lookup honest for entries the caller has no usage row for.
func effective(h index.Hit, heat HeatOf) float64 {
	multiplier := heat(h.ID)
	if multiplier < 1 {
		multiplier = 1
	}
	return h.Score * multiplier
}
