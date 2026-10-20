package cli

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
)

const waitHelp = "ikesu wait commands:\n  ikesu wait output <pane_id> --match <text> [--source visible|recent|recent-unwrapped] [--lines N] [--timeout MS] [--regex] [--raw]\n  ikesu wait agent-status <pane_id> --status <idle|working|blocked|done|unknown> [--timeout MS]\n"

func (r *runner) waitCLI(args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(r.out(), waitHelp)
		return 0
	}
	switch args[0] {
	case "agent-status":
		return r.agentWaitCLI(args[1:])
	case "output":
		return r.waitOutputCLI(args[1:])
	default:
		return usageErr(r, "unknown wait command: "+args[0])
	}
}

func (r *runner) waitOutputCLI(args []string) int {
	if len(args) == 0 {
		return usageErr(r, "usage: ikesu wait output <pane_id> --match <text> [--source visible|recent|recent-unwrapped] [--lines N] [--timeout MS] [--regex] [--raw]")
	}
	strip := true
	p := api.PaneWaitForOutputParams{PaneID: args[0], Source: "recent_unwrapped", Match: api.OutputMatch{Type: "substring"}, StripAnsi: &strip}
	matched := false
	for i := 1; i < len(args); {
		flag := args[i]
		switch flag {
		case "--regex":
			p.Match.Type = "regex"
			i++
		case "--raw":
			strip = false
			i++
		case "--match", "--source", "--lines", "--timeout":
			value, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for "+flag)
			}
			switch flag {
			case "--match":
				p.Match.Value = value
				matched = true
			case "--source":
				source, err := parseReadSource(value)
				if err != nil || source == "detection" {
					return usageErr(r, "invalid read source: "+value)
				}
				p.Source = source
			case "--lines":
				n, err := parseU32Flag(flag, value)
				if err != nil {
					return usageErr(r, err.Error())
				}
				p.Lines = &n
			case "--timeout":
				n, err := parseU64Flag(flag, value)
				if err != nil {
					return usageErr(r, err.Error())
				}
				p.TimeoutMs = &n
			}
		default:
			return usageErr(r, "unknown option: "+flag)
		}
	}
	if !matched {
		return usageErr(r, "missing required --match")
	}
	return r.printResult("cli:wait:output", api.MethodPaneWaitForOutput, p)
}
