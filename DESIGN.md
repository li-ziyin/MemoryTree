# MemoryTree Design Document

- Status: M1 implemented and retrieval acceptance re-verified (2026-10-25: 20/20 complete and first-hit answers; 14,723 vs baseline 67,796 tokens per question in isolated sessions, see [docs/m1-acceptance.md](docs/m1-acceptance.md)); human rating 4.95 confirmed (2026-10-26). Engineering hardening done (2026-10-26): three-OS CI matrix with a schema consistency job, direct parser tests and fuzzing, a scale baseline ([docs/perf-baseline.md](docs/perf-baseline.md)), the `memoryd bench` retrieval regression gate, rune-split FTS for short queries (index schema v2), stale-index warnings and request deadlines. M2 write path done (2026-10-27): `memory_add`/`memory_supersede` (MCP + CLI), single-writer lock with crash recovery, git commits, inbox capture with a distillation prompt, 40 real entries ([docs/m2-acceptance.md](docs/m2-acceptance.md)). M3 mounting and gardener done (2026-10-27): `memory_suggest_mount` (MCP+CLI) with leave-one-out acceptance, `memoryd gardener data/apply` (6 primitives, G- rule codes, dry-run by default, rollback, one commit; see [docs/gardener.md](docs/gardener.md) and [docs/m3-acceptance.md](docs/m3-acceptance.md)). The user-approved restructure landed in the live repository (2026-10-27: single commit `dc95f2d`, 7 nodes 40 entries, bench 21/21 first-hit). Crash-recovery `memoryd doctor` shipped (2026-10-27: detects uncommitted managed changes and crash leftovers, `--fix` recovers with one commit after validation, including index-ahead-of-HEAD states from interrupted manual recovery; never discards data, never resets). Release hardening done (2026-10-27): golangci-lint (`.golangci.yml`) and govulncheck joined `scripts/check.sh` and CI (new lint/vuln/race jobs; Go version follows go.mod); the dependency allowlist test `internal/depcheck` locks go.mod to THIRD_PARTY_NOTICES; `.goreleaser.yaml` + `release.yml` produce cross-platform archives (with license files), SHA-256 checksums, syft SPDX SBOMs, keyless cosign signatures, and draft releases. Upgraded x/text v0.29.0→v0.43.0 to close the reachable GO-2026-5970 (go directive now 1.26). Core documents are English-first with synced Chinese versions (`*.zh-CN.md`); acceptance records stay Chinese.
- Working name: MemoryTree; binary name: `memoryd`
- Language and shape: Go, single binary, stdio by default, no resident daemon
- Data directory: a separate git repository; local development data at `E:\MemoryTreeData`, `~/.memorytree` for other users
- License: Apache-2.0 (code and repository documentation); users' memory data is outside this license

## 1. Problem and goals

Cross-agent memory today is a "shared Markdown scratchpad": ZCode, OpenCode, and Qoder all read one directory, each through its own global rules. It works, but it has no capture, distillation, ownership, freshness, retrieval, or project boundaries; one agent easily mistakes another agent's notes for the current project state.

MemoryTree is a **shared memory layer independent of every agent**:

1. Data is a plain Markdown directory tree — human-readable, human-editable, movable as a whole.
2. The tree is organized by tags; a child's effective tag set always contains its parent's.
3. New memories are distilled by an agent before being written; tree changes are proposed by an agent and executed after human approval.
4. Any MCP-capable agent reads and writes through the same set of tools.
5. Local by default: no network, no telemetry; the core calls no LLM.

Non-goals (V1): cloud sync, multi-user permissions, a GUI, semantic vector retrieval, per-agent adapter bundles, a resident daemon, live automatic reorganization.

## 2. Core decisions

### D1: the Markdown tree is the only truth; SQLite is a rebuildable index

The data repository is itself a git repository. Nodes are directories, node metadata lives in `node.md`, and memory entries live in the project leaves' `memories/*.md`. SQLite holds only the full-text index and derived fields; deleting it loses nothing — everything can be rebuilt from the files. When open-sourced, there is no "your data lives in our database" lock-in.

### D2: tags are stored as deltas; the invariant is guaranteed by structure

Each node stores only its own `tags_delta`; effective tags are the union along the path:

```
/work                 delta {work}        -> {work}
/work/agentflow       delta {java, agent} -> {work, java, agent}
```

