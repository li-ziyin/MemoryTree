# MemoryTree Memory Format

The on-disk format is the project's long-lived contract. Code is replaceable; a user's memory repository should outlive any single implementation.

## Current version

- **v1 (draft)** — [memory-format-v1.md](memory-format-v1.md)（[中文同步版](memory-format-v1.zh-CN.md)）
- JSON Schemas:
  - [repo.schema.json](schema/repo.schema.json) — `memory.yaml`
  - [node.schema.json](schema/node.schema.json) — `node.md`
  - [entry.schema.json](schema/entry.schema.json) — `memories/*.md`
- Example repository: [examples/](examples/)

## Versioning

- `memory.yaml.format_version` is an integer major version.
- Minor, backward-compatible changes (optional fields, compatible enum values) keep the same `format_version`; the specification version increments (e.g. 1.1.0).
- Renames, removals, required-field changes, or semantic changes require a new major format version and an explicit migration path.
- Readers must refuse a newer major version rather than guessing.
- Unknown frontmatter fields must be preserved when rewriting a file.

## Validation

At M0, `memoryd validate` will enforce the normative rules in section 12 of the specification, in addition to the JSON Schemas. The JSON Schemas alone cannot express:

- exactly one root node;
- parent existence and acyclicity;
- project nodes having no child nodes;
- entries only under projects or `global`;
- effective tag computation and inherited-tag rules;
- reciprocal `supersedes` / `superseded_by` links;
- sensitivity monotonicity;
- slug/ID uniqueness and reference integrity.

The example repository is the first golden fixture for the validator.
