# M1.5 工程加固实施计划（Engineering Hardening）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 M2 写路径动工之前，补齐 2026-10-26 工程评审发现的六类缺口：质量门（CI）、解析层直接测试与 fuzz、规模数据、可复跑的检索回归、中文短查询的 FTS 路径、MCP 服务运行时卫生。

**Architecture:** 全部改动落在现有 `internal/` 包与 `cmd/memoryd`，不引入新的 Go 运行时依赖；CI 的 schema 一致性检查是唯一的 Node 侧脚本（仅 CI 使用）。索引 schema 从 v1 升到 v2（新增 rune 拆分 FTS 表），沿用既有的 `--rebuild` 显式迁移机制。

**Tech Stack:** Go 1.25、modernc.org/sqlite（纯 Go，禁 cgo）、FTS5（trigram + unicode61）、GitHub Actions（windows/ubuntu/macos 矩阵）、bash（Git Bash 本地与 CI 同构）。

**Spec:** [DESIGN.md](../../../DESIGN.md)、[spec/memory-format-v1.md](../../../spec/memory-format-v1.md)、[docs/m1-acceptance.md](../../m1-acceptance.md)（检索质量基线），以及 2026-10-26 会话中的工程评审结论（六类缺口）。

## Global Constraints

- Go 1.25.0；`go.mod` 不新增任何 require（modernc.org/sqlite 已含 FTS5，unicode61 是 FTS5 内置分词器）。
- 不使用 cgo；二进制保持交叉可编译。
- 提交信息用英文 conventional commit（`feat:`/`fix:`/`test:`/`chore:`/`ci:`/`docs:`），正文说明动机，末尾 DCO：`Signed-off-by: li-ziyin <duner779@163.com>`。
- CLI 退出码契约：0 成功，1 校验错误或 bench 未全首条命中，2 用法或 I/O 错误。
- 索引 schema 变更必须递增 `schemaVersion` 并保留「不兼容索引要求显式 `--rebuild`」行为。
- 文档语言约定：README 英文，DESIGN.md / 本计划中文。
- 本地开发环境是 Windows + Git Bash；所有脚本必须同时跑在 Git Bash 与 Linux CI 上（`bash scripts/...`，注意路径用 `/`）。
- 用户数据仓 `E:\MemoryTreeData` 独立于源码仓；往它写入的只有 `.index/`（gitignore）与 Task 8 的 `bench-questions.jsonl`。
- 超出范围（明确不做）：模块路径更换、增量索引、goreleaser、M2 写路径、mimosa 重扫。

**任务依赖：** Task 1–8 相互独立（建议按编号顺序做）。**Task 9 依赖 Task 7 和 Task 8**（需要前后规模数据与真实仓 bench 基线做采用/回退决策）。Task 10 独立。

---

### Task 1: 本地质量门脚本与版本注入

**Files:**
- Create: `scripts/check.sh`
- Modify: `README.md`（Quick start 增加一行）
- Modify: `CONTRIBUTING.md`（开发流程一节）

**Interfaces:**
- Consumes: `cmd/memoryd/main.go:19-23` 既有的 `version/commit/date` ldflags 变量。
- Produces: `bash scripts/check.sh` 作为本地与 CI 共用的唯一质量门；后续所有任务以它为验证命令。

- [ ] **Step 1: 写 scripts/check.sh**

```bash
#!/usr/bin/env bash
# Single quality gate: identical checks locally (Git Bash) and in CI.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== gofmt =="
unformatted=$(gofmt -l cmd internal)
if [ -n "$unformatted" ]; then
  echo "gofmt needed on: $unformatted" >&2
  exit 1
fi

echo "== go vet =="
go vet ./...

echo "== go test =="
go test ./...

echo "== build with version stamping =="
version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
commit=$(git rev-parse --short HEAD 2>/dev/null || echo none)
built=$(date -u +%Y-%m-%dT%H:%M:%SZ)
go build -trimpath \
  -ldflags "-X main.version=$version -X main.commit=$commit -X main.date=$built" \
  -o bin/memoryd-check ./cmd/memoryd
./bin/memoryd-check version
rm -f bin/memoryd-check
```

- [ ] **Step 2: 本地跑通**

Run: `bash scripts/check.sh`
Expected: 四段全部通过，`memoryd-check version` 打印类似 `memoryd dev (commit 33ecc1c, built 2026-10-26T...)` 而非 `dev/none/unknown`。

- [ ] **Step 3: README 与 CONTRIBUTING 各加一句**

README Quick start 代码块后追加：

```sh
# Run the full quality gate used by CI (gofmt, vet, tests, stamped build)
bash scripts/check.sh
```

CONTRIBUTING.md 开发小节加一行：`Run bash scripts/check.sh before committing; CI runs the same script.`

- [ ] **Step 4: 再跑一次门禁并提交**

Run: `bash scripts/check.sh` → 通过。
Commit:

```bash
git add scripts/check.sh README.md CONTRIBUTING.md
git commit -s -m "chore: add local quality gate script with version stamping" -m "One bash script runs gofmt, vet, tests, and a stamped build so local checks and CI stay identical; memoryd version now reports real commit and date when built through it."
```

---

### Task 2: CI 工作流与 JSON Schema 一致性检查

**Files:**
- Create: `.github/workflows/ci.yml`
- Create: `scripts/schema-check.mjs`

**Interfaces:**
- Consumes: Task 1 的 `scripts/check.sh`；`spec/schema/{repo,node,entry}.schema.json`（draft 2020-12）；`spec/examples/` 下的 `memory.yaml`、`node.md`、`memories/*.md`。
- Produces: push/PR 时三平台矩阵测试 + schema 与示例的一致性门（仓库推上 GitHub 前文件就绪，推送后自动生效）。

- [ ] **Step 1: 写 workflow**

`.github/workflows/ci.yml`：

```yaml
name: CI
on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, windows-latest, macos-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.25"
      - run: bash scripts/check.sh
        shell: bash

  schema:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: "20"
      - run: npm install --no-save ajv js-yaml
      - run: node scripts/schema-check.mjs
```

- [ ] **Step 2: 写 schema-check.mjs**

```js
// Validates every spec/examples frontmatter against spec/schema/*.json,
// so the JSON Schemas and the Go validator cannot silently drift apart.
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { parse } from "js-yaml";
import Ajv2020 from "ajv/dist/2020.js";

const root = fileURLToPath(new URL("../spec", import.meta.url));
const ajv = new Ajv2020({ strict: false });

function loadSchema(name) {
  const schema = JSON.parse(readFileSync(join(root, "schema", name), "utf8"));
  return ajv.compile(schema);
}

function frontmatter(path) {
  const text = readFileSync(path, "utf8");
  const lines = text.split("\n");
  if (lines[0].trim() !== "---") throw new Error(`${path}: no frontmatter`);
  const end = lines.indexOf("---", 1);
  if (end < 0) throw new Error(`${path}: unterminated frontmatter`);
  return parse(lines.slice(1, end).join("\n"));
}

let checked = 0;
const nodeValidate = loadSchema("node.schema.json");
const entryValidate = loadSchema("entry.schema.json");
const repoValidate = loadSchema("repo.schema.json");

repoValidate(parse(readFileSync(join(root, "examples", "memory.yaml"), "utf8")));
checked += 1;

function walk(dir, validate, kind) {
  for (const name of readdirSync(dir)) {
    if (name === "memories") {
      walk(join(dir, name), entryValidate, "entry");
      continue;
    }
    if (name === "node.md") {
      const ok = validate(frontmatter(join(dir, name)));
      if (!ok) throw new Error(`${dir}/node.md: ${JSON.stringify(validate.errors)}`);
      checked += 1;
      continue;
    }
    let stat;
    try { stat = readdirSync(join(dir, name), { withFileTypes: true }); } catch { continue; }
    if (Array.isArray(stat)) walk(join(dir, name), validate, kind);
  }
}
walk(join(root, "examples"), nodeValidate, "node");
console.log(`schema-check: ${checked} documents valid`);
```

- [ ] **Step 3: 本地验证 YAML 语法与脚本（node 可用时）**

