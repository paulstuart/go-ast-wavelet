// go-ast-wavelet MCP server.
//
// Exposes three tools any MCP-compatible LLM can call:
//
//	analyze_package       — full call graph + complexity scores
//	refactor_candidates   — functions with high structural heterogeneity
//	structural_boundaries — natural split points by wavelet scale band
//
// Install:
//
//	go install github.com/paulstuart/go-ast-wavelet/cmd/mcp@latest
//
// Register in Claude Code (~/.claude/settings.json):
//
//	"mcpServers": {
//	  "go-ast-wavelet": {
//	    "command": "mcp",
//	    "args": [],
//	    "type": "stdio"
//	  }
//	}
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/paulstuart/go-ast-wavelet/astwavelet"
)

func main() {
	s := server.NewMCPServer("go-ast-wavelet", "0.1.0")

	s.AddTool(
		mcp.NewTool("analyze_package",
			mcp.WithDescription(
				"Analyze a Go package's structural complexity using wavelet transforms. "+
					"Returns a call graph rooted at the entry function with per-function "+
					"Complexity (total subtree energy) and Entropy (internal heterogeneity) "+
					"scores, plus per-line irregularity hotspots. "+
					"High Entropy on a function means its callees are structurally mixed — "+
					"a primary signal for refactoring. "+
					"Use this first to get an overview before drilling down.",
			),
			mcp.WithString("dir",
				mcp.Required(),
				mcp.Description("Absolute or relative path to the Go package directory to analyze."),
			),
			mcp.WithString("entry",
				mcp.Description("Name of the entry function to root the call graph. Defaults to 'main'."),
			),
		),
		handleAnalyzePackage,
	)

	s.AddTool(
		mcp.NewTool("refactor_candidates",
			mcp.WithDescription(
				"List functions that are strong refactoring candidates based on structural "+
					"heterogeneity (Entropy). A function with high Entropy has direct callees "+
					"that span a wide complexity range — it mixes trivial and complex operations, "+
					"which often signals a missing abstraction layer or a function doing too much. "+
					"Returns functions sorted by Entropy descending.",
			),
			mcp.WithString("dir",
				mcp.Required(),
				mcp.Description("Absolute or relative path to the Go package directory."),
			),
			mcp.WithString("entry",
				mcp.Description("Entry function name. Defaults to 'main'."),
			),
			mcp.WithNumber("min_entropy",
				mcp.Description("Minimum Entropy threshold to include. Defaults to 1.0."),
			),
		),
		handleRefactorCandidates,
	)

	s.AddTool(
		mcp.NewTool("structural_boundaries",
			mcp.WithDescription(
				"Identify natural structural boundaries in the source code using the Ricker "+
					"(Mexican hat) continuous wavelet transform at eight scales. "+
					"Boundaries are classified into three bands: "+
					"'fine' (individual statements, scales 1-2), "+
					"'medium' (functions/types, scales 4-16), "+
					"'coarse' (file sections, scales 32-128). "+
					"Coarse boundaries are the best split points when assembling context "+
					"chunks for LLM consumption — they mark where one coherent section "+
					"ends and another begins.",
			),
			mcp.WithString("dir",
				mcp.Required(),
				mcp.Description("Absolute or relative path to the Go package directory."),
			),
			mcp.WithString("band",
				mcp.Description("Filter to one band: 'fine', 'medium', or 'coarse'. Returns all bands if omitted."),
			),
		),
		handleStructuralBoundaries,
	)

	s.AddTool(
		mcp.NewTool("find_similar_functions",
			mcp.WithDescription(
				"Identify pairs of functions that are structurally similar and may be candidates "+
					"for consolidation into a single function. "+
					"Similarity is measured by comparing per-function Haar wavelet fingerprints: "+
					"each function's complexity signal is resampled to a fixed length, "+
					"decomposed into multi-scale detail coefficients, and compared via cosine similarity. "+
					"A score of 1.0 means identical structural rhythm; 0.9+ is a strong clone signal. "+
					"Note: this detects *structural* similarity (same pattern of control flow and complexity), "+
					"not semantic similarity. Two functions can be structurally identical but logically different. "+
					"Use this as a starting point for manual review, not a definitive consolidation plan.",
			),
			mcp.WithString("dir",
				mcp.Required(),
				mcp.Description("Absolute or relative path to the Go package directory."),
			),
			mcp.WithNumber("threshold",
				mcp.Description("Minimum cosine similarity to report (0.0–1.0). Defaults to 0.90. Lower values find looser matches."),
			),
		),
		handleFindSimilarFunctions,
	)

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("mcp server error: %v", err)
	}
}

