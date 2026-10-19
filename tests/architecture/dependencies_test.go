package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestHLACoreHasNoInfrastructureImports(t *testing.T) {
	root := filepath.Join(repositoryRoot(t), "rti", "internal", "hla")
	forbidden := []string{
		"github.com/cbchoi/gorti/rti/internal/adapters",
		"google.golang.org/grpc",
		"google.golang.org/protobuf",
		"github.com/prometheus",
		"os",
		"path/filepath",
		"net/http",
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			return nil
		}
		for _, spec := range parsed.Imports {
			value, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Errorf("unquote import in %s: %v", path, err)
				continue
			}
			for _, prefix := range forbidden {
				if value == prefix || strings.HasPrefix(value, prefix+"/") {
					t.Errorf("HLA package imports infrastructure: %s imports %s", filepath.ToSlash(path), value)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProductionCodeHasNoTestOnlyAPI(t *testing.T) {
	root := filepath.Join(repositoryRoot(t), "rti", "internal")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		name := strings.ToLower(entry.Name())
		for _, marker := range []string{"fake", "fixture", "benchmark"} {
			if strings.Contains(name, marker) {
				t.Errorf("test-only production file: %s", filepath.ToSlash(path))
			}
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			return nil
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch declaration := node.(type) {
			case *ast.FuncDecl:
				if declaration.Name.IsExported() && strings.HasSuffix(declaration.Name.Name, "ForTest") {
					t.Errorf("test-only exported function %s in %s", declaration.Name.Name, filepath.ToSlash(path))
				}
			case *ast.TypeSpec:
				if declaration.Name.IsExported() && strings.Contains(declaration.Name.Name, "Fake") {
					t.Errorf("test fake type %s in %s", declaration.Name.Name, filepath.ToSlash(path))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPublicPackagesRespectClientBoundary(t *testing.T) {
	root := filepath.Join(repositoryRoot(t), "rti", "pkg")
	const internal = "github.com/cbchoi/gorti/rti/internal/"
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		client := strings.HasPrefix(filepath.ToSlash(relative), "federate/")
		for _, spec := range parsed.Imports {
			value, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(value, internal) {
				continue
			}
			protocol := value == internal+"adapters/protobuf" || strings.HasPrefix(value, internal+"adapters/protobuf/")
			profile := value == internal+"adapters/delivery/callbackprofile"
			if !client || (!protocol && !profile) {
				t.Errorf("public package imports server implementation: %s imports %s", relative, value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
