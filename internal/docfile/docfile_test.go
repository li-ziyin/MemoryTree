package docfile

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/li-ziyin/memorytree/internal/format"
)

type meta struct {
	ID      string   `yaml:"id"`
	Title   string   `yaml:"title"`
	Tags    []string `yaml:"tags_delta"`
	Private bool     `yaml:"private"`
}

var sample = meta{
	ID:      "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b05",
	Title:   "标题 docker",
	Tags:    []string{"a", "b"},
	Private: true,
}

func TestMarshalIsStable(t *testing.T) {
	first, err := Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("Marshal not stable:\n%q\n%q", first, second)
	}
}

func TestFrontmatterRoundTrip(t *testing.T) {
	data, err := MarshalFrontmatter(sample, []byte("正文\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := format.Split(data)
	if err != nil {
		t.Fatalf("output is not a valid frontmatter file: %v", err)
	}
	var back meta
	if err := format.Decode(doc.Meta, &back); err != nil {
		t.Fatalf("output fails the format safety rules: %v", err)
	}
	if !reflect.DeepEqual(back, sample) {
		t.Fatalf("round trip changed %v", back)
	}
	if string(doc.Body) != "正文\n" {
		t.Fatalf("body = %q", doc.Body)
	}
}

func TestWriteFrontmatterPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are ACL-driven on Windows")
	}
	path := filepath.Join(t.TempDir(), "entry.md")
	if err := WriteFrontmatter(path, sample, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %o, want 600", info.Mode().Perm())
	}
}
