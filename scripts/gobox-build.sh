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
go run generator/generator.go -dir=examples/markdown
go run generator/generator.go -dir=examples/pdf

# 3. Check for go compiler presence
if ! command -v go &> /dev/null; then
    echo "[WARNING] go compiler was not found on your system PATH."
    echo "          Skipping compilation of guest Go source code to WASI WASM."
    echo "          (Local fallback proxy execution remains fully operational)."
    echo "================================================================="
    exit 0
fi

# 4. Compile guest Go logic into WASI WASM binaries
if [ -d "demo/guest" ]; then
    echo "[gobox-build] Compiling guest Go code into wasm/demo.wasm..."
    GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o wasm/demo.wasm ./demo/guest
    echo "[gobox-build] Successfully compiled Wasm binary: wasm/demo.wasm"
fi

if [ -d "examples/markdown/guest" ]; then
    echo "[gobox-build] Compiling guest Go code into wasm/markdown.wasm..."
    GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o wasm/markdown.wasm ./examples/markdown/guest
    echo "[gobox-build] Successfully compiled Wasm binary: wasm/markdown.wasm"
fi

if [ -d "examples/pdf/guest" ]; then
    echo "[gobox-build] Compiling guest Go code into wasm/pdf.wasm..."
    GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o wasm/pdf.wasm ./examples/pdf/guest
    echo "[gobox-build] Successfully compiled Wasm binary: wasm/pdf.wasm"
fi
echo "[gobox-build] Build automation completed successfully."
echo "================================================================="