"A child's effective tag set ⊇ its parent's" is therefore not maintained by validation — it holds by construction. Moving a project or category changes only `parent` and the directory location; descendants' effective tags are recomputed, no per-file tag rewriting. An audit produces an effective-tag diff of the affected nodes.

This corrects the 2026-10-17 assessment's "reorganization amplifies the tag invariant into a full-path rewrite": execution cost drops from O(files in subtree) to "change one pointer + rebuild the index". The remaining hard question shifts from "how to change" to "where to move, and when" — left to the offline gardener and its triggers.

Constraint: the invariant constrains **effective tags**, not the stored form. Display and retrieval use effective tags uniformly; node files store only deltas.

### D3: the MCP server is the only write gateway

All agents read and write through the local MCP server, never editing the data directory directly. The server is responsible for:

- frontmatter and JSON Schema validation
- deduplication, status transitions, `supersedes` bidirectional consistency
- atomic file writes, SQLite index updates, git commits
- path containment, symlink rejection, secret redaction

V1 speaks stdio only, no local port; logically single-writer, with a cross-process file lock serializing writes. A resident daemon or concurrent clients would arrive later with the same tool surface.

### D4: the tree is for navigation; hybrid search is for hits

Retrieval has two layers:

- `memory_tree`: a compressed tree with node summaries and counts, so an agent can build a sense of direction.
- `memory_search`: SQLite FTS5 (trigram tokenizer for Chinese) plus tag/project/status filters, for precise hits.

V1 has no embeddings. At personal scale, "tree + keyword/tag filtering" is enough; semantic retrieval moves to V2 and gets decided by a real-query comparison. The most important external interface is not `search` but `memory_brief`: within a fixed token budget, return the user profile, current project state, relevant memories, and open questions.

### D5: capture goes through the inbox; distillation is done by an agent

At session end, each agent writes a summary or raw log into `.inbox/<agent>/`. Distillation is done by the current agent (or manually by the user) — the core process never secretly calls an external model. Distilled results become atomic entries via `memory_add`. Raw sessions go to `.archive/`, outside git, mode 0600, for later review or re-distillation.

V1 ships one real pipeline first (Qoder or ZCode preferred), not per-platform adapters. OpenCode session exports, Qoder session reads, and ZCode's existing memory files are all viable input sources.

### D6: the gardener proposes offline; nothing reorganizes in real time

The gardener is a periodic task any agent can run. It reads node summaries, branching factors, and retrieval-failure logs, and proposes `split / merge / move / retag` operations, emitting a git patch with rationale. After the server validates the invariants, a human approves the merge.

Trigger metrics (suggested for V1):

- a category exceeds 8 children
- a class of queries keeps missing (retrieval-failure log)
- two nodes' summaries/tags overlap heavily
- a project concludes or goes inactive

### D7: one Go binary; code and data separated

- One Go binary, no Node/Python runtime; targets: ~30 MB binary, <50 MB resident memory, p95 retrieval <50 ms at 10k entries.
- Dependencies kept to single digits: a hand-written MCP JSON-RPC 2.0 stdio layer (no SDK), a pure-Go SQLite driver, a YAML parser, UUID, and a file lock.
- Code repository and user data repositories are separate. Code is Apache-2.0; users' memory data belongs to the users.
- Releases ship Windows/macOS/Linux builds, checksums, signatures, and an SBOM; CI runs tests, static analysis, and dependency vulnerability scans.

## 3. Overall architecture

```
 ZCode / OpenCode / Qoder / other MCP clients
                    |
                    | MCP over stdio
                    v
              +-----------+
              |  memoryd  |
              |-----------|
              | validate  |
              | store     |----> data repository (Markdown tree + git)
              | index     |----> SQLite (FTS5, rebuildable)
              | brief     |
              +-----------+
                    ^
                    | proposals / approval
              +-----------+
              | gardener  |  (run by an agent, offline)
              +-----------+
```

Write path:

```
Agent -> memory_add -> validate -> atomic file write -> git commit
```

Read path:

```
Agent -> memory_brief / search / tree / get -> read-only SQLite / files
```

## 4. Data model summary

The full specification is [spec/memory-format-v1.md](spec/memory-format-v1.md); JSON Schemas live in `spec/schema/`.

