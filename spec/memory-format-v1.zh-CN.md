# MemoryTree Memory Format v1（中文同步版）

> 本文件是 [spec/memory-format-v1.md](memory-format-v1.md)（英文，权威版本）的中文同步版；规则码与 YAML 结构两边一致，冲突时以英文版为准。

- 规范版本：1.0.0（draft）
- 数据格式版本：`format_version: 1`
- 状态：Draft，2026-10-20
- 许可证：Apache-2.0（本文档与 JSON Schema）；用户记忆数据不属于该许可证范围

本文定义 MemoryTree 数据仓库的磁盘布局、frontmatter 字段、标签语义、状态机与校验规则。索引结构和 MCP 接口不在本规范范围内。

关键词 **MUST**、**MUST NOT**、**SHOULD**、**SHOULD NOT**、**MAY** 按 RFC 2119 / RFC 8174 解释。

## 1. 范围

一个 MemoryTree 数据仓库是一个目录，包含：

- 一棵由目录和 `node.md` 组成的节点树；
- 项目节点和保留的 `global` 分类下的记忆条目；
- 一个仓库级配置文件 `memory.yaml`；
- 可选的保留目录 `.inbox/`、`.archive/`、`.index/`（不属于格式管理范围）；
- 可选的 `.git/`（推荐，用于审计与恢复）。

格式只规定文件内容，不规定工具如何索引、检索或生成摘要。

## 2. 一致性

一个实现要声明自己符合本规范，必须：

1. 能读取并原样保留未知 frontmatter 字段；
2. 能执行第 12 节的全部 MUST 校验；
3. 遇到更高 major 的 `format_version` 时拒绝打开并给出明确错误；
4. 写入时使用本规范的字段名、类型和枚举值；
5. 不删除不属于本规范的未知文件或目录。

## 3. 仓库布局

```
<repo-root>/
├── memory.yaml
├── node.md                         # root 节点
├── global/                         # 保留分类，只能放条目
│   ├── node.md
│   └── memories/
│       └── 2026-10-20-user-profile.md
├── work/                           # 普通分类
│   ├── node.md
│   └── agentflow/                  # 项目叶
│       ├── node.md
│       └── memories/
│           └── 2026-10-20-sse-pitfall.md
├── .inbox/                         # 原始采集，工具保留
├── .archive/                       # 原始会话，工具保留
├── .index/                         # 派生索引，工具保留
└── .git/                           # 可选
```

规则：

- 仓库根目录 MUST 包含 `memory.yaml` 和根 `node.md`。
- 每个表示节点的目录 MUST 包含 `node.md`。
- 只有 `project` 节点和保留的 `global` 分类 MAY 包含 `memories/` 目录。
- `global` 是根下的保留分类，`slug` MUST 为 `global`；它 MUST NOT 包含子节点，只能包含条目。
- `.inbox/`、`.archive/`、`.index/` 是保留目录，读者 MUST 忽略其内容；它们 SHOULD 被数据仓库的 `.gitignore` 排除。
- 其他未知文件/目录 MAY 存在；工具 MUST NOT 删除或改写它们。

## 4. 标识符与引用

### 4.1 ID

节点和条目的 `id` MUST 是 RFC 9562 定义的 UUIDv7，使用小写规范形式：

```
^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$
```

ID 在同一个仓库内 MUST 全局唯一（节点和条目共用一个命名空间）。ID 一旦分配 MUST NOT 改变。

### 4.2 slug

目录名和节点的 `slug` 是供人阅读的稳定标识，不是身份。非根节点的 `slug` MUST：

- 匹配 `^[a-z0-9][a-z0-9-]{0,63}$`；
- 与所在目录名完全一致；
- 在同一父节点下唯一；
- 不使用保留名：`node.md`、`memories`、`.inbox`、`.archive`、`.index`、`global`（`global` 仅允许作为根下的保留分类）。

根节点的 `slug` MUST 为空字符串，目录就是仓库根。

### 4.3 引用

Markdown 正文中的稳定引用：

```
memory://<entry-id>
node://<node-id>
```

例如：

```markdown
参见 [SSE 解码坑](memory://019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b06)。
```

