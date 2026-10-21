package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func (r *runner) server(args []string) int {
	if len(args) == 0 {
		return usageErr(r, "usage: ikesu server <stop|restart|reload-config|ping>")
	}
	switch args[0] {
	case "help", "--help", "-h":
		fmt.Fprintln(r.out(), "ikesu server commands:\n  ikesu server                run as headless server\n  ikesu server stop           stop the running server via the API socket\n  ikesu server restart        restart the running server and restore its saved session\n  ikesu server reload-config  reload config.toml in the running server\n  ikesu server ping           query the running server\n  ikesu server live-handoff   hand off live panes to a new local server")
		return 0
	case "live-handoff":
		var p api.ServerLiveHandoffParams
		for i := 1; i < len(args); i++ {
			if i+1 >= len(args) {
				return usageErr(r, "usage: ikesu server live-handoff [--import-exe <path>] [--expected-protocol <n>] [--expected-version <version>]")
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
	case "reload-config":
		if len(args) != 1 {
			return usageErr(r, "usage: ikesu server reload-config")
		}
		return r.printResult("cli:server:reload-config", api.MethodServerReloadConfig, api.EmptyParams{})
	case "stop":
		if len(args) != 1 {
			return usageErr(r, "usage: ikesu server stop")
		}
		return r.sendOK(api.MethodServerStop, api.EmptyParams{})
	case "restart":
		if len(args) != 1 {
			return usageErr(r, "usage: ikesu server restart")
		}
		return r.restartServer()
	case "ping":
		if len(args) != 1 {
			return usageErr(r, "usage: ikesu server ping")
		}
		return r.printResult("cli:ping", api.MethodPing, api.EmptyParams{})
	default:
		fmt.Fprintf(r.errOut(), "unknown server command: %s\n", args[0])
		return 2
	}
}

// Keep the original transport open: EOF identifies retirement of that server,
// even if the successor has the same version and socket pathname.
func (r *runner) restartServer() int {
	conn, err := net.DialTimeout("unix", r.socketPath(), 3*time.Second)
	if err != nil {
		fmt.Fprintln(r.errOut(), string(r.serverNotRunning("cli:request")))
		return 1
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if err := api.WriteRequest(conn, api.Request{ID: "cli:request", Method: api.MethodServerRestart, Params: api.MarshalParams(api.EmptyParams{})}); err != nil {
		fmt.Fprintln(r.errOut(), string(r.serverNotRunning("cli:request")))
		return 1
	}
	reader := bufio.NewReader(conn)
	raw, err := readRawLine(reader)
	if err != nil {
		fmt.Fprintln(r.errOut(), string(r.serverNotRunning("cli:request")))
		return 1
	}
	var response api.Response
	if err := json.Unmarshal(raw, &response); err != nil {
		fmt.Fprintf(r.errOut(), "ikesu: invalid server response: %v\n", err)
		return 1
	}
	if response.Err != nil {
		fmt.Fprintln(r.errOut(), string(raw))
		return 1
	}
	if _, err := io.Copy(io.Discard, reader); err != nil {
		fmt.Fprintf(r.errOut(), "ikesu: server did not stop after restart: %v\n", err)
		return 1
	}
	_ = conn.Close()
	return r.waitForServer()
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
				var pong api.PongResult
				if json.Unmarshal(resp.Result, &pong) == nil && pong.Type == "pong" {
					return 0
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Fprintln(r.errOut(), "ikesu: server did not come back after restart")
	return 1
}
