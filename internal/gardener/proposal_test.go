package gardener

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/li-ziyin/memorytree/internal/model"
	"github.com/li-ziyin/memorytree/internal/validate"
)

// Example-repository IDs fixed by spec/memory-format-v1.md.
const (
	exRootID = "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b01"
	exGlobal = "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b02"
	exWork   = "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b03"
	exFlow   = "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b04"
	exSSE    = "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b06"
)

func exampleSnapshot(t *testing.T) *validate.Snapshot {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "..", "spec", "examples"))); err != nil {
		t.Fatalf("copy example: %v", err)
	}
	_, snap, err := validate.ValidateWithSnapshot(root)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	return snap
}

func opReportsByCode(t *testing.T, report *Report) map[string]OpReport {
	t.Helper()
	byCode := map[string]OpReport{}
	for _, op := range report.Ops {
		if op.Code != "" {
			byCode[op.Code] = op
		}
	}
	return byCode
}

func TestPlanAcceptsMoveWithTagDiff(t *testing.T) {
	snap := exampleSnapshot(t)
	report := Plan(snap, &Proposal{
		Author: "test",
		Operations: []Operation{
			{Op: OpMoveNode, Node: "work/agentflow", Target: ".", Reason: "promote to top level"},
		},
	})
	if !report.OK {
		t.Fatalf("expected OK, got %+v", report.Ops)
	}
	if len(report.Steps) != 1 || report.Steps[0].FromPath != "work/agentflow" || report.Steps[0].ToPath != "agentflow" {
		t.Fatalf("steps = %+v", report.Steps)
	}
	if report.Steps[0].Node.ID != exFlow || report.Steps[0].Parent.ID != exRootID {
		t.Fatalf("step refs = %+v", report.Steps[0])
	}
	// Moving out of work drops the inherited work tag.
	if len(report.TagDiffs) != 1 || report.TagDiffs[0].Path != "agentflow" {
		t.Fatalf("tag diffs = %+v", report.TagDiffs)
	}
	if len(report.TagDiffs[0].Removed) != 1 || report.TagDiffs[0].Removed[0] != "work" {
		t.Fatalf("removed tags = %+v", report.TagDiffs[0])
	}
}

func TestPlanChainedCreateAndMove(t *testing.T) {
	snap := exampleSnapshot(t)
	report := Plan(snap, &Proposal{
		Operations: []Operation{
			{Op: OpCreateNode, Target: "work", Kind: model.KindProject, Slug: "ml",
				Title: "ML", Summary: "机器学习试验", TagsDelta: []string{"ml"}},
			{Op: OpMoveEntry, Entry: exSSE, Target: "work/ml"},
		},
	})
	if !report.OK {
		t.Fatalf("expected OK, got %+v", report.Ops)
	}
	if report.MovedEntries != 1 || report.Affected != 2 {
		t.Errorf("moved=%d affected=%d", report.MovedEntries, report.Affected)
	}
	// The move step must carry the created node's path, not a UUID.
	if report.Steps[1].Parent.ID != "" || report.Steps[1].Parent.Path != "work/ml" {
		t.Fatalf("move step parent ref = %+v", report.Steps[1].Parent)
	}
}

