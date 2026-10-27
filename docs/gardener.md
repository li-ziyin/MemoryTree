# Gardener: the offline tree-maintenance flow

The gardener is an offline task executed by an agent: read the analysis package → produce a proposal JSON → dry-run validation → human approval → apply. The core process calls no model; the agent owns proposal quality, `memoryd` owns legality.

## Flow

```text
memoryd gardener data <data-dir>          # 1. emit the analysis package (JSON, with a proposal format guide)
the agent reads the package and writes proposal.json   # 2. propose (operation list + rationale)
memoryd gardener apply <data-dir> < proposal.json        # 3. dry-run (default, zero writes)
memoryd gardener apply --apply <data-dir> < proposal.json # 4. apply (after human approval)
```

- Passing step 3 never obligates step 4; application is always human-triggered.
- Any violating operation rejects the whole proposal with zero writes; a repository-wide validation failure after applying rolls everything back — the original tree is untouched.
- Apply = write lock → simulated validation → execution → repository-wide validation → index rebuild → exactly one git commit prefixed `gardener:`.

## Proposal JSON

```json
{
  "author": "agent-name",
  "reason": "one line of motivation (lands in the git commit message)",
  "operations": [
    {"op": "move_node",   "node": "work/agentflow", "target": ".", "reason": "..."},
    {"op": "move_entry",  "entry": "<UUIDv7>", "target": "work/agentflow", "reason": "..."},
    {"op": "retag_node",  "node": "work", "add": ["java"], "remove": ["career"], "reason": "..."},
    {"op": "revise_node", "node": "work/agentflow", "summary": "new summary", "reason": "..."},
    {"op": "create_node", "target": "work", "kind": "project", "slug": "ml",
     "title": "ML", "summary": "machine-learning experiments", "tags_delta": ["ml"], "reason": "..."},
    {"op": "archive_node", "node": "work/old-thing", "reason": "..."}
  ]
}
```

- `node`/`target` take a UUIDv7 or a slug path; paths resolve against the simulated tree after earlier operations, so "create then move into it" works inside one proposal.
- Fields omitted by `revise_node` keep their current values; a `create_node` without `sensitivity` inherits the parent's.
- split = `create_node` + `move_node`/`move_entry`; merge = moves + `archive_node`.
- Cap: 200 operations per proposal.
- `move_entry` automatically drops tags from the entry's `tags_delta` that the target node already provides (V-TAG-2); the step reports `drop_tags`.

## Rule codes (G-)

| Code | Meaning |
|---|---|
| G-OP-1 | unknown operation or malformed step |
| G-NODE-1 / G-ENTRY-1 | referenced node/entry does not exist |
| G-CYCLE-1 | move into itself or a descendant |
| G-KIND-1/2/3/4 | kind limits: project/global may not have children; create builds only project/category; entries enter only project/global; root and global cannot be archived |
| G-SLUG-1 | slug illegal, reserved, or colliding under the target parent |
| G-SENS-1/2 | sensitivity weakening (node toward a new parent / entry toward a new host) |
| G-TAG-1/2 | tag syntax/duplicate/inherited-duplicate / `remove` of a tag not in the node's own delta |
| G-NODE-2 | target node is not active |
| G-CONTENT-1 | title/summary empty or too long |
| G-LIMIT-1 | operation count over the cap |
| G-DEPTH-1 / G-BRANCH-1 | warnings: depth>4 / >8 children under one parent (non-blocking, tool policy) |

## Package signals

`gardener data` reports, per node: path, kind, effective tags and deltas, depth, child count, entry count, timestamps, plus the structural signals `children>8`, `depth>4`, and `empty-project`; it also reports effective-tag overlap between same-parent node pairs (Jaccard ≥ 0.5) as merge/restructure triggers, and aggregates the MCP layer's retrieval-miss log (`query_misses`: the queries that kept finding nothing, with counts and last-seen times) — repeated misses are the third restructuring trigger.
