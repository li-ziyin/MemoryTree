package mcpserver

import (
	"testing"
)

func TestSuggestMountRanksProjectByOverlap(t *testing.T) {
	srv := newTestServer(t)

	result := callTool(t, srv, "memory_suggest_mount", map[string]any{
		"type":    "project",
		"title":   "SSE 解码吞空格",
		"summary": "前端 SSE 解码按原始字节处理，别按字符",
		"tags":    []string{"sse", "frontend"},
	})
	if result["isError"] == true {
		t.Fatalf("memory_suggest_mount failed: %+v", toolError(t, result))
	}
	env := mustMap(t, result["structuredContent"], "structuredContent")
	items := mustSlice(t, env["data"], "suggestions")
	if len(items) == 0 {
		t.Fatal("no suggestions returned")
	}
	top := mustMap(t, items[0], "top suggestion")
	if top["path"] != "work/agentflow" {
		t.Errorf("top suggestion = %v, want work/agentflow", top["path"])
	}
	if top["trust"] != trustUntrusted {
		t.Errorf("trust = %v", top["trust"])
	}
	meta := mustMap(t, env["meta"], "meta")
	if meta["total"] != float64(len(items)) {
		t.Errorf("meta total = %v, items = %d", meta["total"], len(items))
	}
}

func TestSuggestMountUserTypePrefersGlobal(t *testing.T) {
	srv := newTestServer(t)

	result := callTool(t, srv, "memory_suggest_mount", map[string]any{
		"type":    "user",
		"title":   "用户画像更新",
		"summary": "偏好先定方案再动手",
	})
	env := mustMap(t, result["structuredContent"], "structuredContent")
	items := mustSlice(t, env["data"], "suggestions")
	if len(items) == 0 {
		t.Fatal("no suggestions returned")
	}
	if top := mustMap(t, items[0], "top suggestion"); top["path"] != "global" {
		t.Errorf("top suggestion = %v, want global", top["path"])
	}
}

func TestSuggestMountRejectsBadInput(t *testing.T) {
	srv := newTestServer(t)

	for name, args := range map[string]map[string]any{
		"missing title":   {"type": "project", "summary": "s"},
		"bad type":        {"type": "note", "title": "t", "summary": "s"},
		"empty summary":   {"type": "project", "title": "t", "summary": " "},
		"limit too large": {"type": "project", "title": "t", "summary": "s", "limit": 11},
		"unknown field":   {"type": "project", "title": "t", "summary": "s", "node": "global"},
	} {
		result := callTool(t, srv, "memory_suggest_mount", args)
		if result["isError"] != true {
			t.Errorf("%s: expected tool error, got %+v", name, result)
		}
		if err := toolError(t, result); err.Code != "INVALID_ARGUMENTS" {
			t.Errorf("%s: code = %v, want INVALID_ARGUMENTS", name, err.Code)
		}
	}
}
