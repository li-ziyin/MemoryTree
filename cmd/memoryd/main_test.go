package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
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