Run: `go run - <<'EOF'
package main
import ("fmt";"os";"gopkg.in/yaml.v3")
func main() {
	data, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil { panic(err) }
	var v any
	if err := yaml.Unmarshal(data, &v); err != nil { panic(err) }
	fmt.Println("ci.yml is valid YAML")
}
EOF`
Expected: `ci.yml is valid YAML`。
若本机有 node：`npm install --no-save ajv js-yaml && node scripts/schema-check.mjs` → `schema-check: N documents valid`。没有 node 则跳过，CI 首跑时验证。

- [ ] **Step 4: 提交**

```bash
git add .github/workflows/ci.yml scripts/schema-check.mjs
git commit -s -m "ci: add cross-platform workflow and schema consistency job" -m "Runs the shared quality gate on windows, ubuntu, and macos, and validates every spec/examples frontmatter against the JSON Schemas with ajv so the schemas and the Go validator cannot drift silently. Jobs activate once the repository is pushed to GitHub."
```

---

### Task 3: format 包直接单测（YAML 安全规则 + 文件级规则）

**Files:**
- Test: `internal/format/format_test.go`（新文件，包 `format`）

**Interfaces:**
- Consumes: `format.Split`、`format.ReadFile`、`format.Decode`（`internal/format/read.go`、`internal/format/yaml.go`）与既有哨兵错误 `ErrBOM/ErrNotUTF8/ErrNoFrontmat/ErrNoClosing/ErrMetaTooBig/ErrBodyTooBig/ErrTooLarge/ErrNotRegular/ErrSymlink/ErrYAML/ErrYAMLUnsafe/ErrNotMapping/ErrEmptyMeta/ErrMultiDoc`。
- Produces: 无新接口；`checkSafe` 的每个拒绝分支获得直接覆盖。

- [ ] **Step 1: 写测试（对既有实现的行为锁定，写完应直接通过）**

```go
package format

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSplit(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr error
	}{
		{"valid", "---\nid: x\n---\nbody\n", nil},
		{"crlf", "---\r\nid: 1\r\n---\r\n正文", nil},
		{"no first newline", "---", ErrNoFrontmat},
		{"no delimiter", "id: x\n", ErrNoFrontmat},
		{"unterminated", "---\nid: 1\n", ErrNoClosing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Split([]byte(tc.in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestSplitRejectsBOMAndInvalidUTF8(t *testing.T) {
	if _, err := Split([]byte("\xEF\xBB\xBF---\nid: 1\n---\nx")); !errors.Is(err, ErrBOM) {
		t.Fatalf("err = %v, want ErrBOM", err)
	}
	if _, err := Split([]byte("---\nid: \xFF\n---\nx")); !errors.Is(err, ErrNotUTF8) {
		t.Fatalf("err = %v, want ErrNotUTF8", err)
	}
}

func TestSplitSizeLimits(t *testing.T) {
	metaOK := "id: " + strings.Repeat("a", 8) + "\n"
	big := "---\n" + strings.Repeat("m", MaxFrontmatter+1) + "\n---\nx"
	if _, err := Split([]byte(big)); !errors.Is(err, ErrMetaTooBig) {
		t.Fatalf("err = %v, want ErrMetaTooBig", err)
	}
	ok := "---\n" + metaOK + "---\n" + strings.Repeat("b", MaxBody)
	if doc, err := Split([]byte(ok)); err != nil || len(doc.Body) != MaxBody {
		t.Fatalf("doc=%v err=%v", doc, err)
	}
	over := "---\n" + metaOK + "---\n" + strings.Repeat("b", MaxBody+1)
	if _, err := Split([]byte(over)); !errors.Is(err, ErrBodyTooBig) {
		t.Fatalf("err = %v, want ErrBodyTooBig", err)
	}
}

func TestSplitIsIdempotentOnRaw(t *testing.T) {
	data := []byte("---\r\nid: 1\r\n---\r\n正文\r\n")
	doc, err := Split(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Split(doc.Raw)
	if err != nil || string(again.Meta) != string(doc.Meta) || string(again.Body) != string(doc.Body) {
		t.Fatalf("Split(Raw) differs: %+v %v", again, err)
	}
}

func TestReadFileRejectsDirectory(t *testing.T) {
	if _, err := ReadFile(t.TempDir()); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("err = %v, want ErrNotRegular", err)
	}
}

func TestReadFileRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Symlink needs privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real.md")
	if err := os.WriteFile(target, []byte("---\nid: 1\n---\nx"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(link); !errors.Is(err, ErrSymlink) {
		t.Fatalf("err = %v, want ErrSymlink", err)
	}
}

func TestDecode(t *testing.T) {
	var out map[string]any
	if err := Decode([]byte("id: x\ncount: 3\n"), &out); err != nil {
		t.Fatalf("valid mapping rejected: %v", err)
	}
	if out["id"] != "x" {
		t.Fatalf("decoded %v", out)
	}
}

func TestDecodeSafetyRules(t *testing.T) {
	cases := []struct {
		name string
		meta string
		want error
	}{
		{"empty", " \n", ErrEmptyMeta},
		{"multi doc", "a: 1\n---\nb: 2\n", ErrMultiDoc},
		{"sequence root", "- a\n- b\n", ErrNotMapping},
		{"scalar root", "just text\n", ErrNotMapping},
		{"alias", "a: &x 1\nb: *x\n", ErrYAMLUnsafe},
		{"anchor", "a: &x 1\n", ErrYAMLUnsafe},
		{"custom tag", "a: !swap 1\n", ErrYAMLUnsafe},
		{"duplicate key", "a: 1\na: 2\n", ErrYAML},
		{"complex key", "? [1, 2]\n: v\n", ErrYAMLUnsafe},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out map[string]any
			err := Decode([]byte(tc.meta), &out)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试验证通过（如有失败，是发现了真实缺陷，修实现而不是改测试，并在提交正文记录）**

Run: `go test ./internal/format/ -v`
Expected: 全部 PASS。

- [ ] **Step 3: 提交**

```bash
git add internal/format/format_test.go
git commit -s -m "test: cover format package file and YAML safety rules directly" -m "The YAML safety rules are the trust boundary for untrusted frontmatter; every checkSafe rejection branch, the size limits, BOM and UTF-8 handling, and Split idempotence now have direct tests instead of indirect coverage through the validator."
```

---

### Task 4: Fuzz 种子（frontmatter 与 JSON-RPC 解析）

**Files:**
- Test: `internal/format/fuzz_test.go`（包 `format`）
- Test: `internal/mcpserver/fuzz_test.go`（包 `mcpserver`）

**Interfaces:**
- Consumes: `format.Split`、`format.Decode`；`mcpserver.New`、`Server.Serve`、既有助手 `newTestServer(t)`（`internal/mcpserver/server_test.go:24`）。
- Produces: 三个 fuzz 目标，`go test` 默认即跑种子语料（CI 兼容）；深挖用 `go test -fuzz`。

- [ ] **Step 1: 写 format fuzz**

`internal/format/fuzz_test.go`：

```go
package format

import (
	"bytes"
	"testing"
	"unicode/utf8"
)

func FuzzSplit(f *testing.F) {
	seeds := [][]byte{
		[]byte("---\nid: x\n---\nbody\n"),
		[]byte("---\r\nid: 1\r\n---\r\n正文"),
		[]byte("\xEF\xBB\xBF---\nid: 1\n---\nx"),
		[]byte("---\nunclosed"),
		[]byte("---\n" + string(bytes.Repeat([]byte("m"), MaxFrontmatter+1)) + "\n---\nx"),
		[]byte("网络 端口 \xF0\x9F\x8C\xB2"),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := Split(data)
		if err != nil {
			return
		}
		if !utf8.Valid(data) {
			t.Fatal("Split accepted invalid UTF-8")
		}
		if len(doc.Meta) > MaxFrontmatter || len(doc.Body) > MaxBody {
			t.Fatalf("limits violated: meta=%d body=%d", len(doc.Meta), len(doc.Body))
		}
		again, err := Split(doc.Raw)
		if err != nil || !bytes.Equal(again.Meta, doc.Meta) || !bytes.Equal(again.Body, doc.Body) {
			t.Fatal("Split is not idempotent on Raw")
		}
	})
}

