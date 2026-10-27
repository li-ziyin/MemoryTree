#!/usr/bin/env bash
# Single quality gate: identical checks locally (Git Bash) and in CI.
# golangci-lint and govulncheck also run here when installed locally
# (go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest,
# go install golang.org/x/vuln/cmd/govulncheck@latest); CI always installs
# them, so a green local run matches a green CI run.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== gofmt =="
unformatted=$(gofmt -l cmd internal scripts)
if [ -n "$unformatted" ]; then
  echo "gofmt needed on: $unformatted" >&2
  exit 1
fi

echo "== go vet =="
go vet ./...

echo "== go test =="
go test ./...

# tool_bin prints the path of a Go tool binary, falling back to GOPATH/bin
# (on Windows that directory is usually not on PATH).
tool_bin() {
  local name="$1" bin candidate
  if bin=$(command -v "$name" 2>/dev/null); then
    printf '%s' "$bin"
    return 0
  fi
  for candidate in "$(go env GOPATH)/bin/$name" "$(go env GOPATH)/bin/$name.exe"; do
    if [ -f "$candidate" ]; then
      printf '%s' "$candidate"
      return 0
    fi
  done
  return 1
}

if lint=$(tool_bin golangci-lint); then
  echo "== golangci-lint =="
  "$lint" run
else
  echo "== golangci-lint: skipped (not installed) =="
fi

if vuln=$(tool_bin govulncheck); then
  echo "== govulncheck =="
  "$vuln" ./...
else
  echo "== govulncheck: skipped (not installed) =="
fi

echo "== build with version stamping =="
version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
commit=$(git rev-parse --short HEAD 2>/dev/null || echo none)
built=$(date -u +%Y-%m-%dT%H:%M:%SZ)
go build -trimpath \
  -ldflags "-X main.version=$version -X main.commit=$commit -X main.date=$built" \
  -o bin/memoryd-check ./cmd/memoryd
./bin/memoryd-check version
rm -f bin/memoryd-check
