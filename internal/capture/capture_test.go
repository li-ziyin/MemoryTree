package capture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func now() time.Time { return time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC) }

func transcript(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBuildExtractsTaskAndOutcome(t *testing.T) {
	p := transcript(t,
		`{"type":"user","message":{"role":"user","content":"帮我把导出功能补上测试"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"导出测试已补齐，共 6 个用例。"}]}}`,
	)
	out := string(Build(Input{Source: "zcode", Session: "s-1", Cwd: "E:\\proj", TranscriptPath: p}, now()))
	for _, want := range []string{
		"# Session capture (zcode)",
		"- ended: 2026-10-27T12:00:00Z",
		"- session: s-1",
		"- cwd: E:\\proj",
		"帮我把导出功能补上测试",
		"导出测试已补齐，共 6 个用例。",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("capture missing %q:\n%s", want, out)
		}
	}
}

func TestBuildUsesLastAssistantNotFirst(t *testing.T) {
	p := transcript(t,
		`{"type":"assistant","message":{"role":"assistant","content":"中间一次失败的尝试"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":"最终结论：用 trigram"}}`,
	)
	out := string(Build(Input{Source: "opencode", TranscriptPath: p}, now()))
	if strings.Contains(out, "中间一次失败的尝试") {
		t.Errorf("kept an intermediate assistant message:\n%s", out)
	}
	if !strings.Contains(out, "最终结论：用 trigram") {
		t.Errorf("missing final outcome:\n%s", out)
	}
}

// Tool chatter in transcripts is not summary material.
func TestBuildSkipsToolParts(t *testing.T) {
	p := transcript(t,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","text":"file contents"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"真实用户请求"}]}}`,
	)
	out := string(Build(Input{Source: "zcode", TranscriptPath: p}, now()))
	if strings.Contains(out, "file contents") {
		t.Errorf("tool_result leaked into task:\n%s", out)
	}
	if !strings.Contains(out, "真实用户请求") {
		t.Errorf("real user text missing:\n%s", out)
	}
}

func TestBuildWithoutTranscriptDegradesToMetadata(t *testing.T) {
	out := string(Build(Input{Source: "zcode", Session: "${CLAUDE_SESSION_ID}", Cwd: ""}, now()))
	for _, want := range []string{"(no transcript available)", "session: unknown", "cwd: unknown"} {
		if !strings.Contains(out, want) {
			t.Errorf("capture missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "CLAUDE_SESSION_ID") {
		t.Errorf("unexpanded template variable kept:\n%s", out)
	}
}

func TestBuildTailsHugeTranscripts(t *testing.T) {
	var lines []string
	for i := 0; i < 20000; i++ {
		lines = append(lines, `{"type":"assistant","message":{"role":"assistant","content":"filler line "+`+string(rune('a'+i%26))+`" padded to a decent length for the cap test"}`)
	}
	lines = append(lines, `{"type":"assistant","message":{"role":"assistant","content":"最后的结论"}}`)
	p := transcript(t, lines...)
	out := string(Build(Input{Source: "zcode", TranscriptPath: p}, now()))
	if !strings.Contains(out, "最后的结论") {
		t.Errorf("tail outcome lost under the cap:\n%.200s…", out)
	}
}
