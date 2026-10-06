// Command gtx is the goterrace terminal multiplexer.
package main

import (
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/kazuph/goterrace/internal/tui"
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
			os.Exit(runDetect(args[1:]))
		}
	}

	code, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gtx:", err)
	}
	os.Exit(code)
}

func run() (int, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return 1, fmt.Errorf("stdin is not a terminal")
	}
	return tui.Run(os.Stdin, os.Stdout)
}

// runDetect loads agent manifests and reports the state of a captured screen.
// Implemented together with the detect package milestone.
func runDetect(_ []string) int {
	fmt.Fprintln(os.Stderr, "usage: gtx detect <screen-file>")
	return 2
}
