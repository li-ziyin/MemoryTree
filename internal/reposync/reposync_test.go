package reposync

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/li-ziyin/memorytree/internal/gitio"
	"github.com/li-ziyin/memorytree/internal/initrepo"
	"github.com/li-ziyin/memorytree/internal/store"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v (in %s): %v: %s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// seedRepo builds a valid data repository with one baseline commit.
func seedRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; sync tests require it")
	}
	root := t.TempDir()
	if _, err := initrepo.Init(initrepo.Options{Root: root, Title: "Sync Test"}); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q")
	git(t, root, "config", "user.email", "test@example.com")
	git(t, root, "config", "user.name", "Test")
	git(t, root, "config", "commit.gpgsign", "false")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "init: baseline")
	return root
}

// twoClones seeds one repository, publishes it to a bare remote, and
// returns two clones whose HEADs both track the same upstream branch.
func twoClones(t *testing.T) (a, b string) {
	t.Helper()
	seed := seedRepo(t)
	base := t.TempDir()
	bare := filepath.Join(base, "origin.git")
	git(t, seed, "clone", "-q", "--bare", ".", bare)
	a, b = filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, dst := range []string{a, b} {
		git(t, seed, "clone", "-q", bare, dst)
		git(t, dst, "config", "user.email", "test@example.com")
		git(t, dst, "config", "user.name", "Test")
		git(t, dst, "config", "commit.gpgsign", "false")
	}
	return a, b
}

func head(t *testing.T, dir string) string {
	t.Helper()
	return git(t, dir, "rev-parse", "HEAD")
}

func addEntry(t *testing.T, root, title string) {
	t.Helper()
	_, err := store.AddEntry(root, store.EntryInput{
		Type:    "project",
		Title:   title,
		Summary: title + " 摘要。",
		Body:    title + " 正文。",
		Source:  store.SourceInput{Agent: "sync-test", Kind: "conversation"},
	})
	if err != nil {
		t.Fatalf("add %q: %v", title, err)
	}
}

// TestSyncTwoClonesConverge is the M4 acceptance: concurrent edits on both
// sides of two clones converge — the second sync rebases onto the pushed
// work and both clones end at the same commit with both entries present.
func TestSyncTwoClonesConverge(t *testing.T) {
	a, b := twoClones(t)
	addEntry(t, a, "A 侧写入")
	addEntry(t, b, "B 侧写入")

	if _, err := Sync(a); err != nil {
		t.Fatalf("sync a: %v", err)
	}
	res, err := Sync(b)
	if err != nil {
		t.Fatalf("sync b: %v", err)
	}
	if !res.Rebased || !res.Pushed {
		t.Fatalf("second sync did not rebase and push: %+v", res)
	}
	// a is now behind; syncing it again is a fetch + fast-forward rebase.
	if _, err := Sync(a); err != nil {
		t.Fatalf("sync a again: %v", err)
	}
	if headA, headB := head(t, a), head(t, b); headA != headB {
		t.Fatalf("clones diverged: %s != %s", headA, headB)
	}
	for _, clone := range []string{a, b} {
		matches, err := filepath.Glob(filepath.Join(clone, "global", "memories", "*.md"))
		if err != nil || len(matches) != 2 {
			t.Errorf("%s has %d entries, want 2 (%v)", clone, len(matches), err)
		}
		if out := git(t, clone, "status", "--porcelain"); out != "" {
			t.Errorf("%s dirty after sync: %s", clone, out)
		}
	}
}

func TestSyncRefusesDirtyTree(t *testing.T) {
	a, _ := twoClones(t)
	noise := filepath.Join(a, "global", "memories", "uncommitted.md")
	if err := os.MkdirAll(filepath.Dir(noise), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(noise, []byte("draft"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Sync(a)
	if !errors.Is(err, ErrDirtyTree) {
		t.Fatalf("err = %v, want ErrDirtyTree", err)
	}
	if !strings.Contains(err.Error(), "doctor") {
		t.Errorf("error does not point at doctor: %v", err)
	}
}

func TestSyncRequiresUpstream(t *testing.T) {
	root := seedRepo(t)
	_, err := Sync(root)
	if !errors.Is(err, ErrNoUpstream) {
		t.Fatalf("err = %v, want ErrNoUpstream", err)
	}
}

// Conflicting edits of the same file surface as plain git conflicts: sync
// stops, the rebase stays in progress, and the error names the recovery.
func TestSyncConflictSurfacesGitConflict(t *testing.T) {
	a, b := twoClones(t)
	// Same file, same line, divergent edits committed on both sides.
	target := filepath.Join(a, "global", "node.md")
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	lineA := strings.Replace(string(raw), "title: 全局", "title: 全局-A", 1)
	lineB := strings.Replace(string(raw), "title: 全局", "title: 全局-B", 1)
	if lineA == string(raw) || lineB == string(raw) {
		t.Skip("example fixture lacks the expected title line")
	}
	if err := os.WriteFile(target, []byte(lineA), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, a, "add", "-A")
	git(t, a, "commit", "-q", "-m", "a edits node")
	if err := os.WriteFile(filepath.Join(b, "global", "node.md"), []byte(lineB), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, b, "add", "-A")
	git(t, b, "commit", "-q", "-m", "b edits node")

	if _, err := Sync(a); err != nil {
		t.Fatalf("sync a: %v", err)
	}
	res, err := Sync(b)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if !res.Conflict {
		t.Errorf("result does not mark the conflict: %+v", res)
	}
	if !strings.Contains(err.Error(), "rebase --continue") {
		t.Errorf("error lacks the recovery hint: %v", err)
	}
	if kind, kerr := gitio.InProgress(b); kerr != nil || kind != "rebase" {
		t.Errorf("rebase not left in progress: kind=%q err=%v", kind, kerr)
	}
	// A retry while the rebase is unfinished refuses cleanly.
	if _, err := Sync(b); !errors.Is(err, ErrRebaseInProgress) {
		t.Fatalf("retry during rebase: err = %v, want ErrRebaseInProgress", err)
	}
	// Aborting restores a clean, still-valid tree.
	git(t, b, "rebase", "--abort")
	if out := git(t, b, "status", "--porcelain"); out != "" {
		t.Errorf("tree dirty after abort: %s", out)
	}
}
