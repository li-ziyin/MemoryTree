# M2 写路径实施计划（Write Path）

> **For agentic workers:** 本计划由本会话直接执行（任务粒度与验收标准如下）；每任务独立测试、独立提交。步骤勾选随执行更新。

**Goal:** 落地 DESIGN.md 的 M2：`memory_add` / `memory_supersede` 写网关（校验 → 原子写 → git commit → 索引刷新，全程单写者锁）、采集收件箱、真实仓补 3 条记忆，并满足 M2 验收（并发无丢失、崩溃可恢复、被取代条目默认检索不返回）。

**Architecture:** 新增 `internal/store` 作为唯一写核心（CLI 与 MCP 都走它）；`internal/gitio` 封装 git 调用；锁与原子写保证「文件是真相」不变量——任何时刻文件树要么全旧要么全新，git 与索引都是衍生物。

**Tech Stack:** 既有 Go 1.25 栈；系统 git 子进程（写路径硬依赖）；无新第三方依赖。

**Spec:** [DESIGN.md](../../../DESIGN.md) §M2/§待定问题、[spec/memory-format-v1.md](../../../spec/memory-format-v1.md) §5.2/§8/§12。

## 设计决策（对 DESIGN §11 待定问题的定案）

- **D1 节点映射**：调用方显式传 `node`（slug 路径如 `work/agentflow`，或节点 UUIDv7）；缺省 `global`。cwd 自动映射留给 M3 gardener。
- **D2 summary**：必填语义摘要（1–500 字符），不做机械截断。超 280 字符产生警告（沿用 V-12.6）。
- **D3 git 硬依赖**：`memory_add` / `memory_supersede` 写入前预检 `git` 在 PATH 且数据目录是 git 工作树；缺失即报错、零写入（对应「缺失时只读可用」）。`inbox` 不需要 git。
- **D4 supersede 语义**：`memory_add` 带 `supersedes[]` 时，写新条目（`status: active`，V-SUP-4）并改写每条旧条目（`status: superseded`、`superseded_by: 新id`、`updated_at` 更新，V-SUP-2/3/5）。`memory_supersede(old_id, by_id)` 用于把两条**已存在**条目连成取代对。
- **D5 单写者锁**：`<data>/.index/write.lock`，`O_CREATE|O_EXCL`，内容为 RFC3339 时间戳。已存在且 mtime 超 10 分钟视为崩溃遗留 → 删除后重试一次；否则返回 `ErrWriteLocked`。锁内完成：解析校验 → 原子写 → git 提交 → 索引重建。
- **D6 原子写**：目标 `memories/` 同目录写 `<name>.tmp-<pid>` 再 `os.Rename`；崩溃遗留的 `*.tmp-*` 文件被校验器忽略（非受管文件名），并由下一次写入顺手清理同目录残留。
- **D7 MCP 写工具**：`memory_add` / `memory_supersede`，annotations `readOnly: false`；服务端 Instructions 更新为「读写」表述。其余四个读工具不变。
- **D8 CLI**：`memoryd add <data-dir> --json -`（stdin 读 EntryInput JSON）；`memoryd supersede <data-dir> --old ID --by ID`；`memoryd inbox <data-dir> [--file PATH|-] [--source NAME]`。
- **D9 文件名**：`<YYYY-MM-DD>-<slug>.md`；slug 取 title 的 NFC 小写 ASCII 子集（`[a-z0-9-]`，压缩连字符，截 48）；为空则用 `entry`。冲突时追加 `-<id 前 8 位>`。时间戳一律 `now().UTC()` RFC3339 `Z`。
- **D10 失败契约**：预检失败（git/输入/目标状态）→ 零写入；文件写入成功后 git 提交失败 → 返回错误但保留文件并重建索引，错误信息注明文件路径与手工补提交的命令。

## 输入校验清单（写前，全部映射到 V- 码）

title 1–200 rune；summary 1–500 rune；body ≤ 1 MiB（V-CONTENT-1/3）；type/status/sensitivity 枚举（§8）；`tags_delta` NFC+trim+lower 后匹配 `^[\p{L}\p{N}_-]{1,32}$`、无重复、字典序写入（V-TAG-1/2）；node 存在且 kind ∈ {project, global-root 特例：条目只挂 project 或 global 节点}（§4）；条目 sensitivity 不低于宿主节点（V-SENS-1）；`supersedes` 目标存在、active、非自身（V-SUP-1/4）；source.agent/kind 非空、ref 非绝对路径（V-CONTENT-2）；节点与条目 id 均为 UUIDv7。

## 任务分解

| # | 任务 | 文件 | 测试要点 |
|---|---|---|---|
| T1 | `store.AddEntry` 输入校验 + 节点解析 + slug + 原子写 | `internal/store/store.go`, `entry.go` | 合法输入落盘且 `index.Build` 全绿；每条非法输入被对应拒绝且目录无新文件 |
| T2 | 单写者锁 + 崩溃遗留回收 | `internal/store/lock.go` | 并发 24 次 Add 全部落盘；陈旧锁被回收；新鲜锁报 `ErrWriteLocked` |
| T3 | `store.SupersedeEntry` + add 内联 supersede 改写 | `internal/store/supersede.go` | 双向一致（V-SUP-2/3/5）；默认检索不再返回旧条目 |
| T4 | `internal/gitio` 提交集成 | `internal/gitio/gitio.go` | 写后仓库 HEAD 含新文件；非 git 目录/无 git 报错且零写入 |
| T5 | CLI `add` / `supersede` | `cmd/memoryd/main.go` | 退出码 0/1/2 契约；stdin JSON 流程 |
| T6 | MCP `memory_add` / `memory_supersede` | `internal/mcpserver/tools.go` | tools/list 注解；端到端写后 search 可见；坏输入 ToolError |
| T7 | `memoryd inbox` + 蒸馏提示词文档 | `internal/inbox/`, `docs/distillation-prompt.md` | 追加落盘 `.inbox/`、不进 git、不进索引 |
| T8 | 真实仓补 3 条记忆 + 验收 | 数据仓 + `docs/m2-acceptance.md` | 3 条新记忆入库可检索；bench 20/20；并发/崩溃清单勾选 |

## M2 验收（DESIGN 原文逐条）

1. 并发写入无丢失：8 goroutine × 3 条并发 Add → 24/24 落盘、校验全绿、git 24 文件一次/多次提交均在。
2. 崩溃后可恢复：遗留 `*.tmp-*` 被忽略；陈旧锁被回收；kill 后未提交文件由下次写入正常提交。
3. 旧条目被取代后默认检索不再返回（store 级 + MCP 级测试）。
4. 真实仓：`memorytree-m0-status` / `m1-index` / `m1-mcp` 三条入库，`memoryd bench` 仍 20/20。

## 执行记录

- 计划撰写：2026-10-27。执行随任务逐条更新本节。
