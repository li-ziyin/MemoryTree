package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/li-ziyin/memorytree/internal/index"
)

// newWriteTestServer copies spec/examples into a git-initialized repository
// and starts a server with Root set, the shape the write tools require.
func newWriteTestServer(t testing.TB) (*Server, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; write-path tests require it")
	}
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "..", "spec", "examples"))); err != nil {
		t.Fatalf("copy example: %v", err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		{"add", "-A"},
		{"commit", "-q", "-m", "init: freeze baseline"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		t.Fatalf("build index: %v", err)
	}
	ix, err := index.Open(root)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	t.Cleanup(func() { ix.Close() })
	srv, err := New(ix, Options{Version: "test", Root: root})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv, root
}

func TestMemoryAddWritesAndIndexes(t *testing.T) {
	srv, root := newWriteTestServer(t)

	result := callTool(t, srv, "memory_add", map[string]any{
		"type":    "project",
		"title":   "MCP 写入条目",
		"summary": "通过 memory_add 工具写入。",
		"body":    "正文。\n",
		"source":  map[string]any{"agent": "zcode", "kind": "conversation"},
	})
	env := mustMap(t, result["structuredContent"], "structuredContent")
	data := mustMap(t, env["data"], "data")
	id, _ := data["id"].(string)
	if id == "" {
		t.Fatalf("data = %v", data)
	}

	// The write is visible to the SAME server without a restart.
	search := callTool(t, srv, "memory_search", map[string]any{"query": "MCP 写入", "limit": 5})
	senv := mustMap(t, search["structuredContent"], "structuredContent")
	hits, _ := senv["data"].([]any)
	found := false
	for _, h := range hits {
		if m, ok := h.(map[string]any); ok && m["id"] == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("new entry %s not in search: %v", id, hits)
	}

	// The written entry is committed: its directory has no pending change.
	// (.index/ is untracked in this fixture; real repos gitignore it.)
	if out, err := exec.Command("git", "-C", root, "status", "--porcelain", "--", "global/memories").CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "" {
		t.Errorf("entry not committed: %v: %s", err, out)
	}
}

func TestMemoryAddSupersedesAndSearchExcludes(t *testing.T) {
	srv, _ := newWriteTestServer(t)

	first := callTool(t, srv, "memory_add", map[string]any{
		"type": "project", "title": "网关基线", "summary": "第一版。",
		"body": "x\n", "source": map[string]any{"agent": "zcode", "kind": "conversation"},
	})
	oldID := dataID(t, first)
	second := callTool(t, srv, "memory_add", map[string]any{
		"type": "project", "title": "网关基线 v2", "summary": "取代版。",
		"body": "x\n", "source": map[string]any{"agent": "zcode", "kind": "conversation"},
		"supersedes": []string{oldID},
	})
	newID := dataID(t, second)

	search := callTool(t, srv, "memory_search", map[string]any{"query": "网关基线", "limit": 10})
	env := mustMap(t, search["structuredContent"], "structuredContent")
	hits, _ := env["data"].([]any)
	for _, h := range hits {
		if m, ok := h.(map[string]any); ok && m["id"] == oldID {
			t.Fatalf("superseded entry still in default search")
		}
	}

	// Standalone supersede tool on a fresh pair.
	third := callTool(t, srv, "memory_add", map[string]any{
		"type": "project", "title": "网关基线 v3", "summary": "第三版。",
		"body": "x\n", "source": map[string]any{"agent": "zcode", "kind": "conversation"},
	})
	thirdID := dataID(t, third)
	sup := callTool(t, srv, "memory_supersede", map[string]any{"old_id": newID, "by_id": thirdID})
	supEnv := mustMap(t, sup["structuredContent"], "structuredContent")
	if supEnv["error"] != nil {
		t.Fatalf("memory_supersede failed: %v", supEnv["error"])
	}
	sdata := mustMap(t, supEnv["data"], "data")
	if sdata["by_id"] != thirdID {
		t.Fatalf("supersede data = %v", sdata)
	}
}

func TestMemoryAddRejectsInvalidInput(t *testing.T) {
	srv, _ := newWriteTestServer(t)

	bad := callTool(t, srv, "memory_add", map[string]any{
		"type": "project", "title": "标题", "summary": "摘。",
		"body":   "x\n",
		"source": map[string]any{"agent": "zcode", "kind": "chat"},
	})
	if te := toolError(t, bad); te == nil || te.Code != "V-CONTENT-2" {
		t.Fatalf("toolError = %+v, want V-CONTENT-2", te)
	}

	empty := callTool(t, srv, "memory_add", map[string]any{
		"type": "project", "title": "", "summary": "摘。",
		"body": "x\n", "source": map[string]any{"agent": "zcode", "kind": "conversation"},
	})
	if te := toolError(t, empty); te == nil || te.Code != "V-CONTENT-1" {
		t.Fatalf("toolError = %+v, want V-CONTENT-1", te)
	}
}

func TestMemoryAddRequiresRoot(t *testing.T) {
	srv := newTestServer(t)
	result := callTool(t, srv, "memory_add", map[string]any{
		"type": "project", "title": "t", "summary": "s",
		"body": "x\n", "source": map[string]any{"agent": "z", "kind": "manual"},
	})
	if te := toolError(t, result); te == nil || te.Code != "WRITE_UNAVAILABLE" {
		t.Fatalf("toolError = %+v, want WRITE_UNAVAILABLE", te)
	}
}

func dataID(t *testing.T, result map[string]any) string {
	t.Helper()
	env := mustMap(t, result["structuredContent"], "structuredContent")
	data := mustMap(t, env["data"], "data")
	id, _ := data["id"].(string)
	if id == "" {
		t.Fatalf("no id in data: %v", data)
	}
	return id
}
