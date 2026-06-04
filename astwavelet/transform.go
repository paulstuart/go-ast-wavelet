package astwavelet

import (
	"fmt"
	"go/ast"
	"math"
)

// WaveletNode wraps a Go AST node with multi-resolution wavelet coefficients.
type WaveletNode struct {
	ASTNode  ast.Node
	Name     string         // identifier name for FuncDecl, TypeSpec, etc.
	Type     string         `json:"type"`
	Children []*WaveletNode `json:"children,omitempty"`

	// Approximation: total structural energy of this node's entire subtree.
	// Higher = more complex subtree overall. Comparable across siblings.
	Approximation float64 `json:"approximation"`

	// Detail: population std dev of children's approximations.
	// Higher = children are structurally heterogeneous (mixed complexity).
	Detail float64 `json:"detail"`
}

// BaselineComplexity assigns initial structural energy to an AST node type.
func BaselineComplexity(node ast.Node) float64 {
	if node == nil {
		return 0.0
	}
	switch node.(type) {
	case *ast.FuncDecl:
		return 5.0
	case *ast.IfStmt, *ast.ForStmt, *ast.SelectStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt:
		return 4.0
	case *ast.RangeStmt:
		return 3.5
	case *ast.GoStmt, *ast.DeferStmt, *ast.ChanType:
		return 3.5
	case *ast.InterfaceType:
		return 3.0
	case *ast.TypeSpec:
		return 2.5
	case *ast.FuncType:
		return 2.0
	case *ast.AssignStmt, *ast.CallExpr:
		return 1.0
	case *ast.ReturnStmt:
		return 0.8
	case *ast.SendStmt:
		return 0.8
	case *ast.Ident:
		return 0.1
	default:
		return 0.5
	}
}

func nodeName(n ast.Node) string {
	switch v := n.(type) {
	case *ast.FuncDecl:
		if v.Name != nil {
			return v.Name.Name
		}
	case *ast.TypeSpec:
		return v.Name.Name
	case *ast.Ident:
		return v.Name
	}
	return ""
}

// BuildWaveletTree wraps a parsed Go file in a WaveletNode tree mirroring the AST hierarchy.
func BuildWaveletTree(file *ast.File) *WaveletNode {
	if file == nil {
		return nil
	}

	var root *WaveletNode
	var stack []*WaveletNode

	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return true
		}

		wNode := &WaveletNode{
			ASTNode:       n,
			Name:          nodeName(n),
			Type:          fmt.Sprintf("%T", n),
			Approximation: BaselineComplexity(n),
		}

		if len(stack) == 0 {
			root = wNode
		} else {
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, wNode)
		}

		stack = append(stack, wNode)
		return true
	})

	return root
}

// Transform runs a bottom-up wavelet decomposition over the tree.
//
// After Transform:
//   - Approximation = baseline + sum of all descendants' baselines (subtree energy budget)
//   - Detail = population std dev of direct children's approximations (sibling heterogeneity)
//
// A high Detail on a FuncDecl means that function mixes very simple and very
// complex statements — a refactoring signal. A high Approximation means the
// entire subtree is costly.
func (wn *WaveletNode) Transform() {
	if wn == nil {
		return
	}

	if len(wn.Children) == 0 {
		wn.Detail = 0.0
		return
	}

	var sum float64
	for _, child := range wn.Children {
		child.Transform()
		sum += child.Approximation
	}

	// Cumulative subtree energy: local complexity + everything beneath it.
	wn.Approximation = wn.Approximation + sum

	// Population std dev of children: measures sibling heterogeneity.
	mean := sum / float64(len(wn.Children))
	var variance float64
	for _, child := range wn.Children {
		diff := child.Approximation - mean
		variance += diff * diff
	}
	wn.Detail = math.Sqrt(variance / float64(len(wn.Children)))
}
