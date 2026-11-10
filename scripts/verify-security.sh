#!/usr/bin/env bash
# Run in a disposable development/CI checkout with network access and Go 1.26.2+.
# No running application, production database, or deployment is used here.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
GO_BIN="${GO_BIN:-go}"
"$GO_BIN" mod download
"$GO_BIN" mod verify
"$GO_BIN" test -mod=readonly -count=1 ./...
"$GO_BIN" vet -mod=readonly ./...
build_dir="$(mktemp -d)"
trap 'rm -rf "$build_dir"' EXIT
"$GO_BIN" build -mod=readonly -o "$build_dir/api" ./cmd/api
"$GO_BIN" build -mod=readonly -o "$build_dir/seed-admin" ./cmd/seed-admin
"$GO_BIN" build -mod=readonly -o "$build_dir/rotate-2fa-key" ./cmd/rotate-2fa-key
if command -v gcc >/dev/null 2>&1; then
  CGO_ENABLED=1 "$GO_BIN" test -mod=readonly -race -count=1 ./handlers ./utils ./realtime ./routes ./repositories
else
  printf '%s\n' 'Race checks were not run: a C compiler is required.' >&2
  exit 1
fi
