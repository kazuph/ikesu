package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kazuph/goterrace/internal/api"
)

// `gtx agent` — agent-facing commands mirroring herdr's agent group.

func printAgentHelp(w *runner) {
	fmt.Fprintln(w.errOut(), "gtx agent commands:")
	fmt.Fprintln(w.errOut(), "  gtx agent list")
	fmt.Fprintln(w.errOut(), "  gtx agent get <target>")
	fmt.Fprintln(w.errOut(), "  gtx agent read <target> [--source visible|recent|recent-unwrapped] [--lines N]")
	fmt.Fprintln(w.errOut(), "  gtx agent send <target> <text>")
	fmt.Fprintln(w.errOut(), "  gtx agent prompt <target> <text> [--wait] [--until STATUS]... [--timeout MS]")
	fmt.Fprintln(w.errOut(), "  gtx agent rename <target> <name>")
	fmt.Fprintln(w.errOut(), "  gtx agent focus <target>")
	fmt.Fprintln(w.errOut(), "  gtx agent wait <target> --status <idle|working|blocked|done|unknown> [--timeout MS]")
	fmt.Fprintln(w.errOut(), "  gtx agent start <name> [--cwd PATH] [--workspace ID] [--tab ID] [--split right|down] [--env KEY=VALUE] [--focus|--no-focus] -- <argv...>")
	fmt.Fprintln(w.errOut(), "  gtx agent restore [--dry-run]")
	fmt.Fprintln(w.errOut(), "targets accept pane ids (pN), unique agent names, and detected/reported agent labels")
}

func (r *runner) agentCLI(args []string) int {
	if len(args) == 0 {
		printAgentHelp(r)
		return 2
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "list":
		return r.agentListCLI()
	case "get":
		if len(rest) != 1 {
			return usageErr(r, "usage: gtx agent get <target>")
		}
		return r.agentGetCLI(rest[0])
	case "read":
		return r.agentReadCLI(rest)
	case "send":
		if len(rest) < 2 {
			return usageErr(r, "usage: gtx agent send <target> <text>")
		}
		return r.printResult("cli:agent:send", api.MethodAgentSend,
			api.AgentSendParams{Target: rest[0], Text: joinArgs(rest[1:])})
	case "prompt":
		return r.agentPromptCLI(rest)
	case "rename":
		if len(rest) != 2 {
			return usageErr(r, "usage: gtx agent rename <target> <name>")
		}
		return r.printResult("cli:agent:rename", api.MethodAgentRename,
			api.AgentRenameParams{Target: rest[0], Name: &rest[1]})
	case "focus":
		if len(rest) != 1 {
			return usageErr(r, "usage: gtx agent focus <target>")
		}
		return r.printResult("cli:agent:focus", api.MethodAgentFocus,
			api.AgentTarget{Target: rest[0]})
	case "wait":
		return r.agentWaitCLI(rest)
	case "start":
		return r.agentStartCLI(rest)
	case "restore":
		dryRun := false
		for _, a := range rest {
			if a == "--dry-run" {
				dryRun = true
			} else {
				return usageErr(r, "usage: gtx agent restore [--dry-run]")
			}
		}
		return r.printResult("cli:agent:restore", api.MethodAgentRestore,
			api.AgentRestoreParams{DryRun: dryRun})
	case "help", "--help", "-h":
		printAgentHelp(r)
		return 0
	default:
		printAgentHelp(r)
		return 2
	}
}

func (r *runner) agentGetRaw(target string) (*api.AgentInfo, *api.Error) {
	raw, err := r.request("cli:agent:get", api.MethodAgentGet, api.AgentTarget{Target: target})
	if err != nil {
		return nil, &api.Error{Code: "cli_error", Message: "server not running"}
	}
	var resp api.Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, &api.Error{Code: "cli_error", Message: err.Error()}
	}
	if resp.Err != nil {
		return nil, resp.Err
	}
	var res api.AgentInfoResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		return nil, &api.Error{Code: "cli_error", Message: err.Error()}
	}
	return &res.Agent, nil
}

func (r *runner) agentListCLI() int {
	raw, err := r.request("cli:agent:list", api.MethodAgentList, api.EmptyParams{})
	if err != nil {
		raw = r.serverNotRunning("cli:agent:list")
	}
	var resp api.Response
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Err != nil {
		fmt.Fprintln(r.errOut(), string(raw))
		return 1
	}
	var res api.AgentListResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		fmt.Fprintf(r.errOut(), "gtx: invalid server response: %v\n", err)
		return 1
	}
	for _, a := range res.Agents {
		fmt.Fprintf(r.out(), "%s name=%s agent=%s status=%s ws=%s tab=%s\n",
			a.PaneID, strOr(a.Name, "-"), strOr(a.Agent, "-"), a.AgentStatus,
			a.WorkspaceID, a.TabID)
	}
	return 0
}

func strOr(p *string, def string) string {
	if p == nil || *p == "" {
		return def
	}
	return *p
}

