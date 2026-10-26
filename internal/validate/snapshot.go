package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/li-ziyin/memorytree/internal/model"
)

// Snapshot is a validated, read-only view of a repository. It is only
// produced when validation has no blocking issues.
type Snapshot struct {
	Root        string
	Repo        model.Repo
	Nodes       []NodeInfo
	Entries     []EntryInfo
	Fingerprint string
}

// NodeInfo is one node with inherited state resolved.
type NodeInfo struct {
	ID          string
	ParentID    string
	Kind        string
	Slug        string
	Path        string
	Title       string
	Summary     string
	Tags        []string
	TagsDelta   []string
	Status      string
	Sensitivity string
	Depth       int
	CreatedAt   string
	UpdatedAt   string
}

// EntryInfo is one memory entry with its effective tags.
type EntryInfo struct {
	ID           string
	NodeID       string
	NodePath     string
	Type         string
	Status       string
	Sensitivity  string
	Title        string
	Summary      string
	Tags         []string
	TagsDelta    []string
	Source       model.Source
	Supersedes   []string
	SupersededBy *string
	Related      []string
	CreatedAt    string
	UpdatedAt    string
	Path         string
	Body         string
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (v *validator) snapshot() *Snapshot {
	snap := &Snapshot{Root: v.root, Repo: *v.repo}

	snap.Nodes = make([]NodeInfo, 0, len(v.nodes))
	for _, rec := range v.nodes {
		n := rec.node
		depth := 0
		for p := rec.parent; p != nil; p = p.parent {
			depth++
		}
		parentID := ""
		if n.Parent != nil {
			parentID = *n.Parent
		}
		snap.Nodes = append(snap.Nodes, NodeInfo{
			ID:          n.ID,
			ParentID:    parentID,
			Kind:        n.Kind,
			Slug:        n.Slug,
			Path:        rec.dir,
			Title:       n.Title,
			Summary:     n.Summary,
			Tags:        sortedSet(rec.effective),
			TagsDelta:   sortedSet(rec.tags),
			Status:      n.Status,
			Sensitivity: n.Sensitivity,
			Depth:       depth,
			CreatedAt:   n.CreatedAt,
			UpdatedAt:   n.UpdatedAt,
		})
	}
	sort.Slice(snap.Nodes, func(i, j int) bool { return snap.Nodes[i].Path < snap.Nodes[j].Path })

	snap.Entries = make([]EntryInfo, 0, len(v.entries))
	for _, rec := range v.entries {
		e := rec.entry
		tags := make(map[string]struct{}, len(rec.host.effective)+len(rec.tags))
		for tag := range rec.host.effective {
			tags[tag] = struct{}{}
		}
		for tag := range rec.tags {
			tags[tag] = struct{}{}
		}
		var supersededBy *string
		if e.SupersededBy != nil {
			id := *e.SupersededBy
			supersededBy = &id
		}
		snap.Entries = append(snap.Entries, EntryInfo{
			ID:           e.ID,
			NodeID:       e.Node,
			NodePath:     rec.host.dir,
			Type:         e.Type,
			Status:       e.Status,
			Sensitivity:  e.Sensitivity,
			Title:        e.Title,
			Summary:      e.Summary,
			Tags:         sortedSet(tags),
			TagsDelta:    sortedSet(rec.tags),
			Source:       e.Source,
			Supersedes:   append([]string(nil), e.Supersedes...),
			SupersededBy: supersededBy,
			Related:      append([]string(nil), e.Related...),
			CreatedAt:    e.CreatedAt,
			UpdatedAt:    e.UpdatedAt,
			Path:         rec.file,
			Body:         rec.body,
		})
	}
	sort.Slice(snap.Entries, func(i, j int) bool { return snap.Entries[i].Path < snap.Entries[j].Path })

	type fileDigest struct {
		path   string
		digest string
	}
	files := make([]fileDigest, 0, len(v.nodes)+len(v.entries)+1)
	files = append(files, fileDigest{path: "memory.yaml", digest: v.repoDigest})
	for _, rec := range v.nodes {
		files = append(files, fileDigest{path: rec.file, digest: rec.digest})
	}
	for _, rec := range v.entries {
		files = append(files, fileDigest{path: rec.file, digest: rec.digest})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })

	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%d:%s%d:%s\n", len(f.path), f.path, len(f.digest), f.digest)
	}
	snap.Fingerprint = hex.EncodeToString(h.Sum(nil))

	return snap
}

func sortedSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for item := range set {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
