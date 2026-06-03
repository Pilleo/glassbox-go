package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"golang.org/x/tools/imports"
)

func main() {
	dirPath := flag.String("dir", ".", "Directory to scan for sandboxed Go interfaces")
	autoBuild := flag.Bool("build", false, "Automatically compile the Wasm binary after generation")
	flag.Parse()

	absPath, err := filepath.Abs(*dirPath)
	if err != nil {
		fmt.Printf("Failed to resolve absolute path: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[gobox-gen] Scanning directory: %s\n", absPath)
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, absPath, nil, parser.ParseComments)
	if err != nil {
		fmt.Printf("Failed to parse Go files: %v\n", err)
		os.Exit(1)
	}

	for _, pkg := range pkgs {
		if err := processPackage(pkg, absPath, *autoBuild); err != nil {
			fmt.Printf("Error processing package %s: %v\n", pkg.Name, err)
			os.Exit(1)
		}
	}
}

type methodSpec struct {
	Name    string
	Params  []paramSpec
	Results []paramSpec
}

type paramSpec struct {
	Name       string
	Type       string
	IsVariadic bool
}

type interfaceSpec struct {
	Name     string
	ImplName string
	Methods  []methodSpec
}

type TemplateData struct {
	PackageName    string
	WasmModuleName string
	Imports        []string
	Interfaces     []interfaceSpec
	Interface      interfaceSpec
	ModulePath     string
}

func processPackage(pkg *ast.Package, absPath string, autoBuild bool) error {
	var sandboxedInterfaces []interfaceSpec
	packageName := pkg.Name

	importSet := make(map[string]bool)
	var fileImports []string
	var processErr error
	for _, file := range pkg.Files {
		if processErr != nil {
			break
		}
		for _, imp := range file.Imports {
			val := imp.Path.Value
			if imp.Name != nil {
				val = imp.Name.Name + " " + val
			}
			if !importSet[val] {
				importSet[val] = true
				fileImports = append(fileImports, val)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if processErr != nil {
				return false
			}
			decl, ok := n.(*ast.GenDecl)
			if !ok || decl.Tok != token.TYPE {
				return true
			}

			isSandboxed := false
			implName := ""
			if decl.Doc != nil {
				for _, comment := range decl.Doc.List {
					if strings.Contains(comment.Text, "//gobox:sandbox") {
						isSandboxed = true
					}
					if strings.HasPrefix(comment.Text, "//gobox:impl ") {
						implName = strings.TrimSpace(strings.TrimPrefix(comment.Text, "//gobox:impl "))
					}
				}
			}

			if isSandboxed {
				for _, spec := range decl.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					interfaceType, ok := typeSpec.Type.(*ast.InterfaceType)
					if !ok {
						continue
					}
					interfaceName := typeSpec.Name.Name
					
					// AST Validation: Reject embedded interfaces
					valid := true
					for _, field := range interfaceType.Methods.List {
						if len(field.Names) == 0 {
							fmt.Printf("[gobox-gen] ERROR: Interface %s contains an embedded interface. Glassbox currently does not support proxying embedded interfaces.\n", interfaceName)
							valid = false
							break
						}
					}
					if !valid {
						continue
					}
					
					fmt.Printf("[gobox-gen] Found Sandboxed Interface: %s\n", interfaceName)
					
					if implName == "" {
						implName = interfaceName + "Impl"
					}
					
					intfSpec := interfaceSpec{Name: interfaceName, ImplName: implName}
					for _, field := range interfaceType.Methods.List {
						funcType, ok := field.Type.(*ast.FuncType)
						if !ok {
							continue
						}

						methodName := field.Names[0].Name
						var params []paramSpec
						var results []paramSpec

						// Parse parameters
						if funcType.Params != nil {
							for idx, p := range funcType.Params.List {
								isVariadic := false
								if _, ok := p.Type.(*ast.Ellipsis); ok {
									isVariadic = true
								}
								pType, err := typeExprToString(p.Type)
								if err != nil {
									processErr = err
									return false
								}
								if len(p.Names) > 0 {
									for _, name := range p.Names {
										if pType == "string" {
											lname := strings.ToLower(name.Name)
											if strings.Contains(lname, "path") || strings.Contains(lname, "file") || strings.Contains(lname, "dir") {
												fmt.Printf("[gobox-gen] WARNING: Parameter '%s' in %s.%s is a string but looks like a file path. Consider using gapi.SandboxPath to enforce host-side file access checks.\n", name.Name, interfaceName, methodName)
											}
										}
										params = append(params, paramSpec{Name: name.Name, Type: pType, IsVariadic: isVariadic})
									}
								} else {
									params = append(params, paramSpec{Name: fmt.Sprintf("arg%d", idx), Type: pType, IsVariadic: isVariadic})
								}
							}
						}

						// Parse results
						if funcType.Results != nil {
							resultIdx := 0
							for _, r := range funcType.Results.List {
								rType, err := typeExprToString(r.Type)
								if err != nil {
									processErr = err
									return false
								}
								if len(r.Names) > 0 {
									for _, name := range r.Names {
										results = append(results, paramSpec{Name: name.Name, Type: rType})
										resultIdx++
									}
								} else {
									results = append(results, paramSpec{Name: fmt.Sprintf("ret%d", resultIdx), Type: rType})
									resultIdx++
								}
							}
						}

						if len(results) == 0 || results[len(results)-1].Type != "error" {
							fmt.Printf("[gobox-gen] ERROR: Sandboxed interface %s.%s must return 'error' as its last return value.\n", interfaceName, methodName)
							valid = false
							break
						}

						intfSpec.Methods = append(intfSpec.Methods, methodSpec{
							Name:    methodName,
							Params:  params,
							Results: results,
						})
					}
					sandboxedInterfaces = append(sandboxedInterfaces, intfSpec)
				}
			}

			return true
		})
	}

	if processErr != nil {
		return processErr
	}

	if len(sandboxedInterfaces) > 0 {
		for _, intf := range sandboxedInterfaces {
			generateProxy(packageName, intf, absPath, fileImports)
		}
		generateGuest(packageName, sandboxedInterfaces, absPath, fileImports)
		generateGoGenerateFile(packageName, packageName, absPath)

		if autoBuild {
			compileWasm(packageName, absPath)
		}
	}
	return nil
}

