package cli

import (
	"fmt"

	"github.com/kazuph/goterrace/internal/api"
)

func (r *runner) tab(args []string) int {
	if len(args) == 0 {
		return usageErr(r, "usage: gtx tab <list|create|get|focus|rename|close> ...")
	}
	switch args[0] {
	case "list":
		return r.tabList(args[1:])
	case "create":
		return r.tabCreate(args[1:])
	case "get":
		return r.tabGet(args[1:])
	case "focus":
		return r.tabFocus(args[1:])
	case "rename":
		return r.tabRename(args[1:])
	case "close":
		return r.tabClose(args[1:])
	default:
		fmt.Fprintf(r.errOut(), "unknown tab command: %s\n", args[0])
		return 2
	}
}

func (r *runner) tabList(args []string) int {
	var params api.TabListParams
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
	return r.printResult("cli:tab:list", api.MethodTabList, params)
}

func (r *runner) tabCreate(args []string) int {
	var params api.TabCreateParams
	params.Env = map[string]string{}
	for i := 0; i < len(args); {
		switch args[i] {
		case "--workspace":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --workspace")
			}
			params.WorkspaceID = &v
		case "--cwd":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --cwd")
			}
			params.Cwd = &v
		case "--label":
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for --label")
			}
			params.Label = &v
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
	return r.printResult("cli:tab:create", api.MethodTabCreate, params)
}

func (r *runner) tabGet(args []string) int {
	if len(args) != 1 {
		return usageErr(r, "usage: gtx tab get <tab_id>")
	}
	return r.printResult("cli:tab:get", api.MethodTabGet, api.TabTarget{TabID: args[0]})
}

func (r *runner) tabFocus(args []string) int {
	if len(args) != 1 {
		return usageErr(r, "usage: gtx tab focus <tab_id>")
	}
	return r.printResult("cli:tab:focus", api.MethodTabFocus, api.TabTarget{TabID: args[0]})
}

func (r *runner) tabRename(args []string) int {
	if len(args) != 2 {
		return usageErr(r, "usage: gtx tab rename <tab_id> <label>")
	}
	return r.printResult("cli:tab:rename", api.MethodTabRename, api.TabRenameParams{
		TabID: args[0],
		Label: args[1],
	})
}

func (r *runner) tabClose(args []string) int {
	if len(args) != 1 {
		return usageErr(r, "usage: gtx tab close <tab_id>")
	}
	return r.printResult("cli:tab:close", api.MethodTabClose, api.TabTarget{TabID: args[0]})
}
