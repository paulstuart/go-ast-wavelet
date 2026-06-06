package astwavelet

import (
	"go/ast"
	"go/token"
	"math"
	"sort"
)

// fingerprintSize is the fixed resampling length before Haar decomposition.
// 32 points gives 5 decomposition levels (detail bands of 16,8,4,2,1 = 31 coefficients).
// Large enough to capture structural rhythm; small enough to be length-independent.
const fingerprintSize = 32

// FuncFingerprint holds the multi-scale structural fingerprint for one function.
type FuncFingerprint struct {
	Name        string // "FuncName" or "TypeName.MethodName"
	File        string
	Line        int
	Fingerprint []float64 // normalized Haar detail coefficients (unit vector)
}

// SimilarPair is a pair of functions whose structural fingerprints are similar.
type SimilarPair struct {
	A          string  // function name
	B          string  // function name
	Similarity float64 // cosine similarity in [0,1]; 1.0 = identical structure
}

// ComputeFingerprints builds a structural fingerprint for every function with a
// body in the provided files. Each fingerprint is derived from:
//  1. The per-line complexity signal for the function's source lines
//  2. Resampled to fingerprintSize for length-independent comparison
//  3. Haar-decomposed at up to 5 levels; detail bands concatenated
//  4. L2-normalized to a unit vector for cosine similarity
func ComputeFingerprints(files []*ast.File, fset *token.FileSet) []FuncFingerprint {
	var fps []FuncFingerprint

	for _, f := range files {
		signal := BuildLineSignal(f, fset)
		fileLines := fset.File(f.Pos()).LineCount()

		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}

			startLine := fset.Position(fd.Pos()).Line  // 1-indexed, inclusive
			endLine := fset.Position(fd.End()).Line    // 1-indexed, inclusive
			if startLine < 1 || endLine > fileLines {
				continue
			}

			// Extract the function's slice of the file signal (0-indexed).
			funcSignal := signal[startLine-1 : min(endLine, len(signal))]

			key := fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				if typeName := receiverType(fd.Recv.List[0].Type); typeName != "" {
					key = typeName + "." + fd.Name.Name
				}
			}

			fps = append(fps, FuncFingerprint{
				Name:        key,
				File:        fset.Position(fd.Pos()).Filename,
				Line:        startLine,
				Fingerprint: computeFingerprint(funcSignal),
			})
		}
	}

	return fps
}

// FindSimilarFunctions returns all pairs of functions whose structural
// fingerprints exceed the similarity threshold (cosine similarity ≥ threshold).
// Results are sorted by similarity descending.
//
// A threshold of 0.90 finds strong structural clones.
// A threshold of 0.75 finds functions with similar structural rhythm.
func FindSimilarFunctions(fps []FuncFingerprint, threshold float64) []SimilarPair {
	var pairs []SimilarPair

	for i := range fps {
		for j := i + 1; j < len(fps); j++ {
			sim := cosineSimilarity(fps[i].Fingerprint, fps[j].Fingerprint)
			if sim >= threshold {
				pairs = append(pairs, SimilarPair{
					A:          fps[i].Name,
					B:          fps[j].Name,
					Similarity: sim,
				})
			}
		}
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Similarity > pairs[j].Similarity
	})

	return pairs
}

// computeFingerprint resamples signal to fingerprintSize, applies multi-level
// Haar decomposition, and returns a normalized detail coefficient vector.
func computeFingerprint(signal []float64) []float64 {
	resampled := resample(signal, fingerprintSize)

	decomp := HaarDecompose(resampled, 5)

	var fp []float64
	for _, level := range decomp.Levels {
		fp = append(fp, level.Detail...)
	}

	return l2Normalize(fp)
}

// resample linearly interpolates signal to exactly n evenly-spaced points.
// Handles empty and single-element signals gracefully.
func resample(signal []float64, n int) []float64 {
	out := make([]float64, n)
	if len(signal) == 0 {
		return out
	}
	if len(signal) == 1 {
		for i := range out {
			out[i] = signal[0]
		}
		return out
	}
	srcMax := float64(len(signal) - 1)
	for i := range n {
		t := float64(i) / float64(n-1) * srcMax
		lo := int(t)
		hi := lo + 1
		if hi >= len(signal) {
			out[i] = signal[len(signal)-1]
			continue
		}
		frac := t - float64(lo)
		out[i] = signal[lo]*(1-frac) + signal[hi]*frac
	}
	return out
}

// l2Normalize returns the unit vector of v, or zero vector if v has zero norm.
func l2Normalize(v []float64) []float64 {
	var sum float64
	for _, x := range v {
		sum += x * x
	}
	if sum == 0 {
		return v
	}
	norm := math.Sqrt(sum)
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = x / norm
	}
	return out
}

// cosineSimilarity returns the dot product of two unit vectors.
// Assumes both vectors are already L2-normalized.
func cosineSimilarity(a, b []float64) float64 {
	n := min(len(a), len(b))
	var dot float64
	for i := range n {
		dot += a[i] * b[i]
	}
	// Clamp to [-1, 1] to guard against floating-point drift.
	return max(-1, min(1, dot))
}
