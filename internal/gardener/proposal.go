// Package gardener implements the offline tree-gardening loop of M3: it
// packages repository analysis for an agent to reason over, then validates
// and applies the agent's tree-restructuring proposal. Plan is a pure
// in-memory simulation that emits concrete steps; the store package executes
// them under the single-writer lock with whole-repo validation, an index
// rebuild, one git commit, and rollback on any surprise.
package gardener

import (
	"fmt"
	"sort"
	"strings"

	"github.com/li-ziyin/memorytree/internal/model"
	"github.com/li-ziyin/memorytree/internal/validate"
)

// Operation kinds. split and merge are not primitive ops: a split is
// create_node + move_node/move_entry, a merge is move_node/move_entry +
// archive_node.
const (
	OpMoveNode    = "move_node"
	OpMoveEntry   = "move_entry"
	OpRetagNode   = "retag_node"
	OpReviseNode  = "revise_node"
	OpCreateNode  = "create_node"
	OpArchiveNode = "archive_node"
)

// MaxOperations bounds one proposal so a runaway agent cannot propose
// unbounded work.
const MaxOperations = 200

// Stable rule codes reported by Plan. They guard the format v1 invariants
// (V-KIND, V-SLUG, V-TAG, V-SENS, V-PARENT) against tree restructuring.
const (
	CodeMalformed  = "G-OP-1"
	CodeNodeGone   = "G-NODE-1"
	CodeEntryGone  = "G-ENTRY-1"
	CodeCycle      = "G-CYCLE-1"
	CodeKind       = "G-KIND-1"
	CodeSlug       = "G-SLUG-1"
	CodeSens       = "G-SENS-1"
	CodeSensEntry  = "G-SENS-2"
	CodeTag        = "G-TAG-1"
	CodeTagRemove  = "G-TAG-2"
	CodeKindNew    = "G-KIND-2"
	CodeKindEntry  = "G-KIND-3"
	CodeKindArch   = "G-KIND-4"
	CodeNodeStat   = "G-NODE-2"
	CodeContent    = "G-CONTENT-1"
	CodeLimit      = "G-LIMIT-1"
	CodeDepthWarn  = "G-DEPTH-1"  // warning
	CodeBranchWarn = "G-BRANCH-1" // warning
)

// Operation is one proposed tree change. Node references (node, target) are
// UUIDv7 or slug path; a path refers to the simulated tree state at that
// point, so a proposal can create a node and then move things into it.
type Operation struct {
	Op     string `json:"op"`
	Node   string `json:"node,omitempty"`   // move/retag/revise/archive target
	Entry  string `json:"entry,omitempty"`  // move_entry target (UUIDv7)
	Target string `json:"target,omitempty"` // new parent (move_node), host node (move_entry/create_node)

	// retag_node
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`

	// revise_node (omit a field to keep the current value) / create_node
	Title     string   `json:"title,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	Kind      string   `json:"kind,omitempty"`       // create_node: project | category
	Slug      string   `json:"slug,omitempty"`       // create_node
	TagsDelta []string `json:"tags_delta,omitempty"` // create_node
	// Sensitivity of a created node; empty means inherit the parent's level.
	Sensitivity string `json:"sensitivity,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// Proposal is the agent-authored change set.
type Proposal struct {
	Author     string      `json:"author"`
	Reason     string      `json:"reason"`
	Operations []Operation `json:"operations"`
}

// NodeRef identifies a node for the executor: by real ID when the node
// exists in the snapshot, or by path when an earlier operation of the same
// proposal created it (its UUID is minted at execution time).
type NodeRef struct {
	ID   string `json:"id,omitempty"`
	Path string `json:"path,omitempty"`
}

// Step is one concrete, executable change derived from the simulation. All
// paths are repository-relative POSIX paths; FromPath/ToPath depend on the
// op (node directory for move_node, entry file for move_entry, created
// directory for create_node).
type Step struct {
	Op    string  `json:"op"`
	Node  NodeRef `json:"node"`
	Entry string  `json:"entry,omitempty"`

	FromPath string `json:"from_path,omitempty"`
	ToPath   string `json:"to_path,omitempty"`

	Parent NodeRef `json:"parent,omitempty"` // move_node new parent / create_node parent

	Add    []string `json:"add,omitempty"`    // retag_node, normalized
	Remove []string `json:"remove,omitempty"` // retag_node

	Title      string   `json:"title,omitempty"`
	Summary    string   `json:"summary,omitempty"`
	HasTitle   bool     `json:"has_title,omitempty"`
	HasSummary bool     `json:"has_summary,omitempty"`
	Kind       string   `json:"kind,omitempty"`       // create_node
	Slug       string   `json:"slug,omitempty"`       // create_node
	TagsDelta  []string `json:"tags_delta,omitempty"` // create_node, normalized
	// Sensitivity of a created node, already resolved (never empty).
	Sensitivity string `json:"sensitivity,omitempty"`
	// DropTags of a move_entry: entry delta tags the target node already
	// provides; the executor removes them from the entry (V-TAG-2).
	DropTags []string `json:"drop_tags,omitempty"`
}

// OpReport is the verdict for one operation.
type OpReport struct {
	Index    int      `json:"index"`
	Op       string   `json:"op"`
	Target   string   `json:"target"` // the id or path the op touches
	Status   string   `json:"status"` // "applied" (in simulation) or "blocked"
	Code     string   `json:"code,omitempty"`
	Message  string   `json:"message,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Detail   string   `json:"detail,omitempty"` // human summary of the simulated change
}

