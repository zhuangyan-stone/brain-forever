#!/bin/bash
# ============================================
# BrainForever Build Script (Linux/macOS)
# Sets CGO, builds both local-server and remote-server
# ============================================

set -e

# Enable CGO (required for go-sqlite3)
export CGO_ENABLED=1

echo "=== BrainForever Builder ==="
echo ""

# Tidy dependencies
echo "[1/5] go mod tidy..."
go mod tidy

# Build local-server
echo "[2/5] Building local-server..."
go build -o local-server ./cmd/local-server/

# Build remote-server
echo "[3/5] Building remote-server..."
go build -o remote-server ./cmd/remote-server/

echo "[4/5] Build success!"
echo "  - local-server  (serves frontend + API)"
echo "  - remote-server (AI backend stub)"
echo ""