var funcMap = template.FuncMap{
	"guestRetType": func(typ string, pkg string) string {
		if strings.HasPrefix(typ, "*") && !strings.HasPrefix(typ, "*"+pkg+".") {
			return "*" + pkg + "." + typ[1:]
		}
		if !strings.HasPrefix(typ, "*") && !strings.Contains(typ, ".") && typ != "error" && typ != "string" && typ != "int" && typ != "int32" && typ != "int64" && typ != "float32" && typ != "float64" && typ != "bool" && typ != "[]byte" && typ != "[]string" && typ != "map[string]interface{}" && !strings.HasPrefix(typ, "map[") && !strings.HasPrefix(typ, "[]") {
            return pkg + "." + typ
        }
		return typ
	},
	"lower": strings.ToLower,
	"joinParams": func(params []paramSpec) string {
		var s []string
		for _, p := range params {
			s = append(s, fmt.Sprintf("%s %s", p.Name, p.Type))
		}
		return strings.Join(s, ", ")
	},
	"joinNamedResults": func(results []paramSpec) string {
		var s []string
		for i, r := range results {
			if i == len(results)-1 {
				s = append(s, "outErr error")
			} else {
				s = append(s, fmt.Sprintf("out%d %s", i, r.Type))
			}
		}
		if len(s) == 0 {
			return ""
		}
		return "(" + strings.Join(s, ", ") + ")"
	},
	"hasContextParam": func(params []paramSpec) bool {
		for _, p := range params {
			if isContextType(p.Type) {
				return true
			}
		}
		return false
	},
	"contextVarName": func(params []paramSpec) string {
		for _, p := range params {
			if isContextType(p.Type) {
				return p.Name
			}
		}
		return "ctx"
	},
	"isSandboxPath": func(t string) bool {
		return t == "gapi.SandboxPath" || t == "SandboxPath"
	},
	"isContextType": isContextType,
	"filterContext": func(params []paramSpec) []paramSpec {
		var out []paramSpec
		for _, p := range params {
			if !isContextType(p.Type) {
				out = append(out, p)
			}
		}
		return out
	},
	"guestArgType": func(p paramSpec) string {
		if p.IsVariadic {
			return "[]" + strings.TrimPrefix(p.Type, "...")
		}
		return p.Type
	},
	"hasErrorResult": func(results []paramSpec) bool {
		for _, r := range results {
			if r.Type == "error" {
				return true
			}
		}
		return false
	},
	"guestCallAssignment": func(results []paramSpec) string {
		var s []string
		for i, r := range results {
			if r.Type == "error" {
				s = append(s, "err")
			} else {
				s = append(s, fmt.Sprintf("ret%d", i))
			}
		}
		if len(s) == 0 {
			return ""
		}
		return strings.Join(s, ", ") + " ="
	},
	"guestCallArgs": func(params []paramSpec) string {
		var s []string
		argIdx := 0
		for _, p := range params {
			if isContextType(p.Type) {
				s = append(s, "context.Background()")
			} else {
				argVal := fmt.Sprintf("args.Arg%d", argIdx)
				if p.Type == "gapi.SandboxPath" || p.Type == "SandboxPath" {
					argVal = fmt.Sprintf("%s(%s)", p.Type, argVal)
				}
				if p.IsVariadic {
					argVal += "..."
				}
				s = append(s, argVal)
				argIdx++
			}
		}
		return strings.Join(s, ", ")
	},
}

