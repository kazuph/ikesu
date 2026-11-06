package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
)

func (r *runner) statusCLI(args []string) int {
	if len(args) > 1 || len(args) == 1 && args[0] != "--json" {
		return usageErr(r, "usage: ikesu status [--json]")
	}
	server := map[string]any{"app_loop_stall": nil, "status": "not_running", "running": false, "session": r.o.Session, "socket": api.APISocket(r.o.Session, r.o.Explicit, r.o.getenv)}
	raw, err := r.request("cli:status", api.MethodPing, api.EmptyParams{})
	if err == nil {
		var resp api.Response
		var pong api.PongResult
		if err := json.Unmarshal(raw, &resp); err != nil {
			return usageErr(r, err.Error())
		}
		if resp.Err != nil {
			fmt.Fprintln(r.errOut(), string(raw))
			return 1
		}
		if err := json.Unmarshal(resp.Result, &pong); err != nil {
			return usageErr(r, err.Error())
		}
		server["status"], server["running"] = "running", true
		if pong.LoopStall != nil {
			server["app_loop_stall"] = pong.LoopStall
		}
		server["version"], server["protocol"], server["capabilities"] = pong.Version, pong.Protocol, pong.Capabilities
		server["compatible"] = pong.Protocol == api.Protocol
		server["restart_needed"] = pong.Version != r.o.Version || pong.Protocol != api.Protocol
	}
	if len(args) == 0 {
		fmt.Fprintf(r.out(), "ikesu server: %s (session %s)\n", server["status"], r.o.Session)
		if stall, ok := server["app_loop_stall"].(*api.LoopStallStatus); ok {
			fmt.Fprintf(r.out(), "app loop stalls: %d, longest: %dms, current: %dms\n", stall.Count, stall.LongestMillis, stall.CurrentMillis)
			if stall.CurrentMillis > 0 {
				if stall.PaneLocksKnown {
					fmt.Fprintf(r.out(), "pane locks currently held (candidates): %v\n", stall.LockedPanes)
				} else {
					fmt.Fprintln(r.out(), "pane locks currently held: unknown")
				}
			}
		} else {
			fmt.Fprintln(r.out(), "app loop stalls: unavailable (server is stopped or does not support diagnostics)")
		}
		return 0
	}
	binary, _ := os.Executable()
	returnCode := 0
	if err := json.NewEncoder(r.out()).Encode(map[string]any{
		"client": map[string]any{"version": r.o.Version, "protocol": api.Protocol, "endpoint_generation": endpoint.Generation, "binary": binary, "session": r.o.Session},
		"server": server,
		"update": map[string]any{"restart_needed": server["restart_needed"] == true},
	}); err != nil {
		fmt.Fprintln(r.errOut(), err)
		returnCode = 1
	}
	return returnCode
}
