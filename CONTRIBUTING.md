# Contributing to MemoryTree

Thanks for your interest. MemoryTree has a working format validator, CLI, legacy import, and SQLite FTS5 search index (M0/M1); the read-only MCP server is next. Useful contributions right now include format review, threat-model review, tests against real-world memory layouts, and MCP client integration.

## Ground rules

- Be respectful and constructive. This project follows the spirit of the Contributor Covenant.
- Do not include real personal memories, credentials, API keys, or customer data in issues, pull requests, tests, or examples. Use synthetic data.
- Security issues must not be reported in public issues. See [SECURITY.md](SECURITY.md).
- By contributing, you agree that your contribution is licensed under Apache-2.0 and that you have the right to submit it. Sign off every commit with the Developer Certificate of Origin (DCO):

```bash
git commit -s
```

The sign-off line must contain your real name and email:

```
Signed-off-by: Your Name <you@example.com>
```

## Development setup

Prerequisites:

- Go 1.25 or newer
- Git 2.30 or newer

Build and test:

```bash
go build -o bin/memoryd ./cmd/memoryd
go test ./...
go vet ./...
gofmt -l .
```

The example repository must stay valid:

```bash
./bin/memoryd validate spec/examples
```

## Changing the memory format

The on-disk format is the project's contract. Changes to `spec/` require more care than code changes:

1. Open an issue describing the problem and the proposed change.
2. Decide whether it is a minor (optional field, compatible enum value) or major (rename, removal, semantic change) change.
3. Update the spec, all affected JSON Schemas, examples, validator, and migration notes in the same pull request.
4. Add tests that prove old repositories still validate, or that the migration converts them correctly.
5. Never silently rewrite user data. Migrations must be explicit, backed up, dry-runnable, and idempotent.

## Pull requests

- Keep changes focused; avoid unrelated refactors.
- Add tests for behavior changes.
- Run the full test suite and static checks before opening a pull request.
- Update documentation when behavior or format changes.
- Do not add a dependency without a license check and a corresponding entry in `THIRD_PARTY_NOTICES.md`.

## Commit messages

Use a clear imperative subject line, optionally with a conventional prefix (`spec:`, `feat:`, `fix:`, `docs:`, `test:`, `chore:`). Example:

```
spec: require reciprocal supersedes links
```

## License

By submitting a contribution, you license it under the Apache License 2.0. See [LICENSE](LICENSE).
