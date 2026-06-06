package astwavelet

import (
	"go/ast"
	"go/token"
	"math"
	"sort"
)

// Report is the structured output of a full wavelet analysis run.
type Report struct {
	Dir        string
	EntryPoint string

	// CallGraph lists every reachable function, ordered by Complexity descending.
	// Complexity = total subtree energy (the function plus everything it calls).
	// Entropy = sibling heterogeneity (how varied the direct callees are).
	CallGraph []CallNode

	// FanIn maps each function name to the number of distinct call sites that
	// reach it within the call graph. High fan-in + high complexity = fragile.
	FanIn map[string]int

	// ExportedHotspots is the exported subset of CallGraph, sorted by Complexity.
	// These represent the API surface complexity burden placed on callers.
	ExportedHotspots []CallNode

	// ComplexityCliffs are functions whose call subtree is disproportionately
	// more expensive than their own body. The surface looks simple; one call
	// away is a complexity explosion.
	ComplexityCliffs []ComplexityCliff

	// Boundaries are structural transition points detected by Ricker CWT.
	// Organized by band: fine (statements), medium (functions), coarse (sections).
	Boundaries []Boundary

	// LineHotspots are the highest-irregularity lines from the Haar DWT.
	LineHotspots []LineHotspot

	// SimilarFunctions are pairs of functions whose structural fingerprints
	// exceed 0.90 cosine similarity — structural clone candidates.
	SimilarFunctions []SimilarPair

	// DeadCode classifies every function as called, referenced-only, or unreachable.
	// Referenced-only covers the template FuncMap / callback pattern where a
	// function is passed as a value but never directly called.
	DeadCode DeadCodeReport
}

// CallNode is one function in the call graph.
type CallNode struct {
	Name           string
	File           string
	Line           int
	Depth          int
	LocalComplexity float64 // body complexity only, excluding callees
	Complexity     float64  // Approximation: total subtree energy
	Entropy        float64  // Detail: sibling heterogeneity
}

// Boundary is a structural transition in the source code.
type Boundary struct {
	Line  int
	Band  string
	Scale float64
	Coeff float64
}

// LineHotspot is a source line with elevated structural irregularity.
type LineHotspot struct {
	Line         int
	Irregularity float64
}

// Analyze runs the full pipeline on the package at dir, starting the call
// graph from entryPoint (typically "main").
func Analyze(dir, entryPoint string) (*Report, error) {
	fset, files, index, err := LoadPackage(dir)
	if err != nil {
		return nil, err
	}

	report := &Report{Dir: dir, EntryPoint: entryPoint}

	// Build call graph and compute all tree-based metrics in one pass.
	root := BuildCallGraph(entryPoint, index, fset)
	if root != nil {
		root.Transform()
		report.CallGraph = collectCallNodes(root, fset)
		report.FanIn = computeFanIn(root)
		report.ExportedHotspots = filterExported(report.CallGraph)
		report.ComplexityCliffs = findComplexityCliffs(report.CallGraph, 5.0)

		// Dead code: use the call graph's reachable set + value-reference scan.
		called := collectCalledNames(root)
		report.DeadCode = computeDeadCode(entryPoint, index, fset, called)
	}

	// Signal analysis.
	report.Boundaries, report.LineHotspots = buildSignalReport(files, fset)

	// Structural similarity.
	fps := ComputeFingerprints(files, fset)
	report.SimilarFunctions = FindSimilarFunctions(fps, 0.90)

	return report, nil
}

func collectCallNodes(root *WaveletNode, fset *token.FileSet) []CallNode {
	var nodes []CallNode
	var collect func(*WaveletNode, int)
	collect = func(wn *WaveletNode, depth int) {
		if wn == nil {
			return
		}
		pos := fset.Position(wn.ASTNode.Pos())

		var local float64
		if fd, ok := wn.ASTNode.(*ast.FuncDecl); ok {
			local = bodyComplexity(fd)
		}

		nodes = append(nodes, CallNode{
			Name:            wn.Name,
			File:            pos.Filename,
			Line:            pos.Line,
			Depth:           depth,
			LocalComplexity: local,
			Complexity:      wn.Approximation,
			Entropy:         wn.Detail,
		})
		for _, child := range wn.Children {
			collect(child, depth+1)
		}
	}
	collect(root, 0)

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Complexity > nodes[j].Complexity
	})
	return nodes
}

func buildSignalReport(files []*ast.File, fset *token.FileSet) ([]Boundary, []LineHotspot) {
	var signal []float64
	for _, f := range files {
		signal = append(signal, BuildLineSignal(f, fset)...)
	}

	cwt := ComputeCWT(signal, DefaultScales)
	peaks := DetectPeaks(cwt, 0.3, 30, 2)

	boundaries := make([]Boundary, len(peaks))
	for i, p := range peaks {
		boundaries[i] = Boundary{
			Line:  p.Line,
			Band:  p.Band.String(),
			Scale: p.Scale,
			Coeff: p.Coefficient,
		}
	}

	maxLevels := min(int(math.Log2(float64(len(signal)))), 8)
	decomp := HaarDecompose(signal, maxLevels)
	irregularity := PerLineIrregularity(decomp, len(signal))

	type scored struct {
		line  int
		score float64
	}
	var scored2 []scored
	for i, s := range irregularity {
		if s > 0.05 {
			scored2 = append(scored2, scored{i + 1, s})
		}
	}
	sort.Slice(scored2, func(i, j int) bool {
		return scored2[i].score > scored2[j].score
	})
	if len(scored2) > 20 {
		scored2 = scored2[:20]
	}

	hotspots := make([]LineHotspot, len(scored2))
	for i, s := range scored2 {
		hotspots[i] = LineHotspot{Line: s.line, Irregularity: s.score}
	}

	return boundaries, hotspots
}