func isContextType(t string) bool {
	return t == "context.Context" || t == "Context"
}

const goGenerateTemplateText = `// Code generated by gobox-gen. DO NOT EDIT.
package {{.PackageName}}

// Run 'go generate' in this directory to recompile the sandboxed Wasm binary.
//` + `go:generate env GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o ../wasm/{{.WasmModuleName}}.wasm ./guest
`

func generateGoGenerateFile(packageName, wasmModuleName string, absPath string) {
	tmpl := template.Must(template.New("generate").Parse(goGenerateTemplateText))
	var buf bytes.Buffer
	data := TemplateData{
		PackageName:    packageName,
		WasmModuleName: wasmModuleName,
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		fmt.Printf("Failed to execute go:generate template: %v\n", err)
		return
	}
	outputPath := filepath.Join(absPath, "generate.go")
	writeFormatted(buf.Bytes(), outputPath)
}

const proxyTemplateText = `// Code generated by gobox-gen. DO NOT EDIT.
package {{.PackageName}}

import (
	"context"
	"fmt"
	"github.com/tetratelabs/wazero/api"
	gapi "github.com/glassbox-go/api"
	gbridge "github.com/glassbox-go/binarybridge"
	gruntime "github.com/glassbox-go/runtime"
{{range .Imports}}	{{.}}
{{end}})

type {{.Interface.Name}}WasmProxy struct {
	engine *gruntime.Engine
	limits *gapi.SandboxLimits
}

var _ {{.Interface.Name}} = (*{{.Interface.Name}}WasmProxy)(nil)

func New{{.Interface.Name}}WasmProxy(engine *gruntime.Engine, limits *gapi.SandboxLimits) (*{{.Interface.Name}}WasmProxy, error) {
	if limits == nil {
		limits = gapi.NewBuilder().Build()
	}
	if engine == nil {
		return nil, fmt.Errorf("engine cannot be nil")
	}
	// Eagerly verify module is compilable/instantiable and initialize the pool
	ctx := context.Background()
	mod, err := engine.GetInstance(ctx, "{{.PackageName}}", limits)
	if err != nil {
		return nil, gapi.NewSandboxSecurityError(fmt.Sprintf("Failed to initialize secure Wasm sandbox for {{.PackageName}}: %v", err))
	}
	if mod.ExportedFunction("malloc") == nil {
		mod.Close(ctx)
		return nil, gapi.NewSandboxSecurityError("wasm module does not export malloc")
	}
	if mod.ExportedFunction("free") == nil {
		mod.Close(ctx)
		return nil, gapi.NewSandboxSecurityError("wasm module does not export free")
	}
{{range .Interface.Methods}}	if mod.ExportedFunction("{{.Name}}") == nil {
		mod.Close(ctx)
		return nil, gapi.NewSandboxSecurityError("wasm module does not export {{.Name}}")
	}
{{end}}	mod.Close(ctx)

	p := &{{.Interface.Name}}WasmProxy{
		engine: engine,
		limits: limits,
	}
	return p, nil
}

func (p *{{.Interface.Name}}WasmProxy) Close() error {
	return nil
}

func (p *{{.Interface.Name}}WasmProxy) acquireModule(ctx context.Context) (api.Module, error) {
	// SECURITY TRADEOFF: Glassbox-Go instantiates a fresh guest module on every single invocation.
	// This guarantees that all memory, global state, and allocated buffers are completely wiped cleanly
	// between calls, completely preventing data leakage between sandboxed invocations. While this adds
	// some instantiation overhead compared to pooling persistent stateful instances, the security benefit
	// of perfect isolation makes it the safest choice for sandboxing untrusted code.
	return p.engine.GetInstance(ctx, "{{.PackageName}}", p.limits)
}

func (p *{{.Interface.Name}}WasmProxy) releaseModule(ctx context.Context, mod api.Module, success bool) {
	p.engine.ReleaseInstance(ctx, mod, p.limits, success)
}

{{range .Interface.Methods}}
func (p *{{$.Interface.Name}}WasmProxy) {{.Name}}({{joinParams .Params}}) {{joinNamedResults .Results}} {
	// 1. Establish Context limits boundary
{{if not (hasContextParam .Params)}}	ctx := context.Background()
{{else}}{{if ne (contextVarName .Params) "ctx"}}	ctx := {{contextVarName .Params}}
{{end}}{{end}}	if p.limits.Timeout() > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.limits.Timeout())
		defer cancel()
	}
	// Early exit if context already canceled
	if err := ctx.Err(); err != nil {
		outErr = err
		return
	}
	ctx = gapi.WithActiveLimits(ctx, p.limits)

{{range .Params}}{{if isSandboxPath .Type}}	gate := &gapi.SecurityGate{}
	if err := gate.CheckFileAccess(ctx, string({{.Name}})); err != nil {
		outErr = err
		return
	}

{{end}}{{end}}	var payload []byte
	var err error
	payload, err = gbridge.SerializeAsBytes([]interface{}{
{{range .Params}}{{if not (isContextType .Type)}}		{{.Name}},
{{end}}{{end}}	})
	if err != nil {
		outErr = fmt.Errorf("serialization failed: %w", err)
		return
	}

	// Acquire a clean guest module instance for this isolated call
	mod, err := p.acquireModule(ctx)
	if err != nil {
		outErr = fmt.Errorf("failed to get sandbox instance: %w", err)
		return
	}
	success := false
	defer func() { p.releaseModule(ctx, mod, success) }()

	malloc := mod.ExportedFunction("malloc")
	if malloc == nil {
		outErr = fmt.Errorf("wasm module does not export malloc")
		return
	}
	free := mod.ExportedFunction("free")
	if free == nil {
		outErr = fmt.Errorf("wasm module does not export free")
		return
	}
	wasmFunc := mod.ExportedFunction("{{.Name}}")
	if wasmFunc == nil {
		outErr = fmt.Errorf("wasm module does not export %s", "{{.Name}}")
		return
	}

	results, err := malloc.Call(ctx, uint64(len(payload)))
	if err != nil {
		outErr = fmt.Errorf("failed to allocate guest memory: %w", err)
		return
	}
	guestPtr := uint32(results[0])
	defer func() { _, _ = free.Call(context.Background(), uint64(guestPtr)) }()

	if !mod.Memory().Write(guestPtr, payload) {
		outErr = fmt.Errorf("failed to write payload to guest memory")
		return
	}

	// Execute guest function and release temporary scratch buffer
	callRes, err := wasmFunc.Call(ctx, uint64(guestPtr), uint64(len(payload)))
	if err != nil {
		outErr = fmt.Errorf("sandbox execution failed: %w", err)
		return
	}

	retPtr := uint32(callRes[0] >> 32)
	defer func() { _, _ = free.Call(context.Background(), uint64(retPtr)) }()

	retLen := uint32(callRes[0] & 0xFFFFFFFF)
	retBytes, ok := mod.Memory().Read(retPtr, retLen)
	if !ok {
		outErr = fmt.Errorf("failed to read return payload from guest memory")
		return
	}

{{if len .Results}}	// Use a local struct for direct deserialization to fix struct type assertion bug
	var outResults struct {
		_msgpack struct{} ` + "`msgpack:\",asArray\"`" + `
{{range $i, $r := .Results}}		Ret{{$i}} {{$r.Type}}
{{end}}	}
	err = gbridge.DeserializeFromBytes(retBytes, &outResults)
	if err != nil {
		outErr = fmt.Errorf("deserialization failed: %w", err)
		return
	}

{{range $i, $r := .Results}}{{if eq $r.Type "error"}}	outErr = gbridge.UnmarshalError(outResults.Ret{{$i}})
{{else}}	out{{$i}} = outResults.Ret{{$i}}
{{end}}{{end}}	
	success = true
	return
{{else}}{{end}}}
{{end}}`

