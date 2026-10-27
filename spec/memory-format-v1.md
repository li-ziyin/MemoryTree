# MemoryTree Memory Format v1

- Spec version: 1.0.0 (draft)
- Data format version: `format_version: 1`
- Status: Draft, 2026-10-20
- License: Apache-2.0 (this document and the JSON Schemas); users' memory data is outside the scope of this license

This document defines the on-disk layout, frontmatter fields, tag semantics, state machines, and validation rules of a MemoryTree data repository. Index structures and MCP interfaces are out of scope.

The key words **MUST**,**MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are to be interpreted as described in RFC 2119 / RFC 8174.

## 1. Scope

A MemoryTree data repository is a directory containing:

- a tree of nodes made of directories and `node.md` files;
- memory entries under project nodes and the reserved `global` category;
- one repository-level metadata file, `memory.yaml`;
- optional reserved directories `.inbox/`, `.archive/`, `.index/` (not managed by this format);
- an optional `.git/` (recommended, for audit and recovery).

The format governs file contents only; it does not prescribe how tools index, search, or summarize.

## 2. Conformance

An implementation claiming conformance to this specification:

1. MUST read and preserve unknown frontmatter fields verbatim;
2. MUST enforce all MUST-level checks in section 12;
3. MUST refuse to open a repository with a higher major `format_version`, with a clear error;
4. MUST use the field names, types, and enum values of this specification when writing;
5. MUST NOT delete unknown files or directories that do not belong to this format.

## 3. Repository layout

```
<repo-root>/
├── memory.yaml
├── node.md                         # the root node
├── global/                         # reserved category; entries only
│   ├── node.md
│   └── memories/
│       └── 2026-10-20-user-profile.md
├── work/                           # an ordinary category
│   ├── node.md
│   └── agentflow/                  # a project leaf
│       ├── node.md
│       └── memories/
│           └── 2026-10-20-sse-pitfall.md
├── .inbox/                         # raw captures, tool-managed
├── .archive/                       # raw sessions, tool-managed
├── .index/                         # derived index, tool-managed
└── .git/                           # optional
```

Rules:

- The repository root MUST contain `memory.yaml` and the root `node.md`.
- Every directory representing a node MUST contain a `node.md`.
- Only `project` nodes and the reserved `global` category MAY contain a `memories/` directory.
- `global` is the reserved category under the root; its `slug` MUST be `global`; it MUST NOT have child nodes, only entries.
- `.inbox/`, `.archive/`, `.index/` are reserved directories; readers MUST ignore their contents; they SHOULD be excluded by the data repository's `.gitignore`.
- Other unknown files/directories MAY exist; tools MUST NOT delete or rewrite them.

## 4. Identifiers and references

### 4.1 IDs

The `id` of a node or entry MUST be a UUIDv7 as defined by RFC 9562, in lowercase canonical form:

```
^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$
```

IDs MUST be globally unique within a repository (nodes and entries share one namespace). Once assigned, an ID MUST NOT change.

### 4.2 slug

Directory names and a node's `slug` are human-readable stable labels, not identity. A non-root node's `slug` MUST:

- match `^[a-z0-9][a-z0-9-]{0,63}$`;
- be exactly the directory name;
- be unique among siblings;
- avoid the reserved names: `node.md`, `memories`, `.inbox`, `.archive`, `.index`, `global` (`global` is allowed only as the reserved category under the root).

The root node's `slug` MUST be the empty string; its directory is the repository root.

### 4.3 References

Stable references inside Markdown bodies:

```
memory://<entry-id>
node://<node-id>
```

For example:

```markdown
See [SSE decoding pitfall](memory://019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b06).
```

References MUST use IDs, never directory paths. Renaming or moving directories MUST NOT break references. Implementations SHOULD resolve `memory://` and `node://` on read and check that the target exists.

File and directory names are not identity; tools MUST NOT rely on them for persistent references.

## 5. Tags and effective tags

### 5.1 Storage form

Each node and entry stores only its own incremental tags:

```yaml
tags_delta: [work, java]
```

Effective tags are the union along the path:

```
effective_tags(node) = effective_tags(parent) ∪ node.tags_delta
effective_tags(entry) = effective_tags(entry.node) ∪ entry.tags_delta
```

