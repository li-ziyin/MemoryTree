package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/initrepo"
	"github.com/li-ziyin/memorytree/internal/scalegen"
)

func TestRunMCPBuildsIndexAndServes(t *testing.T) {
	dir := t.TempDir()
	if _, err := initrepo.Init(initrepo.Options{Root: dir, Title: "Test"}); err != nil {
		t.Fatalf("init repo: %v", err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n"

	var stdout, stderr bytes.Buffer
	code := run([]string{"mcp", dir}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run mcp = %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"name":"memoryd"`) {
		t.Errorf("MCP output missing server info: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"memory_brief"`) {
		t.Errorf("MCP output missing tools: %s", stdout.String())
	}
	if _, err := os.Stat(index.Path(dir)); err != nil {
		t.Errorf("startup did not build the index: %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr: %s", stderr.String())
	}
}

func TestRunMCPRejectsInvalidArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"mcp"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run mcp without a data dir = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage: memoryd mcp") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"no args", nil},
		{"unknown command", []string{"frobnicate"}},
		{"init missing dir", []string{"init"}},
		{"validate missing dir", []string{"validate"}},
		{"import-legacy missing flags", []string{"import-legacy"}},
		{"index missing dir", []string{"index"}},
		{"mcp missing dir", []string{"mcp"}},
		{"add missing dir", []string{"add"}},
		{"supersede missing flags", []string{"supersede"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tc.args, strings.NewReader(""), &stdout, &stderr); code != 2 {
				t.Fatalf("code = %d, want 2; stderr: %s", code, stderr.String())
			}
		})
	}
}

func TestRunValidateAndIndexExitCodes(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	if code := run([]string{"init", dir}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init code = %d, stderr: %s", code, stderr.String())
	}
	if code := run([]string{"validate", dir}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("validate code = %d, stderr: %s", code, stderr.String())
	}
	stdout.Reset()
	if code := run([]string{"index", "--json", dir}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("index code = %d, stderr: %s", code, stderr.String())
	}
	if !json.Valid(bytes.TrimSpace(stdout.Bytes())) {
		t.Fatalf("index --json emitted invalid JSON: %s", stdout.String())
	}

	// Corrupt one managed file: validation errors must exit 1, and index
	// must refuse and leave the previous index untouched.
	nodePath := filepath.Join(dir, "global", "node.md")
	if err := os.WriteFile(nodePath, []byte("not frontmatter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"validate", dir}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("corrupt validate code = %d, want 1", code)
	}
	if code := run([]string{"index", dir}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("corrupt index code = %d, want 1", code)
	}
}

func TestRunBenchExitCodes(t *testing.T) {
	dir := t.TempDir()
	if err := scalegen.Generate(dir, 150); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"index", dir}, strings.NewReader(""), io.Discard, &bytes.Buffer{}); code != 0 {
		t.Fatalf("index code = %d", code)
	}
	ix, err := index.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := ix.Search(nil, index.SearchOptions{Query: "金丝雀 needle-0"})
	if err != nil || len(hits) == 0 {
		t.Fatalf("no gold entry: %v", err)
	}
	gold := hits[0].ID
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}

	good := filepath.Join(t.TempDir(), "q.jsonl")
	goodLine := `{"id":1,"query":"金丝雀 needle-0","expect_id":"` + gold + `"}` + "\n"
	if err := os.WriteFile(good, []byte(goodLine), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"bench", dir, good}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("bench good code = %d, want 0", code)
	}

	bad := filepath.Join(t.TempDir(), "q.jsonl")
	badLine := `{"id":1,"query":"金丝雀 needle-0","expect_id":"019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b99"}` + "\n"
	if err := os.WriteFile(bad, []byte(badLine), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"bench", dir, bad}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); code != 1 {
		t.Fatalf("bench bad code = %d, want 1", code)
	}

	if code := run([]string{"bench", dir}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Fatalf("bench usage code = %d, want 2", code)
	}
}

// writeRepo initializes a data repository with a frozen git baseline, the
// shape the write path requires.
func writeRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; write-path tests require it")
	}
	dir := t.TempDir()
	if _, err := initrepo.Init(initrepo.Options{Root: dir, Title: "Test"}); err != nil {
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
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

const addJSON = `{
  "type": "project",
  "title": "CLI 写入条目",
  "summary": "通过 memoryd add 写入的条目。",
  "body": "正文由 stdin JSON 提供。\n",
  "tags_delta": ["cli"],
  "source": {"agent": "zcode", "kind": "conversation"}
}`

func TestRunAddAndSupersede(t *testing.T) {
	dir := writeRepo(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"add", dir}, strings.NewReader(addJSON), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("add code = %d, stderr: %s", code, stderr.String())
	}
	var first struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &first); err != nil || first.ID == "" {
		t.Fatalf("add stdout = %s (%v)", stdout.String(), err)
	}

	// The entry is committed and visible through the read path.
	if out, err := exec.Command("git", "-C", dir, "status", "--porcelain").CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "" {
		t.Errorf("git status not clean: %v: %s", err, out)
	}
	if code := run([]string{"validate", dir}, strings.NewReader(""), io.Discard, &bytes.Buffer{}); code != 0 {
		t.Fatalf("validate after add = %d", code)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"add", dir}, strings.NewReader(`{"type":"project","title":"CLI 写入条目 v2","summary":"取代版。","body":"x\n","source":{"agent":"zcode","kind":"conversation"},"supersedes":["`+first.ID+`"]}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("add v2 code = %d, stderr: %s", code, stderr.String())
	}
	var second struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &second); err != nil || second.ID == "" {
		t.Fatalf("add v2 stdout = %s (%v)", stdout.String(), err)
	}

	// A third entry exercises the standalone supersede command.
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"add", dir}, strings.NewReader(`{"type":"project","title":"独立条目","summary":"用来测 supersede 命令。","body":"x\n","source":{"agent":"zcode","kind":"conversation"}}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("add third code = %d, stderr: %s", code, stderr.String())
	}
	var third struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &third); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"supersede", "--old", second.ID, "--by", third.ID, dir}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("supersede code = %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), third.ID) {
		t.Fatalf("supersede stdout = %s", stdout.String())
	}
	if code := run([]string{"validate", dir}, strings.NewReader(""), io.Discard, &bytes.Buffer{}); code != 0 {
		t.Fatalf("validate after supersede = %d", code)
	}
}

func TestRunAddUsageAndValidation(t *testing.T) {
	dir := writeRepo(t)

	if code := run([]string{"add"}, strings.NewReader(addJSON), io.Discard, &bytes.Buffer{}); code != 2 {
		t.Fatalf("add usage code = %d, want 2", code)
	}
	if code := run([]string{"add", dir}, strings.NewReader("{not json"), io.Discard, &bytes.Buffer{}); code != 2 {
		t.Fatalf("add bad json code = %d, want 2", code)
	}
	var stderr bytes.Buffer
	if code := run([]string{"add", dir}, strings.NewReader(`{"type":"project","title":"","summary":"空标题。","body":"x","source":{"agent":"z","kind":"c"}}`), io.Discard, &stderr); code != 1 {
		t.Fatalf("add invalid input code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "V-CONTENT-1") {
		t.Errorf("stderr = %s", stderr.String())
	}
	if code := run([]string{"supersede", "--old", "x", dir}, strings.NewReader(""), io.Discard, &bytes.Buffer{}); code != 2 {
		t.Fatalf("supersede usage code = %d, want 2", code)
	}
}
