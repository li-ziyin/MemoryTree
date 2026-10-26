package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	userEntryID = "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b05"
	sseEntryID  = "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b06"
	globalNode  = "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b02"
	sseNode     = "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b04"
)

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatalf("copy example: %v", err)
	}
}

func exampleCopy(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	copyDir(t, filepath.Join("..", "..", "spec", "examples"), dst)
	return dst
}

func mustBuild(t *testing.T, root string, opts BuildOptions) *BuildResult {
	t.Helper()
	res, err := Build(root, opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return res
}

func mustOpen(t *testing.T, root string) *Index {
	t.Helper()
	ix, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return ix
}

func search(t *testing.T, root string, opts SearchOptions) []Hit {
	t.Helper()
	ix := mustOpen(t, root)
	defer ix.Close()
	hits, err := ix.Search(context.Background(), opts)
	if err != nil {
		t.Fatalf("Search(%+v): %v", opts, err)
	}
	return hits
}

func edit(t *testing.T, file, old, new string) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s does not contain %q", file, old)
	}
	data = []byte(strings.Replace(string(data), old, new, 1))
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestBuildExampleAndUpToDate(t *testing.T) {
	root := exampleCopy(t)

	res := mustBuild(t, root, BuildOptions{})
	if !res.Rebuilt || res.UpToDate {
		t.Fatalf("first build: rebuilt=%v up_to_date=%v, want true/false", res.Rebuilt, res.UpToDate)
	}
	if res.Generation != 1 {
		t.Errorf("generation = %d, want 1", res.Generation)
	}
	if res.Nodes != 4 || res.Entries != 2 {
		t.Errorf("counts = %d nodes, %d entries; want 4 and 2", res.Nodes, res.Entries)
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("index file: %v", err)
	}

	ix := mustOpen(t, root)
	meta, err := ix.Meta()
	ix.Close()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if meta.Generation != 1 || meta.Nodes != 4 || meta.Entries != 2 {
		t.Errorf("meta = %+v", meta)
	}
	if meta.SchemaVersion != schemaVersion {
		t.Errorf("schema version = %d, want %d", meta.SchemaVersion, schemaVersion)
	}

	again := mustBuild(t, root, BuildOptions{})
	if !again.UpToDate || again.Rebuilt {
		t.Fatalf("second build: rebuilt=%v up_to_date=%v, want false/true", again.Rebuilt, again.UpToDate)
	}
	if again.Generation != 1 {
		t.Errorf("generation after no-op = %d, want 1", again.Generation)
	}
}

func TestRebuildIncrementsGeneration(t *testing.T) {
	root := exampleCopy(t)
	mustBuild(t, root, BuildOptions{})

	res := mustBuild(t, root, BuildOptions{Rebuild: true})
	if !res.Rebuilt || res.UpToDate {
		t.Fatalf("rebuild: rebuilt=%v up_to_date=%v", res.Rebuilt, res.UpToDate)
	}
	if res.Generation != 2 {
		t.Errorf("generation = %d, want 2", res.Generation)
	}

	ix := mustOpen(t, root)
	meta, err := ix.Meta()
	ix.Close()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if meta.Generation != 2 {
		t.Errorf("meta generation = %d, want 2", meta.Generation)
	}
}

func TestSearchFTSAndShortQueryFallback(t *testing.T) {
	root := exampleCopy(t)
	mustBuild(t, root, BuildOptions{})

	tests := []struct {
		name  string
		query string
		want  string
	}{
		{name: "three rune Chinese uses trigram", query: "开发者", want: userEntryID},
		{name: "ASCII trigram", query: "SSE", want: sseEntryID},
		{name: "two rune Chinese uses rune-split FTS", query: "解码", want: sseEntryID},
		{name: "two rune Chinese in title", query: "用户", want: userEntryID},
		{name: "mixed short token uses rune-split FTS", query: "SSE 解码", want: sseEntryID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits := search(t, root, SearchOptions{Query: tt.query})
			if len(hits) != 1 {
				t.Fatalf("query %q returned %d hits, want 1: %+v", tt.query, len(hits), hits)
			}
			if hits[0].ID != tt.want {
				t.Errorf("query %q returned %s, want %s", tt.query, hits[0].ID, tt.want)
			}
		})
	}

	if hits := search(t, root, SearchOptions{Query: "不存在的记忆"}); len(hits) != 0 {
		t.Errorf("unmatched query returned %d hits", len(hits))
	}
	if hits := search(t, root, SearchOptions{Query: "100%"}); len(hits) != 0 {
		t.Errorf("LIKE wildcard was not escaped: %+v", hits)
	}

	all := search(t, root, SearchOptions{})
	if len(all) != 2 {
		t.Errorf("empty query returned %d hits, want 2", len(all))
	}
	limited := search(t, root, SearchOptions{Limit: 1})
	if len(limited) != 1 {
		t.Errorf("limit 1 returned %d hits", len(limited))
	}
}

