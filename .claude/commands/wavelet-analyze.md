---
description: Analyze Go code complexity, dead code, clones, and engineering metrics via wavelet transforms
argument-hint: <directory> [entry-function]
allowed-tools: [Bash, Read]
---

Analyze the Go package at: $ARGUMENTS

Parse the arguments: first word = directory (default `.`), second word = entry function (default `main`).

## Available MCP tools

- `analyze_package` — full overview: call graph, complexity, entropy, line hotspots
- `refactor_candidates` — functions with high entropy (mixed-complexity callees)
- `structural_boundaries` — CWT boundaries by band for context chunking
- `find_similar_functions` — structural clone candidates (consolidation targets)
- `dead_code_analysis` — called / referenced-only / unreachable classification
- `engineering_metrics` — fan-in, exported API hotspots, complexity cliffs

If the server is not registered, install it first:

```bash
go install github.com/paulstuart/go-ast-wavelet/cmd/mcp@latest
```

## Analysis workflow

### 1. Start with `analyze_package`

Get the call graph. Note:

- **Top 3 by Complexity**: dominant subtrees — most expensive to change
- **Top 3 by Entropy**: refactor candidates — functions whose callees are structurally mixed
- **Depth > 3**: deep chains that hide complexity from callers

### 2. Run `dead_code_analysis`

Classify every function:

- **unreachable**: safe deletion candidates — verify with tests first
- **referenced-only**: the category Go's native tools miss — functions passed as values to `template.FuncMap`, `http.HandleFunc`, goroutine launchers, etc. These are alive at runtime even though no call site is visible. Do not delete without tracing the reference sites.
- **called**: reachable, keep

### 3. Run `engineering_metrics`

Three views:

- **Fan-in**: high fan-in + high complexity = most dangerous functions to change
- **Exported hotspots**: public API ranked by complexity — high scores burden every caller
- **Complexity cliffs**: `total_score / surface_score` is high — the caller sees a cheap function but one level down is a complexity explosion. Ratio ≥ 10 warrants a naming or doc change to make the cost visible.

### 4. Run `find_similar_functions`

Pairs above 0.90 cosine similarity share structural pattern. At 0.98+ they are near-identical. Before consolidating confirm compatible signatures and that the difference isn't intentional (e.g., one has error handling the other doesn't).

### 5. Use `structural_boundaries` for context assembly

Coarse boundaries (scale ≥ 32) are natural section dividers. Use them as `Read` call boundaries to load only the relevant section of a large file rather than the whole thing.

## Output format

Provide:

1. **Summary** (3 sentences): overall complexity level, biggest risk, top action item
2. **Refactor table**: top 5 functions by entropy with a one-line explanation each
3. **Dead code**: count of unreachable + list of referenced-only with their reference sites
4. **Engineering risks**: any fan-in > 3 or cliff ratio > 10, with specific recommendations
5. **Consolidation candidates**: similar function pairs with a proposed unified signature

Keep total response under 500 words unless the codebase has more than 30 functions.
