package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/detect"
)

func (r *runner) agentExplainCLI(args []string) int {
	const usage = "usage: gtx agent explain <target> [--json]\nusage: gtx agent explain --file PATH --agent LABEL [--json]"
	var target, file, agent string
	jsonOutput := false
	for i := 0; i < len(args); {
		switch args[i] {
		case "--json":
			jsonOutput = true
			i++
		case "--file", "--agent":
			flag := args[i]
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, usage)
			}
			if flag == "--file" {
				file = v
			} else {
				agent = v
			}
		default:
			if len(args[i]) > 0 && args[i][0] == '-' {
				return usageErr(r, "unknown option: "+args[i])
			}
			if target != "" {
				return usageErr(r, usage)
			}
			target = args[i]
			i++
		}
	}
	var e detect.Explanation
	if file != "" {
		if agent == "" {
			return usageErr(r, "gtx agent explain --file requires --agent LABEL")
		}
		if target != "" {
			return usageErr(r, "usage: gtx agent explain --file PATH --agent LABEL [--json]")
		}
		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
		e = detect.Explain(agent, detect.Input{Screen: string(data)})
	} else {
		if target == "" || agent != "" {
			return usageErr(r, usage)
		}
		raw, err := r.request("cli:agent:explain", api.MethodAgentExplain, api.AgentTarget{Target: target})
		if err != nil {
			raw = r.serverNotRunning("cli:agent:explain")
		}
		var resp api.Response
		if err := json.Unmarshal(raw, &resp); err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
		if resp.Err != nil {
			fmt.Fprintln(r.errOut(), string(raw))
			return 1
		}
		var result api.AgentExplainResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
		e = result.Explain
	}
	if jsonOutput {
		data, _ := json.Marshal(e)
		fmt.Fprintln(r.out(), string(data))
		return 0
	}
	fmt.Fprintf(r.out(), "agent: %s\nstate: %s\nmanifest: %s %s\n", e.Agent, e.State, strOr(e.ManifestSource, "none"), strOr(e.ManifestVersion, "unknown"))
	if e.MatchedRule == nil {
		fmt.Fprintln(r.out(), "rule: none")
	} else {
		m := e.MatchedRule
		fmt.Fprintf(r.out(), "rule: %s (region=%s priority=%d)\n", m.ID, m.Region, m.Priority)
		for _, rule := range e.EvaluatedRules {
			if rule.ID == m.ID {
				b, _ := json.Marshal(rule.Evidence.RegionPreview)
				fmt.Fprintf(r.out(), "evidence: %s\n", b)
				break
			}
		}
	}
	if e.SkippedUpdateReason != nil {
		fmt.Fprintf(r.out(), "skipped_update_reason: %s\n", *e.SkippedUpdateReason)
	}
	if e.FallbackReason != nil {
		fmt.Fprintf(r.out(), "fallback_reason: %s\n", *e.FallbackReason)
	}
	return 0
}
