package gardener

import (
	"fmt"
	"sort"
	"time"

	"github.com/li-ziyin/memorytree/internal/misslog"
	"github.com/li-ziyin/memorytree/internal/model"
	"github.com/li-ziyin/memorytree/internal/validate"
)

// NodeReport is the analysis portrait of one node.
type NodeReport struct {
	ID          string   `json:"id"`
	ParentID    string   `json:"parent_id"`
	Kind        string   `json:"kind"`
	Slug        string   `json:"slug"`
	Path        string   `json:"path"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	Tags        []string `json:"tags"`       // effective
	TagsDelta   []string `json:"tags_delta"` // own delta only
	Status      string   `json:"status"`
	Sensitivity string   `json:"sensitivity"`
	Depth       int      `json:"depth"`
	ChildCount  int      `json:"child_count"`
	EntryTotal  int      `json:"entry_total"`
	EntryActive int      `json:"entry_active"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	Signals     []string `json:"signals,omitempty"`
}

// PairSignal flags two sibling nodes whose effective tags overlap heavily —
// the merge/restructure trigger of DESIGN D6.
type PairSignal struct {
	A          string   `json:"a"`
	B          string   `json:"b"`
	SharedTags []string `json:"shared_tags"`
	Jaccard    float64  `json:"jaccard"`
}

// DataReport is the complete analysis package an agent reasons over to
// author a Proposal.
type DataReport struct {
	RepoID      string       `json:"repo_id"`
	GeneratedAt string       `json:"generated_at"`
	Fingerprint string       `json:"fingerprint"`
	Nodes       []NodeReport `json:"nodes"`
	PairSignals []PairSignal `json:"pair_signals"`
	// QueryMisses aggregates the MCP layer's retrieval-miss log: queries
	// that kept finding nothing, a restructuring trigger. Empty when no
	// misses are recorded.
	QueryMisses []misslog.Miss `json:"query_misses,omitempty"`
	Guidelines  string         `json:"guidelines"`
}

// pairJaccardMin is the overlap threshold for a pair signal.
const pairJaccardMin = 0.5

// Data validates the repository and builds the analysis package.
func Data(root string) (*validate.Result, *DataReport, error) {
	res, snap, err := validate.ValidateWithSnapshot(root)
	if err != nil {
		return res, nil, err
	}
	if !res.OK() {
		return res, nil, nil
	}

	childCount := map[string]int{}
	entryTotal := map[string]int{}
	entryActive := map[string]int{}
	for _, n := range snap.Nodes {
		if n.ParentID != "" {
			childCount[n.ParentID]++
		}
	}
	for _, e := range snap.Entries {
		entryTotal[e.NodeID]++
		if e.Status == model.StatusActive {
			entryActive[e.NodeID]++
		}
	}

	report := &DataReport{
		RepoID:      snap.Repo.RepoID,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Fingerprint: snap.Fingerprint,
		Guidelines:  Guidelines,
	}
	for _, n := range snap.Nodes {
		node := NodeReport{
			ID: n.ID, ParentID: n.ParentID, Kind: n.Kind, Slug: n.Slug, Path: n.Path,
			Title: n.Title, Summary: n.Summary, Tags: n.Tags, TagsDelta: n.TagsDelta,
			Status: n.Status, Sensitivity: n.Sensitivity, Depth: n.Depth,
			ChildCount: childCount[n.ID], EntryTotal: entryTotal[n.ID], EntryActive: entryActive[n.ID],
			CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
		}
		if node.Tags == nil {
			node.Tags = []string{}
		}
		if node.TagsDelta == nil {
			node.TagsDelta = []string{}
		}
		if n.Depth > 4 {
			node.Signals = append(node.Signals, "depth>4")
		}
		if childCount[n.ID] > 8 {
			node.Signals = append(node.Signals, fmt.Sprintf("children=%d>8", childCount[n.ID]))
		}
		if n.Kind == model.KindProject && n.Slug != model.GlobalSlug && entryTotal[n.ID] == 0 {
			node.Signals = append(node.Signals, "empty-project")
		}
		report.Nodes = append(report.Nodes, node)
	}

	report.PairSignals = overlapSignals(snap)
	// Retrieval misses drive restructuring too; an unreadable or missing
	// log simply yields no signal.
	if misses, err := misslog.Top(root, 10); err == nil {
		report.QueryMisses = misses
	}
	return res, report, nil
}

// overlapSignals compares every pair of nodes that share a parent.
func overlapSignals(snap *validate.Snapshot) []PairSignal {
	byParent := map[string][]validate.NodeInfo{}
	for _, n := range snap.Nodes {
		if n.ParentID != "" {
			byParent[n.ParentID] = append(byParent[n.ParentID], n)
		}
	}
	var signals []PairSignal
	for _, siblings := range byParent {
		for i := 0; i < len(siblings); i++ {
			for j := i + 1; j < len(siblings); j++ {
				a, b := siblings[i], siblings[j]
				shared := intersection(a.Tags, b.Tags)
				if len(shared) < 2 {
					continue
				}
				union := len(a.Tags) + len(b.Tags) - len(shared)
				jaccard := float64(len(shared)) / float64(union)
				if jaccard < pairJaccardMin {
					continue
				}
				signals = append(signals, PairSignal{
					A: a.Path, B: b.Path, SharedTags: shared, Jaccard: round3(jaccard),
				})
			}
		}
	}
	sort.Slice(signals, func(i, j int) bool {
		if signals[i].A != signals[j].A {
			return signals[i].A < signals[j].A
		}
		return signals[i].B < signals[j].B
	})
	return signals
}

func intersection(a, b []string) []string {
	set := map[string]bool{}
	for _, v := range b {
		set[v] = true
	}
	var out []string
	for _, v := range a {
		if set[v] {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func round3(v float64) float64 {
	return float64(int(v*1000+0.5)) / 1000
}

// Guidelines tells the proposing agent how to author the proposal JSON. It
// is embedded in every data package so the package is self-describing.
const Guidelines = `You are the gardener. Author a proposal JSON with {"author","reason","operations":[...]}.
Operations (node references are UUIDv7 or slug path; paths resolve against the tree state after earlier ops):
- {"op":"move_node","node":"x","target":"new-parent","reason":"..."}
- {"op":"move_entry","entry":"UUID","target":"host-node","reason":"..."}
- {"op":"retag_node","node":"x","add":["tag"],"remove":["tag"],"reason":"..."}
- {"op":"revise_node","node":"x","title":"...","summary":"...","reason":"..."}   (omit a field to keep it)
- {"op":"create_node","target":"parent","kind":"project|category","slug":"...","title":"...","summary":"...","tags_delta":[...],"sensitivity":"inherit|public|internal|private","reason":"..."}
- {"op":"archive_node","node":"x","reason":"..."}   (descendants keep their own status)
Split = create_node + moves; merge = moves + archive_node. Stay within depth 4 and 8 children per node.
Rules enforced by memoryd gardener apply: no cycles, no slug conflicts, tags_delta never repeats inherited tags,
entries only move to active project/global nodes, sensitivity never weakens, root/global cannot move or archive.
Run "memoryd gardener apply --dry-run" first; a human approves the real apply. Propose only changes the signals justify.`