// TagDiff is the effective-tag change of one node caused by the proposal.
type TagDiff struct {
	Path    string   `json:"path"`
	Before  []string `json:"before"`
	After   []string `json:"after"`
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
}

// Report is the full simulation verdict.
type Report struct {
	OK           bool       `json:"ok"`
	Ops          []OpReport `json:"ops"`
	Steps        []Step     `json:"steps,omitempty"` // executable only when OK
	TagDiffs     []TagDiff  `json:"tag_diffs"`
	Affected     int        `json:"affected"` // nodes and entries whose placement changed
	MovedEntries int        `json:"moved_entries"`
	Warnings     []string   `json:"warnings"`
}

// simNode is the mutable simulation state for one node.
type simNode struct {
	id          string
	parent      string // parent id, "" for root
	kind        string
	slug        string
	title       string
	summary     string
	status      string
	sensitivity string
	delta       map[string]bool
	effective   map[string]bool
}

// simState is the whole simulated tree. Effective tags are recomputed from
// scratch after every operation so later ops validate against reality.
type simState struct {
	nodes            map[string]*simNode
	order            []string // stable iteration order (snapshot path order, then created)
	rootID           string
	entryNode        map[string]string
	entryFile        map[string]string // entry id -> repo-relative file path
	entrySensitivity map[string]string
	entryDelta       map[string][]string // entry id -> own tags_delta
}

func newSimState(snap *validate.Snapshot) *simState {
	st := &simState{
		nodes:            map[string]*simNode{},
		entryNode:        map[string]string{},
		entryFile:        map[string]string{},
		entrySensitivity: map[string]string{},
		entryDelta:       map[string][]string{},
	}
	for _, n := range snap.Nodes {
		delta := map[string]bool{}
		for _, tag := range n.TagsDelta {
			delta[tag] = true
		}
		st.nodes[n.ID] = &simNode{
			id: n.ID, parent: n.ParentID, kind: n.Kind, slug: n.Slug,
			title: n.Title, summary: n.Summary, status: n.Status,
			sensitivity: n.Sensitivity, delta: delta,
		}
		if n.Kind == model.KindRoot {
			st.rootID = n.ID
		}
		st.order = append(st.order, n.ID)
	}
	for _, e := range snap.Entries {
		st.entryNode[e.ID] = e.NodeID
		st.entryFile[e.ID] = e.Path
		st.entrySensitivity[e.ID] = e.Sensitivity
		st.entryDelta[e.ID] = e.TagsDelta
	}
	st.recompute()
	return st
}

