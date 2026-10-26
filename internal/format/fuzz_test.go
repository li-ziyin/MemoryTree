package format

import (
	"bytes"
	"testing"
	"unicode/utf8"
)

func FuzzSplit(f *testing.F) {
	seeds := [][]byte{
		[]byte("---\nid: x\n---\nbody\n"),
		[]byte("---\r\nid: 1\r\n---\r\n正文"),
		[]byte("\xEF\xBB\xBF---\nid: 1\n---\nx"),
		[]byte("---\nunclosed"),
		[]byte("---\n" + string(bytes.Repeat([]byte("m"), MaxFrontmatter+1)) + "\n---\nx"),
		[]byte("网络 端口 \xF0\x9F\x8C\xB2"),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := Split(data)
		if err != nil {
			return
		}
		if !utf8.Valid(data) {
			t.Fatal("Split accepted invalid UTF-8")
		}
		if len(doc.Meta) > MaxFrontmatter || len(doc.Body) > MaxBody {
			t.Fatalf("limits violated: meta=%d body=%d", len(doc.Meta), len(doc.Body))
		}
		again, err := Split(doc.Raw)
		if err != nil || !bytes.Equal(again.Meta, doc.Meta) || !bytes.Equal(again.Body, doc.Body) {
			t.Fatal("Split is not idempotent on Raw")
		}
	})
}

func FuzzDecode(f *testing.F) {
	f.Add([]byte("id: 1\ntags: [a, b]\n"))
	f.Add([]byte("a: &x 1\nb: *x"))
	f.Add([]byte("网络: 端口\n"))
	f.Add([]byte("---\n"))
	f.Fuzz(func(t *testing.T, meta []byte) {
		var first map[string]any
		err1 := Decode(meta, &first)
		var second map[string]any
		err2 := Decode(meta, &second)
		if (err1 == nil) != (err2 == nil) {
			t.Fatalf("Decode not deterministic: %v vs %v", err1, err2)
		}
	})
}
