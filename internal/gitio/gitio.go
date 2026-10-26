// Package gitio wraps the system git executable for the MemoryTree write
// path. Git is a hard dependency for writes: the Markdown tree is the
// source of truth and git provides the audit trail around it.
package gitio

import (
	"errors"
	"fmt"
	"os/exec"
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

// Commit stages exactly the given repository-relative paths and commits
// them with message. Pathspec commits keep unrelated staged work out of
// memoryd's commits.
func Commit(root string, relPaths []string, message string) error {
	args := append([]string{"-C", root, "add", "--"}, relPaths...)
	if out, err := run(args); err != nil {
		return fmt.Errorf("git add %v: %w: %s", relPaths, err, out)
	}
	args = append([]string{"-C", root, "commit", "-m", message, "--"}, relPaths...)
	if out, err := run(args); err != nil {
		return fmt.Errorf("git commit: %w: %s", err, out)
	}
	return nil
}

func run(args []string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
