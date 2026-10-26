package mount

import (
	"testing"

	"github.com/li-ziyin/memorytree/internal/model"
	"github.com/li-ziyin/memorytree/internal/validate"
)

func testNodes() []CandidateNode {
	return []CandidateNode{
		{ID: "n-global", Path: "global", Kind: "category", Slug: "global",
			Title: "全局", Summary: "跨项目通用记忆", Tags: []string{"global"},
			Status: "active", Sensitivity: "private"},
		{ID: "n-work", Path: "work", Kind: "category", Slug: "work",
			Title: "工作", Summary: "求职与职业", Tags: []string{"work"},
			Status: "active", Sensitivity: "private"},
		{ID: "n-agentflow", Path: "work/agentflow", Kind: "project", Slug: "agentflow",
			Title: "AgentFlow", Summary: "Spring Boot 多 Agent 工作流平台",
			Tags:   []string{"work", "java", "spring", "agent"},
			Status: "active", Sensitivity: "private",
			Entries: []EntryLite{
				{Title: "SSE 解码坑", Summary: "前端 SSE 解码按字节处理", Tags: []string{"sse", "frontend"}},
				{Title: "线程池调参", Summary: "虚拟线程池大小经验值", Tags: []string{"thread"}},
			}},
		{ID: "n-memorytree", Path: "work/memorytree", Kind: "project", Slug: "memorytree",
			Title: "MemoryTree", Summary: "跨 Agent 共享记忆层，Go 单二进制",
			Tags:   []string{"work", "go", "memory", "mcp"},
			Status: "active", Sensitivity: "private"},
		{ID: "n-old", Path: "hobby/old", Kind: "project", Slug: "old",
			Title: "旧项目", Summary: "已归档", Tags: []string{"hobby"},
			Status: "archived", Sensitivity: "private"},
	}
}

func TestSuggestRanksTagAndTextMatchesFirst(t *testing.T) {
	nodes := testNodes()
	got := Suggest(nodes, Query{
		Type:    "project",
		Title:   "Go 并发写锁的坑",
		Summary: "go 单写者锁在 windows 上要处理共享冲突",
		Tags:    []string{"go", "concurrency"},
	}, 3)

	if len(got) == 0 || got[0].NodeID != "n-memorytree" {
		t.Fatalf("expected memorytree first, got %+v", got)
	}
	if got[0].TagScore != 0.5 { // "go" matched, "concurrency" did not
		t.Fatalf("tag score = %v, want 0.5", got[0].TagScore)
	}
	if got[0].TextScore <= 0 {
		t.Fatalf("expected text overlap with memorytree summary, got %v", got[0].TextScore)
	}
	if len(got[0].MatchedTags) != 1 || got[0].MatchedTags[0] != "go" {
		t.Fatalf("matched tags = %v", got[0].MatchedTags)
	}
	if len(got) > 3 {
		t.Fatalf("limit not honored: %d", len(got))
	}
}

func TestSuggestCJKTokensMatchEntryText(t *testing.T) {
	got := Suggest(testNodes(), Query{
		Type:    "project",
		Title:   "SSE 解码又吞空格了",
		Summary: "前端 sse 解码按字节处理才对",
		Tags:    []string{"frontend"},
	}, 1)

	if len(got) != 1 || got[0].NodeID != "n-agentflow" {
		t.Fatalf("expected agentflow first, got %+v", got)
	}
	if got[0].TextScore <= 0 {
		t.Fatalf("expected CJK text overlap to score, got %v", got[0].TextScore)
	}
}

func TestSuggestUserPriorPrefersGlobal(t *testing.T) {
	nodes := testNodes()
	// Even with a Java-heavy query, a user-profile entry should land on
	// global thanks to the explicit prior.
	got := Suggest(nodes, Query{
		Type:    "user",
		Title:   "用户画像",
		Summary: "java spring 偏好",
		Tags:    []string{"java", "profile"},
	}, 1)

	if len(got) != 1 || got[0].NodeID != "n-global" {
		t.Fatalf("expected global first for user type, got %+v", got)
	}
	found := false
	for _, r := range got[0].Reasons {
		if r == "user-profile entries belong under global" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing prior reason: %v", got[0].Reasons)
	}
}

func TestSuggestSkipsNonHostAndArchivedNodes(t *testing.T) {
	got := Suggest(testNodes(), Query{Type: "reference", Title: "anything"}, 10)
	for _, s := range got {
		if s.NodeID == "n-work" {
			t.Fatal("category node work must not be a candidate")
		}
		if s.NodeID == "n-old" {
			t.Fatal("archived node must not be a candidate")
		}
	}
}

func TestSuggestTieBreaksByPath(t *testing.T) {
	nodes := []CandidateNode{
		{ID: "b", Path: "zeta/tool", Kind: "project", Slug: "tool", Status: "active"},
		{ID: "a", Path: "alpha/tool", Kind: "project", Slug: "tool", Status: "active"},
	}
	got := Suggest(nodes, Query{Type: "reference", Title: "nothing matches"}, 2)
	if len(got) != 2 || got[0].NodeID != "a" {
		t.Fatalf("expected deterministic path order, got %+v", got)
	}
}

func TestFromSnapshotGroupsAndExcludes(t *testing.T) {
	snap := &validate.Snapshot{
		Nodes: []validate.NodeInfo{
			{ID: "n1", Kind: model.KindProject, Slug: "p1", Path: "p1", Status: model.StatusActive},
			{ID: "n2", Kind: model.KindCategory, Slug: "cat", Path: "cat", Status: model.StatusActive},
			{ID: "n3", Kind: model.KindCategory, Slug: model.GlobalSlug, Path: "global", Status: model.StatusActive},
		},
		Entries: []validate.EntryInfo{
			{ID: "e1", NodeID: "n1", Status: model.StatusActive, Title: "one"},
			{ID: "e2", NodeID: "n1", Status: model.StatusActive, Title: "two"},
			{ID: "e3", NodeID: "n1", Status: model.EntrySuperseded, Title: "old"},
			{ID: "held-out", NodeID: "n1", Status: model.StatusActive, Title: "held"},
		},
	}
	nodes := FromSnapshot(snap, "held-out")
	if len(nodes) != 2 { // p1 and global; plain category dropped
		t.Fatalf("nodes = %+v", nodes)
	}
	for _, n := range nodes {
		if n.ID == "n1" && len(n.Entries) != 2 {
			t.Fatalf("p1 sample = %+v, want the two active non-excluded entries", n.Entries)
		}
	}
}
