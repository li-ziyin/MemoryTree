package bench_test

import (
	"context"
	"testing"
	"time"

	"github.com/li-ziyin/memorytree/internal/bench"
	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/scalegen"
	"github.com/li-ziyin/memorytree/internal/usagelog"
)

func goldEntryID(t *testing.T, root string) string {
	t.Helper()
	ix, err := index.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	hits, err := ix.Search(context.Background(), index.SearchOptions{Query: "金丝雀 needle-0"})
	if err != nil || len(hits) == 0 {
		t.Fatalf("no gold entry: %v", err)
	}
	return hits[0].ID
}

func scaleRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := scalegen.Generate(root, 150); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRunRanksGoldFirst(t *testing.T) {
	root := scaleRepo(t)
	gold := goldEntryID(t, root)
	results, err := bench.Run(root, []bench.Question{{ID: 1, Query: "金丝雀 needle-0", ExpectID: gold}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Rank != 1 || results[0].TopID != gold {
		t.Fatalf("result = %+v", results[0])
	}
}

// The gate runs with the same re-ranking production serves, so a boost must
// actually reach the result sets: a maximally hot entry overtakes its
// near-tie peers on a query the whole corpus matches.
func TestRunWithHeatAppliesBoost(t *testing.T) {
	root := scaleRepo(t)
	plain, err := bench.Run(root, []bench.Question{{ID: 1, Query: "网关 排障", Limit: 20}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plain[0].Hits < 5 {
		t.Fatalf("fixture too small for a near-tie: %+v", plain[0])
	}
	ix, err := index.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	hits, err := ix.Search(context.Background(), index.SearchOptions{Query: "网关 排障", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	lagging := hits[len(hits)-1].ID

	for i := 0; i < 1000; i++ {
		if err := usagelog.Record(root, []string{lagging}, "seed", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	agg, err := usagelog.Heats(root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	boosted, err := bench.Run(root, []bench.Question{{ID: 1, Query: "网关 排障", Limit: 20, ExpectID: lagging}}, func(id string) float64 {
		return agg[id]
	})
	if err != nil {
		t.Fatal(err)
	}
	if boosted[0].TopID != lagging || boosted[0].Rank != 1 {
		t.Fatalf("heat did not lift the maximally hot entry: %+v", boosted[0])
	}
}

// The coldest possible situation — a unique exact match with every other
// entry maximally hot — still returns the gold first: heat refines ranking
// among matches, it never buries relevance or summons non-matches.
func TestRunWithHeatKeepsUniqueMatchFirst(t *testing.T) {
	root := scaleRepo(t)
	gold := goldEntryID(t, root)

	ix, err := index.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	maxHeat := usagelog.Usage{Count: 1_000_000, Last: time.Now().UTC().Format(time.RFC3339)}.Heat(time.Now())
	results, err := bench.Run(root, []bench.Question{{ID: 1, Query: "金丝雀 needle-0", ExpectID: gold}}, func(id string) float64 {
		if id == gold {
			return 1
		}
		return maxHeat
	})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Rank != 1 || results[0].TopID != gold {
		t.Fatalf("hot noise displaced the unique match: %+v", results[0])
	}
}
