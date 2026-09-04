package main

import (
	"fmt"
	"os"
)

// @TODO: version will be set at build time through ldflags, see the Makefile.
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("wrike-tui " + version)
		return
	}

	// @TODO: expand
	fmt.Println("wrike-tui " + version + " - nothing to see yet")
}
