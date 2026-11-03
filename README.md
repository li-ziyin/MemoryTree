# MemoryTree

[English](README.en.md)

给 AI 编码助手用的本地记忆层：一棵 Markdown 记忆树 + 一个 MCP 服务器（Go），ZCode、OpenCode、Qoder 等多个客户端共用同一份记忆。

## 为什么做

跨客户端共享 agent 记忆，现状通常是一个共享笔记文件夹——可读，但没有采集、出处、新鲜度、检索，也没有项目边界，agent 很容易把别的项目的笔记当成当前状态。MemoryTree 把共享记忆做成一层独立的基础设施：

- 记忆是纯 Markdown 树：人可读，git 记录每次变更，随时整体迁走，无数据库锁定
- 文件是唯一真相：SQLite FTS5 只是可随时重建的索引，删掉无损
- MCP 是唯一写入口：校验、出处、去重、原子写入、git 提交集中在一处
- 会话结束自动采集：钩子把会话摘要脱敏后收入 inbox，再蒸馏入库
- 核心零 LLM 调用、零联网、零遥测：本地优先，数据属于你

## 工作原理

```
 ZCode / OpenCode / Qoder / other MCP clients
                    |
                    | MCP over stdio
                    v
              +-----------+
              |  memoryd  |
              |-----------|
              | validate  |
              | store     |----> data repo (Markdown tree + git)
              | index     |----> SQLite FTS5 (rebuildable)
              | brief     |
              +-----------+
                    ^
                    | proposals / approval
              +-----------+
              | gardener  |  (agent-driven, offline)
              +-----------+
```

## 快速开始

需要 Go 1.26+ 与 Git 2.30+。

```sh
go build -o bin/memoryd ./cmd/memoryd

# 初始化数据仓（与源码分离，放任意目录；Windows 下如 C:\Users\you\memorytree-data）
./bin/memoryd init --title "My Memory" ~/.memorytree

# 一键接入客户端（zcode / qoder / workbuddy / opencode / deepseek；写前自动备份配置，--remove 移除）
./bin/memoryd install --client zcode ~/.memorytree

# 可选：会话结束自动采集钩子（zcode / opencode）
./bin/memoryd install --hooks --client zcode ~/.memorytree

# 多机同步：写锁下 fetch + rebase + push，脏树拒绝
./bin/memoryd sync ~/.memorytree
```

不在上述列表的 MCP 客户端，手动加一段即可：

```json
{
  "mcpServers": {
    "memorytree": {
      "command": "<memoryd 绝对路径>",
      "args": ["mcp", "<数据目录绝对路径>"]
    }
  }
}
```

其余命令速查：`validate` 校验 / `index` 建索引（损坏 `--rebuild`）/ `add` 写单条 / `inbox` 原始采集 / `doctor` 崩溃恢复 / `export` 按敏感度过滤导出 / `bench` 检索回归门。`memoryd --help` 看全部。

## 状态

M1–M5 全部完成并验收；尚未打第一个 tag 版本。

| 里程碑 | 内容 | 验收记录 |
|---|---|---|
| M1 | 格式 v1、校验器、FTS5 索引、只读 MCP 服务 | [docs/m1-acceptance.md](docs/m1-acceptance.md) |
| M2 | 写网关 memory_add / memory_supersede、单写者锁、git 提交 | [docs/m2-acceptance.md](docs/m2-acceptance.md) |
| M3 | 挂载建议、园丁（dry-run 提案 + 单提交应用） | [docs/m3-acceptance.md](docs/m3-acceptance.md) |
| M4 | 会话自动捕获、状态脱敏、多机 sync、用量信号 | [docs/m4-acceptance.md](docs/m4-acceptance.md) |
| M5 | 使用热度加权检索、attic 退役模式 | [docs/m5-acceptance.md](docs/m5-acceptance.md) |

CI：三平台测试矩阵 + lint + govulncheck + race，依赖白名单与 THIRD_PARTY_NOTICES 锁定。推送 `v*` tag 自动构建跨平台产物（校验和、SBOM、cosign 签名，draft release）。

## 刻意推迟的事

| 项 | 什么时候做 | 为什么现在不做 |
|---|---|---|
| 增量索引 | 超过约 1 万条、全量重建可感时 | 当前全量重建约 1.2s/千条，简单且正确 |
| 语义检索（embedding） | V2，等真实检索日志积累 | 关键词 + BM25 已 21/21 首中，新索引须先打赢这条基线 |
| 内嵌 go-git | 系统 git 真正挡住某个场景时 | 单静态二进制是目标；目前每台写机都有 git |
| `.archive/` 静态加密 | 威胁模型包含本机磁盘失陷时 | 现阶段 OS 权限（0600、不入 git）已足够 |

## 数据归属

Apache-2.0 只覆盖本仓库的软件与文档，**不覆盖你记忆仓里的内容**——你的记忆始终是你的。

## 文档

- [设计文档](DESIGN.zh-CN.md)（[English](DESIGN.md)）
- [记忆格式 v1](spec/memory-format-v1.zh-CN.md)（[English](spec/memory-format-v1.md)）
- [MCP API](docs/mcp-api.md) · [客户端接入](docs/clients.md) · [园丁工作流](docs/gardener.zh-CN.md)
- [全部文档](docs/README.md)（含各里程碑验收记录、性能基线、采集流程）

## License

Apache-2.0，见 [LICENSE](LICENSE) 与 [NOTICE](NOTICE)。贡献流程见 [CONTRIBUTING.md](CONTRIBUTING.md)，安全问题走 [SECURITY.md](SECURITY.md)。
