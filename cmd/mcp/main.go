// go-ast-wavelet MCP server.
//
// Exposes tools any MCP-compatible LLM can call to analyze Go code complexity.
//
// Install:
//
//	go install github.com/paulstuart/go-ast-wavelet/cmd/mcp@latest
//
// Register in Claude Code (~/.claude/settings.json):
//
//	"mcpServers": {
//	  "go-ast-wavelet": { "command": "mcp", "args": [], "type": "stdio" }
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
	s := server.NewMCPServer("go-ast-wavelet", "0.2.0")

	s.AddTool(mcp.NewTool("analyze_package",
		mcp.WithDescription(
			"Full wavelet analysis: call graph rooted at the entry function with per-function "+
				"Complexity (total subtree energy) and Entropy (internal heterogeneity) scores, "+
				"plus per-line irregularity hotspots. High Entropy = refactor signal. Use first.",
		),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Path to the Go package directory.")),
		mcp.WithString("entry", mcp.Description("Entry function name. Defaults to 'main'.")),
	), handleAnalyzePackage)

	s.AddTool(mcp.NewTool("refactor_candidates",
		mcp.WithDescription(
			"Functions with high structural heterogeneity (Entropy): their direct callees span a "+
				"wide complexity range, signalling a missing abstraction or a function doing too much.",
		),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Path to the Go package directory.")),
		mcp.WithString("entry", mcp.Description("Entry function name. Defaults to 'main'.")),
		mcp.WithNumber("min_entropy", mcp.Description("Minimum Entropy threshold. Defaults to 1.0.")),
	), handleRefactorCandidates)

	s.AddTool(mcp.NewTool("structural_boundaries",
		mcp.WithDescription(
			"Natural structural boundaries via Ricker CWT at eight scales. Bands: "+
				"fine (statements, 1-2), medium (functions, 4-16), coarse (sections, 32-128). "+
				"Coarse boundaries are best split points for LLM context chunks.",
		),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Path to the Go package directory.")),
		mcp.WithString("band", mcp.Description("Filter to 'fine', 'medium', or 'coarse'. All if omitted.")),
	), handleStructuralBoundaries)

	s.AddTool(mcp.NewTool("find_similar_functions",
		mcp.WithDescription(
			"Pairs of functions with similar Haar wavelet fingerprints — structural clone candidates. "+
				"Detects same control-flow pattern regardless of identifier names. "+
				"Score 1.0 = identical structure. Structural similarity only; verify semantics manually.",
		),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Path to the Go package directory.")),
		mcp.WithNumber("threshold", mcp.Description("Cosine similarity threshold (0–1). Defaults to 0.90.")),
	), handleFindSimilarFunctions)

	s.AddTool(mcp.NewTool("dead_code_analysis",
		mcp.WithDescription(
			"Classify every function as called, referenced-only, or unreachable. "+
				"'referenced' covers the case Go's native tools miss: functions passed as values "+
				"(template FuncMaps, http.HandleFunc callbacks, etc.) that look dead to the "+
				"compiler but are alive at runtime. 'unreachable' are genuine deletion candidates.",
		),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Path to the Go package directory.")),
		mcp.WithString("entry", mcp.Description("Entry function name. Defaults to 'main'.")),
	), handleDeadCodeAnalysis)

	s.AddTool(mcp.NewTool("engineering_metrics",
		mcp.WithDescription(
			"Derived call-graph metrics: fan-in (how many callers reach each function — "+
				"high fan-in + high complexity = fragile change target), exported API hotspots "+
				"(complexity burden on package consumers), and complexity cliffs (functions that "+
				"look cheap but trigger expensive subtrees — where surprising bugs hide).",
		),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Path to the Go package directory.")),
		mcp.WithString("entry", mcp.Description("Entry function name. Defaults to 'main'.")),
		mcp.WithNumber("cliff_ratio", mcp.Description("Min subtree/surface ratio for cliffs. Defaults to 5.0.")),
	), handleEngineeringMetrics)

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
		nodes[i] = outNode{n.Name, filepath.Base(n.File), n.Line, n.Depth, round2(n.Complexity), round2(n.Entropy)}
	}
	type outHotspot struct {
		Line         int     `json:"line"`
		Irregularity float64 `json:"irregularity"`
	}
	hotspots := make([]outHotspot, len(report.LineHotspots))
	for i, h := range report.LineHotspots {
		hotspots[i] = outHotspot{h.Line, round2(h.Irregularity)}
	}
	return jsonResult(map[string]any{
		"dir": report.Dir, "entry_point": report.EntryPoint,
		"call_graph": nodes, "line_hotspots": hotspots,
		"interpretation": map[string]string{
			"complexity": "Total structural energy of function + everything it calls.",
			"entropy":    "Std dev of direct callees' complexity. Higher = heterogeneous = refactor signal.",
		},
	})
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
		switch {
		case n.Entropy > 10:
			reason = "extreme mismatch — likely doing too much"
		case n.Entropy > 5:
			reason = "high spread — consider extracting an abstraction"
		}
		candidates = append(candidates, candidate{n.Name, filepath.Base(n.File), n.Line, round2(n.Entropy), round2(n.Complexity), reason})
	}
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && candidates[j].Entropy > candidates[j-1].Entropy; j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}
	return jsonResult(map[string]any{"dir": report.Dir, "min_entropy": minEntropy, "candidates": candidates})
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
		boundaries = append(boundaries, outBoundary{b.Line, b.Band, b.Scale, round2(b.Coeff)})
	}
	return jsonResult(map[string]any{
		"dir": report.Dir, "band_filter": bandFilter, "boundaries": boundaries,
		"interpretation": map[string]string{
			"fine": "Statement boundaries (1-2).", "medium": "Function/type boundaries (4-16).",
			"coarse": "Section boundaries (32-128). Best LLM context split points.",
		},
	})
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
		note := "similar structural rhythm — worth comparing"
		switch {
		case p.Similarity >= 0.98:
			note = "near-identical structure — strong consolidation candidate"
		case p.Similarity >= 0.95:
			note = "very similar structure — likely consolidation candidate"
		}
		pairs = append(pairs, outPair{p.A, p.B, round2(p.Similarity), note})
	}
	return jsonResult(map[string]any{
		"dir": report.Dir, "threshold": threshold, "pairs": pairs,
		"caveat": "Structural similarity only — verify semantics before consolidating.",
	})
}

