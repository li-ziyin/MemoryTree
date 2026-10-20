# MemoryTree

Local-first, file-based memory for AI agents: a self-organizing tree of Markdown memories shared across ZCode, OpenCode, Qoder, and any MCP-capable client.

> **Status: M0 complete.** The v1 format, a Go validator with stable error codes, repository initialization, and legacy import are working. The validated M0 source and data baselines are committed to git. Search indexing and the MCP server (M1) are next.

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

Requires Go 1.25 or newer and Git 2.30+ (Git is not used by `validate` yet).

```sh
go build -o bin/memoryd ./cmd/memoryd

# Create a data repository (separate from this source tree)
./bin/memoryd init --title "My Memory" ~/.memorytree

# Validate the example repository or your own data
./bin/memoryd validate spec/examples
./bin/memoryd validate ~/.memorytree

# Import an existing flat directory of agent memories
./bin/memoryd import-legacy --from ~/.zcode/memories --to ~/.memorytree --agent zcode
```

On Windows, use paths such as `E:\MemoryTreeData`. `memoryd validate --json` emits machine-readable results. Exit codes: `0` success, `1` validation errors, `2` usage or I/O error, `3` not implemented yet.

## Repository layout

```
cmd/memoryd/            CLI entry point
internal/
  docfile/              frontmatter writing with stable YAML style
  format/               file-level parsing and YAML safety rules
  ids/                  UUIDv7 generation and validation
  initrepo/             data repository initialization
  legacy/               flat legacy memory import
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
- **M1** — read-only MCP (`brief/search/get/tree`) and first client integration
- **M2** — write path (`add/supersede`), capture inbox, concurrency and crash tests
- **M3** — insertion placement, gardener proposals, release hardening

## Deliberately deferred

These items are not forgotten; each has a fixed point in the roadmap.

| Item | Do it when | Why then |
|---|---|---|
| Replace the provisional module path `github.com/li-ziyin/memorytree` | when the public GitHub repository is created, before the first push or tag | A module path is cheap to change now but leaks into imports, CI, and downstream references. Change it once the real URL exists, before anyone depends on it. |
| `memoryd index` (FTS5 build and rebuild) | first task of M1, before any MCP read tool | `memory_brief`, `search`, `get`, and `tree` all read the index. Building the index without the MCP layer is independently testable and keeps M1 failures easy to localise. |

## Data ownership

The Apache-2.0 license covers the MemoryTree software and the documentation in this repository. It does **not** cover the contents of a user's memory repository. Your memories remain yours.

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

## Contributing

Contributions are welcome once the first runnable milestone lands. Please read [CONTRIBUTING.md](CONTRIBUTING.md) for the DCO requirement, development setup, and format-change policy. Security issues should follow [SECURITY.md](SECURITY.md).
