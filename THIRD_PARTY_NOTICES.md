# Third-Party Notices

MemoryTree links the following third-party code into the `memoryd` binary. This file must be regenerated and reviewed whenever the dependency graph changes, and the result must be included in release archives and the SBOM.

## Direct dependencies

| Component | Version | Purpose | License |
|---|---|---|---|
| gopkg.in/yaml.v3 | v3.0.1 | YAML frontmatter parsing and writing | MIT / Apache-2.0 (dual) |
| golang.org/x/text | v0.29.0 | Unicode NFC normalisation for tags | BSD-3-Clause |
| Go standard library | Go 1.27.3 | runtime and core packages | BSD-3-Clause |

`gopkg.in/yaml.v3` includes portions derived from libyaml under the MIT license. The full license text is distributed with the module at `gopkg.in/yaml.v3@v3.0.1/LICENSE`.

## External executables (not linked)

| Component | Purpose | License |
|---|---|---|
| system `git` | optional audit history for the data repository | GPL-2.0 |

MemoryTree invokes `git` as a separate process when git integration is enabled. It does not link GPL code into the binary.

## Planned dependencies

These are not used yet and must be verified before they are added:

| Component | Purpose | License status |
|---|---|---|
| MCP Go SDK | MCP transport and protocol | to verify |
| pure-Go SQLite driver | local FTS5 index | to verify |
| file-lock library | cross-process writes | to verify |

## Release checklist

- Every direct and transitive dependency has its license recorded.
- No GPL/AGPL or unknown-license dependency is linked into the distributed binary.
- Generated notices are included in release archives.
- The dependency inventory matches the SBOM attached to the release.