引用 MUST 使用 ID，不得使用目录路径。目录重命名或移动不得破坏引用。实现 SHOULD 在读取时解析 `memory://` 和 `node://` 并检查目标存在。

文件名和目录名不是身份；工具不得依赖它们做持久引用。

## 5. 标签与有效标签

### 5.1 存储形式

每个节点和条目只存自己的增量标签：

```yaml
tags_delta: [work, java]
```

有效标签按路径求并：

```
effective_tags(node) = effective_tags(parent) ∪ node.tags_delta
effective_tags(entry) = effective_tags(entry.node) ∪ entry.tags_delta
```

根节点的 `tags_delta` SHOULD 为空数组；它的有效标签为空集。

例子：

| 节点 | `tags_delta` | 有效标签 |
|---|---|---|
| `/` | `[]` | `{}` |
| `/work` | `[work]` | `{work}` |
| `/work/agentflow` | `[java, agent]` | `{work, java, agent}` |
| `agentflow` 下某条目 | `[sse]` | `{work, java, agent, sse}` |

因此"子节点的有效标签集 ⊇ 父节点的有效标签集"由数据结构保证，不需要在移动时重写后代文件。移动节点只改变 `parent` 和目录位置；有效标签在读取时重算。

### 5.2 标签语法

- 标签 MUST 先做 NFC 规范化、去除首尾空白、转小写。
- 规范化后 MUST 匹配 `^[\p{L}\p{N}_-]{1,32}$`（允许中文、字母、数字、下划线、连字符；不允许空格）。
- `tags_delta` 内部 MUST 不重复。
- 节点/条目的 `tags_delta` MUST NOT 包含已经从祖先继承的标签（无意义的重复会让 diff 失真）。
- 标签顺序无语义；写入时 SHOULD 按字典序排序，保证可复现。

### 5.3 展示与检索

文件和规范存储的是 delta；面向用户和检索的展示 MUST 使用 effective tags。工具 MAY 在生成的树视图、索引或摘要中显示 effective tags，但 MUST NOT 把它们回写到 `node.md` / 条目 frontmatter。

## 6. 仓库元数据 `memory.yaml`

位置：仓库根目录。编码 UTF-8，换行 LF。

```yaml
schema: memorytree.repo/v1
format_version: 1
repo_id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b00
created_at: "2026-10-20T00:00:00Z"
default_sensitivity: private
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `schema` | string | 是 | 固定为 `memorytree.repo/v1` |
| `format_version` | integer | 是 | v1 为 `1` |
| `repo_id` | string (UUIDv7) | 是 | 仓库唯一 ID，创建后不变 |
| `created_at` | string (RFC3339) | 是 | 仓库创建时间 |
| `default_sensitivity` | enum | 否 | `public` / `internal` / `private`，默认 `private` |
| `description` | string | 否 | 仓库用途说明 |

未知字段 MUST 被保留。

## 7. 节点文件 `node.md`

每个节点目录一个 `node.md`。YAML frontmatter + 可选 Markdown 正文。

```yaml
---
schema: memorytree.node/v1
id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b04
kind: project
parent: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b03
slug: agentflow
title: AgentFlow
summary: Spring Boot 多 Agent 工作流平台，求职项目。
tags_delta: [java, spring, agent]
aliases: [agent-flow]
status: active
sensitivity: private
created_at: "2026-10-20T00:00:00Z"
updated_at: "2026-10-20T00:00:00Z"
---
项目备注可以写在这里；正文不参与默认检索。
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `schema` | string | 是 | 固定为 `memorytree.node/v1` |
| `id` | string (UUIDv7) | 是 | 节点 ID |
| `kind` | enum | 是 | `root` / `category` / `project` |
| `parent` | string (UUIDv7) \| null | 是 | 根节点为 `null` |
| `slug` | string | 是 | 根为空；非根为目录名 |
| `title` | string | 是 | 1–120 字符 |
| `summary` | string | 是 | 1–500 字符；建议 ≤280；用于导航 |
| `tags_delta` | array&lt;string&gt; | 是 | 本节点新增标签 |
| `aliases` | array&lt;string&gt; | 否 | 检索别名，默认 `[]` |
| `status` | enum | 是 | `active` / `archived` |
| `sensitivity` | enum | 是 | `public` / `internal` / `private` |
| `created_at` | string (RFC3339) | 是 | 创建时间 |
| `updated_at` | string (RFC3339) | 是 | 最后更新时间，不早于 `created_at` |

