# go-ast-wavelet

Multi-resolution structural analysis of Go codebases using wavelet transforms applied to abstract syntax trees. Surfaces complexity hotspots, structural boundaries, dead code, refactoring candidates, and engineering risk metrics — and exposes everything as an MCP server any LLM can call.

Inspired by [WaveScope](https://yogthos.net/posts/2026-06-02-wavescope.html) and [wavescope-mcp](https://github.com/yogthos/wavescope-mcp), which apply wavelet analysis to source text for LLM context efficiency. This project extends the idea to the structured domain of Go ASTs and semantic call graphs.

---

## Why wavelets on code?

Traditional complexity metrics (cyclomatic complexity, lines of code, nesting depth) are single-scale: they give one number per function. Wavelet analysis is inherently *multi-resolution*: the same signal viewed at scale 2 shows individual statement boundaries; at scale 16 it shows function-level structure; at scale 64 it shows file-section architecture. Each scale reveals different patterns.

Applied to a codebase, this lets you ask:

- *Fine scale*: which statements are structural outliers?
- *Medium scale*: which functions are internally heterogeneous (a refactoring signal)?
- *Coarse scale*: where are the natural section boundaries for chunking code into LLM context windows?

---

## Architecture

```
Go Source Files
       │
  go/packages ──────── multi-package loader (follows imports within module)
       │
  FuncIndex + []*ast.File
       │
       ├─── Call Graph ─────────────────────────────────────────────────┐
       │    BuildCallGraph("main", index, fset)                         │
       │         │                                                       │
       │    Haar tree transform (bottom-up)                             │
       │    Approximation = subtree energy                              │
       │    Detail        = sibling heterogeneity                       │
       │         │                                                       │
       │    ┌────┴──────────────────────┐                              │
       │    │ Fan-in · Cliffs · Exports │                              │
       │    └───────────────────────────┘                              │
       │                                                                │
       │    Dead Code ──────────────── reachable via call edges        │
       │    computeDeadCode()           referenced as values (FuncMap) │
       │                                unreachable                    │
       │                                                                │
       └─── Per-Line Signal ────────────────────────────────────────────┘
            BuildLineSignal(file, fset)
                 │
                 ├── Ricker CWT ── 8 scales ── peak detection ── band classification
                 │   fine (1-2) · medium (4-16) · coarse (32-128)
                 │
                 └── Haar DWT ── multi-level ── per-line irregularity back-projection
                      │
                 Function fingerprints ── cosine similarity ── structural clones
```

---

## Analysis capabilities

### Call graph complexity

Builds a call graph rooted at the entry point (typically `main`), following call edges across all packages in the module. Each function node carries two wavelet coefficients after a bottom-up [Haar transform](https://en.wikipedia.org/wiki/Haar_wavelet):

- **Approximation** (low-pass): total structural energy of the function plus everything it transitively calls. A high score means an expensive subtree — the most important functions to understand before making changes.
- **Detail** (high-pass): population standard deviation of direct callees' approximations. A high score means the function's children are structurally heterogeneous — some trivial, some complex. This is the primary refactoring signal: the function is likely doing too many things at different levels of abstraction.

The call graph loader uses [`golang.org/x/tools/go/packages`](https://pkg.go.dev/golang.org/x/tools/go/packages) to follow imports across the module boundary. Name-based call resolution handles the common cases; the resolution step is designed as a seam for upgrading to full type-aware analysis via [`golang.org/x/tools/go/callgraph`](https://pkg.go.dev/golang.org/x/tools/go/callgraph).

### Structural boundary detection

Converts each source file to a per-line numeric signal by summing the structural weight of every AST node whose position falls on that line. This is richer than keyword-weight scoring (the approach used by WaveScope) because it uses the actual parsed structure rather than text patterns.

The [Ricker (Mexican hat) wavelet](https://en.wikipedia.org/wiki/Mexican_hat_wavelet) is then convolved with the signal at eight scales (1, 2, 4, 8, 16, 32, 64, 128 lines) using the Continuous Wavelet Transform. Local maxima after cross-scale ridge collapse are classified into three bands:

| Band | Scales | Meaning |
|------|--------|---------|
| fine | 1–2 | Individual statement boundaries |
| medium | 4–16 | Function and type boundaries |
| coarse | 32–128 | Section-level architecture |

Coarse boundaries are the most useful for LLM context assembly: they mark where one coherent unit of the codebase ends and another begins, giving clean split points for `Read` operations.

### Per-line irregularity

A multi-level [Haar DWT](https://en.wikipedia.org/wiki/Discrete_wavelet_transform) (up to 8 levels) on the per-line signal produces detail coefficients at each scale. These are back-projected to individual lines: a line with high irregularity sits in a region that changes structural character at one or more scales. These are natural focal points for code review — lines that sit at the intersection of multiple structural transitions.

### Dead code detection

Classifies every function in the package into three categories by traversing the call graph from the entry point:

**Called** — reachable via direct call edges. Safe to keep.

**Referenced** — this is the category Go's native dead code tools miss. Functions that appear as *values* in reachable code — registered in `template.FuncMap`, passed to `http.HandleFunc`, assigned to function-typed variables, used as goroutine targets — are alive at runtime but have no visible call site. A two-pass scan identifies them: pass one collects all source positions that are in "call position" (the `Fun` field of a `CallExpr`); pass two finds identifiers at *other* positions that name functions in the index.

**Unreachable** — not called and not referenced from the entry point. Genuine deletion candidates.

The referenced/unreachable distinction matters most in codebases that use the standard library's `text/template` or `html/template` packages, where function maps are built at runtime and the compiler cannot trace the invocation.

### Structural clone detection

Each function's source lines are extracted as a 1D complexity signal, resampled to 32 points for length-independent comparison, and then Haar-decomposed at 5 levels. The concatenated detail coefficients (31 values) are L2-normalized to a unit vector — a multi-scale structural fingerprint.

[Cosine similarity](https://en.wikipedia.org/wiki/Cosine_similarity) between pairs of fingerprints measures structural resemblance: how similar is the rhythm of control flow and complexity changes, independent of identifier names or exact code. A score ≥ 0.90 indicates similar structural rhythm; ≥ 0.98 indicates near-identical pattern.

This detects *structural clones* — functions that follow the same pattern of simple and complex operations. It does not detect semantic clones (functions that do the same thing through structurally different code). For full clone detection, structural fingerprinting can serve as a fast pre-filter before more expensive semantic analysis.

For background on code clone taxonomy and detection approaches, see:

- Roy, C.K., Cordy, J.R., Koschke, R. (2009). [Comparison and Evaluation of Code Clone Detection Techniques and Tools](https://doi.org/10.1016/j.jss.2009.06.007). *Journal of Systems and Software*.
- Jiang, L. et al. (2007). [DECKARD: Scalable and Accurate Tree-based Detection of Code Clones](https://doi.org/10.1109/ICSE.2007.30). *ICSE*. (AST-based approach most similar to this one.)

### Engineering metrics

Derived from the call graph in a single pass:

**Fan-in** — how many distinct call sites within the call graph reach each function. High fan-in combined with high complexity identifies the most dangerous functions to change: any bug propagates to all callers. This is related to the coupling/cohesion analysis introduced by [Stevens, Myers, and Constantine (1974)](https://doi.org/10.1147/sj.132.0115).

**Exported API hotspots** — the exported subset of the call graph ranked by total complexity. High-complexity exported functions place a cognitive and reliability burden on every consumer of the package.

**Complexity cliffs** — functions where the ratio of total subtree complexity to body-only complexity is high. The caller sees a short, apparently cheap function; one call deeper is an expensive subtree. These are where "simple" changes cause surprising failures. The cliff ratio is `Approximation / LocalComplexity`; ratios ≥ 5 are reported, ≥ 10 warrant documentation or renaming to make the cost visible.

---

## MCP server

The `cmd/mcp` binary exposes all analysis capabilities as [Model Context Protocol](https://modelcontextprotocol.io/) tools, callable by any MCP-compatible LLM (Claude, Cursor, etc.).

### Install

```bash
go install github.com/paulstuart/go-ast-wavelet/cmd/mcp@latest
```

### Register with Claude Code

Add to `~/.claude/settings.json`:

```json
{
  "mcpServers": {
    "go-ast-wavelet": {
      "command": "mcp",
      "args": [],
      "type": "stdio"
    }
  }
}
```

### Tools

#### `analyze_package`

Full analysis: call graph with Complexity and Entropy per function, plus per-line irregularity hotspots.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `dir` | string | required | Path to Go package directory |
| `entry` | string | `"main"` | Entry function name |

#### `refactor_candidates`

Functions whose direct callees span a wide complexity range (high Entropy).

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `dir` | string | required | Path to Go package directory |
| `entry` | string | `"main"` | Entry function name |
| `min_entropy` | float | `1.0` | Minimum Entropy to include |

#### `structural_boundaries`

Natural structural transitions detected by Ricker CWT. Useful for splitting large files into coherent context chunks.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `dir` | string | required | Path to Go package directory |
| `band` | string | all | Filter to `"fine"`, `"medium"`, or `"coarse"` |

#### `find_similar_functions`

Pairs of functions with similar Haar wavelet fingerprints — structural clone candidates.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `dir` | string | required | Path to Go package directory |
| `threshold` | float | `0.90` | Cosine similarity threshold (0–1) |

#### `dead_code_analysis`

Classifies every function as `called`, `referenced` (value use — FuncMap, callback), or `unreachable`.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `dir` | string | required | Path to Go package directory |
| `entry` | string | `"main"` | Entry function name |

#### `engineering_metrics`

Fan-in, exported API hotspots, and complexity cliffs.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `dir` | string | required | Path to Go package directory |
| `entry` | string | `"main"` | Entry function name |
| `cliff_ratio` | float | `5.0` | Minimum subtree/surface ratio to report |

---

## Claude Code slash command

A `/wavelet-analyze` command is included at `.claude/commands/wavelet-analyze.md`. It instructs Claude to run all six tools in sequence, interpret the results, and produce a structured report with refactoring priorities, dead code findings, engineering risks, and consolidation candidates.

Usage:

```
/wavelet-analyze ./path/to/package [entry-function]
```

---

## Package structure

```
astwavelet/
  analyze.go       — Analyze() entry point; Report struct
  callgraph.go     — LoadPackage, BuildCallGraph, FuncIndex
  cwt.go           — Ricker CWT, peak detection, band classification
  haar.go          — multi-level Haar DWT, per-line irregularity
  metrics.go       — fan-in, exported hotspots, complexity cliffs
  reachability.go  — dead code classification with value-reference tracking
  signal.go        — per-line complexity signal from AST positions
  similarity.go    — Haar fingerprints, cosine similarity, clone detection
  transform.go     — WaveletNode tree, BaselineComplexity, tree Haar transform

cmd/mcp/
  main.go          — MCP server (6 tools)

samples/simple/    — demo package with multi-package structure
```

---

## Further reading

### Wavelet analysis

- Mallat, S. (1989). [A Theory for Multiresolution Signal Decomposition: The Wavelet Representation](https://doi.org/10.1109/34.192463). *IEEE TPAMI*. — The foundational multiresolution paper.
- Torrence, C. and Compo, G.P. (1998). [A Practical Guide to Wavelet Analysis](https://doi.org/10.1175/1520-0477(1998)079%3C0061:APGTWA%3E2.0.CO;2). *BAMS*. — The most accessible practical reference for CWT implementation.
- Daubechies, I. (1992). [Ten Lectures on Wavelets](https://doi.org/10.1137/1.9781611970104). SIAM. — The standard graduate reference.
- [Haar wavelet — Wikipedia](https://en.wikipedia.org/wiki/Haar_wavelet) — accessible introduction to the DWT used here.
- [Mexican hat wavelet — Wikipedia](https://en.wikipedia.org/wiki/Mexican_hat_wavelet) — the Ricker wavelet used for CWT boundary detection.

### Code complexity

- McCabe, T.J. (1976). [A Complexity Measure](https://doi.org/10.1109/TSE.1976.233837). *IEEE TSE*. — Cyclomatic complexity; the baseline against which structural wavelet analysis is an improvement.
- Halstead, M.H. (1977). *Elements of Software Science*. Elsevier. — Halstead complexity metrics; another single-scale predecessor.

### Code clones

- Roy, C.K., Cordy, J.R., Koschke, R. (2009). [Comparison and Evaluation of Code Clone Detection Techniques and Tools](https://doi.org/10.1016/j.jss.2009.06.007). *JSS*. — Survey of the four clone types (exact, renamed, near-miss, semantic).
- Jiang, L. et al. (2007). [DECKARD: Scalable and Accurate Tree-based Detection of Code Clones](https://doi.org/10.1109/ICSE.2007.30). *ICSE*. — Tree-based clone detection most similar to the fingerprinting approach here.
- Kamiya, T. et al. (2002). [CCFinder: A Multilinguistic Token-based Code Clone Detection System](https://doi.org/10.1109/TSE.2002.1019480). *IEEE TSE*. — Token-based approach; a complement to structural fingerprinting.

### Call graphs and program analysis

- Ryder, B.G. (1979). [Constructing the Call Graph of a Program](https://doi.org/10.1109/TSE.1979.234183). *IEEE TSE*. — Original call graph construction algorithms.
- Grove, D. and Chambers, C. (2001). [A Framework for Call Graph Construction Algorithms](https://doi.org/10.1145/503502.503504). *TOPLAS*. — Compares CHA, RTA, and other call graph algorithms.
- [`golang.org/x/tools/go/callgraph`](https://pkg.go.dev/golang.org/x/tools/go/callgraph) — the Go implementation of CHA/RTA/pointer-analysis call graphs; the natural upgrade path from the name-based resolution used here.

### Software metrics and coupling

- Stevens, W., Myers, G., and Constantine, L. (1974). [Structured Design](https://doi.org/10.1147/sj.132.0115). *IBM Systems Journal*. — Introduced coupling and cohesion; the conceptual foundation for fan-in analysis.
- Card, D.N. and Glass, R.L. (1990). *Measuring Software Design Quality*. Prentice Hall. — Covers fan-in/fan-out and their relationship to defect rates.

### LLM context and code navigation

- [WaveScope blog post](https://yogthos.net/posts/2026-06-02-wavescope.html) — the direct inspiration; explains the multi-resolution LLM navigation concept.
- [wavescope-mcp](https://github.com/yogthos/wavescope-mcp) — the original TypeScript MCP implementation using text-based signals.
- [Model Context Protocol specification](https://modelcontextprotocol.io/specification) — MCP protocol reference.
- [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) — the Go MCP library used by this project.
