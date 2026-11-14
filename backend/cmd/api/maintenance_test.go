package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// The auth usecase's DeleteAccountByID deletes an account without a password. It is for
// maintenance commands (cmd/seedsample) only: no handler and no wiring of the API server may
// call it, so no route can reach it. This scans every non-test Go file under internal/ and
// cmd/api for a call, wherever its receiver comes from; only the method itself may name it.
func TestNoRouteDeletesAccountsByID(t *testing.T) {
	const method = "DeleteAccountByID"
	scanned := 0
	for _, root := range []string{"../../internal", "."} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			scanned++
			ast.Inspect(file, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == method {
					t.Errorf("%s uses %s; only maintenance commands outside cmd/api may", path, method)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if scanned < 50 {
		t.Fatalf("scanned only %d files; the paths are wrong", scanned)
	}
}
