// Package inbox stores raw captures before they are distilled into memory
// entries. Captures land in the gitignored .inbox/ directory: no lock, no
// git, no validation — the write gateway's strictness applies only after
// distillation. Captures that carry a session id are keyed by (source,
// session) and overwrite themselves, so repeated end-of-turn hooks leave
// one capture per session instead of one per turn.
package inbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/li-ziyin/memorytree/internal/ids"
	"github.com/li-ziyin/memorytree/internal/redact"
)

// DirName is the reserved capture directory at the repository root.
const DirName = ".inbox"

// Append stores one raw capture and returns its path plus the number of
// credentials redacted from the content before it touched disk. source
// identifies the agent or person that captured the text.
func Append(dataDir, source string, content []byte) (string, int, error) {
	return store(dataDir, source, "", content)
}

// AppendSession stores one raw capture keyed by (source, session): a
// capture for the same pair is overwritten in place — same file, fresh
// content and captured_at — so a client that fires its end-of-turn hook
// every turn (ZCode's Stop) still leaves exactly one capture per session,
// carrying the session's first task and latest outcome. An empty session
// falls back to a plain append.
func AppendSession(dataDir, source, session string, content []byte) (string, int, error) {
	return store(dataDir, source, session, content)
}

// store is the shared capture writer: redact once, then overwrite the
// existing (source, session) capture or create a new file.
func store(dataDir, source, session string, content []byte) (string, int, error) {
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return "", 0, err
	}
	clean, redactions := redact.Scan(string(content))
	content = []byte(clean)
	dir := filepath.Join(root, DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", redactions, err
	}
	name := ""
	if session != "" {
		name = findSessionCapture(dir, source, session)
	}
	if name == "" {
		name = fmt.Sprintf("%s-%s.md", time.Now().UTC().Format("20060102T150405"), ids.NewUUIDv7()[28:36])
	}
	path := filepath.Join(dir, name)
	if err := writeCapture(path, source, session, content); err != nil {
		return "", redactions, err
	}
	return path, redactions, nil
}

// writeCapture writes the note with its frontmatter, atomically when the
// file already exists (a same-session overwrite).
func writeCapture(path, source, session string, content []byte) error {
	note := fmt.Sprintf("---\ncaptured_at: %s\nsource: %s\n", time.Now().UTC().Format(time.RFC3339), source)
	if session != "" {
		note += fmt.Sprintf("session: %s\n", session)
	}
	note += fmt.Sprintf("---\n\n%s", content)
	if _, err := os.Stat(path); err == nil {
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(note), 0o600); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(note)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// findSessionCapture returns the name of the existing capture for exactly
// this source+session pair, or "". Only the frontmatter is inspected, and
// only the frontmatter's own lines match, so body text cannot cause a
// false hit. Inbox files are small; the scan is bounded by the inbox
// itself.
func findSessionCapture(dir, source, session string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if hasFrontmatterPair(raw, "source", source) && hasFrontmatterPair(raw, "session", session) {
			return e.Name()
		}
	}
	return ""
}

// hasFrontmatterPair reports whether the file's frontmatter block carries
// `key: value` as a whole line.
func hasFrontmatterPair(raw []byte, key, value string) bool {
	text := string(raw)
	if !strings.HasPrefix(text, "---\n") {
		return false
	}
	rest, ok := strings.CutPrefix(text, "---\n")
	end := strings.Index(rest, "\n---")
	if !ok || end < 0 {
		return false
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		if line == key+": "+value {
			return true
		}
	}
	return false
}

// List returns the capture file names, oldest first.
func List(dataDir string) ([]string, error) {
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(root, DirName))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
			names = append(names, e.Name())
		}
	}
	return names, nil
}
