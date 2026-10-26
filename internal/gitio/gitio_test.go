package gitio

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

func gitRepo(t *testing.T) string {
	t.Helper()
	if !gitAvailable() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return root
}

func TestPreflightAndCommit(t *testing.T) {
	root := gitRepo(t)
	if err := Preflight(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Commit(root, []string{"a.md"}, "test: add a"); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", root, "log", "--oneline", "-1").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "test: add a") {
		t.Fatalf("log = %s", out)
	}
}

func TestPreflightRejectsPlainDirectory(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git is not installed")
	}
	if err := Preflight(t.TempDir()); !errors.Is(err, ErrNotRepository) {
		t.Fatalf("err = %v, want ErrNotRepository", err)
	}
}

func TestCommitOnlyTouchesPathspec(t *testing.T) {
	root := gitRepo(t)
	other := filepath.Join(root, "other.md")
	mine := filepath.Join(root, "mine.md")
	if err := os.WriteFile(other, []byte("user work"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("memoryd work"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Commit(root, []string{"mine.md"}, "memoryd: add mine"); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", root, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "?? other.md" {
		t.Fatalf("status = %q, want only other.md untracked", out)
	}
}
