package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/li-ziyin/memorytree/internal/initrepo"
)

// newGitRepo initializes a data repository that is also a git work tree.
func newGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	if _, err := initrepo.Init(initrepo.Options{Root: root, Title: "Test"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		{"add", "-A"},
		{"commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return root
}

func TestRunGardenerDataAndApply(t *testing.T) {
	root := newGitRepo(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"gardener", "data", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("gardener data = %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"guidelines"`) || !strings.Contains(stdout.String(), `"path": "global"`) {
		t.Errorf("data output missing nodes or guidelines: %s", stdout.String()[:300])
	}

	proposal := `{"author":"test","reason":"seed","operations":[
		{"op":"create_node","target":".","kind":"category","slug":"work","title":"工作","summary":"职业","tags_delta":["work"]}]}`
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"gardener", "apply", root}, strings.NewReader(proposal), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("gardener dry-run = %d, stderr: %s\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "dry-run") {
		t.Errorf("expected dry-run summary, got: %s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, "work")); !os.IsNotExist(err) {
		t.Error("dry-run created work/")
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"gardener", "apply", "--apply", root}, strings.NewReader(proposal), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("gardener apply = %d, stderr: %s\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "applied and committed") {
		t.Errorf("expected apply summary, got: %s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, "work", "node.md")); err != nil {
		t.Errorf("work/node.md missing: %v", err)
	}
}

func TestRunGardenerRejectedAndUsage(t *testing.T) {
	root := newGitRepo(t)

	bad := `{"author":"test","reason":"x","operations":[{"op":"archive_node","node":"global"}]}`
	var stdout, stderr bytes.Buffer
	code := run([]string{"gardener", "apply", root}, strings.NewReader(bad), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("rejected apply = %d, want 1; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "rejected") {
		t.Errorf("stderr = %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"gardener"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Errorf("missing subcommand = %d, want 2", code)
	}
	if code := run([]string{"gardener", "apply", root}, strings.NewReader(`{`), &stdout, &stderr); code != 2 {
		t.Errorf("bad json = %d, want 2", code)
	}
	if code := run([]string{"gardener", "prune", root}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Errorf("unknown subcommand = %d, want 2", code)
	}
}
