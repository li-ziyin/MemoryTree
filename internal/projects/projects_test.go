package projects

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t testing.TB, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissing(t *testing.T) {
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("missing file must not error: %v", err)
	}
	if m.Len() != 0 {
		t.Fatalf("expected empty map, got %d entries", m.Len())
	}
	if _, ok := m.Match(filepath.Join(t.TempDir(), "any")); ok {
		t.Fatal("empty map must not match")
	}
}

func TestLoadAndLongestPrefixMatch(t *testing.T) {
	base := t.TempDir()
	writeFile(t, base, "schema: memorytree.projects/v1\nprojects:\n  - path: "+filepath.Join(base, "work")+"\n    node: work\n  - path: "+filepath.Join(base, "work", "agentflow")+"\n    node: work/agentflow\n")
	m, err := Load(base)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", m.Len())
	}
	cases := []struct {
		dir  string
		want string
		ok   bool
	}{
		{filepath.Join(base, "work", "agentflow"), "work/agentflow", true},
		{filepath.Join(base, "work", "agentflow", "cmd"), "work/agentflow", true},
		{filepath.Join(base, "work", "other"), "work", true},
		{filepath.Join(base, "work"), "work", true},
		{filepath.Join(base, "workx"), "", false},
		{filepath.Join(base, "elsewhere", "deep"), "", false},
	}
	for _, tc := range cases {
		got, ok := m.Match(tc.dir)
		if ok != tc.ok || got != tc.want {
			t.Errorf("Match(%q) = (%q, %v), want (%q, %v)", tc.dir, got, ok, tc.want, tc.ok)
		}
	}
}

func TestMatchExpandsHomeInQuery(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	base := t.TempDir()
	writeFile(t, base, "schema: memorytree.projects/v1\nprojects:\n  - path: "+filepath.Join("~", "work")+"\n    node: work\n")
	m, err := Load(base)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got, ok := m.Match(filepath.Join("~", "work", "sub")); !ok || got != "work" {
		t.Fatalf("tilde query = (%q, %v), want (work, true)", got, ok)
	}
	if _, ok := m.Match(filepath.Join(home, "elsewhere")); ok {
		t.Fatal("unrelated home path must not match")
	}
}

func TestMatchCaseInsensitiveOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only case-insensitivity")
	}
	base := t.TempDir()
	writeFile(t, base, "schema: memorytree.projects/v1\nprojects:\n  - path: "+filepath.Join(base, "Work")+"\n    node: work\n")
	m, err := Load(base)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	lower := filepath.Join(base, "work", "sub")
	if got, ok := m.Match(lower); !ok || got != "work" {
		t.Fatalf("case-differing query = (%q, %v), want (work, true)", got, ok)
	}
}

func TestLoadRejectsBadSchema(t *testing.T) {
	base := t.TempDir()
	writeFile(t, base, "schema: something.else/v1\nprojects: []\n")
	if _, err := Load(base); err == nil {
		t.Fatal("bad schema must error")
	}
}

func TestLoadRejectsDuplicates(t *testing.T) {
	base := t.TempDir()
	p := filepath.Join(base, "work")
	writeFile(t, base, "schema: memorytree.projects/v1\nprojects:\n  - path: "+p+"\n    node: a\n  - path: "+p+"\n    node: b\n")
	if _, err := Load(base); err == nil {
		t.Fatal("duplicate paths must error")
	}
}

func TestLoadRejectsRelativePath(t *testing.T) {
	base := t.TempDir()
	writeFile(t, base, "schema: memorytree.projects/v1\nprojects:\n  - path: relative/path\n    node: work\n")
	if _, err := Load(base); err == nil {
		t.Fatal("relative path must error")
	}
}

func TestLoadRejectsEmptyNode(t *testing.T) {
	base := t.TempDir()
	writeFile(t, base, "schema: memorytree.projects/v1\nprojects:\n  - path: "+filepath.Join(base, "work")+"\n")
	if _, err := Load(base); err == nil {
		t.Fatal("missing node must error")
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	base := t.TempDir()
	writeFile(t, base, "schema: memorytree.projects/v1\nproject:\n  - path: "+filepath.Join(base, "work")+"\n    node: work\n")
	if _, err := Load(base); err == nil {
		t.Fatal("unknown top-level key must error")
	}
	writeFile(t, base, "schema: memorytree.projects/v1\nprojects:\n  - paths: "+filepath.Join(base, "work")+"\n    node: work\n")
	if _, err := Load(base); err == nil {
		t.Fatal("unknown entry key must error")
	}
}
