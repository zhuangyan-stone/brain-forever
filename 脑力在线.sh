#!/bin/bash
# ============================================
# BrainForever Launcher (Linux/macOS)
# Reads .env, sets environment variables,
# then starts brain-forever
# ============================================

set -e

echo "=== 脑力在线 BrainForever Launcher ==="
echo ""

# --------------------------------------------------
# 1. Check if .env exists
# --------------------------------------------------
if [ ! -f ".env" ]; then
    echo "[ERROR] .env file not found! Please create .env first."
    exit 1
fi

# --------------------------------------------------
# 2. Load .env — parse each non-empty, non-comment line
#    and export it as an environment variable.
#    Supports:
#      - Lines with # comments (full-line and inline)
#      - Quoted values (single or double quotes)
#      - Values containing # inside quotes
#      - Trailing whitespace trimming
# --------------------------------------------------
echo "[1/3] Loading environment variables from .env..."

while IFS= read -r _line || [ -n "$_line" ]; do
    # Trim leading whitespace
    _line="${_line#"${_line%%[![:space:]]*}"}"

    # Skip empty lines and comment lines
    if [ -z "$_line" ] || [ "${_line:0:1}" = "#" ]; then
        continue
    fi

    # Extract key (everything before first =)
    _key="${_line%%=*}"
    _rest="${_line#*=}"

    # Skip if no key
    [ -z "$_key" ] && continue

    # Trim trailing whitespace from key
    _key="${_key%"${_key##*[![:space:]]}"}"

    # Handle quoted values
    _val=""
    if [ "${_rest:0:1}" = '"' ] || [ "${_rest:0:1}" = "'" ]; then
        _quote="${_rest:0:1}"
        _rest="${_rest:1}"
        # Find matching closing quote
        _val="${_rest%%"$_quote"*}"
        # Everything after the closing quote — check for inline comment
        _after="${_rest#*"$_quote"}"
        # Trim leading whitespace from _after
        _after="${_after#"${_after%%[![:space:]]*}"}"
        # If there's a # after the closing quote, it's a comment — ignore it
        if [ "${_after:0:1}" = "#" ]; then
            : # comment, do nothing
        fi
    else
        # Unquoted value — strip inline comments (first unquoted #)
        # Since there are no quotes, just take everything before #
        _val="${_rest%%#*}"
        # Trim trailing whitespace
        _val="${_val%"${_val##*[![:space:]]}"}"
    fi

    # Export the variable
    export "$_key=$_val"
    echo "  set $_key=$_val"

done < ".env"

echo ""
echo "[2/3] Environment variables loaded successfully."
echo ""

# --------------------------------------------------
# 3. Determine the binary name
#    On Linux/macOS the built binary is "brain-forever" (no .exe)
# --------------------------------------------------
_binary="brain-forever"
if [ ! -f "$_binary" ]; then
    # Also check for brain-forever.exe (cross-compiled)
    if [ -f "brain-forever.exe" ]; then
        _binary="brain-forever.exe"
    else
        echo "[ERROR] $_binary not found! Please build first with: bash b.sh"
        exit 1
    fi
fi

echo "[3/3] Starting $_binary..."
echo ""
echo "============================================"
echo "  BrainForever is starting..."
echo "  Open http://localhost:8080 in your browser"
echo "  Press Ctrl+C to stop the server"
echo "============================================"
echo ""

exec "./$_binary"
