# M3 挂载与 gardener 验收（2026-10-27）

代码：M3 系列提交（`6549bd1` 打分器 → `a2bc785` 冗余标签修复），数据仓 `E:\MemoryTreeData`（40 条真实记忆，4 节点）。

## DESIGN M3 验收逐条

| 验收项 | 结果 | 证据 |
|---|---|---|
| 新条目自动匹配挂载节点 | ✅ | `memory_suggest_mount`（MCP，只读）+ `memoryd suggest-mount`（CLI，支持 `--exclude-entry` 留一法）。打分 = 0.45×标签覆盖 + 0.45×文本相似度（CJK 逐字 token，字段加权：节点标题 3 / 摘要 1 / 标签 2 / 条目标题 1.5 / 条目摘要 0.5）+ user 类型对 global 的先验；输出分数、命中标签、命中 token 与理由，Agent 最终判定 |
| gardener 提案流程与 dry-run 应用 | ✅ | `memoryd gardener data` 输出节点画像（含 tags_delta、子数、条目数、深度）与结构信号（children>8、depth>4、空项目、同父标签重叠 Jaccard≥0.5），内嵌提案格式指南；`memoryd gardener apply` 默认 dry-run；`--apply` 走写锁 → 模拟校验 → 执行（带回滚日志）→ 全仓校验 → 索引重建 → 单次 git 提交 |
| 挂载准确率可接受 | ✅* | 真实仓 40 条留一法：top-1 39/40（97.5%）、top-3 40/40、平均名次 1.02。唯一 miss 是 user-profile：user 先验推荐 global 而非其实际位置 legacy/shared-memory——这是先验按设计生效（该条本应放 global），非误判。*注意：真实仓仅 2 个可挂载节点（global + legacy/shared-memory），区分度证据弱；规范要求 100 条重测，当前 40 条，留待记忆量增长与树重构后复测 |
| 提案不破坏不变量 | ✅ | 模拟器对 6 种原语（move_node/move_entry/retag_node/revise_node/create_node/archive_node）逐条执行 17 条 G- 规则码（环、slug 冲突、kind 限制、V-TAG-2 继承重复、敏感级别弱化、深度/分支因子警告）；任一操作被拒则整案拒绝、零写入；应用后再全仓校验，失败自动回滚。20+ 个规则单测 + 5 个执行器测试（含回滚、零写入、单提交）覆盖 |
| 与扁平基线对比有明确结论 | ✅ | 检索层沿用 M1 结论（20 问 20/20 首条命中，token 省 78%）。M3 新增：重构后副本上 bench 21/21 首条命中、5,238 token（≤ M2 基线 5,298），证明树化不改检索质量；挂载层的「扁平基线」即「不分类全放一处」，正是当前 40 条堆在 legacy/shared-memory 的现状，gardener 提案即为消除它的工具 |

## 真实仓演练（不落盘）

17 步提案：user-profile → global；新建 projects 分类 + memorytree、agent-ide 项目；13 条主题条目迁移；legacy/shared-memory 摘要更新。

- 真实仓 dry-run：17/17 通过，零写入（git status、HEAD、validate 均不变）。
- 数据仓副本 `--apply`：一次成功——7 节点 40 条 0 错 0 警、工作区干净、恰好一个 `gardener:` 提交、generation 6；条目分布 global 1 / projects/memorytree 6 / projects/agent-ide 6 / legacy/shared-memory 27；副本 bench 21/21。
- 该提案未对真实仓执行：DESIGN §7 规定人批准后合并，等用户确认。

## 开发中发现并修复的问题

1. **move_entry 吸收标签导致 V-TAG-2 违规**——M2 导入的 3 条 memorytree 条目自带 `tags_delta: [memorytree]`，移入同名标签项目后 delta 与继承标签重复，应用后校验失败触发自动回滚。修复：模拟器计算目标节点已提供的 delta 标签，执行时从条目 delta 剔除（有效标签不变，存储保持最小，符合 D2），步骤报告 `drop_tags` 并有回归测试 `TestGardenerMoveEntryDropsRedundantDeltaTags`（`a2bc785`）。
2. **快照缺 delta 字段**——NodeInfo/EntryInfo 原本只有有效标签，gardener 数据包与提案校验都需要 delta，补齐 `TagsDelta` 字段。

## 契约摘要

- `memory_suggest_mount` 只读、标注 readOnlyHint；返回候选含 `trust: untrusted-history` 与固定警告「建议仅供排序，最终由 Agent 判定」。
- 节点引用支持 UUIDv7 或 slug 路径；路径按提案内先前操作后的模拟态解析，因此「建节点→往里移」链式提案可用（新建节点 ID 在执行时铸造）。
- 提案规模上限 200 操作；深度>4、同父子节点>8 为警告不阻断（对齐规范 12.6 与待定问题 3 的决策：作为工具策略而非规范硬约束）。
- gardener 真实应用与其他写路径共用单写者锁、`finishWrite`（先校验后提交、pathspec 提交、提交失败保留文件与索引并给手工恢复命令）；应用失败或应用后校验失败自动回滚，原始树不变。

## 遗留

- 100 条挂载准确率复测：当前 40 条、2 个可挂载节点，数字区分度有限；建议树重构落地且记忆到 100 条后重跑 `go run ./scripts/loo <data-dir>`。
- 真实仓的重排提案（tmp/gardener-proposal.json）待人批准后执行；批准前真实仓保持扁平现状。
- split/merge 无专用原语，按 create+move+archive 组合表达；若真实语料证明高频，再考虑糖衣操作。