func TestPlanBlocksRuleViolations(t *testing.T) {
	snap := exampleSnapshot(t)
	cases := []struct {
		name string
		ops  []Operation
		code string
	}{
		{"cycle", []Operation{{Op: OpMoveNode, Node: "work", Target: "work/agentflow"}}, CodeCycle},
		{"move under project", []Operation{{Op: OpMoveNode, Node: "work", Target: "work/agentflow/x"}}, CodeNodeGone},
		{"move under global", []Operation{{Op: OpMoveNode, Node: "work/agentflow", Target: "global"}}, CodeKind},
		{"move root", []Operation{{Op: OpMoveNode, Node: ".", Target: "work"}}, CodeKind},
		{"slug conflict", []Operation{{Op: OpCreateNode, Target: "work", Kind: model.KindProject, Slug: "agentflow",
			Title: "dup", Summary: "dup"}}, CodeSlug},
		{"reserved slug", []Operation{{Op: OpCreateNode, Target: "work", Kind: model.KindProject, Slug: "memories",
			Title: "x", Summary: "x"}}, CodeSlug},
		{"bad kind", []Operation{{Op: OpCreateNode, Target: "work", Kind: "root", Slug: "x",
			Title: "x", Summary: "x"}}, CodeKindNew},
		{"create under global", []Operation{{Op: OpCreateNode, Target: "global", Kind: model.KindProject, Slug: "x",
			Title: "x", Summary: "x"}}, CodeKind},
		{"inherited tag", []Operation{{Op: OpRetagNode, Node: "work/agentflow", Add: []string{"work"}}}, CodeTag},
		{"remove inherited tag", []Operation{{Op: OpRetagNode, Node: "work/agentflow", Remove: []string{"work"}}}, CodeTagRemove},
		{"bad tag syntax", []Operation{{Op: OpRetagNode, Node: "work/agentflow", Add: []string{"has space"}}}, CodeTag},
		{"entry to category", []Operation{{Op: OpMoveEntry, Entry: exSSE, Target: "work"}}, CodeKindEntry},
		{"entry to root", []Operation{{Op: OpMoveEntry, Entry: exSSE, Target: "."}}, CodeKindEntry},
		{"unknown entry", []Operation{{Op: OpMoveEntry, Entry: "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1bff", Target: "global"}}, CodeEntryGone},
		{"unknown node", []Operation{{Op: OpArchiveNode, Node: "no/such"}}, CodeNodeGone},
		{"archive root", []Operation{{Op: OpArchiveNode, Node: "."}}, CodeKindArch},
		{"archive global", []Operation{{Op: OpArchiveNode, Node: "global"}}, CodeKindArch},
		{"empty title create", []Operation{{Op: OpCreateNode, Target: "work", Kind: model.KindProject, Slug: "x",
			Title: " ", Summary: "x"}}, CodeContent},
		{"empty summary revise", []Operation{{Op: OpReviseNode, Node: "work", Summary: " "}}, CodeContent},
		{"unknown op", []Operation{{Op: "explode", Node: "work"}}, CodeMalformed},
	}
	for _, tc := range cases {
		report := Plan(snap, &Proposal{Operations: tc.ops})
		if report.OK {
			t.Errorf("%s: expected rejection", tc.name)
			continue
		}
		byCode := opReportsByCode(t, report)
		if _, ok := byCode[tc.code]; !ok {
			t.Errorf("%s: codes = %v, want %s", tc.name, keysOf(byCode), tc.code)
		}
		if len(report.Steps) != 0 {
			t.Errorf("%s: rejected plan must not carry steps", tc.name)
		}
	}
}

func TestPlanDeprecateEntry(t *testing.T) {
	snap := exampleSnapshot(t)
	report := Plan(snap, &Proposal{
		Operations: []Operation{
			{Op: OpDeprecateEntry, Entry: exSSE, Reason: "cold entry"},
		},
	})
	if !report.OK {
		t.Fatalf("expected OK, got %+v", report.Ops)
	}
	step := report.Steps[0]
	if step.Entry != exSSE || step.FromPath != "work/agentflow/memories/2026-10-20-sse-pitfall.md" {
		t.Fatalf("step = %+v", step)
	}
	if report.Affected != 1 {
		t.Errorf("affected = %d, want 1", report.Affected)
	}
}

func TestPlanDeprecateEntryRuleViolations(t *testing.T) {
	snap := exampleSnapshot(t)
	report := Plan(snap, &Proposal{
		Operations: []Operation{
			{Op: OpDeprecateEntry, Entry: "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b99"},
		},
	})
	if report.OK {
		t.Fatalf("unknown entry accepted: %+v", report.Ops)
	}
	if got := report.Ops[0].Code; got != CodeEntryGone {
		t.Errorf("code = %s, want %s", got, CodeEntryGone)
	}

	// Deprecating twice: the second op sees the simulated status.
	report = Plan(snap, &Proposal{
		Operations: []Operation{
			{Op: OpDeprecateEntry, Entry: exSSE, Reason: "cold"},
			{Op: OpDeprecateEntry, Entry: exSSE, Reason: "again"},
		},
	})
	if report.OK {
		t.Fatalf("double deprecation accepted: %+v", report.Ops)
	}
	if got := report.Ops[1].Code; got != CodeEntryStat {
		t.Errorf("code = %s, want %s", got, CodeEntryStat)
	}
	if len(report.Steps) != 0 {
		t.Errorf("blocked proposal leaked steps: %+v", report.Steps)
	}
}

