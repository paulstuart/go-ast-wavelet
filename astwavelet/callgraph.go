package astwavelet

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// FuncIndex maps declaration keys to their AST nodes.
// Plain functions are keyed by name ("main", "GnarlyFunction").
// Methods are keyed by "TypeName.MethodName" ("Worker.Run").
type FuncIndex map[string]*ast.FuncDecl

// resolve returns the FuncDecl for a call expression, or nil if not in the index.
//
// For plain calls (Ident), looks up by name directly.
// For method calls (SelectorExpr), tries "TypeName.Method" by scanning all
// method-shaped keys whose suffix matches ".Method". This is the seam that
// a full type-aware implementation would replace with a single O(1) lookup.
func (idx FuncIndex) resolve(expr ast.Expr) []*ast.FuncDecl {
	switch v := expr.(type) {
	case *ast.Ident:
		if fd, ok := idx[v.Name]; ok {
			return []*ast.FuncDecl{fd}
		}
	case *ast.SelectorExpr:
		method := "." + v.Sel.Name
		var matches []*ast.FuncDecl
		for key, fd := range idx {
			if strings.HasSuffix(key, method) {
				matches = append(matches, fd)
			}
		}
		return matches
	}
	return nil
}

// BuildFuncIndex indexes all function and method declarations across a set of files.
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

func receiverType(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.StarExpr:
		return receiverType(v.X)
	case *ast.Ident:
		return v.Name
	}
	return ""
}

// LoadDir parses all .go files in dir and returns the file set, parsed files,
// and a function index covering all declarations.
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
		// Represent the back-edge as a leaf so the parent still carries
		// some complexity signal, but we don't recurse.
		return &WaveletNode{
			ASTNode:       fd,
			Name:          name + " (↺)",
			Type:          "*ast.FuncDecl",
			Approximation: bodyComplexity(fd),
		}
	}

	visited[name] = true
	defer delete(visited, name) // allow re-entry from independent call paths

	wn := &WaveletNode{
		ASTNode:       fd,
		Name:          name,
		Type:          "*ast.FuncDecl",
		Approximation: bodyComplexity(fd),
	}

	if fd.Body == nil {
		return wn
	}

	// Collect distinct callees within this function body.
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
// body. This gives each call graph node a realistic initial complexity based on
// the actual statements inside it, rather than the flat FuncDecl weight.
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

// funcDeclKey returns the index key for a FuncDecl by reverse-looking it up.
func funcDeclKey(fd *ast.FuncDecl, index FuncIndex) string {
	for key, v := range index {
		if v == fd {
			return key
		}
	}
	return ""
}