### 7.1 kind 规则

- 仓库 MUST 有且只有一个 `kind: root` 节点；它的 `parent` MUST 为 `null`，`slug` MUST 为 `""`。
- `kind: project` 的节点 MUST NOT 有子节点；它可以有 `memories/`。
- `kind: category` 的节点 MAY 有子节点；除保留的 `global` 外，MUST NOT 直接放条目。
- 根节点 MAY 直接包含分类或项目。
- `global` MUST 是根下的 `kind: category` 节点，`slug` 为 `global`；它 MUST NOT 有子节点，MAY 有 `memories/`。

### 7.2 状态

- `active`：正常参与默认树和检索。
- `archived`：默认树和检索可以隐藏；仍可显式查询。归档一个节点不会改变其后代的 `status`，但工具 SHOULD 让后代继承"归档视图"。

### 7.3 正文

`node.md` 的正文是可选的人类备注，不参与默认检索，也不得被当作指令执行。工具写入时可以保留原文，不应擅自重写正文。

## 8. 条目文件

条目位于 `memories/` 下，文件名 SHOULD 为：

```
<YYYY-MM-DD>-<slug>.md
```

例如 `2026-10-20-sse-pitfall.md`。文件名不是身份；冲突时 MAY 追加短 ID 后缀。文件名 MUST 使用 ASCII slug 字符。

```yaml
---
schema: memorytree.entry/v1
id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b06
node: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b04
type: project
status: active
sensitivity: private
title: SSE 解码坑
summary: 前端 SSE 解码会把空格吞掉，需要按原始字节处理。
tags_delta: [sse, frontend]
source:
  agent: zcode
  kind: conversation
  session: sess_example
  captured_at: "2026-10-20T00:00:00Z"
supersedes: []
superseded_by: null
related: []
created_at: "2026-10-20T00:00:00Z"
updated_at: "2026-10-20T00:00:00Z"
---
正文使用 Markdown。相关记忆用 `memory://<id>` 引用。
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `schema` | string | 是 | 固定为 `memorytree.entry/v1` |
| `id` | string (UUIDv7) | 是 | 条目 ID |
| `node` | string (UUIDv7) | 是 | 所属 `project` 或 `global` 节点 |
| `type` | enum | 是 | `user` / `feedback` / `project` / `reference` |
| `status` | enum | 是 | `active` / `superseded` / `deprecated` / `draft` |
| `sensitivity` | enum | 是 | `public` / `internal` / `private` |
| `title` | string | 是 | 1–200 字符 |
| `summary` | string | 是 | 1–500 字符；建议 ≤280；检索主要返回它 |
| `tags_delta` | array&lt;string&gt; | 是 | 本条新增标签 |
| `source` | object | 是 | 来源与 provenance |
| `supersedes` | array&lt;UUIDv7&gt; | 是 | 被本条取代的旧条目，默认 `[]` |
| `superseded_by` | string (UUIDv7) \| null | 是 | 取代本条的新条目 |
| `related` | array&lt;UUIDv7&gt; | 否 | 相关条目，默认 `[]` |
| `created_at` | string (RFC3339) | 是 | 创建时间 |
| `updated_at` | string (RFC3339) | 是 | 最后更新时间 |

### 8.1 type 语义

- `user`：稳定的用户画像、偏好、沟通约定。
- `feedback`：对 Agent 工作方式的纠正、教训、验收偏好。
- `project`：项目状态、决策、踩坑、待办。
- `reference`：外部资料、命令、链接、可复用知识。

类型是检索过滤条件，不限制正文内容。

### 8.2 source

