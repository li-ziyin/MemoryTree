// Package capture turns a client hook payload into a bounded session
// summary for the inbox: the session's first user request and last assistant
// outcome, when the client exposes a transcript, plus always-safe metadata.
// Everything passes through the inbox's redaction before it touches disk.
package capture

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits keep one capture small no matter how large the transcript was.
const (
	// maxTranscriptBytes bounds how much of a transcript file is read; the
	// interesting messages (first task, final outcome) sit at either end,
	// so a head slice plus a tail slice covers them.
	maxTranscriptBytes = 4 << 20
	maxPartChars       = 4000
)

// Input is what a hook provides, merged from flags and the hook's stdin.
type Input struct {
	Source         string // client name recorded as the capture source
	Session        string
	Cwd            string
	Event          string
	TranscriptPath string
}

// Build renders the capture markdown.
func Build(in Input, now time.Time) []byte {
	task, outcome := extractTranscript(in.TranscriptPath)
	var b strings.Builder
	fmt.Fprintf(&b, "# Session capture (%s)\n\n", orUnknown(in.Source))
	fmt.Fprintf(&b, "- ended: %s\n", now.UTC().Format(time.RFC3339))
	if in.Event != "" {
		fmt.Fprintf(&b, "- event: %s\n", in.Event)
	}
	fmt.Fprintf(&b, "- session: %s\n", orUnknown(cleanTemplateVar(in.Session)))
	fmt.Fprintf(&b, "- cwd: %s\n", orUnknown(cleanTemplateVar(in.Cwd)))
	fmt.Fprintf(&b, "\n## Task\n\n%s\n\n## Outcome\n\n%s\n", task, outcome)
	return []byte(b.String())
}

// extractTranscript pulls the first user text and the last assistant text
// out of a JSONL transcript in the shape Claude-protocol clients write:
// one JSON object per line, user/assistant messages under message.content
// as either a string or an array of {type,text} parts. Missing or
// unreadable transcripts degrade to placeholders, never an error — a
// metadata-only capture is still a valid signal.
func extractTranscript(path string) (task, outcome string) {
	task, outcome = "(no transcript available)", "(no transcript available)"
	if path == "" {
		return task, outcome
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return task, outcome
	}
	head, tail := splitHeadTail(raw, maxTranscriptBytes)

	var firstUser, lastAssistant string
	scan := func(chunk []byte) {
		for _, line := range strings.Split(string(chunk), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var msg struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Message *struct {
					Role    string          `json:"role"`
					Content json.RawMessage `json:"content"`
				} `json:"message"`
				Content json.RawMessage `json:"content"`
			}
			if json.Unmarshal([]byte(line), &msg) != nil {
				continue
			}
			role := msg.Role
			if role == "" && msg.Message != nil {
				role = msg.Message.Role
			}
			kind := msg.Type
			if kind == "" {
				kind = role
			}
			content := msg.Content
			if msg.Message != nil && len(msg.Message.Content) > 0 {
				content = msg.Message.Content
			}
			text := contentText(content)
			if utf8.RuneCountInString(text) == 0 {
				continue
			}
			switch kind {
			case "user":
				if firstUser == "" {
					firstUser = text
				}
			case "assistant":
				lastAssistant = text
			}
		}
	}
	scan(head)
	scan(tail)
	if firstUser != "" {
		task = truncateRunes(firstUser, maxPartChars)
	}
	if lastAssistant != "" {
		outcome = truncateRunes(lastAssistant, maxPartChars)
	}
	return task, outcome
}

// contentText decodes a message content field that is either a plain string
// or an array of parts with type/text.
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Text != "" && p.Type != "tool_use" && p.Type != "tool_result" {
			b.WriteString(p.Text)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// splitHeadTail keeps the first and last n/2 bytes of raw when it exceeds
// n, so both the opening task and the closing outcome survive the cap.
func splitHeadTail(raw []byte, n int) (head, tail []byte) {
	if len(raw) <= n {
		return raw, nil
	}
	half := n / 2
	head, tail = raw[:half], raw[len(raw)-half:]
	// Do not start a chunk mid-line.
	if i := strings.IndexByte(string(tail), '\n'); i >= 0 {
		tail = tail[i+1:]
	}
	return head, tail
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:max])) + " …"
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return s
}

// cleanTemplateVar treats an unexpanded hook template variable (a client
// that did not define it) as absent.
func cleanTemplateVar(s string) string {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "${") && strings.HasSuffix(t, "}") {
		return ""
	}
	return t
}
