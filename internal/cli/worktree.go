package cli

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/api"
)

const worktreeHelp = `gtx worktree commands:
  gtx worktree list [--workspace ID | --cwd PATH] [--json]
  gtx worktree create [--workspace ID | --cwd PATH] [--branch NAME] [--base REF] [--path PATH] [--label TEXT] [--focus] [--no-focus] [--json]
  gtx worktree open [--workspace ID | --cwd PATH] (--path PATH | --branch NAME) [--label TEXT] [--focus] [--no-focus] [--json]
  gtx worktree remove --workspace ID [--force] [--json]`

func (r *runner) worktree(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(r.out(), worktreeHelp)
		return 0
	}
	op := args[0]
	if op != "list" && op != "create" && op != "open" && op != "remove" {
		return usageErr(r, "unknown worktree command: "+op)
	}
	var p api.WorktreeCreateParams
	force := false
	for i := 1; i < len(args); {
		flag := args[i]
		switch flag {
		case "--json":
			i++
		case "--focus", "--no-focus":
			if op != "create" && op != "open" {
				return usageErr(r, "unknown option: "+flag)
			}
			p.Focus = flag == "--focus"
			i++
		case "--force":
			if op != "remove" {
				return usageErr(r, "unknown option: "+flag)
			}
			force = true
			i++
		case "--workspace", "--cwd", "--branch", "--base", "--path", "--label":
			if op == "remove" && flag != "--workspace" || op == "list" && flag != "--workspace" && flag != "--cwd" || op == "open" && flag == "--base" {
				return usageErr(r, "unknown option: "+flag)
			}
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for "+flag)
			}
			switch flag {
			case "--workspace":
				p.WorkspaceID = &v
			case "--cwd":
				p.Cwd = &v
			case "--branch":
				p.Branch = &v
			case "--base":
				p.Base = &v
			case "--path":
				p.Path = &v
			case "--label":
				p.Label = &v
			}
		default:
			return usageErr(r, "unknown option: "+flag)
		}
	}
	if p.WorkspaceID != nil && p.Cwd != nil {
		return usageErr(r, "--workspace and --cwd are mutually exclusive")
	}
	switch op {
	case "list":
		return r.printResult("cli:worktree:list", api.MethodWorktreeList, api.WorktreeListParams{Cwd: p.Cwd, WorkspaceID: p.WorkspaceID})
	case "create":
		return r.printResult("cli:worktree:create", api.MethodWorktreeCreate, p)
	case "open":
		if (p.Path == nil) == (p.Branch == nil) {
			return usageErr(r, "worktree open requires exactly one of --path or --branch")
		}
		return r.printResult("cli:worktree:open", api.MethodWorktreeOpen, api.WorktreeOpenParams{Branch: p.Branch, Cwd: p.Cwd, Focus: p.Focus, Label: p.Label, Path: p.Path, WorkspaceID: p.WorkspaceID})
	default:
		if p.WorkspaceID == nil {
			return usageErr(r, "worktree remove requires --workspace")
		}
		return r.printResult("cli:worktree:remove", api.MethodWorktreeRemove, api.WorktreeRemoveParams{Force: force, WorkspaceID: *p.WorkspaceID})
	}
}
