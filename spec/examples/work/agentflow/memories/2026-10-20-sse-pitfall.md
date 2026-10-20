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
