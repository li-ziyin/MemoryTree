package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture copies the example repository and marks the global subtree
// public while the work subtree stays private, giving every level
// something to include and something to filter.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "..", "spec", "examples"))); err != nil {
		t.Fatalf("copy example: %v", err)
	}
	for _, rel := range []string{
		"node.md", // the root gates the subtree: relax it first
		filepath.Join("global", "node.md"),
		filepath.Join("global", "memories", "2026-10-20-user-profile.md"),
	} {
		p := filepath.Join(root, rel)
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out := strings.Replace(string(data), "sensitivity: private", "sensitivity: public", 1)
		if out == string(data) {
			t.Fatalf("no sensitivity field to relax in %s", rel)
		}
		if err := os.WriteFile(p, []byte(out), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestExportFiltersBySensitivity(t *testing.T) {
	root := fixture(t)
	out := filepath.Join(t.TempDir(), "share")

	report, err := Export(root, out, "public")
	if err != nil {
		t.Fatal(err)
	}
	// The public global subtree exports; the private work subtree does not.
	for _, rel := range []string{
		"memory.yaml",
		filepath.Join("global", "node.md"),
		filepath.Join("global", "memories", "2026-10-20-user-profile.md"),
		"EXPORT-NOTES.md",
	} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "work")); !os.IsNotExist(err) {
		t.Errorf("private work subtree leaked into the export: %v", err)
	}
	if report.Entries != 1 || report.NodesSkipped == 0 {
		t.Errorf("report = %+v, want 1 entry and skipped nodes", report)
	}
	notes, err := os.ReadFile(filepath.Join(out, "EXPORT-NOTES.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(notes), "public") || !strings.Contains(string(notes), "share view") {
		t.Errorf("notes = %s", notes)
	}
}

func TestExportPrivateIncludesEverything(t *testing.T) {
	root := fixture(t)
	out := filepath.Join(t.TempDir(), "full")
	report, err := Export(root, out, "private")
	if err != nil {
		t.Fatal(err)
	}
	if report.NodesSkipped != 0 || report.EntriesSkipped != 0 {
		t.Errorf("private export skipped something: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(out, "work", "agentflow", "node.md")); err != nil {
		t.Errorf("work/agentflow missing: %v", err)
	}
}

func TestExportRefusesDirtyTargetAndBadLevel(t *testing.T) {
	root := fixture(t)
	busy := filepath.Join(t.TempDir(), "busy")
	if err := os.MkdirAll(filepath.Join(busy, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(busy, "x", "keep.txt"), []byte("user data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(root, busy, "internal"); err == nil {
		t.Fatal("export accepted a non-empty target directory")
	}
	if _, err := os.Stat(filepath.Join(busy, "x", "keep.txt")); err != nil {
		t.Fatalf("export touched the busy target: %v", err)
	}
	if _, err := Export(root, filepath.Join(t.TempDir(), "o"), "secret"); err == nil {
		t.Fatal("export accepted an unknown level")
	}
}
