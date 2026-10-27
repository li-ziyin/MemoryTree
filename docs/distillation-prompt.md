# 采集与蒸馏流程（inbox → memory_add）

原始采集与结构化记忆分离：`memoryd inbox` 只做无门槛落盘，蒸馏由 agent 离线完成，写入必须走 `memory_add`（校验、原子写、git、索引一步到位）。

## 采集（随时、零校验）

```sh
echo "会话要点……" | ./bin/memoryd inbox ~/.memorytree --source zcode
./bin/memoryd inbox ~/.memorytree --file notes.md --source qoder
./bin/memoryd inbox ~/.memorytree --list
```

采集落在 `<data>/.inbox/`（已 gitignore，不进索引、不进校验），文件名含 UTC 时间戳与随机尾。带会话 id 的采集（如 ZCode/OpenCode 的会话结束钩子）按 (source, session) 去重：同一会话反复触发钩子只覆盖同一份文件，蒸馏时不会重复。

## 蒸馏提示词（交给 agent，逐条处理 .inbox/ 文件）

> 你在读 MemoryTree 数据仓的原始采集（.inbox/ 目录，格式：YAML 头 captured_at/source + 可选 session + 正文）。逐个文件处理：
>
> 1. 判断该采集是否值得成为一条记忆：可复用的事实、偏好、坑、项目状态才值得；纯过程性内容丢弃。
> 2. 用 `memory_search` 查同一主题是否已有条目（用 2–3 个关键词，含一个中文双字词）。
>    - 无：为它调 `memory_add`，写语义摘要（不要截断正文），正文保留可操作细节。
>    - 有且已过时：调 `memory_add` 并带 `supersedes: [旧条目ID]`。
>    - 有且仍准确：什么都不做。
> 3. 选择条目类型：user（用户画像/偏好）、feedback（用户对工作方式的纠正）、project（项目状态）、reference（工具/环境事实）。挂载节点用 `memory_tree` 找 slug 路径，默认 `global`。
> 4. 处理完成后把 .inbox/ 里对应文件删除，只留下无法判断的内容等待人工。
>
> 硬规则：不要凭空补细节；正文与摘要只来自采集原文；不确定就跳过并说明。

## 处理记录

蒸馏完成后 `memoryd bench ~/.memorytree bench-questions.jsonl` 确认检索质量未回退。
