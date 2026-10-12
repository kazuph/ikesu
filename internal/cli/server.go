package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/kazuph/goterrace/internal/api"
)

func (r *runner) server(args []string) int {
	if len(args) == 0 {
		return usageErr(r, "usage: gtx server <stop|restart|ping>")
	}
	switch args[0] {
	case "help", "--help", "-h":
		fmt.Fprintln(r.out(), "gtx server commands:\n  gtx server                run as headless server\n  gtx server stop           stop the running server via the API socket\n  gtx server restart        restart the running server and restore its saved session\n  gtx server ping           query the running server\n  gtx server live-handoff   hand off live panes to a new local server")
		return 0
	case "live-handoff":
		var p api.ServerLiveHandoffParams
		for i := 1; i < len(args); i++ {
			if i+1 >= len(args) {
				return usageErr(r, "usage: gtx server live-handoff [--import-exe <path>] [--expected-protocol <n>] [--expected-version <version>]")
			}
			key, value := args[i], args[i+1]
			i++
			switch key {
			case "--import-exe":
				p.ImportExe = &value
			case "--expected-version":
				p.ExpectedVersion = &value
			case "--expected-protocol":
				n, err := strconv.ParseUint(value, 10, 32)
				if err != nil {
					return usageErr(r, err.Error())
				}
				v := uint32(n)
				p.ExpectedProtocol = &v
			default:
				return usageErr(r, "unknown live-handoff option: "+key)
			}
		}
		return r.sendOK(api.MethodServerLiveHandoff, p)
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