func generateProxy(packageName string, intf interfaceSpec, absPath string, fileImports []string) {
	tmpl := template.Must(template.New("proxy").Funcs(funcMap).Parse(proxyTemplateText))
	var buf bytes.Buffer
	data := TemplateData{
		PackageName: packageName,
		Interface:   intf,
		Imports:     fileImports,
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		fmt.Printf("Failed to execute proxy template: %v\n", err)
		return
	}

	outputPath := filepath.Join(absPath, fmt.Sprintf("%s_proxy.go", strings.ToLower(intf.Name)))
	writeFormatted(buf.Bytes(), outputPath)
}

const guestTemplateText = `// Code generated by gobox-gen. DO NOT EDIT.
//go:build wasip1

package main

import (
	"context"
	"fmt"
	"unsafe"
	"github.com/glassbox-go/binarybridge"
{{range .Imports}}	{{.}}
{{end}}	"{{.ModulePath}}"
)

func main() {}

{{range .Interfaces}}
{{$intfName := .Name}}
// --- {{.Name}} ---
var {{lower .Name}}Impl = &{{$.PackageName}}.{{.ImplName}}{}

{{range .Methods}}
//go:wasmexport {{.Name}}
func {{.Name}}(ptr *byte, size uint32) uint64 {
	payload := unsafe.Slice(ptr, size)
	var args struct {
		_msgpack struct{} ` + "`msgpack:\",asArray\"`" + `
{{range $i, $p := (filterContext .Params)}}		Arg{{$i}} {{guestArgType $p}}
{{end}}	}
	errDeserialize := binarybridge.DeserializeFromBytes(payload, &args)

{{range $i, $r := .Results}}{{if eq $r.Type "error"}}	var err error
{{else}}	var ret{{$i}} {{guestRetType $r.Type $.PackageName}}
{{end}}{{end}}	var errOut string

	if errDeserialize != nil {
		errOut = fmt.Sprintf("deserialization failed in guest: %v", errDeserialize)
	} else {
		func() {
			defer func() {
				if r := recover(); r != nil {
					errOut = fmt.Sprintf("panic in wasm guest: %v", r)
				}
			}()
			{{guestCallAssignment .Results}} {{lower $intfName}}Impl.{{.Name}}({{guestCallArgs .Params}})
		}()
	}

{{if hasErrorResult .Results}}	if errOut == "" && err != nil {
		errOut = err.Error()
	}
{{end}}
	retBytes, _ := binarybridge.SerializeAsBytes([]interface{}{
{{range $i, $r := .Results}}{{if eq $r.Type "error"}}		errOut,
{{else}}		ret{{$i}},
{{end}}{{end}}	})

	return binarybridge.KeepAliveAndPack(retBytes)
}
{{end}}{{end}}`

