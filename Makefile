.PHONY: build test tidy gen clean build-wasm

# Default task: Tidy, generate, compile Wasm, and run test coverage
build: tidy gen build-wasm test

# Run all unit tests with full statement coverage
test:
	go test -coverpkg=github.com/glassbox-go/api,github.com/glassbox-go/binarybridge,github.com/glassbox-go/runtime,github.com/glassbox-go/demo ./demo ./runtime
	go test -coverpkg=github.com/glassbox-go/api,github.com/glassbox-go/binarybridge,github.com/glassbox-go/runtime ./examples/pdf
	go test -cover ./generator

# Tidy Go module dependencies
tidy:
	go mod tidy

# Run AOT proxy generator (generates both host proxies and guest shims)
gen:
	go run generator/generator.go -dir=demo
	go run generator/generator.go -dir=examples/markdown
	go run generator/generator.go -dir=examples/pdf

# Compile guest Go logic into WASIP1 WASM binaries
build-wasm:
	mkdir -p wasm
	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o wasm/demo.wasm ./demo/guest
	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o wasm/markdown.wasm ./examples/markdown/guest
	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o wasm/pdf.wasm ./examples/pdf/guest

# Run the complete build-time automation script
build-all:
	./scripts/gobox-build.sh

# Clean generated proxy files and compiled Wasm binaries
clean:
	rm -f demo/*_proxy.go
	rm -rf demo/guest/
	rm -f examples/markdown/*_proxy.go
	rm -rf examples/markdown/guest/
	rm -f examples/pdf/*_proxy.go
	rm -rf examples/pdf/guest/
	rm -rf wasm/
