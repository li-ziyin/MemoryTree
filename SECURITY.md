# Security Policy

## Status

MemoryTree is pre-release software. There is no supported production version yet. Security reports are still welcome and will be handled before any public release.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability.

Use a private GitHub security advisory on the project repository once it is published. Until then, contact the maintainer privately through the channel listed in the repository profile.

Please include:

- affected version or commit;
- a minimal reproduction;
- impact and plausible attack path;
- whether user memory data, the data repository, or the host machine is exposed;
- any suggested mitigation.

Do not include real secrets or personal memories in the report. Redact them.

## Threat model (summary)

MemoryTree reads and writes a local directory of Markdown memories. Relevant threats include:

- **Untrusted memory content**: captured conversations or documents may contain prompt injection, malicious links, or instructions. Memory content is always treated as data, never executed.
- **Local data exposure**: the data repository may contain private information. File permissions, backups, git remotes, and cloud-synced folders are part of the user's security boundary.
- **Path traversal and symlinks**: a malicious repository could try to make the tool read or write outside its root.
- **Parser attacks**: oversized or malicious YAML/frontmatter.
- **Supply chain**: compromised dependencies or release artifacts.
- **MCP client behavior**: a client may misuse retrieved content or pass untrusted input to write tools.

## Security design commitments

- Local-first: the core process does not make network requests and does not collect telemetry.
- V1 MCP transport is stdio only; no listening port.
- All file operations are confined to the configured data repository root; symlinked format files are rejected.
- YAML is parsed with a restricted parser; custom tags, anchors, and aliases are rejected where feasible; input size is bounded.
- Secrets are scanned and redacted before storage where feasible. Redaction is best-effort, not a guarantee.
- Retrieved memory is labeled as untrusted historical data. The tool itself never executes content.
- Exports filter by `sensitivity`; `private` content is excluded by default.
- Releases are built from CI, with checksums, signatures, and an SBOM.

## Out of scope

- Security of third-party MCP clients.
- Confidentiality of a data repository that the user chooses to sync to a public or untrusted remote.
- Full disk encryption or OS-level access control.
- Malicious users with the same OS account and filesystem access.
- Prompt injection that occurs entirely inside a client after the client ignores the untrusted-data labeling.

## Disclosure

We aim to acknowledge reports within 7 days and to provide a remediation plan within 30 days for confirmed issues. Please allow time for a fix before public disclosure.