The root node's `tags_delta` SHOULD be an empty array; its effective tags are the empty set.

Example:

| Node | `tags_delta` | Effective tags |
|---|---|---|
| `/` | `[]` | `{}` |
| `/work` | `[work]` | `{work}` |
| `/work/agentflow` | `[java, agent]` | `{work, java, agent}` |
| an entry under `agentflow` | `[sse]` | `{work, java, agent, sse}` |

The invariant "a child's effective tag set ⊇ its parent's" is therefore guaranteed by the data model itself; no descendant files need rewriting when a subtree moves. Moving a node changes only `parent` and the directory location; effective tags are recomputed on read.

### 5.2 Tag syntax

- Tags MUST be NFC-normalized, trimmed, and lowercased.
- After normalization they MUST match `^[\p{L}\p{N}_-]{1,32}$` (letters including CJK, digits, underscore, hyphen; no spaces).
- `tags_delta` MUST NOT contain duplicates.
- A node's or entry's `tags_delta` MUST NOT contain a tag already inherited from an ancestor (a meaningless repeat that distorts diffs).
- Tag order carries no meaning; writers SHOULD sort lexicographically for reproducibility.

### 5.3 Display and retrieval

Files and this specification store deltas; anything shown to users or used for retrieval MUST use effective tags. Tools MAY display effective tags in generated tree views, indexes, or summaries, but MUST NOT write them back into `node.md` or entry frontmatter.

## 6. Repository metadata `memory.yaml`

Location: the repository root. Encoding UTF-8, line endings LF.

```yaml
schema: memorytree.repo/v1
format_version: 1
repo_id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b00
created_at: "2026-10-20T00:00:00Z"
default_sensitivity: private
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `schema` | string | yes | fixed, `memorytree.repo/v1` |
| `format_version` | integer | yes | `1` for v1 |
| `repo_id` | string (UUIDv7) | yes | unique repository ID, immutable after creation |
| `created_at` | string (RFC3339) | yes | repository creation time |
| `default_sensitivity` | enum | no | `public` / `internal` / `private`; default `private` |
| `description` | string | no | what this repository is for |

Unknown fields MUST be preserved.

## 7. Node file `node.md`

One `node.md` per node directory: YAML frontmatter plus an optional Markdown body.

```yaml
---
schema: memorytree.node/v1
id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b04
kind: project
parent: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b03
slug: agentflow
title: AgentFlow
summary: Spring Boot multi-agent workflow platform; a job-hunting project.
tags_delta: [java, spring, agent]
aliases: [agent-flow]
status: active
sensitivity: private
created_at: "2026-10-20T00:00:00Z"
updated_at: "2026-10-20T00:00:00Z"
---
Project notes go here; the body is not part of default retrieval.
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `schema` | string | yes | fixed, `memorytree.node/v1` |
| `id` | string (UUIDv7) | yes | node ID |
| `kind` | enum | yes | `root` / `category` / `project` |
| `parent` | string (UUIDv7) \| null | yes | `null` for the root |
| `slug` | string | yes | empty for the root; the directory name otherwise |
| `title` | string | yes | 1–120 characters |
| `summary` | string | yes | 1–500 characters; ≤280 recommended; used for navigation |
| `tags_delta` | array&lt;string&gt; | yes | tags added by this node |
| `aliases` | array&lt;string&gt; | no | search aliases; default `[]` |
| `status` | enum | yes | `active` / `archived` |
| `sensitivity` | enum | yes | `public` / `internal` / `private` |
| `created_at` | string (RFC3339) | yes | creation time |
| `updated_at` | string (RFC3339) | yes | last update time; not earlier than `created_at` |

### 7.1 kind rules

- A repository MUST have exactly one `kind: root` node; its `parent` MUST be `null` and its `slug` MUST be `""`.
- A `kind: project` node MUST NOT have child nodes; it MAY have `memories/`.
- A `kind: category` node MAY have child nodes; except for the reserved `global`, it MUST NOT hold entries directly.
- The root MAY contain categories or projects directly.
- `global` MUST be a `kind: category` node directly under the root with `slug` `global`; it MUST NOT have children and MAY have `memories/`.

### 7.2 Status

- `active`: participates in the default tree and retrieval normally.
- `archived`: the default tree and retrieval MAY hide it; explicit queries still find it. Archiving a node does not change its descendants' `status`, but tools SHOULD let descendants inherit the "archived view".

