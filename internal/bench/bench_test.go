package bench_test

import (
	"context"
	"testing"

	"github.com/li-ziyin/memorytree/internal/bench"
	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/scalegen"
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

func TestRunRanksGoldFirst(t *testing.T) {
	root := t.TempDir()
	if err := scalegen.Generate(root, 150); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		t.Fatal(err)
	}
	gold := goldEntryID(t, root)
	results, err := bench.Run(root, []bench.Question{{ID: 1, Query: "金丝雀 needle-0", ExpectID: gold}})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Rank != 1 || results[0].TopID != gold {
		t.Fatalf("result = %+v", results[0])
	}
}
