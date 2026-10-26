// Package store is the single write gateway for MemoryTree data
// repositories. Every write goes through the same contract: validate the
// whole repository, validate the input against the format v1 rules, write
// files atomically, and leave the tree valid. Markdown files remain the
// source of truth; git and the index are derived.
package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/li-ziyin/memorytree/internal/ids"
	"github.com/li-ziyin/memorytree/internal/model"
	"github.com/li-ziyin/memorytree/internal/validate"
)

// InputError is a rejected write with the format v1 rule it violated.
type InputError struct {
	Code    string
	Message string
}

func (e *InputError) Error() string { return e.Code + ": " + e.Message }

func inputf(code, format string, args ...any) error {
	return &InputError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ErrInvalidRepository means the repository itself has validation errors and
// must be fixed before anything is written.
var ErrInvalidRepository = errors.New("repository has validation errors")

// SourceInput records where a captured memory came from.
type SourceInput struct {
	Agent   string `json:"agent"`
	Kind    string `json:"kind"`
	Session string `json:"session,omitempty"`
	Ref     string `json:"ref,omitempty"`
}

// EntryInput is a new memory entry to write.
type EntryInput struct {
	// Node is the slug path (work/agentflow) or UUIDv7 of the host node.
	// Empty means the global node.
	Node        string      `json:"node"`
	Type        string      `json:"type"`
	Title       string      `json:"title"`
	Summary     string      `json:"summary"`
	Body        string      `json:"body"`
	TagsDelta   []string    `json:"tags_delta"`
	Sensitivity string      `json:"sensitivity"`
	Status      string      `json:"status"` // active (default) or draft
	Supersedes  []string    `json:"supersedes"`
	Related     []string    `json:"related"`
	Source      SourceInput `json:"source"`
}

// AddResult describes one completed write.
type AddResult struct {
	// ID is the new entry's UUIDv7.
	ID string `json:"id"`
	// Path is the absolute path of the written file.
	Path string `json:"path"`
	// Generation is the index generation after the post-write refresh, or 0
	// when the index was not refreshed in this call.
	Generation int64 `json:"generation"`
	// Committed reports whether a git commit was created.
	Committed bool `json:"committed"`
}

// now is overridable in tests.
var now = func() time.Time { return time.Now().UTC() }

// AddEntry validates the input, writes the entry file atomically, rewrites
// superseded entries, commits to git, and refreshes the index — all under
// the single-writer lock. The repository must already validate clean; the
// write leaves it clean. Concurrent writers fail fast with ErrWriteLocked
// and may retry.
func AddEntry(dataDir string, in EntryInput) (*AddResult, error) {
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	lock, err := acquireWriteLock(root)
	if err != nil {
		return nil, err
	}
	defer lock.release()

	res, snap, err := validate.ValidateWithSnapshot(root)
	if err != nil {
		return nil, err
	}
	if !res.OK() {
		return nil, fmt.Errorf("%w: %d errors; run memoryd validate %s", ErrInvalidRepository, res.Errors(), root)
	}

	node := resolveNode(snap, in.Node)
	if node == nil {
		return nil, inputf("V-NODE-1", "node %q does not exist", in.Node)
	}
	if err := checkNodeAcceptsEntries(node); err != nil {
		return nil, err
	}

	created := now().Format(time.RFC3339)
	entry, err := buildEntry(node, in, created)
	if err != nil {
		return nil, err
	}
	if err := checkSupersedes(snap, entry.Supersedes); err != nil {
		return nil, err
	}

	memDir := filepath.Join(root, filepath.FromSlash(node.Path), "memories")
	path, err := writeEntryFile(memDir, entry, []byte(in.Body))
	if err != nil {
		return nil, err
	}
	return &AddResult{ID: entry.ID, Path: path}, nil
}

// resolveNode finds the host node by UUIDv7 or by slug path; nil when absent.
func resolveNode(snap *validate.Snapshot, ref string) *validate.NodeInfo {
	if ref == "" {
		ref = model.GlobalSlug
	}
	for i := range snap.Nodes {
		n := &snap.Nodes[i]
		if ids.IsUUIDv7(ref) {
			if n.ID == ref {
				return n
			}
			continue
		}
		if strings.EqualFold(n.Path, ref) || ref == "." && n.Kind == model.KindRoot {
			return n
		}
	}
	return nil
}

// checkNodeAcceptsEntries enforces the format rule that entries hang only
// under a project node or the global category, and only on active nodes.
func checkNodeAcceptsEntries(n *validate.NodeInfo) error {
	if n.Kind == model.KindRoot {
		return inputf("V-NODE-2", "the root node cannot hold entries; use a project or global node")
	}
	if n.Kind != model.KindProject && n.Slug != model.GlobalSlug {
		return inputf("V-NODE-2", "node %q (kind %s) cannot hold entries; only project and global nodes can", n.Path, n.Kind)
	}
	if n.Status != model.StatusActive {
		return inputf("V-NODE-3", "node %q is %s; reactivate it before writing entries", n.Path, n.Status)
	}
	return nil
}

func checkSupersedes(snap *validate.Snapshot, supersedes []string) error {
	byID := make(map[string]validate.EntryInfo, len(snap.Entries))
	for _, e := range snap.Entries {
		byID[e.ID] = e
	}
	for _, id := range supersedes {
		target, ok := byID[id]
		if !ok {
			return inputf("V-SUP-1", "supersedes target %s does not exist", id)
		}
		if target.Status != model.StatusActive {
			return inputf("V-SUP-5", "supersedes target %s is %s; superseded entries are terminal", id, target.Status)
		}
		if target.SupersededBy != nil {
			return inputf("V-SUP-2", "supersedes target %s is already superseded by %s", id, *target.SupersededBy)
		}
	}
	return nil
}
