#!/usr/bin/env bash
set -euo pipefail

# Build-time automation script for Glassbox-Go.
# Automatically runs gobox-gen AOT code generator and compiles guest Go code into WASI WASM binaries.

echo "================================================================="
echo "               Glassbox-Go Build Automation Tool                 "
echo "================================================================="

# 1. Establish workspace output directories
mkdir -p wasm

# 2. Run AST Proxy Code Generator
echo "[gobox-build] Executing gobox-gen AOT proxy generator (Dual-Side)..."
go run generator/generator.go -dir=demo

# 3. Compile guest Go logic into WASI WASM binary
# Standard Go compilation for WASIP1
echo "[gobox-build] Compiling guest Go code into wasm/demo.wasm..."
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o wasm/demo.wasm ./demo/guest/
echo "[gobox-build] Successfully compiled Wasm binary: wasm/demo.wasm"

echo "[gobox-build] Build automation completed successfully."
echo "================================================================="
