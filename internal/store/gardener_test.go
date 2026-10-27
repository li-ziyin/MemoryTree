package store

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/li-ziyin/memorytree/internal/gardener"
	"github.com/li-ziyin/memorytree/internal/model"
	"github.com/li-ziyin/memorytree/internal/validate"
)

func gitLogCount(t *testing.T, root string) int {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "rev-list", "--count", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-list: %v", err)
	}
	n := 0
	for _, c := range strings.TrimSpace(string(out)) {
		n = n*10 + int(c-'0')
	}
	return n
}

func gitStatusClean(t *testing.T, root string) bool {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	return len(strings.TrimSpace(string(out))) == 0
}

func mustValidateClean(t *testing.T, root string) *validate.Snapshot {
	t.Helper()
	res, snap, err := validate.ValidateWithSnapshot(root)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !res.OK() {
		t.Fatalf("validation errors: %v", res.Issues)
	}
	return snap
}

func createProposal() *gardener.Proposal {
	return &gardener.Proposal{
		Author: "test-agent",
		Reason: "seed work tree with two projects",
		Operations: []gardener.Operation{
			{Op: gardener.OpCreateNode, Target: ".", Kind: model.KindCategory, Slug: "work",
				Title: "工作", Summary: "求职与职业", TagsDelta: []string{"work"}},
			{Op: gardener.OpCreateNode, Target: "work", Kind: model.KindProject, Slug: "p1",
				Title: "项目一", Summary: "第一个项目", TagsDelta: []string{"alpha"}},
			{Op: gardener.OpCreateNode, Target: "work", Kind: model.KindProject, Slug: "p2",
				Title: "项目二", Summary: "第二个项目", TagsDelta: []string{"beta"}},
		},
	}
}

func TestGardenerCreateApplyAndCommit(t *testing.T) {
	root := newRepo(t)
	before := gitLogCount(t, root)

	res, err := ApplyGardenerProposal(root, createProposal(), GardenerOptions{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Committed || res.Generation == 0 {
		t.Fatalf("result = %+v", res)
	}
	if got := gitLogCount(t, root); got != before+1 {
		t.Fatalf("git commits = %d, want %d (one gardener commit)", got, before+1)
	}
	if !gitStatusClean(t, root) {
		t.Fatal("working tree is dirty after apply")
	}
	for _, dir := range []string{"work", "work/p1", "work/p2"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(dir), "node.md")); err != nil {
			t.Errorf("missing %s/node.md: %v", dir, err)
		}
	}
	snap := mustValidateClean(t, root)
	var p1 *validate.NodeInfo
	for i := range snap.Nodes {
		if snap.Nodes[i].Path == "work/p1" {
			p1 = &snap.Nodes[i]
		}
	}
	if p1 == nil || len(p1.Tags) != 2 { // work + alpha
		t.Fatalf("work/p1 effective tags = %+v", p1)
	}
}

