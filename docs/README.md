# Documentation

The design document currently lives at the repository root:

- [DESIGN.md](../DESIGN.md) — architecture, decisions, security, milestones
- [M1 acceptance](m1-acceptance.md) — 20-question retrieval benchmark against MEMORY.md + grep, final rerun (2026-10-25)
- [Performance baseline](perf-baseline.md) — index rebuild and search costs at 100/1000/5000 entries
- [Bench questions format](bench-questions.example.jsonl) — retrieval regression gate for `memoryd bench`
- [Capture and distillation](distillation-prompt.md) — inbox → memory_add workflow with the agent prompt

Documents planned before the first public release:

- MCP API reference
- Migration guide
- Threat model (expanded from [SECURITY.md](../SECURITY.md))
- Client integration guides (ZCode, OpenCode, Qoder)
- Release verification guide
