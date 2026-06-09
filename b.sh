#!/bin/bash
# ============================================
# BrainForever Build Script (Linux/macOS)
# Sets CGO, builds both local-server and remote-server
# ============================================

set -e

# Enable CGO (required for go-sqlite3)
export CGO_ENABLED=1

echo "=== d2Brain Builder ==="
echo ""

# Tidy dependencies
echo "[1/4] go mod tidy..."
go mod tidy

# Build local-server
echo "[2/4] Building brain-forever (local-server)..."
go build -o brain-forever ./cmd/local-server/

# Build remote-server
echo "[3/4] Building brain-online (remote-server)..."
go build -o brain-online ./cmd/remote-server/

echo "[4/4] Build success!"
echo "  - brain-forever (local-server, serves frontend + API)"
echo "  - brain-online  (remote-server, AI backend stub)"
echo ""
