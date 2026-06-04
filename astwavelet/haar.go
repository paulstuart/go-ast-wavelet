package astwavelet

import "math"

// HaarLevel holds one level of a multi-level Haar decomposition.
type HaarLevel struct {
	Approx []float64
	Detail []float64
}

// HaarDecomposition holds all levels from a multi-level Haar DWT.
type HaarDecomposition struct {
	Levels      []HaarLevel
	FinalApprox []float64
}

// HaarDecompose applies a multi-level forward Haar DWT to signal.
// Decomposition stops early if the approximation band drops below length 2.
// Cap levels at floor(log2(len(signal))) for a natural maximum.
func HaarDecompose(signal []float64, levels int) HaarDecomposition {
	var result HaarDecomposition
	approx := make([]float64, len(signal))
	copy(approx, signal)

	for range levels {
		if len(approx) < 2 {
			break
		}
		next, detail := haarFwd1d(approx)
		result.Levels = append(result.Levels, HaarLevel{Approx: next, Detail: detail})
		approx = next
	}

	result.FinalApprox = approx
	return result
}

// haarFwd1d applies a single-level forward Haar transform.
// Odd-length input drops the last sample.
func haarFwd1d(signal []float64) (approx, detail []float64) {
	n := len(signal) >> 1
	approx = make([]float64, n)
	detail = make([]float64, n)
	for i := range n {
		a, b := signal[2*i], signal[2*i+1]
		approx[i] = (a + b) / 2
		detail[i] = a - b
	}
	return
}

// PerLineIrregularity back-projects Haar detail coefficients to individual
// source lines. Each detail coefficient covers a span of 2^(level+1) lines;
// its magnitude is distributed evenly across those lines.
//
// The result is a per-line score where higher values indicate that the line
// sits in a region of high structural irregularity at one or more scales.
func PerLineIrregularity(decomp HaarDecomposition, totalLines int) []float64 {
	scores := make([]float64, totalLines)

	for lev, lvl := range decomp.Levels {
		span := 1 << (lev + 1)
		for di, d := range lvl.Detail {
			weight := math.Abs(d)
			start := di * span
			end := min(start+span, totalLines)
			contrib := weight / float64(span)
			for li := start; li < end; li++ {
				scores[li] += contrib
			}
		}
	}

	return scores
}