// recompute derives every node's effective tag set from parent chains.
func (st *simState) recompute() {
	var visit func(id string, inherited map[string]bool)
	visit = func(id string, inherited map[string]bool) {
		n := st.nodes[id]
		eff := make(map[string]bool, len(inherited)+len(n.delta))
		for tag := range inherited {
			eff[tag] = true
		}
		for tag := range n.delta {
			eff[tag] = true
		}
		n.effective = eff
		for _, childID := range st.order {
			if st.nodes[childID].parent == id {
				visit(childID, eff)
			}
		}
	}
	visit(st.rootID, map[string]bool{})
}

func (st *simState) path(id string) string {
	var parts []string
	cur := st.nodes[id]
	for cur != nil && cur.id != st.rootID {
		parts = append([]string{cur.slug}, parts...)
		cur = st.nodes[cur.parent]
	}
	if len(parts) == 0 {
		return "."
	}
	return strings.Join(parts, "/")
}

func (st *simState) depth(id string) int {
	d := 0
	cur := st.nodes[id]
	for cur != nil && cur.id != st.rootID {
		d++
		cur = st.nodes[cur.parent]
	}
	return d
}

func (st *simState) children(parentID string) []*simNode {
	var out []*simNode
	for _, id := range st.order {
		if st.nodes[id].parent == parentID {
			out = append(out, st.nodes[id])
		}
	}
	return out
}

func (st *simState) isDescendant(candidate, of string) bool {
	cur := st.nodes[candidate]
	for cur != nil && cur.id != st.rootID {
		if cur.parent == of {
			return true
		}
		cur = st.nodes[cur.parent]
	}
	return false
}

// ref builds the executor reference for a simulated node: real ID for
// snapshot nodes, path for nodes this proposal creates.
func (st *simState) ref(id string) NodeRef {
	if strings.HasPrefix(id, "new:") {
		return NodeRef{Path: st.path(id)}
	}
	return NodeRef{ID: id, Path: st.path(id)}
}

// resolve finds a node by UUIDv7 or by slug path in the simulated tree.
func (st *simState) resolve(ref string) (*simNode, bool) {
	ref = strings.TrimSpace(strings.Trim(ref, "/"))
	if ref == "" || ref == "." {
		return st.nodes[st.rootID], true
	}
	if n, ok := st.nodes[ref]; ok {
		return n, true
	}
	for _, id := range st.order {
		if st.path(id) == ref {
			return st.nodes[id], true
		}
	}
	return nil, false
}

// canHostChildren reports whether a node may hold child nodes (V-KIND).
func canHostChildren(n *simNode) bool {
	return n.kind == model.KindRoot ||
		(n.kind == model.KindCategory && n.slug != model.GlobalSlug)
}

// apply simulates one operation and, on success, emits its executable step.
func (st *simState) apply(op Operation) (code, msg string, warnings []string, detail string, step Step) {
	switch op.Op {
	case OpMoveNode:
		return st.applyMoveNode(op)
	case OpMoveEntry:
		return st.applyMoveEntry(op)
	case OpRetagNode:
		return st.applyRetagNode(op)
	case OpReviseNode:
		return st.applyReviseNode(op)
	case OpCreateNode:
		return st.applyCreateNode(op)
	case OpArchiveNode:
		return st.applyArchiveNode(op)
	default:
		return CodeMalformed, fmt.Sprintf("unknown op %q", op.Op), nil, "", Step{}
	}
}