```yaml
source:
  agent: qoder
  kind: conversation
  session: sess_...
  captured_at: "2026-10-20T00:00:00Z"
  ref: ".inbox/qoder/2026-10-20-session.md"
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `agent` | string | 是 | 来源 Agent：`zcode` / `opencode` / `qoder` / `manual` / `import` 或自定义 |
| `kind` | enum | 是 | `conversation` / `document` / `manual` / `import` |
| `session` | string | 否 | 会话标识 |
| `captured_at` | string (RFC3339) | 否 | 原始内容采集时间 |
| `ref` | string | 否 | 相对路径或 URL；不得是本地绝对路径 |

`source` 是 provenance 的一部分。检索返回条目时应同时返回来源和状态，避免把旧结论当最新事实。

### 8.3 正文

正文是 UTF-8 Markdown，最大 1 MiB（实现 MAY 拒绝更大的文件）。正文是数据，不是指令；实现不得解析或执行其中的命令。

## 9. 状态与取代

### 9.1 状态

- `draft`：尚未确认，默认检索不返回（除非显式请求）。
- `active`：当前有效。
- `superseded`：已被新条目取代，默认检索不返回。
- `deprecated`：已失效但没有替代条目；默认检索不返回。

### 9.2 状态迁移

```
draft      -> active | deprecated
active     -> superseded | deprecated
deprecated -> active        （显式恢复，需记录）
superseded -> （终态，不再改状态）
```

### 9.3 取代关系

- 新条目的 `supersedes` 非空时，新条目 MUST 为 `active`。
- 被取代的旧条目 MUST 变为 `superseded`，且 `superseded_by` MUST 指向新条目。
- 新条目的 `supersedes` MUST 包含旧条目 ID；旧条目的 `superseded_by` MUST 包含在新条目的 `supersedes` 中。双向一致。
- `supersedes` MUST NOT 包含自身；取代链 MUST 无环。
- `superseded_by` MUST 仅在 `status: superseded` 时非空。

## 10. 敏感级别

顺序（从宽到严）：

```
public < internal < private
```

- 条目 `sensitivity` MUST 不低于所属节点的 `sensitivity`。
- 节点 `sensitivity` MUST 不低于父节点的 `sensitivity`。
- 未设置时使用 `memory.yaml.default_sensitivity`（默认 `private`）。
- 本地检索 MAY 返回任意级别，但 MUST 带 `sensitivity` 字段。
- 导出、共享、生成公开摘要时 MUST 过滤掉高于导出级别的条目。
- 原始 `.archive/` 默认按 `private` 处理。

## 11. 时间、编码与路径

- 时间戳 MUST 是 RFC 3339；SHOULD 使用 UTC 并以 `Z` 结尾。
- 文本文件 MUST 是 UTF-8，无 BOM；换行 SHOULD 为 LF。
- 标题、摘要、标签 SHOULD 使用 NFC 规范化。
- frontmatter MUST 是合法的 YAML 1.2 子集；禁止自定义 tag、锚点和别名（解析器 SHOULD 拒绝）。
- frontmatter 大小 SHOULD ≤64 KiB；正文 SHOULD ≤1 MiB。
- 元数据中 MUST NOT 出现本地绝对路径、盘符路径或 `..` 路径段。
- 格式管理的文件 MUST NOT 是符号链接。

## 12. 校验规则

实现 MUST 在写入前执行以下校验；`memoryd validate` 应能对整仓执行并输出稳定错误码。

### 12.1 结构

- **V-ROOT-1**：有且只有一个 `kind: root` 的节点，且位于仓库根。
- **V-PARENT-1**：每个非根节点的 `parent` 存在、不是自身、且不形成环。
- **V-KIND-1**：`project` 节点没有子节点目录。
- **V-KIND-2**：条目只能挂在 `project` 或 `global` 节点。
- **V-KIND-3**：`global` 节点没有子节点。
- **V-SLUG-1**：`slug` 与目录名一致；同父下唯一；符合 4.2 的语法和保留名规则。
- **V-ID-1**：节点和条目的 ID 都是 UUIDv7，且全仓唯一。
- **V-ID-2**：所有 `parent`、`node`、`supersedes`、`superseded_by`、`related` 引用的 ID 都存在。
- **V-FILE-1**：格式管理的文件不是符号链接；frontmatter 可解析。

### 12.2 标签

- **V-TAG-1**：每个标签符合 5.2 的规范化与语法。
- **V-TAG-2**：`tags_delta` 内无重复，且不包含已从祖先继承的标签。
- **V-TAG-3**：有效标签按 5.1 计算；展示层不得把 effective tags 写回文件。

### 12.3 状态与取代

- **V-SUP-1**：`supersedes` 不含自身，链无环。
- **V-SUP-2**：`supersedes` / `superseded_by` 双向一致。
- **V-SUP-3**：`superseded_by` 仅在 `status: superseded` 时非空。
- **V-SUP-4**：`supersedes` 非空的新条目状态为 `active`。
- **V-SUP-5**：`superseded` 是终态。

### 12.4 敏感级别与时间

- **V-SENS-1**：节点敏感级别不低于父节点；条目不低于所属节点。
- **V-TIME-1**：时间戳可解析；`updated_at` 不早于 `created_at`。
- **V-TIME-2**：条目 `created_at` 与文件名日期 SHOULD 一致；不一致只告警。

### 12.5 内容

- **V-CONTENT-1**：`title`、`summary` 非空且在长度限制内。
- **V-CONTENT-2**：`source.agent`、`source.kind` 存在；`source.ref` 不是绝对路径。
- **V-CONTENT-3**：正文大小在限制内。

### 12.6 警告（非致命）

- 节点深度超过 4。
- 同一父节点下子节点超过 8。
- `summary` 超过 280 字符。
- 索引 generation 落后于文件状态。
- 条目的 `created_at` 与其文件名日期不一致。

## 13. 版本与迁移

- `memory.yaml.format_version` 是整数 major 版本；v1 为 `1`。
- 增加可选字段、增加枚举的兼容值、澄清文档：属于 minor 变更，不提升 `format_version`；规范版本号递增（如 1.1.0）。
- 删除/重命名字段、改变语义、改变必填性：属于 major 变更，必须提升 `format_version`。
- 读取器 MUST 接受所有 v1 仓库；遇到更高的 major MUST 拒绝并返回 `FORMAT_UNSUPPORTED`。
- 写入器遇到未知字段 MUST 原样保留。
- 迁移 MUST 显式执行，不得在打开仓库时自动迁移；迁移前自动备份，支持 `--dry-run`，幂等，失败可回滚。
- 应用版本（SemVer）与数据格式版本相互独立。

## 14. 完整示例

以下示例是一个最小但合法的仓库。

### 14.1 `memory.yaml`

```yaml
schema: memorytree.repo/v1
format_version: 1
repo_id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b00
created_at: "2026-10-20T00:00:00Z"
default_sensitivity: private
```

### 14.2 根 `node.md`

```yaml
---
schema: memorytree.node/v1
id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b01
kind: root
parent: null
slug: ""
title: MemoryTree
summary: 个人与跨项目记忆的根节点。
tags_delta: []
aliases: [root]
status: active
sensitivity: private
created_at: "2026-10-20T00:00:00Z"
updated_at: "2026-10-20T00:00:00Z"
---
```

### 14.3 `global/node.md`

```yaml
---
schema: memorytree.node/v1
id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b02
kind: category
parent: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b01
slug: global
title: 全局
summary: 跨项目通用的用户画像、偏好与长期约定。
tags_delta: [global]
aliases: []
status: active
sensitivity: private
created_at: "2026-10-20T00:00:00Z"
updated_at: "2026-10-20T00:00:00Z"
---
```

### 14.4 `global/memories/2026-10-20-user-profile.md`

```yaml
---
schema: memorytree.entry/v1
id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b05
node: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b02
type: user
status: active
sensitivity: private
title: 用户画像
summary: 中文鸿蒙开发者、大学生，偏好先定方案再动手。
tags_delta: [profile]
source:
  agent: manual
  kind: manual
  captured_at: "2026-10-20T00:00:00Z"
