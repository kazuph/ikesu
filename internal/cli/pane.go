package cli

import (
	"encoding/json"
	"fmt"

	"github.com/kazuph/goterrace/internal/api"
)

func (r *runner) pane(args []string) int {
	if len(args) == 0 {
		return usageErr(r, "usage: gtx pane <list|current|get|layout|neighbor|edges|focus|resize|zoom|rename|read|split|swap|move|close|send-text|send-keys|report-agent|report-metadata|run> ...")
	}
	switch args[0] {
	case "report-metadata":
		return r.paneReportMetadata(args[1:])
	case "list":
		return r.paneList(args[1:])
	case "current":
		return r.paneCurrent(args[1:])
	case "get":
		return r.paneGet(args[1:])
	case "layout":
		return r.paneLayout(args[1:])
	case "neighbor":
		return r.paneNeighbor(args[1:])
	case "edges":
		return r.paneEdges(args[1:])
	case "focus":
		return r.paneFocus(args[1:])
	case "resize":
		return r.paneResize(args[1:])
	case "zoom":
		return r.paneZoom(args[1:])
	case "rename":
		return r.paneRename(args[1:])
	case "read":
		return r.paneRead(args[1:])
	case "split":
		return r.paneSplit(args[1:])
	case "swap":
		return r.paneSwap(args[1:])
	case "move":
		return r.paneMove(args[1:])
	case "close":
		return r.paneClose(args[1:])
	case "send-text":
		return r.paneSendText(args[1:])
	case "send-keys":
		return r.paneSendKeys(args[1:])
	case "report-agent":
		return r.paneReportAgent(args[1:])
	case "run":
		return r.paneRun(args[1:])
	default:
		fmt.Fprintf(r.errOut(), "unknown pane command: %s\n", args[0])
		return 2
	}
}

// paneSelector parses [--pane ID|--current] from args. It returns a nil
// pane id (meaning the focused pane) when neither flag is given.
func paneSelector(r *runner, args []string) (*string, error) {
	var paneID *string
	for i := 0; i < len(args); {
		switch args[i] {
		case "--pane":
			v, ok := flagValue(args, &i)
			if !ok {
				return nil, fmt.Errorf("missing value for --pane")
			}
			paneID = &v
		case "--current":
			if env := r.envPaneID(); env != "" {
				paneID = &env
			}
			i++
		default:
			return nil, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	return paneID, nil
}

func (r *runner) paneLayout(args []string) int {
	paneID, err := paneSelector(r, args)
	if err != nil {
		return usageErr(r, "usage: gtx pane layout [--pane ID|--current]")
	}
	return r.printResult("cli:pane:layout", api.MethodPaneLayout, api.PaneLayoutParams{PaneID: paneID})
}

func (r *runner) paneNeighbor(args []string) int {
	var params api.PaneNeighborParams
	for i := 0; i < len(args); {
		switch args[i] {
		case "--pane":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --pane")
			}
			params.PaneID = &v
		case "--current":
			if env := r.envPaneID(); env != "" {
				params.PaneID = &env
			}
			i++
		case "--direction":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --direction")
			}
			d, err := parseNavDirection(v)
			if err != nil {
				fmt.Fprintln(r.errOut(), err)
				return 2
			}
			params.Direction = d
		default:
			fmt.Fprintf(r.errOut(), "unknown option: %s\n", args[i])
			return 2
		}
	}
	if params.Direction == "" {
		return usageErr(r, "usage: gtx pane neighbor --direction left|right|up|down [--pane ID|--current]")
	}
	return r.printResult("cli:pane:neighbor", api.MethodPaneNeighbor, params)
}

func (r *runner) paneEdges(args []string) int {
	paneID, err := paneSelector(r, args)
	if err != nil {
		return usageErr(r, "usage: gtx pane edges [--pane ID|--current]")
	}
	return r.printResult("cli:pane:edges", api.MethodPaneEdges, api.PaneEdgesParams{PaneID: paneID})
}

func (r *runner) paneResize(args []string) int {
	var params api.PaneResizeParams
	for i := 0; i < len(args); {
		switch args[i] {
		case "--pane":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --pane")
			}
			params.PaneID = &v
		case "--current":
			if env := r.envPaneID(); env != "" {
				params.PaneID = &env
			}
			i++
		case "--direction":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --direction")
			}
			d, err := parseNavDirection(v)
			if err != nil {
				fmt.Fprintln(r.errOut(), err)
				return 2
			}
			params.Direction = d
		case "--amount":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --amount")
			}
			n, err := parseFloatFlag("--amount", v)
			if err != nil {
				fmt.Fprintln(r.errOut(), err)
				return 2
			}
			params.Amount = &n
		default:
			fmt.Fprintf(r.errOut(), "unknown option: %s\n", args[i])
			return 2
		}
	}
	if params.Direction == "" {
		return usageErr(r, "usage: gtx pane resize --direction left|right|up|down [--amount FLOAT] [--pane ID|--current]")
	}
	return r.printResult("cli:pane:resize", api.MethodPaneResize, params)
}

