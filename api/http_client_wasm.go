//go:build wasip1

package api

import (
	"context"
	"errors"
	"unsafe"

	"github.com/glassbox-go/binarybridge"
)

// VirtualHTTPClient is a capability-scoped outbound HTTP client in Go.
type VirtualHTTPClient struct {
}

func NewVirtualHTTPClient() *VirtualHTTPClient {
	return &VirtualHTTPClient{}
}

// Link host capability fetch function
//go:wasmimport gobox_host fetch_http
func fetchHTTP(urlPtr uint32, urlLen uint32) uint64

// Fetch executes a GET request against the target URL by delegating to the host.
func (v *VirtualHTTPClient) Fetch(ctx context.Context, urlString string) (string, error) {
	urlBytes := []byte(urlString)
	if len(urlBytes) == 0 {
		return "", errors.New("empty URL")
	}
	urlPtr := &urlBytes[0]
	urlLen := uint32(len(urlBytes))

	res := fetchHTTP(uint32(uintptr(unsafe.Pointer(urlPtr))), urlLen)
	if res == 0xFFFFFFFFFFFFFFFF {
		return "", errors.New("HTTP fetch failed: host rejected or network error")
	}

	respPtr := uint32(res >> 32)
	respLen := uint32(res & 0xFFFFFFFF)

	respBytes := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(respPtr))), respLen)
	respStr := string(respBytes)
	
	binarybridge.Free((*byte)(unsafe.Pointer(uintptr(respPtr))))
	
	return respStr, nil
}
