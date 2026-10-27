# Documentation

Documents are English-first; `*.zh-CN.md` files are synced Chinese versions (the English files are authoritative). Acceptance records are historical and stay in Chinese.

- [DESIGN.md](../DESIGN.md) — architecture, decisions, security, milestones（[中文](../DESIGN.zh-CN.md)）
- [Memory format v1](../spec/memory-format-v1.md) — the on-disk contract（[中文](../spec/memory-format-v1.zh-CN.md)）
- [MCP API](mcp-api.md) — tool reference for MCP clients
- [Migration guide](migration.md) — format versioning and migrations
- [Gardener workflow](gardener.md) — tree-maintenance proposal format, rule codes, and safety contract（[中文](gardener.zh-CN.md)）
- [Client integration](clients.md) — wiring Qoder / ZCode / OpenCode to `memoryd mcp`
- [M1 acceptance](m1-acceptance.md)
- [M2 acceptance](m2-acceptance.md) — write-path concurrency, crash, and supersede results (2026-10-27)
- [M3 acceptance](m3-acceptance.md) — mount suggestion and gardener results (2026-10-27)
- [M4 acceptance](m4-acceptance.md) — capture hooks, state redaction, sync, usage signals, write retry (2026-10-27)
- [Performance baseline](perf-baseline.md) — index rebuild and search costs at 100/1000/5000 entries
- [Bench questions format](bench-questions.example.jsonl) — retrieval regression gate for `memoryd bench`
- [Capture and distillation](distillation-prompt.md) — inbox → memory_add workflow with the agent prompt

Documents planned before the first public release:

- Threat model (expanded from [SECURITY.md](../SECURITY.md))
- Release verification guide
