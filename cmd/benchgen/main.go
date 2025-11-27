// Command benchgen generates HTML benchmark reports.
package main

import (
	"fmt"
	"os"

	"github.com/edgedlt/hotstuff2/bench"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: benchgen <template> <bench_output> <output_html>")
		os.Exit(1)
	}

	templatePath := os.Args[1]
	benchOutputPath := os.Args[2]
	outputPath := os.Args[3]

	// Read benchmark output
	benchData, err := os.ReadFile(benchOutputPath)
	if err != nil {
		fmt.Printf("Error reading benchmark output: %v\n", err)
		os.Exit(1)
	}

	// Generate report
	if err := bench.GenerateHTMLReport(templatePath, outputPath, benchData); err != nil {
		fmt.Printf("Error generating report: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Report generated: %s\n", outputPath)
}
