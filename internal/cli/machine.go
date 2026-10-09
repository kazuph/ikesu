package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kazuph/goterrace/internal/machine"
)

const machineHelp = `Usage:
  gtx machine list [--json]
  gtx machine add <ssh-target> --label <label> [--remote-session <name>]
  gtx machine rename <label-or-id> --label <label>
  gtx machine remove <label-or-id>
  gtx machine enable <label-or-id>
  gtx machine disable <label-or-id>

Saved machines contain only a label, SSH target, explicit gtx session, and enabled state.
SSH credentials and key material remain owned by OpenSSH.
Adding probes SSH reachability first; unreachable machines are not saved.
Removing or disabling a machine leaves its remote sessions running.
`

func (r *runner) catalog() machine.Catalog {
	return machine.Catalog{Path: filepath.Join(filepath.Dir(r.socketPath()), "machines.json")}
}

func (r *runner) machineCLI(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(r.errOut(), machineHelp)
		return 2
	}
	for i, arg := range args {
		if (i == 0 && arg == "help") || arg == "--help" || arg == "-h" {
			fmt.Fprint(r.out(), machineHelp)
			return 0
		}
	}
	c := r.catalog()
	if args[0] == "list" {
		profiles := c.Load()
		if len(args) == 2 && args[1] == "--json" {
			b, _ := json.MarshalIndent(profiles, "", "  ")
			fmt.Fprintln(r.out(), string(b))
			return 0
		}
		if len(args) != 1 {
			fmt.Fprintln(r.errOut(), "unknown machine list option")
			return 2
		}
		if len(profiles) == 0 {
			fmt.Fprintln(r.out(), "No saved SSH machines.")
		}
		for _, p := range profiles {
			state := "disabled"
			if p.Enabled {
				state = "enabled"
			}
			fmt.Fprintf(r.out(), "%s\t%s\t%s\t%s\t%s\n", p.ID, p.Label, p.Target, p.Session, state)
		}
		return 0
	}
	if len(args) < 2 {
		fmt.Fprint(r.errOut(), machineHelp)
		return 2
	}
	action, ref := args[0], args[1]
	if action != "add" && action != "rename" && action != "remove" && action != "enable" && action != "disable" {
		fmt.Fprint(r.errOut(), machineHelp)
		return 2
	}
	label, session := "", "default"
	for i := 2; i < len(args); {
		flag := args[i]
		if flag != "--label" && (flag != "--remote-session" || action != "add") {
			fmt.Fprintf(r.errOut(), "unknown machine %s option: %s\n", action, flag)
			return 2
		}
		v, ok := flagValue(args, &i)
		if !ok {
			fmt.Fprintf(r.errOut(), "missing value for %s\n", flag)
			return 2
		}
		if flag == "--label" {
			label = v
		} else {
			session = v
		}
	}
	var p machine.Profile
	var err error
	if action == "add" {
		if err = machine.ValidateAdd(c.Load(), ref, label, session); err != nil {
			fmt.Fprintln(r.errOut(), "error:", err)
			return 2
		}
		p, err = c.Add(context.Background(), ref, label, session)
	} else {
		if action == "rename" && label == "" {
			fmt.Fprintln(r.errOut(), "error: --label is required")
			return 2
		}
		if action != "rename" && len(args) != 2 {
			fmt.Fprintln(r.errOut(), "unexpected machine option")
			return 2
		}
		p, err = c.Change(ref, action, label, nil)
	}
	if err != nil {
		fmt.Fprintln(r.errOut(), "error:", err)
		return 1
	}
	verbs := map[string]string{"add": "Saved", "rename": "Renamed", "remove": "Removed", "enable": "Enabled", "disable": "Disabled"}
	fmt.Fprintf(r.out(), "%s SSH machine %s.\n", verbs[action], p.ID)
	return 0
}

// ForwardMachine finishes a global machine invocation before any local API dispatch.
func ForwardMachine(ref string, args []string, o Options) int {
	r := &runner{o: o}
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return r.helpCLI()
		}
	}
	if len(args) == 0 {
		fmt.Fprintln(r.errOut(), "--machine requires agent, pane, workspace or worktree")
		return 2
	}
	for _, arg := range args {
		if arg == "--remote" || strings.HasPrefix(arg, "--remote=") {
			fmt.Fprintln(r.errOut(), "--machine cannot be combined with --remote")
			return 2
		}
	}
	switch args[0] {
	case "agent", "pane", "workspace", "worktree":
	default:
		fmt.Fprintln(r.errOut(), "unsupported --machine command:", args[0])
		return 2
	}
	p, err := machine.Resolve(r.catalog().Load(), ref, true)
	if err != nil {
		fmt.Fprintln(r.errOut(), "error:", err)
		return 2
	}
	bin := o.getenv("GTX_MACHINE_REMOTE_BINARY")
	if bin == "" {
		bin = "gtx"
	}
	cmd := exec.Command("ssh", machine.ForwardArgs(p, bin, args)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, r.out(), r.errOut()
	if err := cmd.Run(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			if e.ExitCode() == 255 {
				fmt.Fprintln(r.errOut(), "SSH connection failed; no local command was executed")
				return 1
			}
			return e.ExitCode()
		}
		fmt.Fprintln(r.errOut(), "SSH forwarding failed:", err)
		return 1
	}
	return 0
}

func MachineRemoteConflict(args []string) bool {
	remote, saved := false, false
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--remote" || strings.HasPrefix(arg, "--remote=") {
			remote = true
		}
		if arg == "--machine" || strings.HasPrefix(arg, "--machine=") {
			saved = true
		}
	}
	return remote && saved
}
