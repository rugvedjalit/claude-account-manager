#!/usr/bin/env bash
# Cross-compiles claude-account for every supported platform into dist/.
# Needs Go 1.22+ (https://go.dev/dl). No other dependencies: the tool uses only the Go standard library.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/go"
for target in windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os="${target%/*}"; arch="${target#*/}"; ext=""
  [[ "$os" == windows ]] && ext=".exe"
  out="../dist/$os-$arch/claude-account$ext"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$out" .
  echo "built $out"
done
