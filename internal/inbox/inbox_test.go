package inbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendStoresCaptureWithHeader(t *testing.T) {
	root := t.TempDir()
	path, err := Append(root, "zcode", []byte("用户偏好先定方案再动手。"))
	if err != nil {
		t.Fatal(err)
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
		path, err := Append(root, "zcode", []byte("x"))
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