// TestGardenerDeprecateEntry is the M4 usage-signal acceptance: a cold
// entry is retired by a deprecate_entry proposal — dry-run plans it, the
// real apply rewrites only the status, and the body survives verbatim.
func TestGardenerDeprecateEntry(t *testing.T) {
	root := newRepo(t)
	added, err := AddEntry(root, validInput())
	if err != nil {
		t.Fatal(err)
	}
	bodyBefore, err := os.ReadFile(added.Path)
	if err != nil {
		t.Fatal(err)
	}
	proposal := &gardener.Proposal{
		Author: "test-agent",
		Reason: "cold entry retirement",
		Operations: []gardener.Operation{
			{Op: gardener.OpDeprecateEntry, Entry: added.ID, Reason: "never returned in 90+ days"},
		},
	}

	dry, err := ApplyGardenerProposal(root, proposal, GardenerOptions{DryRun: true})
	if err != nil || !dry.Report.OK {
		t.Fatalf("dry-run: %v, report %+v", err, dry.Report.Ops)
	}
	if n := gitLogCount(t, root); n != 2 { // baseline + the add
		t.Fatalf("dry-run changed history: %d commits", n)
	}

	res, err := ApplyGardenerProposal(root, proposal, GardenerOptions{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Committed {
		t.Fatalf("apply was not committed")
	}
	snap := mustValidateClean(t, root)
	for _, e := range snap.Entries {
		if e.ID == added.ID {
			if e.Status != model.EntryDeprecated {
				t.Fatalf("status = %s, want deprecated", e.Status)
			}
		}
	}
	after, err := os.ReadFile(added.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) == string(bodyBefore) {
		t.Fatal("entry file was not rewritten")
	}
	if !strings.Contains(string(after), "status: deprecated") {
		t.Fatalf("rewritten frontmatter missing deprecated status:\n%s", after)
	}
	if !strings.Contains(string(after), "正文：先 docker compose logs，再核对端口。") {
		t.Fatalf("body not preserved verbatim:\n%s", after)
	}
}

func TestGardenerDryRunTouchesNothing(t *testing.T) {
	root := newRepo(t)
	before := gitLogCount(t, root)

	res, err := ApplyGardenerProposal(root, createProposal(), GardenerOptions{DryRun: true})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !res.DryRun || res.Committed {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "work")); !os.IsNotExist(err) {
		t.Error("dry-run created work/")
	}
	if got := gitLogCount(t, root); got != before {
		t.Errorf("dry-run committed: %d != %d", got, before)
	}
	if !gitStatusClean(t, root) {
		t.Error("dry-run left a dirty tree")
	}
}

func TestGardenerRejectedProposalWritesNothing(t *testing.T) {
	root := newRepo(t)
	if _, err := ApplyGardenerProposal(root, createProposal(), GardenerOptions{}); err != nil {
		t.Fatalf("seed apply: %v", err)
	}
	before := gitLogCount(t, root)

	bad := &gardener.Proposal{
		Reason: "self-contradicting",
		Operations: []gardener.Operation{
			{Op: gardener.OpMoveNode, Node: "work", Target: "work/p1"}, // cycle
			{Op: gardener.OpCreateNode, Target: "work", Kind: model.KindProject, Slug: "p1",
				Title: "dup", Summary: "dup"}, // slug conflict
		},
	}
	res, err := ApplyGardenerProposal(root, bad, GardenerOptions{})
	if !errors.Is(err, ErrProposalRejected) {
		t.Fatalf("err = %v, want ErrProposalRejected", err)
	}
	if res == nil || res.Report.OK {
		t.Fatalf("report = %+v", res)
	}
	if got := gitLogCount(t, root); got != before {
		t.Errorf("rejected proposal committed: %d != %d", got, before)
	}
	if !gitStatusClean(t, root) {
		t.Error("rejected proposal left a dirty tree")
	}
}

func TestGardenerFullRestructureRound(t *testing.T) {
	root := newRepo(t)
	if _, err := ApplyGardenerProposal(root, createProposal(), GardenerOptions{}); err != nil {
		t.Fatalf("seed apply: %v", err)
	}
	in := validInput()
	in.Node = "work/p1"
	add, err := AddEntry(root, in)
	if err != nil {
		t.Fatalf("add entry: %v", err)
	}

	proposal := &gardener.Proposal{
		Author: "test-agent",
		Reason: "move the entry, retag and promote p1, archive p2",
		Operations: []gardener.Operation{
			{Op: gardener.OpMoveEntry, Entry: add.ID, Target: "work/p2"},
			{Op: gardener.OpRetagNode, Node: "work/p1", Remove: []string{"alpha"}, Add: []string{"promoted"}},
			{Op: gardener.OpReviseNode, Node: "work/p1", Summary: "升级为顶层项目"},
			{Op: gardener.OpMoveNode, Node: "work/p1", Target: "."},
			{Op: gardener.OpArchiveNode, Node: "work/p2"},
		},
	}
	res, err := ApplyGardenerProposal(root, proposal, GardenerOptions{})
	if err != nil {
		t.Fatalf("restructure: %v", err)
	}
	if res.Report.MovedEntries != 1 {
		t.Errorf("moved entries = %d", res.Report.MovedEntries)
	}

	snap := mustValidateClean(t, root)
	if _, err := os.Stat(filepath.Join(root, "p1", "node.md")); err != nil {
		t.Errorf("p1 was not promoted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "work", "p1")); !os.IsNotExist(err) {
		t.Error("work/p1 still exists after move")
	}
	for _, e := range snap.Entries {
		if e.ID != add.ID {
			continue
		}
		if e.NodePath != "work/p2" {
			t.Errorf("entry node path = %s, want work/p2", e.NodePath)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(e.Path))); err != nil {
			t.Errorf("entry file missing at new location: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "work", "p1", "memories")); !os.IsNotExist(err) {
			t.Error("old memories dir still exists")
		}
	}
	var promoted *validate.NodeInfo
	for i := range snap.Nodes {
		if snap.Nodes[i].Path == "p1" {
			promoted = &snap.Nodes[i]
		}
		if snap.Nodes[i].Path == "work/p2" && snap.Nodes[i].Status != model.StatusArchived {
			t.Error("work/p2 not archived")
		}
	}
	if promoted == nil {
		t.Fatal("promoted p1 missing")
	}
	if len(promoted.TagsDelta) != 1 || promoted.TagsDelta[0] != "promoted" {
		t.Errorf("p1 delta = %v", promoted.TagsDelta)
	}
	// Moving out of work drops the inherited work tag.
	if len(promoted.Tags) != 1 || promoted.Tags[0] != "promoted" {
		t.Errorf("p1 effective tags = %v", promoted.Tags)
	}
	if !gitStatusClean(t, root) {
		t.Error("dirty tree after restructure")
	}
}

func TestGardenerMoveEntryDropsRedundantDeltaTags(t *testing.T) {
	root := newRepo(t)
	if _, err := ApplyGardenerProposal(root, &gardener.Proposal{
		Reason: "seed",
		Operations: []gardener.Operation{
			{Op: gardener.OpCreateNode, Target: ".", Kind: model.KindProject, Slug: "src",
				Title: "src", Summary: "source host"},
			{Op: gardener.OpCreateNode, Target: ".", Kind: model.KindProject, Slug: "dst",
				Title: "dst", Summary: "target host", TagsDelta: []string{"go"}},
		},
	}, GardenerOptions{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	in := validInput()
	in.Node = "src"
	in.TagsDelta = []string{"go", "extra"}
	add, err := AddEntry(root, in)
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	res, err := ApplyGardenerProposal(root, &gardener.Proposal{
		Reason: "absorb redundant delta",
		Operations: []gardener.Operation{
			{Op: gardener.OpMoveEntry, Entry: add.ID, Target: "dst"},
		},
	}, GardenerOptions{})
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	// The plan must record the absorbed tag.
	foundDrop := false
	for _, s := range res.Report.Steps {
		for _, tag := range s.DropTags {
			if tag == "go" {
				foundDrop = true
			}
		}
	}
	if !foundDrop {
		t.Fatalf("plan did not drop the redundant tag: %+v", res.Report.Steps)
	}

	snap := mustValidateClean(t, root) // V-TAG-2 would fail without the drop
	for _, e := range snap.Entries {
		if e.ID != add.ID {
			continue
		}
		if len(e.TagsDelta) != 1 || e.TagsDelta[0] != "extra" {
			t.Fatalf("entry delta after move = %v, want [extra]", e.TagsDelta)
		}
		if len(e.Tags) != 2 { // go + extra, both still effective
			t.Fatalf("effective tags = %v, want [extra go]", e.Tags)
		}
	}
}

func TestGardenerExecutorRollbackRestoresBytes(t *testing.T) {
	root := newRepo(t)
	snap := mustValidateClean(t, root)
	globalDir := filepath.Join(root, "global")
	nodePath := filepath.Join(globalDir, "node.md")
	original, err := os.ReadFile(nodePath)
	if err != nil {
		t.Fatal(err)
	}

	ex := newGardenerExecutor(root, snap)
	step := gardener.Step{
		Op:   gardener.OpRetagNode,
		Node: gardener.NodeRef{ID: snap.Nodes[1].ID, Path: "global"}, // global node
		Add:  []string{"extra"},
	}
	if err := ex.retagNode(step); err != nil {
		t.Fatalf("retag: %v", err)
	}
	if changed, _ := os.ReadFile(nodePath); string(changed) == string(original) {
		t.Fatal("retag did not modify node.md")
	}
	ex.rollback()
	after, err := os.ReadFile(nodePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("rollback did not restore original bytes:\n%s", after)
	}
}
