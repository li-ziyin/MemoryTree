// Package inbox stores raw captures before they are distilled into memory
// entries. Captures land in the gitignored .inbox/ directory: no lock, no
// git, no validation — the write gateway's strictness applies only after
// distillation.
package inbox

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/li-ziyin/memorytree/internal/ids"
)

// DirName is the reserved capture directory at the repository root.
const DirName = ".inbox"

// Append stores one raw capture and returns its path. source identifies the
// agent or person that captured the text.
func Append(dataDir, source string, content []byte) (string, error) {
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%s.md", time.Now().UTC().Format("20060102T150405"), ids.NewUUIDv7()[28:36])
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	note := fmt.Sprintf("---\ncaptured_at: %s\nsource: %s\n---\n\n%s",
		time.Now().UTC().Format(time.RFC3339), source, content)
	_, werr := f.WriteString(note)
	cerr := f.Close()
	if werr != nil {
		return "", werr
	}
	if cerr != nil {
		return "", cerr
	}
	return path, nil
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
