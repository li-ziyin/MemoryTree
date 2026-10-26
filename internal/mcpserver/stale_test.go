package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/li-ziyin/memorytree/internal/index"
)

// newTestServerRoot is newTestServerWith for callers that need the
// repository root to mutate files after the server was created.
func newTestServerRoot(t testing.TB) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "..", "spec", "examples"))); err != nil {
		t.Fatalf("copy example: %v", err)
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

func TestStaleIndexWarning(t *testing.T) {
	srv, root := newTestServerRoot(t)

	// Force the throttle open; the repository is unchanged, so no warning.
	srv.lastCheck = time.Now().Add(-time.Minute)
	callTool(t, srv, "memory_search", map[string]any{"query": "SSE"})
	if len(srv.warnings) != 0 {
		t.Fatalf("fresh repository warned: %v", srv.warnings)
	}

	nodePath := filepath.Join(root, "global", "node.md")
	data, err := os.ReadFile(nodePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nodePath, append(data, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.lastCheck = time.Now().Add(-time.Minute)
	result := callTool(t, srv, "memory_search", map[string]any{"query": "SSE"})
	env := mustMap(t, result["structuredContent"], "structuredContent")
	meta := mustMap(t, env["meta"], "meta")
	warnings, ok := meta["warnings"].([]any)
	if !ok {
		t.Fatalf("no warnings in meta: %v", meta)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(fmt.Sprint(w), "source files changed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing stale warning: %v", warnings)
	}
}

func TestFreshServerHasNoStaleWarning(t *testing.T) {
	srv, _ := newTestServerRoot(t)
	if srv.root == "" {
		t.Fatal("newTestServerRoot did not set Root")
	}
	result := callTool(t, srv, "memory_search", map[string]any{"query": "SSE"})
	env := mustMap(t, result["structuredContent"], "structuredContent")
	meta := mustMap(t, env["meta"], "meta")
	if warnings := meta["warnings"].([]any); len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
}
