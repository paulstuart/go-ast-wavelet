package astwavelet

import (
	"fmt"
	"go/ast"
	"math"
)

// WaveletNode wraps a standard Go AST node with multi-resolution wavelet coefficients.
type WaveletNode struct {
	ASTNode  ast.Node
	Type     string         `json:"type"`
	Children []*WaveletNode `json:"children,omitempty"`

	// Wavelet Coefficients
	Approximation float64 `json:"approximation"` // Low-pass: Cumulative structural energy
	Detail        float64 `json:"detail"`        // High-pass: Local structural irregularity
}

// BaselineComplexity assigns initial "energy" values to nodes based on language semantics.
func BaselineComplexity(node ast.Node) float64 {
	if node == nil {
		return 0.0
	}
	switch node.(type) {
	case *ast.FuncDecl:
		return 5.0 // Major structural boundary
	case *ast.IfStmt, *ast.ForStmt, *ast.SelectStmt, *ast.SwitchStmt:
		return 4.0 // High-frequency control flow branching
	case *ast.GoStmt, *ast.DeferStmt, *ast.ChanType:
		return 3.5 // Concurrency landmarks
	case *ast.AssignStmt, *ast.CallExpr:
		return 1.0 // Standard execution operations
	case *ast.Ident:
		return 0.1 // Baseline identifier noise
	default:
		return 0.5
	}
}

// BuildWaveletTree mirrors Go's flat AST inspection into a explicit parent-child node tree.
func BuildWaveletTree(file *ast.File) *WaveletNode {
	if file == nil {
		return nil
	}

	var root *WaveletNode
	// Track the current path down the tree to attach children to parents correctly
	var stack []*WaveletNode

	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			// Popping off the stack as we retreat back up the tree
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return true
		}

		wNode := &WaveletNode{
			ASTNode:       n,
			Type:          fmt.Sprintf("%T", n),
			Approximation: BaselineComplexity(n),
		}

		if len(stack) == 0 {
			root = wNode
		} else {
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, wNode)
		}

		// Push current node onto the stack for its children
		stack = append(stack, wNode)
		return true
	})

	return root
}

// Transform runs a Haar-style wavelet decomposition bottom-up over the tree.
func (wn *WaveletNode) Transform() {
	if wn == nil {
		return
	}

	if len(wn.Children) == 0 {
		// Leaf nodes retain their baseline complexity; detail variance is zero.
		wn.Detail = 0.0
		return
	}

	// 1. Bottom-up post-order traversal: Transform all subtrees first
	var sum float64
	for _, child := range wn.Children {
		child.Transform()
		sum += child.Approximation
	}

	// 2. Low-Pass Filter: Average the structural energy of children
	childAverage := sum / float64(len(wn.Children))

	// Combine parent's local complexity with the children's average energy
	wn.Approximation = (wn.Approximation + childAverage) / 2.0

	// 3. High-Pass Filter: Compute structural variance (Detail Coefficient)
	var variance float64
	for _, child := range wn.Children {
		diff := child.Approximation - childAverage
		variance += diff * diff
	}
	wn.Detail = math.Sqrt(variance / float64(len(wn.Children)))
}