supersedes: []
superseded_by: null
related: []
created_at: "2026-10-20T00:00:00Z"
updated_at: "2026-10-20T00:00:00Z"
---
用户是中文交流的鸿蒙生态开发者，偏好先讨论方案再写代码。
```

### 14.5 `work/node.md`

```yaml
---
schema: memorytree.node/v1
id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b03
kind: category
parent: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b01
slug: work
title: 工作
summary: 求职、项目与职业相关记忆。
tags_delta: [work]
aliases: [career]
status: active
sensitivity: private
created_at: "2026-10-20T00:00:00Z"
updated_at: "2026-10-20T00:00:00Z"
---
```

### 14.6 `work/agentflow/node.md`

```yaml
---
schema: memorytree.node/v1
id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b04
kind: project
parent: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b03
slug: agentflow
title: AgentFlow
summary: Spring Boot 多 Agent 工作流平台，求职项目。
tags_delta: [java, spring, agent]
aliases: [agent-flow]
status: active
sensitivity: private
created_at: "2026-10-20T00:00:00Z"
updated_at: "2026-10-20T00:00:00Z"
---
```

有效标签：`{work, java, spring, agent}`。

### 14.7 `work/agentflow/memories/2026-10-20-sse-pitfall.md`

```yaml
---
schema: memorytree.entry/v1
id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b06
node: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b04
type: project
status: active
sensitivity: private
title: SSE 解码坑
summary: 前端 SSE 解码会把空格吞掉，需要按原始字节处理。
tags_delta: [sse, frontend]
source:
  agent: zcode
  kind: conversation
  session: sess_example
  captured_at: "2026-10-20T00:00:00Z"