func (r *runner) paneZoom(args []string) int {
	var params api.PaneZoomParams
	for i := 0; i < len(args); {
		switch args[i] {
		case "--pane":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --pane")
			}
			params.PaneID = &v
		case "--current":
			if env := r.envPaneID(); env != "" {
				params.PaneID = &env
			}
			i++
		case "--toggle":
			params.Mode = "toggle"
			i++
		case "--on":
			params.Mode = "on"
			i++
		case "--off":
			params.Mode = "off"
			i++
		default:
			if !isFlag(args[i]) && params.PaneID == nil {
				v := args[i]
				params.PaneID = &v
				i++
				continue
			}
			fmt.Fprintf(r.errOut(), "unknown option: %s\n", args[i])
			return 2
		}
	}
	return r.printResult("cli:pane:zoom", api.MethodPaneZoom, params)
}

func (r *runner) paneSwap(args []string) int {
	var params api.PaneSwapParams
	for i := 0; i < len(args); {
		switch args[i] {
		case "--pane":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --pane")
			}
			params.PaneID = &v
		case "--current":
			if env := r.envPaneID(); env != "" {
				params.PaneID = &env
			}
			i++
		case "--direction":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --direction")
			}
			d, err := parseNavDirection(v)
			if err != nil {
				fmt.Fprintln(r.errOut(), err)
				return 2
			}
			params.Direction = &d
		case "--source-pane":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --source-pane")
			}
			params.SourcePaneID = &v
		case "--target-pane":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --target-pane")
			}
			params.TargetPaneID = &v
		default:
			fmt.Fprintf(r.errOut(), "unknown option: %s\n", args[i])
			return 2
		}
	}
	if params.Direction == nil && (params.SourcePaneID == nil || params.TargetPaneID == nil) {
		return usageErr(r, "usage: gtx pane swap --direction left|right|up|down [--pane ID|--current] | gtx pane swap --source-pane ID --target-pane ID")
	}
	return r.printResult("cli:pane:swap", api.MethodPaneSwap, params)
}

func (r *runner) paneMove(args []string) int {
	if len(args) == 0 {
		return usageErr(r, "usage: gtx pane move <pane_id> --tab <tab_id> --split right|down [--target-pane ID] [--ratio FLOAT] [--focus|--no-focus] | --new-tab [--workspace ID] [--label TEXT] | --new-workspace [--label TEXT] [--tab-label TEXT]")
	}
	params := api.PaneMoveParams{PaneID: args[0]}
	var tabID *string
	var newTab, newWorkspace bool
	i := 1
	for i < len(args) {
		switch args[i] {
		case "--tab":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --tab")
			}
			tabID = &v
		case "--new-tab":
			newTab = true
			i++
		case "--new-workspace":
			newWorkspace = true
			i++
		case "--workspace":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --workspace")
			}
			params.Destination.WorkspaceID = &v
		case "--split":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --split")
			}
			d, err := parseSplitDirection(v)
			if err != nil {
				fmt.Fprintln(r.errOut(), err)
				return 2
			}
			params.Destination.Split = &d
		case "--target-pane":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --target-pane")
			}
			params.Destination.TargetPaneID = &v
		case "--ratio":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --ratio")
			}
			n, err := parseFloatFlag("--ratio", v)
			if err != nil {
				fmt.Fprintln(r.errOut(), err)
				return 2
			}
			params.Destination.Ratio = &n
		case "--label":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --label")
			}
			params.Destination.Label = &v
		case "--tab-label":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --tab-label")
			}
			params.Destination.TabLabel = &v
		case "--focus":
			params.Focus = true
			i++
		case "--no-focus":
			params.Focus = false
			i++
		default:
			fmt.Fprintf(r.errOut(), "unknown option: %s\n", args[i])
			return 2
		}
	}
	switch {
	case tabID != nil && !newTab && !newWorkspace:
		params.Destination.Type = "tab"
		params.Destination.TabID = *tabID
		if params.Destination.Split == nil {
			return usageErr(r, "pane move --tab requires --split right|down")
		}
	case newTab && !newWorkspace && tabID == nil:
		params.Destination.Type = "new_tab"
	case newWorkspace && !newTab && tabID == nil:
		params.Destination.Type = "new_workspace"
	default:
		return usageErr(r, "pane move needs exactly one of --tab, --new-tab, --new-workspace")
	}
	return r.printResult("cli:pane:move", api.MethodPaneMove, params)
}

