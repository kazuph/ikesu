package cli

import (
	"encoding/json"
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/envcompat"
	"github.com/kazuph/ikesu/internal/guard"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func (r *runner) guardCLI(a []string) int {
	if len(a) == 0 || a[0] == "--help" || a[0] == "-h" {
		fmt.Fprintln(r.out(), "ikesu guard exec -- <cmd> [args...]\nikesu guard line -- <command line>\nikesu guard claude-prefix -- <command line>\nikesu guard check -- <cmd> [args...] [--cwd DIR] [--launch-dir DIR]\nikesu guard log [--lines N] [--json]\nikesu guard compare [--guard-log PATH] [--ai-guard-log PATH] [--window SEC] [--since EPOCH]\nikesu guard status")
		return 0
	}
	sub := a[0]
	a = a[1:]
	var c config.Config
	var e error
	if (sub != "exec" && sub != "line" && sub != "claude-prefix") || envcompat.Get("GTX_GUARD_MARKER") == "" {
		c, e = config.Load()
		if e != nil {
			fmt.Fprintln(r.errOut(), e)
			return 1
		}
	}
	root := api.StateDir()
	cwd, _ := os.Getwd()
	launch := r.o.getenv("GTX_GUARD_LAUNCH_CWD")
	via := "shim"
	i := guard.Input{Cwd: cwd, LaunchCwd: launch, Env: guard.EnvMap(os.Environ())}
	// Generated entries pin the marker path. Keep diagnostics with that pane's
	// assets even when its shell still carries the pre-handoff state directory.
	if marker := i.Env["GTX_GUARD_MARKER"]; marker != "" && (sub == "exec" || sub == "line" || sub == "claude-prefix") {
		root = filepath.Dir(filepath.Dir(filepath.Dir(marker)))
	}
	await := func(p api.DecisionCreateParams, timeout time.Duration) (api.Decision, error) {
		marker, active, err := guard.ReadMarker(i)
		if err != nil {
			return api.Decision{}, err
		}
		if !active {
			return api.Decision{}, fmt.Errorf("承認先の目印がありません")
		}
		next := *r
		getenv := next.o.getenv
		next.o.Explicit = false
		next.o.Getenv = func(key string) string {
			if key == "IKESU_SOCKET_PATH_EXPLICIT" || key == "GTX_SOCKET_PATH_EXPLICIT" {
				return "1"
			}
			if key == "IKESU_SOCKET_PATH" || key == "GTX_SOCKET_PATH" {
				return marker.Socket
			}
			return getenv(key)
		}
		return next.guardAwait(p, timeout)
	}

	writeJSON := func(v any) int {
		enc := json.NewEncoder(r.out())
		enc.SetIndent("", "  ")
		if e := enc.Encode(v); e != nil {
			fmt.Fprintln(r.errOut(), e)
			return 1
		}
		return 0
	}
	if sub == "claude-prefix" {
		if len(a) > 0 && a[0] == "--" {
			a = a[1:]
		}
		if len(a) != 1 {
			i.Command, i.Args = "gtx", append([]string{"guard", "claude-prefix", "--"}, a...)
			guard.Authorize(c.Guard, root, i, "line", r.errOut(), await)
			return usageErr(r, "guard claude-prefix requires exactly one command line")
		}
		line := a[0]
		i.Command, i.Args = line, nil
		if !guard.AuthorizeLine(c.Guard, root, i, guard.ClaudePrefixLine(i, line), r.errOut(), await) {
			return 1
		}
		if prefix := i.Env["GTX_GUARD_CLAUDE_PREFIX_ORIG"]; prefix != "" {
			i.Command, i.Args = prefix, []string{line}
			if !guard.AuthorizeLine(c.Guard, root, i, shellQuote(prefix)+" "+shellQuote(line), r.errOut(), await) {
				return 1
			}
		} else {
			i.Command = i.Env["SHELL"]
			if i.Command == "" {
				i.Command = "/bin/sh"
			}
			i.Args = []string{"-c", line}
		}
		if err := guard.Execute(i); err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
		return 0
	}
	if sub == "status" {
		v, e := guard.Status(c.Guard, root, i)
		if e != nil {
			fmt.Fprintln(r.errOut(), e)
			return 1
		}
		issues, e := guard.CoverageIssues(root)
		if e != nil {
			fmt.Fprintln(r.errOut(), e)
			return 1
		}
		for _, issue := range issues {
			fmt.Fprintln(r.errOut(), issue.String())
		}
		return writeJSON(v)
	}
	if sub == "compare" {
		o := guard.CompareOptions{GuardLog: filepath.Join(root, "guard", "decisions.jsonl"), AILog: filepath.Join(i.Env["HOME"], ".ai_extreme_confirm.log")}
		for len(a) > 0 {
			if len(a) < 2 {
				return usageErr(r, "guard compare option requires a value")
			}
			flag, value := a[0], a[1]
			a = a[2:]
			switch flag {
			case "--guard-log":
				o.GuardLog = value
			case "--ai-guard-log":
				o.AILog = value
			case "--window", "--since":
				n, e := strconv.ParseInt(value, 10, 64)
				if e != nil || n < 0 {
					return usageErr(r, "guard compare requires a nonnegative integer")
				}
				if flag == "--window" {
					o.Window = &n
				} else {
					o.Since = n
				}
			default:
				return usageErr(r, "unknown guard compare option")
			}
		}
		code, e := guard.Compare(o, r.out())
		if e != nil {
			fmt.Fprintln(r.errOut(), e)
			return 1
		}
		return code
	}
	if sub == "log" {
		defaultLines := 20
		lines := &defaultLines
		jsonMode := false
		for len(a) > 0 {
			switch a[0] {
			case "--json":
				jsonMode = true
				a = a[1:]
			case "--lines":
				if len(a) < 2 {
					return usageErr(r, "--lines requires a value")
				}
				n, e := strconv.Atoi(a[1])
				if e != nil || n < 0 {
					return usageErr(r, "--lines requires a nonnegative integer")
				}
				lines = &n
				a = a[2:]
			default:
				return usageErr(r, "unknown guard log option")
			}
		}
		if e = guard.ReadLog(filepath.Join(root, "guard", "decisions.jsonl"), lines, r.out(), jsonMode); e != nil {
			fmt.Fprintln(r.errOut(), e)
			return 1
		}
		return 0
	}
	if sub != "check" && sub != "exec" && sub != "line" {
		return usageErr(r, "unsupported guard operation: "+sub)
	}
	var argv []string
	after := false
	for len(a) > 0 {
		flag := a[0]
		if flag == "--" && !after {
			after = true
			a = a[1:]
			continue
		}
		option := flag == "--cwd" || flag == "--launch-dir" || flag == "--launch-cwd" || flag == "--via"
		if option && (sub == "check" || !after) {
			if len(a) < 2 {
				return usageErr(r, "guard option requires a value")
			}
			switch flag {
			case "--cwd":
				i.Cwd = a[1]
			case "--launch-dir", "--launch-cwd":
				i.LaunchCwd = a[1]
			case "--via":
				via = a[1]
			}
			a = a[2:]
			continue
		}
		argv = append(argv, flag)
		a = a[1:]
	}
	if len(argv) == 0 {
		return usageErr(r, "guard requires a command")
	}
	i.Command = argv[0]
	i.Args = argv[1:]
	if sub == "check" {
		return writeJSON(guard.Evaluate(c.Guard, i))
	}
	if sub == "line" {
		if len(argv) != 1 {
			return usageErr(r, "guard line requires one command line")
		}
		if !guard.AuthorizeLine(c.Guard, root, i, argv[0], r.errOut(), await) {
			return 1
		}
		return 0
	}
	if !guard.Authorize(c.Guard, root, i, via, r.errOut(), await) {
		return 1
	}
	if e = guard.Execute(i); e != nil {
		fmt.Fprintln(r.errOut(), e)
		return 1
	}
	return 0
}

func (r *runner) guardAwait(p api.DecisionCreateParams, timeout time.Duration) (api.Decision, error) {
	data, e := r.request("cli:guard:create", api.MethodDecisionCreate, p)
	if e != nil {
		return api.Decision{}, e
	}
	var response api.Response
	if e = json.Unmarshal(data, &response); e != nil {
		return api.Decision{}, e
	}
	if response.Err != nil {
		return api.Decision{}, response.Err
	}
	var created api.DecisionResult
	if e = json.Unmarshal(response.Result, &created); e != nil {
		return api.Decision{}, e
	}
	ms := uint64(timeout / time.Millisecond)
	data, e = r.request("cli:guard:wait", api.MethodDecisionWait, api.DecisionWaitParams{DecisionID: created.Decision.DecisionID, TimeoutMS: &ms})
	if e != nil {
		return api.Decision{}, e
	}
	if e = json.Unmarshal(data, &response); e != nil {
		return api.Decision{}, e
	}
	if response.Err != nil {
		return api.Decision{}, response.Err
	}
	var resolved api.DecisionResult
	e = json.Unmarshal(response.Result, &resolved)
	return resolved.Decision, e
}
