package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProjectsYAML(t testing.TB, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "projects.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestBriefProjectFromCwd: a cwd matched by projects.yaml resolves the brief's
// project section without the caller naming a node.
func TestBriefProjectFromCwd(t *testing.T) {
	srv, root := newTestServerRoot(t)
	writeProjectsYAML(t, root, "schema: memorytree.projects/v1\nprojects:\n  - path: "+root+"\n    node: work/agentflow\n")

	result := callTool(t, srv, "memory_brief", map[string]any{"task": "agentflow status", "cwd": filepath.Join(root, "cmd")})
	env := mustMap(t, result["structuredContent"], "structuredContent")
	data := mustMap(t, env["data"], "data")
	project := mustMap(t, data["project"], "project")
	if project["path"] != "work/agentflow" {
		t.Fatalf("project path = %v, want work/agentflow", project["path"])
	}
	for _, w := range mustSlice(t, mustMap(t, env["meta"], "meta")["warnings"], "warnings") {
		if s, ok := w.(string); ok && s == "no project was specified; the project overview and project updates are empty" {
			t.Fatalf("resolved cwd must not warn: %v", w)
		}
	}
}

// TestBriefProjectFromEnv: MEMORYTREE_PROJECT resolves the project when the
// caller passes neither project nor cwd.
func TestBriefProjectFromEnv(t *testing.T) {
	srv, _ := newTestServerRoot(t)
	t.Setenv("MEMORYTREE_PROJECT", "work/agentflow")

	result := callTool(t, srv, "memory_brief", map[string]any{"task": "agentflow status"})
	env := mustMap(t, result["structuredContent"], "structuredContent")
	project := mustMap(t, mustMap(t, env["data"], "data")["project"], "project")
	if project["path"] != "work/agentflow" {
		t.Fatalf("project path = %v, want work/agentflow", project["path"])
	}
}

// TestBriefExplicitProjectBeatsEnvAndCwd: an explicit project argument wins
// over MEMORYTREE_PROJECT and a cwd mapping.
func TestBriefExplicitProjectBeatsEnvAndCwd(t *testing.T) {
	srv, root := newTestServerRoot(t)
	writeProjectsYAML(t, root, "schema: memorytree.projects/v1\nprojects:\n  - path: "+root+"\n    node: work/agentflow\n")
	t.Setenv("MEMORYTREE_PROJECT", "work")

	result := callTool(t, srv, "memory_brief", map[string]any{
		"task": "agentflow status", "project": "work/agentflow", "cwd": root,
	})
	env := mustMap(t, result["structuredContent"], "structuredContent")
	project := mustMap(t, mustMap(t, env["data"], "data")["project"], "project")
	if project["path"] != "work/agentflow" {
		t.Fatalf("explicit project must win, got %v", project["path"])
	}
}

// TestBriefUnmatchedCwdWarns: a cwd that matches no entry keeps the
// no-project warning.
func TestBriefUnmatchedCwdWarns(t *testing.T) {
	srv, root := newTestServerRoot(t)
	writeProjectsYAML(t, root, "schema: memorytree.projects/v1\nprojects:\n  - path: "+root+"\n    node: work/agentflow\n")

	result := callTool(t, srv, "memory_brief", map[string]any{"task": "x", "cwd": filepath.Join(root, "..", "elsewhere")})
	env := mustMap(t, result["structuredContent"], "structuredContent")
	warnings := mustSlice(t, mustMap(t, env["meta"], "meta")["warnings"], "warnings")
	found := false
	for _, w := range warnings {
		if s, ok := w.(string); ok && strings.HasPrefix(s, "no project was specified") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unmatched cwd must keep the no-project warning: %v", warnings)
	}
}

// TestBriefInvalidProjectsYAMLWarns: a broken projects.yaml disables cwd
// resolution with a warning instead of failing the brief.
func TestBriefInvalidProjectsYAMLWarns(t *testing.T) {
	srv, root := newTestServerRoot(t)
	writeProjectsYAML(t, root, "schema: wrong/v1\nprojects: []\n")

	result := callTool(t, srv, "memory_brief", map[string]any{"task": "x", "cwd": root})
	env := mustMap(t, result["structuredContent"], "structuredContent")
	warnings := mustSlice(t, mustMap(t, env["meta"], "meta")["warnings"], "warnings")
	found := false
	for _, w := range warnings {
		if s, ok := w.(string); ok && strings.HasPrefix(s, "projects.yaml") {
			found = true
		}
	}
	if !found {
		t.Fatalf("invalid projects.yaml must warn: %v", warnings)
	}
}