func (r *runner) paneList(args []string) int {
	var params api.PaneListParams
	for i := 0; i < len(args); {
		switch args[i] {
		case "--workspace":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --workspace")
			}
			params.WorkspaceID = &v
		default:
			fmt.Fprintf(r.errOut(), "unknown option: %s\n", args[i])
			return 2
		}
	}
	return r.printResult("cli:pane:list", api.MethodPaneList, params)
}

func (r *runner) paneCurrent(args []string) int {
	var callerPaneID *string
	if env := r.envPaneID(); env != "" {
		callerPaneID = &env
	}
	for i := 0; i < len(args); {
		switch args[i] {
		case "--pane":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --pane")
			}
			callerPaneID = &v
		case "--current":
			if env := r.envPaneID(); env != "" {
				callerPaneID = &env
			} else {
				callerPaneID = nil
			}
			i++
		default:
			fmt.Fprintf(r.errOut(), "unknown option: %s\n", args[i])
			return 2
		}
	}
	pid := r.o.pid()
	return r.printResult("cli:pane:current", api.MethodPaneCurrent, api.PaneCurrentParams{
		CallerPaneID:    callerPaneID,
		CallerProcessID: &pid,
	})
}

func (r *runner) paneGet(args []string) int {
	if len(args) != 1 {
		return usageErr(r, "usage: gtx pane get <pane_id>")
	}
	return r.printResult("cli:pane:get", api.MethodPaneGet, api.PaneTarget{PaneID: args[0]})
}

func (r *runner) paneFocus(args []string) int {
	if len(args) != 1 {
		return usageErr(r, "usage: gtx pane focus <pane_id>")
	}
	return r.printResult("cli:pane:focus", api.MethodPaneFocus, api.PaneFocusParams{PaneID: args[0]})
}

func (r *runner) paneRename(args []string) int {
	if len(args) < 2 {
		return usageErr(r, "usage: gtx pane rename <pane_id> <label>|--clear")
	}
	var label *string
	if len(args) == 2 && args[1] == "--clear" {
		label = nil
	} else {
		joined := joinArgs(args[1:])
		label = &joined
	}
	return r.printResult("cli:pane:rename", api.MethodPaneRename, api.PaneRenameParams{
		PaneID: args[0],
		Label:  label,
	})
}

func (r *runner) paneRead(args []string) int {
	if len(args) < 1 {
		return usageErr(r, "usage: gtx pane read <pane_id> [--source visible|recent|recent-unwrapped] [--lines N]")
	}
	params := api.PaneReadParams{
		PaneID:    args[0],
		Source:    "recent",
		Format:    "text",
		StripANSI: true,
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
	raw, err := r.request("cli:pane:read", api.MethodPaneRead, params)
	if err != nil {
		raw = r.serverNotRunning("cli:pane:read")
	}
	var resp api.Response
	if uerr := json.Unmarshal(raw, &resp); uerr != nil {
		fmt.Fprintf(r.errOut(), "gtx: invalid server response: %v\n", uerr)
		return 1
	}
	if resp.Err != nil {
		// herdr prints just the error object for pane read.
		errJSON, _ := json.Marshal(resp.Err)
		fmt.Fprintln(r.errOut(), string(errJSON))
		return 1
	}
	var res api.PaneReadResultEnvelope
	if err := json.Unmarshal(resp.Result, &res); err == nil {
		fmt.Fprint(r.out(), res.Read.Text)
	}
	return 0
}

func (r *runner) paneSplit(args []string) int {
	env := r.envPaneID()
	params := api.PaneSplitParams{Env: map[string]string{}}
	var direction string
	i := 0
	if len(args) > 0 && !isFlag(args[0]) {
		v := args[0]
		params.TargetPaneID = &v
		i = 1
	}
	for i < len(args) {
		switch args[i] {
		case "--pane":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --pane")
			}
			params.TargetPaneID = &v
		case "--current":
			if env != "" {
				params.TargetPaneID = &env
			}
			i++
		case "--direction":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --direction")
			}
			d, err := parseSplitDirection(v)
			if err != nil {
				fmt.Fprintln(r.errOut(), err)
				return 2
			}
			direction = d
		case "--ratio":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --ratio")
			}
			n, err := parseFloatFlag("--ratio", v)
			if err != nil {
				fmt.Fprintln(r.errOut(), err)
				return 2
			}
			params.Ratio = &n
		case "--cwd":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --cwd")
			}
			params.Cwd = &v
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
			params.Env[k] = val
		case "--focus":
			params.Focus = true
			i++
		case "--no-focus":
			params.Focus = false
			i++
		default:
			fmt.Fprintf(r.errOut(), "unknown option: %s\n", args[i])
			return 2
		}
	}
	if direction == "" {
		return usageErr(r, "usage: gtx pane split [<pane_id>|--pane ID|--current] --direction right|down [--ratio FLOAT] [--cwd PATH] [--env KEY=VALUE] [--focus] [--no-focus]")
	}
	params.Direction = direction
	return r.printResult("cli:pane:split", api.MethodPaneSplit, params)
}

