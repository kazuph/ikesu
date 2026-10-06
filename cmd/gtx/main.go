// Command gtx is the goterrace terminal multiplexer.
package main

import (
	"fmt"
	"os"
)

const version = "0.1.0"

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-version", "version":
			fmt.Printf("goterrace %s\n", version)
			return
		case "detect":
			// Filled in by the detect milestone.
			fmt.Fprintln(os.Stderr, "gtx detect: not implemented yet")
			os.Exit(2)
		}
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gtx:", err)
		os.Exit(1)
	}
}

func run() error {
	return fmt.Errorf("not implemented yet")
}
