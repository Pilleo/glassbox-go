package binarybridge

import (
	"encoding/binary"
	"fmt"
	"math"
	"unsafe"

	"github.com/vmihailenco/msgpack/v5"
)

var isLittleEndian bool

func init() {
	buf := [2]byte{}
	*(*uint16)(unsafe.Pointer(&buf[0])) = uint16(0xABCD)
	isLittleEndian = buf[0] == 0xCD
}

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
	if !isLittleEndian {
		out := make([]byte, len(data)*4)
		for i, f := range data {
			binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(f))
		}
		return out
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&data[0])), len(data)*4)
}

// ZeroCopyBytesToFloat32 casts a raw byte slice back to a float32 slice without allocations if aligned.
func ZeroCopyBytesToFloat32(data []byte) []float32 {
	if len(data) == 0 {
		return nil
	}
	// Check for 4-byte alignment
	aligned := uintptr(unsafe.Pointer(&data[0]))%4 == 0
	if !isLittleEndian || !aligned {
		out := make([]float32, len(data)/4)
		for i := 0; i < len(data)/4; i++ {
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
		}
		return out
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&data[0])), len(data)/4)
}

// UnmarshalError is a helper to consistently unpack error values from Wasm guest responses.
func UnmarshalError(v interface{}) error {
	if v == nil {
		return nil
	}
	if err, ok := v.(error); ok {
		if err.Error() == "" {
			return nil
		}
		return err
	}
	if errStr, ok := v.(string); ok && errStr != "" {
		return fmt.Errorf("%s", errStr)
	}
	if errMap, ok := v.(map[string]interface{}); ok {
		if msg, exists := errMap["Message"]; exists && msg != "" {
			return fmt.Errorf("%v", msg)
		}
	}
	return nil
}