func FuzzDecode(f *testing.F) {
	f.Add([]byte("id: 1\ntags: [a, b]\n"))
	f.Add([]byte("a: &x 1\nb: *x"))
	f.Add([]byte("网络: 端口\n"))
	f.Add([]byte("---\n"))
	f.Fuzz(func(t *testing.T, meta []byte) {
		var first map[string]any
		err1 := Decode(meta, &first)
		var second map[string]any
		err2 := Decode(meta, &second)
		if (err1 == nil) != (err2 == nil) {
			t.Fatalf("Decode not deterministic: %v vs %v", err1, err2)
		}
	})
}
```

- [ ] **Step 2: 本地短跑 fuzz（各 15 秒，记录是否有崩溃）**

Run: `go test ./internal/format/ -fuzz FuzzSplit -fuzztime 15s` 然后 `go test ./internal/format/ -fuzz FuzzDecode -fuzztime 15s`
Expected: 无崩溃（crash 会写入 `testdata/fuzz/` 并使命令失败；若出现，修实现后把语料留在 testdata 一并提交）。

- [ ] **Step 3: 写 mcpserver fuzz**

`internal/mcpserver/fuzz_test.go`：

```go
package mcpserver

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func FuzzServeLine(f *testing.F) {
	f.Add(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	f.Add(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_search","arguments":{"query":"网络"}}}`)
	f.Add(`[1,2,3]`)
	f.Add(`{"jsonrpc":"2.0"`)
	f.Add("\xEF\xBB\xBF{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}")
	f.Add(`{"jsonrpc":"1.0","id":7,"method":"ping"}`)
	// One server for all iterations: read tools are stateless, and rebuilding
	// an index per iteration would starve the fuzzer.
	srv := newTestServer(f)
	f.Fuzz(func(t *testing.T, line string) {
		var out bytes.Buffer
		_ = srv.Serve(strings.NewReader(line+"\n"), &out)
		for _, l := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
			if len(l) == 0 {
				continue
			}
			if !json.Valid(l) {
				t.Fatalf("non-JSON response line: %q", l)
			}
		}
	})
}
```

注意：`newTestServer(f)` 接受 `testing.TB`（其签名是 `func newTestServer(t *testing.T) *Server`，如只收 `*testing.T`，在 fuzz 里用 `f.T()` 先取 `*testing.T`：`srv := newTestServer(f.T())`）。写完按编译器提示选择其中一种写法。

- [ ] **Step 4: 跑 mcpserver fuzz 与全套测试**

Run: `go test ./internal/mcpserver/ -fuzz FuzzServeLine -fuzztime 15s` 然后 `bash scripts/check.sh`
Expected: 无崩溃；全套通过。

- [ ] **Step 5: 提交**

```bash
git add internal/format/fuzz_test.go internal/mcpserver/fuzz_test.go
git commit -s -m "test: add fuzz seeds for frontmatter and JSON-RPC parsing" -m "Both parsers take untrusted input: memory files come from any agent, the stdio transport from any client. Seed corpora run in plain go test; -fuzz digs deeper. Properties: Split never accepts invalid UTF-8, honors limits, and is idempotent; Decode is deterministic; every Serve response line is valid JSON."
```

---

### Task 5: docfile 往返稳定性测试

**Files:**
- Test: `internal/docfile/docfile_test.go`（新文件，包 `docfile`）

**Interfaces:**
- Consumes: `docfile.Marshal`、`docfile.MarshalFrontmatter`、`docfile.WriteFrontmatter`；`format.Split`、`format.Decode` 做往返验证。
- Produces: M2 写路径将依赖的字节级稳定性承诺（同输入 → 同输出字节）。

- [ ] **Step 1: 写测试**

```go
package docfile

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/li-ziyin/memorytree/internal/format"
)

type meta struct {
	ID      string   `yaml:"id"`
	Title   string   `yaml:"title"`
	Tags    []string `yaml:"tags_delta"`
	Private bool     `yaml:"private"`
}

var sample = meta{ID: "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b05", Title: "标题 docker", Tags: []string{"a", "b"}, Private: true}

func TestMarshalIsStable(t *testing.T) {
	first, err := Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("Marshal not stable:\n%q\n%q", first, second)
	}
}

func TestFrontmatterRoundTrip(t *testing.T) {
	data, err := MarshalFrontmatter(sample, []byte("正文\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := format.Split(data)
	if err != nil {
		t.Fatalf("output is not a valid frontmatter file: %v", err)
	}
	var back meta
	if err := format.Decode(doc.Meta, &back); err != nil {
		t.Fatalf("output fails the format safety rules: %v", err)
	}
	if back != sample {
		t.Fatalf("round trip changed %v", back)
	}
	if string(doc.Body) != "正文\n" {
		t.Fatalf("body = %q", doc.Body)
	}
}

func TestWriteFrontmatterPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are ACL-driven on Windows")
	}
	path := filepath.Join(t.TempDir(), "entry.md")
	if err := WriteFrontmatter(path, sample, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %o, want 600", info.Mode().Perm())
	}
}
```

- [ ] **Step 2: 跑测试**

Run: `go test ./internal/docfile/ -v`
Expected: 全部 PASS（`TestWriteFrontmatterPermissions` 在 Windows 上 SKIP）。

- [ ] **Step 3: 提交**

```bash
git add internal/docfile/docfile_test.go
git commit -s -m "test: cover docfile YAML round-trip stability" -m "docfile becomes the M2 write path's file writer; pin down that its output is byte-stable, parses back through the format safety rules unchanged, and is written owner-only where permission bits apply."
```

---

### Task 6: CLI 退出码契约测试

**Files:**
- Test: `cmd/memoryd/main_test.go`（追加两个测试函数）

**Interfaces:**
- Consumes: 既有 `run(args, stdin, stdout, stderr) int`（`cmd/memoryd/main.go:29`）；README 声明的退出码契约（0/1/2）。
- Produces: 无新接口；CLI 契约锁定，供 Task 8 的 `bench` 复用同一模式。

- [ ] **Step 1: 追加测试**

```go
func TestRunUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"no args", nil},
		{"unknown command", []string{"frobnicate"}},
		{"init missing dir", []string{"init"}},
		{"validate missing dir", []string{"validate"}},
		{"import-legacy missing flags", []string{"import-legacy"}},
		{"index missing dir", []string{"index"}},
		{"mcp missing dir", []string{"mcp"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tc.args, strings.NewReader(""), &stdout, &stderr); code != 2 {
				t.Fatalf("code = %d, want 2; stderr: %s", code, stderr.String())
			}
		})
	}
}

func TestRunValidateAndIndexExitCodes(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	if code := run([]string{"init", dir}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init code = %d, stderr: %s", code, stderr.String())
	}
	if code := run([]string{"validate", dir}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("validate code = %d, stderr: %s", code, stderr.String())
	}
	stdout.Reset()
	if code := run([]string{"index", "--json", dir}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("index code = %d, stderr: %s", code, stderr.String())
	}
	if !json.Valid(bytes.TrimSpace(stdout.Bytes())) {
		t.Fatalf("index --json emitted invalid JSON: %s", stdout.String())
	}

	// Corrupt one managed file: validation errors must exit 1, index must
	// refuse and leave the previous index untouched.
	nodePath := filepath.Join(dir, "global", "node.md")
	if err := os.WriteFile(nodePath, []byte("not frontmatter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"validate", dir}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("corrupt validate code = %d, want 1", code)
	}
	if code := run([]string{"index", dir}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("corrupt index code = %d, want 1", code)
	}
}
```

（文件头如缺 `encoding/json`、`os`、`path/filepath` import 则补上。）

- [ ] **Step 2: 跑测试**

Run: `go test ./cmd/memoryd/ -run TestRun -v`
Expected: 全部 PASS。

- [ ] **Step 3: 提交**

```bash
git add cmd/memoryd/main_test.go
git commit -s -m "test: cover CLI exit-code contract" -m "The 0/1/2 exit codes are the scripted-client API; every subcommand's usage failure, a clean init-validate-index round, JSON output validity, and the invalid-repository path are now pinned."
```

---

### Task 7: 规模夹具生成器与索引基准

**Files:**
- Create: `internal/scalegen/scalegen.go`
- Test: `internal/scalegen/bench_test.go`（包 `scalegen_test`）
- Create: `docs/perf-baseline.md`

**Interfaces:**
- Consumes: `initrepo.Init(initrepo.Options{Root, Title})`、`format.ReadFile`、`format.Decode`、`ids.NewUUIDv7()`、`docfile.WriteFrontmatter`、`index.Build`、`index.Open`、`index.Search`。
- Produces: `scalegen.Generate(root string, n int) error`——生成 n 条合法条目的数据仓（Task 8 的 CLI 测试复用）；`docs/perf-baseline.md` 中的基线数字（Task 9 的决策依据）。

- [ ] **Step 1: 写生成器（先让它能被测试调用）**

`internal/scalegen/scalegen.go`：

```go
// Package scalegen generates valid synthetic data repositories for
// benchmarks and scale tests. Bodies mix Chinese and ASCII so both the
// trigram and rune-split tokenizers get realistic input.
package scalegen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/li-ziyin/memorytree/internal/docfile"
	"github.com/li-ziyin/memorytree/internal/format"
	"github.com/li-ziyin/memorytree/internal/ids"
	"github.com/li-ziyin/memorytree/internal/initrepo"
)

