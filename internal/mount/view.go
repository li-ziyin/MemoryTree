package mount

import (
	"github.com/li-ziyin/memorytree/internal/model"
	"github.com/li-ziyin/memorytree/internal/validate"
)

// FromSnapshot builds the scorer's view of a validated repository. Entries
// under nodes that cannot host them are ignored (validation guarantees there
// are none). excludeEntryID removes one entry from every node sample — used
// by the leave-one-out accuracy harness so an entry is never matched
// against itself.
func FromSnapshot(snap *validate.Snapshot, excludeEntryID string) []CandidateNode {
	entriesByNode := make(map[string][]EntryLite, len(snap.Nodes))
	counts := make(map[string]int, len(snap.Nodes))
	for _, e := range snap.Entries {
		if e.ID == excludeEntryID {
			continue
		}
		if e.Status != model.StatusActive {
			continue
		}
		entriesByNode[e.NodeID] = append(entriesByNode[e.NodeID], EntryLite{
			Title:   e.Title,
			Summary: e.Summary,
			Tags:    e.Tags,
		})
		counts[e.NodeID]++
	}
	nodes := make([]CandidateNode, 0, len(snap.Nodes))
	for _, n := range snap.Nodes {
		if n.Kind != model.KindProject && n.Slug != model.GlobalSlug {
			continue
		}
		sample := entriesByNode[n.ID]
		// Cap the similarity sample so a very large node cannot dominate
		// scoring time; the snapshot is path-sorted, so the kept subset is
		// arbitrary but deterministic.
		if len(sample) > 100 {
			sample = sample[:100]
		}
		nodes = append(nodes, CandidateNode{
			ID:          n.ID,
			Path:        n.Path,
			Kind:        n.Kind,
			Slug:        n.Slug,
			Title:       n.Title,
			Summary:     n.Summary,
			Tags:        n.Tags,
			Status:      n.Status,
			Sensitivity: n.Sensitivity,
			Entries:     sample,
		})
	}
	return nodes
}
