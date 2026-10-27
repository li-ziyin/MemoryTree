package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The hook entry point end to end: a Claude-protocol Stop payload on stdin
// lands as one redacted inbox capture, transcript summary included.
func TestRunCaptureSessionFromHookPayload(t *testing.T) {
	data := t.TempDir()
	transcript := filepath.Join(filepath.Dir(data), "transcript.jsonl")
	body := `{"type":"user","message":{"role":"user","content":"帮我把 sync 文档补上"}}
{"type":"assistant","message":{"role":"assistant","content":"文档已补齐。token: ghp_000000000000000000000000000000000000"}}
`
	if err := os.WriteFile(transcript, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := `{"session_id":"sess-42","transcript_path":` + quoteJSON(transcript) + `,"cwd":"E:\\proj","hook_event_name":"Stop"}`

	var stdout, stderr bytes.Buffer
	code := run([]string{"capture-session", "--source", "zcode", data}, strings.NewReader(payload), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr.String())
	}

	names, err := filepath.Glob(filepath.Join(data, ".inbox", "*.md"))
	if err != nil || len(names) != 1 {
		t.Fatalf("inbox captures = %d (err %v)", len(names), err)
	}
	raw, err := os.ReadFile(names[0])
	if err != nil {
		t.Fatal(err)
	}
	capture := string(raw)
	for _, want := range []string{"source: zcode", "帮我把 sync 文档补上", "文档已补齐"} {
		if !strings.Contains(capture, want) {
			t.Errorf("capture missing %q:\n%s", want, capture)
		}
	}
	if strings.Contains(capture, "ghp_") {
		t.Errorf("secret survived into the capture:\n%s", capture)
	}
	if !strings.Contains(capture, "[REDACTED]") {
		t.Errorf("redaction marker missing:\n%s", capture)
	}
	if !strings.Contains(stdout.String(), "captured session sess-42") {
		t.Errorf("stdout = %s", stdout.String())
	}
}

// Flags win over the stdin payload, and a metadata-only payload still
// captures (the "no manual action" acceptance path).
func TestRunCaptureSessionFlagsAndMetadataFallback(t *testing.T) {
	data := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"capture-session", "--source", "opencode",
		"--session", "s-7", "--cwd", "E:\\work", "--event", "session.idle",
		data,
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr.String())
	}
	names, err := filepath.Glob(filepath.Join(data, ".inbox", "*.md"))
	if err != nil || len(names) != 1 {
		t.Fatalf("inbox captures = %d (err %v)", len(names), err)
	}
	raw, _ := os.ReadFile(names[0])
	for _, want := range []string{"opencode", "session.idle", "s-7", "(no transcript available)"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("capture missing %q:\n%s", want, raw)
		}
	}
}

func TestRunCaptureSessionUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"capture-session"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

// A zcode capture without a usable transcript still gets its task from the
// durable rollout log, resolved by session id under the real home directory.
func TestRunCaptureSessionZCodeRolloutFallback(t *testing.T) {
	home := t.TempDir()
	for _, v := range []string{"USERPROFILE", "HOME"} {
		t.Setenv(v, home)
	}
	rolloutDir := filepath.Join(home, ".zcode", "cli", "rollout")
	if err := os.MkdirAll(rolloutDir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"request":{"body":{"messages":[{"role":"user","content":[{"type":"text","text":"<system-reminder>ctx</system-reminder>"},{"type":"text","text":"接着推进捕获功能"}]}]}},"response":{"text":""}}
{"request":{},"response":{"text":"rollout 回退已实现并验证。"}}
`
	// Real ids carry the session_ prefix, so the log is model-io-<id>.jsonl.
	if err := os.WriteFile(filepath.Join(rolloutDir, "model-io-session_9.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := `{"session_id":"session_9","cwd":"E:\\MemoryTree","hook_event_name":"Stop"}`

	data := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"capture-session", "--source", "zcode", data}, strings.NewReader(payload), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr.String())
	}
	names, err := filepath.Glob(filepath.Join(data, ".inbox", "*.md"))
	if err != nil || len(names) != 1 {
		t.Fatalf("inbox captures = %d (err %v)", len(names), err)
	}
	raw, _ := os.ReadFile(names[0])
	for _, want := range []string{"session: session_9", "接着推进捕获功能", "rollout 回退已实现并验证。"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("capture missing %q:\n%s", want, raw)
		}
	}
	if strings.Contains(string(raw), "<system-reminder>") {
		t.Errorf("injected context leaked:\n%s", raw)
	}
}

func TestZCodeRolloutPathGuards(t *testing.T) {
	for _, v := range []string{"USERPROFILE", "HOME"} {
		t.Setenv(v, t.TempDir())
	}
	if got := zcodeRolloutPath("opencode", "session_1"); got != "" {
		t.Errorf("non-zcode source should skip the rollout, got %q", got)
	}
	if got := zcodeRolloutPath("zcode", ""); got != "" {
		t.Errorf("empty session should skip the rollout, got %q", got)
	}
	if got := zcodeRolloutPath("zcode", `../evil`); got != "" {
		t.Errorf("path-traversal session should be rejected, got %q", got)
	}
	wantSuffix := filepath.Join(".zcode", "cli", "rollout", "model-io-session_1.jsonl")
	if got := zcodeRolloutPath("zcode", "session_1"); !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("rollout path = %q, want suffix %q", got, wantSuffix)
	}
}

func quoteJSON(s string) string {
	var b bytes.Buffer
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