func TestSearchFilters(t *testing.T) {
	root := exampleCopy(t)
	mustBuild(t, root, BuildOptions{})

	tests := []struct {
		name string
		opts SearchOptions
		want int
	}{
		{name: "node", opts: SearchOptions{NodeID: globalNode}, want: 1},
		{name: "type user", opts: SearchOptions{Types: []string{"user"}}, want: 1},
		{name: "type project", opts: SearchOptions{Types: []string{"project"}}, want: 1},
		{name: "effective tag", opts: SearchOptions{Tags: []string{"work"}}, want: 1},
		{name: "short query with tag", opts: SearchOptions{Query: "解码", Tags: []string{"work"}}, want: 1},
		{name: "two effective tags", opts: SearchOptions{Tags: []string{"work", "sse"}}, want: 1},
		{name: "missing tag", opts: SearchOptions{Tags: []string{"nope"}}, want: 0},
		{name: "draft excluded by default", opts: SearchOptions{Statuses: []string{"draft"}}, want: 0},
		{name: "node and query disagree", opts: SearchOptions{NodeID: globalNode, Query: "SSE"}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if hits := search(t, root, tt.opts); len(hits) != tt.want {
				t.Fatalf("returned %d hits, want %d: %+v", len(hits), tt.want, hits)
			}
		})
	}

	hits := search(t, root, SearchOptions{Query: "SSE"})
	if len(hits) != 1 {
		t.Fatalf("SSE hits = %d", len(hits))
	}
	wantTags := []string{"agent", "frontend", "java", "spring", "sse", "work"}
	if strings.Join(hits[0].Tags, ",") != strings.Join(wantTags, ",") {
		t.Errorf("effective tags = %v, want %v", hits[0].Tags, wantTags)
	}
	if hits[0].NodePath != "work/agentflow" {
		t.Errorf("node path = %q", hits[0].NodePath)
	}

	sseFile := filepath.Join(root, "work", "agentflow", "memories", "2026-10-20-sse-pitfall.md")
	edit(t, sseFile, "status: active", "status: draft")
	mustBuild(t, root, BuildOptions{})

	if hits := search(t, root, SearchOptions{}); len(hits) != 1 || hits[0].ID != userEntryID {
		t.Errorf("default search should exclude draft: %+v", hits)
	}
	if hits := search(t, root, SearchOptions{Statuses: []string{"draft"}}); len(hits) != 1 || hits[0].ID != sseEntryID {
		t.Errorf("draft search: %+v", hits)
	}
}

func TestInvalidRepositoryLeavesIndexUntouched(t *testing.T) {
	root := exampleCopy(t)
	res := mustBuild(t, root, BuildOptions{})
	before := fileHash(t, res.Path)

	entry := filepath.Join(root, "work", "agentflow", "memories", "2026-10-20-sse-pitfall.md")
	edit(t, entry, "title: SSE 解码坑", `title: ""`)

	invalid, err := Build(root, BuildOptions{})
	if !errors.Is(err, ErrInvalidRepository) {
		t.Fatalf("Build error = %v, want ErrInvalidRepository", err)
	}
	if invalid == nil || invalid.Validation == nil || invalid.Validation.OK() {
		t.Fatalf("validation report missing or unexpectedly clean")
	}
	if after := fileHash(t, res.Path); after != before {
		t.Errorf("index file changed after a failed validation")
	}
}

func TestFingerprintChangeRebuilds(t *testing.T) {
	root := exampleCopy(t)
	first := mustBuild(t, root, BuildOptions{})
	ix := mustOpen(t, root)
	metaBefore, err := ix.Meta()
	ix.Close()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}

	entry := filepath.Join(root, "work", "agentflow", "memories", "2026-10-20-sse-pitfall.md")
	edit(t, entry, "按原始字节解码。", "按原始字节解码，并实现幂等重连。")

	res := mustBuild(t, root, BuildOptions{})
	if !res.Rebuilt || res.UpToDate || res.Generation != first.Generation+1 {
		t.Fatalf("changed source: rebuilt=%v up_to_date=%v generation=%d", res.Rebuilt, res.UpToDate, res.Generation)
	}

	ix = mustOpen(t, root)
	metaAfter, err := ix.Meta()
	ix.Close()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if metaBefore.Fingerprint == metaAfter.Fingerprint {
		t.Errorf("fingerprint did not change")
	}
	if hits := search(t, root, SearchOptions{Query: "幂等重连"}); len(hits) != 1 || hits[0].ID != sseEntryID {
		t.Errorf("new body text not searchable: %+v", hits)
	}

	again := mustBuild(t, root, BuildOptions{})
	if !again.UpToDate || again.Generation != res.Generation {
		t.Errorf("unchanged source after rebuild: %+v", again)
	}
}

