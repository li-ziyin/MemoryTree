# MemoryTree 设计文档

- 状态：M1 实现完成，检索验收复测通过（2026-10-25：20/20 完整与首条命中；每题独立会话 14,723 vs 基线 67,796 token，见 [docs/m1-acceptance.md](docs/m1-acceptance.md)）；人工评分 4.95 已确认（2026-10-26），M2 写路径待开始
- 工作名：MemoryTree；二进制名：`memoryd`
- 语言与形态：Go，单二进制、默认 stdio、无后台常驻服务
- 数据目录：独立 git 仓库；本机开发数据先定 `E:\MemoryTreeData`，其他用户默认 `~/.memorytree`
- 许可证：Apache-2.0（代码与仓库内文档）；用户记忆数据不属于该许可证范围

## 1. 问题与目标

当前跨 Agent 记忆是"共享 Markdown 便签本"：ZCode、OpenCode、Qoder 共读一个目录，靠各自的全局规则去读。它能用，但没有采集、蒸馏、归属、时效、检索和项目边界；一个 Agent 很容易把另一个 Agent 留下的资料当成当前项目状态。

MemoryTree 要做的是一层**独立于各 Agent 的共享记忆层**：

1. 数据是纯 Markdown 目录树，用户可读、可改、可整仓搬走。
2. 树由标签组织，子节点的有效标签集天然包含父节点标签集。
3. 新记忆由 Agent 蒸馏后写入；树的调整由 Agent 提案、人审后执行。
4. 任意支持 MCP 的 Agent 都能通过同一组工具读取和写入。
5. 默认本地、无网络、无遥测；核心不调用任何 LLM。

非目标（V1 不做）：云同步、多用户权限、图形界面、语义向量检索、各 Agent 的自动适配器全家桶、常驻 daemon、实时自动重组。

## 2. 核心决策

### D1：Markdown 树是唯一真相，SQLite 只是可重建索引

数据仓库本身是一个 git 仓库。节点是目录，节点元数据在 `node.md`，记忆条目在项目叶的 `memories/*.md`。SQLite 只保存全文索引和派生字段；删掉索引后可以从文件完整重建。开源时不会出现"数据只在某个数据库里"的锁定。

### D2：标签存增量，不变量由结构保证

每个节点只存自己的 `tags_delta`，有效标签沿路径求并：

```
/work                 delta {work}        -> {work}
/work/agentflow       delta {java, agent} -> {work, java, agent}
```

因此"子节点有效标签集 ⊇ 父节点有效标签集"不是靠校验维护的，而是数据模型天然成立。移动一个项目或分类时，只改 `parent` 和目录位置，后代的有效标签自动重算，不需要逐个文件重写标签。审计时生成一份受影响节点的 effective-tag diff 即可。

这是对 2026-10-17 评估中"重组会把标签不变量放大成路径全链重写"的修正：执行成本从 O(子树文件数) 降到"改一个指针 + 重算索引"。剩下的难点从"怎么改"变成"往哪移、何时移"，交给离线 gardener 和指标触发。

约束：不变量约束的是**有效标签**，不是存储形式。展示和检索统一用 effective tags；节点文件里只写 delta。

### D3：MCP server 是唯一写入口

所有 Agent 通过本地 MCP server 读写，不直接改数据目录。服务端负责：

- frontmatter 与 JSON Schema 校验
- 去重、状态迁移、`supersedes` 双向一致性
- 原子写文件、更新 SQLite 索引、git commit
- 路径约束、符号链接拒绝、敏感信息脱敏

V1 只开 stdio，不开本地端口；逻辑上单写者，跨进程用文件锁串行化写操作。以后需要常驻或多客户端并发，再引入本地 daemon，工具接口不变。

### D4：树负责导航，混合搜索负责命中

检索分两层：

- `memory_tree`：返回压缩后的树、节点摘要、计数，供 Agent 建立方向感。
- `memory_search`：SQLite FTS5（trigram 分词处理中文）+ 标签/项目/状态过滤，负责精确命中。

V1 不引入 embedding。个人规模下"树 + 关键词/标签过滤"足够；语义检索放到 V2，并用真实查询做对照后再决定是否值得加。对外最重要的接口不是 `search`，而是 `memory_brief`：在固定 token 预算内给出用户画像、当前项目状态、相关记忆和待定事项。

### D5：采集走 inbox，蒸馏由 Agent 完成

