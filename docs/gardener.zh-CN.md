# Gardener：离线树维护流程（中文同步版）

> 本文件是 [docs/gardener.md](gardener.md)（英文，权威版本）的中文同步版。

gardener 是一个由 Agent 执行的离线任务：读取数据包 → 产出提案 JSON → dry-run 校验 → 人批准 → 应用。核心进程不调用任何模型；提案质量由 Agent 负责，合法性由 `memoryd` 保证。

## 流程

```text
memoryd gardener data <data-dir>          # 1. 生成分析数据包（JSON，含提案格式指南）
Agent 阅读数据包，产出 proposal.json       # 2. 提案（操作列表 + 理由）
memoryd gardener apply <data-dir> < proposal.json        # 3. dry-run（默认，零写入）
memoryd gardener apply --apply <data-dir> < proposal.json # 4. 应用（人批准后）
```

- 第 3 步通过不代表必须执行第 4 步；应用永远由人触发。
- 任一操作违规 → 整案拒绝、零写入；应用后全仓校验失败 → 自动回滚，原始树不变。
- 应用 = 写锁 → 模拟校验 → 执行 → 全仓校验 → 索引重建 → 恰好一个 `gardener:` 前缀的 git 提交。

## 提案 JSON

```json
{
  "author": "agent-name",
  "reason": "一句话说明动机（进入 git 提交信息）",
  "operations": [
    {"op": "move_node",   "node": "work/agentflow", "target": ".", "reason": "..."},
    {"op": "move_entry",  "entry": "<UUIDv7>", "target": "work/agentflow", "reason": "..."},
    {"op": "retag_node",  "node": "work", "add": ["java"], "remove": ["career"], "reason": "..."},
    {"op": "revise_node", "node": "work/agentflow", "summary": "新摘要", "reason": "..."},
    {"op": "create_node", "target": "work", "kind": "project", "slug": "ml",
     "title": "ML", "summary": "机器学习试验", "tags_delta": ["ml"], "reason": "..."},
    {"op": "archive_node", "node": "work/old-thing", "reason": "..."},
    {"op": "deprecate_entry", "entry": "<UUIDv7>", "reason": "冷条目：90+ 天无返回"}
  ]
}
```

- `node`/`target` 用 UUIDv7 或 slug 路径；路径按先前操作后的模拟树解析，因此「先 create 再往里 move」可行。
- `revise_node` 省略的字段保持原值；`create_node` 的 `sensitivity` 省略时继承父节点。
- split = `create_node` + `move_node`/`move_entry`；merge = moves + `archive_node`。
- 上限 200 操作/提案。
- `move_entry` 会自动从条目 `tags_delta` 剔除目标节点已提供的标签（V-TAG-2），步骤报告 `drop_tags`。
- `deprecate_entry` 将 active/draft 条目退役（`status: deprecated`，默认检索不再返回）；与 `entry_usage` 标出的冷条目配套。

## 规则码（G-）

| 码 | 含义 |
|---|---|
| G-OP-1 | 未知操作或格式错误 |
| G-NODE-1 / G-ENTRY-1 | 引用的节点/条目不存在 |
| G-ENTRY-2 | deprecate_entry 的目标不是 active/draft（superseded 终态、deprecated 已退役） |
| G-CYCLE-1 | move 到自身或后代 |
| G-KIND-1/2/3/4 | kind 限制：project/global 不能有子节点；create 只能建 project/category；条目只能进 project/global；root 与 global 不能归档 |
| G-SLUG-1 | slug 非法、保留名或与目标父下冲突 |
| G-SENS-1/2 | 敏感级别弱化（节点对新父 / 条目对新宿主） |
| G-TAG-1/2 | 标签语法/重复/继承重复 / remove 不在自身 delta |
| G-NODE-2 | 目标节点非 active |
| G-CONTENT-1 | title/summary 为空或超长 |
| G-LIMIT-1 | 操作数超上限 |
| G-DEPTH-1 / G-BRANCH-1 | 警告：深度>4 / 同父子节点>8（不阻断，工具策略） |

## 数据包信号

`gardener data` 为每个节点给出：路径、kind、有效标签与 delta、深度、子节点数、条目数、时间戳，以及结构信号 `children>8`、`depth>4`、`empty-project`；并给出同父节点对的有效标签重叠（Jaccard ≥ 0.5）作为 merge/restructure 触发线索，同时汇总 MCP 层的检索失败日志（`query_misses`：持续落空的查询及次数与最近时间）——连续落空是第三类重构触发信号。

`entry_usage` 列出每条 active/draft 条目在 MCP 层的返回历史：`memory_search` / `memory_brief` / `memory_get` 返回过几次（`returns`）、最近一次是什么时候（`last_return`，从未返回则为空），以及 `cold` 标记（从未返回且 90+ 天未动）。冷条目即退役候选——对它们提案 `deprecate_entry`。计数只是 `.index/` 下一次性日志里的派生数据，绝不写入记忆文件。
