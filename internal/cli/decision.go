package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

const decisionHelp = `ikesu decision commands:
  ikesu decision ask --title <title> --option <id:label:role> [--option ...] [--option-desc <id:text>] [--kind ask|command_guard|tool_permission|agent_prompt] [--body <text>] [--allow-text] [--multi-select] [--timeout <ms>]
  ikesu decision watch [--all-machines]
  ikesu decision answer <id> [<option_id> ...] [--text <text>] [--responder <name>] [--machine <label-or-id>]
  ikesu decision list [--status pending|answered|expired|cancelled]
  ikesu decision get <id> [--machine <label-or-id>]
  ikesu decision cancel <id>
`

func (r *runner) decisionCLI(args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprint(r.out(), decisionHelp)
		return 0
	}
	switch args[0] {
	case "watch":
		return r.decisionWatch(args[1:])
	case "ask":
		return r.decisionAsk(args[1:])
	case "get", "cancel":
		if args[0] == "get" {
			if handled, code := r.decisionMachineCommand("get", args[1:]); handled {
				return code
			}
		}
		if len(args) != 2 {
			return usageErr(r, decisionHelp)
		}
		return r.printResult("cli:decision:"+args[0], "decision."+args[0], api.DecisionTargetParams{DecisionID: args[1]})
	case "list":
		p := api.DecisionListParams{}
		if len(args) == 3 && args[1] == "--status" {
			p.Status = args[2]
		} else if len(args) != 1 {
			return usageErr(r, decisionHelp)
		}
		return r.printResult("cli:decision:list", api.MethodDecisionList, p)
	case "answer":
		if handled, code := r.decisionMachineAnswer(args[1:]); handled {
			return code
		}
		if len(args) < 2 {
			return usageErr(r, decisionHelp)
		}
		p := api.DecisionAnswerParams{DecisionID: args[1], Responder: "cli"}
		for i := 2; i < len(args); {
			flag := args[i]
			switch flag {
			case "--text", "--responder":
				v, ok := flagValue(args, &i)
				if !ok {
					return usageErr(r, "missing value for "+flag)
				}
				if flag == "--text" {
					p.Text = &v
				} else {
					p.Responder = v
				}
			default:
				if strings.HasPrefix(flag, "-") {
					return usageErr(r, decisionHelp)
				}
				v := flag
				if p.OptionID == nil {
					p.OptionID = &v
				} else {
					if len(p.OptionIDs) == 0 {
						p.OptionIDs = append(p.OptionIDs, *p.OptionID)
					}
					p.OptionIDs = append(p.OptionIDs, v)
				}
				i++
			}
		}
		return r.printResult("cli:decision:answer", api.MethodDecisionAnswer, p)
	default:
		return usageErr(r, decisionHelp)
	}
}
func (r *runner) decisionAsk(args []string) int {
	p := api.DecisionCreateParams{Kind: "ask"}
	descriptions := map[string]string{}
	for i := 0; i < len(args); {
		flag := args[i]
		if flag == "--allow-text" {
			p.AllowText = true
			i++
			continue
		}
		if flag == "--multi-select" {
			p.MultiSelect = true
			i++
			continue
		}
		switch flag {
		case "--title", "--body", "--kind", "--option", "--option-desc", "--timeout":
		default:
			return usageErr(r, decisionHelp)
		}
		v, ok := flagValue(args, &i)
		if !ok {
			return usageErr(r, "missing value for "+flag)
		}
		switch flag {
		case "--title":
			p.Title = v
		case "--body":
			p.Body = &v
		case "--kind":
			p.Kind = v
		case "--option-desc":
			id, text, found := strings.Cut(v, ":")
			if !found || id == "" {
				return usageErr(r, "option-desc must be id:text")
			}
			descriptions[id] = text
		case "--option":
			var o api.DecisionOption
			if strings.HasPrefix(v, "{") {
				if err := json.Unmarshal([]byte(v), &o); err != nil {
					return usageErr(r, err.Error())
				}
			} else {
				id, rest, found := strings.Cut(v, ":")
				at := strings.LastIndex(rest, ":")
				if !found || at < 0 {
					return usageErr(r, "option must be id:label:role or a JSON object")
				}
				o = api.DecisionOption{ID: id, Label: rest[:at], Role: rest[at+1:]}
			}
			p.Options = append(p.Options, o)
		case "--timeout":
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return usageErr(r, "invalid timeout")
			}
			p.TimeoutMS = &n
		}
	}
	if p.Title == "" || len(p.Options) == 0 {
		return usageErr(r, decisionHelp)
	}
	for id, text := range descriptions {
		found := false
		for i := range p.Options {
			if p.Options[i].ID == id {
				p.Options[i].Description = &text
				found = true
			}
		}
		if !found {
			return usageErr(r, "unknown option id for option-desc: "+id)
		}
	}
	var caller *string
	if env := r.envPaneID(); env != "" {
		caller = &env
	}
	// Resolve exactly as pane.current, but an invocation outside panes has no origin.
	if id, err := r.currentPaneID(caller); err == nil {
		p.Origin = &api.DecisionOrigin{PaneID: id}
	}
	raw, err := r.request("cli:decision:ask", api.MethodDecisionCreate, p)
	if err != nil {
		fmt.Fprintln(r.errOut(), string(r.serverNotRunning("cli:decision:ask")))
		return 1
	}
	var resp api.Response
	if err = json.Unmarshal(raw, &resp); err != nil {
		fmt.Fprintln(r.errOut(), err)
		return 1
	}
	if resp.Err != nil {
		fmt.Fprintln(r.errOut(), string(raw))
		return 1
	}
	var result api.DecisionResult
	if err = json.Unmarshal(resp.Result, &result); err != nil {
		fmt.Fprintln(r.errOut(), err)
		return 1
	}
	id := result.Decision.DecisionID
	for {
		raw, err = r.request("cli:decision:ask", api.MethodDecisionWait, api.DecisionWaitParams{DecisionID: id})
		if err != nil {
			time.Sleep(decisionRetryInitial)
			continue
		}
		if err = json.Unmarshal(raw, &resp); err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
		if resp.Err != nil {
			fmt.Fprintln(r.errOut(), string(raw))
			return 1
		}
		if err = json.Unmarshal(resp.Result, &result); err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
		switch result.Decision.Status {
		case "answered":
			fmt.Fprintln(r.out(), string(raw))
			return 0
		case "expired":
			fmt.Fprintln(r.out(), string(raw))
			return 3
		case "cancelled":
			fmt.Fprintln(r.out(), string(raw))
			return 4
		}
	}
}
