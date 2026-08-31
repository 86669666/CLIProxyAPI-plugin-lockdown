// Command validate_models validates an embedded models catalog file.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

func main() {
	var inputPath string
	flag.StringVar(&inputPath, "file", "", "Models catalog JSON file")
	flag.Parse()

	if strings.TrimSpace(inputPath) == "" {
		fmt.Fprintln(os.Stderr, "error: --file is required")
		os.Exit(2)
	}
	data, errRead := os.ReadFile(inputPath)
	if errRead != nil {
		fmt.Fprintf(os.Stderr, "error: read %s: %v\n", inputPath, errRead)
		os.Exit(1)
	}
	if errValidate := registry.ValidateModelsJSON(data); errValidate != nil {
		fmt.Fprintf(os.Stderr, "error: invalid models catalog %s: %v\n", inputPath, errValidate)
		os.Exit(1)
	}
	fmt.Printf("Validated models catalog: %s\n", inputPath)
}
