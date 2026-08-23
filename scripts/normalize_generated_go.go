// Command normalize_generated_go makes Ard-generated Go stable across builds.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var temporaryName = regexp.MustCompile(`^_tmp_[0-9]+$`)
var functionBoundary = regexp.MustCompile(`(?m)}\n(?:[ \t]*\n)*func `)

func declarationKey(declaration ast.Decl) string {
	function := declaration.(*ast.FuncDecl)
	var receiver bytes.Buffer
	if function.Recv != nil {
		_ = format.Node(&receiver, token.NewFileSet(), function.Recv)
	}
	return receiver.String() + "\x00" + function.Name.Name
}

func normalize(path string) error {
	files := token.NewFileSet()
	parsed, err := parser.ParseFile(files, path, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	generated := false
	for _, imported := range parsed.Imports {
		if strings.Contains(imported.Path.Value, "kb/internal/ard") {
			generated = true
			break
		}
	}
	if !generated {
		return nil
	}

	indices := make([]int, 0)
	functions := make([]ast.Decl, 0)
	for index, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name != "init" {
			indices = append(indices, index)
			functions = append(functions, declaration)
		}
	}
	sort.SliceStable(functions, func(left, right int) bool {
		return declarationKey(functions[left]) < declarationKey(functions[right])
	})
	for index, declarationIndex := range indices {
		parsed.Decls[declarationIndex] = functions[index]
	}

	// Rename by resolved object identity after sorting. Choose a prefix absent
	// from the complete file so canonical names cannot capture or shadow an
	// existing identifier.
	usedNames := make(map[string]struct{})
	ast.Inspect(parsed, func(node ast.Node) bool {
		if identifier, ok := node.(*ast.Ident); ok {
			usedNames[identifier.Name] = struct{}{}
		}
		return true
	})
	prefix := "_kb_release_tmp_"
	for {
		collision := false
		for name := range usedNames {
			if strings.HasPrefix(name, prefix) {
				collision = true
				break
			}
		}
		if !collision {
			break
		}
		prefix += "x"
	}

	objects := make(map[*ast.Object]string)
	var unresolved string
	ast.Inspect(parsed, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok || !temporaryName.MatchString(identifier.Name) {
			return true
		}
		if identifier.Obj == nil {
			unresolved = identifier.Name
			return true
		}
		replacement, exists := objects[identifier.Obj]
		if !exists {
			replacement = fmt.Sprintf("%s%d", prefix, len(objects))
			objects[identifier.Obj] = replacement
			identifier.Obj.Name = replacement
		}
		identifier.Name = replacement
		return true
	})
	if unresolved != "" {
		return fmt.Errorf("unresolved generated temporary %q in %s", unresolved, path)
	}

	var output bytes.Buffer
	if err := format.Node(&output, files, parsed); err != nil {
		return err
	}
	canonical := functionBoundary.ReplaceAll(output.Bytes(), []byte("}\n\nfunc "))
	canonical, err = format.Source(canonical)
	if err != nil {
		return err
	}
	return os.WriteFile(path, canonical, 0o644)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: normalize_generated_go DIRECTORY")
		os.Exit(2)
	}
	if err := filepath.WalkDir(os.Args[1], func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".go") {
			return normalize(path)
		}
		return nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
