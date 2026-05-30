package main

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"unsafe"

	gbridge "github.com/glassbox-go/binarybridge"
	"github.com/yuin/goldmark"
	"gopkg.in/yaml.v3"
)

func main() {}

// Safe memory allocation registry to protect returned guest buffers from GC sweeps
var allocations = make(map[uintptr][]byte)

//go:wasmexport malloc
func malloc(size uint32) *byte {
	buf := make([]byte, size)
	ptr := &buf[0]
	allocations[uintptr(unsafe.Pointer(ptr))] = buf
	return ptr
}

//go:wasmexport free
func free(ptr *byte) {
	delete(allocations, uintptr(unsafe.Pointer(ptr)))
}

func keepAliveAndPack(buf []byte) uint64 {
	ptr := &buf[0]
	allocations[uintptr(unsafe.Pointer(ptr))] = buf
	return (uint64(uint32(uintptr(unsafe.Pointer(ptr)))) << 32) | uint64(len(buf))
}

//go:wasmexport Parse
func Parse(ptr *byte, size uint32) uint64 {
	payload := unsafe.Slice(ptr, size)

	var args []interface{}
	_ = gbridge.DeserializeFromBytes(payload, &args)

	var data []byte
	if len(args) > 0 {
		if d, ok := args[0].([]byte); ok {
			data = d
		}
	}

	var result map[string]interface{}
	var errOut string

	err := yaml.Unmarshal(data, &result)
	if err != nil {
		errOut = err.Error()
	}

	retBytes, _ := gbridge.SerializeAsBytes([]interface{}{
		result,
		errOut,
	})

	return keepAliveAndPack(retBytes)
}

//go:wasmexport Render
func Render(ptr *byte, size uint32) uint64 {
	payload := unsafe.Slice(ptr, size)

	var args []interface{}
	_ = gbridge.DeserializeFromBytes(payload, &args)

	var markdown []byte
	if len(args) > 0 {
		if m, ok := args[0].([]byte); ok {
			markdown = m
		}
	}

	var html string
	var errOut string

	var buf bytes.Buffer
	err := goldmark.Convert(markdown, &buf)
	if err != nil {
		errOut = err.Error()
	} else {
		html = buf.String()
	}

	retBytes, _ := gbridge.SerializeAsBytes([]interface{}{
		html,
		errOut,
	})

	return keepAliveAndPack(retBytes)
}

// Link host capability fetch function
//go:wasmimport gobox_host fetch_http
func fetchHTTP(urlPtr uint32, urlLen uint32) uint64

func fetch(ctx context.Context, urlString string) (string, error) {
	urlBytes := []byte(urlString)
	if len(urlBytes) == 0 {
		return "", errors.New("empty URL")
	}
	urlPtr := &urlBytes[0]
	urlLen := uint32(len(urlBytes))

	res := fetchHTTP(uint32(uintptr(unsafe.Pointer(urlPtr))), urlLen)
	if res == 0xFFFFFFFFFFFFFFFF {
		return "", errors.New("HTTP fetch failed")
	}

	respPtr := uint32(res >> 32)
	respLen := uint32(res & 0xFFFFFFFF)

	respBytes := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(respPtr))), respLen)
	return string(respBytes), nil
}

//go:wasmexport RenderWithTemplate
func RenderWithTemplate(ptr *byte, size uint32) uint64 {
	payload := unsafe.Slice(ptr, size)

	var args []interface{}
	_ = gbridge.DeserializeFromBytes(payload, &args)

	var markdown []byte
	var templateUrl string
	if len(args) > 0 {
		if m, ok := args[0].([]byte); ok {
			markdown = m
		}
	}
	if len(args) > 1 {
		if t, ok := args[1].(string); ok {
			templateUrl = t
		}
	}

	var html string
	var errOut string

	// Securely fetch styling template via host fetch_http function
	template, err := fetch(context.Background(), templateUrl)
	if err != nil {
		errOut = err.Error()
	} else {
		var buf bytes.Buffer
		err = goldmark.Convert(markdown, &buf)
		if err != nil {
			errOut = err.Error()
		} else {
			html = template + "\n" + buf.String()
		}
	}

	retBytes, _ := gbridge.SerializeAsBytes([]interface{}{
		html,
		errOut,
	})

	return keepAliveAndPack(retBytes)
}

//go:wasmexport ExtractTextFromFile
func ExtractTextFromFile(ptr *byte, size uint32) uint64 {
	payload := unsafe.Slice(ptr, size)

	var args []interface{}
	_ = gbridge.DeserializeFromBytes(payload, &args)

	var path string
	if len(args) > 0 {
		if p, ok := args[0].(string); ok {
			path = p
		}
	}

	var content string
	var errOut string

	// Standard OS ReadFile goes securely through WASI virtual chroot filesystem mount!
	bytes, err := os.ReadFile(path)
	if err != nil {
		errOut = err.Error()
	} else {
		content = string(bytes)
	}

	retBytes, _ := gbridge.SerializeAsBytes([]interface{}{
		content,
		errOut,
	})

	return keepAliveAndPack(retBytes)
}