```
E:\MemoryTreeData\
  memory.yaml
  node.md                         # root
  global/
    node.md                       # reserved category; may hold entries
    memories/
      2026-10-20-user-profile.md
  work/
    node.md
    agentflow/
      node.md
      memories/
        2026-10-20-sse-pitfall.md
  .inbox/                         # raw captures, gitignored
  .archive/                       # raw sessions, gitignored, 0600
  .index/                         # SQLite, gitignored, rebuildable
```

- Nodes: `root / category / project`; exactly one root; projects are leaves; categories may contain categories or projects.
- Entries: hang under `project` or `global` nodes; types `user / feedback / project / reference`.
- Identity: UUIDv7 for nodes and entries; directory names are readable slugs, not identity. References use stable IDs, so moving directories never breaks links.
- Tags: `tags_delta` is stored; effective tags = union along the path + the entry's own delta.
- Status: `active / superseded / deprecated / draft`; `supersedes` and `superseded_by` stay consistent in both directions.
- Sensitivity: `public / internal / private`; an entry is never laxer than its node.
- Time: RFC3339 UTC (`Z`); files are UTF-8, LF.

### 4.1 Retrieval index (`memoryd index`)

- Location and permissions: `<data>/.index/index.sqlite`, directory 0700, file 0600 (on Windows, ACLs decide), SQLite WAL; the whole `.index/` is gitignored.
- Build order: full validation first; blocking errors exit 1 without touching the old index. Once clean, the index is built from the validated snapshot.
- Transactionality: drop tables, create, insert, and FTS integrity checks run in one SQLite transaction; on failure it rolls back and the old index stays usable. A corrupt or schema-incompatible index requires an explicit `--rebuild`.
- Metadata: the `meta` table records `schema_version / repo_id / generation / fingerprint / built_at / nodes / entries`. The fingerprint is the SHA-256 over all managed files (`memory.yaml`, `node.md`, `memories/*.md`).
- Idempotence: an unchanged fingerprint without `--rebuild` writes nothing and leaves the generation alone; every real rebuild increments it.
- Tables: `nodes`, `entries`, `entry_tags`, external-content FTS5 tables `entries_fts(title, summary, tags, body, tokenize='trigram')` and the rune-split `entries_fts_chars(title, summary, tags, body, tokenize='unicode61')`. Bodies are stored once in `entries`; `node.md` bodies never enter the index.
- Queries: FTS5 trigram when every term has ≥3 characters (works for Chinese); the `entries_fts_chars` path when any term is shorter — unicode61 makes each CJK character its own token, so a phrase query over space-joined runes behaves like a substring match, with bm25 column weighting (title 8, summary 4, tags 2, body 1). Only `active` entries return by default; filters by node, type, effective tags (AND), and status; results cap at 100. `schema_version` is 2 (the v1 LIKE fallback is gone); an incompatible old index needs an explicit `--rebuild`.

## 5. MCP interface

### 5.1 Tool list (V1)

| Tool | Purpose | Writes |
|---|---|---|
| `memory_brief` | A fixed-token-budget context brief for a task and project | no |
| `memory_search` | Hybrid search returning summaries, IDs, node paths, status | no |
| `memory_get` | Fetch one entry by ID; full text by default, or a bounded excerpt given `query` | no |
| `memory_tree` | Compressed tree with node summaries and counts | no |
| `memory_suggest_mount` | Ranks candidate host nodes for a new entry (tags + text similarity + a `user` prior); the agent decides and passes the node to `memory_add` | no |
| `memory_add` | Add an entry; may declare `supersedes` at the same time (inherit and flatten, V-SUP-4) | yes |
| `memory_supersede` | Link two existing entries into a supersede pair | yes |

Tree restructuring is not exposed over MCP in V1: the gardener runs as a CLI flow (section 7); a `memory_propose_move`-style tool waits for a V1.5 evaluation.

Implementation: a hand-written JSON-RPC 2.0 over stdio (one JSON per line), no MCP SDK; `initialize`, `ping`, `tools/list`, and `tools/call` are supported; notifications get no response; batch requests are unsupported. Protocol versions 2024-11-05, 2025-03-26, and 2025-06-18 are accepted; unknown versions fall back to 2025-06-18. Every tool carries `readOnlyHint` where applicable.

### 5.2 `memory_brief`

