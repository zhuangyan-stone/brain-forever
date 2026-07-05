#!/bin/bash
# ============================================
# BrainForever Build Script (Linux/macOS)
# Sets CGO, builds brain-forever
# ============================================

set -e

# Enable CGO (required for go-sqlite3)
export CGO_ENABLED=1

echo "=== d2Brain Builder ==="
echo ""

# Tidy dependencies
echo "[1/3] go mod tidy..."
go mod tidy

# Build
echo "[2/3] Building brain-forever..."
go build -o brain-forever ./cmd/server/

echo "[3/3] Build success!"
echo "  - brain-forever"
echo ""
