package inbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendStoresCaptureWithHeader(t *testing.T) {
	root := t.TempDir()
	path, redactions, err := Append(root, "zcode", []byte("用户偏好先定方案再动手。"))
	if err != nil {
		t.Fatal(err)
	}
	if redactions != 0 {
		t.Errorf("redactions = %d on clean text, want 0", redactions)
	}
	if filepath.Dir(path) != filepath.Join(root, DirName) {
		t.Fatalf("path = %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"captured_at: ", "source: zcode", "用户偏好先定方案再动手。"} {
		if !strings.Contains(text, want) {
			t.Errorf("capture missing %q:\n%s", want, text)
		}
	}
	names, err := List(root)
	if err != nil || len(names) != 1 || names[0] != filepath.Base(path) {
		t.Fatalf("List = %v (%v)", names, err)
	}
}

func TestAppendNamesNeverCollide(t *testing.T) {
	root := t.TempDir()
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		path, _, err := Append(root, "zcode", []byte("x"))
		if err != nil {
			t.Fatal(err)
		}
		base := filepath.Base(path)
		if seen[base] {
			t.Fatalf("duplicate capture name %s", base)
		}
		seen[base] = true
	}
}

// Session captures overwrite in place: repeated end-of-turn hooks for one
// session leave exactly one file with the latest content, while other
// sessions and sources keep their own files.
func TestAppendSessionOverwritesSameSession(t *testing.T) {
	root := t.TempDir()
	first, _, err := AppendSession(root, "zcode", "sess-1", []byte("第一轮的结果"))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := AppendSession(root, "zcode", "sess-1", []byte("最终轮的结果"))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("same session wrote two files: %s vs %s", first, second)
	}
	raw, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "第一轮的结果") || !strings.Contains(text, "最终轮的结果") {
		t.Errorf("overwrite did not replace content:\n%s", text)
	}
	if !strings.Contains(text, "session: sess-1") {
		t.Errorf("session missing from frontmatter:\n%s", text)
	}
	if count := strings.Count(text, "source: zcode"); count != 1 {
		t.Errorf("frontmatter written %d times", count)
	}

	// A different session and a different source each get their own file.
	if _, _, err := AppendSession(root, "zcode", "sess-2", []byte("另一会话")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AppendSession(root, "opencode", "sess-1", []byte("另一客户端")); err != nil {
		t.Fatal(err)
	}
	names, err := List(root)
	if err != nil || len(names) != 3 {
		t.Fatalf("captures = %v (%v), want 3 files", names, err)
	}
}

// An empty session id behaves like a plain append; body text can never be
// mistaken for a session marker.
func TestAppendSessionWithoutSessionAppends(t *testing.T) {
	root := t.TempDir()
	trick := []byte("---\nsource: zcode\nsession: ghost\n---\n\n正文伪装成 frontmatter")
	for i := 0; i < 2; i++ {
		if _, _, err := AppendSession(root, "zcode", "", trick); err != nil {
			t.Fatal(err)
		}
	}
	names, err := List(root)
	if err != nil || len(names) != 2 {
		t.Fatalf("captures = %v (%v), want 2 files", names, err)
	}
}
