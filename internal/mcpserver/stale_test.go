package mcpserver

import (
	"os"
	"path/filepath"
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

// TestAdoptsExternallyRefreshedIndex covers the multi-process setup: another
// memoryd process changed the sources and already refreshed the shared index.
// The running server must adopt the new generation and fingerprint silently —
// queries already serve current data through the shared SQLite file.
func TestAdoptsExternallyRefreshedIndex(t *testing.T) {
	srv, root := newTestServerRoot(t)
	if srv.generation != 1 {
		t.Fatalf("expected startup generation 1, got %d", srv.generation)
	}

	nodePath := filepath.Join(root, "global", "node.md")
	data, err := os.ReadFile(nodePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nodePath, append(data, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	// The "other process" writes and refreshes the shared index.
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		t.Fatalf("external build: %v", err)
	}

	srv.lastCheck = time.Now().Add(-time.Minute)
	result := callTool(t, srv, "memory_search", map[string]any{"query": "SSE"})
	env := mustMap(t, result["structuredContent"], "structuredContent")
	meta := mustMap(t, env["meta"], "meta")
	if gen := meta["index_generation"]; gen != float64(2) {
		t.Fatalf("expected adopted generation 2, got %v", gen)
	}
	if warnings := meta["warnings"].([]any); len(warnings) != 0 {
		t.Fatalf("adoption must not warn: %v", warnings)
	}
}

// TestSelfHealsAfterHandEdit covers hand edits that no other process indexed:
// the server rebuilds the index itself instead of demanding a restart.
func TestSelfHealsAfterHandEdit(t *testing.T) {
	srv, root := newTestServerRoot(t)

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
	if gen := meta["index_generation"]; gen != float64(2) {
		t.Fatalf("expected self-healed generation 2, got %v", gen)
	}
	if warnings := meta["warnings"].([]any); len(warnings) != 0 {
		t.Fatalf("self-heal must not warn: %v", warnings)
	}
}

// TestStaleWarningClearsAfterHeal checks that a latched staleness warning is
// removed once the repository validates again and the index catches up.
func TestStaleWarningClearsAfterHeal(t *testing.T) {
	srv, root := newTestServerRoot(t)

	// Break the repository: unparseable frontmatter in the root node.
	nodePath := filepath.Join(root, "node.md")
	original, err := os.ReadFile(nodePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nodePath, []byte("not: [valid frontmatter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.lastCheck = time.Now().Add(-time.Minute)
	callTool(t, srv, "memory_search", map[string]any{"query": "SSE"})
	if len(srv.warnings) == 0 {
		t.Fatal("expected a warning while the repository is broken")
	}

	// Repair the node with different valid content; the fingerprint drifts
	// and the server must self-heal and clear the warning.
	repaired := append([]byte(nil), original...)
	repaired = append(repaired, '\n')
	if err := os.WriteFile(nodePath, repaired, 0o600); err != nil {
		t.Fatal(err)
	}
	srv.lastCheck = time.Now().Add(-time.Minute)
	result := callTool(t, srv, "memory_search", map[string]any{"query": "SSE"})
	env := mustMap(t, result["structuredContent"], "structuredContent")
	meta := mustMap(t, env["meta"], "meta")
	if warnings := meta["warnings"].([]any); len(warnings) != 0 {
		t.Fatalf("warning must clear after heal: %v", warnings)
	}
	if gen := meta["index_generation"]; gen != float64(2) {
		t.Fatalf("expected healed generation 2, got %v", gen)
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
