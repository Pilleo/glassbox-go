.PHONY: build test tidy gen clean build-wasm

# Default task: Tidy, generate, compile Wasm, and run test coverage
build: tidy gen build-wasm test

# Run all unit tests with full statement coverage
test:
	go test -coverpkg=github.com/glassbox-go/api,github.com/glassbox-go/binarybridge,github.com/glassbox-go/runtime,github.com/glassbox-go/demo ./demo ./runtime
	go test -cover ./generator

# Tidy Go module dependencies
tidy:
	go mod tidy

# Run AOT proxy generator (generates both host proxies and guest shim)
gen:
	go run generator/generator.go -dir=demo

# Compile guest Go logic into WASIP1 WASM binary (per package)
build-wasm:
	mkdir -p wasm
	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o wasm/demo.wasm ./demo/guest/

# Run the complete build-time automation script
build-all: build-wasm
	./scripts/gobox-build.sh

# Clean generated proxy files and compiled Wasm binaries
clean:
	rm -f demo/*_proxy.go
	rm -rf demo/guest/
	rm -rf wasm/