const bodyLine = "排障记录：网关返回 502 时先查 docker compose 日志，再确认端口映射与重试策略；" +
	"the gateway retries with exponential backoff and mirrors sessions.\n"

type entryMeta struct {
	Schema      string `yaml:"schema"`
	ID          string `yaml:"id"`
	Node        string `yaml:"node"`
	Type        string `yaml:"type"`
	Status      string `yaml:"status"`
	Sensitivity string `yaml:"sensitivity"`
	Title       string `yaml:"title"`
	Summary     string `yaml:"summary"`
	TagsDelta   []string `yaml:"tags_delta"`
	Source      struct {
		Agent      string `yaml:"agent"`
		Kind       string `yaml:"kind"`
		Session    string `yaml:"session"`
		CapturedAt string `yaml:"captured_at"`
	} `yaml:"source"`
	Supersedes   []string `yaml:"supersedes"`
	SupersededBy *string  `yaml:"superseded_by"`
	CreatedAt    string   `yaml:"created_at"`
	UpdatedAt    string   `yaml:"updated_at"`
}

// Generate initializes root and fills global/memories with n valid entries.
// Every hundredth entry carries the token needle-<i%10> in its title so
// benchmarks have known targets.
func Generate(root string, n int) error {
	if _, err := initrepo.Init(initrepo.Options{Root: root, Title: "Scale fixture"}); err != nil {
		return err
	}
	doc, err := format.ReadFile(filepath.Join(root, "global", "node.md"))
	if err != nil {
		return err
	}
	var node struct {
		ID string `yaml:"id"`
	}
	if err := format.Decode(doc.Meta, &node); err != nil {
		return err
	}
	memDir := filepath.Join(root, "global", "memories")
	if err := os.MkdirAll(memDir, 0o700); err != nil {
		return err
	}

	body := []byte(strings.Repeat(bodyLine, 16)) // ~2 KiB per entry
	for i := 0; i < n; i++ {
		title := fmt.Sprintf("规模条目 %d：docker 端口与网关排障 %d", i, i%50)
		if i%100 == 0 {
			title = fmt.Sprintf("金丝雀 needle-%d 规模条目 %d", i%10, i)
		}
		m := entryMeta{
			Schema:      "memorytree.entry/v1",
			ID:          ids.NewUUIDv7(),
			Node:        node.ID,
			Type:        "project",
			Status:      "active",
			Sensitivity: "internal",
			Title:       title,
			Summary:     fmt.Sprintf("第 %d 条合成条目：端口、网关、docker 排障记录。", i),
			TagsDelta:   []string{"scale", fmt.Sprintf("batch-%d", i%10)},
			Supersedes:   []string{},
			CreatedAt:    "2026-01-01T00:00:00Z",
			UpdatedAt:    "2026-01-01T00:00:00Z",
		}
		m.Source.Agent = "scalegen"
		m.Source.Kind = "manual"
		m.Source.Session = "scale"
		m.Source.CapturedAt = "2026-01-01T00:00:00Z"
		path := filepath.Join(memDir, fmt.Sprintf("2026-01-01-scale-%06d.md", i))
		if err := docfile.WriteFrontmatter(path, m, body); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 2: 先写一个合法性冒烟测试（生成 10 条 → Build 必须成功）**

追加到 `internal/scalegen/bench_test.go`：

```go
package scalegen_test

import (
	"testing"

	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/scalegen"
)

func TestGenerateProducesValidRepository(t *testing.T) {
	root := t.TempDir()
	if err := scalegen.Generate(root, 10); err != nil {
		t.Fatal(err)
	}
	res, err := index.Build(root, index.BuildOptions{})
	if err != nil {
		t.Fatalf("generated repository does not validate: %v", err)
	}
	if res.Entries != 10 {
		t.Fatalf("entries = %d, want 10", res.Entries)
	}
}
```

Run: `go test ./internal/scalegen/ -v`
Expected: PASS。若 validator 报某个必填字段缺失，按报错补进 `entryMeta`（模板已对齐 spec §8 示例）。

- [ ] **Step 3: 写基准**

同文件追加：

```go
import (
	"context"
	...
)

func benchRoot(b *testing.B, n int) string {
	b.Helper()
	root := b.TempDir()
	if err := scalegen.Generate(root, n); err != nil {
		b.Fatal(err)
	}
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		b.Fatal(err)
	}
	return root
}

func benchSizes() []struct {
	name string
	n    int
} {
	return []struct {
		name string
		n    int
	}{{"100", 100}, {"1000", 1000}, {"5000", 5000}}
}

// BenchmarkBuildRebuild measures a full index rebuild, the cost every source
// change pays today (incremental indexing is deferred to M2).
func BenchmarkBuildRebuild(b *testing.B) {
	for _, sz := range benchSizes() {
		b.Run(sz.name, func(b *testing.B) {
			root := benchRoot(b, sz.n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := index.Build(root, index.BuildOptions{Rebuild: true}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkStartupNoop measures validate + fingerprint on an unchanged
// repository: the fixed cost every memoryd mcp start pays.
func BenchmarkStartupNoop(b *testing.B) {
	root := benchRoot(b, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := index.Build(root, index.BuildOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearchFTS(b *testing.B) { // all tokens >= 3 runes: trigram path
	root := benchRoot(b, 1000)
	ix, err := index.Open(root)
	if err != nil {
		b.Fatal(err)
	}
	defer ix.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hits, err := ix.Search(context.Background(), index.SearchOptions{Query: "docker troubleshooting"})
		if err != nil {
			b.Fatal(err)
		}
		if len(hits) == 0 {
			b.Fatal("no hits")
		}
	}
}

func BenchmarkSearchShortCJK(b *testing.B) { // 2-rune tokens: short path
	root := benchRoot(b, 1000)
	ix, err := index.Open(root)
	if err != nil {
		b.Fatal(err)
	}
	defer ix.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hits, err := ix.Search(context.Background(), index.SearchOptions{Query: "端口 网关"})
		if err != nil {
			b.Fatal(err)
		}
		if len(hits) == 0 {
			b.Fatal("no hits")
		}
	}
}
```

Run: `go test ./internal/scalegen/ -bench . -run '^$' -benchtime 3x`
Expected: 五组基准全部出数，`BenchmarkSearchShortCJK` 有结果且不为 0 命中。

- [ ] **Step 4: 记录基线到 docs/perf-baseline.md**

```markdown
# 索引规模基线（perf baseline）

生成方法：`go test ./internal/scalegen/ -bench . -run '^$' -benchtime 3x`
数据：合成仓库（scalegen），每条约 2 KiB 中英混排正文。

| 基准 | 100 条 | 1000 条 | 5000 条 |
|---|---:|---:|---:|
| 全量重建 BuildRebuild | <填写> | <填写> | <填写> |
| 启动空转 StartupNoop | — | <填写> | — |
| 搜索 FTS（trigram） | — | <填写> | — |
| 搜索短词（Task 9 前为 LIKE 扫描） | — | <填写> | — |

- 记录日期：2026-10-__，机器：__，Go：1.25.0
- 结论（供 M2 决策）：重建耗时 __ 秒/千条 → 在 __ 条以内，每次源变更全量重建可接受。
```

跑完基准把真实数字填进表格（`<填写>` 全部替换）。

- [ ] **Step 5: 全量验证并提交**

Run: `bash scripts/check.sh` → 通过。
Commit:

```bash
git add internal/scalegen docs/perf-baseline.md
git commit -s -m "feat: add scale fixture generator and index benchmarks" -m "Generates valid synthetic repositories with mixed Chinese/ASCII bodies and measures full rebuilds, startup no-op cost, and both search paths at 100/1000/5000 entries. Turns the 'full rebuild is fine for now' assumption into recorded numbers in docs/perf-baseline.md ahead of the M2 write path."
```

---

### Task 8: token 估算包 + memoryd bench 检索回归工具

**Files:**
- Create: `internal/tokens/tokens.go`
- Test: `internal/tokens/tokens_test.go`
- Create: `internal/bench/bench.go`
- Test: `internal/bench/bench_test.go`
- Modify: `cmd/memoryd/main.go`（新增 `bench` 子命令与 usage 行）
- Test: `cmd/memoryd/main_test.go`（追加 bench 契约测试）
- Create: `docs/bench-questions.example.jsonl`
- Modify: `README.md`、`docs/README.md`

**Interfaces:**
- Consumes: `index.Open`、`index.Search`、`index.SearchOptions`、`index.Hit`；`envelope.go` 现有 `estimateTokens/estimateTextTokens`（迁出）。
- Produces: `tokens.Estimate(v any) int`、`tokens.EstimateText(s string) int`；`bench.Question{ID, Query, ExpectID, Limit}`、`bench.Result{ID, Query, Rank, Hits, Tokens, TopID}`、`bench.LoadQuestions(path string) ([]Question, error)`、`bench.Run(dataDir string, questions []Question) ([]Result, error)`；CLI `memoryd bench [--json] <data-dir> <questions.jsonl>`（退出 0 全首条命中 / 1 有 miss / 2 用法或 I/O）。

- [ ] **Step 1: 建 internal/tokens 并迁入估算函数（测试先行）**

`internal/tokens/tokens_test.go`：

```go
package tokens

import "testing"

func TestEstimateText(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},       // 0
		{"abcd", 1},   // 4 * 0.25
		{"abc", 1},    // 0.75 -> ceil
		{"中文", 2},     // 2 * 1
		{"ab中", 2},    // 0.5 + 1 = 1.5 -> ceil
	}
	for _, tc := range cases {
		if got := EstimateText(tc.in); got != tc.want {
			t.Errorf("EstimateText(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestEstimateMarshals(t *testing.T) {
	if got := Estimate(map[string]any{"k": "abcd"}); got < 4 {
		t.Fatalf("Estimate = %d, want >= 4 (json envelope)", got)
	}
}
```

Run: `go test ./internal/tokens/` → 编译失败（包不存在）。
`internal/tokens/tokens.go`（从 `internal/mcpserver/envelope.go:89-110` 原样迁移算法）：

```go
// Package tokens estimates response size in tokens: one quarter token per
// ASCII rune and one token per non-ASCII rune, deliberately conservative
// for Chinese.
package tokens

import (
	"encoding/json"
	"math"
)

// Estimate approximates the token count of v's JSON encoding.
func Estimate(v any) int {
	data, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return EstimateText(string(data))
}

// EstimateText approximates the token count of text.
func EstimateText(text string) int {
	var tokens float64
	for _, r := range text {
		if r < 0x80 {
			tokens += 0.25
		} else {
			tokens++
		}
	}
	return int(math.Ceil(tokens))
}
```

`internal/mcpserver/envelope.go`：删除 `estimateTokens` 与 `estimateTextTokens` 的函数体实现，改为薄委托（调用点不变）：

```go
func estimateTokens(v any) int  { return tokens.Estimate(v) }
func estimateTextTokens(s string) int { return tokens.EstimateText(s) }
```

并加 import `"github.com/li-ziyin/memorytree/internal/tokens"`。若 `estimateTextTokens` 无调用点则直接删除。
Run: `go test ./internal/tokens/ ./internal/mcpserver/` → PASS。

- [ ] **Step 2: 写 internal/bench**

`internal/bench/bench.go`：

```go
// Package bench reruns retrieval questions against a built index and
// reports first-hit rank, giving ranking changes a regression gate.
package bench

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/tokens"
)

// Question is one retrieval question with its expected gold entry.
type Question struct {
	ID       int    `json:"id"`
	Query    string `json:"query"`
	ExpectID string `json:"expect_id"`
	Limit    int    `json:"limit,omitempty"`
}

// Result is the outcome of one question.
type Result struct {
	ID     int    `json:"id"`
	Query  string `json:"query"`
	Rank   int    `json:"rank"` // 1-based; 0 means the gold entry was not returned
	Hits   int    `json:"hits"`
	Tokens int    `json:"tokens_estimated"`
	TopID  string `json:"top_id"`
}

// Run executes all questions and returns their results in order.
func Run(dataDir string, questions []Question) ([]Result, error) {
	ix, err := index.Open(dataDir)
	if err != nil {
		return nil, err
	}
	defer ix.Close()
	results := make([]Result, 0, len(questions))
	for _, q := range questions {
		limit := q.Limit
		if limit <= 0 {
			limit = 5
		}
		hits, err := ix.Search(context.Background(), index.SearchOptions{Query: q.Query, Limit: limit})
		if err != nil {
			return nil, fmt.Errorf("question %d: %w", q.ID, err)
		}
		rank := 0
		topID := ""
		for i, h := range hits {
			if i == 0 {
				topID = h.ID
			}
			if h.ID == q.ExpectID {
				rank = i + 1
				break
			}
		}
		results = append(results, Result{
			ID: q.ID, Query: q.Query, Rank: rank, Hits: len(hits),
			Tokens: tokens.Estimate(hits), TopID: topID,
		})
	}
	return results, nil
}

// LoadQuestions reads a JSONL file; blank lines and #-comments are skipped.
func LoadQuestions(path string) ([]Question, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Question
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		var q Question
		if err := json.Unmarshal([]byte(text), &q); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if q.Query == "" || q.ExpectID == "" {
			return nil, fmt.Errorf("%s:%d: question needs query and expect_id", path, line)
		}
		out = append(out, q)
	}
	return out, sc.Err()
}
```

`internal/bench/bench_test.go`：

```go
package bench_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/li-ziyin/memorytree/internal/bench"
	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/scalegen"
)

func goldEntryID(t *testing.T, root string) string {
	t.Helper()
	ix, err := index.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	hits, err := ix.Search(nil, index.SearchOptions{Query: "金丝雀 needle-0"})
	if err != nil || len(hits) == 0 {
		t.Fatalf("no gold entry: %v", err)
	}
	return hits[0].ID
}

func TestRunRanksGoldFirst(t *testing.T) {
	root := t.TempDir()
	if err := scalegen.Generate(root, 150); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		t.Fatal(err)
	}
	gold := goldEntryID(t, root)
	results, err := bench.Run(root, []bench.Question{{ID: 1, Query: "金丝雀 needle-0", ExpectID: gold}})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Rank != 1 || results[0].TopID != gold {
		t.Fatalf("result = %+v", results[0])
	}
}
```

Run: `go test ./internal/bench/ -v` → PASS。
注意：`ix.Search(nil, ...)` 依赖 `Search` 的 `ctx == nil` 容错（`search.go:66-68` 已有）。

- [ ] **Step 3: 接入 CLI**

`cmd/memoryd/main.go`：`run` 的 switch 增加 `case "bench": return runBench(args[1:], stdout, stderr)`；`printUsage` 增加一行 `memoryd bench [--json] <data-dir> <questions.jsonl>  rerun retrieval questions and check first hits`。新函数：

```go
func runBench(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: memoryd bench [--json] <data-dir> <questions.jsonl>")
		return 2
	}
	questions, err := bench.LoadQuestions(fs.Arg(1))
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: cannot load questions: %v\n", err)
		return 2
	}
	results, err := bench.Run(fs.Arg(0), questions)
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: bench failed: %v\n", err)
		return 2
	}
	firstHit, totalTokens := 0, 0
	for _, r := range results {
		totalTokens += r.Tokens
		if r.Rank == 1 {
			firstHit++
		}
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{"results": results, "first_hit": firstHit, "total": len(results), "tokens_estimated": totalTokens}); err != nil {
			fmt.Fprintf(stderr, "memoryd: cannot write JSON: %v\n", err)
			return 2
		}
	} else {
		for _, r := range results {
			rank := "miss"
			if r.Rank > 0 {
				rank = strconv.Itoa(r.Rank)
			}
			fmt.Fprintf(stdout, "%4d  rank %-4s hits %-3d tokens %-6d %s\n", r.ID, rank, r.Hits, r.Tokens, r.Query)
		}
		fmt.Fprintf(stdout, "\nfirst-hit %d/%d, total tokens %d\n", firstHit, len(results), totalTokens)
	}
	if firstHit != len(results) {
		return 1
	}
	return 0
}
```

（import 补 `strconv` 与 `"github.com/li-ziyin/memorytree/internal/bench"`。）

`cmd/memoryd/main_test.go` 追加：

```go
func TestRunBenchExitCodes(t *testing.T) {
	dir := t.TempDir()
	if err := scalegen.Generate(dir, 150); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"index", dir}, strings.NewReader(""), io.Discard, &bytes.Buffer{}); code != 0 {
		t.Fatalf("index code = %d", code)
	}
	// Find the gold id through the tool under test's own dependency.
	ix, err := index.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := ix.Search(nil, index.SearchOptions{Query: "金丝雀 needle-0"})
	if err != nil || len(hits) == 0 {
		t.Fatalf("no gold entry: %v", err)
	}
	gold := hits[0].ID
	ix.Close()

	good := filepath.Join(t.TempDir(), "q.jsonl")
	if err := os.WriteFile(good, []byte(`{"id":1,"query":"金丝雀 needle-0","expect_id":"`+gold+`"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"bench", dir, good}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("bench good code = %d, want 0", code)
	}
	bad := filepath.Join(t.TempDir(), "q.jsonl")
	if err := os.WriteFile(bad, []byte(`{"id":1,"query":"金丝雀 needle-0","expect_id":"019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b99"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"bench", dir, bad}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); code != 1 {
		t.Fatalf("bench bad code = %d, want 1", code)
	}
	if code := run([]string{"bench", dir}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Fatalf("bench usage code = %d, want 2", code)
	}
}
```

（import 补 `io`、`os`、`path/filepath`、index、scalegen。）

`docs/bench-questions.example.jsonl`：

```
# One JSON object per line. expect_id is the gold entry the FIRST hit must be.
# Copy this file next to your data repository and fill real ids via memory_search.
{"id": 1, "query": "docker 端口", "expect_id": "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b05", "limit": 5}
{"id": 2, "query": "网络 峰值", "expect_id": "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b06", "limit": 5}
```

- [ ] **Step 4: 文档**

README Quick start 在 `mcp` 之后加：

```sh
# Rerun retrieval questions and fail when a gold entry is not the first hit
./bin/memoryd bench ~/.memorytree bench-questions.jsonl
```

docs/README.md 的计划文档清单前加一行：`- [Bench questions format](../docs/bench-questions.example.jsonl) — retrieval regression gate`。

- [ ] **Step 5: 验证并提交**

Run: `bash scripts/check.sh` → 通过。
Commit:

```bash
git add internal/tokens internal/bench cmd/memoryd docs/bench-questions.example.jsonl README.md docs/README.md
git commit -s -m "feat: add memoryd bench retrieval regression harness" -m "The 20-question acceptance run was a manual workflow; memoryd bench reruns a JSONL question set against a built index, reports first-hit rank and token estimates with the same estimator as the MCP envelope, and exits 1 on any miss. Token estimation moves to internal/tokens so CLI and server share one number."
```

- [ ] **Step 6: 建真实数据仓的问题集（数据仓产物，不进源码仓）**

以 `docs/m1-acceptance.md` 逐题表为源，在 `E:\MemoryTreeData\bench-questions.jsonl` 建 20 行：query 用当时的检索词，`expect_id` 通过先跑一次 `memoryd bench`（expect 先填任意合法 UUIDv7，看每题 `top_id` 与人工核对）确定。建好后跑：

Run: `./bin/memoryd.exe bench E:/MemoryTreeData E:/MemoryTreeData/bench-questions.jsonl`
Expected: `first-hit 20/20`，退出码 0。此输出是 Task 9 的对照基线（记录到 `docs/perf-baseline.md` 末尾）。

---

### Task 9: 中文短查询走 FTS（索引 schema v2）

**前置：** Task 7 与 Task 8 已完成——采用/回退决策需要前后两份证据（规模基准 + 真实仓 bench 20/20）。

**Files:**
- Modify: `internal/index/index.go:32`（`schemaVersion = 2`）
- Modify: `internal/index/build.go`（建表/插入/完整性检查）
- Modify: `internal/index/search.go`（短词路由；删除 LIKE 分支）
- Test: `internal/index/index_test.go`（重写 LIKE 排序测试；更新短词断言）
- Modify: `DESIGN.md` §4.1、`README.md` 索引段落

**Interfaces:**
- Consumes: 既有 `ftsExpression`、`ftsEligible`、`exampleCopy(t)`、`mustBuild(t, root, opts)`、`search(t, root, opts)` 测试助手（`internal/index/index_test.go`）。
- Produces: 新内部函数 `charSplit(s string) string`、`charExpression(tokens []string) (string, bool)`；FTS5 表 `entries_fts_chars(title, summary, tags, body, tokenize='unicode61')`（rowid 与 `entries` 对齐，索引 rune 间加空格后的文本）；`checkIndex` 签名变为 `checkIndex(ctx context.Context, q execQueryer, wantEntries int) error`。

**背景（为什么这样做）**：trigram 分词要求词 ≥3 字符，而中文查询以双字词为主，目前全部落在无索引、无 BM25 的 LIKE 全表扫描上。unicode61 把每个CJK 字符当独立 token：把文本按「rune 间插空格」写入第二个 FTS 表后，双字词查询 `"网 络"` 作为 FTS5 短语精确等价于子串匹配，并拿到 bm25 列加权排序（标题 8 / 摘要 4 / 标签 2 / 正文 1）。

- [ ] **Step 1: 写失败测试（短词排序属性，仿照被替换的 LIKE 测试）**

在 `internal/index/index_test.go` 中，将 `TestLikeFallbackRanksTitleThenSummaryThenBody` 整体替换为：

```go
func TestShortQueryRanksTitleAboveBody(t *testing.T) {
	root := exampleCopy(t)

	doc, err := format.ReadFile(filepath.Join(root, "global", "node.md"))
	if err != nil {
		t.Fatal(err)
	}
	var node struct {
		ID string `yaml:"id"`
	}
	if err := format.Decode(doc.Meta, &node); err != nil {
		t.Fatal(err)
	}
	write := func(name, title, summary, body string, updated string) string {
		m := map[string]any{
			"schema": "memorytree.entry/v1", "id": ids.NewUUIDv7(), "node": node.ID,
			"type": "project", "status": "active", "sensitivity": "internal",
			"title": title, "summary": summary, "tags_delta": []string{},
			"source": map[string]any{"agent": "t", "kind": "manual",
				"captured_at": "2026-10-01T00:00:00Z"},
			"supersedes": []string{}, "superseded_by": nil,
			"created_at": updated, "updated_at": updated,
		}
		path := filepath.Join(root, "global", "memories", name)
		if err := docfile.WriteFrontmatter(path, m, []byte(body)); err != nil {
			t.Fatal(err)
		}
		return m["id"].(string)
	}
	// Older title hit, newer body-only hit: bm25 column weights must keep
	// the title entry first for a two-character Chinese query.
	titleID := write("2026-10-01-title-hit.md", "端口排障手册", "常规运维摘要。", "正文提到别的内容。", "2026-10-01T00:00:00Z")
	write("2026-10-02-body-hit.md", "无关标题", "无关摘要。", "正文偶然提到端口两个字。", "2026-10-02T00:00:00Z")

	mustBuild(t, root, BuildOptions{Rebuild: true})
	hits := search(t, root, SearchOptions{Query: "端口", Limit: 10})
	if len(hits) < 2 || hits[0].ID != titleID {
		t.Fatalf("short query order = %+v", hits)
	}
}
```

（import 如缺 `format`、`docfile`、`ids` 则补。）

Run: `go test ./internal/index/ -run TestShortQueryRanksTitleAboveBody -v`
Expected: FAIL（当前走 LIKE 分支时标题权重测试可能恰好通过——该测试真正的失败点是 Step 3 之后仍要求同一性质；若当前已 PASS，先记录，Step 5 用执行计划断言改用「explain 计划不含 LIKE」类断言不现实，改为依赖 Step 6 的 bench 对比作为行为差异证据）。

- [ ] **Step 2: schema v2 与 chars 表**

`internal/index/index.go`：`schemaVersion = 1` → `schemaVersion = 2`。
`internal/index/build.go`：`dropStatements` 首部插入 `` `DROP TABLE IF EXISTS entries_fts_chars` ``；`schemaStatements` 末尾追加：

```go
	// Rune-split mirror of the entry text. unicode61 tokenizes each CJK
	// character separately, so a phrase over space-joined runes is an exact
	// substring match and short queries get bm25 ranking.
	`CREATE VIRTUAL TABLE entries_fts_chars USING fts5(
		title,
		summary,
		tags,
		body,
		tokenize='unicode61'
	)`,
```

新增：

```go
// charSplit inserts one space between every rune.
func charSplit(s string) string {
	runes := []rune(s)
	out := make([]rune, 0, len(runes)*2)
	for i, r := range runes {
		if i > 0 {
			out = append(out, ' ')
		}
		out = append(out, r)
	}
	return string(out)
}
```

`insertSnapshot` 中 `ftsStmt` 之后增加 chars 预处理与插入：

```go
	charsStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO entries_fts_chars(rowid, title, summary, tags, body) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer charsStmt.Close()
```

循环内 `ftsStmt.ExecContext` 之后：

```go
		if _, err := charsStmt.ExecContext(ctx, rowid,
			charSplit(e.Title), charSplit(e.Summary), charSplit(tags), charSplit(e.Body),
		); err != nil {
			return fmt.Errorf("index entry chars %s: %w", e.ID, err)
		}
```

`checkIndex` 增加行数校验（签名改 `checkIndex(ctx, q, wantEntries int)`，调用处传 `len(snap.Entries)`）：

```go
	var charsCount int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM entries_fts_chars").Scan(&charsCount); err != nil {
		return fmt.Errorf("FTS integrity check failed: %w", err)
	}
	if charsCount != wantEntries {
		return fmt.Errorf("FTS integrity check failed: entries_fts_chars has %d rows, want %d", charsCount, wantEntries)
	}
```

- [ ] **Step 3: search.go 路由切换**

`default:` 分支整体替换为：

```go
	default:
		expr, ok := charExpression(tokens)
		if !ok {
			return []Hit{}, nil
		}
		query = fmt.Sprintf(selectColumns, "entries_fts_chars JOIN entries e ON e.rowid = entries_fts_chars.rowid")
		query += " WHERE entries_fts_chars MATCH ?"
		args = append(args, expr)
		for _, condition := range conditions {
			query += " AND " + condition
		}
		args = append(args, filterArgs...)
		query += " ORDER BY bm25(entries_fts_chars, 8.0, 4.0, 2.0, 1.0), e.updated_at DESC, e.id LIMIT ?"
```

删除 `likeConditions`、`likeRanking`、`likePattern`（`escapeLike` 保留——`SubtreePath` 过滤仍在用）。新增：

```go
// charExpression converts every token into an FTS5 phrase over its runes so
// one- and two-character queries — most Chinese lookups — match the
// rune-split index with bm25 ranking instead of a table scan.
func charExpression(tokens []string) (string, bool) {
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		var kept []rune
		for _, r := range token {
			if r < 0x80 && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				continue // unicode61 never indexes ASCII punctuation
			}
			kept = append(kept, r)
		}
		if len(kept) == 0 {
			return "", false
		}
		quoted := make([]string, len(kept))
		for i, r := range kept {
			if r == '"' {
				quoted[i] = `""`
			} else {
				quoted[i] = string(r)
			}
		}
		parts = append(parts, `"`+strings.Join(quoted, " ")+`"`)
	}
	return strings.Join(parts, " AND "), true
}
```

import 增加 `"unicode"`。`Search` 顶部 doc 注释同步改写：短词走 rune 拆分 FTS 表。

- [ ] **Step 4: 更新既有测试预期**

Run: `go test ./internal/...`
`TestSearchFTSAndShortQueryFallback`（`index_test.go:178`）：其短词断言若依赖 LIKE 特有顺序，改为断言「命中集合不变、标题命中排前」，并把测试改名为 `TestSearchFTSAndShortQuery`。`mcpserver` 与 `cmd` 套件中涉及短词查询的用例同理（跑失败列表逐个对照更新，语义标准：结果集不丢、相关度序不回退）。

- [ ] **Step 5: 规模对比（采用证据之一）**

Run: `go test ./internal/scalegen/ -bench 'BenchmarkSearchShortCJK|BenchmarkBuildRebuild' -run '^$' -benchtime 5x`
Expected: `BenchmarkSearchShortCJK/1000` 相比 `docs/perf-baseline.md` 记录值不劣化（ns/op 下降或持平）；`BenchmarkBuildRebuild` 允许小幅上升（新增一张表），把两组新数字追加到 perf-baseline.md 的表下方。

- [ ] **Step 6: 真实仓对比（采用证据之二，回退门槛）**

```sh
./bin/memoryd.exe index --rebuild E:/MemoryTreeData
./bin/memoryd.exe bench E:/MemoryTreeData E:/MemoryTreeData/bench-questions.jsonl
```

Expected: `first-hit 20/20`，总 token 不高于基线 +10%。**任一条件不满足 → `git revert` 本任务提交，在 `docs/perf-baseline.md` 记录否决原因，任务关闭（决策：保留 LIKE 回退，双字词问题挂到 M2 重新评估）。**

- [ ] **Step 7: 文档同步与提交**

DESIGN.md §4.1 查询要点改为：

> - 查询：每个词至少 3 个字符时走 FTS5 trigram（中文可用）；不足 3 个字符的查询走 rune 拆分的 `entries_fts_chars`（unicode61，短语等价于子串匹配，bm25 列加权：标题 8、摘要 4、标签 2、正文 1）。默认只返回 `active`，可按节点、类型、有效标签（AND）、状态过滤，结果上限 100 条。`schema_version` 升至 2，旧索引需 `--rebuild`。

README 第 84 行段落中 "an escaped LIKE fallback for shorter Chinese queries"（若已在 Task 前更新过则按现状）改为对应描述：短词走 rune 拆分 FTS 表。

```bash
git add internal/index DESIGN.md README.md
git commit -s -m "feat: route short CJK queries through a rune-split FTS index" -m "Two-character queries are the most common Chinese lookup and previously hit an unindexed LIKE scan with hand-rolled ranking. Schema v2 adds entries_fts_chars, a unicode61 table over space-joined runes where a phrase query is an exact substring match, giving short queries bm25 column-weighted ranking. Verified against the scale benchmarks and the 20-question real-repository bench; LIKE fallback removed."
```

---

### Task 10: MCP 服务运行时卫生（请求超时 + 过期索引警告）

**Files:**
- Modify: `internal/mcpserver/server.go`、`internal/mcpserver/tools.go`（四个工具方法与助手接收 ctx）
- Modify: `cmd/memoryd/main.go`（`runMCP` 传 `Root`）
- Test: `internal/mcpserver/server_test.go`（新增助手与测试）

**Interfaces:**
- Consumes: `validate.ValidateWithSnapshot(root)`（返回 `(res *validate.Result, snap *validate.Snapshot, err error)`，`snap` 仅在校验全绿时非 nil）；`ix.Meta().Fingerprint`。
- Produces: `mcpserver.Options` 新增 `Root string` 字段；`Server` 新增未导出字段 `root/fingerprint/lastCheck/staleWarned`；`dispatch(ctx context.Context, req request)`、`callTool(ctx, raw)`、`memoryBrief/memorySearch/memoryGet/memoryTree(ctx, raw)`、`search/nodes/entry/entryCounts(ctx, ...)` 签名变更（包内私有，无外部影响）。新测试助手 `newTestServerRoot(t *testing.T) (*Server, string)`。

- [ ] **Step 1: 写失败测试**

`internal/mcpserver/server_test.go` 追加（同时新增助手，逻辑复制自 `newTestServerWith` 但保留 root）：

```go
func newTestServerRoot(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	res, err := index.Build(root, index.BuildOptions{})
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	ix, err := index.Open(root)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	t.Cleanup(func() { ix.Close() })
	srv, err := New(ix, Options{Root: root})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	_ = res
	return srv, root
}

func TestStaleIndexWarning(t *testing.T) {
	srv, root := newTestServerRoot(t)

	// Force the throttle open: same package, internal field.
	srv.lastCheck = time.Now().Add(-time.Minute)
	callTool(t, srv, "memory_search", map[string]any{"query": "SSE"})
	if got := srv.warnings; len(got) != 0 {
		t.Fatalf("fresh repository warned: %v", got)
	}

	nodePath := filepath.Join(root, "global", "node.md")
	data, err := os.ReadFile(nodePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nodePath, append(data, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.lastCheck = time.Now().Add(-time.Minute)
	result := callTool(t, srv, "memory_search", map[string]any{"query": "SSE"})
	env := mustMap(t, result["structuredContent"], "structuredContent")
	meta := mustMap(t, env["meta"], "meta")
	warnings, ok := meta["warnings"].([]any)
	if !ok {
		t.Fatalf("no warnings in meta: %v", meta)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(fmt.Sprint(w), "source files changed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing stale warning: %v", warnings)
	}
}
```

（import 补 `fmt`、`os`、`path/filepath`、`strings`、`time` 中缺的。）若 `newTestServerRoot` 里的空仓校验需要非空仓库导致 `callTool memory_search` 报错，则改为在 setup 里用 `docfile` 写一条条目（参考 Task 9 测试里的 write 闭包）后再 Build。
Run: `go test ./internal/mcpserver/ -run TestStaleIndexWarning -v` → 编译失败（`Options.Root`、`srv.lastCheck` 不存在）。

- [ ] **Step 2: 实现服务端改动**

`server.go`：

```go
const (
	latestProtocolVersion = "2025-06-18"
	maxMessageBytes       = 8 << 20
	requestTimeout        = 60 * time.Second
	staleCheckInterval    = 30 * time.Second
)

type Options struct {
	Version  string
	Warnings []string
	// Root enables the stale-index check. Empty disables it.
	Root string
}

type Server struct {
	index      *index.Index
	version    string
	generation int64
	warnings   []string
	root       string
	fingerprint string
	lastCheck  time.Time
	staleWarned bool
}
```

`New` 填充 `root: opts.Root`、`fingerprint: meta.Fingerprint`、`lastCheck: time.Now()`。
`Serve` 循环内在 `s.dispatch(req)` 之前插入：

```go
		if req.Method == "tools/call" {
			s.refreshStaleness(time.Now())
		}
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		result, rpcErr := s.dispatch(ctx, req)
		cancel()
```

新增方法：

```go
// refreshStaleness re-reads the source repository at most once per
// staleCheckInterval and records one warning when it no longer matches the
// index the server started from. Serve is single-goroutine; no locking.
func (s *Server) refreshStaleness(now time.Time) {
	if s.root == "" || s.staleWarned || now.Sub(s.lastCheck) < staleCheckInterval {
		return
	}
	s.lastCheck = now
	res, snap, err := validate.ValidateWithSnapshot(s.root)
	var warning string
	switch {
	case err != nil:
		warning = fmt.Sprintf("repository cannot be re-read (%v); results are from the startup snapshot", err)
	case snap == nil:
		warning = fmt.Sprintf("repository has validation errors (%d); results are from the startup snapshot", res.Errors())
	case snap.Fingerprint != s.fingerprint:
		warning = "source files changed since the index was built; restart memoryd mcp to serve current data"
	default:
		return
	}
	s.warnings = append(s.warnings, warning)
	s.staleWarned = true
}
```

import 增加 `"time"` 与 `"github.com/li-ziyin/memorytree/internal/validate"`。
`dispatch`/`callTool` 与 `tools.go` 四个工具方法、`search/nodes/entry/entryCounts` 助手全部加 `ctx context.Context` 首参并传递（把 `server.go:251-265` 的 `context.Background()` 换成收到的 ctx；`memoryGet` 内部的 `s.entry(ctx, id)` 同步）。
`cmd/memoryd/main.go` `runMCP`：`mcpserver.Options{Version: version, Warnings: warnings}` → 增 `Root: dir`。

- [ ] **Step 3: 跑套件**

Run: `bash scripts/check.sh`
Expected: 全部通过（既有 e2e 测试覆盖签名重构后的行为不变）。

- [ ] **Step 4: 真实仓冒烟 + 文档**

```sh
./bin/memoryd.exe mcp E:/MemoryTreeData
```

stdin 送一行 `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` 后关闭（或直接启动看无报错退出）。DESIGN.md §5.2 之后补一句：

> 服务端每次 `tools/call` 前最多每 30 秒重读一次数据仓指纹；与启动快照不一致时在响应 warnings 中提示重启，而不是静默服务过期数据。每个请求带 60 秒超时。

- [ ] **Step 5: 提交**

```bash
git add internal/mcpserver cmd/memoryd DESIGN.md
git commit -s -m "feat: add per-request deadlines and stale-index warnings to the MCP server" -m "The server served its startup snapshot silently until restarted; it now re-checks the source fingerprint at most every 30 seconds and surfaces a stale-data warning, and every tools/call runs under a 60-second context so a wedged query can no longer hang the loop indefinitely. The context plumbing also prepares the M2 write path for cancellation."
```

---

## 收尾（全部任务完成后）

- [ ] `bash scripts/check.sh` 全绿；`git log --oneline` 显示 10 个新提交。
- [ ] DESIGN.md 状态行追加：`2026-10-__ 工程加固完成：CI 三平台矩阵、fuzz、规模基线（docs/perf-baseline.md）、bench 回归工具、短词 FTS（schema v2）、MCP 过期警告`。
- [ ] README Roadmap 的 M1 行补一句工程加固完成；M2 行补「动工前已有 perf-baseline 与 bench 回归门槛」。
- [ ] 单独提交 `docs: record engineering hardening completion`。

## Self-Review 记录

- 评审六类缺口 → 任务映射：CI（1、2）；解析层测试与 fuzz（3、4、5）；规模未测（7）；中文短词（9）；基准不可复跑（8）；运行时棱角——ctx/过期警告（10）、版本注入（1）、schema 一致性（2）。全部有归属。
- 类型一致性抽查：`charExpression` 返回 `(string, bool)` 与 `Search` 调用一致；`checkIndex` 新签名 `(ctx, q, wantEntries int)` 与调用处 `len(snap.Entries)` 一致；`bench.Result` 字段与 `runBench` 引用（`Rank/Hits/Tokens/Query/TopID`）一致；`Options.Root` 在 `New`、测试助手、`runMCP` 三处一致。
- 已知留白（有意）：Task 8 Step 6 的真实问题集依赖执行时人工核对 gold ID，无法预先写死（数据在用户私有仓）；Task 9 Step 4 的既有测试预期更新以「跑失败列表逐个对照」为流程，语义标准已写明。