每个 Agent 在会话结束把摘要或原始记录写入 `.inbox/<agent>/`。蒸馏由当前 Agent（或用户手动）完成，不在核心进程里偷偷调用外部模型。蒸馏结果通过 `memory_add` 变成原子条目。原始会话进 `.archive/`，不进 git，权限 0600，便于回溯和重做。

V1 先支持一条真实链路（优先 Qoder 或 ZCode），不先写各平台适配器。OpenCode 的会话导出、Qoder 的会话读取工具、ZCode 的现有记忆文件都可以作为输入来源。

### D6：gardener 离线提案，不实时重排

gardener 是一个周期性任务，可以由任意 Agent 执行。它读取节点摘要、分支因子和检索失败日志，提出 `split / merge / move / retag` 操作，输出一个 git patch 和理由。服务端校验不变量后，由人批准合并。

触发指标（V1 建议）：

- 某个分类的子节点数超过 8
- 某类查询连续落空（检索失败日志）
- 两个节点的摘要/标签高度重叠
- 项目结项或长期不活跃

### D7：Go 单二进制，代码与数据分离

- Go 单二进制，无 Node/Python 运行时；目标二进制约 30MB、常驻内存 < 50MB、1 万条记忆检索 p95 < 50ms。
- 依赖控制在个位数：手写 MCP JSON-RPC 2.0 stdio（不引入 SDK）、纯 Go SQLite 驱动、YAML 解析器、UUID、文件锁。
- 代码仓库与用户数据仓库分离。代码 Apache-2.0；用户记忆数据归用户所有。
- 发布提供 Windows/macOS/Linux 构建、校验和、签名、SBOM；CI 跑测试、静态检查和依赖漏洞扫描。

## 3. 总体架构

```
 ZCode / OpenCode / Qoder / 其他 MCP 客户端
                    |
                    | MCP over stdio
                    v
              +-----------+
              |  memoryd  |
              |-----------|
              | validate  |
              | store     |----> 数据仓库（Markdown 树 + git）
              | index     |----> SQLite（FTS5，可重建）
              | brief     |
              +-----------+
                    ^
                    | 提案 / 审批
              +-----------+
              | gardener  |  （由 Agent 执行，离线）
              +-----------+
```

写入路径：

```
Agent -> memory_add -> 校验 -> 原子写文件 -> 更新索引 -> git commit
```

读取路径：

```
Agent -> memory_brief / search / tree / get -> 只读 SQLite / 文件
```

## 4. 数据模型摘要

完整规范见 [spec/memory-format-v1.md](spec/memory-format-v1.md)，JSON Schema 见 `spec/schema/`。

```
E:\MemoryTreeData\
  memory.yaml
  node.md                         # root
  global/
    node.md                       # 保留分类，可挂条目
    memories/
      2026-10-20-user-profile.md
  work/
    node.md
    agentflow/
      node.md
      memories/
        2026-10-20-sse-pitfall.md
  .inbox/                         # 原始采集，gitignore
  .archive/                       # 原始会话，gitignore，0600
  .index/                         # SQLite，gitignore，可重建
```

- 节点：`root / category / project` 三种；根节点唯一；项目是叶；分类可含分类或项目。
- 条目：挂在 `project` 或 `global` 节点下；类型 `user / feedback / project / reference`。
- 标识：节点和条目都用 UUIDv7；目录名是可读 slug，不是身份。引用用稳定 ID，移动目录不会断链。
- 标签：存储 `tags_delta`；有效标签 = 路径并集 + 条目自身 delta。
- 状态：`active / superseded / deprecated / draft`；`supersedes` 与 `superseded_by` 双向一致。
- 敏感级别：`public / internal / private`，条目不比所属节点更宽松。
- 时间：RFC3339 UTC（`Z`）；文件 UTF-8、LF。

### 4.1 检索索引（`memoryd index`）

