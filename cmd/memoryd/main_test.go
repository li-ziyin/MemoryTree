package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/initrepo"
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
