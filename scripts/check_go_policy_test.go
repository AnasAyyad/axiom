package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestExportedNames(t *testing.T) {
	spec := &ast.ValueSpec{Names: []*ast.Ident{{Name: "Exported"}, {Name: "private"}}}
	names := exportedNames(spec)
	if len(names) != 1 || names[0] != "Exported" {
		t.Fatalf("unexpected exported names: %v", names)
	}
}

func TestAPDLeakDetection(t *testing.T) {
	source := `package sample
import decimal "github.com/cockroachdb/apd/v3"
func Exported() decimal.Decimal { return decimal.Decimal{} }
func private() decimal.Decimal { return decimal.Decimal{} }
`
	file, err := parser.ParseFile(token.NewFileSet(), "sample.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	aliases := apdAliases(file)
	if len(aliases) != 1 || leakedExport(file.Decls[1], aliases) == nil {
		t.Fatal("exported apd type was not detected")
	}
	if leakedExport(file.Decls[2], aliases) != nil {
		t.Fatal("private apd implementation was reported as an API leak")
	}
}