- 位置与权限：`<data>/.index/index.sqlite`，目录 0700、文件 0600（Windows 上实际由 ACL 决定），SQLite WAL；整个 `.index/` 被 gitignore。
- 构建顺序：先完整校验；有阻塞错误时退出 1，且不触碰旧索引。校验通过后从验证快照全量构建。
- 事务性：删表、建表、写入、FTS 完整性检查在同一个 SQLite 事务内；失败回滚，旧索引保持可用。损坏或 schema 不兼容的索引需要显式 `--rebuild` 才替换。
- 元数据：`meta` 表记录 `schema_version / repo_id / generation / fingerprint / built_at / nodes / entries`。fingerprint 为所有受管文件（`memory.yaml`、`node.md`、`memories/*.md`）内容的 SHA-256。
- 幂等：fingerprint 未变且未指定 `--rebuild` 时不写入，generation 不变；每次实际重建 generation 加 1。
- 表：`nodes`、`entries`、`entry_tags`，以及 external-content FTS5 表 `entries_fts(title, summary, tags, body, tokenize='trigram')`。正文只在 `entries` 存一份，`node.md` 正文不进入索引。
- 查询：每个词至少 3 个字符时走 FTS5 trigram（中文可用）；任一词不足 3 个字符时整体回退为对 title/summary/body 的转义 LIKE。LIKE 回退按命中位置计分（标题权重 2、摘要权重 1、正文权重 0），再按更新时间排序，避免更新更近但仅正文偶然命中的条目压过标题/摘要命中。默认只返回 `active`，可按节点、类型、有效标签（AND）、状态过滤，结果上限 100 条。

## 5. MCP 接口

### 5.1 工具列表（V1）

| 工具 | 作用 | 写操作 |
|---|---|---|
| `memory_brief` | 按任务和项目返回固定 token 预算的上下文简报 | 否 |
| `memory_search` | 混合搜索，返回摘要、ID、节点路径、状态 | 否 |
| `memory_get` | 按 ID 取条目；默认全文，可给 query 取片段 | 否 |
| `memory_tree` | 返回压缩树、节点摘要、计数 | 否 |
| `memory_add` | 新增条目；可同时声明 `supersedes` | 是 |
| `memory_supersede` | 显式取代旧条目 | 是 |

`memory_propose_move` 等 gardener 工具放到 V1.5，避免第一版出现自动改树能力。

实现方式：手写 JSON-RPC 2.0 over stdio（一行一条 JSON），不引入 MCP SDK；支持 `initialize`、`ping`、`tools/list`、`tools/call`，通知不返回响应，不支持批量请求。协议版本接受 2024-11-05、2025-03-26、2025-06-18，未知版本回落到 2025-06-18。所有工具标注 `readOnlyHint`。

### 5.2 `memory_brief`

输入：`task`（必填）、`project`（节点 ID 或 slug 路径，可选）、`budget_tokens`（默认 1200）。

输出分区：

1. 用户画像与长期偏好（`global` 下 `user` 条目）
2. 当前项目概览（节点摘要 + 最近 `project` 条目）
3. 与任务相关的记忆（`search` 命中的摘要）
4. 待定事项/开放问题（`project` 类型的活跃条目）
5. 警告（被取代的旧结论、敏感级别、索引过期）

只返回摘要和 ID，不返回全文；Agent 需要时再 `memory_get`。服务端只做确定性拼装，不做生成。

预算按分区轮转取用：每轮每个分区最多加入一条，避免靠前的分区吃光预算。`tokens_estimated` 是近似值（ASCII 计 0.25 token，非 ASCII 计 1 token），只统计 `data`，不计 envelope 元数据；预算不足时返回 `truncated` 警告。

**memory_get 片段模式**：默认返回完整正文。传 `query` 时按词在正文中的命中位置截取片段，最多 `max_chars` 个字符（默认 1200，范围 200–8000）；响应中 `body` 为片段，并带 `excerpted=true`、`full_chars` 和 warning。`query` 未命中正文时返回开头片段并警告；正文本身不超过 `max_chars` 时仍返回全文。全文模式保持原行为。

### 5.3 响应与错误

所有工具返回一个带元信息的 envelope：

```json
{
  "data": [],
  "meta": {
    "total": 0,
    "tokens_estimated": 0,
    "index_generation": 0,
    "warnings": []
  }
}
```

记忆内容在返回时标注 `"trust": "untrusted-history"`，提醒客户端这是历史数据，不是可执行指令。错误使用稳定错误码（如 `MEMORY_NOT_FOUND`、`NODE_NOT_FOUND`、`INVALID_ARGUMENTS`、`INDEX_ERROR`），并附带可操作的修复建议；工具错误通过 MCP 的 `isError: true` 返回，同时在 `structuredContent.error` 中给出 `code`、`message`、`suggestion`。

### 5.4 并发与一致性

