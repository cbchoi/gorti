//go:build ignore

// Source inventory only: this does not execute or infer service behavior.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		panic("expected federate source directory")
	}
	files, err := filepath.Glob(filepath.Join(os.Args[1], "*.go"))
	if err != nil {
		panic(err)
	}
	rows := []map[string]any{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			panic(err)
		}
		render := func(node any) string {
			var b bytes.Buffer
			if err := format.Node(&b, set, node); err != nil {
				panic(err)
			}
			return b.String()
		}
		add := func(name, kind, contract string, pos token.Pos) {
			rows = append(rows, map[string]any{"file": filepath.Base(path), "name": name, "kind": kind, "contract": contract, "line": set.Position(pos).Line})
		}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if !decl.Name.IsExported() {
					continue
				}
				name := decl.Name.Name
				if decl.Recv != nil {
					name = render(decl.Recv.List[0].Type) + "." + name
				}
				add(name, "go-callable", render(decl.Type), decl.Pos())
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						if spec.Name.IsExported() {
							add(spec.Name.Name, "go-type", render(spec.Type), spec.Pos())
						}
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							if name.IsExported() {
								add(name.Name, "go-value", render(spec), spec.Pos())
							}
						}
					}
				}
			}
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(rows); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
