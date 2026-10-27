# M4 采集、加固与同步验收（2026-10-27）

代码：M4 系列提交（`4d5f447` 写入重试 → `91e5d69` 状态脱敏 → `f40c366` 用量信号 → `da12329` sync → `e82f2fa` 会话捕获），数据仓 `E:\MemoryTreeData`。

## DESIGN M4 验收逐条

| 验收项 | 结果 | 证据 |
|---|---|---|
| 会话结束自动捕获 hooks | ✅ | `memoryd capture-session`：合并 stdin 钩子载荷与旗标，从 Claude 协议 JSONL transcript 提取首个用户请求与末个助手结论（4 MiB 头尾截取、每段 4000 字上限、跳过 tool_use/tool_result），无 transcript 时降级为元数据采集；入库走 inbox，凭据与状态脱敏先行。`memoryd install --hooks`：ZCode 写入 `~/.zcode/cli/config.json` 的 `hooks.events.Stop`（`type: process` 参数向量、`hooks.enabled: true`、保留外部 hooks、幂等、可 `--remove`）；OpenCode 写入自动发现的 `~/.config/opencode/plugins/memorytree-capture.js`（`session.idle` 事件、`Bun.spawn` 直调、不改配置文件）。Qoder/WorkBuddy/DeepSeek 无已验证的会话结束机制，文档按手工路径说明 |
| 状态脱敏 | ✅ | `internal/redact` 扩展四类：home 锚定绝对路径（`C:\Users\…`/`c:\users\…`/`/home/…`/`/Users/…`/`/root/…`，边界捕获组防止 URL 尾部误配）、`HOME`/`USERPROFILE` 赋值、标签字段中的无点主机名（`host:`/`hostname:`/`COMPUTERNAME=`）、无点 `user@host` 提示符记号。「无点」判据天然排除邮箱、FQDN 与 IP；仓库相对引用、URL、非 home 绝对路径保持原样——误伤策略写入包注释、docs/mcp-api.md 与 DESIGN 8.2（中英同步），并有钉死测试 `TestScanKeepsQualifiedAndRelativeText`。写入响应沿用 `redactions` 总计数 |
| `memoryd sync` | ✅ | `internal/reposync` + gitio 硬化原语（Branch/Upstream/InProgress/Fetch/Rebase/Push/RunRevListCount，全程 `-c core.hooksPath` 反钩子）。流程：检测未完成 rebase/merge → 拒；`gitio.Status` 脏树 → 拒（指向 doctor）；`store.WithWriteLock`（有界重试锁）内 fetch → behind 计数 → rebase → push，「远端持续移动」最多 3 轮；push「无可推送」与「被拒」区分（修掉了把 up-to-date 误判为被拒的 bug）；冲突不自动解决，rebase 留在原地、错误给出 `--continue`/`--abort` 指引；收敛后重建索引 |
| gardener 使用信号 | ✅ | 新包 `internal/usagelog`（`.index/entry-usage.jsonl`，1 MiB 轮转、容错读取）；MCP 三读工具记录**实际返回**的条目（search/brief/get，每会话每工具每条目去重，失败不影响读）；`gardener data` 新增 `entry_usage`：active/draft 条目的 `returns`/`last_return` 与 `cold`（零返回且 90+ 天未动）；第七原语 `deprecate_entry`（active/draft → deprecated，G-ENTRY-1/2 规则，正文逐字保留、可回滚）。计数只在一次性 `.index/` 日志，绝不进记忆文件（测试断言） |
| 写入自动重试 | ✅ | `acquireWriteLockRetried`：首拿失败后进入一个 20 秒竞争窗口、每 100ms 重试，拿到即写；窗口耗尽仍持有才报 `ErrWriteLocked`（MCP 层 `WRITE_LOCKED`，提示语更新）。三条写路径（add/supersede/gardener apply）统一换用。**与 DESIGN 原文「重试一次」的偏差**：字面的单次重试在 >2 方竞争下必然丢写者（所有等待者在释放瞬间抢锁，只有一个赢），测试实测后按「一个有界窗口」收口，DESIGN 完成记录已注明 |