- 读：SQLite WAL，多进程可并发读；索引带 `generation`。只读 MCP 在启动时校验仓库并按文件指纹刷新索引，会话期间使用该启动快照；外部手改文件后需重启服务端。损坏或 schema 不兼容的索引不会自动重建，服务端拒绝启动并提示 `memoryd index --rebuild`。
- 写：跨进程文件锁串行化；写文件用同目录临时文件 + fsync + rename；索引更新与 git commit 在同一临界区内（M2）。
- 崩溃恢复：写文件成功但 commit 失败时保留 dirty tree，`memoryd doctor` 检测并补提交；不丢数据，不自动 reset。
- 索引损坏：`memoryd index --rebuild` 从文件重建；源文件永远优先。

## 6. 采集流程

1. Agent 结束会话，把会话摘要写入 `.inbox/<agent>/<date>-<session>.md`。
2. 蒸馏（当前 Agent 执行）：读取入箱摘要 + 相关的现有记忆，产出原子条目候选；判重、决定是否 `supersedes`。
3. `memory_add` 写入；服务端做 schema 校验、脱敏、索引和提交。
4. 原始摘要归档到 `.archive/`，默认不进 git；需要时可重新蒸馏。

V1 用"会话结束提示 + 手动触发"即可，不做后台常驻监听。适配器只是输入格式的转换，不应成为第一版的核心工程量。

## 7. gardener 流程

1. 生成数据包：节点摘要、有效标签、子节点数、最近变更、检索失败日志。
2. Agent 产出提案 JSON：操作列表（`move / split / merge / retag`）、理由、受影响 ID、预期 effective-tag diff。
3. `memoryd` 校验：根唯一、无环、slug 冲突、条目不越级、敏感级别不降级。
4. 在独立分支/补丁中应用，附审计报告；人批准后合并到主分支。
5. 不通过则丢弃提案，原始树不变。

V1 只生成提案，不自动应用。等真实语料证明树需要频繁调整后，再讨论自动合并低风险操作。

## 8. 安全与隐私

### 8.1 威胁模型

- 记忆内容来自对话和外部文档，可能包含 prompt injection、密钥、个人信息。
- 数据仓库可能被同步到云盘、被其他本地进程读取、被恶意仓库替换。
- MCP 客户端可能被诱导执行记忆中的指令。
- 第三方依赖和发布产物存在供应链风险。

### 8.2 控制措施

- 默认本地：V1 只开 stdio，不开端口、不联网、不遥测；核心不调用外部模型。
- 路径约束：所有文件操作限制在数据仓库根目录；解析真实路径后拒绝逃逸；格式管理的文件拒绝符号链接。
- YAML 安全：解析器禁用自定义 tag；限制 frontmatter 大小（建议 64 KiB）、正文大小（建议 1 MiB）；对解析器做 fuzz 测试。
- 脱敏：采集和写入前扫描常见密钥格式（云厂商 AK、GitHub token、私钥头、密码字段等）；命中则替换为 `[REDACTED]`，并在元信息里记录脱敏次数，不记录原文。
- 权限：数据目录 0700，文件 0600（Windows 上使用等效 ACL/继承策略）；raw archive 默认不进 git。
- prompt injection：MCP 响应明确标记内容为历史数据；工具不提供"执行命令/访问网络"能力；文档要求客户端不把记忆当系统指令。
- 导出与共享：按 `sensitivity` 过滤；默认导出不含 private 条目；不自动 push 到远端。
- 供应链：签名 release、SHA256 校验和、SBOM、`govulncheck`、依赖许可证检查；发布包附 LICENSE/NOTICE/THIRD_PARTY_NOTICES。

### 8.3 数据归属

用户的记忆内容归用户所有。Apache-2.0 只覆盖 MemoryTree 软件和仓库内文档/示例，不覆盖用户数据。README 和首次初始化时都要明确这一点。

## 9. 兼容性与质量基线