func (st *simState) applyMoveNode(op Operation) (string, string, []string, string, Step) {
	node, ok := st.resolve(op.Node)
	if !ok {
		return CodeNodeGone, fmt.Sprintf("node %q does not exist", op.Node), nil, "", Step{}
	}
	parent, ok := st.resolve(op.Target)
	if !ok {
		return CodeNodeGone, fmt.Sprintf("new parent %q does not exist", op.Target), nil, "", Step{}
	}
	if node.kind == model.KindRoot {
		return CodeKind, "the root node cannot move", nil, "", Step{}
	}
	if parent.id == node.id || st.isDescendant(parent.id, node.id) {
		return CodeCycle, fmt.Sprintf("cannot move %s under itself or its descendant %s", st.path(node.id), st.path(parent.id)), nil, "", Step{}
	}
	if !canHostChildren(parent) {
		return CodeKind, fmt.Sprintf("%s (%s) cannot hold child nodes", st.path(parent.id), parent.kind), nil, "", Step{}
	}
	for _, child := range st.children(parent.id) {
		if child.slug == node.slug {
			return CodeSlug, fmt.Sprintf("slug %q already exists under %s", node.slug, st.path(parent.id)), nil, "", Step{}
		}
	}
	if rank(node.sensitivity) < rank(parent.sensitivity) {
		return CodeSens, fmt.Sprintf("%s is %s, weaker than new parent %s (%s)",
			st.path(node.id), node.sensitivity, st.path(parent.id), parent.sensitivity), nil, "", Step{}
	}
	oldPath := st.path(node.id)
	node.parent = parent.id
	st.recompute()
	newPath := st.path(node.id)
	var warns []string
	if d := st.depth(node.id); d > 4 {
		warns = append(warns, fmt.Sprintf("depth %d exceeds the recommended 4 (%s)", d, CodeDepthWarn))
	}
	if n := len(st.children(parent.id)); n > 8 {
		warns = append(warns, fmt.Sprintf("%s now has %d children, more than the recommended 8 (%s)", st.path(parent.id), n, CodeBranchWarn))
	}
	step := Step{Op: OpMoveNode, Node: st.ref(node.id), Parent: st.ref(parent.id), FromPath: oldPath, ToPath: newPath}
	return "", "", warns, fmt.Sprintf("%s -> %s", oldPath, newPath), step
}

func (st *simState) applyMoveEntry(op Operation) (string, string, []string, string, Step) {
	hostID, ok := st.entryNode[op.Entry]
	if !ok {
		return CodeEntryGone, fmt.Sprintf("entry %q does not exist", op.Entry), nil, "", Step{}
	}
	target, ok := st.resolve(op.Target)
	if !ok {
		return CodeNodeGone, fmt.Sprintf("host node %q does not exist", op.Target), nil, "", Step{}
	}
	if target.kind != model.KindProject && target.slug != model.GlobalSlug {
		return CodeKindEntry, fmt.Sprintf("%s (%s) cannot hold entries; use a project or global node", st.path(target.id), target.kind), nil, "", Step{}
	}
	if target.status != model.StatusActive {
		return CodeNodeStat, fmt.Sprintf("%s is %s; reactivate it before moving entries", st.path(target.id), target.status), nil, "", Step{}
	}
	if rank(st.entrySensitivity[op.Entry]) < rank(target.sensitivity) {
		return CodeSensEntry, fmt.Sprintf("entry %s is %s, weaker than %s (%s)",
			op.Entry, st.entrySensitivity[op.Entry], st.path(target.id), target.sensitivity), nil, "", Step{}
	}
	// Tags the target node already provides become redundant in the entry's
	// own delta (V-TAG-2); the move drops them so effective tags stay equal.
	var drop []string
	for _, tag := range st.entryDelta[op.Entry] {
		if target.effective[tag] {
			drop = append(drop, tag)
		}
	}
	oldHost := st.path(hostID)
	st.entryNode[op.Entry] = target.id
	st.entryDelta[op.Entry] = minusSet(st.entryDelta[op.Entry], target.effective)
	step := Step{
		Op: OpMoveEntry, Entry: op.Entry,
		Node:     st.ref(hostID),
		Parent:   st.ref(target.id),
		FromPath: st.entryFile[op.Entry],
		ToPath:   st.path(target.id),
		DropTags: drop,
	}
	detail := fmt.Sprintf("%s: %s -> %s", op.Entry, oldHost, st.path(target.id))
	if len(drop) > 0 {
		detail += fmt.Sprintf(" (delta drops redundant %s)", strings.Join(drop, ","))
	}
	return "", "", nil, detail, step
}

