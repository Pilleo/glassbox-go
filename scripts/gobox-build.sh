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
echo "[gobox-build] Executing gobox-gen AOT proxy generator..."
go run generator/generator.go -dir=demo

# 3. Check for TinyGo compiler presence
if ! command -v tinygo &> /dev/null; then
    echo "[WARNING] tinygo compiler was not found on your system PATH."
    echo "          Skipping compilation of guest Go source code to WASI WASM."
    echo "          (Local fallback proxy execution remains fully operational)."
    echo "================================================================="
    exit 0
fi

# 4. Compile guest Go logic into WASI WASM binary
# Assuming guest entry point resides in demo/guest/main.go
if [ -f "demo/guest/main.go" ]; then
    echo "[gobox-build] Compiling guest Go code via TinyGo into wasm/MLProcessor.wasm..."
    tinygo build -o wasm/MLProcessor.wasm -target=wasi demo/guest/main.go
    echo "[gobox-build] Successfully compiled Wasm binary: wasm/MLProcessor.wasm"
else
    echo "[gobox-build] No guest entry point found at demo/guest/main.go. Skipping TinyGo compilation."
fi

echo "[gobox-build] Build automation completed successfully."
echo "================================================================="
