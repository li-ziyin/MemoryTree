# Data format versioning and migration

How MemoryTree versions its on-disk format and how repositories move between versions. The normative rules live in [spec/memory-format-v1.md](../spec/memory-format-v1.md) §13; this page is the operational guide.

## Two independent versions

| Version | Where | Bumps on |
|---|---|---|
| Application version (SemVer) | `memoryd version`, git tags | Features and fixes of the software — independent of the data format. |
| Data format version | `memory.yaml: format_version` | Breaking changes to the on-disk format only. Today: `1`. |

Minor format changes (new optional fields, new compatible enum values, documentation clarifications) do **not** bump `format_version`; the spec version increments instead (e.g. 1.1.0). Removing or renaming fields, changing semantics, or changing requiredness bumps `format_version` and is a major change.

## Reader and writer rules

- A reader accepts every repository of its own major and **refuses** a higher one with the `FORMAT_UNSUPPORTED` error and an upgrade hint — never a best-effort partial open.
- Unknown frontmatter fields are preserved verbatim on every read/write cycle; upgrading `memoryd` never discards user data this way.
- Writers always emit the field names, types, and enums of the format they target.

## Running a migration

Migrations are always explicit — `memoryd` never migrates a repository just by opening it.

```
memoryd migrate --to N --dry-run <data-dir>   # preview: what would change, per file
memoryd migrate --to N <data-dir>             # execute
```

Contract for every migration step:

1. Back up the repository first (a git commit plus, when git is unavailable, a file copy).
2. Support `--dry-run` with a full change preview.
3. Idempotent: running twice leaves the repository in the same state.
4. Rollback-safe: on failure, restore the pre-migration state; a half-migrated repository never happens.
5. After the migration, `memoryd validate` must pass before anything else touches the repository.

## Status

- `format_version: 1` is the only version; no migration steps exist yet — `memoryd migrate` will land together with the first format major (v2).
- The **index** is not versioned data: `.index/` is disposable and rebuilt from files. An index schema change (currently schema v2) is handled by an explicit `memoryd index --rebuild`, not by migration.
