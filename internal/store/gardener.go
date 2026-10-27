package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/li-ziyin/memorytree/internal/docfile"
	"github.com/li-ziyin/memorytree/internal/format"
	"github.com/li-ziyin/memorytree/internal/gardener"
	"github.com/li-ziyin/memorytree/internal/gitio"
	"github.com/li-ziyin/memorytree/internal/ids"
	"github.com/li-ziyin/memorytree/internal/model"
	"github.com/li-ziyin/memorytree/internal/validate"
)

// ErrProposalRejected means the proposal violates a G- rule; nothing was
// written and the accompanying Report lists every blocked operation.
var ErrProposalRejected = errors.New("gardener proposal rejected")

// GardenerOptions selects dry-run or real application.
type GardenerOptions struct {
	DryRun bool
}

// GardenerResult is the outcome of a gardener apply.
type GardenerResult struct {
	DryRun     bool             `json:"dry_run"`
	Report     *gardener.Report `json:"report"`
	Generation int64            `json:"generation"`
	Committed  bool             `json:"committed"`
	// Changed lists the repository-relative paths touched (old and new).
	Changed []string `json:"changed,omitempty"`
}

// ApplyGardenerProposal simulates the proposal against the repository and,
// when every operation passes and DryRun is false, executes it under the
// single-writer lock: apply steps, re-validate the whole tree, rebuild the
// index, and create exactly one git commit. A failed post-apply validation
// rolls every mutation back, so a rejected or failed proposal leaves the
// tree untouched.
func ApplyGardenerProposal(dataDir string, p *gardener.Proposal, opts GardenerOptions) (*GardenerResult, error) {
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}

	if opts.DryRun {
		_, snap, err := validateSnapshot(root)
		if err != nil {
			return nil, err
		}
		report := gardener.Plan(snap, p)
		if !report.OK {
			return &GardenerResult{DryRun: true, Report: report}, fmt.Errorf("%w: %d blocked operation(s)", ErrProposalRejected, blockedCount(report))
		}
		return &GardenerResult{DryRun: true, Report: report}, nil
	}

	if err := gitio.Preflight(root); err != nil {
		return nil, err
	}
	lock, err := acquireWriteLockRetried(root)
	if err != nil {
		return nil, err
	}
	defer lock.release()

	_, snap, err := validateSnapshot(root)
	if err != nil {
		return nil, err
	}
	report := gardener.Plan(snap, p)
	if !report.OK {
		return &GardenerResult{Report: report}, fmt.Errorf("%w: %d blocked operation(s)", ErrProposalRejected, blockedCount(report))
	}

	ex := newGardenerExecutor(root, snap)
	if err := ex.run(report.Steps); err != nil {
		ex.rollback()
		return nil, fmt.Errorf("gardener apply failed and was rolled back: %w", err)
	}
	if _, _, err := validateSnapshot(root); err != nil {
		ex.rollback()
		return nil, fmt.Errorf("applied tree failed validation; rolled back: %w", err)
	}

	gen, err := finishWrite(root, ex.changed(), commitMessage(p, len(report.Steps)))
	if err != nil {
		// Same contract as AddEntry: files stay, index refreshed, manual
		// recovery command in the error. The tree itself is valid.
		return nil, err
	}
	return &GardenerResult{
		Report:     report,
		Generation: gen,
		Committed:  true,
		Changed:    ex.changed(),
	}, nil
}

func blockedCount(report *gardener.Report) int {
	blocked := 0
	for _, op := range report.Ops {
		if op.Status == "blocked" {
			blocked++
		}
	}
	return blocked
}

func commitMessage(p *gardener.Proposal, ops int) string {
	reason := strings.TrimSpace(p.Reason)
	if reason == "" {
		reason = "restructure proposal"
	}
	if len([]rune(reason)) > 72 {
		reason = strings.TrimSpace(string([]rune(reason)[:72]))
	}
	return fmt.Sprintf("gardener: %s (%d ops)", reason, ops)
}

