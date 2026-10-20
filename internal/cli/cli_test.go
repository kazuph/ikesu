package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
)

func testEnv(stateDir string) func(string) string {
	return func(key string) string {
		if key == "GTX_STATE_DIR" {
			return stateDir
		}
		return ""
	}
}

func runCLI(t *testing.T, o Options, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	o.Stdout = &out
	o.Stderr = &errb
	code := Run(args, o)
	return code, out.String(), errb.String()
}

func TestPaneListNoServer(t *testing.T) {
	dir := t.TempDir()
	code, out, errOut := runCLI(t, Options{Getenv: testEnv(dir)}, "pane", "list")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (stderr=%q)", code, errOut)
	}
	if out != "" {
		t.Fatalf("unexpected stdout: %q", out)
	}
	var resp api.Response
	if err := json.Unmarshal([]byte(strings.TrimSpace(errOut)), &resp); err != nil {
		t.Fatalf("stderr is not a response: %v", err)
	}
	if resp.Err == nil || resp.Err.Code != api.ErrServerNotRunning {
		t.Fatalf("expected server_not_running, got %s", errOut)
	}
	if resp.ID != "cli:pane:list" {
		t.Fatalf("id = %q", resp.ID)
	}
}

func TestPaneGetUsage(t *testing.T) {
	code, _, errOut := runCLI(t, Options{Getenv: testEnv(t.TempDir())}, "pane", "get")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, "usage: ikesu pane get") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestPaneReadBadSource(t *testing.T) {
	code, _, errOut := runCLI(t, Options{Getenv: testEnv(t.TempDir())},
		"pane", "read", "p1", "--source", "bogus")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, "invalid read source") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestReportAgentRequiresFlags(t *testing.T) {
	code, _, _ := runCLI(t, Options{Getenv: testEnv(t.TempDir())},
		"pane", "report-agent", "p1", "--source", "x")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

// fakeServer runs a unix-socket API server that replays one canned response
// per request and records the requests it received.
type fakeServer struct {
	sock  string
	ln    net.Listener
	reqs  []api.Request
	reply api.Response
	done  chan struct{}
}

func newFakeServer(t *testing.T, reply api.Response) *fakeServer {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "gtx.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	fs := &fakeServer{sock: sock, ln: ln, reply: reply, done: make(chan struct{})}
	go func() {
		defer close(fs.done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go fs.serve(conn)
		}
	}()
	return fs
}

func (fs *fakeServer) serve(conn net.Conn) {
	defer conn.Close()
	rd := bufio.NewReader(conn)
	for {
		req, err := api.ReadRequest(rd)
		if err != nil {
			return
		}
		fs.reqs = append(fs.reqs, *req)
		resp := fs.reply
		resp.ID = req.ID
		_ = api.WriteResponse(conn, resp)
	}
}

func (fs *fakeServer) close() { fs.ln.Close(); <-fs.done }

func sockEnv(sock string) func(string) string {
	return func(key string) string {
		if key == "GTX_SOCKET_PATH" {
			return sock
		}
		return ""
	}
}

func TestPaneListPrintsResponse(t *testing.T) {
	reply := api.Success("", api.PaneListResult{
		Type: "pane_list",
		Panes: []api.PaneInfo{{
			PaneID: "p1", TerminalID: "t1", WorkspaceID: "s1", TabID: "s1:t1",
			AgentStatus: "unknown",
		}},
	})
	fs := newFakeServer(t, reply)
	defer fs.close()

	code, out, _ := runCLI(t, Options{Getenv: sockEnv(fs.sock)}, "pane", "list")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var resp api.Response
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); err != nil {
		t.Fatalf("stdout not a response: %v", err)
	}
	if resp.ID != "cli:pane:list" {
		t.Fatalf("id = %q", resp.ID)
	}
	var res api.PaneListResult
	if err := json.Unmarshal(resp.Result, &res); err != nil || len(res.Panes) != 1 {
		t.Fatalf("result mismatch: %v %s", err, out)
	}
	if fs.reqs[0].Method != api.MethodPaneList {
		t.Fatalf("method = %q", fs.reqs[0].Method)
	}
}

