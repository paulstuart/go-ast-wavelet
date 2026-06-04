package astwavelet

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// FuncIndex maps declaration keys to their AST nodes.
//
// Indexing conventions:
//   - Plain function in the entry package:  "FuncName"
//   - Plain function in another package:    "pkgname.FuncName" (and "FuncName" as fallback)
//   - Method (any package):                 "TypeName.MethodName"
//
// This lets the resolver handle all three call forms in Go:
//   - localFunc()          → Ident → direct lookup by "FuncName"
//   - pkg.Func()           → SelectorExpr → scan for suffix ".Func"
//   - receiver.Method()    → SelectorExpr → scan for suffix ".Method"
type FuncIndex map[string]*ast.FuncDecl

// resolve returns all FuncDecls that could be the target of a call expression.
//
// For Ident calls (local functions), returns the single exact match.
// For SelectorExpr calls (pkg.Func or receiver.Method), returns all index
// entries whose key ends in ".Sel" — intentionally over-approximating since
// we have no type information. This is the seam a full type-aware
// implementation would replace with a single O(1) lookup per call site.
func (idx FuncIndex) resolve(expr ast.Expr) []*ast.FuncDecl {
	switch v := expr.(type) {
	case *ast.Ident:
		if fd, ok := idx[v.Name]; ok {
			return []*ast.FuncDecl{fd}
		}
	case *ast.SelectorExpr:
		suffix := "." + v.Sel.Name
		var matches []*ast.FuncDecl
		for key, fd := range idx {
			if strings.HasSuffix(key, suffix) {
				matches = append(matches, fd)
			}
		}
		return matches
	}
	return nil
}

// LoadPackage loads the Go package at dir and all same-module packages it
// transitively imports, returning a unified file set, AST files, and index.
//
// Same-module packages are identified by sharing the root module path.
// Standard library and external dependencies are excluded — they have no
// local ASTs we can walk anyway.
func LoadPackage(dir string) (*token.FileSet, []*ast.File, FuncIndex, error) {
	fset := token.NewFileSet()

	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedSyntax |
			packages.NeedImports |
			packages.NeedDeps |
			packages.NeedModule,
		Fset: fset,
		Dir:  dir,
	}

	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load %s: %w", dir, err)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		return nil, nil, nil, fmt.Errorf("%d package error(s)", n)
	}

	// Determine the module path so we can filter to same-module packages only.
	var modulePath string
	if len(pkgs) > 0 && pkgs[0].Module != nil {
		modulePath = pkgs[0].Module.Path
	}

	seen := make(map[string]bool)
	var allFiles []*ast.File
	idx := make(FuncIndex)

	var walk func(pkg *packages.Package, isEntry bool)
	walk = func(pkg *packages.Package, isEntry bool) {
		if seen[pkg.ID] {
			return
		}
		seen[pkg.ID] = true

		// Skip stdlib and external deps; they have no local AST to walk.
		if modulePath != "" {
			if pkg.Module == nil || pkg.Module.Path != modulePath {
				return
			}
		}

		allFiles = append(allFiles, pkg.Syntax...)
		addToIndex(idx, pkg, isEntry)

		for _, imp := range pkg.Imports {
			walk(imp, false)
		}
	}

	for _, pkg := range pkgs {
		walk(pkg, true)
	}

	return fset, allFiles, idx, nil
}

// addToIndex adds all function and method declarations from pkg to idx.
func addToIndex(idx FuncIndex, pkg *packages.Package, isEntry bool) {
	for _, f := range pkg.Syntax {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}

			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				// Method: "TypeName.MethodName" (package-independent — calls
				// always go through a receiver, so the selector is the method name)
				if typeName := receiverType(fd.Recv.List[0].Type); typeName != "" {
					idx[typeName+"."+fd.Name.Name] = fd
				}
			} else {
				// Plain function: always index by bare name for local calls.
				idx[fd.Name.Name] = fd
				// Non-entry packages are called as pkg.Func() from outside,
				// so also index as "pkgname.FuncName" for cross-package resolution.
				if !isEntry {
					idx[pkg.Name+"."+fd.Name.Name] = fd
				}
			}
		}
	}
}

func receiverType(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.StarExpr:
		return receiverType(v.X)
	case *ast.Ident:
		return v.Name
	}
	return ""
}

// LoadDir parses all .go files in a single directory. Use LoadPackage for
// programs that span multiple packages.
func LoadDir(dir string) (*token.FileSet, []*ast.File, FuncIndex, error) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, nil, err
	}

	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			return nil, nil, nil, err
		}
		files = append(files, f)
	}

	return fset, files, BuildFuncIndex(files), nil
}

// BuildFuncIndex indexes all function and method declarations across a set of
// files. All functions are keyed by bare name; methods by "TypeName.Method".
// Use LoadPackage (and its addToIndex) for multi-package programs where
// cross-package indexing is needed.
func BuildFuncIndex(files []*ast.File) FuncIndex {
	idx := make(FuncIndex)
	for _, f := range files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			key := fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				if typeName := receiverType(fd.Recv.List[0].Type); typeName != "" {
					key = typeName + "." + fd.Name.Name
				}
			}
			idx[key] = fd
		}
	}
	return idx
}

// BuildCallGraph constructs a WaveletNode tree rooted at entryName, following
// call edges through index. Each node represents a function; its children are
// the distinct functions it calls that appear in the index.
//
// Recursion is handled by a path-scoped visited set: the same function can be
// reached from multiple call sites, but cycles produce a stub leaf rather than
// infinite recursion.
func BuildCallGraph(entryName string, index FuncIndex, fset *token.FileSet) *WaveletNode {
	visited := make(map[string]bool)
	return buildCallNode(entryName, index, fset, visited)
}

func buildCallNode(name string, index FuncIndex, fset *token.FileSet, visited map[string]bool) *WaveletNode {
	fd, ok := index[name]
	if !ok {
		return nil
	}

	if visited[name] {
		return &WaveletNode{
			ASTNode:       fd,
			Name:          name + " (↺)",
			Type:          "*ast.FuncDecl",
			Approximation: bodyComplexity(fd),
		}
	}

	visited[name] = true
	defer delete(visited, name)

	wn := &WaveletNode{
		ASTNode:       fd,
		Name:          name,
		Type:          "*ast.FuncDecl",
		Approximation: bodyComplexity(fd),
	}

	if fd.Body == nil {
		return wn
	}

	seen := make(map[string]bool)
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, callee := range index.resolve(call.Fun) {
			calleeName := funcDeclKey(callee, index)
			if calleeName == "" || seen[calleeName] {
				continue
			}
			seen[calleeName] = true
			if child := buildCallNode(calleeName, index, fset, visited); child != nil {
				wn.Children = append(wn.Children, child)
			}
		}
		return true
	})

	return wn
}

// bodyComplexity sums BaselineComplexity across all AST nodes in a function's
// body, giving each call graph node a realistic initial score.
func bodyComplexity(fd *ast.FuncDecl) float64 {
	total := BaselineComplexity(fd)
	if fd.Body == nil {
		return total
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if n != nil {
			total += BaselineComplexity(n)
		}
		return true
	})
	return total
}

// funcDeclKey reverse-looks up a FuncDecl's key in the index.
func funcDeclKey(fd *ast.FuncDecl, index FuncIndex) string {
	for key, v := range index {
		if v == fd {
			return key
		}
	}
	return ""
}
