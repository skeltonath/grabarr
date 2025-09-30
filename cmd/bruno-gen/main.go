package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"grabarr/internal/brunogen"
)

func main() {
	outputDir := flag.String("output", "bruno_auto", "Output directory for generated Bruno collection")
	baseURL := flag.String("base-url", "{{baseUrl}}", "Base URL for API requests")
	apiDir := flag.String("api-dir", "internal/api", "Directory containing API handler files")
	flag.Parse()

	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	generator := brunogen.NewGenerator(*outputDir, *baseURL, *apiDir)

	if err := generator.Generate(); err != nil {
		log.Fatalf("Failed to generate Bruno collection: %v", err)
	}

	fmt.Printf("✓ Successfully generated Bruno collection in %s/\n", *outputDir)
}