func TestPaneReadPrintsRawText(t *testing.T) {
	reply := api.Success("", api.PaneReadResultEnvelope{
		Type: "pane_read",
		Read: api.PaneReadResult{PaneID: "p1", Text: "hello\nworld\n"},
	})
	fs := newFakeServer(t, reply)
	defer fs.close()

	code, out, _ := runCLI(t, Options{Getenv: sockEnv(fs.sock)}, "pane", "read", "p1")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if out != "hello\nworld\n" {
		t.Fatalf("pane read should print raw text, got %q", out)
	}
	var params api.PaneReadParams
	if err := fs.reqs[0].DecodeParams(&params); err != nil {
		t.Fatal(err)
	}
	if params.Source != "recent" || !params.StripANSI || params.Format != "text" {
		t.Fatalf("defaults mismatch: %+v", params)
	}
}

func TestPaneRunSendsTextPlusEnter(t *testing.T) {
	fs := newFakeServer(t, api.Success("", api.OkResult{Type: "ok"}))
	defer fs.close()

	code, out, _ := runCLI(t, Options{Getenv: sockEnv(fs.sock)},
		"pane", "run", "p1", "echo", "hi")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if out != "" {
		t.Fatalf("run is silent on success, got %q", out)
	}
	var params api.PaneSendInputParams
	if err := fs.reqs[0].DecodeParams(&params); err != nil {
		t.Fatal(err)
	}
	if params.Text != "echo hi" || len(params.Keys) != 1 || params.Keys[0] != "Enter" {
		t.Fatalf("params = %+v", params)
	}
}

func TestPaneCurrentUsesEnvAndPid(t *testing.T) {
	reply := api.Success("", api.PaneCurrentResult{
		Type: "pane_current",
		Pane: api.PaneInfo{PaneID: "p3"},
	})
	fs := newFakeServer(t, reply)
	defer fs.close()

	getenv := func(key string) string {
		switch key {
		case "GTX_SOCKET_PATH":
			return fs.sock
		case "GTX_PANE_ID":
			return "p_3"
		}
		return ""
	}
	code, out, _ := runCLI(t, Options{Getenv: getenv, Pid: 4242}, "pane", "current")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var params api.PaneCurrentParams
	if err := fs.reqs[0].DecodeParams(&params); err != nil {
		t.Fatal(err)
	}
	if params.CallerPaneID == nil || *params.CallerPaneID != "p_3" {
		t.Fatalf("caller_pane_id = %+v", params.CallerPaneID)
	}
	if params.CallerProcessID == nil || *params.CallerProcessID != 4242 {
		t.Fatalf("caller_process_id = %+v", params.CallerProcessID)
	}
	_ = out
}

func TestPaneSplitArgs(t *testing.T) {
	fs := newFakeServer(t, api.Success("", api.PaneInfoResult{
		Type: "pane_info",
		Pane: api.PaneInfo{PaneID: "p2"},
	}))
	defer fs.close()

	code, _, errOut := runCLI(t, Options{Getenv: sockEnv(fs.sock)},
		"pane", "split", "--direction", "right", "--ratio", "0.4", "--cwd", "/tmp",
		"--env", "A=B", "--focus")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%q", code, errOut)
	}
	var params api.PaneSplitParams
	if err := fs.reqs[0].DecodeParams(&params); err != nil {
		t.Fatal(err)
	}
	if params.Direction != "right" || params.Ratio == nil || *params.Ratio != 0.4 ||
		params.Cwd == nil || *params.Cwd != "/tmp" || params.Env["A"] != "B" || !params.Focus {
		t.Fatalf("params = %+v", params)
	}
}

func TestExplicitSessionOverridesEnvSocket(t *testing.T) {
	dir := t.TempDir()
	getenv := func(key string) string {
		if key == "GTX_SOCKET_PATH" {
			return "/nonexistent/elsewhere.sock"
		}
		if key == "GTX_STATE_DIR" {
			return dir
		}
		return ""
	}
	code, _, errOut := runCLI(t, Options{Getenv: getenv, Session: "mine", Explicit: true},
		"workspace", "list")
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(errOut, filepath.Join(dir, "mine", "gtx.sock")) {
		t.Fatalf("expected session socket path in error, got %q", errOut)
	}
}