func generateGuest(packageName string, interfaces []interfaceSpec, absPath string, fileImports []string) {
	guestDir := filepath.Join(absPath, "guest")
	if err := os.MkdirAll(guestDir, 0755); err != nil {
		fmt.Printf("Failed to create guest directory: %v\n", err)
		return
	}

	tmpl := template.Must(template.New("guest").Funcs(funcMap).Parse(guestTemplateText))
	var buf bytes.Buffer
	data := TemplateData{
		PackageName: packageName,
		Interfaces:  interfaces,
		Imports:     fileImports,
		ModulePath:  getModulePath(absPath),
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		fmt.Printf("Failed to execute guest template: %v\n", err)
		return
	}

	outputPath := filepath.Join(guestDir, "main.go")
	writeFormatted(buf.Bytes(), outputPath)
}

func compileWasm(packageName string, absPath string) {
	fmt.Printf("[gobox-gen] Automatically compiling Wasm binary for package: %s\n", packageName)
	
	root := absPath
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			break // Reached filesystem root
		}
		root = parent
	}
	
	wasmDir := filepath.Join(root, "wasm")
	if err := os.MkdirAll(wasmDir, 0755); err != nil {
		fmt.Printf("[gobox-gen] ERROR: Failed to create wasm directory: %v\n", err)
		return
	}
	
	outputPath := filepath.Join(wasmDir, packageName+".wasm")
	guestPath := filepath.Join(absPath, "guest")
	
	cmd := exec.Command("go", "build", "-buildmode=c-shared", "-o", outputPath, ".")
	cmd.Dir = guestPath
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("[gobox-gen] ERROR: Wasm compilation failed:\n%s\n", string(output))
	} else {
		fmt.Printf("[gobox-gen] Successfully compiled: %s\n", outputPath)
	}
}