Inputs: `task` (required), `project` (node ID or slug path, optional), `budget_tokens` (default 1200).

Output sections:

1. User profile and long-term preferences (`user` entries under `global`)
2. Current project overview (node summary + recent `project` entries)
3. Memories relevant to the task (summaries from `search`)
4. Open questions (active `project`-type entries)
5. Warnings (superseded old conclusions, sensitivity, stale index)

Only summaries and IDs return; the agent calls `memory_get` for full text. The server assembles deterministically; it generates nothing.

The budget rotates across sections: each round adds at most one entry per section, so an early section cannot eat the whole budget. `tokens_estimated` is approximate (0.25 tokens per ASCII character, 1 per non-ASCII) and counts `data` only, not envelope metadata; when the budget runs out the response carries a `truncated` warning.

**`memory_get` excerpt mode**: the default returns the full body. With `query`, the server extracts fragments around term hits, up to `max_chars` (default 1200, range 200–8000); the response's `body` is the excerpt, with `excerpted=true`, `full_chars`, and a warning. When `query` misses the body, the opening fragment returns with a warning; a body no longer than `max_chars` still returns whole. Full-text mode is unchanged.

**Stale-index warnings and request deadlines** (2026-10-26): before every `tools/call` the server re-reads the data repository fingerprint at most once per 30 seconds; on a mismatch with the startup snapshot (changed sources, failed validation, unreadable) it warns in the response instead of silently serving stale data (the warning appears once; a restart resets it). Every request carries a 60-second deadline context that reaches the SQLite queries, paving the way for M2 write-path cancellation.

### 5.3 Responses and errors

Every tool returns an envelope with metadata:

```json
{
  "data": [],
  "meta": {
    "total": 0,
    "tokens_estimated": 0,
    "index_generation": 0,
    "warnings": []
  }
}
```

Memory content is labelled `"trust": "untrusted-history"`, reminding clients this is historical data, not executable instructions. Errors use stable codes (e.g. `MEMORY_NOT_FOUND`, `NODE_NOT_FOUND`, `INVALID_ARGUMENTS`, `INDEX_ERROR`) with an actionable suggestion; tool errors surface via MCP's `isError: true` and repeat `code`, `message`, and `suggestion` in `structuredContent.error`.

### 5.4 Concurrency and consistency

- Reads: SQLite WAL allows concurrent multi-process reads; the index carries a `generation`. The read-only MCP server validates the repository at startup and refreshes the index by file fingerprint, then works from that snapshot for the session; after hand edits the server restarts. A corrupt or incompatible index never rebuilds automatically — the server refuses to start and points at `memoryd index --rebuild`.
- Writes: serialized by a cross-process file lock; files are written via a same-directory temp file + fsync + rename; index updates and the git commit share one critical section (M2).
- Crash recovery: a write that lands on disk but fails to commit keeps a dirty tree; `memoryd doctor` detects it and completes the commit (implemented 2026-10-27: `memoryd doctor [--fix] [--json]`; detects uncommitted adds/modifications/deletions of managed files plus `.tmp-` crash leftovers; `--fix` removes leftovers under the write lock and, once the tree validates, commits exactly those paths as one `memoryd: doctor recovery` commit; a tree that does not validate is refused — nothing is committed or discarded); data is never lost, nothing resets automatically.
- Index corruption: `memoryd index --rebuild` rebuilds from files; sources always win.

## 6. Capture pipeline

1. At session end the agent writes a session summary into `.inbox/<agent>/<date>-<session>.md`.
2. Distillation (by the current agent): read the inbox summary plus related existing memories; produce atomic entry candidates; dedupe; decide on `supersedes`.
3. `memory_add` writes it: schema validation, redaction, index, and commit all server-side.
4. Raw summaries move to `.archive/`, outside git by default; re-distillation stays possible.

V1 needs only a "session-end prompt + manual trigger"; no background listener. Adapters are input-format converters and must not dominate the first version's engineering budget.

## 7. Gardener flow

