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

	"github.com/li-ziyin/memorytree/internal/gitio"
	"github.com/li-ziyin/memorytree/internal/ids"
	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/model"
	"github.com/li-ziyin/memorytree/internal/redact"
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
	// Redactions counts credentials replaced with [REDACTED] before the
	// write; the originals never reach disk.
	Redactions int `json:"redactions"`
}

// now is overridable in tests.
var now = func() time.Time { return time.Now().UTC() }

// timeFormat is the canonical timestamp form written into frontmatter.
const timeFormat = time.RFC3339

// validateSnapshot returns a clean snapshot or ErrInvalidRepository.
func validateSnapshot(root string) (*validate.Result, *validate.Snapshot, error) {
	res, snap, err := validate.ValidateWithSnapshot(root)
	if err != nil {
		return nil, nil, err
	}
	if !res.OK() {
		return res, nil, fmt.Errorf("%w: %d errors; run memoryd validate %s", ErrInvalidRepository, res.Errors(), root)
	}
	return res, snap, nil
}

// AddEntry validates the input, writes the entry file atomically, rewrites
// superseded entries, commits to git, and refreshes the index — all under
// the single-writer lock. The repository must already validate clean and be
// a git work tree; the write leaves both clean. Concurrent writers fail
// fast with ErrWriteLocked and may retry.
func AddEntry(dataDir string, in EntryInput) (*AddResult, error) {
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if err := gitio.Preflight(root); err != nil {
		return nil, err
	}
	lock, err := acquireWriteLock(root)
	if err != nil {
		return nil, err
	}
	defer lock.release()

	_, snap, err := validateSnapshot(root)
	if err != nil {
		return nil, err
	}

	// Secrets never reach disk: scan before any validation or write, and
	// report only the count.
	var n int
	redactions := 0
	if in.Title, n = redact.Scan(in.Title); n > 0 {
		redactions += n
	}
	if in.Summary, n = redact.Scan(in.Summary); n > 0 {
		redactions += n
	}
	if in.Body, n = redact.Scan(in.Body); n > 0 {
		redactions += n
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
	relNew := relFromRoot(root, path)
	rels := []string{relNew}
	if len(entry.Supersedes) > 0 {
		supRels, err := supersedeOldEntries(root, snap, relNew, entry.ID, entry.Supersedes)
		if err != nil {
			return nil, err
		}
		rels = append(rels, supRels...)
	}

	message := fmt.Sprintf("memoryd: add %q", entry.Title)
	if len(entry.Supersedes) > 0 {
		message = fmt.Sprintf("memoryd: add %q (supersedes %d)", entry.Title, len(entry.Supersedes))
	}
	gen, err := finishWrite(root, rels, message)
	if err != nil {
		return nil, err
	}
	return &AddResult{ID: entry.ID, Path: path, Generation: gen, Committed: true, Redactions: redactions}, nil
}

// finishWrite validates the freshly written tree, rebuilds the index, and
// commits exactly the written paths. Validation runs before the commit so a
// broken write never enters git; a failed commit keeps files and index and
// returns an error naming the manual recovery command (D10).
func finishWrite(root string, rels []string, message string) (int64, error) {
	build, buildErr := index.Build(root, index.BuildOptions{})
	if buildErr != nil {
		if errors.Is(buildErr, index.ErrInvalidRepository) {
			return 0, fmt.Errorf(
				"files written but the tree no longer validates: %w; inspect with memoryd validate %s and fix or revert",
				buildErr, root)
		}
		// A stale or corrupt index does not invalidate the write itself.
		build = nil
	}
	if err := gitio.Commit(root, rels, message); err != nil {
		return 0, fmt.Errorf(
			"files written but git commit failed: %v; recover with: git -C %s add %s && git -C %s commit -m %q",
			err, root, strings.Join(rels, " "), root, message)
	}
	if build != nil {
		return build.Generation, nil
	}
	return 0, nil
}

// relFromRoot converts an absolute path inside root into a POSIX rel path
// for git.
func relFromRoot(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
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
