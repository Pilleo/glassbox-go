package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratorUnsupportedTypes(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gobox-gen-types-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mockSrc := `
package demo

//gobox:sandbox
type BadInterface interface {
	DoChannel(ch chan int) error
}
`
	mockFilePath := filepath.Join(tempDir, "bad_interface.go")
	err = os.WriteFile(mockFilePath, []byte(mockSrc), 0644)
	if err != nil {
		t.Fatalf("Failed to write mock source file: %v", err)
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, tempDir, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse mock source: %v", err)
	}

	var errs []error
	for _, pkg := range pkgs {
		err := processPackage(pkg, tempDir, false)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) == 0 {
		t.Errorf("Expected error when processing unsupported chan type, but got success")
	} else if !strings.Contains(errs[0].Error(), "Channel types are not supported") {
		t.Errorf("Expected error about channel types, got: %v", errs[0])
	}
}

func TestGeneratorUnsupportedFuncType(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gobox-gen-types-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mockSrc := `
package demo

//gobox:sandbox
type BadInterface interface {
	DoFunc(cb func()) error
}
`
	mockFilePath := filepath.Join(tempDir, "bad_interface.go")
	err = os.WriteFile(mockFilePath, []byte(mockSrc), 0644)
	if err != nil {
		t.Fatalf("Failed to write mock source file: %v", err)
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, tempDir, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse mock source: %v", err)
	}

	var errs []error
	for _, pkg := range pkgs {
		err := processPackage(pkg, tempDir, false)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) == 0 {
		t.Errorf("Expected error when processing unsupported func type, but got success")
	} else if !strings.Contains(errs[0].Error(), "Function types are not supported") {
		t.Errorf("Expected error about function types, got: %v", errs[0])
	}
}

func TestGeneratorGenericTypes(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gobox-gen-generic-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mockSrc := `
package demo

type List[T any] []T
type Result[K any, V any] map[K]V

//gobox:sandbox
type GenericInterface interface {
	ProcessData(input map[string]List[int]) (Result[string, int], error)
}
`
	mockFilePath := filepath.Join(tempDir, "generic_interface.go")
	err = os.WriteFile(mockFilePath, []byte(mockSrc), 0644)
	if err != nil {
		t.Fatalf("Failed to write mock source file: %v", err)
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, tempDir, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse mock source: %v", err)
	}

	var errs []error
	for _, pkg := range pkgs {
		err := processPackage(pkg, tempDir, false)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		t.Fatalf("Expected proxy generation for generic types to succeed, but got error: %v", errs[0])
	}

	generatedProxyPath := filepath.Join(tempDir, "genericinterface_proxy.go")
	bytes, err := os.ReadFile(generatedProxyPath)
	if err != nil {
		t.Fatalf("Failed to read generated proxy file: %v", err)
	}

	content := string(bytes)
	// Make sure the generic type was correctly translated to string format in the generated proxy parameters
	if !strings.Contains(content, "input map[string]List[int]") {
		t.Errorf("Expected generated proxy to correctly contain generic type struct parameter, got:\n%s", content)
	}
	if !strings.Contains(content, "Result[string, int]") {
		t.Errorf("Expected generated proxy to correctly contain generic type result parameter, got:\n%s", content)
	}
}