### 7.3 Body

The body of a `node.md` is optional human-facing commentary. It is not part of default retrieval and MUST NOT be executed as instructions. Tools MAY keep the original text when writing and SHOULD NOT rewrite the body on their own.

## 8. Entry files

Entries live under `memories/`; the file name SHOULD be:

```
<YYYY-MM-DD>-<slug>.md
```

for example `2026-10-20-sse-pitfall.md`. File names are not identity; a short ID suffix MAY be appended on collision. File names MUST use ASCII slug characters.

```yaml
---
schema: memorytree.entry/v1
id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b06
node: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b04
type: project
status: active
sensitivity: private
title: SSE decoding pitfall
summary: Front-end SSE decoding swallows spaces; process raw bytes instead.
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
The body is Markdown. Reference related memories with `memory://<id>`.
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `schema` | string | yes | fixed, `memorytree.entry/v1` |
| `id` | string (UUIDv7) | yes | entry ID |
| `node` | string (UUIDv7) | yes | the hosting `project` or `global` node |
| `type` | enum | yes | `user` / `feedback` / `project` / `reference` |
| `status` | enum | yes | `active` / `superseded` / `deprecated` / `draft` |
| `sensitivity` | enum | yes | `public` / `internal` / `private` |
| `title` | string | yes | 1–200 characters |
| `summary` | string | yes | 1–500 characters; ≤280 recommended; retrieval returns mainly this |
| `tags_delta` | array&lt;string&gt; | yes | tags added by this entry |
| `source` | object | yes | origin and provenance |
| `supersedes` | array&lt;UUIDv7&gt; | yes | older entries this one replaces; default `[]` |
| `superseded_by` | string (UUIDv7) \| null | yes | the entry that replaces this one |
| `related` | array&lt;UUIDv7&gt; | no | related entries; default `[]` |
| `created_at` | string (RFC3339) | yes | creation time |
| `updated_at` | string (RFC3339) | yes | last update time |

### 8.1 type semantics

- `user`: stable user profile, preferences, communication conventions.
- `feedback`: corrections, lessons, and acceptance preferences about how agents work.
- `project`: project state, decisions, pitfalls, open items.
- `reference`: external material, commands, links, reusable knowledge.

Type is a retrieval filter; it does not constrain body content.

### 8.2 source