func TestPlanSensitivityWeakerThanTarget(t *testing.T) {
	// A hand-built snapshot: a public entry cannot move into a private node
	// (V-SENS-1: an entry must never be less strict than its host).
	snap := &validate.Snapshot{
		Nodes: []validate.NodeInfo{
			{ID: "r", Kind: model.KindRoot, Slug: "", Path: ".", Sensitivity: model.SensitivityPrivate, Status: model.StatusActive},
			{ID: "g", ParentID: "r", Kind: model.KindCategory, Slug: model.GlobalSlug, Path: "global",
				Tags: []string{model.GlobalSlug}, TagsDelta: []string{model.GlobalSlug},
				Sensitivity: model.SensitivityPublic, Status: model.StatusActive},
			{ID: "p", ParentID: "r", Kind: model.KindProject, Slug: "vault", Path: "vault",
				Tags: []string{}, TagsDelta: []string{},
				Sensitivity: model.SensitivityPrivate, Status: model.StatusActive},
		},
		Entries: []validate.EntryInfo{
			{ID: "e", NodeID: "g", Path: "global/memories/e.md", Status: model.StatusActive,
				Sensitivity: model.SensitivityPublic},
		},
	}
	report := Plan(snap, &Proposal{Operations: []Operation{
		{Op: OpMoveEntry, Entry: "e", Target: "vault"},
	}})
	if report.OK {
		t.Fatal("expected G-SENS-2 rejection")
	}
	if report.Ops[0].Code != CodeSensEntry {
		t.Fatalf("code = %s, want %s", report.Ops[0].Code, CodeSensEntry)
	}
}

func TestPlanDepthAndBranchWarnings(t *testing.T) {
	// Chain of 5 nested categories plus 9 siblings under one parent.
	snap := &validate.Snapshot{Nodes: []validate.NodeInfo{
		{ID: "r", Kind: model.KindRoot, Slug: "", Path: ".", Status: model.StatusActive, Title: "r", Summary: "s"},
	}}
	parent := "r"
	path := ""
	for i := 0; i < 5; i++ {
		id := "c" + string(rune('a'+i))
		path = path + "/" + id
		snap.Nodes = append(snap.Nodes, validate.NodeInfo{
			ID: id, ParentID: parent, Kind: model.KindCategory, Slug: id, Path: path[1:],
			Status: model.StatusActive, Title: "node " + id, Summary: "s",
		})
		parent = id
	}
	for i := 0; i < 9; i++ {
		id := "s" + string(rune('a'+i))
		snap.Nodes = append(snap.Nodes, validate.NodeInfo{
			ID: id, ParentID: "r", Kind: model.KindProject, Slug: id, Path: id,
			Status: model.StatusActive, Title: "node " + id, Summary: "s",
		})
	}
	report := Plan(snap, &Proposal{Operations: []Operation{
		{Op: OpReviseNode, Node: "ca", Summary: "noop-ish revision to keep the proposal non-empty"},
	}})
	if !report.OK {
		t.Fatalf("unexpected rejection: %+v", report.Ops)
	}
	sawDepth, sawBranch := false, false
	for _, warn := range report.Warnings {
		if containsCode(warn, CodeDepthWarn) {
			sawDepth = true
		}
		if containsCode(warn, CodeBranchWarn) {
			sawBranch = true
		}
	}
	if !sawDepth || !sawBranch {
		t.Errorf("warnings = %v (depth=%v branch=%v)", report.Warnings, sawDepth, sawBranch)
	}
}

func keysOf(m map[string]OpReport) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func containsCode(warn, code string) bool {
	return strings.Contains(warn, code)
}