func (st *simState) applyRetagNode(op Operation) (string, string, []string, string, Step) {
	node, ok := st.resolve(op.Node)
	if !ok {
		return CodeNodeGone, fmt.Sprintf("node %q does not exist", op.Node), nil, "", Step{}
	}
	delta := make(map[string]bool, len(node.delta))
	for tag := range node.delta {
		delta[tag] = true
	}
	for _, tag := range op.Remove {
		if !delta[tag] {
			return CodeTagRemove, fmt.Sprintf("tag %q is not in %s own tags_delta (it may be inherited)", tag, st.path(node.id)), nil, "", Step{}
		}
		delete(delta, tag)
	}
	// Everything effective on the parent chain is inherited by this node.
	inherited := st.nodes[node.parent].effective
	var addNormalized []string
	for _, raw := range op.Add {
		tag, errMsg := normalizeTag(raw)
		if errMsg != "" {
			return CodeTag, errMsg, nil, "", Step{}
		}
		if inherited[tag] || delta[tag] {
			return CodeTag, fmt.Sprintf("tag %q is already inherited or already present on %s", tag, st.path(node.id)), nil, "", Step{}
		}
		delta[tag] = true
		addNormalized = append(addNormalized, tag)
	}
	node.delta = delta
	st.recompute()
	step := Step{Op: OpRetagNode, Node: st.ref(node.id), Add: addNormalized, Remove: op.Remove}
	return "", "", nil, fmt.Sprintf("%s tags_delta -> %s", st.path(node.id), strings.Join(sortedKeys(delta), ",")), step
}

func (st *simState) applyReviseNode(op Operation) (string, string, []string, string, Step) {
	node, ok := st.resolve(op.Node)
	if !ok {
		return CodeNodeGone, fmt.Sprintf("node %q does not exist", op.Node), nil, "", Step{}
	}
	title := node.title
	if op.Title != "" {
		title = op.Title
	}
	summary := node.summary
	if op.Summary != "" {
		summary = op.Summary
	}
	if strings.TrimSpace(title) == "" || len([]rune(title)) > 120 {
		return CodeContent, "title must be 1-120 characters", nil, "", Step{}
	}
	if strings.TrimSpace(summary) == "" || len([]rune(summary)) > 500 {
		return CodeContent, "summary must be 1-500 characters", nil, "", Step{}
	}
	parts := []string{}
	if op.Title != "" && op.Title != node.title {
		parts = append(parts, fmt.Sprintf("title %q -> %q", node.title, title))
	}
	if op.Summary != "" && op.Summary != node.summary {
		parts = append(parts, "summary updated")
	}
	step := Step{
		Op: OpReviseNode, Node: st.ref(node.id),
		Title: title, HasTitle: op.Title != "",
		Summary: summary, HasSummary: op.Summary != "",
	}
	node.title, node.summary = title, summary
	if len(parts) == 0 {
		parts = append(parts, "no field changed")
	}
	return "", "", nil, strings.Join(parts, "; "), step
}