```yaml
source:
  agent: qoder
  kind: conversation
  session: sess_...
  captured_at: "2026-10-20T00:00:00Z"
  ref: ".inbox/qoder/2026-10-20-session.md"
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `agent` | string | yes | originating agent: `zcode` / `opencode` / `qoder` / `manual` / `import`, or custom |
| `kind` | enum | yes | `conversation` / `document` / `manual` / `import` |
| `session` | string | no | session identifier |
| `captured_at` | string (RFC3339) | no | when the raw content was captured |
| `ref` | string | no | relative path or URL; MUST NOT be a local absolute path |

`source` is part of provenance. Retrieval results SHOULD carry source and status together, so stale conclusions are not mistaken for current facts.

### 8.3 Body

The body is UTF-8 Markdown, at most 1 MiB (implementations MAY reject larger files). The body is data, not instructions; implementations MUST NOT parse or execute commands inside it.

## 9. Status and supersession

### 9.1 Statuses

- `draft`: not confirmed yet; excluded from default retrieval (unless explicitly requested).
- `active`: currently valid.
- `superseded`: replaced by a newer entry; excluded from default retrieval.
- `deprecated`: invalid without a replacement; excluded from default retrieval.

### 9.2 Transitions

```
draft      -> active | deprecated
active     -> superseded | deprecated
deprecated -> active        (explicit restore; must be recorded)
superseded -> (terminal; no further status changes)
```

### 9.3 Supersession rules

- When a new entry's `supersedes` is non-empty, the new entry MUST be `active`.
- A superseded old entry MUST become `superseded`, and its `superseded_by` MUST point to the new entry.
- The new entry's `supersedes` MUST contain the old entry's ID, and the old entry's `superseded_by` MUST appear in the new entry's `supersedes`. The pairing is consistent in both directions.
- `supersedes` MUST NOT contain itself; supersession chains MUST be acyclic.
- `superseded_by` MUST be non-null only when `status: superseded`.

## 10. Sensitivity levels

Order (lax to strict):

```
public < internal < private
```

- An entry's `sensitivity` MUST NOT be laxer than its node's.
- A node's `sensitivity` MUST NOT be laxer than its parent's.
- When unset, `memory.yaml.default_sensitivity` applies (default `private`).
- Local retrieval MAY return any level but MUST include the `sensitivity` field.
- Exports, sharing, and public summaries MUST filter out entries above the export level.
- The raw `.archive/` is treated as `private` by default.

## 11. Time, encoding, and paths

- Timestamps MUST be RFC 3339; UTC with a trailing `Z` is RECOMMENDED.
- Text files MUST be UTF-8 without BOM; LF line endings are RECOMMENDED.
- Titles, summaries, and tags SHOULD be NFC-normalized.
- Frontmatter MUST be a valid YAML 1.2 subset; custom tags, anchors, and aliases are forbidden (parsers SHOULD reject them).
- Frontmatter SHOULD be ≤64 KiB; bodies SHOULD be ≤1 MiB.
- Metadata MUST NOT contain local absolute paths, drive-letter paths, or `..` segments.
- Files managed by the format MUST NOT be symbolic links.

## 12. Validation rules

Implementations MUST enforce these checks before writing; `memoryd validate` runs them over a whole repository and emits stable error codes.

### 12.1 Structure

- **V-ROOT-1**: exactly one `kind: root` node, located at the repository root.
- **V-PARENT-1**: every non-root node's `parent` exists, is not itself, and forms no cycle.
- **V-KIND-1**: a `project` node has no child node directories.
- **V-KIND-2**: entries hang only under `project` or `global` nodes.
- **V-KIND-3**: the `global` node has no child nodes.
- **V-SLUG-1**: `slug` matches the directory name; unique among siblings; follows the syntax and reserved-name rules of 4.2.
- **V-ID-1**: node and entry IDs are UUIDv7 and unique repository-wide.
- **V-ID-2**: every ID referenced by `parent`, `node`, `supersedes`, `superseded_by`, and `related` exists.
- **V-FILE-1**: files managed by the format are not symbolic links; frontmatter parses.

### 12.2 Tags

- **V-TAG-1**: every tag follows the normalization and syntax of 5.2.
- **V-TAG-2**: no duplicates inside `tags_delta`, and no tags already inherited from ancestors.
- **V-TAG-3**: effective tags are computed per 5.1; display layers MUST NOT write effective tags back into files.

### 12.3 Status and supersession

- **V-SUP-1**: `supersedes` excludes itself; chains are acyclic.
- **V-SUP-2**: `supersedes` / `superseded_by` are consistent in both directions.
- **V-SUP-3**: `superseded_by` is non-null only for `status: superseded`.
- **V-SUP-4**: a new entry with non-empty `supersedes` is `active`.
- **V-SUP-5**: `superseded` is terminal.

### 12.4 Sensitivity and time

- **V-SENS-1**: a node is not laxer than its parent; an entry is not laxer than its node.
- **V-TIME-1**: timestamps parse; `updated_at` is not earlier than `created_at`.
- **V-TIME-2**: an entry's `created_at` SHOULD match the file-name date; a mismatch is only a warning.

### 12.5 Content

- **V-CONTENT-1**: `title` and `summary` are non-empty and within their length limits.
- **V-CONTENT-2**: `source.agent` and `source.kind` exist; `source.ref` is not an absolute path.
- **V-CONTENT-3**: the body is within its size limit.

### 12.6 Warnings (non-fatal)

- Node depth greater than 4.
- More than 8 children under one parent.
- `summary` longer than 280 characters.
- Index generation behind the file state.
- An entry's `created_at` disagreeing with its file-name date.

## 13. Versioning and migration

- `memory.yaml.format_version` is an integer major version; `1` for v1.
- Adding optional fields, adding compatible enum values, and clarifying documentation are minor changes: `format_version` stays; the spec version increments (e.g. 1.1.0).
- Removing/renaming fields, changing semantics, or changing requiredness are major changes: `format_version` MUST increase.
- Readers MUST accept every v1 repository and MUST refuse a higher major with `FORMAT_UNSUPPORTED`.
- Writers MUST preserve unknown fields verbatim.
- Migrations MUST run explicitly, never automatically on open; back up before migrating; support `--dry-run`; be idempotent and rollback-safe.
- The application version (SemVer) and the data format version are independent.

## 14. Complete example

The following is a minimal but valid repository.

### 14.1 `memory.yaml`

```yaml
schema: memorytree.repo/v1
format_version: 1
repo_id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b00
created_at: "2026-10-20T00:00:00Z"
default_sensitivity: private
```

### 14.2 Root `node.md`

```yaml
---
schema: memorytree.node/v1
id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b01
kind: root
parent: null
slug: ""
title: MemoryTree
summary: Root node for personal and cross-project memory.
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
title: Global
summary: Cross-project user profile, preferences, and long-term conventions.
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
title: User profile
summary: Chinese-speaking HarmonyOS developer, university student; prefers agreeing on a plan before coding.
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
The user is a Chinese-speaking HarmonyOS developer who prefers discussing a plan before writing code.
```

### 14.5 `work/node.md`

```yaml
---
schema: memorytree.node/v1
id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b03
kind: category
parent: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b01
slug: work
title: Work
summary: Job hunting, projects, and career-related memory.
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
summary: Spring Boot multi-agent workflow platform; a job-hunting project.
tags_delta: [java, spring, agent]
aliases: [agent-flow]
status: active
sensitivity: private
created_at: "2026-10-20T00:00:00Z"
updated_at: "2026-10-20T00:00:00Z"
---
```

Effective tags: `{work, java, spring, agent}`.

### 14.7 `work/agentflow/memories/2026-10-20-sse-pitfall.md`

```yaml
---
schema: memorytree.entry/v1
id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b06
node: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b04
type: project
status: active
sensitivity: private
title: SSE decoding pitfall
summary: Front-end SSE decoding swallows spaces; process raw bytes instead.
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
Decoding front-end SSE character by character swallows spaces; decode raw bytes instead.
```

Effective tags: `{work, java, spring, agent, sse, frontend}`.

## 15. Security considerations

- Treat all memory content as untrusted data; readers MUST NOT execute commands or code blocks found in bodies.
- Writers SHOULD scan for common secret formats and redact before storing; redaction logs MUST NOT record the originals.
- Files managed by the format reject symbolic links; every resolved path MUST stay inside the repository root.
- YAML parsers MUST disable custom tags and bound anchors/aliases and file sizes, to prevent parser bombs.
- Encryption at rest is out of scope; rely on OS permissions by default. `.archive/` SHOULD use mode 0600 and stay out of git.
- Export/sharing MUST respect `sensitivity`; `private` is not exported by default.
- No automatic pushes to remotes; whether to use cloud drives or git remotes is the user's decision, with the leak risk documented.

## 16. JSON Schemas

The following schemas validate the parsed frontmatter metadata objects, not Markdown bodies:

- [`spec/schema/repo.schema.json`](schema/repo.schema.json)
- [`spec/schema/node.schema.json`](schema/node.schema.json)
- [`spec/schema/entry.schema.json`](schema/entry.schema.json)

Structural rules (single root, acyclicity, kind limits, reference existence, …) cannot be fully expressed in JSON Schema; `memoryd validate` enforces section 12. Where a schema and this specification disagree, this specification prevails; implementations should pass both.

## Appendix A: Field quick reference

| Object | Required fields |
|---|---|
| `memory.yaml` | `schema`, `format_version`, `repo_id`, `created_at` |
| Node | `schema`, `id`, `kind`, `parent`, `slug`, `title`, `summary`, `tags_delta`, `status`, `sensitivity`, `created_at`, `updated_at` |
| Entry | `schema`, `id`, `node`, `type`, `status`, `sensitivity`, `title`, `summary`, `tags_delta`, `source`, `supersedes`, `superseded_by`, `created_at`, `updated_at` |

## Appendix B: Glossary

- **Node**: a directory in the tree, representing a category or a project.
- **Entry**: one atomic memory, living under a project or `global`.
- **Delta tags**: the tags a node/entry itself adds, excluding inherited ones.
- **Effective tags**: the full tag set obtained by unioning along the path.
- **Generation**: the data version an index corresponds to; used to detect a stale index.
- **Gardener**: the offline agent task that proposes tree-restructuring changes.
