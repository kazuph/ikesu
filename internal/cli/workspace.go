package cli

import (
	"fmt"

	"github.com/kazuph/ikesu/internal/api"
)

func (r *runner) workspace(args []string) int {
	if len(args) == 0 {
		return usageErr(r, "usage: ikesu workspace <list|create|get|focus|rename|close> ...")
	}
	switch args[0] {
	case "report-metadata":
		return r.workspaceReportMetadata(args[1:])
	case "list":
		return r.printResult("cli:workspace:list", api.MethodWorkspaceList, api.EmptyParams{})
	case "create":
		return r.workspaceCreate(args[1:])
	case "get":
		return r.workspaceGet(args[1:])
	case "focus":
		return r.workspaceFocus(args[1:])
	case "rename":
		return r.workspaceRename(args[1:])
	case "close":
		return r.workspaceClose(args[1:])
	default:
		fmt.Fprintf(r.errOut(), "unknown workspace command: %s\n", args[0])
		return 2
	}
}

func (r *runner) workspaceCreate(args []string) int {
	var params api.WorkspaceCreateParams
	params.Env = map[string]string{}
	for i := 0; i < len(args); {
		switch args[i] {
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
	return r.printResult("cli:workspace:create", api.MethodWorkspaceCreate, params)
}

func (r *runner) workspaceGet(args []string) int {
	if len(args) != 1 {
		return usageErr(r, "usage: ikesu workspace get <workspace_id>")
	}
	return r.printResult("cli:workspace:get", api.MethodWorkspaceGet, api.WorkspaceTarget{WorkspaceID: args[0]})
}

func (r *runner) workspaceFocus(args []string) int {
	if len(args) != 1 {
		return usageErr(r, "usage: ikesu workspace focus <workspace_id>")
	}
	return r.printResult("cli:workspace:focus", api.MethodWorkspaceFocus, api.WorkspaceTarget{WorkspaceID: args[0]})
}

func (r *runner) workspaceRename(args []string) int {
	if len(args) != 2 {
		return usageErr(r, "usage: ikesu workspace rename <workspace_id> <label>")
	}
	return r.printResult("cli:workspace:rename", api.MethodWorkspaceRename, api.WorkspaceRenameParams{
		WorkspaceID: args[0],
		Label:       args[1],
	})
}

func (r *runner) workspaceClose(args []string) int {
	if len(args) != 1 {
		return usageErr(r, "usage: ikesu workspace close <workspace_id>")
	}
	return r.printResult("cli:workspace:close", api.MethodWorkspaceClose, api.WorkspaceTarget{WorkspaceID: args[0]})
}
