package gardener

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/li-ziyin/memorytree/internal/docfile"
	"github.com/li-ziyin/memorytree/internal/ids"
	"github.com/li-ziyin/memorytree/internal/misslog"
	"github.com/li-ziyin/memorytree/internal/model"
)

// addNode writes a node directory with node.md into the copied example repo.
func addNode(t *testing.T, root, parentDir, parentID, slug, kind string, tagsDelta []string) {
	t.Helper()
	dir := filepath.Join(root, parentDir, slug)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	parent := parentID
	node := &model.Node{
		Schema:      model.NodeSchema,
		ID:          ids.NewUUIDv7(),
		Kind:        kind,
		Parent:      &parent,
		Slug:        slug,
		Title:       slug,
		Summary:     "test node " + slug,
		TagsDelta:   tagsDelta,
		Status:      model.StatusActive,
		Sensitivity: model.SensitivityPrivate,
		CreatedAt:   "2026-10-27T00:00:00Z",
		UpdatedAt:   "2026-10-27T00:00:00Z",
	}
	if err := docfile.WriteFrontmatter(filepath.Join(dir, "node.md"), node, nil); err != nil {
		t.Fatal(err)
	}
}

func TestDataIncludesQueryMisses(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "..", "spec", "examples"))); err != nil {
		t.Fatalf("copy example: %v", err)
	}
	if err := misslog.Record(root, "arkts 布局", time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := misslog.Record(root, "arkts 布局", time.Date(2026, 10, 27, 9, 5, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := misslog.Record(root, "docker 迁移", time.Date(2026, 10, 27, 9, 6, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	res, report, err := Data(root)
	if err != nil {
		t.Fatalf("data: %v", err)
	}
	if report == nil {
		t.Fatalf("no report: %d validation errors", res.Errors())
	}
	if len(report.QueryMisses) != 2 {
		t.Fatalf("query misses = %+v, want 2", report.QueryMisses)
	}
	top := report.QueryMisses[0]
	if top.Query != "arkts 布局" || top.Count != 2 || top.Last != "2026-10-27T09:05:00Z" {
		t.Errorf("top miss = %+v", top)
	}
}

func TestDataReportsStructureAndSignals(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "..", "spec", "examples"))); err != nil {
		t.Fatalf("copy example: %v", err)
	}
	// Two projects under work sharing most tags -> pair signal.
	addNode(t, root, "work", exWork, "ai-1", model.KindProject, []string{"ai", "ml"})
	addNode(t, root, "work", exWork, "ai-2", model.KindProject, []string{"ai", "ml", "llm"})
	// Nine children under work -> branch signal (work starts with agentflow
	// plus these two; add seven more).
	for i := 3; i <= 9; i++ {
		addNode(t, root, "work", exWork, fmt.Sprintf("filler-%d", i), model.KindProject, []string{"filler"})
	}

	res, report, err := Data(root)
	if err != nil {
		t.Fatalf("data: %v", err)
	}
	if report == nil {
		t.Fatalf("report is nil; validation errors: %d", res.Errors())
	}
	if len(report.Nodes) != 13 { // root, global, work, agentflow, ai-1/2, filler-3..9
		t.Fatalf("nodes = %d", len(report.Nodes))
	}
	if report.Guidelines == "" || !contains(report.Guidelines, "move_node") {
		t.Error("guidelines must describe the op schema")
	}

	var work *NodeReport
	for i := range report.Nodes {
		if report.Nodes[i].Path == "work" {
			work = &report.Nodes[i]
		}
	}
	if work == nil {
		t.Fatal("work node missing from report")
	}
	if !hasSignal(work.Signals, "children=") {
		t.Errorf("work signals = %v, want children>N>8", work.Signals)
	}
	if work.ChildCount != 10 {
		t.Errorf("work children = %d, want 10", work.ChildCount)
	}

	found := false
	for _, pair := range report.PairSignals {
		if pair.A == "work/ai-1" && pair.B == "work/ai-2" {
			found = true
			if pair.Jaccard < 0.5 || len(pair.SharedTags) < 2 {
				t.Errorf("pair = %+v", pair)
			}
		}
	}
	if !found {
		t.Errorf("expected ai-1/ai-2 overlap signal, got %+v", report.PairSignals)
	}
}

func TestDataEmptyProjectSignal(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "..", "spec", "examples"))); err != nil {
		t.Fatalf("copy example: %v", err)
	}
	addNode(t, root, "work", exWork, "ghost", model.KindProject, nil)

	_, report, err := Data(root)
	if err != nil || report == nil {
		t.Fatalf("data: %v %v", report, err)
	}
	for _, n := range report.Nodes {
		if n.Path == "work/ghost" && !hasSignal(n.Signals, "empty-project") {
			t.Errorf("ghost signals = %v", n.Signals)
		}
	}
}

func hasSignal(signals []string, prefix string) bool {
	for _, s := range signals {
		if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