func (r *runner) agentGetCLI(target string) int {
	a, e := r.agentGetRaw(target)
	if e != nil {
		fmt.Fprintln(r.errOut(), e.Message)
		return 1
	}
	out, _ := json.MarshalIndent(a, "", "  ")
	fmt.Fprintln(r.out(), string(out))
	return 0
}

func (r *runner) agentReadCLI(args []string) int {
	if len(args) < 1 {
		return usageErr(r, "usage: gtx agent read <target> [--source visible|recent|recent-unwrapped] [--lines N]")
	}
	params := api.AgentReadParams{
		Target: args[0], Source: "recent", Format: "text", StripANSI: true,
	}
	for i := 1; i < len(args); {
		switch args[i] {
		case "--source":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --source")
			}
			src, err := parseReadSource(v)
			if err != nil {
				fmt.Fprintln(r.errOut(), err)
				return 2
			}
			params.Source = src
		case "--lines":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --lines")
			}
			n, err := parseU32Flag("--lines", v)
			if err != nil {
				fmt.Fprintln(r.errOut(), err)
				return 2
			}
			params.Lines = &n
		case "--format":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --format")
			}
			params.Format = v
			params.StripANSI = v != "ansi"
		case "--ansi":
			params.Format = "ansi"
			params.StripANSI = false
			i++
		default:
			fmt.Fprintf(r.errOut(), "unknown option: %s\n", args[i])
			return 2
		}
	}
	raw, err := r.request("cli:agent:read", api.MethodAgentRead, params)
	if err != nil {
		raw = r.serverNotRunning("cli:agent:read")
	}
	var resp api.Response
	if uerr := json.Unmarshal(raw, &resp); uerr != nil {
		fmt.Fprintf(r.errOut(), "gtx: invalid server response: %v\n", uerr)
		return 1
	}
	if resp.Err != nil {
		errJSON, _ := json.Marshal(resp.Err)
		fmt.Fprintln(r.errOut(), string(errJSON))
		return 1
	}
	var res api.AgentReadResultEnvelope
	if err := json.Unmarshal(resp.Result, &res); err == nil {
		fmt.Fprint(r.out(), res.Read.Text)
	}
	return 0
}

func (r *runner) agentWaitCLI(args []string) int {
	if len(args) < 1 {
		return usageErr(r, "usage: gtx agent wait <target> --status <idle|working|blocked|done|unknown> [--timeout MS]")
	}
	target := args[0]
	status := ""
	timeout := api.DefaultAgentWaitTimeoutMS
	for i := 1; i < len(args); {
		switch args[i] {
		case "--status":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --status")
			}
			status = v
		case "--timeout":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --timeout")
			}
			n, err := parseU32Flag("--timeout", v)
			if err != nil {
				fmt.Fprintln(r.errOut(), err)
				return 2
			}
			timeout = int(n)
		default:
			fmt.Fprintf(r.errOut(), "unknown option: %s\n", args[i])
			return 2
		}
	}
	switch status {
	case "idle", "working", "blocked", "done", "unknown":
	case "":
		return usageErr(r, "--status is required")
	default:
		fmt.Fprintf(r.errOut(), "invalid --status %q\n", status)
		return 2
	}
	deadline := time.Now().Add(time.Duration(timeout) * time.Millisecond)
	for {
		a, e := r.agentGetRaw(target)
		if e == nil && a.AgentStatus == status {
			fmt.Fprintf(r.out(), "%s reached status %s\n", target, status)
			return 0
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(r.errOut(), "timeout waiting for %s to reach status %s\n", target, status)
			return 1
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (r *runner) agentStartCLI(args []string) int {
	if len(args) < 1 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(r.errOut(), "usage: gtx agent start <name> [--cwd PATH] [--workspace ID] [--tab ID] [--split right|down] [--env KEY=VALUE] [--focus|--no-focus] -- <argv...>")
		if len(args) < 1 {
			return 2
		}
		return 0
	}
	p := api.AgentStartParams{Name: args[0], Focus: true, Env: map[string]string{}}
	i := 1
	for i < len(args) && p.Argv == nil {
		switch args[i] {
		case "--":
			p.Argv = append([]string{}, args[i+1:]...)
		case "--cwd":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --cwd")
			}
			p.Cwd = &v
		case "--workspace":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --workspace")
			}
			p.WorkspaceID = &v
		case "--tab":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --tab")
			}
			p.TabID = &v
		case "--split":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --split")
			}
			if v != "right" && v != "down" {
				fmt.Fprintln(r.errOut(), "--split must be right or down")
				return 2
			}
			p.Split = &v
		case "--env":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --env")
			}
			k, val, err := parseEnvAssignment(v)
			if err != nil {
				fmt.Fprintln(r.errOut(), err)
				return 2
			}
			p.Env[k] = val
		case "--focus":
			p.Focus = true
			i++
		case "--no-focus":
			p.Focus = false
			i++
		default:
			fmt.Fprintf(r.errOut(), "unknown option: %s\n", args[i])
			return 2
		}
	}
	if len(p.Argv) == 0 {
		return usageErr(r, "usage: gtx agent start <name> [options] -- <argv...>")
	}
	return r.printResult("cli:agent:start", api.MethodAgentStart, p)
}

var _ = strings.TrimSpace