func (r *runner) paneClose(args []string) int {
	if len(args) != 1 {
		return usageErr(r, "usage: gtx pane close <pane_id>")
	}
	return r.printResult("cli:pane:close", api.MethodPaneClose, api.PaneTarget{PaneID: args[0]})
}

func (r *runner) paneSendText(args []string) int {
	if len(args) < 2 {
		return usageErr(r, "usage: gtx pane send-text <pane_id> <text>")
	}
	return r.sendOK(api.MethodPaneSendText, api.PaneSendTextParams{
		PaneID: args[0],
		Text:   joinArgs(args[1:]),
	})
}

func (r *runner) paneSendKeys(args []string) int {
	if len(args) < 2 {
		return usageErr(r, "usage: gtx pane send-keys <pane_id> <key> [key ...]")
	}
	return r.sendOK(api.MethodPaneSendKeys, api.PaneSendKeysParams{
		PaneID: args[0],
		Keys:   args[1:],
	})
}

func (r *runner) paneRun(args []string) int {
	if len(args) < 2 {
		return usageErr(r, "usage: gtx pane run <pane_id> <command>")
	}
	return r.sendOK(api.MethodPaneSendInput, api.PaneSendInputParams{
		PaneID: args[0],
		Text:   joinArgs(args[1:]),
		Keys:   []string{"Enter"},
	})
}

func (r *runner) paneReportAgent(args []string) int {
	if len(args) < 1 {
		return usageErr(r, "usage: gtx pane report-agent <pane_id> --source ID --agent LABEL --state idle|working|blocked|unknown [--message TEXT] [--custom-status TEXT] [--seq N] [--title TEXT] [--session-id ID] [--agent-session-id ID] [--agent-session-path PATH] [--model NAME]")
	}
	params := api.PaneReportAgentParams{PaneID: args[0]}
	for i := 1; i < len(args); {
		flag := args[i]
		v, ok := flagValue(args, &i)
		if !ok {
			return usageErr(r, fmt.Sprintf("missing value for %s", flag))
		}
		switch flag {
		case "--source":
			params.Source = v
		case "--agent":
			params.Agent = v
		case "--state":
			switch v {
			case "idle", "working", "blocked", "unknown":
				params.State = v
			default:
				fmt.Fprintf(r.errOut(), "invalid agent state: %s\n", v)
				return 2
			}
		case "--message":
			params.Message = &v
		case "--custom-status":
			params.CustomStatus = &v
		case "--seq":
			n, err := parseU64Flag("--seq", v)
			if err != nil {
				fmt.Fprintln(r.errOut(), err)
				return 2
			}
			params.Seq = &n
		case "--title":
			params.Title = &v
		case "--session-id":
			params.SessionID = &v
		case "--agent-session-id":
			params.AgentSessionID = &v
		case "--agent-session-path":
			params.AgentSessionPath = &v
		case "--model":
			params.Model = &v
		default:
			fmt.Fprintf(r.errOut(), "unknown option: %s\n", flag)
			return 2
		}
	}
	if params.Source == "" || params.Agent == "" || params.State == "" {
		return usageErr(r, "usage: gtx pane report-agent <pane_id> --source ID --agent LABEL --state idle|working|blocked|unknown [--message TEXT] [--custom-status TEXT] [--seq N] [--title TEXT] [--session-id ID] [--agent-session-id ID] [--agent-session-path PATH] [--model NAME]")
	}
	return r.sendOK(api.MethodPaneReportAgent, params)
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

func isFlag(s string) bool {
	return len(s) > 0 && s[0] == '-'
}
