//go:build wasip1

package binarybridge

import "unsafe"

// We use a global map to hold memory allocations alive so the WASM guest's 
// internal Go Garbage Collector does not reclaim them before the host reads them.
var allocations = make(map[uintptr][]byte)

//go:wasmexport malloc
func malloc(size uint32) *byte {
	if size == 0 {
		return nil
	}
	buf := make([]byte, size)
	ptr := &buf[0]
	allocations[uintptr(unsafe.Pointer(ptr))] = buf
	return ptr
}

//go:wasmexport free
func free(ptr *byte) {
	delete(allocations, uintptr(unsafe.Pointer(ptr)))
}

// Free is the Go-accessible wrapper to explicitly release memory that was
// allocated via malloc, preventing guest memory leaks (e.g. for HTTP payloads).
func Free(ptr *byte) {
	free(ptr)
}

// KeepAliveAndPack keeps the buffer alive from GC and packs its pointer 
// and length into a single uint64 value for host return boundaries.
func KeepAliveAndPack(buf []byte) uint64 {
	if len(buf) == 0 {
		return 0
	}
	ptr := &buf[0]
	allocations[uintptr(unsafe.Pointer(ptr))] = buf
	return (uint64(uint32(uintptr(unsafe.Pointer(ptr)))) << 32) | uint64(len(buf))
}
