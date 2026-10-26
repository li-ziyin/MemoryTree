package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/li-ziyin/memorytree/internal/docfile"
	"github.com/li-ziyin/memorytree/internal/model"
)

// tmpSuffix marks in-flight writes from a crashed process. Such files are
// not managed by format v1 and are ignored by the validator; every write
// removes leftovers in the same directory.
const tmpInfix = ".tmp-"

// writeEntryFile writes the entry frontmatter plus body into memDir with an
// atomic rename and returns the final path. The directory is created when
// missing; crash leftovers are removed first.
func writeEntryFile(memDir string, entry *model.Entry, body []byte) (string, error) {
	if err := os.MkdirAll(memDir, 0o700); err != nil {
		return "", err
	}
	if err := cleanTempFiles(memDir); err != nil {
		return "", err
	}
	taken, err := takenNames(memDir)
	if err != nil {
		return "", err
	}
	name := entryFileName(entry, entry.CreatedAt, taken)
	final := filepath.Join(memDir, name)
	data, err := docfile.MarshalFrontmatter(entry, body)
	if err != nil {
		return "", err
	}
	if err := writeFileAtomic(final, data); err != nil {
		return "", err
	}
	return final, nil
}

// writeFileAtomic writes data to a temp file next to path and renames it
// into place, so readers never observe a partial file.
func writeFileAtomic(path string, data []byte) error {
	tmp := fmt.Sprintf("%s%s%d", path, tmpInfix, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func cleanTempFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.Contains(e.Name(), tmpInfix) {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func takenNames(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	taken := make(map[string]bool, len(entries))
	for _, e := range entries {
		taken[e.Name()] = true
	}
	return taken, nil
}
