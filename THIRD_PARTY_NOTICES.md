# Third-Party Notices

MemoryTree links the following third-party code into the `memoryd` binary. This file must be regenerated and reviewed whenever the dependency graph changes, and the result must be included in release archives and the SBOM.

## Direct dependencies

| Component | Version | Purpose | License |
|---|---|---|---|
| gopkg.in/yaml.v3 | v3.0.1 | YAML frontmatter parsing and writing | MIT / Apache-2.0 (dual) |
| golang.org/x/text | v0.43.0 | Unicode NFC normalisation for tags | BSD-3-Clause |
| modernc.org/sqlite | v1.59.0 | pure-Go SQLite driver with FTS5 for the local index | BSD-3-Clause |
| Go standard library | Go 1.27.3 | runtime and core packages | BSD-3-Clause |

`gopkg.in/yaml.v3` includes portions derived from libyaml under the MIT license. The full license text is distributed with the module at `gopkg.in/yaml.v3@v3.0.1/LICENSE`.

`modernc.org/sqlite` embeds SQLite itself, which is in the public domain (`LICENSE-SQLITE` in that module), and links the transitive modules below. Its `LICENSE` is BSD-3-Clause.

## Transitive dependencies

| Component | Version | Required by | License |
|---|---|---|---|
| modernc.org/libc | v1.75.7 | modernc.org/sqlite | BSD-3-Clause |
| modernc.org/mathutil | v1.7.1 | modernc.org/sqlite | BSD-3-Clause |
| modernc.org/memory | v1.12.1 | modernc.org/sqlite | BSD-3-Clause |
| github.com/dustin/go-humanize | v1.0.1 | modernc.org/sqlite | MIT |
| github.com/google/uuid | v1.6.0 | modernc.org/sqlite | BSD-3-Clause |
| github.com/mattn/go-isatty | v0.0.24 | modernc.org/sqlite | MIT |
| github.com/ncruces/go-strftime | v1.0.0 | modernc.org/sqlite | MIT |
| github.com/remyoudompheng/bigfft | v0.0.0-20230129092748-24d4a6f8daec | modernc.org/sqlite | BSD-3-Clause |
| golang.org/x/sys | v0.47.0 | modernc.org/sqlite | BSD-3-Clause |

`modernc.org/libc` and `modernc.org/memory` contain additional third-party notices in their modules; these are also permissive and must be reproduced in release archives.

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
| file-lock library | cross-process writes | to verify |

## Release checklist

- Every direct and transitive dependency has its license recorded.
- No GPL/AGPL or unknown-license dependency is linked into the distributed binary.
- Generated notices are included in release archives.
- The dependency inventory matches the SBOM attached to the release.
