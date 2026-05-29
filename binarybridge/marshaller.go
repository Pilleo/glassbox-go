package binarybridge

import (
	"unsafe"

	"github.com/vmihailenco/msgpack/v5"
)

// SerializeAsBytes converts any Go structure/value into MessagePack binary bytes.
func SerializeAsBytes(v interface{}) ([]byte, error) {
	return msgpack.Marshal(v)
}

// DeserializeFromBytes unpacks MessagePack binary bytes back into a target Go pointer.
func DeserializeFromBytes(data []byte, v interface{}) error {
	return msgpack.Unmarshal(data, v)
}

// ZeroCopyFloat32ToBytes casts a float32 slice directly to a raw byte slice without allocations.
func ZeroCopyFloat32ToBytes(data []float32) []byte {
	if len(data) == 0 {
		return nil
	}
	// Direct pointer arithmetic view
	return unsafe.Slice((*byte)(unsafe.Pointer(&data[0])), len(data)*4)
}

// ZeroCopyBytesToFloat32 casts a raw byte slice back to a float32 slice without allocations.
func ZeroCopyBytesToFloat32(data []byte) []float32 {
	if len(data) == 0 {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&data[0])), len(data)/4)
}
