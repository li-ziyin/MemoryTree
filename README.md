# MemoryTree

Local-first, file-based memory for AI agents: a self-organizing tree of Markdown memories shared across ZCode, OpenCode, Qoder, and any MCP-capable client.

> **Status: M1 feature-complete and benchmarked.** The v1 format, a Go validator with stable error codes, repository initialization, legacy import, the disposable SQLite FTS5 search index, and the read-only MCP server are working. The 20-question retrieval benchmark was rerun after short-query ranking and excerpt retrieval improvements: 20/20 complete and first-hit answers, 14,723 vs 67,796 tokens per task ([results](docs/m1-acceptance.md)). The write path is next; the data repository still needs three newer memories imported.

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

Requires Go 1.25 or newer and Git 2.30+ (Git is not used by `validate`, `index`, or `mcp` yet).

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

# Serve the read-only MCP server over stdio
./bin/memoryd mcp ~/.memorytree
```

An MCP client only needs a command and arguments:

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

`memoryd mcp` validates the repository and refreshes the index at startup, then serves `memory_brief`, `memory_search`, `memory_get`, and `memory_tree` as read-only tools. `memory_get` returns the full body by default, or a bounded excerpt when a `query` is supplied; excerpt responses set `excerpted` and `full_chars`. The server writes only inside `.index/`; memory files are never changed. It works from its startup snapshot, so restart it after editing memory files by hand. A corrupt or schema-incompatible index is not rebuilt automatically: the server refuses to start and reports that `memoryd index --rebuild` is required. Every response carries `index_generation` and warnings, and memory text is labelled `trust: untrusted-history`.

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
  mcpserver/            read-only MCP over JSON-RPC 2.0 stdio
  model/                v1 metadata objects
  validate/             repository-wide invariant checks
spec/                   memory format v1 + JSON Schemas
docs/                   architecture notes and guides
```

The user data repository is separate. The default is `~/.memorytree`; it can be placed anywhere (for local development on this machine: `E:\MemoryTreeData`).

## Documentation

- [Design document](DESIGN.md)
- [Memory format v1](spec/memory-format-v1.md)
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)

## Roadmap

- **M0** — complete: format spec, JSON Schemas, Go skeleton, validator, legacy import
- **M1** — feature-complete and benchmarked: FTS5 trigram index (fingerprint, generation, `--rebuild`), the read-only MCP server (`brief/search/get/tree`), and the 20-question benchmark against `MEMORY.md` + grep. Rerun on 2026-10-25 after short-query ranking and excerpt retrieval: 20/20 complete and first-hit answers, 14,723 vs 67,796 tokens per task, near parity when `MEMORY.md` is amortized
- **M2** — next: write path (`add/supersede`), capture inbox, import the three newer memories, concurrency and crash tests
- **M3** — insertion placement, gardener proposals, release hardening

## Deliberately deferred

These items are not forgotten; each has a fixed point in the roadmap.

| Item | Do it when | Why then |
|---|---|---|
| Replace the provisional module path `github.com/li-ziyin/memorytree` | when the public GitHub repository is created, before the first push or tag | A module path is cheap to change now but leaks into imports, CI, and downstream references. Change it once the real URL exists, before anyone depends on it. |
| Incremental index updates | when the `memory_add` / `memory_supersede` write gateway lands in M2 | M1 rebuilds the whole index from a validated snapshot. Incremental updates only pay off once a single-writer gateway can keep the files and index in one transaction. |

## Data ownership

The Apache-2.0 license covers the MemoryTree software and the documentation in this repository. It does **not** cover the contents of a user's memory repository. Your memories remain yours.

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

## Contributing

Contributions are welcome once the first runnable milestone lands. Please read [CONTRIBUTING.md](CONTRIBUTING.md) for the DCO requirement, development setup, and format-change policy. Security issues should follow [SECURITY.md](SECURITY.md).
