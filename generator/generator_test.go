package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratorProxyGeneration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gobox-gen-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mockSrc := `
package demo

import "context"

//gobox:sandbox
type MockProcessor interface {
	ProcessData(ctx context.Context, input []byte, ptr *int) ([]*byte, error)
}
`
	mockFilePath := filepath.Join(tempDir, "mock_processor.go")
	err = os.WriteFile(mockFilePath, []byte(mockSrc), 0644)
	if err != nil {
		t.Fatalf("Failed to write mock source file: %v", err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, mockFilePath, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse mock source: %v", err)
	}

	// Run AST parser and code generator
	processFile(mockFilePath, file, tempDir)

	// Assert proxy file was successfully generated
	generatedProxyPath := filepath.Join(tempDir, "mockprocessor_proxy.go")
	if _, err := os.Stat(generatedProxyPath); os.IsNotExist(err) {
		t.Errorf("Expected generated proxy file at %s, but it does not exist", generatedProxyPath)
	}

	bytes, err := os.ReadFile(generatedProxyPath)
	if err != nil {
		t.Fatalf("Failed to read generated proxy file: %v", err)
	}

	content := string(bytes)
	expectedStrings := []string{
		"package demo",
		"type MockProcessorWasmProxy struct",
		"func NewMockProcessorWasmProxy",
		"func (p *MockProcessorWasmProxy) ProcessData",
		"gapi.WithActiveLimits",
		"gruntime.GetInstance",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(content, expected) {
			t.Errorf("Expected generated proxy content to contain '%s', but it was not found", expected)
		}
	}
}
