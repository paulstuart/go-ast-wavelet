package astwavelet

import (
	"go/ast"
	"go/token"
	"math"
	"sort"
)

// Report is the structured output of a full wavelet analysis run.
// It contains all three complementary views of the program.
type Report struct {
	Dir        string
	EntryPoint string

	// CallGraph lists every reachable function, ordered by Complexity descending.
	// Complexity = total subtree energy (the function plus everything it calls).
	// Entropy = sibling heterogeneity (how varied the direct callees are).
	// A high Entropy with mixed-complexity children is the primary refactor signal.
	CallGraph []CallNode

	// Boundaries are structural transition points detected by Ricker CWT.
	// Organized by band: fine (statements), medium (functions), coarse (sections).
	// Useful for splitting code into coherent context chunks for LLM consumption.
	Boundaries []Boundary

	// LineHotspots are the highest-irregularity lines from the Haar DWT.
	// A line with high irregularity sits in a region that changes structural
	// character at one or more scales — a natural focal point for review.
	LineHotspots []LineHotspot

	// SimilarFunctions are pairs of functions whose structural fingerprints
	// are similar enough to warrant consideration for consolidation.
	// Similarity is cosine similarity of their Haar detail coefficient vectors.
	SimilarFunctions []SimilarPair
}

// CallNode is one function in the call graph.
type CallNode struct {
	Name       string
	File       string
	Line       int
	Depth      int
	Complexity float64 // Approximation: subtree energy
	Entropy    float64 // Detail: sibling heterogeneity
}

// Boundary is a structural transition in the source code.
type Boundary struct {
	Line  int
	Band  string  // "fine", "medium", "coarse"
	Scale float64 // wavelet scale at which the peak was detected
	Coeff float64 // signed coefficient (magnitude = confidence)
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

	report := &Report{
		Dir:        dir,
		EntryPoint: entryPoint,
	}

	report.CallGraph = buildCallGraphReport(entryPoint, index, fset)
	report.Boundaries, report.LineHotspots = buildSignalReport(files, fset)

	fps := ComputeFingerprints(files, fset)
	report.SimilarFunctions = FindSimilarFunctions(fps, 0.90)

	return report, nil
}

func buildCallGraphReport(entry string, index FuncIndex, fset *token.FileSet) []CallNode {
	root := BuildCallGraph(entry, index, fset)
	if root == nil {
		return nil
	}
	root.Transform()

	var nodes []CallNode
	var collect func(*WaveletNode, int)
	collect = func(wn *WaveletNode, depth int) {
		if wn == nil {
			return
		}
		pos := fset.Position(wn.ASTNode.Pos())
		nodes = append(nodes, CallNode{
			Name:       wn.Name,
			File:       pos.Filename,
			Line:       pos.Line,
			Depth:      depth,
			Complexity: wn.Approximation,
			Entropy:    wn.Detail,
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

	// Ricker CWT for boundaries.
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

	// Haar DWT for per-line irregularity.
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