func getModulePath(dir string) string {
	originalDir := dir
	for {
		modPath := filepath.Join(dir, "go.mod")
		if data, err := os.ReadFile(modPath); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "module ") {
					modName := strings.TrimSpace(strings.TrimPrefix(line, "module "))
					rel, _ := filepath.Rel(filepath.Dir(modPath), originalDir)
					if rel == "." {
						return modName
					}
					return filepath.Join(modName, filepath.ToSlash(rel))
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "github.com/glassbox-go/" + filepath.Base(dir)
}

func writeFormatted(src []byte, outputPath string) {
	formatted, err := imports.Process(outputPath, src, nil)
	if err != nil {
		fmt.Printf("[gobox-gen] WARNING: Failed to format %s: %v — writing unformatted\n", outputPath, err)
		formatted = src
	}
	if err := os.WriteFile(outputPath, formatted, 0644); err != nil {
		fmt.Printf("[gobox-gen] ERROR: Failed to write %s: %v\n", outputPath, err)
	} else {
		fmt.Printf("[gobox-gen] Generated: %s\n", outputPath)
	}
}

func typeExprToString(expr ast.Expr) (string, error) {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name, nil
	case *ast.ArrayType:
		e, err := typeExprToString(t.Elt)
		return "[]" + e, err
	case *ast.SelectorExpr:
		x, err := typeExprToString(t.X)
		return x + "." + t.Sel.Name, err
	case *ast.Ellipsis:
		e, err := typeExprToString(t.Elt)
		return "..." + e, err
	case *ast.StarExpr:
		x, err := typeExprToString(t.X)
		return "*" + x, err
	case *ast.MapType:
		k, errK := typeExprToString(t.Key)
		v, errV := typeExprToString(t.Value)
		if errK != nil { return "", errK }
		if errV != nil { return "", errV }
		return "map[" + k + "]" + v, nil
	case *ast.InterfaceType:
		return "interface{}", nil
	case *ast.ChanType:
		return "", fmt.Errorf("[gobox-gen] ERROR: Channel types are not supported for WASM serialization")
	case *ast.FuncType:
		return "", fmt.Errorf("[gobox-gen] ERROR: Function types are not supported for WASM serialization")
	case *ast.StructType:
		return "", fmt.Errorf("[gobox-gen] ERROR: Inline struct types are not supported for WASM serialization")
	case *ast.IndexExpr:
		x, errX := typeExprToString(t.X)
		idx, errIdx := typeExprToString(t.Index)
		if errX != nil { return "", errX }
		if errIdx != nil { return "", errIdx }
		return x + "[" + idx + "]", nil
	case *ast.IndexListExpr:
		x, errX := typeExprToString(t.X)
		if errX != nil { return "", errX }
		var indices []string
		for _, idx := range t.Indices {
			s, err := typeExprToString(idx)
			if err != nil { return "", err }
			indices = append(indices, s)
		}
		return x + "[" + strings.Join(indices, ", ") + "]", nil
	default:
		return fmt.Sprintf("%T", expr), nil
	}
}