func handleDeadCodeAnalysis(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dir := req.GetString("dir", ".")
	entry := req.GetString("entry", "main")
	report, err := astwavelet.Analyze(filepath.Clean(dir), entry)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("analysis failed: %v", err)), nil
	}
	type outRef struct {
		Name string `json:"name"`
		File string `json:"file"`
		Line int    `json:"line"`
	}
	toOut := func(refs []astwavelet.FuncRef) []outRef {
		out := make([]outRef, len(refs))
		for i, r := range refs {
			out[i] = outRef{r.Name, filepath.Base(r.File), r.Line}
		}
		return out
	}
	return jsonResult(map[string]any{
		"dir": report.Dir, "entry_point": report.EntryPoint,
		"called": toOut(report.DeadCode.Called), "referenced": toOut(report.DeadCode.Referenced),
		"unreachable": toOut(report.DeadCode.Unreachable),
		"interpretation": map[string]string{
			"called":      "Reachable via call edges. Safe to keep.",
			"referenced":  "Used as a value (callback, FuncMap, etc.) — may be alive at runtime. Verify before removing.",
			"unreachable": "Not called and not referenced. Deletion candidates.",
		},
	})
}

func handleEngineeringMetrics(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dir := req.GetString("dir", ".")
	entry := req.GetString("entry", "main")
	cliffRatio := req.GetFloat("cliff_ratio", 5.0)
	report, err := astwavelet.Analyze(filepath.Clean(dir), entry)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("analysis failed: %v", err)), nil
	}
	type fanInEntry struct {
		Function string `json:"function"`
		Callers  int    `json:"callers"`
	}
	var fanIn []fanInEntry
	for name, count := range report.FanIn {
		fanIn = append(fanIn, fanInEntry{name, count})
	}
	for i := 1; i < len(fanIn); i++ {
		for j := i; j > 0 && fanIn[j].Callers > fanIn[j-1].Callers; j-- {
			fanIn[j], fanIn[j-1] = fanIn[j-1], fanIn[j]
		}
	}
	type outNode struct {
		Function   string  `json:"function"`
		File       string  `json:"file"`
		Line       int     `json:"line"`
		Complexity float64 `json:"complexity"`
	}
	exported := make([]outNode, len(report.ExportedHotspots))
	for i, n := range report.ExportedHotspots {
		exported[i] = outNode{n.Name, filepath.Base(n.File), n.Line, round2(n.Complexity)}
	}
	cliffs := astwavelet.FindComplexityCliffs(report.CallGraph, cliffRatio)
	type outCliff struct {
		Function     string  `json:"function"`
		File         string  `json:"file"`
		Line         int     `json:"line"`
		SurfaceScore float64 `json:"surface_score"`
		TotalScore   float64 `json:"total_score"`
		Ratio        float64 `json:"ratio"`
		Note         string  `json:"note"`
	}
	var outCliffs []outCliff
	for _, c := range cliffs {
		note := "subtree significantly more expensive than surface"
		switch {
		case c.Ratio >= 20:
			note = "extreme cliff — callers see a one-liner but trigger massive complexity"
		case c.Ratio >= 10:
			note = "steep cliff — make complexity visible via naming or docs"
		}
		outCliffs = append(outCliffs, outCliff{c.Name, filepath.Base(c.File), c.Line,
			round2(c.SurfaceScore), round2(c.TotalScore), round2(c.Ratio), note})
	}
	return jsonResult(map[string]any{
		"dir": report.Dir, "fan_in": fanIn,
		"exported_hotspots": exported, "complexity_cliffs": outCliffs,
	})
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
