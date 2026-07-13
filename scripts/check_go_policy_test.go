package main

import (
	"go/ast"
	"testing"
)

func TestExportedNames(t *testing.T) {
	spec := &ast.ValueSpec{Names: []*ast.Ident{{Name: "Exported"}, {Name: "private"}}}
	names := exportedNames(spec)
	if len(names) != 1 || names[0] != "Exported" {
		t.Fatalf("unexpected exported names: %v", names)
	}
}
