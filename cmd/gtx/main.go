// Command gtx is the goterrace terminal multiplexer.
package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/cli"
	"github.com/kazuph/goterrace/internal/client"
	"github.com/kazuph/goterrace/internal/detect"
	"github.com/kazuph/goterrace/internal/server"
	"github.com/kazuph/goterrace/internal/tui"
)

const version = "0.1.0"

func main() {
	args := os.Args[1:]

	// Global flags may precede any subcommand.
	session := api.DefaultSession
	explicitSession := false
	noSession := false
	for len(args) > 0 {
		switch {
		case args[0] == "--session":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "gtx: --session requires a name")
				os.Exit(2)
			}
			session = args[1]
			explicitSession = true
			args = args[2:]
		case strings.HasPrefix(args[0], "--session="):
			session = strings.TrimPrefix(args[0], "--session=")
			explicitSession = true
			args = args[1:]
		case args[0] == "--no-session":
			noSession = true
			args = args[1:]
		default:
			goto dispatch
		}
	}
dispatch:
	if !api.ValidSessionName(session) {
		fmt.Fprintf(os.Stderr, "gtx: invalid session name %q\n", session)
		os.Exit(2)
	}

	if len(args) > 0 {
		switch args[0] {
		case "--version", "-version", "version":
			fmt.Printf("goterrace %s\n", version)
			return
		case "detect":
			os.Exit(runDetect(args[1:]))
		case "server":
			// `gtx server` runs the server; `gtx server <sub>` is the CLI.
			if len(args) == 1 {
				code, err := server.Run(session, version)
				if err != nil {
					fmt.Fprintln(os.Stderr, "gtx server:", err)
				}
				os.Exit(code)
			}
			fallthrough
		case "pane", "workspace", "tab":
			os.Exit(cli.Run(args, cli.Options{
				Session:  session,
				Explicit: explicitSession,
			}))
		}
	}

	code, err := run(noSession, session)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gtx:", err)
	}
	os.Exit(code)
}

func run(noSession bool, session string) (int, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return 1, fmt.Errorf("stdin is not a terminal")
	}
	if noSession {
		return tui.Run(os.Stdin, os.Stdout)
	}
	return client.Attach(session, os.Stdin, os.Stdout)
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