func (st *simState) applyCreateNode(op Operation) (string, string, []string, string, Step) {
	parent, ok := st.resolve(op.Target)
	if !ok {
		return CodeNodeGone, fmt.Sprintf("parent %q does not exist", op.Target), nil, "", Step{}
	}
	if !canHostChildren(parent) {
		return CodeKind, fmt.Sprintf("%s (%s) cannot hold child nodes", st.path(parent.id), parent.kind), nil, "", Step{}
	}
	if op.Kind != model.KindProject && op.Kind != model.KindCategory {
		return CodeKindNew, "kind must be project or category", nil, "", Step{}
	}
	if !validSlug(op.Slug) {
		return CodeSlug, fmt.Sprintf("slug %q is not valid", op.Slug), nil, "", Step{}
	}
	for _, child := range st.children(parent.id) {
		if child.slug == op.Slug {
			return CodeSlug, fmt.Sprintf("slug %q already exists under %s", op.Slug, st.path(parent.id)), nil, "", Step{}
		}
	}
	if strings.TrimSpace(op.Title) == "" || len([]rune(op.Title)) > 120 {
		return CodeContent, "title must be 1-120 characters", nil, "", Step{}
	}
	if strings.TrimSpace(op.Summary) == "" || len([]rune(op.Summary)) > 500 {
		return CodeContent, "summary must be 1-500 characters", nil, "", Step{}
	}
	sensitivity := op.Sensitivity
	if sensitivity == "" {
		sensitivity = parent.sensitivity
	}
	if !validSensitivity(sensitivity) {
		return CodeSens, fmt.Sprintf("sensitivity %q is not public, internal, or private", sensitivity), nil, "", Step{}
	}
	delta := map[string]bool{}
	var tagsNormalized []string
	for _, raw := range op.TagsDelta {
		tag, errMsg := normalizeTag(raw)
		if errMsg != "" {
			return CodeTag, errMsg, nil, "", Step{}
		}
		if parent.effective[tag] || delta[tag] {
			return CodeTag, fmt.Sprintf("tag %q is inherited from %s or duplicated", tag, st.path(parent.id)), nil, "", Step{}
		}
		delta[tag] = true
		tagsNormalized = append(tagsNormalized, tag)
	}
	// Synthetic id: created nodes are referenced by path in later ops.
	id := "new:" + st.path(parent.id) + "/" + op.Slug
	st.nodes[id] = &simNode{
		id: id, parent: parent.id, kind: op.Kind, slug: op.Slug,
		title: op.Title, summary: op.Summary, status: model.StatusActive,
		sensitivity: sensitivity, delta: delta,
	}
	st.order = append(st.order, id)
	st.recompute()
	var warns []string
	if d := st.depth(id); d > 4 {
		warns = append(warns, fmt.Sprintf("depth %d exceeds the recommended 4 (%s)", d, CodeDepthWarn))
	}
	step := Step{
		Op: OpCreateNode, Node: NodeRef{Path: st.path(id)}, Parent: st.ref(parent.id),
		ToPath: st.path(id), Kind: op.Kind, Slug: op.Slug,
		Title: op.Title, Summary: op.Summary,
		TagsDelta: tagsNormalized, Sensitivity: sensitivity,
	}
	return "", "", warns, fmt.Sprintf("created %s %s", op.Kind, st.path(id)), step
}

func (st *simState) applyArchiveNode(op Operation) (string, string, []string, string, Step) {
	node, ok := st.resolve(op.Node)
	if !ok {
		return CodeNodeGone, fmt.Sprintf("node %q does not exist", op.Node), nil, "", Step{}
	}
	if node.kind == model.KindRoot || node.slug == model.GlobalSlug {
		return CodeKindArch, "the root and global nodes cannot be archived", nil, "", Step{}
	}
	node.status = model.StatusArchived
	step := Step{Op: OpArchiveNode, Node: st.ref(node.id)}
	return "", "", nil, fmt.Sprintf("%s -> archived (descendants keep their own status)", st.path(node.id)), step
}

