package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"math"
	"sort"

	"github.com/paulstuart/go-ast-wavelet/astwavelet"
)

const demoSource = "samples/simple/main.go"

func main() {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, demoSource, nil, 0)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	treeAnalysis(fset, file)
	fmt.Println()
	signalAnalysis(fset, file)
}

// treeAnalysis uses the tree-based Haar transform to rank nodes by
// subtree energy (Approximation) and sibling heterogeneity (Detail).
func treeAnalysis(fset *token.FileSet, file *ast.File) {
	root := astwavelet.BuildWaveletTree(file)
	root.Transform()

	type entry struct {
		name   string
		ntype  string
		line   int
		approx float64
		detail float64
	}

	var rows []entry
	var collect func(*astwavelet.WaveletNode)
	collect = func(wn *astwavelet.WaveletNode) {
		if wn == nil {
			return
		}
		if wn.Detail > 0.5 || wn.Approximation > 5.0 {
			pos := fset.Position(wn.ASTNode.Pos())
			rows = append(rows, entry{
				name:   wn.Name,
				ntype:  wn.Type,
				line:   pos.Line,
				approx: wn.Approximation,
				detail: wn.Detail,
			})
		}
		for _, child := range wn.Children {
			collect(child)
		}
	}
	collect(root)

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].approx > rows[j].approx
	})

	fmt.Println("=== TREE ANALYSIS: Subtree Energy & Sibling Heterogeneity ===")
	fmt.Printf("%-24s %-26s %5s  %10s  %10s\n", "Name", "AST Type", "Line", "Approx", "Detail")
	fmt.Println(repeat("-", 80))
	for _, r := range rows {
		name := r.name
		if name == "" {
			name = "-"
		}
		fmt.Printf("%-24s %-26s %5d  %10.2f  %10.2f\n", name, r.ntype, r.line, r.approx, r.detail)
	}
}

// signalAnalysis applies Ricker CWT and Haar DWT to a flat per-line signal
// derived from AST node positions, then reports structural boundaries and
// per-line irregularity.
func signalAnalysis(fset *token.FileSet, file *ast.File) {
	signal := astwavelet.BuildLineSignal(file, fset)

	// Ricker CWT: structural boundary detection
	cwt := astwavelet.ComputeCWT(signal, astwavelet.DefaultScales)
	threshold := 0.3
	peaks := astwavelet.DetectPeaks(cwt, threshold, 20, 2)

	fmt.Println("=== SIGNAL ANALYSIS: Structural Boundaries (Ricker CWT) ===")
	fmt.Printf("%-5s  %-8s  %-10s  %s\n", "Line", "Band", "Coeff", "Scale")
	fmt.Println(repeat("-", 40))
	for _, p := range peaks {
		fmt.Printf("L%-4d  %-8s  %+.4f    %.0f\n", p.Line, p.Band, p.Coefficient, p.Scale)
	}

	// Haar DWT: per-line irregularity back-projection
	maxLevels := min(int(math.Log2(float64(len(signal)))), 8)
	decomp := astwavelet.HaarDecompose(signal, maxLevels)
	irregularity := astwavelet.PerLineIrregularity(decomp, len(signal))

	type lineScore struct {
		line  int
		score float64
	}
	var hot []lineScore
	for i, s := range irregularity {
		if s > 0.05 {
			hot = append(hot, lineScore{i + 1, s})
		}
	}
	sort.Slice(hot, func(i, j int) bool {
		return hot[i].score > hot[j].score
	})
	if len(hot) > 15 {
		hot = hot[:15]
	}

	fmt.Println()
	fmt.Println("=== SIGNAL ANALYSIS: Per-Line Irregularity (Haar DWT) ===")
	fmt.Printf("%-5s  %s\n", "Line", "Irregularity")
	fmt.Println(repeat("-", 25))
	for _, h := range hot {
		fmt.Printf("L%-4d  %.4f\n", h.line, h.score)
	}
}

func repeat(s string, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = s[0]
	}
	return string(out)
}
