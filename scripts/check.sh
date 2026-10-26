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