// Plan simulates the proposal against the snapshot without touching the
// file system. Every operation is validated against the state left by the
// previous ones, so chained operations (create + move) work. When every
// operation passes, Report.Steps carries the executable plan; any blocked
// operation rejects the whole proposal and Steps stays empty.
func Plan(snap *validate.Snapshot, p *Proposal) *Report {
	report := &Report{OK: true}
	if len(p.Operations) > MaxOperations {
		report.OK = false
		report.Ops = append(report.Ops, OpReport{
			Index: 0, Op: "proposal", Status: "blocked",
			Code: CodeLimit, Message: fmt.Sprintf("proposal has %d operations, at most %d", len(p.Operations), MaxOperations),
		})
		return report
	}
	if len(p.Operations) == 0 {
		report.Warnings = append(report.Warnings, "proposal has no operations")
		return report
	}

	st := newSimState(snap)
	affected := map[string]bool{}
	for i, op := range p.Operations {
		opr := OpReport{Index: i, Op: op.Op, Target: opTarget(op)}
		code, msg, warnings, detail, step := st.apply(op)
		opr.Warnings = warnings
		opr.Detail = detail
		if code != "" {
			opr.Status = "blocked"
			opr.Code = code
			opr.Message = msg
			report.OK = false
			report.Ops = append(report.Ops, opr)
			// Keep simulating so the report can list every problem at once.
			continue
		}
		opr.Status = "applied"
		report.Ops = append(report.Ops, opr)
		report.Steps = append(report.Steps, step)
		switch op.Op {
		case OpMoveNode, OpRetagNode, OpReviseNode, OpArchiveNode, OpCreateNode:
			affected[op.Node] = true
		case OpMoveEntry:
			affected[op.Entry] = true
			report.MovedEntries++
		}
	}

	report.TagDiffs = computeTagDiffs(snap, st)
	report.Affected = len(affected)

	// Structural warnings over the final state.
	for _, id := range st.order {
		if d := st.depth(id); d > 4 {
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("%s: depth %d exceeds the recommended 4 (%s)", st.path(id), d, CodeDepthWarn))
		}
	}
	countedByParent := map[string]int{}
	for _, id := range st.order {
		countedByParent[st.nodes[id].parent]++
	}
	for parentID, count := range countedByParent {
		if count > 8 && parentID != "" {
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("%s: %d children exceed the recommended 8 (%s)", st.path(parentID), count, CodeBranchWarn))
		}
	}
	return report
}

// Check is Plan without the steps.
func Check(snap *validate.Snapshot, p *Proposal) *Report {
	report := Plan(snap, p)
	report.Steps = nil
	return report
}

func opTarget(op Operation) string {
	switch op.Op {
	case OpMoveEntry:
		return op.Entry
	case OpCreateNode:
		return op.Target + "/" + op.Slug
	default:
		return op.Node
	}
}

func computeTagDiffs(snap *validate.Snapshot, st *simState) []TagDiff {
	before := map[string][]string{}
	for _, n := range snap.Nodes {
		before[n.ID] = n.Tags
	}
	var diffs []TagDiff
	for _, id := range st.order {
		node := st.nodes[id]
		if strings.HasPrefix(id, "new:") {
			continue
		}
		after := sortedKeys(node.effective)
		b := before[id]
		if equalSlices(b, after) {
			continue
		}
		added, removed := diffSets(b, after)
		diffs = append(diffs, TagDiff{Path: st.path(id), Before: b, After: after, Added: added, Removed: removed})
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Path < diffs[j].Path })
	return diffs
}

func entrySensitivities(snap *validate.Snapshot) map[string]string {
	out := make(map[string]string, len(snap.Entries))
	for _, e := range snap.Entries {
		out[e.ID] = e.Sensitivity
	}
	return out
}

func rank(level string) int { return model.SensitivityRank(level) }

func minusSet(tags []string, drop map[string]bool) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		if !drop[tag] {
			out = append(out, tag)
		}
	}
	return out
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func diffSets(before, after []string) (added, removed []string) {
	bset := map[string]bool{}
	for _, v := range before {
		bset[v] = true
	}
	aset := map[string]bool{}
	for _, v := range after {
		aset[v] = true
	}
	for _, v := range after {
		if !bset[v] {
			added = append(added, v)
		}
	}
	for _, v := range before {
		if !aset[v] {
			removed = append(removed, v)
		}
	}
	return added, removed
}