// gardenerExecutor executes planned steps mechanically and keeps an undo
// journal so any surprise rolls the tree back to its exact prior state.
type gardenerExecutor struct {
	root    string
	nodeDir map[string]string      // real node ID -> current repo-relative dir
	created map[string]*model.Node // created path -> node with its minted ID
	rels    []string               // repo-relative touched paths
	undos   []func() error         // executed in reverse on rollback
}

func newGardenerExecutor(root string, snap *validate.Snapshot) *gardenerExecutor {
	nodeDir := make(map[string]string, len(snap.Nodes))
	for _, n := range snap.Nodes {
		if n.Kind == model.KindRoot {
			nodeDir[n.ID] = ""
		} else {
			nodeDir[n.ID] = n.Path
		}
	}
	return &gardenerExecutor{
		root:    root,
		nodeDir: nodeDir,
		created: map[string]*model.Node{},
	}
}

func (ex *gardenerExecutor) changed() []string {
	out := make([]string, 0, len(ex.rels))
	seen := map[string]bool{}
	for _, rel := range ex.rels {
		if !seen[rel] {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// resolveNodeID maps a step reference to a real node ID.
func (ex *gardenerExecutor) resolveNodeID(ref gardener.NodeRef) (string, error) {
	if ref.ID != "" {
		return ref.ID, nil
	}
	if node, ok := ex.created[ref.Path]; ok {
		return node.ID, nil
	}
	return "", fmt.Errorf("node %q is neither an existing ID nor a node created by this proposal", ref.Path)
}

// resolveDir maps a step reference to the node's current directory.
func (ex *gardenerExecutor) resolveDir(ref gardener.NodeRef) (string, error) {
	if ref.ID != "" {
		dir, ok := ex.nodeDir[ref.ID]
		if !ok {
			return "", fmt.Errorf("node %s vanished during apply", ref.ID)
		}
		return dir, nil
	}
	if node, ok := ex.created[ref.Path]; ok {
		return ex.nodeDir[node.ID], nil
	}
	return "", fmt.Errorf("node %q is neither an existing ID nor a node created by this proposal", ref.Path)
}

func (ex *gardenerExecutor) abs(rel string) string {
	return filepath.Join(ex.root, filepath.FromSlash(rel))
}

func (ex *gardenerExecutor) run(steps []gardener.Step) error {
	for i, step := range steps {
		var err error
		switch step.Op {
		case gardener.OpMoveNode:
			err = ex.moveNode(step)
		case gardener.OpMoveEntry:
			err = ex.moveEntry(step)
		case gardener.OpRetagNode:
			err = ex.retagNode(step)
		case gardener.OpReviseNode:
			err = ex.reviseNode(step)
		case gardener.OpCreateNode:
			err = ex.createNode(step)
		case gardener.OpArchiveNode:
			err = ex.archiveNode(step)
		case gardener.OpDeprecateEntry:
			err = ex.deprecateEntry(step)
		default:
			err = fmt.Errorf("unknown step op %q", step.Op)
		}
		if err != nil {
			return fmt.Errorf("step %d (%s): %w", i, step.Op, err)
		}
	}
	return nil
}

// rollback replays the undo journal in reverse, best effort.
func (ex *gardenerExecutor) rollback() {
	for i := len(ex.undos) - 1; i >= 0; i-- {
		_ = ex.undos[i]()
	}
}

func (ex *gardenerExecutor) rememberRestore(absPath string, original []byte) {
	ex.undos = append(ex.undos, func() error {
		return os.WriteFile(absPath, original, 0o600)
	})
}

func (ex *gardenerExecutor) rememberRename(from, to string) {
	ex.undos = append(ex.undos, func() error {
		return os.Rename(to, from)
	})
}

// loadNode reads and decodes a node.md, returning the node, the exact
// original bytes, and the body (which rewrite must preserve, spec 7.3).
func (ex *gardenerExecutor) loadNode(dirRel string) (*model.Node, []byte, []byte, error) {
	abs := ex.abs(filepath.Join(filepath.FromSlash(dirRel), "node.md"))
	doc, err := format.ReadFile(abs)
	if err != nil {
		return nil, nil, nil, err
	}
	var node model.Node
	if err := format.Decode(doc.Meta, &node); err != nil {
		return nil, nil, nil, err
	}
	return &node, append([]byte(nil), doc.Raw...), append([]byte(nil), doc.Body...), nil
}

// saveNode rewrites node.md atomically after a mutation and records the
// touched path plus an undo restoring the original bytes. The body is
// carried through unchanged.
func (ex *gardenerExecutor) saveNode(dirRel string, original []byte, body []byte, node *model.Node) error {
	node.UpdatedAt = now().Format(timeFormat)
	data, err := docfile.MarshalFrontmatter(node, body)
	if err != nil {
		return err
	}
	abs := ex.abs(filepath.Join(filepath.FromSlash(dirRel), "node.md"))
	if err := writeFileAtomic(abs, data); err != nil {
		return err
	}
	ex.rememberRestore(abs, original)
	ex.rels = append(ex.rels, filepath.ToSlash(filepath.Join(filepath.FromSlash(dirRel), "node.md")))
	return nil
}

func (ex *gardenerExecutor) moveNode(step gardener.Step) error {
	parentID, err := ex.resolveNodeID(step.Parent)
	if err != nil {
		return err
	}
	node, original, body, err := ex.loadNode(step.FromPath)
	if err != nil {
		return err
	}
	if step.FromPath != step.ToPath {
		fromAbs, toAbs := ex.abs(step.FromPath), ex.abs(step.ToPath)
		if err := os.Rename(fromAbs, toAbs); err != nil {
			return err
		}
		ex.rememberRename(fromAbs, toAbs)
		ex.rels = append(ex.rels, step.FromPath, step.ToPath)
		ex.nodeDir[node.ID] = step.ToPath
	}
	parentCopy := parentID
	node.Parent = &parentCopy
	dir := step.ToPath
	if step.FromPath == step.ToPath {
		dir = step.FromPath
	}
	return ex.saveNode(dir, original, body, node)
}

func (ex *gardenerExecutor) createNode(step gardener.Step) error {
	parentID, err := ex.resolveNodeID(step.Parent)
	if err != nil {
		return err
	}
	dirAbs := ex.abs(step.ToPath)
	if err := os.MkdirAll(dirAbs, 0o700); err != nil {
		return err
	}
	created := &model.Node{
		Schema:      model.NodeSchema,
		ID:          ids.NewUUIDv7(),
		Kind:        step.Kind,
		Parent:      &parentID,
		Slug:        step.Slug,
		Title:       step.Title,
		Summary:     step.Summary,
		TagsDelta:   sortedCopy(step.TagsDelta),
		Aliases:     []string{},
		Status:      model.StatusActive,
		Sensitivity: step.Sensitivity,
		CreatedAt:   now().Format(timeFormat),
		UpdatedAt:   now().Format(timeFormat),
	}
	data, err := docfile.MarshalFrontmatter(created, nil)
	if err != nil {
		return err
	}
	nodeAbs := filepath.Join(dirAbs, "node.md")
	if err := writeFileAtomic(nodeAbs, data); err != nil {
		return err
	}
	dirForUndo := dirAbs
	ex.undos = append(ex.undos, func() error {
		return os.RemoveAll(dirForUndo)
	})
	ex.created[step.ToPath] = created
	ex.nodeDir[created.ID] = step.ToPath
	ex.rels = append(ex.rels, filepath.ToSlash(filepath.Join(filepath.FromSlash(step.ToPath), "node.md")))
	return nil
}

func (ex *gardenerExecutor) moveEntry(step gardener.Step) error {
	targetID, err := ex.resolveNodeID(step.Parent)
	if err != nil {
		return err
	}
	targetDir, err := ex.resolveDir(step.Parent)
	if err != nil {
		return err
	}
	fromAbs := ex.abs(step.FromPath)
	doc, err := format.ReadFile(fromAbs)
	if err != nil {
		return err
	}
	original := append([]byte(nil), doc.Raw...)
	var entry model.Entry
	if err := format.Decode(doc.Meta, &entry); err != nil {
		return err
	}

	memDirAbs := ex.abs(filepath.Join(filepath.FromSlash(targetDir), "memories"))
	if err := os.MkdirAll(memDirAbs, 0o700); err != nil {
		return err
	}
	if err := cleanTempFiles(memDirAbs); err != nil {
		return err
	}
	taken, err := takenNames(memDirAbs)
	if err != nil {
		return err
	}
	name := filepath.Base(fromAbs)
	if taken[name] {
		base := strings.TrimSuffix(name, filepath.Ext(name))
		tail := entry.ID[28:36]
		for n := 1; ; n++ {
			candidate := fmt.Sprintf("%s-%s.md", base, tail)
			if n > 1 {
				candidate = fmt.Sprintf("%s-%s-%d.md", base, tail, n)
			}
			if !taken[candidate] {
				name = candidate
				break
			}
		}
	}

	entry.Node = targetID
	entry.UpdatedAt = now().Format(timeFormat)
	if len(step.DropTags) > 0 {
		drop := make(map[string]bool, len(step.DropTags))
		for _, tag := range step.DropTags {
			drop[tag] = true
		}
		kept := entry.TagsDelta[:0]
		for _, tag := range entry.TagsDelta {
			if !drop[tag] {
				kept = append(kept, tag)
			}
		}
		entry.TagsDelta = kept
	}
	data, err := docfile.MarshalFrontmatter(&entry, doc.Body)
	if err != nil {
		return err
	}
	toAbs := filepath.Join(memDirAbs, name)
	if err := writeFileAtomic(toAbs, data); err != nil {
		return err
	}
	if err := os.Remove(fromAbs); err != nil {
		_ = os.Remove(toAbs)
		return err
	}
	fromRel, toRel := step.FromPath, filepath.ToSlash(filepath.Join(filepath.FromSlash(targetDir), "memories", name))
	fromAbsCopy, toAbsCopy := fromAbs, toAbs
	ex.undos = append(ex.undos, func() error {
		_ = os.Remove(toAbsCopy)
		return os.WriteFile(fromAbsCopy, original, 0o600)
	})
	ex.rels = append(ex.rels, fromRel, toRel)
	return nil
}

func (ex *gardenerExecutor) retagNode(step gardener.Step) error {
	dir, err := ex.resolveDir(step.Node)
	if err != nil {
		return err
	}
	node, original, body, err := ex.loadNode(dir)
	if err != nil {
		return err
	}
	delta := map[string]bool{}
	for _, tag := range node.TagsDelta {
		delta[tag] = true
	}
	for _, tag := range step.Remove {
		delete(delta, tag)
	}
	for _, tag := range step.Add {
		delta[tag] = true
	}
	node.TagsDelta = sortedSetFrom(delta)
	return ex.saveNode(dir, original, body, node)
}

func (ex *gardenerExecutor) reviseNode(step gardener.Step) error {
	dir, err := ex.resolveDir(step.Node)
	if err != nil {
		return err
	}
	node, original, body, err := ex.loadNode(dir)
	if err != nil {
		return err
	}
	if step.HasTitle {
		node.Title = step.Title
	}
	if step.HasSummary {
		node.Summary = step.Summary
	}
	return ex.saveNode(dir, original, body, node)
}

func (ex *gardenerExecutor) archiveNode(step gardener.Step) error {
	dir, err := ex.resolveDir(step.Node)
	if err != nil {
		return err
	}
	node, original, body, err := ex.loadNode(dir)
	if err != nil {
		return err
	}
	node.Status = model.StatusArchived
	return ex.saveNode(dir, original, body, node)
}

// deprecateEntry rewrites one entry's frontmatter to status: deprecated,
// preserving the body verbatim; the undo restores the original bytes.
func (ex *gardenerExecutor) deprecateEntry(step gardener.Step) error {
	abs := ex.abs(step.FromPath)
	doc, err := format.ReadFile(abs)
	if err != nil {
		return err
	}
	var entry model.Entry
	if err := format.Decode(doc.Meta, &entry); err != nil {
		return err
	}
	entry.Status = model.EntryDeprecated
	entry.UpdatedAt = now().Format(timeFormat)
	data, err := docfile.MarshalFrontmatter(&entry, doc.Body)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(abs, data); err != nil {
		return err
	}
	ex.rememberRestore(abs, doc.Raw)
	ex.rels = append(ex.rels, step.FromPath)
	return nil
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func sortedSetFrom(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for tag := range set {
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}
