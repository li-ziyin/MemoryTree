# 索引规模基线（perf baseline）

生成方法：`go test ./internal/scalegen/ -bench . -run '^$' -benchtime 3x`
数据：合成仓库（`internal/scalegen`），每条约 2 KiB 中英混排正文，挂在 `global` 节点下。

首轮基线（2026-10-26，Windows / AMD Ryzen 7 7735H，Go 1.25.0，索引 schema v1）：

| 基准 | 100 条 | 1000 条 | 5000 条 |
|---|---:|---:|---:|
| 全量重建 `BenchmarkBuildRebuild` | 116 ms | 1.16 s | 5.96 s |
| 启动空转 `BenchmarkStartupNoop`（校验 + 指纹） | — | 645 ms | — |
| 搜索 FTS `BenchmarkSearchFTS`（trigram，"docker backoff"） | — | 22.9 ms | — |
| 搜索短词 `BenchmarkSearchShortCJK`（"端口 网关"，schema v1 走 LIKE 扫描） | — | 13.1 ms | — |

## 真实仓检索回归基线

问题集：`E:\MemoryTreeData\bench-questions.jsonl`（20 问，源自 [m1-acceptance.md](m1-acceptance.md) 逐题表；gold ID 已按 2026-10-26 索引核对）。

| 日期 | 索引 schema | 首条命中 | 总 token |
|---|---|---|---:|
| 2026-10-26（Task 9 前，LIKE 回退） | v1 | 20/20 | 4,907 |
| 2026-10-26（Task 9 后，rune 拆分 FTS） | v2 | 20/20 | 4,907 |
| 2026-10-27（M2 导入 3 条后，21 问口径） | v2 | 21/21 | 5,298 |

Task 9 采用依据：真实仓 20 问质量与 token 完全持平；合成最坏情形（1000 条全命中）下短词查询 13.1→19.5 ms（绝对值无感）、全量重建 1.16→1.77 s/千条（多一张 FTS 表的代价），换取 bm25 列加权排序与索引化路径。

`memoryd bench` 任一改动后复跑：首条命中不得低于 20/20，总 token 超过基线 +10% 需要说明理由。

## 结论（供 M2 决策）

- 重建耗时约 1.2 s/千条，近线性；5000 条时单次重建约 6 s。在 1 万条以内，"每次源变更全量重建"仍可接受；超过 1 万条或写入频率上升后，M2 的增量索引才有必要。
- `memoryd mcp` 每次启动固定支付一次校验 + 指纹计算：1000 条约 0.65 s。频繁重启 MCP 客户端的场景下，这一成本随条目数线性增长，是 Task 10「过期指纹警告」之外第二个支持未来引入增量刷新的数字。
- 短词 LIKE 扫描在 1000 条时 13 ms，尚不是瓶颈；真正的代价是排序质量（无 BM25），见 Task 9。
