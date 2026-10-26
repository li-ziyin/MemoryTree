package format

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSplit(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr error
	}{
		{"valid", "---\nid: x\n---\nbody\n", nil},
		{"crlf", "---\r\nid: 1\r\n---\r\n正文", nil},
		{"no first newline", "---", ErrNoFrontmat},
		{"no delimiter", "id: x\n", ErrNoFrontmat},
		{"unterminated", "---\nid: 1\n", ErrNoClosing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Split([]byte(tc.in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestSplitRejectsBOMAndInvalidUTF8(t *testing.T) {
	if _, err := Split([]byte("\xEF\xBB\xBF---\nid: 1\n---\nx")); !errors.Is(err, ErrBOM) {
		t.Fatalf("err = %v, want ErrBOM", err)
	}
	if _, err := Split([]byte("---\nid: \xFF\n---\nx")); !errors.Is(err, ErrNotUTF8) {
		t.Fatalf("err = %v, want ErrNotUTF8", err)
	}
}

func TestSplitSizeLimits(t *testing.T) {
	metaOK := "id: " + strings.Repeat("a", 8) + "\n"
	big := "---\n" + strings.Repeat("m", MaxFrontmatter+1) + "\n---\nx"
	if _, err := Split([]byte(big)); !errors.Is(err, ErrMetaTooBig) {
		t.Fatalf("err = %v, want ErrMetaTooBig", err)
	}
	ok := "---\n" + metaOK + "---\n" + strings.Repeat("b", MaxBody)
	if doc, err := Split([]byte(ok)); err != nil || len(doc.Body) != MaxBody {
		t.Fatalf("doc=%v err=%v", doc, err)
	}
	over := "---\n" + metaOK + "---\n" + strings.Repeat("b", MaxBody+1)
	if _, err := Split([]byte(over)); !errors.Is(err, ErrBodyTooBig) {
		t.Fatalf("err = %v, want ErrBodyTooBig", err)
	}
}

func TestSplitIsIdempotentOnRaw(t *testing.T) {
	data := []byte("---\r\nid: 1\r\n---\r\n正文\r\n")
	doc, err := Split(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Split(doc.Raw)
	if err != nil || string(again.Meta) != string(doc.Meta) || string(again.Body) != string(doc.Body) {
		t.Fatalf("Split(Raw) differs: %+v %v", again, err)
	}
}

func TestReadFileRejectsDirectory(t *testing.T) {
	if _, err := ReadFile(t.TempDir()); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("err = %v, want ErrNotRegular", err)
	}
}

func TestReadFileRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Symlink needs privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real.md")
	if err := os.WriteFile(target, []byte("---\nid: 1\n---\nx"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(link); !errors.Is(err, ErrSymlink) {
		t.Fatalf("err = %v, want ErrSymlink", err)
	}
}

func TestReadPlainRejectsBOMAndInvalidUTF8(t *testing.T) {
	dir := t.TempDir()
	bomPath := filepath.Join(dir, "bom.yaml")
	if err := os.WriteFile(bomPath, []byte("\xEF\xBB\xBFkey: v"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPlain(bomPath); !errors.Is(err, ErrBOM) {
		t.Fatalf("err = %v, want ErrBOM", err)
	}
	badPath := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(badPath, []byte("key: \xFF"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPlain(badPath); !errors.Is(err, ErrNotUTF8) {
		t.Fatalf("err = %v, want ErrNotUTF8", err)
	}
}

func TestDecode(t *testing.T) {
	var out map[string]any
	if err := Decode([]byte("id: x\ncount: 3\n"), &out); err != nil {
		t.Fatalf("valid mapping rejected: %v", err)
	}
	if out["id"] != "x" {
		t.Fatalf("decoded %v", out)
	}
}

func TestDecodeSafetyRules(t *testing.T) {
	cases := []struct {
		name string
		meta string
		want error
	}{
		{"empty", " \n", ErrEmptyMeta},
		{"multi doc", "a: 1\n---\nb: 2\n", ErrMultiDoc},
		{"sequence root", "- a\n- b\n", ErrNotMapping},
		{"scalar root", "just text\n", ErrNotMapping},
		{"alias", "a: &x 1\nb: *x\n", ErrYAMLUnsafe},
		{"anchor", "a: &x 1\n", ErrYAMLUnsafe},
		{"custom tag", "a: !swap 1\n", ErrYAMLUnsafe},
		{"duplicate key", "a: 1\na: 2\n", ErrYAML},
		{"complex key", "? [1, 2]\n: v\n", ErrYAMLUnsafe},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out map[string]any
			err := Decode([]byte(tc.meta), &out)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
