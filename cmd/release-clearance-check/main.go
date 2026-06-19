package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aneviaro/stratz-mcp/internal/releasegate"
)

func main() {
	recordPath := flag.String("record", "docs/release-clearance.json", "path to the release-clearance record")
	flag.Parse()

	record, err := releasegate.Load(*recordPath)
	if err == nil {
		err = releasegate.Check(record)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("STRATZ release clearance approved")
}
