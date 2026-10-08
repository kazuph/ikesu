// Package cli implements the non-interactive gtx subcommands (pane,
// workspace, tab, server stop). Each command sends one request to the
// session's API socket and prints the raw response, matching herdr's CLI
// conventions: success responses go to stdout, errors to stderr, and
// silent "ok" commands print nothing.
package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kazuph/goterrace/internal/api"
)

// Options configures a CLI invocation.
type Options struct {
	// Session selects the session directory; "" means the default session.
	Session string
	// Explicit reports whether --session was given on the command line.
	// Explicit sessions ignore the GTX_SOCKET_PATH pane override.
	Explicit bool
	Stdout   io.Writer
	Stderr   io.Writer
	// Getenv overrides environment lookup (tests); nil uses os.Getenv.
	Getenv func(string) string
	// Pid overrides the caller process id for pane.current; 0 = os.Getpid().
	Pid uint32
}

func (o Options) getenv(key string) string {
	if o.Getenv != nil {
		return o.Getenv(key)
	}
	return os.Getenv(key)
}

func (o Options) pid() uint32 {
	if o.Pid != 0 {
		return o.Pid
	}
	return uint32(os.Getpid())
}

func (o Options) stdout() io.Writer {
	if o.Stdout != nil {
		return o.Stdout
	}
	return os.Stdout
}

func (o Options) stderr() io.Writer {
	if o.Stderr != nil {
		return o.Stderr
	}
	return os.Stderr
}

// Run executes one CLI command and returns its exit code.
func Run(args []string, o Options) int {
	r := &runner{o: o}
	if len(args) == 0 {
		fmt.Fprintln(r.errOut(), "usage: gtx <pane|workspace|tab|server|send|inbox|msg|run|log|agent> <command> ...")
		return 2
	}
	switch args[0] {
	case "pane":
		return r.pane(args[1:])
	case "workspace":
		return r.workspace(args[1:])
	case "tab":
		return r.tab(args[1:])
	case "server":
		return r.server(args[1:])
	case "send":
		return r.sendCLI(args[1:])
	case "inbox":
		return r.inboxCLI(args[1:])
	case "msg":
		return r.msgCLI(args[1:])
	case "run":
		return r.runCLI(args[1:])
	case "log":
		return r.logCLI(args[1:])
	case "agent":
		return r.agentCLI(args[1:])
	case "help", "--help", "-h":
		return r.helpCLI()
	case "__pane-run":
		return r.paneRunWrapper(args[1:])
	default:
		fmt.Fprintf(r.errOut(), "unknown command: %s\n", args[0])
		return 2
	}
}

type runner struct {
	o Options
}

func (r *runner) out() io.Writer    { return r.o.stdout() }
func (r *runner) errOut() io.Writer { return r.o.stderr() }

func (r *runner) socketPath() string {
	return api.APISocket(r.o.Session, r.o.Explicit, r.o.getenv)
}

// request sends one API request and returns the raw response line. Any
// transport failure is converted into a server_not_running error response,
// matching herdr's CLI behavior.
func (r *runner) request(id, method string, params any) ([]byte, error) {
	conn, err := net.DialTimeout("unix", r.socketPath(), 3*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if err := api.WriteRequest(conn, api.Request{
		ID:     id,
		Method: method,
		Params: api.MarshalParams(params),
	}); err != nil {
		return nil, err
	}
	rd := bufio.NewReader(conn)
	line, err := readRawLine(rd)
	if err != nil {
		return nil, err
	}
	return line, nil
}

// serverNotRunning synthesizes the same error shape herdr prints when the
// API socket is unreachable.
func (r *runner) serverNotRunning(id string) []byte {
	socket := r.socketPath()
	start := "gtx"
	if r.o.Explicit && r.o.Session != "" {
		start = "gtx --session " + r.o.Session
	}
	resp := api.Failure(id, api.ErrServerNotRunning,
		fmt.Sprintf("no gtx server is running at %s; run `%s` to start or attach it", socket, start))
	raw, _ := json.Marshal(resp)
	return raw
}

// printResult sends a request and prints the raw response: errors go to
// stderr with exit 1, successes to stdout with exit 0.
func (r *runner) printResult(id, method string, params any) int {
	raw, err := r.request(id, method, params)
	if err != nil {
		raw = r.serverNotRunning(id)
	}
	var resp api.Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		fmt.Fprintf(r.errOut(), "gtx: invalid server response: %v\n", err)
		return 1
	}
	if resp.Err != nil {
		fmt.Fprintln(r.errOut(), string(raw))
		return 1
	}
	fmt.Fprintln(r.out(), string(raw))
	return 0
}

// sendOK sends a request that succeeds silently (herdr's send_ok_request):
// nothing is printed on success.
func (r *runner) sendOK(method string, params any) int {
	raw, err := r.request("cli:request", method, params)
	if err != nil {
		raw = r.serverNotRunning("cli:request")
	}
	var resp api.Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		fmt.Fprintf(r.errOut(), "gtx: invalid server response: %v\n", err)
		return 1
	}
	if resp.Err != nil {
		fmt.Fprintln(r.errOut(), string(raw))
		return 1
	}
	return 0
}

// currentPaneID resolves the caller's pane via pane.current.
func (r *runner) currentPaneID(callerPaneID *string) (string, error) {
	pid := r.o.pid()
	raw, err := r.request("cli:pane:current", api.MethodPaneCurrent, api.PaneCurrentParams{
		CallerPaneID:    callerPaneID,
		CallerProcessID: &pid,
	})
	if err != nil {
		raw = r.serverNotRunning("cli:pane:current")
	}
	var resp api.Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	if resp.Err != nil {
		return "", resp.Err
	}
	var res api.PaneCurrentResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		return "", err
	}
	if res.Pane.PaneID == "" {
		return "", fmt.Errorf("pane.current response did not include pane id")
	}
	return res.Pane.PaneID, nil
}

// envPaneID returns the GTX_PANE_ID set inside gtx panes.
func (r *runner) envPaneID() string {
	return strings.TrimSpace(r.o.getenv("GTX_PANE_ID"))
}

func usageErr(r *runner, usage string) int {
	fmt.Fprintln(r.errOut(), usage)
	return 2
}

func flagValue(args []string, i *int) (string, bool) {
	if *i+1 >= len(args) {
		return "", false
	}
	v := args[*i+1]
	*i += 2
	return v, true
}

func parseFloatFlag(flag, value string) (float64, error) {
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || !isFinite(n) {
		return 0, fmt.Errorf("invalid value for %s: %s", flag, value)
	}
	return n, nil
}

func isFinite(f float64) bool {
	return f == f && f <= 1e308 && f >= -1e308
}

// readRawLine reads one newline-terminated JSON line and returns the bytes
// without the trailing newline.
func readRawLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	return line[:len(line)-1], nil
}
