package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kazuph/goterrace/internal/api"
)

func (r *runner) server(args []string) int {
	if len(args) == 0 {
		return usageErr(r, "usage: gtx server <stop|restart|ping>")
	}
	switch args[0] {
	case "stop":
		if len(args) != 1 {
			return usageErr(r, "usage: gtx server stop")
		}
		return r.sendOK(api.MethodServerStop, api.EmptyParams{})
	case "restart":
		if len(args) != 1 {
			return usageErr(r, "usage: gtx server restart")
		}
		if code := r.sendOK(api.MethodServerRestart, api.EmptyParams{}); code != 0 {
			return code
		}
		return r.waitForServer()
	case "ping":
		if len(args) != 1 {
			return usageErr(r, "usage: gtx server ping")
		}
		return r.printResult("cli:ping", api.MethodPing, api.EmptyParams{})
	default:
		fmt.Fprintf(r.errOut(), "unknown server command: %s\n", args[0])
		return 2
	}
}

// waitForServer polls ping until the restarted server answers, so scripts
// can chain commands right after `gtx server restart`.
func (r *runner) waitForServer() int {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := r.request("cli:ping", api.MethodPing, api.EmptyParams{})
		if err == nil {
			var resp api.Response
			if json.Unmarshal(raw, &resp) == nil && resp.Err == nil {
				return 0
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Fprintln(r.errOut(), "gtx: server did not come back after restart")
	return 1
}
