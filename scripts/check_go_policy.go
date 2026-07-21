// Command check-go-policy enforces reviewable Go source and package comments.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	files, packageDirectories, err := discover(".")
	if err != nil {
		fail(err.Error())
	}
	violations := 0
	for _, file := range files {
		violations += checkFile(file)
	}
	for directory := range packageDirectories {
		if _, err := os.Stat(filepath.Join(directory, "doc.go")); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR [go-policy] %s has no doc.go overview\n", directory)
			violations++
		}
	}
	if violations > 0 {
		os.Exit(1)
	}
	fmt.Println("Go documentation and function-size policy passed")
}

func discover(root string) ([]string, map[string]struct{}, error) {
	var files []string
	directories := make(map[string]struct{})
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && skippedDirectory(path) {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || generated(path) {
			return nil
		}
		files = append(files, path)
		directories[filepath.Dir(path)] = struct{}{}
		return nil
	})
	return files, directories, err
}

func checkFile(path string) int {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, path, nil, parser.ParseComments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR [go-policy] cannot parse %s\n", path)
		return 1
	}
	violations := 0
	violations += checkAPDLeaks(path, file, set)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok {
			lines := set.Position(function.End()).Line - set.Position(function.Pos()).Line + 1
			if lines > 50 {
				fmt.Fprintf(os.Stderr, "ERROR [go-policy] %s:%d function %s has %d lines\n", path, set.Position(function.Pos()).Line, function.Name.Name, lines)
				violations++
			}
			if !strings.HasSuffix(path, "_test.go") && function.Name.IsExported() && function.Doc == nil {
				fmt.Fprintf(os.Stderr, "ERROR [go-policy] %s:%d exported function %s lacks documentation\n", path, set.Position(function.Pos()).Line, function.Name.Name)
				violations++
			}
			continue
		}
		general, ok := declaration.(*ast.GenDecl)
		if ok && !strings.HasSuffix(path, "_test.go") {
			violations += checkGeneral(path, set, general)
		}
	}
	return violations
}

func checkAPDLeaks(path string, file *ast.File, set *token.FileSet) int {
	aliases := apdAliases(file)
	if len(aliases) == 0 {
		return 0
	}
	violations := 0
	for _, declaration := range file.Decls {
		if leaked := leakedExport(declaration, aliases); leaked != nil {
			fmt.Fprintf(os.Stderr, "ERROR [go-policy] %s:%d exported API leaks cockroachdb/apd\n", path, set.Position(leaked.Pos()).Line)
			violations++
		}
	}
	return violations
}

func apdAliases(file *ast.File) map[string]struct{} {
	aliases := make(map[string]struct{})
	for _, imported := range file.Imports {
		if strings.Trim(imported.Path.Value, `"`) != "github.com/cockroachdb/apd/v3" {
			continue
		}
		name := "apd"
		if imported.Name != nil {
			name = imported.Name.Name
		}
		aliases[name] = struct{}{}
	}
	return aliases
}

func leakedExport(declaration ast.Decl, aliases map[string]struct{}) ast.Node {
	switch value := declaration.(type) {
	case *ast.FuncDecl:
		if value.Name.IsExported() && containsAPD(value.Type, aliases) {
			return value
		}
	case *ast.GenDecl:
		for _, specification := range value.Specs {
			if exportedSpecificationLeaks(specification, aliases) {
				return specification
			}
		}
	}
	return nil
}

func exportedSpecificationLeaks(specification ast.Spec, aliases map[string]struct{}) bool {
	switch value := specification.(type) {
	case *ast.TypeSpec:
		return value.Name.IsExported() && containsAPD(value.Type, aliases)
	case *ast.ValueSpec:
		return len(exportedNames(value)) > 0 && containsAPD(value.Type, aliases)
	default:
		return false
	}
}

func containsAPD(node ast.Node, aliases map[string]struct{}) bool {
	if node == nil {
		return false
	}
	found := false
	ast.Inspect(node, func(current ast.Node) bool {
		selector, ok := current.(*ast.SelectorExpr)
		identifier, identifierOK := selectorX(selector, ok)
		if identifierOK {
			_, found = aliases[identifier.Name]
		}
		return !found
	})
	return found
}

func selectorX(selector *ast.SelectorExpr, ok bool) (*ast.Ident, bool) {
	if !ok {
		return nil, false
	}
	identifier, identifierOK := selector.X.(*ast.Ident)
	return identifier, identifierOK
}

func checkGeneral(path string, set *token.FileSet, declaration *ast.GenDecl) int {
	violations := 0
	for _, spec := range declaration.Specs {
		for _, name := range exportedNames(spec) {
			if declaration.Doc == nil {
				fmt.Fprintf(os.Stderr, "ERROR [go-policy] %s:%d exported identifier %s lacks documentation\n", path, set.Position(spec.Pos()).Line, name)
				violations++
			}
		}
	}
	return violations
}

func exportedNames(spec ast.Spec) []string {
	switch value := spec.(type) {
	case *ast.TypeSpec:
		if value.Name.IsExported() {
			return []string{value.Name.Name}
		}
	case *ast.ValueSpec:
		var names []string
		for _, name := range value.Names {
			if name.IsExported() {
				names = append(names, name.Name)
			}
		}
		return names
	}
	return nil
}

func skippedDirectory(path string) bool {
	base := filepath.Base(path)
	return base == ".git" || base == "node_modules" || base == "dist" || base == ".local" || base == ".secrets"
}

func generated(path string) bool {
	return strings.Contains(path, string(filepath.Separator)+"generated"+string(filepath.Separator)) || strings.HasSuffix(path, ".gen.go")
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, "ERROR [go-policy] "+message)
	os.Exit(1)
}
