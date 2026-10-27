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
