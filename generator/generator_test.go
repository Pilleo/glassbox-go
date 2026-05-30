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

type MockProcessorImpl struct{}
func (m *MockProcessorImpl) ProcessData(ctx context.Context, input []byte, ptr *int) ([]*byte, error) {
	return nil, nil
}
`
	mockFilePath := filepath.Join(tempDir, "mock_processor.go")
	err = os.WriteFile(mockFilePath, []byte(mockSrc), 0644)
	if err != nil {
		t.Fatalf("Failed to write mock source file: %v", err)
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, tempDir, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse mock source: %v", err)
	}

	// Run AST parser and code generator
	for _, pkg := range pkgs {
		processPackage(pkg, tempDir, false) // Added false for autoBuild
	}

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
		"engine.GetInstance",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(content, expected) {
			t.Errorf("Expected generated proxy content to contain '%s', but it was not found", expected)
		}
	}

	// Assert guest file was generated
	generatedGuestPath := filepath.Join(tempDir, "guest", "main.go")
	if _, err := os.Stat(generatedGuestPath); os.IsNotExist(err) {
		t.Errorf("Expected generated guest file at %s, but it does not exist", generatedGuestPath)
	}

	// Assert generate.go was generated
	generateGoPath := filepath.Join(tempDir, "generate.go")
	if _, err := os.Stat(generateGoPath); os.IsNotExist(err) {
		t.Errorf("Expected generated generate.go file at %s, but it does not exist", generateGoPath)
	}
}

func TestGeneratorContextCollision(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gobox-gen-ctx-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mockSrc := `
package demo

import "context"

//gobox:sandbox
type MockContextCollision interface {
	DoThing(ctx string, myCtx context.Context) error
}

type MockContextCollisionImpl struct{}
func (m *MockContextCollisionImpl) DoThing(ctx string, myCtx context.Context) error {
	return nil
}
`
	mockFilePath := filepath.Join(tempDir, "mock_collision.go")
	err = os.WriteFile(mockFilePath, []byte(mockSrc), 0644)
	if err != nil {
		t.Fatalf("Failed to write mock source file: %v", err)
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, tempDir, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse mock source: %v", err)
	}

	for _, pkg := range pkgs {
		err := processPackage(pkg, tempDir, false)
		if err != nil {
			t.Fatalf("Failed to process package: %v", err)
		}
	}

	generatedProxyPath := filepath.Join(tempDir, "mockcontextcollision_proxy.go")
	bytes, err := os.ReadFile(generatedProxyPath)
	if err != nil {
		t.Fatalf("Failed to read generated proxy file: %v", err)
	}

	content := string(bytes)
	// We want to make sure it selects myCtx for context.Context logic, NOT ctx string
	if !strings.Contains(content, "ctx := myCtx") {
		t.Errorf("Expected proxy to alias 'myCtx' to 'ctx' because it is the context.Context parameter")
	}
	// We also ensure it doesn't erroneously use ctx (the string) for the context.WithTimeout
	if strings.Contains(content, "ctx := ctx") {
		t.Errorf("Proxy erroneously mapped the 'ctx' string parameter to the Context object")
	}
}

