package index

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSearchSubtreePath(t *testing.T) {
	root := exampleCopy(t)
	mustBuild(t, root, BuildOptions{})

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "category includes descendants", path: "work", want: sseEntryID},
		{name: "project node", path: "work/agentflow", want: sseEntryID},
		{name: "global node", path: "global", want: userEntryID},
		{name: "root includes everything", path: ".", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits := search(t, root, SearchOptions{SubtreePath: tt.path})
			if tt.want == "" {
				if len(hits) != 2 {
					t.Fatalf("path %q returned %d hits, want 2", tt.path, len(hits))
				}
				return
			}
			if len(hits) != 1 || hits[0].ID != tt.want {
				t.Fatalf("path %q returned %+v, want %s", tt.path, hits, tt.want)
			}
		})
	}

	if hits := search(t, root, SearchOptions{SubtreePath: "nope"}); len(hits) != 0 {
		t.Errorf("unknown subtree returned %d hits", len(hits))
	}
}

func TestNodesEntryAndCounts(t *testing.T) {
	root := exampleCopy(t)
	mustBuild(t, root, BuildOptions{})

	ix := mustOpen(t, root)
	defer ix.Close()

	nodes, err := ix.Nodes(context.Background())
	if err != nil {
		t.Fatalf("Nodes: %v", err)
	}
	if len(nodes) != 4 {
		t.Fatalf("Nodes returned %d nodes, want 4", len(nodes))
	}
	var rootNode, global, project *Node
	for i := range nodes {
		switch nodes[i].Path {
		case ".":
			rootNode = &nodes[i]
		case "global":
			global = &nodes[i]
		case "work/agentflow":
			project = &nodes[i]
		}
	}
	if rootNode == nil || rootNode.ParentID != "" || rootNode.Kind != "root" {
		t.Errorf("root node = %+v", rootNode)
	}
	if global == nil || global.Tags == nil || strings.Join(global.Tags, ",") != "global" {
		t.Errorf("global node = %+v", global)
	}
	if project == nil || project.Tags == nil {
		t.Errorf("project node = %+v", project)
	}

	counts, err := ix.EntryCounts(context.Background())
	if err != nil {
		t.Fatalf("EntryCounts: %v", err)
	}
	if got := counts[globalNode]; got.Total != 1 || got.Active != 1 {
		t.Errorf("global counts = %+v, want 1/1", got)
	}
	if got := counts[sseNode]; got.Total != 1 || got.Active != 1 {
		t.Errorf("project counts = %+v, want 1/1", got)
	}

	entry, err := ix.Entry(context.Background(), sseEntryID)
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	if entry.NodeID != sseNode || entry.NodePath != "work/agentflow" {
		t.Errorf("entry node = %s at %s", entry.NodeID, entry.NodePath)
	}
	if !strings.Contains(entry.Body, "按原始字节解码") {
		t.Errorf("entry body = %q", entry.Body)
	}
	if entry.Source.Agent != "zcode" || entry.Source.Kind != "conversation" || entry.Source.Session != "sess_example" {
		t.Errorf("entry source = %+v", entry.Source)
	}
	if entry.SupersededBy != nil || len(entry.Supersedes) != 0 || len(entry.Related) != 0 {
		t.Errorf("unexpected supersession data: %+v", entry)
	}
	if _, err := ix.Entry(context.Background(), "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1bff"); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("unknown entry error = %v, want ErrEntryNotFound", err)
	}
}
