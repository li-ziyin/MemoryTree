package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sseEntryID is the agentflow SSE entry of the example repository.
const sseEntryID = "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b06"

func exampleRepoCopy(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "..", "spec", "examples"))); err != nil {
		t.Fatalf("copy example: %v", err)
	}
	return dir
}

func TestRunSuggestMountRanksNodes(t *testing.T) {
	dir := exampleRepoCopy(t)
	entry := `{"type":"project","title":"SSE 解码吞空格","summary":"前端 SSE 解码按原始字节处理","tags_delta":["sse","frontend"]}`

	var stdout, stderr bytes.Buffer
	code := run([]string{"suggest-mount", dir}, strings.NewReader(entry), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("suggest-mount = %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "work/agentflow") {
		t.Errorf("expected agentflow in output: %s", out)
	}
	if !strings.Contains(out, "shared tags") {
		t.Errorf("expected reasons in output: %s", out)
	}
}

func TestRunSuggestMountJSONAndExclusion(t *testing.T) {
	dir := exampleRepoCopy(t)
	// Excluding the agentflow SSE entry removes it from that node's sample;
	// the agentflow suggestion must then report zero sampled entries.
	entry := `{"type":"project","title":"SSE 解码吞空格","summary":"前端 SSE 解码按原始字节处理","tags_delta":["sse","frontend"]}`

	var stdout, stderr bytes.Buffer
	code := run([]string{"suggest-mount", "--json", "--exclude-entry", sseEntryID, dir},
		strings.NewReader(entry), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("suggest-mount = %d, stderr: %s", code, stderr.String())
	}
	var decoded struct {
		Suggestions []struct {
			Path       string  `json:"path"`
			Score      float64 `json:"score"`
			EntryCount int     `json:"entry_count"`
		} `json:"suggestions"`
		Excluded string `json:"excluded"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode JSON: %v: %s", err, stdout.String())
	}
	if decoded.Excluded != sseEntryID {
		t.Errorf("excluded = %s", decoded.Excluded)
	}
	var agentflow = -1
	for i, sugg := range decoded.Suggestions {
		if sugg.Path == "work/agentflow" {
			agentflow = i
		}
	}
	if agentflow < 0 {
		t.Fatalf("agentflow missing from suggestions: %s", stdout.String())
	}
	if decoded.Suggestions[agentflow].EntryCount != 0 {
		t.Errorf("agentflow entry_count = %d, want 0 after exclusion",
			decoded.Suggestions[agentflow].EntryCount)
	}
}

func TestRunSuggestMountUsageAndInputErrors(t *testing.T) {
	dir := exampleRepoCopy(t)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"suggest-mount"}, strings.NewReader("{}"), &stdout, &stderr); code != 2 {
		t.Errorf("missing data dir = %d, want 2", code)
	}
	if code := run([]string{"suggest-mount", dir}, strings.NewReader(`{"type":"project"}`), &stdout, &stderr); code != 2 {
		t.Errorf("missing title = %d, want 2", code)
	}
	if code := run([]string{"suggest-mount", dir}, strings.NewReader(`not json`), &stdout, &stderr); code != 2 {
		t.Errorf("bad json = %d, want 2", code)
	}
	if code := run([]string{"suggest-mount", dir}, strings.NewReader(`{"type":"wiki","title":"t"}`), &stdout, &stderr); code != 2 {
		t.Errorf("bad type = %d, want 2", code)
	}
}
