package localize

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDomainCatalogCoverage(t *testing.T) {
	for _, pkg := range []string{"patch", "profile", "backup", "kernel", "service", "sysproxy", "autostart", "securefs", "mesh", "applications"} {
		files, _ := filepath.Glob("../" + pkg + "/*.go")
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) != 1 {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Text" {
					return true
				}
				id, ok := selector.X.(*ast.Ident)
				if !ok || id.Name != "localize" {
					return true
				}
				literal, ok := call.Args[0].(*ast.BasicLit)
				if !ok {
					return true
				}
				key, _ := strconv.Unquote(literal.Value)
				if _, ok := messages[key]; !ok {
					t.Errorf("%s: missing English translation for %q", path, key)
				}
				return true
			})
		}
	}
}
