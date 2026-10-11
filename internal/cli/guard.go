package cli

import (
	"encoding/json"
	"fmt"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/guard"
	"os"
	"time"
)

func (r *runner) guardCLI(a []string) int {
	if len(a) == 0 || a[0] == "--help" || a[0] == "-h" {
		fmt.Fprintln(r.out(), "gtx guard exec -- <cmd> [args...]\ngtx guard line -- <command line>\ngtx guard check -- <cmd> [args...] [--cwd DIR] [--launch-dir DIR]\ngtx guard log [--lines N] [--json]\ngtx guard compare [--guard-log PATH] [--ai-guard-log PATH] [--window SEC] [--since EPOCH]\ngtx guard status")
		return 0
	}
	sub := a[0]
	a = a[1:]
	c, e := config.Load()
	if e != nil {
		fmt.Fprintln(r.errOut(), e)
		return 1
	}
	root := api.StateDir()
	cwd, _ := os.Getwd()
	launch := r.o.getenv("GTX_GUARD_LAUNCH_CWD")
	via := "shim"
	i := guard.Input{Cwd: cwd, LaunchCwd: launch, Env: guard.EnvMap(os.Environ())}
	writeJSON := func(v any) int {
		enc := json.NewEncoder(r.out())
		enc.SetIndent("", "  ")
		if e := enc.Encode(v); e != nil {
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
		if !guard.AuthorizeLine(c.Guard, root, i, argv[0], r.errOut(), r.guardAwait) {
			return 1
		}
		return 0
	}
	if !guard.Authorize(c.Guard, root, i, via, r.errOut(), r.guardAwait) {
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