func TestCorruptIndexRequiresRebuild(t *testing.T) {
	root := exampleCopy(t)
	first := mustBuild(t, root, BuildOptions{})

	if err := removeIndexFiles(first.Path); err != nil {
		t.Fatalf("remove index: %v", err)
	}
	garbage := []byte("this is not a sqlite database")
	if err := os.WriteFile(first.Path, garbage, 0o600); err != nil {
		t.Fatalf("write corrupt index: %v", err)
	}
	corruptHash := fileHash(t, first.Path)

	if _, err := Build(root, BuildOptions{}); !errors.Is(err, ErrIndexCorrupt) {
		t.Fatalf("Build error = %v, want ErrIndexCorrupt", err)
	}
	if after := fileHash(t, first.Path); after != corruptHash {
		t.Errorf("corrupt index was modified without --rebuild")
	}
	if _, err := Open(root); !errors.Is(err, ErrIndexCorrupt) {
		t.Fatalf("Open error = %v, want ErrIndexCorrupt", err)
	}

	res := mustBuild(t, root, BuildOptions{Rebuild: true})
	if res.Generation != 1 || res.Entries != 2 {
		t.Errorf("rebuilt index = %+v", res)
	}
	if hits := search(t, root, SearchOptions{Query: "SSE"}); len(hits) != 1 {
		t.Errorf("rebuilt index is not searchable: %+v", hits)
	}
}

func TestIncompatibleSchemaRequiresRebuild(t *testing.T) {
	root := exampleCopy(t)
	first := mustBuild(t, root, BuildOptions{})

	db, err := openDB(first.Path, false)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	if _, err := db.Exec(`UPDATE meta SET meta_value = '999' WHERE meta_key = 'schema_version'`); err != nil {
		db.Close()
		t.Fatalf("rewrite schema version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close index: %v", err)
	}

	if _, err := Open(root); !errors.Is(err, ErrIndexIncompatible) {
		t.Fatalf("Open error = %v, want ErrIndexIncompatible", err)
	}
	if _, err := Build(root, BuildOptions{}); !errors.Is(err, ErrIndexIncompatible) {
		t.Fatalf("Build error = %v, want ErrIndexIncompatible", err)
	}

	res := mustBuild(t, root, BuildOptions{Rebuild: true})
	if res.Generation != first.Generation+1 {
		t.Errorf("generation = %d, want %d", res.Generation, first.Generation+1)
	}
	if hits := search(t, root, SearchOptions{Query: "SSE"}); len(hits) != 1 {
		t.Errorf("rebuilt index is not searchable: %+v", hits)
	}
}

func TestShortQueryRanksTitleThenSummaryThenBody(t *testing.T) {
	root := exampleCopy(t)
	write := func(filename, id, title, summary, body, date string) {
		t.Helper()
		content := fmt.Sprintf(`---
schema: memorytree.entry/v1
id: %s
node: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b02
type: user
status: active
sensitivity: private
title: %s
summary: %s
tags_delta: []
source:
  agent: test
  kind: manual
supersedes: []
superseded_by: null
related: []
created_at: "%sT00:00:00Z"
updated_at: "%sT00:00:00Z"
---

%s
`, id, title, summary, date, date, body)
		if err := os.WriteFile(filepath.Join(root, "global", "memories", filename), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", filename, err)
		}
	}

	write("2026-10-22-summary-note.md", "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b07",
		"summary-note", "与解码有关的摘要。", "正文不含关键词。", "2026-10-22")
	write("2026-10-23-body-note.md", "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b08",
		"body-note", "一条普通记录。", "正文顺带提到解码一次。", "2026-10-23")

	mustBuild(t, root, BuildOptions{})

	hits := search(t, root, SearchOptions{Query: "解码"})
	want := []string{sseEntryID, "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b07", "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b08"}
	if len(hits) != len(want) {
		t.Fatalf("query returned %d hits, want %d: %+v", len(hits), len(want), hits)
	}
	for i, id := range want {
		if hits[i].ID != id {
			t.Errorf("hit %d = %s, want %s", i, hits[i].ID, id)
		}
	}
}

func TestOpenWithoutIndex(t *testing.T) {
	root := exampleCopy(t)
	if _, err := Open(root); !errors.Is(err, ErrNoIndex) {
		t.Fatalf("Open error = %v, want ErrNoIndex", err)
	}
}
