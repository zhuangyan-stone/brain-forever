#!/bin/bash
# ============================================
# BrainForever Build Script (Linux/macOS)
# Sets CGO, then builds
# ============================================

set -e

# Enable CGO
export CGO_ENABLED=1

echo "=== BrainForever Builder ==="
echo ""

# Tidy dependencies
echo "[1/3] go mod tidy..."
go mod tidy

# Build
echo "[2/3] go build..."
go build -o brain-forever .

echo "[3/3] Build success: brain-forever"
echo ""
