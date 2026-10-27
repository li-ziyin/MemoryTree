# M5 使用加权检索与维护验收（2026-10-28）

代码：M5 系列提交，数据仓 `E:\MemoryTreeData`（真实 bench 双跑 21/21）。

## DESIGN M5 验收逐条

| 验收项 | 结果 | 证据 |
|---|---|---|
| 热度重排 | ✅ | 乘数 `1 + 0.15·ln(1+returns)·2^(−age/90d)`（`usagelog.Usage.Heat`），实现收敛在 `internal/rank.Apply`（score×heat、稳定排序、heat<1 钳制为 1、无分数的浏览原样通过）。`memory_search` 仅文本查询重排，`memory_brief` 只重排 related 段（user/项目更新/pending 保持时间序——usage 只优化相关性，不干预浏览）。翻转：`TestSearchUsageHeatReordersNearTies`（近平手翻转、成员数不变）；不埋：`TestRunWithHeatKeepsUniqueMatchFirst`（唯一命中 + 其余 149 条全部最大热度，仍是第一）+ `TestSearchUsageHeatCannotBuryMatches`（不相关条目的热度不改变顺序与成员）；饱和：`TestHeatSaturates`（百万次返回 ≈3.07 封顶）；零返回恒为 1（`TestHeatNeverPenalizesCold`）；90 天半衰精确到 1e-9（`TestHeatDecaysWithHalfLife`） |
| 树展示热度排序 | ✅ | `nodeHeat` 子树聚合（Σ heat−1，仅 active），`buildTreeNode` 兄弟按热度降序、平手回退路径序；无用量日志时不进入排序分支，输出与原先一致。`TestTreeChildrenOrderByHeat`（加热 work/agentflow 后 work 子树前移） |
| attic 模式 | ✅ | `gardener data` 新增 `attic_candidates`（恰为 cold 条目，`TestDataIncludesEntryUsage` 断言逐字段）；Guidelines 写明两条退休路径（attic=create_node+move_entry 保持可检索 vs deprecate_entry 不再真），由既有原语组合、无新增 apply 规则——与讨论时「加 G- 规则码」的设想不同：数据包信号本就无 G- 前缀（G- 只用于 apply 校验），attic 不需要校验规则，按既有结构成文 |
| 带 usage 的回归门 | ✅ | `bench.Run` 增 heat 参数；`memoryd bench --usage` 从数据仓真实日志构建 heat，与 MCP 端逐位同源（同一 `rank.Apply`）。`TestRunWithHeatAppliesBoost`（1000 次返回把第 20 名顶到第一——全链路生效）；真实仓双跑：不带 `--usage` 21/21（5435 tokens）、带 `--usage` 21/21（5435 tokens），完全一致 |

## 关键设计点（与讨论的对应）

- **「频率影响排名」而非「频率改变树形」**：物理树一次未动，D6 不变；热度只出现在重排与展示层。
- **冷是缺加成，不是惩罚**：乘数下界 1；被埋风险只存在于近平手翻转（那正是期望行为）。
- **反馈回路**：每会话每工具每条目去重的返回计数 × log 饱和 × 90 天半衰；百万次返回也只有 ~3 倍，强相关永远赢。
- **brief 的 token 预算受益**：related 段先重排再进预算轮转，被验证有用的条目优先存活。

## 测试与质量门

- 新增 21 个测试：usagelog 6（冷不罚/饱和/半衰/坏时间戳/聚合）、rank 7（翻转/不越级/缺行中性/钳制/浏览透传/稳定/短路）、mcpserver 5（近平手翻转/不相关不扰/related 跟随/树序/无日志不变）、bench 2 新场景（boost 全链路、唯一命中不被埋）、gardener 1（attic 候选）。
- `scripts/check.sh` 全绿（gofmt/vet/全量测试/golangci-lint 0/govulncheck 无漏洞/盖章构建）。
- 真实仓 `bench` 与 `bench --usage` 双跑 21/21。

## 遗留

- 热度参数（α=0.15、半衰 90 天）为首次取值，等真实使用积累后可调；调参只需改 `usagelog` 两个常量，回归门已就位。
- usage 日志仍是本机 `.index/` 派生数据（M4 决定），多端热度各自演化；若将来需要一致，可把聚合摘要作为派生数据提交——开放问题保持。
