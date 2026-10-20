package cli

import (
	"strconv"

	"github.com/kazuph/ikesu/internal/api"
)

func (r *runner) agentPromptCLI(args []string) int {
	if len(args) < 2 {
		return usageErr(r, "usage: ikesu agent prompt <target> <text> [--wait] [--until STATUS]... [--timeout MS]")
	}
	p := api.AgentPromptParams{Target: args[0], Text: args[1]}
	options := api.AgentPromptWaitOptions{Until: []api.AgentStatus{}}
	wait := false
	for i := 2; i < len(args); {
		switch args[i] {
		case "--wait":
			wait = true
			i++
		case "--until":
			value, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --until")
			}
			switch value {
			case api.AgentIdle, api.AgentWorking, api.AgentBlocked, api.AgentDone, api.AgentUnknown:
			default:
				return usageErr(r, "invalid --until status: "+value)
			}
			options.Until = append(options.Until, value)
		case "--timeout":
			value, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --timeout")
			}
			n, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return usageErr(r, "invalid --timeout value: "+value)
			}
			options.TimeoutMS = &n
		default:
			return usageErr(r, "unknown option: "+args[i])
		}
	}
	if !wait && len(options.Until) > 0 {
		return usageErr(r, "--until requires --wait")
	}
	if !wait && options.TimeoutMS != nil {
		return usageErr(r, "--timeout requires --wait")
	}
	if wait {
		p.Wait = &options
	}
	return r.printResult("cli:agent:prompt", api.MethodAgentPrompt, p)
}
