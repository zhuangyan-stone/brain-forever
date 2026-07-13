#!/bin/bash
# ============================================
# BrainForever Build Script (Linux/macOS)
# Builds brain-forever
# ============================================

set -e

echo "=== d2Brain Builder ==="
echo ""

# Ensure bin directory exists
mkdir -p bin

# Tidy dependencies
echo "[1/3] go mod tidy..."
go mod tidy

# Build
echo "[2/3] Building brain-forever..."
go build -o bin/brain-forever ./cmd/server/

echo "[3/3] Build success!"
echo "  - bin/brain-forever"
echo ""
