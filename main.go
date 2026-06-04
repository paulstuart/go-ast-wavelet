package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"log"
	"math"
	"sort"

	"github.com/paulstuart/go-ast-wavelet/astwavelet"
)

const sampleDir = "samples/simple"

func main() {
	fset, files, index, err := astwavelet.LoadPackage(sampleDir)
	if err != nil {
		log.Fatalf("load error: %v", err)
	}

	callGraphAnalysis(fset, index)
	fmt.Println()
	signalAnalysis(fset, files)
}

// callGraphAnalysis walks the call graph from main and applies the tree-based
// wavelet transform to the resulting call hierarchy.
func callGraphAnalysis(fset *token.FileSet, index astwavelet.FuncIndex) {
	root := astwavelet.BuildCallGraph("main", index, fset)
	if root == nil {
		log.Fatal("no main function found in index")
	}
	root.Transform()

	type entry struct {
		name   string
		line   int
		approx float64
		detail float64
		depth  int
	}

	var rows []entry
	var collect func(*astwavelet.WaveletNode, int)
	collect = func(wn *astwavelet.WaveletNode, depth int) {
		if wn == nil {
			return
		}
		pos := fset.Position(wn.ASTNode.Pos())
		rows = append(rows, entry{
			name:   wn.Name,
			line:   pos.Line,
			approx: wn.Approximation,
			detail: wn.Detail,
			depth:  depth,
		})
		for _, child := range wn.Children {
			collect(child, depth+1)
		}
	}
	collect(root, 0)

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].approx > rows[j].approx
	})

	fmt.Println("=== CALL GRAPH ANALYSIS: Subtree Energy & Sibling Heterogeneity ===")
	fmt.Printf("%-28s %5s  %10s  %10s  %s\n", "Function", "Line", "Approx", "Detail", "Depth")
	fmt.Println(repeat("-", 70))
	for _, r := range rows {
		fmt.Printf("%-28s %5d  %10.2f  %10.2f  %d\n",
			r.name, r.line, r.approx, r.detail, r.depth)
	}
}

// signalAnalysis applies Ricker CWT and Haar DWT to a flat per-line signal
// built from AST node positions across all files in the package.
func signalAnalysis(fset *token.FileSet, files []*ast.File) {
	// Merge per-line signals across all files into a single signal.
	// Each file contributes a slice; we concatenate them.
	var signal []float64
	for _, f := range files {
		signal = append(signal, astwavelet.BuildLineSignal(f, fset)...)
	}

	cwt := astwavelet.ComputeCWT(signal, astwavelet.DefaultScales)
	peaks := astwavelet.DetectPeaks(cwt, 0.3, 20, 2)

	fmt.Println("=== SIGNAL ANALYSIS: Structural Boundaries (Ricker CWT) ===")
	fmt.Printf("%-5s  %-8s  %-10s  %s\n", "Line", "Band", "Coeff", "Scale")
	fmt.Println(repeat("-", 40))
	for _, p := range peaks {
		fmt.Printf("L%-4d  %-8s  %+.4f    %.0f\n", p.Line, p.Band, p.Coefficient, p.Scale)
	}

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
	sort.Slice(hot, func(i, j int) bool { return hot[i].score > hot[j].score })
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
