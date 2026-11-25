package cli_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/cli"
	"github.com/kazuph/ikesu/internal/snapshot"
)

// The race run can exceed five seconds because a real server and PTY child
// executables are required to check actual process ancestry and delivery.
// The independent caller checks run together.
func TestPaneSnapshotCLIThroughServer(t *testing.T) {
	// Keep the Unix socket path short on macOS.
	dir, err := os.MkdirTemp("/tmp", "ikesu-snap-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	session := "snapshot"
	socket := api.APISocketPath(filepath.Join(dir, session))
	cmd := exec.Command(self, "--snapshot-cli-test-child", "server", session)
	cmd.Dir = dir
	cmd.Env = append(apiEnvWithoutIKESU(), "IKESU_STATE_DIR="+dir, "IKESU_CONFIG="+filepath.Join(dir, "config.toml"), "SHELL=/bin/sh")
	var serverLog bytes.Buffer
	cmd.Stdout, cmd.Stderr = &serverLog, &serverLog
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if conn, err := net.DialTimeout("unix", socket, time.Second); err == nil {
			_, _ = snapshotRPC(conn, api.MethodServerStop, api.EmptyParams{})
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("server exit: %v: %s", err, serverLog.String())
			}
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Error("server did not stop")
		}
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if conn, err := net.DialTimeout("unix", socket, time.Second); err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("isolated snapshot test server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	call := func(method string, params any) api.Response {
		t.Helper()
		conn, err := net.DialTimeout("unix", socket, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := snapshotRPC(conn, method, params)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	ok := func(method string, params any) json.RawMessage {
		t.Helper()
		resp := call(method, params)
		if resp.Err != nil {
			t.Fatalf("%s: %v", method, resp.Err)
		}
		return resp.Result
	}
	var stdout, stderr bytes.Buffer
	run := func(wantCode int, args ...string) string {
		t.Helper()
		stdout.Reset()
		stderr.Reset()
		opts := cli.Options{Session: session, Explicit: true, Stdout: &stdout, Stderr: &stderr, Getenv: func(key string) string {
			if key == "IKESU_STATE_DIR" {
				return dir
			}
			return ""
		}}
		if code := cli.Run(args, opts); code != wantCode {
			t.Fatalf("%v exit=%d want=%d stdout=%q stderr=%q", args, code, wantCode, stdout.String(), stderr.String())
		}
		return stdout.String()
	}
	sendInput := func(text string) {
		t.Helper()
		ok(api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: "p1", Text: text, Keys: []string{"Enter"}})
	}
	waitOutput := func(line string) {
		t.Helper()
		timeout := uint64(3000)
		resp := ok(api.MethodPaneWaitForOutput, api.PaneWaitForOutputParams{
			PaneID: "p1", Match: api.OutputMatch{Type: "substring", Value: line}, TimeoutMs: &timeout,
		})
		if !bytes.Contains(resp, []byte(`"type":"output_matched"`)) {
			t.Fatalf("did not see %q in the sender pane: %s", line, resp)
		}
	}

	ok(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: new("p1"), Direction: "right"})
	for _, pane := range []string{"p1", "p2"} {
		ok(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: pane, Source: "test", Agent: "codex", State: "blocked"})
	}
	sendInput(`stty -echo; PS1=''; printf '> older turn\nold answer\n\033[32m› latest request\033[0m\nanswer AWS_SECRET_KEY=example-value\nSNAPSHOT_%s\n' READY`)
	waitOutput("SNAPSHOT_READY")
	const secret = "SNAPSHOT_TEST_ONLY"
	sendInput(`printf '%s\n' '{"password":"SNAPSHOT_TEST_ONLY"}' 'token: SNAPSHOT_TEST_ONLY' '-----BEGIN PRIVATE KEY-----' 'SNAPSHOT_TEST_ONLY' '-----END PRIVATE KEY-----'; printf 'REDACT_%s\n' READY`)
	waitOutput("REDACT_READY")

	jsonText := run(0, "pane", "snapshot", "p1", "--json", "--save-log")
	var result snapshot.Result
	if err := json.Unmarshal([]byte(jsonText), &result); err != nil {
		t.Fatal(err)
	}
	if result.Mode != "handoff" || result.PaneID != "p1" || !result.BoundaryFound ||
		!strings.HasPrefix(result.Markdown, "# ikesu handoff:") || !strings.Contains(result.LastTurn, "latest request") ||
		strings.Contains(result.LastTurn, "older turn") || strings.Contains(jsonText, "example-value") || strings.Contains(jsonText, secret) {
		t.Fatalf("unexpected default handoff: %s", jsonText)
	}
	defer os.Remove(result.LogPath)
	info, err := os.Stat(result.LogPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("log file %q must exist with mode 0600: %v %v", result.LogPath, info, err)
	}
	saved, err := os.ReadFile(result.LogPath)
	if err != nil || !bytes.Contains(saved, []byte("older turn")) || bytes.Contains(saved, []byte("example-value")) || bytes.Contains(saved, []byte(secret)) {
		t.Fatalf("unexpected saved log: %q (%v)", saved, err)
	}
	body := strings.TrimSuffix(run(0, "pane", "snapshot", "p1"), "\n")
	if !strings.HasPrefix(body, "# ikesu handoff:") {
		t.Fatalf("plain default: %q", body)
	}
	last := run(0, "pane", "snapshot", "p1", "--last-turn", "--lines", "1", "--max-chars", "500")
	if !strings.HasPrefix(last, "# ikesu last turn:") || !strings.Contains(last, "latest request") {
		t.Fatalf("last-turn output: %q", last)
	}

	if strings.Contains(body, secret) {
		t.Fatalf("plaintext Markdown leaked fixture data: %q", body)
	}

	// Invoke the CLI under p1's real shell. SSH_TTY selects its PTY as
	// the clipboard output, keeping the host clipboard isolated.
	entry := "'" + strings.ReplaceAll(self, "'", "'\\''") + "' --snapshot-cli-test-child"
	readFile := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	sendInput("SSH_TTY=fixture DISPLAY= WAYLAND_DISPLAY= " + entry + " pane snapshot --json --copy --send p2 >combined.json 2>combined.err; printf 'COMBINED_%s\\n' READY")
	waitOutput("COMBINED_READY")
	var combined snapshot.Result
	if data := readFile("combined.json"); json.Unmarshal([]byte(data), &combined) != nil || combined.Markdown != body {
		t.Fatalf("combined stdout is not a single snapshot: %q", data)
	}
	if diagnostic := readFile("combined.err"); !strings.Contains(diagnostic, "copied ") || !strings.Contains(diagnostic, "sent 1 message(s)") {
		t.Fatalf("copy/send diagnostics did not go to stderr: %q", diagnostic)
	}
	inbox := run(0, "inbox", "--to", "p2")
	if !strings.Contains(inbox, "-> p2: "+body) || strings.Contains(inbox, secret) {
		t.Fatalf("recipient inbox does not contain only the redacted Markdown: %q", inbox)
	}

	// Both commands must resolve p1 from ancestry without the environment,
	// and reject a leaked p2 value. Start the four independent reads together.
	sendInput("printf '%s' \"$$\" >caller.pid; " +
		"env -u IKESU_PANE_ID " + entry + " pane current >current.json & " +
		"env -u IKESU_PANE_ID " + entry + " pane snapshot --last-turn --json >caller.json & " +
		"IKESU_PANE_ID=p2 " + entry + " pane current >stale-current.out 2>&1 & " +
		"IKESU_PANE_ID=p2 " + entry + " pane snapshot --last-turn >stale-snapshot.out 2>&1 & " +
		"wait; printf 'ANCESTRY_%s\\n' READY")
	waitOutput("ANCESTRY_READY")
	var current api.Response
	var currentResult api.PaneCurrentResult
	var caller snapshot.Result
	if err := json.Unmarshal([]byte(readFile("current.json")), &current); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(current.Result, &currentResult); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(readFile("caller.json")), &caller); err != nil || caller.PaneID != "p1" || caller.PaneID != currentResult.Pane.PaneID {
		t.Fatalf("ancestry: current=%q snapshot=%q err=%v", currentResult.Pane.PaneID, caller.PaneID, err)
	}
	for _, file := range []string{"stale-current.out", "stale-snapshot.out"} {
		if output := readFile(file); !strings.Contains(output, "could not be proven from process ancestry") {
			t.Fatalf("stale caller accepted by %s: %q", file, output)
		}
	}

	pid, err := strconv.ParseUint(readFile("caller.pid"), 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	callerParams := api.PaneSnapshotParams{Mode: "last_turn", CallerProcessID: new(uint32(pid))}
	var apiSnapshot api.PaneSnapshotResult[snapshot.Result]
	if err := json.Unmarshal(ok(api.MethodPaneSnapshot, callerParams), &apiSnapshot); err != nil || apiSnapshot.Snapshot.PaneID != caller.PaneID {
		t.Fatalf("API ancestry: %+v err=%v", apiSnapshot, err)
	}
	callerParams.CallerPaneID = new("p2")
	if resp := call(api.MethodPaneSnapshot, callerParams); resp.Err == nil || resp.Err.Code != api.ErrPaneNotFound {
		t.Fatalf("API accepted stale caller: %+v", resp)
	}
	callerParams.CallerProcessID = nil
	if resp := call(api.MethodPaneSnapshot, callerParams); resp.Err == nil || resp.Err.Code != api.ErrPaneNotFound {
		t.Fatalf("API accepted unproven caller: %+v", resp)
	}

	var workspaces api.WorkspaceListResult
	if err := json.Unmarshal(ok(api.MethodWorkspaceList, api.EmptyParams{}), &workspaces); err != nil {
		t.Fatal(err)
	}
	ws := workspaces.Workspaces[0]
	if result.Workspace != ws.Label || result.Workspace != filepath.Base(dir) {
		t.Fatalf("automatic workspace label: snapshot=%q list=%q", result.Workspace, ws.Label)
	}
	ok(api.MethodWorkspaceRename, api.WorkspaceRenameParams{WorkspaceID: ws.WorkspaceID, Label: "Snapshot's manual name"})
	var renamed snapshot.Result
	if err := json.Unmarshal([]byte(run(0, "pane", "snapshot", "p1", "--json")), &renamed); err != nil || renamed.Workspace != "Snapshot's manual name" {
		t.Fatalf("manual workspace label: %q err=%v", renamed.Workspace, err)
	}

	run(2, "pane", "snapshot", "--last-turn", "--handoff")
	run(2, "pane", "snapshot", "--send", "*")
	run(2, "pane", "snapshot", "p1", "--last-turn", "--save-log")
	for _, params := range []api.PaneSnapshotParams{
		{PaneID: "p1", Mode: "invalid"},
		{PaneID: "p1", Mode: "last_turn", SaveLog: true},
		{PaneID: "p1", Mode: "handoff", Lines: new(uint32(0))},
		{PaneID: "p1", Mode: "handoff", Lines: new(uint32(5001))},
		{PaneID: "p1", Mode: "handoff", MaxChars: new(uint32(499))},
		{PaneID: "p1", Mode: "handoff", MaxChars: new(uint32(100001))},
	} {
		if resp := call(api.MethodPaneSnapshot, params); resp.Err == nil || resp.Err.Code != api.ErrInvalidRequest {
			t.Fatalf("expected invalid_request for %+v, got %+v", params, resp)
		}
	}
}

func TestPaneSnapshotHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"pane", "help"}, {"pane", "snapshot", "help"}} {
		var out bytes.Buffer
		if code := cli.Run(args, cli.Options{Stdout: &out}); code != 0 || !strings.Contains(out.String(), "ikesu pane snapshot") {
			t.Fatalf("%v exit=%d output=%q", args, code, out.String())
		}
	}
	var out bytes.Buffer
	cli.Run([]string{"pane", "snapshot", "help"}, cli.Options{Stdout: &out})
	if !strings.Contains(out.String(), "--save-log is only supported in handoff mode") {
		t.Fatalf("missing handoff-only save help: %q", out.String())
	}
}

func apiEnvWithoutIKESU() []string {
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "IKESU_") {
			env = append(env, entry)
		}
	}
	return env
}

func snapshotRPC(conn net.Conn, method string, params any) (api.Response, error) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
	var response api.Response
	if err := api.WriteRequest(conn, api.Request{ID: method, Method: method, Params: api.MarshalParams(params)}); err != nil {
		return response, err
	}
	err := json.NewDecoder(bufio.NewReader(conn)).Decode(&response)
	return response, err
}
