package legacy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/li-ziyin/memorytree/internal/format"
	"github.com/li-ziyin/memorytree/internal/initrepo"
	"github.com/li-ziyin/memorytree/internal/model"
	"github.com/li-ziyin/memorytree/internal/validate"
)

func writeLegacy(t *testing.T, dir, name, body string) {
	t.Helper()
	content := "---\n" +
		"name: " + name + "\n" +
		"description: 旧记忆的说明。\n" +
		"metadata:\n" +
		"  node_type: memory\n" +
		"  type: project\n" +
		"  originSessionId: sess_test\n" +
		"---\n" +
		body
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestImportLegacy(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	if _, err := initrepo.Init(initrepo.Options{Root: data}); err != nil {
		t.Fatalf("init: %v", err)
	}

	src := t.TempDir()
	writeLegacy(t, src, "2026-10-01-alpha", "# Alpha 状态\n\n正文。\n")
	writeLegacy(t, src, "2026-10-02-beta", "# Beta 状态\n\n正文。\n")
	if err := os.WriteFile(filepath.Join(src, "MEMORY.md"), []byte("# Index\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stats, err := Import(Options{From: src, To: data, Agent: "zcode"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if stats.Imported != 2 {
		t.Errorf("imported = %d, want 2", stats.Imported)
	}

	res, err := validate.Validate(data)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.OK() {
		t.Fatalf("imported repository is invalid: %+v", res.Issues)
	}
	if res.Nodes != 4 || res.Entries != 2 {
		t.Errorf("nodes/entries = %d/%d, want 4/2", res.Nodes, res.Entries)
	}

	entries, err := filepath.Glob(filepath.Join(data, "legacy", "shared-memory", "memories", "*.md"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("entry files: %v, err = %v", entries, err)
	}
	doc, err := format.ReadFile(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	var e model.Entry
	if err := format.Decode(doc.Meta, &e); err != nil {
		t.Fatal(err)
	}
	if e.Type != model.TypeProject || e.Source.Agent != "zcode" || e.Source.Session != "sess_test" {
		t.Errorf("unexpected entry metadata: %+v", e)
	}

	if _, err := Import(Options{From: src, To: data}); !errors.Is(err, ErrAlreadyImported) {
		t.Fatalf("second import err = %v, want ErrAlreadyImported", err)
	}
}
