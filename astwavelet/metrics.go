package astwavelet

import (
	"strings"
	"unicode"
)

// ComplexityCliff identifies a function whose surface looks simple but whose
// call subtree is disproportionately expensive. The surface complexity is the
// function's own body; the total complexity includes everything it calls.
// A high Ratio means: one call away from a complexity explosion.
type ComplexityCliff struct {
	Name          string
	File          string
	Line          int
	SurfaceScore  float64 // body complexity only
	TotalScore    float64 // full subtree (surface + all callees)
	Ratio         float64 // TotalScore / SurfaceScore — higher = steeper cliff
}

// computeFanIn counts how many distinct parent nodes each function has in the
// call graph tree. A high fan-in means many things depend on this function —
// it's a high-risk change target regardless of its own complexity.
func computeFanIn(root *WaveletNode) map[string]int {
	fanIn := make(map[string]int)
	var walk func(*WaveletNode)
	walk = func(wn *WaveletNode) {
		if wn == nil {
			return
		}
		for _, child := range wn.Children {
			name := strings.TrimSuffix(child.Name, " (↺)")
			if name != "" {
				fanIn[name]++
			}
			walk(child)
		}
	}
	walk(root)
	return fanIn
}

// filterExported returns the subset of nodes whose function name is exported
// (starts with an uppercase letter), sorted by Complexity descending.
// For methods ("TypeName.MethodName"), the method name is checked.
func filterExported(nodes []CallNode) []CallNode {
	var out []CallNode
	for _, n := range nodes {
		if isExported(n.Name) {
			out = append(out, n)
		}
	}
	return out // already sorted by Complexity from the caller
}

// findComplexityCliffs returns functions where TotalScore / SurfaceScore
// exceeds minRatio, sorted by Ratio descending.
// The default minRatio of 5 means: the subtree is at least 5× the surface.
// FindComplexityCliffs is the exported form for use outside the package.
func FindComplexityCliffs(nodes []CallNode, minRatio float64) []ComplexityCliff {
	return findComplexityCliffs(nodes, minRatio)
}

func findComplexityCliffs(nodes []CallNode, minRatio float64) []ComplexityCliff {
	if minRatio <= 0 {
		minRatio = 5.0
	}
	var cliffs []ComplexityCliff
	for _, n := range nodes {
		if n.LocalComplexity <= 0 {
			continue
		}
		ratio := n.Complexity / n.LocalComplexity
		if ratio >= minRatio {
			cliffs = append(cliffs, ComplexityCliff{
				Name:         n.Name,
				File:         n.File,
				Line:         n.Line,
				SurfaceScore: n.LocalComplexity,
				TotalScore:   n.Complexity,
				Ratio:        ratio,
			})
		}
	}
	// Sort by Ratio descending.
	for i := 1; i < len(cliffs); i++ {
		for j := i; j > 0 && cliffs[j].Ratio > cliffs[j-1].Ratio; j-- {
			cliffs[j], cliffs[j-1] = cliffs[j-1], cliffs[j]
		}
	}
	return cliffs
}

// isExported reports whether a function name (possibly "TypeName.MethodName")
// represents an exported identifier.
func isExported(name string) bool {
	// For "TypeName.MethodName", check the method part.
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	if len(name) == 0 {
		return false
	}
	return unicode.IsUpper(rune(name[0]))
}
