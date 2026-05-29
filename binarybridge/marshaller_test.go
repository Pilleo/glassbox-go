package binarybridge

import (
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
