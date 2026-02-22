#!/usr/bin/env bash
set -euo pipefail

# makebuild.sh - Production-quality local build of bd
# Runs lint, tests (with race detector), then builds a stripped binary
# matching GoReleaser production flags. Leaves ./bd in project root.

cd "$(dirname "$0")"

if command -v golangci-lint &>/dev/null; then
  echo "==> Linting..."
  golangci-lint run ./...
else
  echo "==> Skipping lint (golangci-lint not installed)"
fi

echo "==> Testing (race detector enabled)..."
go test -race -short ./...

VERSION=$(grep 'Version = ' cmd/bd/version.go | head -1 | sed 's/.*"\(.*\)".*/\1/')
BUILD=$(git rev-parse --short HEAD)
COMMIT=$(git rev-parse HEAD)
BRANCH=$(git rev-parse --abbrev-ref HEAD)

LDFLAGS="-s -w"
LDFLAGS="$LDFLAGS -X main.Version=${VERSION}"
LDFLAGS="$LDFLAGS -X main.Build=${BUILD}"
LDFLAGS="$LDFLAGS -X main.Commit=${COMMIT}"
LDFLAGS="$LDFLAGS -X main.Branch=${BRANCH}"

echo "==> Building bd ${VERSION} (${BRANCH}@${BUILD})..."
CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o ./bd ./cmd/bd

echo "==> Verifying..."
./bd version

echo "==> Done. ./bd is ready."
