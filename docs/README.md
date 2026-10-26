# Documentation

The design document currently lives at the repository root:

- [DESIGN.md](../DESIGN.md) — architecture, decisions, security, milestones
- [M1 acceptance](m1-acceptance.md)
- [M2 acceptance](m2-acceptance.md) — write-path concurrency, crash, and supersede results (2026-10-27)
- [M3 acceptance](m3-acceptance.md) — mount suggestion and gardener results (2026-10-27)
- [Gardener workflow](gardener.md) — tree-maintenance proposal format, rule codes, and safety contract
- [Performance baseline](perf-baseline.md) — index rebuild and search costs at 100/1000/5000 entries
- [Bench questions format](bench-questions.example.jsonl) — retrieval regression gate for `memoryd bench`
- [Capture and distillation](distillation-prompt.md) — inbox → memory_add workflow with the agent prompt

Documents planned before the first public release:

- MCP API reference
- Migration guide
- Threat model (expanded from [SECURITY.md](../SECURITY.md))
- Client integration guides (ZCode, OpenCode, Qoder)
- Release verification guide
