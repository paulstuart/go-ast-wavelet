package astwavelet

import (
	"go/ast"
	"go/token"
	"strings"
)

// DeadCodeReport classifies every function in the package into one of three
// reachability categories.
type DeadCodeReport struct {
	// Called functions are reachable via direct call edges from the entry point.
	Called []FuncRef

	// Referenced functions appear as values within reachable code — passed as
	// callbacks, registered in FuncMaps, assigned to variables — but are never
	// directly called. At runtime they may be invoked; static analysis cannot
	// trace the call. The canonical case is html/template FuncMaps.
	// These should NOT be deleted without verifying the reference sites.
	Referenced []FuncRef

	// Unreachable functions are in the package but appear in neither of the
	// above sets. They are genuine dead code candidates.
	Unreachable []FuncRef
}

// FuncRef identifies one function declaration.
type FuncRef struct {
	Name string
	File string
	Line int
}

// computeDeadCode builds the reachability report by traversing from entryName,
// following call edges and tracking value references separately.
//
// The calledSet is the set of function names already known to be reachable via
// call edges (derived from the call graph tree). computeDeadCode uses it as a
// starting point and additionally scans for value-position references within
// each reachable function's body.
func computeDeadCode(entryName string, index FuncIndex, fset *token.FileSet, calledSet map[string]bool) DeadCodeReport {
	referenced := make(map[string]bool)

	// For each already-called function, scan its body for function values
	// appearing in non-call positions (callbacks, FuncMaps, etc.).
	for name := range calledSet {
		fd, ok := index[name]
		if !ok || fd.Body == nil {
			continue
		}
		scanValueRefs(fd.Body, index, name, calledSet, referenced)
	}

	var report DeadCodeReport
	for key, fd := range index {
		pos := fset.Position(fd.Pos())
		ref := FuncRef{Name: key, File: pos.Filename, Line: pos.Line}
		switch {
		case calledSet[key]:
			report.Called = append(report.Called, ref)
		case referenced[key]:
			report.Referenced = append(report.Referenced, ref)
		default:
			report.Unreachable = append(report.Unreachable, ref)
		}
	}

	sortFuncRefs(report.Called)
	sortFuncRefs(report.Referenced)
	sortFuncRefs(report.Unreachable)

	return report
}

// scanValueRefs finds function identifiers used as values (not in call position)
// within body. These are candidates for the Referenced set.
//
// The key insight: in `template.FuncMap{"sum": sum}`, the identifier `sum` has
// a source position that is NOT the Fun field of any CallExpr. By first
// collecting all Fun positions, we can exclude them and identify value uses.
func scanValueRefs(
	body *ast.BlockStmt,
	index FuncIndex,
	ownerName string,
	calledSet map[string]bool,
	referenced map[string]bool,
) {
	// Pass 1: collect positions that are in "call" position (Fun of a CallExpr).
	callPos := make(map[token.Pos]bool)
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			callPos[call.Fun.Pos()] = true
		}
		return true
	})

	// Pass 2: find identifiers/selectors in value position that name functions.
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.SelectorExpr:
			if callPos[v.Pos()] {
				return true // in call position — handled elsewhere
			}
			// Cross-package value reference: pkg.Func or receiver.Method used as value.
			suffix := "." + v.Sel.Name
			for key := range index {
				if strings.HasSuffix(key, suffix) && !calledSet[key] && key != ownerName {
					referenced[key] = true
				}
			}
			return false // don't re-visit v.Sel as a plain Ident

		case *ast.Ident:
			if callPos[v.Pos()] {
				return true
			}
			// Local function used as value.
			if fd2, ok := index[v.Name]; ok {
				key := funcDeclKey(fd2, index)
				if key != "" && !calledSet[key] && key != ownerName {
					referenced[key] = true
				}
			}
		}
		return true
	})
}

// collectCalledNames walks a call graph tree and returns the set of all
// function names that appear in it (i.e., are reachable via call edges).
func collectCalledNames(root *WaveletNode) map[string]bool {
	called := make(map[string]bool)
	var walk func(*WaveletNode)
	walk = func(wn *WaveletNode) {
		if wn == nil {
			return
		}
		// Strip the recursion marker from stub nodes.
		name := strings.TrimSuffix(wn.Name, " (↺)")
		if name != "" {
			called[name] = true
		}
		for _, child := range wn.Children {
			walk(child)
		}
	}
	walk(root)
	return called
}

func sortFuncRefs(refs []FuncRef) {
	for i := 1; i < len(refs); i++ {
		for j := i; j > 0 && refs[j].Name < refs[j-1].Name; j-- {
			refs[j], refs[j-1] = refs[j-1], refs[j]
		}
	}
}