- 应用版本用 SemVer；数据格式有独立的 `format_version` 整数。
- 读取器遇到更高 major 格式必须拒绝并给出升级提示；未知 frontmatter 字段必须原样保留。
- 迁移必须显式执行：`memoryd migrate --to N --dry-run`，自动备份，幂等，失败可回滚。
- 测试：schema/校验单测、树操作属性测试、格式 golden 测试、MCP 集成测试、多进程并发测试、路径/脱敏安全测试、10k 条目性能基准。
- CI：Linux/macOS/Windows；`go vet`、`staticcheck`/`golangci-lint`、`go test -race`、`govulncheck`、许可证扫描。
- 发布：GoReleaser；zip/tar.gz 附 LICENSE/NOTICE/THIRD_PARTY_NOTICES；校验和；Sigstore/cosign 或 minisign 签名；SBOM。
- 模块路径：`go.mod` 目前暂用 `github.com/li-ziyin/memorytree`；公开 GitHub 仓库建立后、首次 push 或打 tag 之前必须改成真实地址，之后不再变动。
- 文档：README、设计、格式规范、MCP API、迁移、威胁模型、贡献指南。当前 M0/M1 阶段中文先行，公开首发前补齐英文版；之后英文为主、中文同步。

## 10. 里程碑与验收

### M0：格式与骨架

- 完成 `spec/memory-format-v1.md`、三份 JSON Schema、示例数据。
- Go 模块骨架、`memoryd init/validate/index` 的 CLI 框架。
- 校验器覆盖：根唯一、无环、kind 规则、slug/ID、tags、状态、supersedes、敏感级别。
- 导入现有 38 条共享记忆（legacy 模式，不强行原子化）。
- 验收：示例仓库 `validate` 全绿；故意破坏每个不变量都能报出稳定错误码。

### M1：只读 MCP

前置（已于 2026-10-20 完成）：源码仓与数据仓各完成一次 `git init` 和初始提交。数据仓的初始提交必须在 `memoryd validate` 全绿之后，用来冻结 M0 基线；M1 及之后的索引、写入才有可审计的起点。

1. `memoryd index`（已完成 2026-10-21）：FTS5 trigram 构建与 `--rebuild`，源文件 fingerprint 不变时跳过，重建时 generation 递增；已独立验收，不依赖 MCP。
2. 只读 MCP（已完成 2026-10-21）：`memory_brief / search / get / tree`，手写 JSON-RPC 2.0 stdio；启动时校验并刷新索引，坏索引要求显式 `--rebuild`。所有读工具依赖第 1 步的索引。真实数据仓冒烟通过；接入 Qoder 客户端待做。

- FTS5 trigram 中文检索可用；索引可重建。
- 验收（2026-10-25 复测）：20 个真实问题，对比 `MEMORY.md + grep` 基线。20/20 完整命中、20/20 首条命中；每题独立会话 14,723 vs 67,796 token（约省 78%），同会话摊薄基线 13,095 token（基本持平）。首轮发现的 LIKE 排序与 `memory_get` 全文成本已修复：LIKE 按标题/摘要/正文分层排序，`memory_get` 支持 `query` 片段模式。数据仓仍缺 3 条新增记忆，留待 M2 补齐后复测。详见 [docs/m1-acceptance.md](docs/m1-acceptance.md)。

### M2：写入与采集

- `memory_add / memory_supersede`，原子写、锁、git commit。
- 一条真实采集链路（Qoder 会话或 ZCode 记忆），inbox + 蒸馏提示词。
- 验收：并发写入无丢失；崩溃后可恢复；旧条目被取代后默认检索不再返回。

### M3：挂载与 gardener

- 新条目自动匹配挂载节点（标签 + 摘要相似度，Agent 参与判定）。
- gardener 提案流程与 dry-run 应用。
- 验收：插入 100 条真实记忆，挂载准确率可接受；提案不破坏不变量；与扁平基线对比有明确结论。

## 11. 待定问题

1. 当前项目如何从 cwd 自动映射到节点：V1 由调用方显式传 `project`，还是加本地 `projects.yaml` 映射？
2. 节点 `summary` 由谁维护、何时失效：Agent 写入时更新，还是 gardener 定期重生成？
3. 默认最大深度/分支因子（建议 4/8）是否写进规范，还是只作为工具策略？
4. `memory_add` 是否允许不传 `summary` 时机械截断正文生成，还是强制要求语义摘要？
5. Git 是否作为硬依赖：V1 要求系统 git，缺失时只读可用；还是内置 go-git？
6. 原始 `.archive/` 是否需要可选的静态加密，还是先依赖 OS 权限？
7. 多设备同步是否只依赖用户自己的 git remote/云盘，不提供内置同步？

## 12. 相关文档

- [格式规范 v1](spec/memory-format-v1.md)
- [贡献指南](CONTRIBUTING.md)
- [安全策略](SECURITY.md)
- JSON Schema：`spec/schema/repo.schema.json`、`node.schema.json`、`entry.schema.json`
