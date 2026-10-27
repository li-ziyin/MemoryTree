package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/li-ziyin/memorytree/internal/gitio"
	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/validate"
)

// DoctorFinding is one managed file that differs from HEAD.
type DoctorFinding struct {
	// Code is the git porcelain status, e.g. "A ", " M", " D", "??".
	Code string `json:"code"`
	// Path is the repository-relative POSIX path.
	Path string `json:"path"`
}

// DoctorReport describes what doctor found and, in fix mode, what it did.
type DoctorReport struct {
	Root string `json:"root"`
	// Dirty lists managed files (memory.yaml, node.md, memories/*.md) that
	// are modified, added, or deleted relative to HEAD.
	Dirty []DoctorFinding `json:"dirty,omitempty"`
	// TempFiles lists crash leftovers from interrupted atomic writes.
	TempFiles []string `json:"temp_files,omitempty"`
	// OtherUntracked counts untracked files outside the managed set; doctor
	// leaves them alone.
	OtherUntracked int `json:"other_untracked,omitempty"`
	// Validation carries the blocking issues that stopped a fix from
	// committing; nil when the tree validates clean.
	Validation *validate.Result `json:"validation,omitempty"`
	// FixedTemp is the number of temp leftovers removed in fix mode.
	FixedTemp int `json:"fixed_temp,omitempty"`
	// Committed reports that fix mode created a recovery commit.
	Committed bool `json:"committed"`
	// CommitMessage names the recovery commit when Committed is set.
	CommitMessage string `json:"commit_message,omitempty"`
	// Aborted explains why a requested fix did not commit.
	Aborted string `json:"aborted,omitempty"`
}

// NeedsAttention reports whether findings remain that the caller must act on.
func (r *DoctorReport) NeedsAttention() bool {
	return len(r.Dirty) > 0 && !r.Committed || r.Aborted != ""
}

// Doctor inspects a data repository for crash leftovers of the write path:
// files written but never committed, uncommitted deletions, and interrupted
// atomic writes. With fix it removes temp leftovers and, after the tree
// validates clean, commits exactly the dirty managed paths under the
// single-writer lock — data is never discarded and nothing is reset.
func Doctor(dataDir string, fix bool) (*DoctorReport, error) {
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if err := gitio.Preflight(root); err != nil {
		return nil, err
	}

	rep := &DoctorReport{Root: root}
	temps, err := findTempFiles(root)
	if err != nil {
		return nil, err
	}
	rep.TempFiles = temps

	entries, err := gitio.Status(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if isManagedPath(e.Path) {
			rep.Dirty = append(rep.Dirty, DoctorFinding{Code: e.Code, Path: e.Path})
			continue
		}
		if e.Code == "??" {
			rep.OtherUntracked++
		}
	}

	if !fix {
		return rep, nil
	}

	lock, err := acquireWriteLock(root)
	if err != nil {
		return nil, err
	}
	defer lock.release()

	for _, rel := range rep.TempFiles {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		rep.FixedTemp++
	}
	if len(rep.Dirty) == 0 {
		return rep, nil
	}

	res, _, err := validate.ValidateWithSnapshot(root)
	if err != nil {
		return nil, err
	}
	if !res.OK() {
		rep.Validation = res
		rep.Aborted = fmt.Sprintf(
			"the tree has %d validation error(s); fix or revert the files below manually, then rerun doctor --fix; nothing was committed or discarded",
			res.Errors())
		return rep, nil
	}

	paths := make([]string, 0, len(rep.Dirty))
	for _, f := range rep.Dirty {
		paths = append(paths, f.Path)
	}
	rep.CommitMessage = fmt.Sprintf("memoryd: doctor recovery (%d paths)", len(paths))
	// Mirror finishWrite: a stale or corrupt index does not block the
	// recovery commit. A validation failure cannot occur here — the tree
	// was just validated under the write lock.
	_, _ = index.Build(root, index.BuildOptions{})
	if err := gitio.Commit(root, paths, rep.CommitMessage); err != nil {
		return nil, fmt.Errorf(
			"recovery commit failed: %v; the files are intact; recover with: git -C %s add %s && git -C %s commit -m %q",
			err, root, strings.Join(paths, " "), root, rep.CommitMessage)
	}
	rep.Committed = true
	return rep, nil
}

// findTempFiles walks the repository for leftover temp files from
// interrupted atomic writes, returning repository-relative POSIX paths.
func findTempFiles(root string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == ".git" && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		i := strings.Index(name, tmpInfix)
		if i <= 0 {
			return nil
		}
		final := name[:i]
		if strings.HasSuffix(final, ".md") || strings.HasSuffix(final, ".yaml") {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

// isManagedPath reports whether rel is a file the format manages and git
// history must cover: memory.yaml, any node.md, or any entry under memories/.
func isManagedPath(rel string) bool {
	if rel == "memory.yaml" {
		return true
	}
	if rel == "node.md" || strings.HasSuffix(rel, "/node.md") {
		return true
	}
	if strings.HasSuffix(rel, ".md") && strings.Contains(rel, "/memories/") {
		return true
	}
	return false
}
