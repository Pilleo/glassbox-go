package binarybridge

import (
	"fmt"
	"reflect"
	"testing"
)

func TestSerializeAndDeserialize(t *testing.T) {
	type TestStruct struct {
		Name  string
		Value int32
	}

	input := TestStruct{Name: "Glassbox", Value: 42}
	data, err := SerializeAsBytes(input)
	if err != nil {
		t.Fatalf("SerializeAsBytes failed: %v", err)
	}

	var output TestStruct
	err = DeserializeFromBytes(data, &output)
	if err != nil {
		t.Fatalf("DeserializeFromBytes failed: %v", err)
	}

	if output.Name != "Glassbox" || output.Value != 42 {
		t.Errorf("Expected matching deserialized output, got %+v", output)
	}
}

func TestZeroCopyFloat32ToBytes(t *testing.T) {
	// Case 1: Empty slice -> nil
	if res := ZeroCopyFloat32ToBytes(nil); res != nil {
		t.Errorf("Expected nil, got %v", res)
	}

	// Case 2: Slice of floats
	input := []float32{1.0, -2.5, 3.14}
	bytes := ZeroCopyFloat32ToBytes(input)
	if len(bytes) != 12 { // 3 * 4 bytes
		t.Errorf("Expected 12 bytes, got %d", len(bytes))
	}

	// Back to float32
	output := ZeroCopyBytesToFloat32(bytes)
	if len(output) != 3 {
		t.Errorf("Expected 3 floats, got %d", len(output))
	}

	if !reflect.DeepEqual(input, output) {
		t.Errorf("Expected equal float slices, input: %v, output: %v", input, output)
	}
}

func TestZeroCopyBytesToFloat32Empty(t *testing.T) {
	if res := ZeroCopyBytesToFloat32(nil); res != nil {
		t.Errorf("Expected nil, got %v", res)
	}
}

func TestUnmarshalError(t *testing.T) {
	if err := UnmarshalError(nil); err != nil {
		t.Errorf("Expected nil, got %v", err)
	}

	errVal := fmt.Errorf("test error interface")
	if err := UnmarshalError(errVal); err == nil || err.Error() != "test error interface" {
		t.Errorf("Expected 'test error interface', got %v", err)
	}

	if err := UnmarshalError("test string error"); err == nil || err.Error() != "test string error" {
		t.Errorf("Expected 'test string error', got %v", err)
	}

	errMap := map[string]interface{}{"Message": "test map error"}
	if err := UnmarshalError(errMap); err == nil || err.Error() != "test map error" {
		t.Errorf("Expected 'test map error', got %v", err)
	}
}

func TestUnalignedBytesToFloat32(t *testing.T) {
	// Allocate a slice of bytes
	data := make([]byte, 16)
	
	// Create an unaligned slice by slicing from index 1 to 13 (12 bytes = 3 floats)
	// index 1 is definitely not 4-byte aligned if the base array is aligned
	unaligned := data[1:13]
	
	// Fill it with some valid float32 bytes
	// 1.0, 2.0, 3.0
	inputFloats := []float32{1.0, 2.0, 3.0}
	for i, f := range inputFloats {
		// Just copy valid bytes into it
		b := ZeroCopyFloat32ToBytes([]float32{f})
		copy(unaligned[i*4:], b)
	}

	// This should not panic
	out := ZeroCopyBytesToFloat32(unaligned)
	if len(out) != 3 {
		t.Fatalf("Expected 3 floats, got %d", len(out))
	}
	for i, f := range inputFloats {
		if out[i] != f {
			t.Errorf("Expected float %f, got %f", f, out[i])
		}
	}
}