func handleAnalyzePackage(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dir := req.GetString("dir", ".")
	entry := req.GetString("entry", "main")

	report, err := astwavelet.Analyze(filepath.Clean(dir), entry)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("analysis failed: %v", err)), nil
	}

	type outNode struct {
		Function   string  `json:"function"`
		File       string  `json:"file"`
		Line       int     `json:"line"`
		Depth      int     `json:"depth"`
		Complexity float64 `json:"complexity"`
		Entropy    float64 `json:"entropy"`
	}

	nodes := make([]outNode, len(report.CallGraph))
	for i, n := range report.CallGraph {
		nodes[i] = outNode{
			Function:   n.Name,
			File:       filepath.Base(n.File),
			Line:       n.Line,
			Depth:      n.Depth,
			Complexity: round2(n.Complexity),
			Entropy:    round2(n.Entropy),
		}
	}

	type outHotspot struct {
		Line         int     `json:"line"`
		Irregularity float64 `json:"irregularity"`
	}
	hotspots := make([]outHotspot, len(report.LineHotspots))
	for i, h := range report.LineHotspots {
		hotspots[i] = outHotspot{Line: h.Line, Irregularity: round2(h.Irregularity)}
	}

	out := map[string]any{
		"dir":          report.Dir,
		"entry_point":  report.EntryPoint,
		"call_graph":   nodes,
		"line_hotspots": hotspots,
		"interpretation": map[string]string{
			"complexity": "Total structural energy of the function plus everything it calls. Higher = more expensive subtree.",
			"entropy":    "Standard deviation of direct callees' complexity. Higher = more heterogeneous children = refactor signal.",
		},
	}
	return jsonResult(out)
}

func handleRefactorCandidates(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dir := req.GetString("dir", ".")
	entry := req.GetString("entry", "main")
	minEntropy := req.GetFloat("min_entropy", 1.0)

	report, err := astwavelet.Analyze(filepath.Clean(dir), entry)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("analysis failed: %v", err)), nil
	}

	type candidate struct {
		Function   string  `json:"function"`
		File       string  `json:"file"`
		Line       int     `json:"line"`
		Entropy    float64 `json:"entropy"`
		Complexity float64 `json:"complexity"`
		Reason     string  `json:"reason"`
	}

	var candidates []candidate
	for _, n := range report.CallGraph {
		if n.Entropy < minEntropy {
			continue
		}
		reason := "mixed-complexity callees"
		if n.Entropy > 10 {
			reason = "extreme complexity mismatch between callees — likely doing too much"
		} else if n.Entropy > 5 {
			reason = "high complexity spread across callees — consider extracting an abstraction"
		}
		candidates = append(candidates, candidate{
			Function:   n.Name,
			File:       filepath.Base(n.File),
			Line:       n.Line,
			Entropy:    round2(n.Entropy),
			Complexity: round2(n.Complexity),
			Reason:     reason,
		})
	}

	// Already sorted by Complexity; re-sort by Entropy for this view.
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && candidates[j].Entropy > candidates[j-1].Entropy; j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}

	out := map[string]any{
		"dir":         report.Dir,
		"min_entropy": minEntropy,
		"candidates":  candidates,
	}
	return jsonResult(out)
}

func handleStructuralBoundaries(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dir := req.GetString("dir", ".")
	bandFilter := req.GetString("band", "")

	report, err := astwavelet.Analyze(filepath.Clean(dir), "main")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("analysis failed: %v", err)), nil
	}

	type outBoundary struct {
		Line        int     `json:"line"`
		Band        string  `json:"band"`
		Scale       float64 `json:"scale"`
		Coefficient float64 `json:"coefficient"`
	}

	var boundaries []outBoundary
	for _, b := range report.Boundaries {
		if bandFilter != "" && b.Band != bandFilter {
			continue
		}
		boundaries = append(boundaries, outBoundary{
			Line:        b.Line,
			Band:        b.Band,
			Scale:       b.Scale,
			Coefficient: round2(b.Coeff),
		})
	}

	out := map[string]any{
		"dir":        report.Dir,
		"band_filter": bandFilter,
		"boundaries": boundaries,
		"interpretation": map[string]string{
			"fine":   "Individual statement boundaries (scales 1-2). Useful for precise line targeting.",
			"medium": "Function/type boundaries (scales 4-16). Natural units for code review.",
			"coarse": "Section boundaries (scales 32-128). Best split points for LLM context chunks.",
		},
	}
	return jsonResult(out)
}

func handleFindSimilarFunctions(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dir := req.GetString("dir", ".")
	threshold := req.GetFloat("threshold", 0.90)

	report, err := astwavelet.Analyze(filepath.Clean(dir), "main")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("analysis failed: %v", err)), nil
	}

	type outPair struct {
		FunctionA  string  `json:"function_a"`
		FunctionB  string  `json:"function_b"`
		Similarity float64 `json:"similarity"`
		Note       string  `json:"note"`
	}

	var pairs []outPair
	for _, p := range report.SimilarFunctions {
		if p.Similarity < threshold {
			continue
		}
		note := "moderate structural similarity — review manually"
		if p.Similarity >= 0.98 {
			note = "near-identical structure — strong consolidation candidate"
		} else if p.Similarity >= 0.95 {
			note = "very similar structure — likely consolidation candidate"
		} else if p.Similarity >= 0.90 {
			note = "similar structural rhythm — worth comparing"
		}
		pairs = append(pairs, outPair{
			FunctionA:  p.A,
			FunctionB:  p.B,
			Similarity: round2(p.Similarity),
			Note:       note,
		})
	}

	out := map[string]any{
		"dir":       report.Dir,
		"threshold": threshold,
		"pairs":     pairs,
		"caveat":    "Structural similarity measures control-flow pattern, not semantics. Always review before consolidating.",
	}
	return jsonResult(out)
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal error: %v", err)), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
