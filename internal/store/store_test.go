package store

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/li-ziyin/memorytree/internal/gitio"
	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/initrepo"
)

func validInput() EntryInput {
	return EntryInput{
		Type:    "project",
		Title:   "Docker 端口排障手册",
		Summary: "网关 502 时先查端口映射。",
		Body:    "正文：先 docker compose logs，再核对端口。\n",
		Source:  SourceInput{Agent: "zcode", Kind: "conversation"},
	}
}

// newRepo initializes a data repository that is also a git work tree, the
// shape every real write target has.
func newRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; write-path tests require it")
	}
	root := t.TempDir()
	if _, err := initrepo.Init(initrepo.Options{Root: root, Title: "Test"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		{"add", "-A"},
		{"commit", "-q", "-m", "init: freeze baseline"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return root
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			n++
		}
	}
	return n
}

func TestAddEntryWritesValidFile(t *testing.T) {
	root := newRepo(t)
	in := validInput()
	in.TagsDelta = []string{"docker", "Ops"} // normalized to lowercase, sorted
	res, err := AddEntry(root, in)
	if err != nil {
		t.Fatal(err)
	}
	if res.ID == "" || !strings.HasSuffix(filepath.ToSlash(res.Path), ".md") {
		t.Fatalf("result = %+v", res)
	}
	if !res.Committed || res.Generation < 1 {
		t.Errorf("committed/generation = %v/%d, want true/>=1", res.Committed, res.Generation)
	}
	// The entry is committed to git: the work tree is clean afterwards.
	if out, err := exec.Command("git", "-C", root, "status", "--porcelain").CombinedOutput(); err != nil || len(strings.TrimSpace(string(out))) > 0 {
		t.Errorf("git status not clean: %v: %s", err, out)
	}

	// The written tree must validate and be indexed.
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		t.Fatalf("repository invalid after add: %v", err)
	}
	ix, err := index.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	hits, err := ix.Search(context.Background(), index.SearchOptions{Query: "排障手册"})
	if err != nil || len(hits) != 1 || hits[0].ID != res.ID {
		t.Fatalf("new entry not searchable: %v %+v", err, hits)
	}
	// Effective tags = global node's delta + the entry's normalized delta.
	if got := hits[0].Tags; strings.Join(got, ",") != "docker,global,ops" {
		t.Errorf("tags = %v, want normalized sorted effective [docker global ops]", got)
	}
}

func TestAddEntryDefaultsSensitivityToNode(t *testing.T) {
	root := newRepo(t)
	if _, err := AddEntry(root, validInput()); err != nil {
		t.Fatal(err)
	}
	ix := indexOpen(t, root)
	defer ix.Close()
	hits, err := ix.Search(context.Background(), index.SearchOptions{Query: "排障手册"})
	if err != nil || len(hits) != 1 {
		t.Fatalf("search: %v %+v", err, hits)
	}
	// initrepo marks the global node private; the entry must not be weaker.
	if hits[0].Sensitivity != "private" {
		t.Errorf("sensitivity = %s, want private (host node default)", hits[0].Sensitivity)
	}
}

func TestAddEntryRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name string
		mut  func(in *EntryInput)
		code string
	}{
		{"empty title", func(in *EntryInput) { in.Title = "  " }, "V-CONTENT-1"},
		{"long title", func(in *EntryInput) { in.Title = strings.Repeat("标", 201) }, "V-CONTENT-1"},
		{"empty summary", func(in *EntryInput) { in.Summary = "" }, "V-CONTENT-1"},
		{"long summary", func(in *EntryInput) { in.Summary = strings.Repeat("摘", 501) }, "V-CONTENT-1"},
		{"huge body", func(in *EntryInput) { in.Body = strings.Repeat("x", (1<<20)+1) }, "V-CONTENT-3"},
		{"bad type", func(in *EntryInput) { in.Type = "note" }, "V-ENUM-1"},
		{"bad status", func(in *EntryInput) { in.Status = "archived" }, "V-ENUM-1"},
		{"bad sensitivity", func(in *EntryInput) { in.Sensitivity = "secret" }, "V-ENUM-1"},
		{"weaker sensitivity", func(in *EntryInput) { in.Sensitivity = "public" }, "V-SENS-1"},
		{"tag with space", func(in *EntryInput) { in.TagsDelta = []string{"has space"} }, "V-TAG-1"},
		{"duplicate tag", func(in *EntryInput) { in.TagsDelta = []string{"Docker", "docker"} }, "V-TAG-2"},
		{"missing agent", func(in *EntryInput) { in.Source.Agent = "" }, "V-CONTENT-2"},
		{"missing kind", func(in *EntryInput) { in.Source.Kind = "" }, "V-CONTENT-2"},
		{"absolute ref", func(in *EntryInput) { in.Source.Ref = `C:\Users\x\secret.md` }, "V-CONTENT-2"},
		{"unknown node", func(in *EntryInput) { in.Node = "work/nope" }, "V-NODE-1"},
		{"root node", func(in *EntryInput) { in.Node = "." }, "V-NODE-2"},
		{"supersedes missing", func(in *EntryInput) { in.Supersedes = []string{"019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b99"} }, "V-SUP-1"},
		{"supersedes not uuid", func(in *EntryInput) { in.Supersedes = []string{"not-a-uuid"} }, "V-REF-1"},
		{"draft superseding", func(in *EntryInput) {
			in.Status = "draft"
			in.Supersedes = []string{"019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b05"}
		}, "V-SUP-4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newRepo(t)
			in := validInput()
			tc.mut(&in)
			_, err := AddEntry(root, in)
			ie, ok := err.(*InputError)
			if !ok {
				t.Fatalf("err = %v, want *InputError", err)
			}
			if ie.Code != tc.code {
				t.Errorf("code = %s (%s), want %s", ie.Code, ie.Message, tc.code)
			}
			if n := countFiles(t, filepath.Join(root, "global", "memories")); n != 0 {
				t.Errorf("rejected write left %d file(s) behind", n)
			}
		})
	}
}

func TestAddEntryRequiresGitRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	if _, err := initrepo.Init(initrepo.Options{Root: root, Title: "Test"}); err != nil {
		t.Fatal(err)
	}
	_, err := AddEntry(root, validInput())
	if !errors.Is(err, gitio.ErrNotRepository) {
		t.Fatalf("err = %v, want ErrNotRepository", err)
	}
	if n := countFiles(t, filepath.Join(root, "global", "memories")); n != 0 {
		t.Errorf("rejected write left %d file(s)", n)
	}
}

func TestAddEntrySlugAndCollision(t *testing.T) {
	root := newRepo(t)
	in := validInput()
	in.Title = "Docker Port Playbook" // ASCII slug
	first, err := AddEntry(root, in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := AddEntry(root, in) // same day, same title -> collision
	if err != nil {
		t.Fatal(err)
	}
	firstBase := filepath.Base(first.Path)
	secondBase := filepath.Base(second.Path)
	if !strings.Contains(firstBase, "docker-port-playbook") {
		t.Errorf("first name = %s", firstBase)
	}
	if secondBase == firstBase || !strings.Contains(secondBase, second.ID[28:36]) {
		t.Errorf("collision name = %s (first %s)", secondBase, firstBase)
	}
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		t.Fatalf("repository invalid after two adds: %v", err)
	}
}

func TestAddEntryCleansCrashTempFiles(t *testing.T) {
	root := newRepo(t)
	memDir := filepath.Join(root, "global", "memories")
	if err := os.MkdirAll(memDir, 0o700); err != nil {
		t.Fatal(err)
	}
	leftover := filepath.Join(memDir, "2026-10-27-entry.md.tmp-9999")
	if err := os.WriteFile(leftover, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AddEntry(root, validInput()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Errorf("crash temp file survived: %v", err)
	}
}

func indexOpen(t *testing.T, root string) *index.Index {
	t.Helper()
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}
