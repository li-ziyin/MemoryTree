# M2 写路径验收（2026-10-27）

代码：M2 系列提交（`213d5e8` 写核心 → `34e41ff` inbox），数据仓 `E:\MemoryTreeData`。

## DESIGN M2 验收逐条

| 验收项 | 结果 | 证据 |
|---|---|---|
| 并发写入无丢失 | ✅ | `TestConcurrentAddsLoseNothing`：8 goroutine × 3 条并发 Add（ErrWriteLocked 重试），24/24 落盘、路径唯一、仓校验全绿。开发中借此抓到真实 bug：碰撞后缀曾用 UUIDv7 时间戳头（同一分钟内恒同），同标题条目互相覆盖 |
| 崩溃后可恢复 | ✅ | 遗留 `*.tmp-*` 被忽略并顺带清理（`TestAddEntryCleansCrashTempFiles`）；崩溃遗留写锁 10 分钟后被回收（`TestAddEntryStealsStaleLock`）；锁外 Windows 共享冲突按竞争重试 |
| 被取代条目默认检索不返回 | ✅ | store 级 `TestAddEntrySupersedesRewritesOld` + MCP 级 `TestMemoryAddSupersedesAndSearchExcludes`；显式 `statuses=["superseded"]` 仍可取回 |
| 真实仓补 3 条记忆 | ✅ | m0-status / m1-index / m1-mcp 经 `memoryd add` 入库（source.kind=import，provenance 指向原文件），40 条 0 错 0 警；数据仓自动产生 3 个 memoryd 提交 |
| 检索质量不回退 | ✅ | `memoryd bench` 21/21 首条命中（原 20 问 + 新增 1 问），总 token 5,298（20 问口径 5,102 vs 基线 4,907，+4%，在 +10% 规则内） |

## 开发中发现并修复的问题

1. **文件名碰撞后缀用 UUIDv7 前 8 位**——那是毫秒时间戳的高位，同一分钟内写入的同标题条目共享后缀，改名互相覆盖造成静默丢失。改为 UUID 随机尾部 + taken 集合计数器兜底（`d101495`）。
2. **source.kind 未按规范枚举校验**——非法 kind 的条目被写入且 AddEntry 报成功。补上 conversation/document/manual/import 枚举（`2c91c1d`）。
3. **supersede 链未扁平化**——格式 v1 只允许当前头持有 supersedes 列表（V-SUP-4），第三版接手时须继承全部前身并清空旧列表、重指 `superseded_by`。CLI 链式流程触发，修复于 `2c91c1d`。
4. **先提交后校验的顺序风险**——写入后先跑校验再 git 提交，坏写入不再进入 git 历史。

## 契约摘要

- 写路径硬依赖 git（缺失即拒写、零写入，`GIT_UNAVAILABLE` / CLI 退出 2）；提交用 pathspec 只含本次文件。
- 单写者锁 `.index/write.lock`（O_EXCL；>10 分钟视为崩溃遗留回收）；竞争者快速失败可重试。
- 失败契约：预检失败零写入；文件写入后提交失败 → 文件保留、索引已刷新、错误信息含手工恢复命令。
- MCP 写工具带 V- 规则码的 ToolError；写后同会话立即可检索（envelope 报告新 generation）。

## 遗留

- `-race` 仍无法在 Windows 上跑（无 cgo/gcc）；CI 三平台矩阵上线后由 ubuntu 覆盖。
- 锁文件无 PID（跨平台最小实现），陈旧判定依赖 10 分钟窗口；写入频率极高时可调短。
