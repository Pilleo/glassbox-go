package main

import (
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTypeExprToString_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		expr     ast.Expr
		expected string
	}{
		{
			name: "multi-dim array",
			expr: &ast.ArrayType{
				Elt: &ast.ArrayType{
					Elt: &ast.Ident{Name: "int"},
				},
			},
			expected: "[][]int",
		},
		{
			name: "nested pointer",
			expr: &ast.StarExpr{
				X: &ast.StarExpr{
					X: &ast.Ident{Name: "string"},
				},
			},
			expected: "**string",
		},
		{
			name: "ellipsis",
			expr: &ast.Ellipsis{
				Elt: &ast.Ident{Name: "float64"},
			},
			expected: "...float64",
		},
		{
			name: "nested map",
			expr: &ast.MapType{
				Key: &ast.Ident{Name: "string"},
				Value: &ast.MapType{
					Key:   &ast.Ident{Name: "int"},
					Value: &ast.Ident{Name: "bool"},
				},
			},
			expected: "map[string]map[int]bool",
		},
		{
			name: "selector expr with pointers",
			expr: &ast.StarExpr{
				X: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "pkg"},
					Sel: &ast.Ident{Name: "Type"},
				},
			},
			expected: "*pkg.Type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := typeExprToString(tt.expr)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestCompileWasm_ErrorHandling(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gobox-gen-compile-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Since guest dir doesn't exist and has no Go files, compileWasm should fail,
	// but it should not crash the generator (it only prints to stdout).
	// We run it just to ensure it doesn't panic.
	
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("compileWasm panicked on invalid input: %v", r)
		}
	}()

	compileWasm("missingpkg", tempDir)
}

func TestGetModulePath_EdgeCases(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gobox-gen-mod-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a mock go.mod in a parent directory
	modContent := "module github.com/test/mockmod\n"
	err = os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(modContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write mock go.mod: %v", err)
	}

	// Create a deeply nested directory
	deepDir := filepath.Join(tempDir, "deeply", "nested", "pkg")
	err = os.MkdirAll(deepDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create nested dir: %v", err)
	}

	// Test resolution from deep directory
	modPath := getModulePath(deepDir)
	if !strings.HasPrefix(modPath, "github.com/test/mockmod") {
		t.Errorf("Expected module path starting with github.com/test/mockmod, got %s", modPath)
	}

	// It should correctly append the nested relative path
	if !strings.HasSuffix(modPath, "deeply/nested/pkg") {
		t.Errorf("Expected module path ending with deeply/nested/pkg, got %s", modPath)
	}
}