1. `memoryd gardener data <data-dir>` emits the analysis package: node profiles (summary, effective tags and deltas, child count, entry count, depth, dates), structural signals (children>8, depth>4, empty projects, same-parent tag-overlap Jaccard≥0.5), and the proposal format guide. Retrieval-failure logs are not persisted in V1.
2. The agent produces a proposal JSON: a list of operations (primitives `move_node / move_entry / retag_node / revise_node / create_node / archive_node`; split/merge compose from these), rationale, affected IDs. Node references accept UUIDv7 or slug paths; paths resolve against the simulated state after earlier operations in the same proposal.
3. `memoryd gardener apply` (dry-run by default) validates: single root, acyclic, kind/slug rules, V-TAG-2 (`move_entry` drops delta tags absorbed by the target), no sensitivity weakening; violations reject the whole proposal with a G- rule code and zero writes; depth>4 and same-parent>8 stay warnings.
4. After human approval, `--apply`: write lock → execute (with a rollback journal) → repository-wide validation (auto-rollback on failure) → index rebuild → exactly one `gardener:` git commit. See [docs/gardener.md](docs/gardener.md).

V1 only generates proposals and dry-runs them; real application is always triggered explicitly by a human; low-risk operations are never auto-merged.

## 8. Security and privacy

### 8.1 Threat model

- Memory content comes from conversations and external documents; it may carry prompt injection, secrets, or personal information.
- The data repository may be synced to cloud drives, read by other local processes, or replaced by a hostile repository.
- MCP clients may be tricked into executing instructions embedded in memories.
- Third-party dependencies and release artifacts carry supply-chain risk.

### 8.2 Controls

- Local by default: V1 is stdio-only — no ports, no network, no telemetry; the core calls no external model.
- Path containment: every file operation stays inside the data repository root; real paths are resolved and escapes rejected; format-managed files reject symbolic links.
- YAML safety: the parser disables custom tags; frontmatter is size-bounded (suggested 64 KiB) as are bodies (suggested 1 MiB); the parser is fuzzed.
- Redaction: before capture and writes, common secret formats are scanned (cloud AKs, GitHub tokens, private key headers, password fields…); hits are replaced with `[REDACTED]`, the count recorded in metadata, never the original.
- Permissions: data directory 0700, files 0600 (Windows uses equivalent ACL/inheritance); the raw archive stays out of git.
- Prompt injection: MCP responses label content as historical data; tools expose no "run commands / access the network" capability; documentation tells clients not to treat memories as system instructions.
- Export and sharing: filtered by `sensitivity`; `private` entries are excluded by default; no automatic pushes to remotes.
- Supply chain: signed releases, SHA-256 checksums, SBOMs, `govulncheck`, license checks; release archives ship LICENSE/NOTICE/THIRD_PARTY_NOTICES.

### 8.3 Data ownership

Users' memory content belongs to the users. Apache-2.0 covers only the MemoryTree software and the repository's documentation/examples — never user data. README and `memoryd init` both state this.

## 9. Compatibility and quality baseline

- Application versions use SemVer; the data format has its own integer `format_version`.
- Readers MUST reject a higher major format with an upgrade hint; unknown frontmatter fields MUST be preserved verbatim.
- Migrations run explicitly: `memoryd migrate --to N --dry-run`, automatic backup, idempotent, rollback-safe.
- Tests: schema/validator unit tests, tree-operation property tests, format golden tests, MCP integration tests, multi-process concurrency tests, path/redaction security tests, 10k-entry performance benchmarks.
- CI: Linux/macOS/Windows; `go vet`, `staticcheck`/`golangci-lint`, `go test -race`, `govulncheck`, license scanning.
- Releases: GoReleaser; zip/tar.gz with LICENSE/NOTICE/THIRD_PARTY_NOTICES; checksums; Sigstore/cosign or minisign signatures; SBOM.
- Module path: `go.mod` currently uses the provisional `github.com/li-ziyin/memorytree`; once the public GitHub repository exists, it MUST change to the real address before the first push or tag, and never again.
- Documentation: README, design, format spec, MCP API, migration, threat model, contributing guide. Core documents are now English-first with synced Chinese versions (`*.zh-CN.md`); acceptance records stay Chinese; new documents are written in English first with Chinese kept in sync.

## 10. Milestones and acceptance

### M0: format and skeleton

- `spec/memory-format-v1.md`, three JSON Schemas, example data.
- Go module skeleton, CLI frame for `memoryd init/validate/index`.
- Validator coverage: single root, acyclicity, kind rules, slug/ID, tags, status, supersedes, sensitivity.
- Import of the 38 existing shared memories (legacy mode, not forced into atomic entries).
- Acceptance: the example repository validates green; breaking each invariant produces its stable error code.

