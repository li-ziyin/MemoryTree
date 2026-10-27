// Package gitio wraps the system git executable for the MemoryTree write
// path. Git is a hard dependency for writes: the Markdown tree is the
// source of truth and git provides the audit trail around it.
package gitio

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	// ErrGitMissing means git is not on PATH; the repository stays usable
	// read-only.
	ErrGitMissing = errors.New("git is not installed or not on PATH")
	// ErrNotRepository means the data directory is not a git work tree.
	ErrNotRepository = errors.New("data directory is not a git repository")
)

// Preflight verifies that writes can be committed: git must be installed
// and the data directory must be a git work tree.
func Preflight(root string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return ErrGitMissing
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Output()
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNotRepository, root)
	}
	if strings.TrimSpace(string(out)) != "true" {
		return fmt.Errorf("%w: %s", ErrNotRepository, root)
	}
	return nil
}

// hardeningArgs are prepended to every git invocation so a hostile data
// repository cannot execute code through its own config: hooks are pointed
// at a directory that never exists (an empty value would fall back to the
// default hooks directory), and fsmonitor probes are disabled. Filter
// drivers configured by the repository remain possible only for an attacker
// who already controls the local repository, which equals the invoker's
// own privileges.
var hardeningArgs = []string{
	"-c", "core.hooksPath=./.memoryd-no-hooks",
	"-c", "core.fsmonitor=false",
}

// Commit stages exactly the given repository-relative paths and commits
// them with message. Pathspec commits keep unrelated staged work out of
// memoryd's commits. Only paths still present on disk are staged (new and
// modified files); deletions need no staging, because the pathspec commit
// below records them from the work tree against both HEAD and the index —
// including deletions that were already staged ahead of HEAD by an
// interrupted manual recovery.
func Commit(root string, relPaths []string, message string) error {
	var onDisk []string
	for _, rel := range relPaths {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			onDisk = append(onDisk, rel)
		}
	}
	if len(onDisk) > 0 {
		args := append([]string{"-C", root, "add", "-A", "--"}, onDisk...)
		if out, err := run(args); err != nil {
			return fmt.Errorf("git add %v: %w: %s", onDisk, err, out)
		}
	}
	args := append([]string{"-C", root, "commit", "-m", message, "--"}, relPaths...)
	if out, err := run(args); err != nil {
		return fmt.Errorf("git commit: %w: %s", err, out)
	}
	return nil
}

// StatusEntry is one work-tree change reported by Status.
type StatusEntry struct {
	// Code is the two-character porcelain status, e.g. "M ", " M", " D", "??".
	Code string
	// Path is the repository-relative POSIX path of the changed file.
	Path string
}

// Status lists every work-tree change (staged and unstaged, untracked
// included) without resolving renames, so each entry maps to exactly one
// path. It runs the same hardened git invocation as Commit. The raw output
// is kept untouched: porcelain records may begin with a space, which the
// trimmed helper used elsewhere would strip.
func Status(root string) ([]StatusEntry, error) {
	cmd := exec.Command("git", append(append([]string{}, hardeningArgs...),
		"-C", root, "status", "--porcelain", "-z", "--no-renames")...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("git status: %w: %s", err, ee.Stderr)
		}
		return nil, fmt.Errorf("git status: %w", err)
	}
	var entries []StatusEntry
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		if len(rec) < 4 || rec[2] != ' ' {
			return nil, fmt.Errorf("git status: unparseable record %q", rec)
		}
		entries = append(entries, StatusEntry{Code: rec[:2], Path: rec[3:]})
	}
	return entries, nil
}

func run(args []string) (string, error) {
	cmd := exec.Command("git", append(append([]string{}, hardeningArgs...), args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