## 关键测试

- 并发与重试：`TestConcurrentAddsLoseNothing` 改为**每写者只调一次**（8×3），全部落地；`TestAddEntryWaitsOutTransientHolder`（300ms 短暂持有后成功）、`TestAddEntryBlockedByFreshLock`（缩短窗口后仍以 `WRITE_LOCKED` 失败）。
- 脱敏：`TestScanRedactsHomePaths`（8 形态）、`TestScanRedactsMachineHostnames`（6 形态）、`TestScanKeepsQualifiedAndRelativeText`（8 条误报钉死）、`TestScanCountsStateAndCredentialsSeparately`（重叠形态各计一次）。
- 用量：`usagelog` 4 测（记录/聚合/轮转/容错）、`TestUsageLogRecordsReturns`（MCP 端到端、会话去重）、`TestDataIncludesEntryUsage`（cold 判定、计数不落记忆文件）、`TestGardenerDeprecateEntry`（dry-run → apply → 状态翻转 → 正文不变 → 单提交）。
- sync：`TestSyncTwoClonesConverge`（双克隆并发编辑收敛到同一 HEAD、两侧各 2 条、工作区干净）、`TestSyncConflictSurfacesGitConflict`（冲突后 rebase 留存、重试拒绝、`--abort` 后树干净）、`TestSyncRefusesDirtyTree`、`TestSyncRequiresUpstream`。
- 捕获：`capture` 5 测（提取/末条优先/跳过 tool 部分/降级/大文件头尾）、`cmd` 端到端 2 测（stdin 载荷 + 脱敏、旗标优先 + 元数据降级）、clients 安装幂等/外部 hooks 保留/卸载干净 4 测。

## 本机落地（真实环境）

- `bin/memoryd.exe` 重建后，ZCode 与 OpenCode 的捕获 hooks 已对 `E:\MemoryTreeData` 安装（ZCode config.json 写前自动备份 `.memoryd-bak`；MCP 条目完好）。
- 模拟 Stop 载荷端到端验证：transcript 摘要提取成功，`host: LAPTOP-TEST123` → `[REDACTED]`，inbox 格式正确。
- 排障记录：首两轮模拟「stdin 未达」为假象——Git Bash/cmd 转义后的 JSON 本身非法（`printf` 吃掉双反斜杠产生 `\M` 非法转义），`json.Unmarshal` 错误被静默吞掉所致；修复为坏载荷打 stderr 警告（仍不失败会话），真管道 stdin 经探针验证正常。

## 开发中发现并修复的问题

1. **`Plan` 与文档承诺不符**——注释称「有 blocked 操作则 Steps 为空」，实际 blocked 之前成功操作的步骤会留在 Steps 里（store 侧因先检查 OK 而未受影响）。修复为全部通过才输出 Steps，测试钉死。
2. **sync 把「无可推送」误判为「被拒」**——fast-forward 收敛后的 up-to-date push 触发重试循环空转、3 轮后误报失败。push 返回值拆为 published/rejected 两义。

## 遗留

- 捕获 hooks 的**真实触发**验证：ZCode 侧已安装，待下一次会话自然结束确认落盘；OpenCode 插件在 `session.idle` 载荷字段上仅做了宽松兼容（`sessionID`/`session` 两名都取），首次真实触发后如有字段偏差再补。Qoder/WorkBuddy/DeepSeek 无已验证机制，维持手工路径。
- 数据仓尚未配置 git remote；`memoryd sync` 首次使用前需 `git -C E:\MemoryTreeData push -u <remote> HEAD` 设好上游。
- 冷条目闭环的真实提案：`entry_usage` 日志刚启用，需积累一段使用数据后由 gardener 出首个 `deprecate_entry` 提案。
