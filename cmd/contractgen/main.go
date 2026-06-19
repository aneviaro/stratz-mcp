package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aneviaro/stratz-mcp/internal/contractgen"
)

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()

	if err := contractgen.Generate(*root); err != nil {
		fmt.Fprintf(os.Stderr, "generate public contracts: %v\n", err)
		os.Exit(1)
	}
}
