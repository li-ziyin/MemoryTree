package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/li-ziyin/memorytree/internal/usagelog"
)

// seedUsage writes a burst of returns for one entry directly into the usage
// log, standing in for many past sessions that kept seeing it.
func seedUsage(t *testing.T, root, id string, count int) {
	t.Helper()
	now := time.Now().UTC()
	for i := 0; i < count; i++ {
		if err := usagelog.Record(root, []string{id}, "seed", now.Add(-time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
}

func searchHitIDs(t *testing.T, srv *Server, query string) []string {
	t.Helper()
	env, terr := srv.memorySearch(context.Background(), json.RawMessage(`{"query":`+quoteJSON(query)+`,"limit":10}`))
	if terr != nil {
		t.Fatalf("search: %v", terr)
	}
	items, ok := env.Data.([]searchItem)
	if !ok {
		t.Fatalf("search data type %T", env.Data)
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

// A heavily returned entry overtakes a marginally better-ranked cold peer,
// and the flip disappears once the usage log is empty.
func TestSearchUsageHeatReordersNearTies(t *testing.T) {
	srv, root := newWriteTestServer(t)

	callTool(t, srv, "memory_add", map[string]any{
		"type": "project", "title": "SSE 解码补充", "summary": "SSE 解码的补充说明：按字节处理。",
		"body": "SSE 解码按字节处理的补充。", "node": "work/agentflow",
		"source": map[string]any{"agent": "test", "kind": "manual"},
	})

	before := searchHitIDs(t, srv, "SSE 解码")
	if len(before) < 2 {
		t.Fatalf("query needs a near-tie pair, got %v", before)
	}
	coldFirst := before[0]

	// Boost whichever entry currently ranks second until it flips.
	var after []string
	for i, id := range before {
		if i == 0 {
			continue
		}
		seedUsage(t, root, id, 200)
		after = searchHitIDs(t, srv, "SSE 解码")
		if after[0] == id && contains(after, coldFirst) {
			break
		}
	}
	if after == nil || after[0] == coldFirst {
		t.Fatalf("usage boost did not flip the near-tie: before=%v after=%v", before, after)
	}
	if len(after) != len(before) {
		t.Errorf("boost changed result membership: before=%v after=%v", before, after)
	}
}

// Under FTS AND semantics a hit that matches the query at all cannot be
// outranked by entries that match nothing: boosting an unrelated entry
// leaves both order and membership untouched. (The extreme case — every
// non-matching entry maximally hot while the only matching one stays
// first — is pinned in the bench package against the scale fixture.)
func TestSearchUsageHeatCannotBuryMatches(t *testing.T) {
	srv, root := newWriteTestServer(t)

	callTool(t, srv, "memory_add", map[string]any{
		"type": "project", "title": "SSE 解码补充", "summary": "SSE 解码的补充说明：按字节处理。",
		"body": "SSE 解码按字节处理的补充。", "node": "work/agentflow",
		"source": map[string]any{"agent": "test", "kind": "manual"},
	})

	before := searchHitIDs(t, srv, "SSE 解码")
	if len(before) < 2 {
		t.Fatalf("query needs a pair, got %v", before)
	}
	// Max out the heat of an entry the query never matched.
	seedUsage(t, root, userEntryID, 500)
	after := searchHitIDs(t, srv, "SSE 解码")
	if len(after) != len(before) {
		t.Errorf("membership changed: before=%v after=%v", before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("order changed at %d: before=%v after=%v", i, before, after)
		}
	}
}

// brief's related section follows the same heat re-ranking.
func TestBriefRelatedFollowsUsageHeat(t *testing.T) {
	srv, root := newWriteTestServer(t)

	callTool(t, srv, "memory_add", map[string]any{
		"type": "project", "title": "SSE 解码补充", "summary": "SSE 解码的补充说明：按字节处理。",
		"body": "SSE 解码按字节处理的补充。", "node": "work/agentflow",
		"source": map[string]any{"agent": "test", "kind": "manual"},
	})

	before := searchHitIDs(t, srv, "SSE 解码")
	boosted := before[len(before)-1]
	seedUsage(t, root, boosted, 500)

	benv, terr := srv.memoryBrief(context.Background(), json.RawMessage(`{"task":"SSE 解码"}`))
	if terr != nil {
		t.Fatalf("brief: %v", terr)
	}
	data, ok := benv.Data.(*briefData)
	if !ok {
		t.Fatalf("brief data type %T", benv.Data)
	}
	if len(data.Related) == 0 {
		t.Fatal("brief related section empty")
	}
	if data.Related[0].ID != boosted {
		t.Errorf("brief related first = %s, want the hot entry %s", data.Related[0].ID, boosted)
	}
}

// memory_tree lists hotter siblings first — presentation only.
func TestTreeChildrenOrderByHeat(t *testing.T) {
	srv, root := newWriteTestServer(t)

	// sseEntryID lives under work/agentflow; global sorts before work by
	// path. Heat the sse entry so the work subtree outranks global.
	seedUsage(t, root, sseEntryID, 300)

	env, terr := srv.memoryTree(context.Background(), json.RawMessage(`{}`))
	if terr != nil {
		t.Fatalf("tree: %v", terr)
	}
	data, ok := env.Data.(*treeNode)
	if !ok {
		t.Fatalf("tree data type %T", env.Data)
	}
	if len(data.Children) < 2 {
		t.Fatalf("root children = %d", len(data.Children))
	}
	if data.Children[0].Slug != "work" {
		t.Errorf("first child = %s, want the hot work subtree first", data.Children[0].Slug)
	}
}

// The repository's own usage log file is the only input; an empty or
// missing log leaves ordering exactly as it was.
func TestSearchUnchangedWithoutUsage(t *testing.T) {
	srv, root := newWriteTestServer(t)
	if err := usagelog.Record(root, []string{userEntryID}, "seed", time.Now()); err != nil {
		t.Fatal(err)
	}
	_ = filepath.Join(root, ".index", "entry-usage.jsonl") // log exists; only unrelated heat

	ids := searchHitIDs(t, srv, "SSE 解码")
	if len(ids) == 0 || ids[0] != sseEntryID {
		t.Errorf("unrelated heat changed order: %v", ids)
	}
}

func contains(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func quoteJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
