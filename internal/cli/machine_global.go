package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/kazuph/ikesu/internal/machine"
)

func MachineUsageError(out io.Writer, message string) {
	fmt.Fprintf(out, "error: %s\nrun 'ikesu --help' for usage\n", message)
}

// GlobalMachineError validates only invocations with a leading global
// machine option. The run subcommand retains its own --machine parser.
func GlobalMachineError(args []string) string {
	global := false
	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--machine" || strings.HasPrefix(arg, "--machine=") {
			global = true
			break
		}
		switch {
		case arg == "--session" || arg == "--remote":
			i += 2
		case arg == "--no-session" || strings.HasPrefix(arg, "--session=") || strings.HasPrefix(arg, "--remote="):
			i++
		default:
			return ""
		}
	}
	if !global {
		return ""
	}
	if MachineRemoteConflict(args) {
		for _, arg := range args {
			if arg == "--" {
				break
			}
			if arg == "--help" || arg == "-h" {
				return "--machine cannot be used with --remote"
			}
		}
		return "--remote can only be used with the default launch command"
	}
	count := 0
	valueError := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "--machine" {
			count++
			if i+1 == len(args) {
				valueError = "missing value for --machine"
				continue
			}
			i++
			if args[i] == "" || strings.HasPrefix(args[i], "-") {
				valueError = "--machine requires a saved machine label or id"
			}
		} else if strings.HasPrefix(arg, "--machine=") {
			count++
			if strings.TrimPrefix(arg, "--machine=") == "" {
				valueError = "--machine requires a saved machine label or id"
			}
		}
	}
	if count > 1 {
		return "--machine can only be specified once"
	}
	return valueError
}

// resolveForwardMachine keeps the global CLI's observed errors separate
// from catalog mutation and remote-run error contracts.
func resolveForwardMachine(profiles []machine.Profile, ref string) (machine.Profile, error) {
	var result machine.Profile
	count := 0
	for _, p := range profiles {
		if p.ID == ref || p.Label == ref {
			result = p
			count++
		}
	}
	if count == 0 {
		return machine.Profile{}, fmt.Errorf("machine %q was not found", ref)
	}
	if count > 1 {
		return machine.Profile{}, fmt.Errorf("machine label %q is ambiguous; use the profile id", ref)
	}
	if !result.Enabled {
		return machine.Profile{}, fmt.Errorf("machine %q is disabled; enable it before use", ref)
	}
	return result, nil
}
