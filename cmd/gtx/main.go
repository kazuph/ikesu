// Command gtx is the goterrace terminal multiplexer.
package main

import (
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/kazuph/goterrace/internal/detect"
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

// runDetect loads a captured screen file and reports every agent's state.
func runDetect(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: gtx detect <screen-file>")
		return 2
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "gtx detect:", err)
		return 1
	}
	in := detect.Input{Screen: string(data)}
	fmt.Printf("%-16s %-8s %s\n", "agent", "state", "rule")
	for _, id := range detect.AgentIDs() {
		d := detect.Detect(id, in)
		rule := d.RuleID
		if rule == "" {
			rule = "-"
		}
		extra := ""
		if d.SkipStateUpdate {
			extra = " [skip_state_update]"
		}
		fmt.Printf("%-16s %-8s %s%s\n", id, d.State, rule, extra)
	}
	return 0
}
