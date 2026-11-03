# MemoryTree

Local-first, file-based memory for AI agents: a self-organizing tree of Markdown memories shared across ZCode, OpenCode, Qoder, and any MCP-capable client.

> **Status: M5 complete.** Everything from M1–M3 (v1 format, validator, init, legacy import, disposable SQLite FTS5 index, read-only MCP server, write gateway `memory_add` / `memory_supersede`, `memoryd inbox` capture, `memory_suggest_mount`, the gardener with dry-run proposals and one-commit applies, `memoryd doctor` crash recovery, sensitivity-filtered export, five-client shared-memory operation with one-command install) plus the M4 hardening wave, all landed 2026-10-27: session-end auto-capture (`memoryd capture-session` + `install --hooks` for ZCode and OpenCode), state redaction beyond credentials (home paths, hostnames, machine-local tokens, with a documented false-positive policy), `memoryd sync` (fetch + rebase + push under the write lock; dirty trees refused; plain git conflicts), gardener usage signals (`entry_usage` plus the `deprecate_entry` primitive for cold entries), and bounded write auto-retry on lock contention. Remotes are self-hosted (2026-10-27: both repositories push to bare repositories on a second physical disk, `D:\GitRepos`); the module path is final. M5 usage-weighted retrieval landed 2026-10-28: search and brief re-rank with a saturating, time-decayed usage-heat multiplier (cold is never a penalty), `memory_tree` lists hotter siblings first (display only), the gardener's attic pattern retires quiet-but-true entries without deprecating them, and `memoryd bench --usage` gates the same re-ranking the server serves — acceptance in [docs/m5-acceptance.md](docs/m5-acceptance.md). Next: the first tagged release.

## Why

Cross-agent memory today is usually a shared folder of notes. That is readable, but it has no capture, provenance, freshness, retrieval, or project boundaries — an agent can easily treat another agent's notes as the current project's state.

MemoryTree separates the shared memory layer from each agent's native memory:

- **Plain Markdown tree** — human-readable, git-audited, portable, no database lock-in.
- **Files are the source of truth** — SQLite is a disposable, rebuildable index.
- **Delta tags** — a node stores only its own tags; effective tags are the union along the path. The parent/child invariant is guaranteed by the data model, so moving a subtree is cheap.
- **MCP as the only write gateway** — validation, provenance, deduplication, atomic writes, and git commits in one place.
- **Agent-assisted curation** — agents distill sessions and propose tree changes; humans approve. The core makes no LLM calls and does not access the network.
- **No telemetry** — local by default; your data belongs to you.

## Architecture

```
 ZCode / OpenCode / Qoder / other MCP clients
                    |
                    | MCP over stdio
                    v
              +-----------+
              |  memoryd  |
              |-----------|
              | validate  |
              | store     |----> data repo (Markdown tree + git)
              | index     |----> SQLite FTS5 (rebuildable)
              | brief     |
              +-----------+
                    ^
                    | proposals / approval
              +-----------+
              | gardener  |  (agent-driven, offline)
              +-----------+
```

## Quick start

Requires Go 1.26 or newer (see the `go` directive in `go.mod`) and Git 2.30+ (Git is not used by `validate`, `index`, or `mcp` yet).

