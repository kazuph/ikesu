package cli

import (
	"encoding/json"
	"fmt"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/guard"
	"os"
	"strings"
)

func (r *runner) guardCLI(a []string) int {
	if len(a) == 0 {
		fmt.Fprintln(r.errOut(), "usage: gtx guard <check|exec|compare> [--cwd PATH] [--launch-cwd PATH] -- COMMAND [ARG...]")
		return 2
	}
	sub := a[0]
	a = a[1:]
	cwd, _ := os.Getwd()
	launch := r.o.getenv("GTX_GUARD_LAUNCH_CWD")
	via := "shim"
	for len(a) > 0 && a[0] != "--" && strings.HasPrefix(a[0], "--") {
		if len(a) < 2 {
			fmt.Fprintln(r.errOut(), "guard option requires a value")
			return 2
		}
		switch a[0] {
		case "--cwd":
			cwd = a[1]
		case "--launch-cwd":
			launch = a[1]
		case "--via":
			via = a[1]
		default:
			fmt.Fprintln(r.errOut(), "unknown guard option:", a[0])
			return 2
		}
		a = a[2:]
	}
	if len(a) > 0 && a[0] == "--" {
		a = a[1:]
	}
	if len(a) == 0 {
		fmt.Fprintln(r.errOut(), "guard requires a command")
		return 2
	}
	c, e := config.Load()
	if e != nil {
		fmt.Fprintln(r.errOut(), e)
		return 1
	}
	i := guard.Input{Command: a[0], Args: a[1:], Cwd: cwd, LaunchCwd: launch, Env: guard.EnvMap(os.Environ())}
	if sub == "check" {
		if e = json.NewEncoder(r.out()).Encode(guard.Evaluate(c.Guard, i)); e != nil {
			fmt.Fprintln(r.errOut(), e)
			return 1
		}
		return 0
	}
	_ = via
	fmt.Fprintln(r.errOut(), "unsupported guard operation:", sub)
	return 2
}