supersedes: []
superseded_by: null
related: []
created_at: "2026-10-20T00:00:00Z"
updated_at: "2026-10-20T00:00:00Z"
---
前端 SSE 解码时按字符处理会吞掉空格，改为按原始字节解码。
```

有效标签：`{work, java, spring, agent, sse, frontend}`。

## 15. 安全考虑

- 所有记忆内容都应按不可信数据处理；读取器不得执行正文里的命令或代码块。
- 写入前 SHOULD 扫描常见密钥格式并脱敏；脱敏日志不得记录原文。
- 格式管理的文件拒绝符号链接；所有路径解析后必须位于仓库根目录内。
- YAML 解析器必须禁用自定义 tag，限制锚点/别名和文件大小，防止解析炸弹。
- 数据静态加密不在本规范范围内；默认依赖操作系统权限。`.archive/` 应使用 0600 权限且不进 git。
- 导出/共享必须遵守 `sensitivity`；默认不导出 `private`。
- 不自动 push 到远端；是否使用云盘/git remote 由用户决定，并在文档中说明泄露风险。

## 16. JSON Schema

以下 schema 校验 frontmatter 解析后的元数据对象，不校验 Markdown 正文：

- [`spec/schema/repo.schema.json`](schema/repo.schema.json)
- [`spec/schema/node.schema.json`](schema/node.schema.json)
- [`spec/schema/entry.schema.json`](schema/entry.schema.json)

结构规则（根唯一、无环、kind 限制、引用存在等）无法完全由 JSON Schema 表达，由 `memoryd validate` 执行第 12 节规则。Schema 与规范冲突时，以本规范为准；实现应同时通过两者。

## 附录 A：字段速查

| 对象 | 必填字段 |
|---|---|
| `memory.yaml` | `schema`, `format_version`, `repo_id`, `created_at` |
| 节点 | `schema`, `id`, `kind`, `parent`, `slug`, `title`, `summary`, `tags_delta`, `status`, `sensitivity`, `created_at`, `updated_at` |
| 条目 | `schema`, `id`, `node`, `type`, `status`, `sensitivity`, `title`, `summary`, `tags_delta`, `source`, `supersedes`, `superseded_by`, `created_at`, `updated_at` |

## 附录 B：术语

- **节点**：树上的一个目录，代表分类或项目。
- **条目**：一条原子记忆，位于项目或 `global` 下。
- **delta 标签**：节点/条目自己新增的标签，不含继承部分。
- **有效标签**：沿路径求并后得到的完整标签集。
- **generation**：索引对应的数据版本号，用于判断索引是否过期。
- **gardener**：离线提出树结构调整提案的 Agent 任务。