```sh
go build -o bin/memoryd ./cmd/memoryd

# Run the full quality gate used by CI (gofmt, vet, tests, stamped build)
bash scripts/check.sh

# Create a data repository (separate from this source tree)
./bin/memoryd init --title "My Memory" ~/.memorytree

# Validate the example repository or your own data
./bin/memoryd validate spec/examples
./bin/memoryd validate ~/.memorytree

# Import an existing flat directory of agent memories
./bin/memoryd import-legacy --from ~/.zcode/memories --to ~/.memorytree --agent zcode

# Build or refresh the search index (validates first; safe to rerun)
./bin/memoryd index ~/.memorytree

# Force a full rebuild, for example after an index schema change or corruption
./bin/memoryd index --rebuild ~/.memorytree

# Serve the MCP server over stdio (read tools plus memory_add / memory_supersede)
./bin/memoryd mcp ~/.memorytree

# Write one entry from stdin JSON (validates, writes atomically, commits, refreshes the index)
echo '{"type":"project","title":"...","summary":"...","body":"...",
       "source":{"agent":"zcode","kind":"conversation"}}' \
  | ./bin/memoryd add ~/.memorytree

# Capture raw text now, distill later (see docs/distillation-prompt.md)
echo "raw notes..." | ./bin/memoryd inbox ~/.memorytree --source zcode

# Detect and recover crashed writes (uncommitted files, interrupted atomic
# writes); --fix removes leftovers and commits, never discarding data
./bin/memoryd doctor ~/.memorytree
./bin/memoryd doctor --fix ~/.memorytree

# Copy a sensitivity-filtered share view (default level: internal, i.e. no
# private entries); the copy is for sharing, not a validatable repository
./bin/memoryd export --level public --out ~/memory-share ~/.memorytree

# Register memorytree MCP in a local client (zcode, qoder, workbuddy,
# opencode, deepseek); --remove uninstalls. Backs up the config first.
./bin/memoryd install --client qoder ~/.memorytree

# Install the session-end capture hook (zcode, opencode); finished sessions
# leave a redacted capture in the inbox without any manual action
./bin/memoryd install --hooks --client zcode ~/.memorytree

# Converge the data repository with its git upstream: fetch + rebase +
# push under the write lock; dirty trees refused, conflicts left for git
./bin/memoryd sync ~/.memorytree

# Rerun retrieval questions and fail when a gold entry is not the first hit
./bin/memoryd bench ~/.memorytree bench-questions.jsonl
```

An MCP client only needs a command and arguments — or one `memoryd install --client <name> <data-dir>` call, which writes the entry for ZCode, Qoder, WorkBuddy, OpenCode, and DeepSeek Harness:

```json
{
  "mcpServers": {
    "memorytree": {
      "command": "<absolute-path-to-memoryd>",
      "args": ["mcp", "<absolute-path-to-data-dir>"]
    }
  }
}
```

On Windows, use paths such as `E:\MemoryTreeData`. `memoryd validate --json` and `memoryd index --json` emit machine-readable results. Exit codes: `0` success, `1` validation errors, `2` usage or I/O error.

The index lives at `<data-dir>/.index/index.sqlite`. It is derived data: deleting it loses nothing. A normal `memoryd index` is a no-op when the managed files have not changed; any source change triggers a full rebuild in one SQLite transaction and increments the stored `generation`. An unreadable, corrupt, or schema-incompatible index is left in place and reported; rerun with `--rebuild` to replace it. The index contains full entry text, so `memoryd init` gitignores `.index/`; treat it as a local cache.

`memoryd mcp` validates the repository and refreshes the index at startup, then serves the read tools `memory_brief`, `memory_search`, `memory_get`, and `memory_tree`, plus the write tools `memory_add` and `memory_supersede` (rule-code validation, atomic write, git commit, index refresh; results are searchable in the same session). `memory_get` returns the full body by default, or a bounded excerpt when a `query` is supplied; excerpt responses set `excerpted` and `full_chars`. The server writes only inside `.index/`; memory files are never changed. Queries go through the shared SQLite index, so entries written by another memoryd process (another client) are visible without a restart; a fingerprint check at most once per 30 s adopts a refreshed index, rebuilds it after hand edits, and warns only when the repository fails to re-read or validate. A corrupt or schema-incompatible index is not rebuilt automatically: the server refuses to start and reports that `memoryd index --rebuild` is required. Every response carries `index_generation` and warnings, and memory text is labelled `trust: untrusted-history`.

## Repository layout

