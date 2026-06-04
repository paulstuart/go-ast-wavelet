---
description: Analyze Go code complexity using wavelet transforms — call graph, refactor candidates, structural boundaries
argument-hint: <directory> [entry-function]
allowed-tools: [Bash, Read]
---

Analyze the Go package at: $ARGUMENTS

Parse the arguments: the first word is the directory (default to `.` if omitted), the second word is the entry function name (default `main`).

## Step 1 — Run the analysis

Use the `analyze_package` MCP tool if the go-ast-wavelet server is registered. Otherwise run:

```
go run github.com/paulstuart/go-ast-wavelet/cmd/mcp
```

as a stdio MCP server, OR if the binary is installed, call it directly. If neither is available, build it first:

```bash
go install github.com/paulstuart/go-ast-wavelet/cmd/mcp@latest
```

Then invoke the three tools in sequence:
1. `analyze_package(dir, entry)` — full picture
2. `refactor_candidates(dir, entry, min_entropy=1.0)` — what to fix
3. `structural_boundaries(dir, band="coarse")` — natural context chunks

## Step 2 — Interpret results

### Call Graph

Present a ranked table of functions. Highlight:
- **Top 3 by Complexity**: these dominate the program's total structural weight
- **Top 3 by Entropy**: these are the primary refactor candidates — high Entropy means a function's direct callees span a wide complexity range, which usually means it's doing too much or is missing an abstraction layer
- **Depth > 3**: deep call chains are latent complexity that's easy to miss in review

### Refactor Candidates

For each candidate explain *why* the Entropy score is high in terms of the actual code. A function that calls both a trivial `fmt.Println` and a complex goroutine/select block will score high — the fix is usually to extract the complex part.

### Structural Boundaries

Coarse boundaries (scale ≥ 32) are the natural split points for assembling context windows. Present them as line ranges: "Lines 1–24: package setup and simple functions. Lines 25–63: Worker type and concurrent logic."

These ranges are directly usable for `Read file_path offset=25 limit=38` calls when you need to pull specific sections into context without loading the full file.

## Step 3 — Recommendations

Provide 3–5 concrete, prioritized actions:
1. Name the specific function to refactor and suggest the extraction boundary
2. Identify the coarse section with the highest line-hotspot density as the highest-risk area
3. Note any functions at depth > 3 that should be documented or flattened

Keep the full response under 400 words unless the codebase is large (>20 functions).
