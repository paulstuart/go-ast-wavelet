package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"log"
	"sort"

	"github.com/paulstuart/go-ast-wavelet/astwavelet"
)

// Mock Go source code containing standard functions and highly nested logic
const demoSource = `
package main

import "fmt"

func SimpleFunction() {
	fmt.Println("I am quite straightforward.")
}

func GnarlyFunction(items []int) {
	for _, item := range items {
		if item > 0 {
			switch item {
			case 42:
				go func() {
					if true {
						fmt.Println("Deeply nested complexity spike!")
					}
				}()
			default:
				fmt.Println(item)
			}
		}
	}
}
`

type ComplexityHotspot struct {
	NodeType string
	Line     int
	Detail   float64
}

func main() {
	fset := token.NewFileSet()

	// 1. Parse the target code into a standard Go AST
	file, err := parser.ParseFile(fset, "demo.go", demoSource, 0)
	if err != nil {
		log.Fatalf("Failed to parse source: %v", err)
	}

	// 2. Wrap into a Wavelet tree structures
	root := astwavelet.BuildWaveletTree(file)

	// 3. Execute the wavelet decomposition transform
	root.Transform()

	// 4. Collect and rank nodes by their Detail Coefficients (High-Pass spikes)
	var hotspots []ComplexityHotspot
	var collectHotspots func(*astwavelet.WaveletNode)
	
	collectHotspots = func(wn *astwavelet.WaveletNode) {
		if wn == nil {
			return
		}
		if wn.Detail > 0.1 {
			pos := fset.Position(wn.ASTNode.Pos())
			hotspots = append(hotspots, ComplexityHotspot{
				NodeType: wn.Type,
				Line:     pos.Line,
				Detail:   wn.Detail,
			})
		}
		for _, child := range wn.Children {
			collectHotspots(child)
		}
	}
	collectHotspots(root)

	// Sort matching hotspots so the highest entropy blocks surface first
	sort.Slice(hotspots, func(i, j int) bool {
		return hotspots[i].Detail > hotspots[j].Detail
	})

	// 5. Output the results
	fmt.Println("--- CODE ANALYSIS COMPLEXITY HEATMAP ---")
	fmt.Printf("%-20s \t %-5s \t %-10s\n", "AST Node Type", "Line", "Detail (Entropy)")
	fmt.Println("-------------------------------------------------------")
	for _, spot := range hotspots {
		fmt.Printf("%-20s \t L%-5d \t %.4f\n", spot.NodeType, spot.Line, spot.Detail)
	}
}
