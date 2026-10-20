package initrepo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/li-ziyin/memorytree/internal/validate"
)

func TestInitCreatesValidRepository(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	got, err := Init(Options{Root: root, Title: "Test Vault"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if got != root {
		t.Errorf("root = %q, want %q", got, root)
	}
	for _, rel := range []string{"memory.yaml", "node.md", "global/node.md", "global/memories", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}

	res, err := validate.Validate(root)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.OK() {
		t.Fatalf("fresh repository is invalid: %+v", res.Issues)
	}
	if res.Nodes != 2 || res.Entries != 0 {
		t.Errorf("nodes/entries = %d/%d, want 2/0", res.Nodes, res.Entries)
	}
}

func TestInitRefusesNonEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Init(Options{Root: root})
	if !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("err = %v, want ErrNotEmpty", err)
	}
}
