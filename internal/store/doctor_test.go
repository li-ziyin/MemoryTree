package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/initrepo"
)

func gitIn(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestDoctorCleanRepoHasNoFindings(t *testing.T) {
	root := newRepo(t)
	rep, err := Doctor(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Dirty) != 0 || len(rep.TempFiles) != 0 || rep.OtherUntracked != 0 {
		t.Fatalf("findings on a clean repo: %+v", rep)
	}
}

// A crashed write leaves the entry file present but uncommitted; an
// unrelated untracked file must not be reported as memoryd's business.
func TestDoctorReportsUncommittedManagedFilesOnly(t *testing.T) {
	root := newRepo(t)
	res, err := AddEntry(root, validInput())
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "reset", "--soft", "HEAD~1") // lose the commit, keep the file
	if err := os.WriteFile(filepath.Join(root, "bench-questions.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	rep, err := Doctor(root, false)
	if err != nil {
		t.Fatal(err)
	}
	rel := relFromRoot(root, res.Path)
	if len(rep.Dirty) != 1 || rep.Dirty[0].Path != rel {
		t.Fatalf("dirty = %+v, want only %s", rep.Dirty, rel)
	}
	if rep.OtherUntracked != 1 {
		t.Errorf("other untracked = %d, want 1 (bench-questions.jsonl)", rep.OtherUntracked)
	}
	// Read-only mode must not touch anything.
	if out := gitIn(t, root, "status", "--porcelain"); strings.Count(out, "\n")+1 < 2 {
		t.Errorf("read-only doctor changed git state: %s", out)
	}
}

func TestDoctorReportsTempLeftoversWithoutRemoving(t *testing.T) {
	root := newRepo(t)
	memDir := filepath.Join(root, "global", "memories")
	if err := os.MkdirAll(memDir, 0o700); err != nil {
		t.Fatal(err)
	}
	leftover := filepath.Join(memDir, "2026-10-27-entry.md.tmp-9999")
	if err := os.WriteFile(leftover, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	rep, err := Doctor(root, false)
	if err != nil {
		t.Fatal(err)
	}
	want := "global/memories/2026-10-27-entry.md.tmp-9999"
	if len(rep.TempFiles) != 1 || rep.TempFiles[0] != want {
		t.Fatalf("temp files = %v, want [%s]", rep.TempFiles, want)
	}
	if _, err := os.Stat(leftover); err != nil {
		t.Fatalf("read-only doctor removed the leftover: %v", err)
	}
}

func TestDoctorFixCommitsWrittenFilesAndDeletions(t *testing.T) {
	root := newRepo(t)
	res, err := AddEntry(root, validInput())
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "reset", "--soft", "HEAD~1") // simulate: written, never committed

	rep, err := Doctor(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Committed {
		t.Fatalf("fix did not commit: %+v", rep)
	}
	if rep.Aborted != "" {
		t.Fatalf("aborted: %s", rep.Aborted)
	}
	if out := gitIn(t, root, "status", "--porcelain"); out != "" {
		t.Errorf("work tree not clean after fix: %s", out)
	}
	head := gitIn(t, root, "log", "--oneline", "-1")
	if !strings.Contains(head, "memoryd: doctor recovery") {
		t.Errorf("head = %q, want a doctor recovery commit", head)
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("recovered entry file lost: %v", err)
	}
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		t.Errorf("repository invalid after recovery: %v", err)
	}

	// A second run on the now-clean repo is a no-op that stays clean.
	rep2, err := Doctor(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.Committed || len(rep2.Dirty) != 0 {
		t.Errorf("second fix run: %+v", rep2)
	}
	if head2 := gitIn(t, root, "log", "--oneline", "-1"); head2 != head {
		t.Errorf("second fix run created a commit: %q -> %q", head, head2)
	}
}

func TestDoctorFixCommitsDeletionOfManagedFile(t *testing.T) {
	root := newRepo(t)
	res, err := AddEntry(root, validInput())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(res.Path); err != nil {
		t.Fatal(err)
	} // uncommitted deletion: still a dirty managed path

	rep, err := Doctor(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Committed {
		t.Fatalf("deletion not recovered: %+v", rep)
	}
	if out := gitIn(t, root, "status", "--porcelain"); out != "" {
		t.Errorf("work tree not clean: %s", out)
	}
}

// Doctor never commits a broken tree and never discards data: a file that
// breaks validation keeps the report and the repository exactly as found.
func TestDoctorFixRefusesInvalidTree(t *testing.T) {
	root := newRepo(t)
	res, err := AddEntry(root, validInput())
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "reset", "--soft", "HEAD~1")
	broken := "not: [valid frontmatter"
	if err := os.WriteFile(res.Path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	headBefore := gitIn(t, root, "log", "--oneline", "-1")

	rep, err := Doctor(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Committed {
		t.Fatalf("doctor committed an invalid tree: %+v", rep)
	}
	if rep.Validation == nil || rep.Validation.Errors() == 0 {
		t.Fatalf("validation findings missing: %+v", rep.Validation)
	}
	if rep.Aborted == "" {
		t.Error("aborted reason missing")
	}
	if headAfter := gitIn(t, root, "log", "--oneline", "-1"); headAfter != headBefore {
		t.Errorf("HEAD moved: %q -> %q", headBefore, headAfter)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil || string(data) != broken {
		t.Errorf("doctor modified the broken file: %v %q", err, data)
	}
}

func TestDoctorFixRemovesTempLeftovers(t *testing.T) {
	root := newRepo(t)
	memDir := filepath.Join(root, "global", "memories")
	if err := os.MkdirAll(memDir, 0o700); err != nil {
		t.Fatal(err)
	}
	leftover := filepath.Join(memDir, "2026-10-27-entry.md.tmp-9999")
	if err := os.WriteFile(leftover, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	rep, err := Doctor(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.FixedTemp != 1 {
		t.Fatalf("fixed temp = %d, want 1", rep.FixedTemp)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Errorf("leftover survived: %v", err)
	}
	if rep.Committed {
		t.Error("temp-only cleanup must not create a commit")
	}
}

// The manual recovery hint tells the user to `git add && git commit`; if
// they finish the add but get interrupted before the commit, doctor must
// complete exactly that state.
func TestDoctorFixFinishesPartiallyStagedRecovery(t *testing.T) {
	root := newRepo(t)
	res, err := AddEntry(root, validInput())
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "reset", "--soft", "HEAD~1")
	rel := relFromRoot(root, res.Path)
	gitIn(t, root, "add", "--", rel)

	rep, err := Doctor(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Committed {
		t.Fatalf("doctor did not finish the staged recovery: %+v", rep)
	}
	if out := gitIn(t, root, "status", "--porcelain"); out != "" {
		t.Errorf("work tree not clean: %s", out)
	}
}

func TestDoctorRequiresGitRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	if _, err := initrepo.Init(initrepo.Options{Root: root, Title: "Test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Doctor(root, false); err == nil {
		t.Fatal("doctor accepted a non-repo directory")
	}
}