### M1: read-only MCP

Precondition (done 2026-10-20): one `git init` and initial commit in each of the source and data repositories. The data repository's initial commit came after `memoryd validate` was green, freezing the M0 baseline as the auditable starting point.

1. `memoryd index` (done 2026-10-21): FTS5 trigram build with `--rebuild`, fingerprint-based skipping, generation increments; accepted independently of MCP.
2. Read-only MCP (done 2026-10-21): `memory_brief / search / get / tree`, hand-written JSON-RPC 2.0 stdio; startup validation and index refresh; a bad index demands an explicit `--rebuild`. All read tools depend on step 1. Smoke-tested against the real data repository; Qoder client wiring pending.

- FTS5 trigram retrieval works for Chinese; the index rebuilds.
- Acceptance (re-run 2026-10-25): 20 real questions against the `MEMORY.md + grep` baseline. 20/20 complete and first-hit answers; 14,723 vs 67,796 tokens per question in isolated sessions (≈78% saved), on par once the baseline amortizes MEMORY.md across one session (13,095). First-round findings (LIKE ranking, `memory_get` full-text cost) were fixed: LIKE ranks by title/summary/body layers; `memory_get` gained the `query` excerpt mode. Three newer memories were pending and landed with M2. Details in [docs/m1-acceptance.md](docs/m1-acceptance.md).

### M2: writing and capture

- `memory_add / memory_supersede`: atomic writes, locking, git commits.
- One real capture pipeline (Qoder session or ZCode memories): inbox + distillation prompt.
- Acceptance: concurrent writes lose nothing; crashes recover; superseded entries disappear from default retrieval.

### M3: mounting and gardener

- Automatic host-node matching for new entries (tags + summary similarity, agent in the loop).
- Gardener proposal flow with dry-run application.
- Acceptance: 100 real memories inserted with acceptable mount accuracy; proposals never break invariants; a clear conclusion versus the flat baseline.

Done (2026-10-27, see [docs/m3-acceptance.md](docs/m3-acceptance.md)): `memory_suggest_mount` (MCP + `memoryd suggest-mount`) and `memoryd gardener data/apply` (6 primitives, 17 G- rule codes, dry-run by default, rollback, one commit). Leave-one-out on 40 real entries: top-1 97.5% / top-3 100%; a 17-step real proposal dry-ran with zero writes, applied cleanly on a copy, and benched 21/21 after the restructure. The mandated 100-entry re-test reruns once the corpus grows (`go run ./scripts/loo <data-dir>`).

## 11. Open questions

1. Mapping the current project from cwd to a node: does V1 take `project` explicitly from the caller, or add a local `projects.yaml` map? (open; V1 takes it explicitly)
2. ~~Who maintains a node's `summary`, and when does it expire~~ Resolved (2026-10-27): `memory_add` never touches node summaries; the gardener's `revise_node` maintains them, effective after human approval.
3. ~~Do the default max depth / branching factor (suggested 4/8) belong in the spec or only in tool policy~~ Resolved (2026-10-27): keep them as spec warnings (12.6) and tool policy — mounting never limits, the gardener warns via G-DEPTH-1/G-BRANCH-1 without blocking.
4. ~~May `memory_add` mechanically truncate the body into a `summary`, or must it require a semantic one~~ Resolved (with M2): require a non-empty semantic summary (V-CONTENT-1); no mechanical truncation; semantics are constrained by the distillation prompt.
5. Is git a hard dependency: V1 requires system git and stays read-only without it; or embed go-git? (current: system git is a hard write dependency; missing git refuses writes)
6. Does the raw `.archive/` need optional encryption at rest, or do OS permissions suffice? (current: OS permissions, 0600, out of git)
7. Is multi-device sync left entirely to the user's own git remote/cloud drive, with no built-in sync? (current: no built-in sync)

## 12. Related documents

- [Memory format v1](spec/memory-format-v1.md)（[中文](spec/memory-format-v1.zh-CN.md)）
- [MCP API reference](docs/mcp-api.md)
- [Migration guide](docs/migration.md)
- [Gardener workflow](docs/gardener.md)（[中文](docs/gardener.zh-CN.md)）
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- JSON Schemas: `spec/schema/repo.schema.json`, `node.schema.json`, `entry.schema.json`