```
cmd/memoryd/            CLI entry point
internal/
  docfile/              frontmatter writing with stable YAML style
  format/               file-level parsing and YAML safety rules
  ids/                  UUIDv7 generation and validation
  index/                SQLite FTS5 build, rebuild, and search
  initrepo/             data repository initialization
  legacy/               flat legacy memory import
  mcpserver/            MCP over JSON-RPC 2.0 stdio (read tools + write gateway)
  model/                v1 metadata objects
  validate/             repository-wide invariant checks
spec/                   memory format v1 + JSON Schemas
docs/                   architecture notes and guides
```

The user data repository is separate. The default is `~/.memorytree`; it can be placed anywhere (for local development on this machine: `E:\MemoryTreeData`).

## Documentation

English-first; `*.zh-CN.md` files are synced Chinese versions.

- [Design document](DESIGN.md)（[中文](DESIGN.zh-CN.md)）
- [Memory format v1](spec/memory-format-v1.md)（[中文](spec/memory-format-v1.zh-CN.md)）
- [MCP API reference](docs/mcp-api.md)
- [Migration guide](docs/migration.md)
- [Client integration guide](docs/clients.md) — Qoder, ZCode, OpenCode
- [Gardener workflow](docs/gardener.md)（[中文](docs/gardener.zh-CN.md)）
- [All documentation](docs/README.md) — acceptance records, performance baseline, capture flow
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)

## Roadmap

- **M0** — complete: format spec, JSON Schemas, Go skeleton, validator, legacy import
- **M1** — feature-complete and benchmarked: FTS5 trigram index (fingerprint, generation, `--rebuild`), the read-only MCP server (`brief/search/get/tree`), and the 20-question benchmark against `MEMORY.md` + grep. Rerun on 2026-10-25 after short-query ranking and excerpt retrieval: 20/20 complete and first-hit answers, 14,723 vs 67,796 tokens per task, near parity when `MEMORY.md` is amortized. Engineering hardening landed on 2026-10-26: cross-platform CI with a schema consistency job, direct parser tests and fuzz seeds, a scale baseline ([docs/perf-baseline.md](docs/perf-baseline.md)), the `memoryd bench` retrieval regression gate, a rune-split FTS index for short CJK queries (schema v2), and stale-index warnings with request deadlines in the MCP server
- **M2** — complete (2026-10-27): write gateway `memory_add`/`memory_supersede` over MCP and CLI (rule-code validation, atomic writes, single-writer lock with crash recovery, git commits, index refresh), `memoryd inbox` capture with the distillation prompt, the three newer memories imported, concurrency and crash tests — acceptance in [docs/m2-acceptance.md](docs/m2-acceptance.md)
- **M3** — complete (2026-10-27): insertion placement (`memory_suggest_mount`, leave-one-out top-1 97.5% / top-3 100% at 40 entries) and the gardener (`gardener data/apply`, six primitives, 17 G- rule codes, dry-run by default, rollback, one commit per proposal) — acceptance in [docs/m3-acceptance.md](docs/m3-acceptance.md). The user-approved restructure of the live repository is applied. Release hardening: golangci-lint and govulncheck gates (`.golangci.yml`, `scripts/check.sh`, CI), `go test -race` in CI, the dependency allowlist (`internal/depcheck`, keeps `go.mod` in lockstep with `THIRD_PARTY_NOTICES.md`), and the GoReleaser pipeline (`.goreleaser.yaml` + `release.yml`: cross-platform archives with license files, SHA-256 checksums, SPDX SBOMs, keyless cosign signatures, draft releases). Remaining: the 100-entry placement re-test once the corpus grows
- **M4** — complete (2026-10-27): session-end auto-capture (`memoryd capture-session` + `install --hooks` for ZCode and OpenCode, live on this machine), state redaction (home paths, `HOME`/`USERPROFILE`, labeled dotless hostnames, dotless `user@host`; qualified names, URLs, emails, and repo-relative refs stay), `memoryd sync` (fetch + rebase + push under the write lock, bounded moving-remote retries, dirty trees and unfinished rebases refused, plain git conflicts), gardener usage signals (`.index/entry-usage.jsonl`, `entry_usage` with a cold flag, the `deprecate_entry` primitive), and bounded write auto-retry (one ~20 s contention window per write). Verified on real sessions the same evening: captures land with the (source, session) overwrite observed live, and the first user request is recovered from ZCode's durable rollout log when the hook transcript lacks it — acceptance in [docs/m4-acceptance.md](docs/m4-acceptance.md). Still deferred: semantic retrieval (V2), incremental indexing, embedded go-git, `.archive/` encryption
- **M5** — complete (2026-10-28): usage-weighted retrieval and maintenance — `memory_search` and brief's related section multiply bm25 by a heat multiplier `1 + 0.15·ln(1+returns)·2^(−age/90d)` (shared `internal/rank`; cold never penalized, ≈3× saturation so relevance dominates), `memory_tree` orders siblings by subtree heat (display only), the gardener's attic pattern (`attic_candidates` + documented create/move composition) separates quiet-but-true from no-longer-true, and `memoryd bench --usage` runs the retrieval gate with the live usage log — acceptance in [docs/m5-acceptance.md](docs/m5-acceptance.md)

