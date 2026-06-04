package astwavelet

import (
	"go/ast"
	"go/token"
)

// BuildLineSignal creates a per-line complexity signal from the AST.
// Each line's score accumulates the BaselineComplexity of all AST nodes
// whose source position falls on that line. This is richer than keyword
// scanning because it uses the actual parsed structure.
func BuildLineSignal(file *ast.File, fset *token.FileSet) []float64 {
	f := fset.File(file.Pos())
	totalLines := f.LineCount()
	signal := make([]float64, totalLines)

	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		pos := fset.Position(n.Pos())
		if pos.IsValid() && pos.Line >= 1 && pos.Line <= totalLines {
			signal[pos.Line-1] += BaselineComplexity(n)
		}
		return true
	})

	return signal
}
