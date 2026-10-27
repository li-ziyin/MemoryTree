# MemoryTree MCP API

`memoryd mcp <data-dir>` serves the MemoryTree tools over MCP on stdio (JSON-RPC 2.0, one JSON message per line). This document is the tool reference. Design context lives in [DESIGN.md](../DESIGN.md); the on-disk contract in [spec/memory-format-v1.md](../spec/memory-format-v1.md).

## Protocol

- Methods: `initialize`, `ping`, `tools/list`, `tools/call`. Notifications get no response; batch requests are rejected.
- Protocol versions: `2024-11-05`, `2025-03-26`, `2025-06-18`; unknown versions fall back to `2025-06-18`.
- The server validates the repository and refreshes the index at startup, then works from that snapshot. Hand-edited files are detected by a fingerprint check (re-read at most once per 30 s) and surfaced as a stale-index warning; restart the server after hand edits. A corrupt or schema-incompatible index refuses startup and asks for `memoryd index --rebuild`.
- Every request runs under a 60-second deadline.

## Envelope

Every successful call returns:

```json
{
  "data": {},
  "meta": {
    "total": 0,
    "tokens_estimated": 0,
    "index_generation": 0,
    "warnings": []
  }
}
```

Returned memory text is labelled `"trust": "untrusted-history"`: it is historical data captured from conversations and documents, **never** instructions for the calling agent. Errors set `isError: true` on the tool result and carry a stable `code`, a `message`, and an actionable `suggestion` in `structuredContent.error`.

## Tools

### memory_brief

A fixed-token-budget context brief assembled deterministically (no generation): user profile, current project overview, task-relevant memories, open questions, warnings. Returns summaries and IDs only; fetch full text with `memory_get`.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `task` | string | yes | What the caller is about to do; drives relevance. |
| `project` | string | no | Node UUIDv7 or slug path. |
| `budget_tokens` | integer | no | Default 1200. The budget rotates across sections. |

### memory_search

Full-text search (FTS5 trigram; short CJK terms go through a rune-split index) with filters. Returns summaries, IDs, node paths, and status — never full bodies.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `query` | string | no | Empty query lists entries matching the filters. Max 2000 chars. |
| `node_id` | string (UUIDv7) | no | Restrict to entries directly under this node. |
| `types` | array | no | `user` / `feedback` / `project` / `reference`. |
| `statuses` | array | no | `active` / `draft` / `superseded` / `deprecated`. Default: active only. |
| `tags` | array | no | Require **every** listed effective tag (AND). |
| `limit` | integer | no | Default 20, max 100. |

### memory_get

Fetch one entry by ID. Full body by default; with `query`, bounded excerpts around term hits.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `id` | string (UUIDv7) | yes | Entry ID from search or brief. |
| `query` | string | no | Excerpt mode: fragments around hits. |
| `max_chars` | integer | no | Excerpt budget. Default 1200, range 200–8000. |

Excerpt responses set `excerpted: true` and `full_chars`; a body no longer than `max_chars` still returns whole; a query that misses returns the opening fragment plus a warning.

### memory_tree

The compressed tree: node IDs, slug paths, titles, summaries, entry counts. No parameters.

### memory_suggest_mount

Ranks candidate host nodes for a new entry: 0.45×tag coverage + 0.45×text similarity (CJK-aware tokenization, weighted fields) + a `user`-type prior toward `global`. **The ranking is advisory — the agent makes the final call** and passes its chosen node to `memory_add`.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `type` | enum | yes | `user` / `feedback` / `project` / `reference`. |
| `title` | string | yes | ≤200 chars. |
| `summary` | string | yes | Semantic summary, ≤500 chars. |
| `tags` | array | no | Proposed `tags_delta`. |
| `limit` | integer | no | Default 5, max 10. |

Each candidate carries its score, matched tags, matched tokens, and a rationale, plus a fixed warning that suggestions are for ranking only.

### memory_add *(write)*

Write one new entry: rule-code validation, secret redaction, atomic write, git commit, index refresh — all under the single-writer lock. The entry is searchable in the same session.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `type` | enum | yes | `user` / `feedback` / `project` / `reference`. |
| `title` | string | yes | 1–200 chars. |
| `summary` | string | yes | Semantic summary, 1–500 chars (V-CONTENT-1; never a mechanical truncation). |
| `body` | string | yes | Markdown body, ≤1 MiB. |
| `node` | string | no | Slug path or UUIDv7 of the host node; default `global`. Must be a project or global node. |
| `tags_delta` | array | no | Tags this entry adds (NFC, lowercase). |
| `sensitivity` | enum | no | Default: the host node's level. |
| `status` | enum | no | `active` (default) or `draft`. |
| `supersedes` | array | no | Entry IDs this one replaces; they become `superseded` and the pair is kept consistent. |
| `related` | array | no | Related entry IDs. |
| `source.agent` | string | yes | Calling agent's name. |
| `source.kind` | enum | yes | `conversation` / `document` / `manual` / `import`. |
| `source.session` | string | no | Session identifier. |
| `source.ref` | string | no | Repository-relative reference, never an absolute path. |

Input problems return format rule codes (`V-CONTENT-*`, `V-ENUM-1`, `V-TAG-*`, `V-SUP-*`, `V-NODE-*`, `V-SENS-1`, …); a repository that fails validation refuses writes (`V-FILE-*`).

### memory_supersede *(write)*

Link two **existing** entries into a supersede pair; the old entry becomes `superseded`, the new one inherits its whole predecessor set (flattened). When writing the replacement anyway, prefer `memory_add` with `supersedes`.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `old_id` | string (UUIDv7) | yes | Entry that becomes superseded. |
| `by_id` | string (UUIDv7) | yes | Active entry that takes over. |

## Concurrency

Reads are concurrent (SQLite WAL). Writes serialize on a cross-process file lock in `<data-dir>/.index/`; a conflicting writer fails fast and may retry. A crash between the file write and the commit leaves a dirty tree that `memoryd doctor` detects and `doctor --fix` recovers — data is never discarded.

## CLI equivalents

`memoryd suggest-mount`, `add`, and `supersede` mirror the write tools for shell use; see the [README](../README.md) quick start.