## Releases

Pushing a `v*` tag runs [.github/workflows/release.yml](.github/workflows/release.yml): tests must pass, GoReleaser builds `memoryd` for Linux/macOS/Windows on amd64/arm64 (pure Go, `CGO_ENABLED=0`), packs zip/tar.gz archives with `LICENSE`, `NOTICE`, and `THIRD_PARTY_NOTICES.md`, writes SHA-256 checksums, the workflow attaches SPDX SBOMs (syft) and a keyless cosign signature of the checksums, and opens a **draft** GitHub release for review. `workflow_dispatch` runs the same pipeline as a snapshot without tagging. With the self-hosted remotes this workflow stays dormant until a public mirror exists; until then a local `goreleaser release --snapshot` (or a plain `git tag`) cuts equivalent artifacts. The dependency inventory behind every archive is enforced by `internal/depcheck`, so no module enters a build without a recorded license.

## Deliberately deferred

These items are not forgotten; each has a fixed point in the roadmap.

| Item | Do it when | Why then |
|---|---|---|
| ~~Replace the provisional module path~~ Resolved (2026-10-27): self-hosted remotes chosen, `github.com/li-ziyin/memorytree` is final | only if the project is ever published under a different host | The path is an opaque identifier for a private, non-importable tool, and it already matches the owner's GitHub account, so even a future public mirror needs no rename. |
| Incremental index updates | when the repository grows past ~10,000 entries or write frequency makes full rebuilds (≈1.2 s per 1,000 entries) felt | The write gateway already refreshes the index transactionally; full rebuilds stay simple and correct below that scale. |
| Semantic retrieval (embeddings) | V2, after real query logs (including the retrieval-miss log) provide an evidence base | Keyword FTS + BM25 + the tree currently reach 21/21 first-hit on the live corpus; embeddings must beat that baseline to justify the added index and cost. |
| Embedded go-git | when the system-git dependency actually blocks a supported use case | A single static binary is the goal; today every write machine has git. |
| `.archive/` encryption at rest | when the threat model includes local disk compromise | OS permissions (0600, outside git) suffice for the current single-user machines. |

## Data ownership

The Apache-2.0 license covers the MemoryTree software and the documentation in this repository. It does **not** cover the contents of a user's memory repository. Your memories remain yours.

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

## Contributing

Contributions are welcome once the first runnable milestone lands. Please read [CONTRIBUTING.md](CONTRIBUTING.md) for the DCO requirement, development setup, and format-change policy. Security issues should follow [SECURITY.md](SECURITY.md).
