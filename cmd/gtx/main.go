// Command gtx is the goterrace terminal multiplexer.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/cli"
	"github.com/kazuph/goterrace/internal/client"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/detect"
	"github.com/kazuph/goterrace/internal/server"
	"github.com/kazuph/goterrace/internal/tui"
)

const version = "0.1.0"

func main() {
	api.ConfigureExecutable(os.Args[0])
	args := os.Args[1:]
	if message := cli.GlobalMachineError(args); message != "" {
		cli.MachineUsageError(os.Stderr, message)
		os.Exit(2)
	}
	if cli.MachineRemoteConflict(args) {
		fmt.Fprintln(os.Stderr, "gtx: --machine cannot be combined with --remote")
		os.Exit(2)
	}

	// Global flags may precede any subcommand.
	session := api.DefaultSession
	explicitSession := false
	noSession := false
	machineRef := ""
	machineRequested := false
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
		case args[0] == "--machine":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "gtx: --machine requires a name")
				os.Exit(2)
			}
			machineRequested = true
			machineRef = args[1]
			args = args[2:]
		case strings.HasPrefix(args[0], "--machine="):
			machineRequested = true
			machineRef = strings.TrimPrefix(args[0], "--machine=")
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

	if machineRequested {
		if machineRef == "" {
			fmt.Fprintln(os.Stderr, "gtx: --machine requires a name")
			os.Exit(2)
		}
		os.Exit(cli.ForwardMachine(machineRef, args, cli.Options{Session: session, Explicit: explicitSession}))
	}

	if len(args) > 0 {
		switch args[0] {
		case "__endpoint":
			if len(args) != 1 || noSession {
				fmt.Fprintln(os.Stderr, "gtx: invalid private endpoint invocation")
				os.Exit(2)
			}
			if err := client.Relay(session, os.Stdin, os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, "gtx:", err)
				os.Exit(1)
			}
			return
		case "--default-config":
			fmt.Print(config.DefaultExample)
			return
		case "--version", "-version", "version":
			fmt.Printf("goterrace %s\n", version)
			return
		case "detect":
			os.Exit(runDetect(args[1:]))
		case "server":
			if len(args) == 2 && args[1] == "--handoff-import" {
				code, err := server.RunHandoffImport(session, version)
				if err != nil {
					fmt.Fprintln(os.Stderr, "gtx handoff import:", err)
				}
				os.Exit(code)
			}
			// `gtx server` runs the server; `gtx server <sub>` is the CLI.
			if len(args) == 1 {
				if client.NestedBlocked() {
					fmt.Fprintln(os.Stderr, "gtx: refusing to start a nested gtx server (a gtx server is an ancestor; set [experimental] allow_nested = true to permit)")
					os.Exit(1)
				}
				code, err := server.Run(session, version)
				if err != nil {
					fmt.Fprintln(os.Stderr, "gtx server:", err)
				}
				os.Exit(code)
			}
			fallthrough
		case "status", "guard", "decision", "config", "wait", "plugin", "pane", "workspace", "worktree", "tab",
			"send", "inbox", "msg", "run", "log", "agent", "machine",
			"help", "--help", "-h", "__pane-run":
			os.Exit(cli.Run(args, cli.Options{
				Session:  session,
				Version:  version,
				Explicit: explicitSession,
			}))
		}
	}

	code, err := run(noSession, session)
	if errors.Is(err, tui.ErrStandaloneRestart) {
		exe, e := os.Executable()
		if e == nil {
			e = syscall.Exec(exe, []string{os.Args[0], "--no-session"}, os.Environ())
		}
		fmt.Fprintln(os.Stderr, "gtx restart:", e)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gtx:", err)
	}
	os.Exit(code)
}

func run(noSession bool, session string) (int, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return 1, fmt.Errorf("stdin is not a terminal")
	}
	if client.NestedBlocked() {
		return 1, fmt.Errorf("refusing to start a nested gtx session (a gtx server is an ancestor; set [experimental] allow_nested = true to permit)")
	}
	if noSession {
		return tui.Run(os.Stdin, os.Stdout, version)
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
