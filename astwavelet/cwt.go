package astwavelet

import (
	"math"
	"sort"
)

// DefaultScales mirrors wavescope's 8-octave span.
var DefaultScales = []float64{1, 2, 4, 8, 16, 32, 64, 128}

// RickerWavelet is the Mexican hat wavelet: ψ(t) = (1 - t²) · exp(-t²/2)
func RickerWavelet(t float64) float64 {
	t2 := t * t
	return (1 - t2) * math.Exp(-t2/2)
}

// CWTResult holds Ricker wavelet coefficients across all scales.
type CWTResult struct {
	Scales       []float64
	Coefficients [][]float64 // [scaleIndex][lineIndex]
}

// ComputeCWT applies the Ricker continuous wavelet transform to signal at
// each scale, using reflective boundary conditions to suppress edge artifacts.
//
// For each scale a, coefficients measure how much the signal resembles the
// Ricker shape at that scale — large values indicate structural boundaries.
func ComputeCWT(signal []float64, scales []float64) CWTResult {
	N := len(signal)
	result := CWTResult{
		Scales:       scales,
		Coefficients: make([][]float64, len(scales)),
	}
	if N == 0 {
		for i := range result.Coefficients {
			result.Coefficients[i] = []float64{}
		}
		return result
	}

	for si, a := range scales {
		half := min(int(math.Ceil(5*a)), (N+1)/2)
		invSqrtA := 1.0 / math.Sqrt(a)
		coeffs := make([]float64, N)

		for pos := range N {
			var sum float64
			for k := -half; k <= half; k++ {
				t := float64(k) / a
				kv := invSqrtA * RickerWavelet(t)
				idx := pos + k
				var sv float64
				if idx >= 0 && idx < N {
					sv = signal[idx]
				} else {
					sv = signal[reflectIdx(idx, N)]
				}
				sum += kv * sv
			}
			coeffs[pos] = sum
		}
		result.Coefficients[si] = coeffs
	}

	return result
}

// reflectIdx mirrors an out-of-range index symmetrically back into [0, N-1].
func reflectIdx(idx, N int) int {
	if N == 1 {
		return 0
	}
	period := 2 * (N - 1)
	i := idx % period
	if i < 0 {
		i += period
	}
	if i >= N {
		return period - i
	}
	return i
}

// Band classifies peaks by the scale at which they appear.
type Band int

const (
	BandFine   Band = iota // scales 1–2: individual statements
	BandMedium             // scales 4–16: functions/types
	BandCoarse             // scales 32–128: file sections
)

func (b Band) String() string {
	switch b {
	case BandFine:
		return "fine"
	case BandMedium:
		return "medium"
	case BandCoarse:
		return "coarse"
	}
	return "unknown"
}

func scaleToBand(scale float64) Band {
	switch {
	case scale <= 2:
		return BandFine
	case scale <= 16:
		return BandMedium
	default:
		return BandCoarse
	}
}

// Peak represents a detected structural boundary in the signal.
type Peak struct {
	Line        int     // 1-indexed source line
	Coefficient float64 // signed CWT coefficient (magnitude = importance)
	Scale       float64 // wavelet scale at which this peak was detected
	Band        Band    // fine / medium / coarse
}

// DetectPeaks finds local maxima across all CWT scales and collapses
// cross-scale ridges: the same structural boundary typically appears as
// a peak at multiple scales, and we keep only the dominant one.
func DetectPeaks(cwt CWTResult, threshold float64, maxPeaks, ridgeWindow int) []Peak {
	if maxPeaks == 0 {
		maxPeaks = 250
	}
	if ridgeWindow == 0 {
		ridgeWindow = 2
	}

	var raw []Peak
	for si, coeffs := range cwt.Coefficients {
		N := len(coeffs)
		scale := cwt.Scales[si]
		for pos := range N {
			mag := math.Abs(coeffs[pos])
			if mag < threshold {
				continue
			}
			leftOK := pos == 0 || mag >= math.Abs(coeffs[pos-1])
			rightOK := pos == N-1 || mag > math.Abs(coeffs[pos+1])
			if leftOK && rightOK {
				raw = append(raw, Peak{
					Line:        pos + 1,
					Coefficient: coeffs[pos],
					Scale:       scale,
					Band:        scaleToBand(scale),
				})
			}
		}
	}

	sort.Slice(raw, func(i, j int) bool {
		return math.Abs(raw[i].Coefficient) > math.Abs(raw[j].Coefficient)
	})

	// Collapse cross-scale ridges: keep only the strongest peak within
	// ridgeWindow lines, since one structural feature creates peaks at
	// multiple scales.
	kept := make([]Peak, 0, maxPeaks)
	for _, p := range raw {
		overlap := false
		for _, k := range kept {
			if absInt(k.Line-p.Line) <= ridgeWindow {
				overlap = true
				break
			}
		}
		if !overlap {
			kept = append(kept, p)
		}
		if len(kept) >= maxPeaks {
			break
		}
	}

	return kept
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
