package scalegen_test

import (
	"context"
	"testing"

	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/scalegen"
)

func TestGenerateProducesValidRepository(t *testing.T) {
	root := t.TempDir()
	if err := scalegen.Generate(root, 10); err != nil {
		t.Fatal(err)
	}
	res, err := index.Build(root, index.BuildOptions{})
	if err != nil {
		t.Fatalf("generated repository does not validate: %v", err)
	}
	if res.Entries != 10 {
		t.Fatalf("entries = %d, want 10", res.Entries)
	}
}

func benchRoot(b *testing.B, n int) string {
	b.Helper()
	root := b.TempDir()
	if err := scalegen.Generate(root, n); err != nil {
		b.Fatal(err)
	}
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		b.Fatal(err)
	}
	return root
}

func benchSizes() []struct {
	name string
	n    int
} {
	return []struct {
		name string
		n    int
	}{{"100", 100}, {"1000", 1000}, {"5000", 5000}}
}

// BenchmarkBuildRebuild measures a full index rebuild, the cost every source
// change pays today (incremental indexing is deferred to M2).
func BenchmarkBuildRebuild(b *testing.B) {
	for _, sz := range benchSizes() {
		b.Run(sz.name, func(b *testing.B) {
			root := benchRoot(b, sz.n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := index.Build(root, index.BuildOptions{Rebuild: true}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkStartupNoop measures validate + fingerprint on an unchanged
// repository: the fixed cost every memoryd mcp start pays.
func BenchmarkStartupNoop(b *testing.B) {
	root := benchRoot(b, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := index.Build(root, index.BuildOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearchFTS(b *testing.B) { // all tokens >= 3 runes: trigram path
	root := benchRoot(b, 1000)
	ix, err := index.Open(root)
	if err != nil {
		b.Fatal(err)
	}
	defer ix.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hits, err := ix.Search(context.Background(), index.SearchOptions{Query: "docker backoff"})
		if err != nil {
			b.Fatal(err)
		}
		if len(hits) == 0 {
			b.Fatal("no hits")
		}
	}
}

func BenchmarkSearchShortCJK(b *testing.B) { // 2-rune tokens: short path
	root := benchRoot(b, 1000)
	ix, err := index.Open(root)
	if err != nil {
		b.Fatal(err)
	}
	defer ix.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hits, err := ix.Search(context.Background(), index.SearchOptions{Query: "端口 网关"})
		if err != nil {
			b.Fatal(err)
		}
		if len(hits) == 0 {
			b.Fatal("no hits")
		}
	}
}
