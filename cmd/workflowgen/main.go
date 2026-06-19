package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aneviaro/stratz-mcp/internal/workflowgen"
)

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	if err := workflowgen.Generate(*root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
