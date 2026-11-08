package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"

	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/guard"
	"github.com/kazuph/ikesu/internal/latency"
	"golang.org/x/sys/unix"
)

func serveProcessStarts(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "test-process-start.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_, start, err := guard.ApprovalProcess(latency.PeerPID(conn))
			if err == nil {
				_, _ = fmt.Fprintln(conn, start)
			}
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-done })
	return path
}

func TestGuardIdentityRejectsMissingOrFalseClaim(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	_, start, err := guard.ApprovalProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		params map[string]any
	}{
		{"missing", map[string]any{"pane_id": "p1"}},
		{"false-pid", map[string]any{"pane_id": "p1", "pid": os.Getppid(), "start_time": start}},
		{"false-start", map[string]any{"pane_id": "p1", "pid": os.Getpid(), "start_time": start + "-changed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := net.Dial("unix", api.APISocketPath(filepath.Join(s.stateDir, s.name)))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := api.WriteRequest(conn, api.Request{ID: tc.name, Method: api.MethodGuardIdentity, Params: api.MarshalParams(tc.params)}); err != nil {
				t.Fatal(err)
			}
			resp, err := api.ReadResponse(bufio.NewReader(conn))
			if err != nil {
				t.Fatal(err)
			}
			if resp.Err == nil || resp.Err.Code != "guard_identity_unknown" {
				t.Fatalf("unverified claim accepted: %+v", resp)
			}
		})
	}
}

func writeExitedGuardRequest(fd *os.File, req api.Request) error {
	return writeExitedGuardRequestAs(fd, req, false)
}

func writeExitedGuardRequestAs(fd *os.File, req api.Request, claimParent bool) error {
	cmd := exec.Command("/bin/sh", "-c", `IFS= read -r request; printf '%s\n' "$request" >&3`)
	cmd.ExtraFiles = []*os.File{fd}
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	claimPID := cmd.Process.Pid
	if claimParent {
		claimPID = os.Getpid()
	}
	_, start, err := guard.ApprovalProcess(claimPID)
	if err == nil {
		var params map[string]any
		err = json.Unmarshal(req.Params, &params)
		if err == nil {
			params["pid"], params["start_time"] = claimPID, start
			req.Params = api.MarshalParams(params)
			err = json.NewEncoder(in).Encode(req)
		}
	}
	_ = in.Close()
	waitErr := cmd.Wait()
	if err != nil {
		return err
	}
	return waitErr
}

func TestGuardSharedConnectionUsesClaimedParentPolicy(t *testing.T) {
	testGuardExitedWriter(t, true)
}

func testGuardExitedWriter(t *testing.T, claimParent bool) {
	pidfile := filepath.Join(t.TempDir(), "server-pid")
	wrapper := filepath.Join(t.TempDir(), "server-wrapper")
	if err := os.WriteFile(wrapper, []byte(fmt.Sprintf("#!/bin/sh\necho $$ > %q\nexec %q \"$@\"\n", pidfile, binPath)), 0700); err != nil {
		t.Fatal(err)
	}
	s := startVersionMode(t, nil, false, binPath, wrapper)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	b, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	serverPID, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	signalLossProcess(t, serverPID, syscall.SIGSTOP, binPath)
	stopped := true
	defer func() {
		if stopped {
			signalLossProcess(t, serverPID, syscall.SIGCONT, binPath)
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(serverPID)).Output()
		if err == nil && strings.HasPrefix(strings.TrimSpace(string(out)), "T") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("isolated server did not suspend")
		}
		time.Sleep(time.Millisecond)
	}
	conn, err := net.Dial("unix", api.APISocketPath(filepath.Join(s.stateDir, s.name)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	childFD, err := conn.(*net.UnixConn).File()
	if err != nil {
		t.Fatal(err)
	}
	err = writeExitedGuardRequestAs(childFD, api.Request{ID: "exited-peer", Method: api.MethodGuardIdentity, Params: api.MarshalParams(api.GuardIdentityParams{PaneID: "p1"})}, claimParent)
	childFD.Close()
	if err != nil {
		t.Fatal(err)
	}
	rawConn, err := conn.(*net.UnixConn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var readErr error
	if err := rawConn.Control(func(fd uintptr) {
		_, _, readErr = unix.Recvfrom(int(fd), make([]byte, 1), unix.MSG_PEEK|unix.MSG_DONTWAIT)
	}); err != nil {
		t.Fatal(err)
	}
	if readErr != unix.EAGAIN {
		t.Fatalf("pre-accept read: %v", readErr)
	}
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	signalLossProcess(t, serverPID, syscall.SIGCONT, binPath)
	stopped = false
	resp, err := api.ReadResponse(bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	if claimParent {
		var identity api.GuardIdentityResult
		if resp.Err != nil || json.Unmarshal(resp.Result, &identity) != nil || identity.Match {
			t.Fatalf("shared connection was not classified as its outside parent: %+v", resp)
		}
		var policy guard.Config
		if err := json.Unmarshal(identity.Policy, &policy); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(s.stateDir, ".allow-main")
		input := guard.Input{Command: "/usr/bin/touch", Args: []string{target}, Cwd: s.stateDir, Env: map[string]string{"HOME": s.stateDir, "PATH": "/usr/bin:/bin"}, VerifiedOutside: &guard.Marker{OutsidePane: true, Cwd: s.stateDir, Policy: &policy}}
		var diagnostic bytes.Buffer
		if guard.Authorize(policy, s.stateDir, input, "exec", &diagnostic, nil) || !strings.Contains(diagnostic.String(), "完全に禁止") {
			t.Fatalf("parent policy did not block protected command: %s", diagnostic.String())
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("protected file exists: %v", err)
		}
		return
	}
	if resp.Err == nil || resp.Err.Code != "guard_identity_unknown" {
		t.Fatalf("exited peer was not rejected as unknown: %+v", resp)
	}
}

func TestGuardIdentityPassedSocketUsesKernelPeer(t *testing.T) {
	if os.Getenv("IKESU_DEAD_PEER_HELPER") == "1" {
		conn, err := net.Dial("unix", os.Getenv("IKESU_TEST_SOCKET"))
		if err != nil {
			t.Fatal(err)
		}
		_, start, err := guard.ApprovalProcess(os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		if err := api.WriteRequest(conn, api.Request{ID: "before-transfer", Method: api.MethodGuardIdentity, Params: api.MarshalParams(api.GuardIdentityParams{PaneID: "p1", PID: os.Getpid(), StartTime: start})}); err != nil {
			t.Fatal(err)
		}
		resp, err := api.ReadResponse(bufio.NewReader(conn))
		if err != nil || resp.Err != nil {
			t.Fatalf("before transfer: %+v %v", resp, err)
		}
		file, err := conn.(*net.UnixConn).File()
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		defer conn.Close()
		if err := unix.Sendmsg(3, []byte{1}, unix.UnixRights(int(file.Fd())), nil, 0); err != nil {
			t.Fatal(err)
		}
		// Keep the original connection alive until the receiver has installed
		// its duplicate, then exit before the request tests peer identity.
		if n, err := unix.Read(3, make([]byte, 1)); err != nil || n != 1 {
			t.Fatalf("socket transfer acknowledgement: %d %v", n, err)
		}
		return
	}
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pair[0])
	tv := unix.NsecToTimeval((3 * time.Second).Nanoseconds())
	if err := unix.SetsockoptTimeval(pair[0], unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		unix.Close(pair[1])
		t.Fatal(err)
	}
	childFD := os.NewFile(uintptr(pair[1]), "peer-transfer")
	cmd := exec.Command(os.Args[0], "-test.run=^TestGuardIdentityPassedSocketUsesKernelPeer$")
	cmd.ExtraFiles = []*os.File{childFD}
	cmd.Env = append(os.Environ(), "CLIENT_UPGRADE_RAW_HELPER=1", "IKESU_DEAD_PEER_HELPER=1", "IKESU_TEST_SOCKET="+api.APISocketPath(filepath.Join(s.stateDir, s.name)))
	if err := cmd.Start(); err != nil {
		childFD.Close()
		t.Fatal(err)
	}
	childFD.Close()
	buf, oob := make([]byte, 1), make([]byte, unix.CmsgSpace(4))
	_, n, _, _, err := unix.Recvmsg(pair[0], buf, oob, 0)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := unix.ParseSocketControlMessage(oob[:n])
	if err != nil || len(messages) != 1 {
		t.Fatalf("socket rights: %v", err)
	}
	rights, err := unix.ParseUnixRights(&messages[0])
	if err != nil || len(rights) != 1 {
		t.Fatalf("socket descriptor: %v", err)
	}
	file := os.NewFile(uintptr(rights[0]), "dead-peer-connection")
	defer file.Close()
	conn, err := net.FileConn(file)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := unix.Write(pair[0], []byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, start, err := guard.ApprovalProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := api.WriteRequest(conn, api.Request{ID: "dead-peer", Method: api.MethodGuardIdentity, Params: api.MarshalParams(api.GuardIdentityParams{PaneID: "p1", PID: os.Getpid(), StartTime: start})}); err != nil {
		t.Fatal(err)
	}
	resp, err := api.ReadResponse(bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Err == nil || resp.Err.Code != "guard_identity_unknown" {
		t.Fatalf("transferred socket was not rejected as unknown: %+v", resp)
	}
}

func TestGuardDecisionRejectsWrongPaneAtCreation(t *testing.T) {
	t.Parallel()
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "outside-test", Agent: "claude", State: "idle"}, nil)
	conn, err := net.Dial("unix", api.APISocketPath(filepath.Join(s.stateDir, s.name)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	params := map[string]any{"kind": "command_guard", "title": "隔離した出所照合", "options": []map[string]string{{"id": "reject", "label": "却下", "role": "reject"}},
		"origin": map[string]string{"pane_id": "p1"}, "guard_context": map[string]any{"pane_id": "p1", "match": true}}
	if err := api.WriteRequest(conn, api.Request{ID: "outside-decision", Method: api.MethodDecisionCreate, Params: api.MarshalParams(params)}); err != nil {
		t.Fatal(err)
	}
	resp, err := api.ReadResponse(bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Err == nil {
		t.Fatal("server created a decision with an unproven pane origin")
	}
}

func TestGuardExecAfterLiveHandoff(t *testing.T) {
	t.Parallel()
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	if out, err := s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	cmd := exec.Command(binPath, "guard", "exec", "--", "/usr/bin/printf", "AFTER_HANDOFF")
	cmd.Env = []string{"HOME=" + s.stateDir, "PATH=/usr/bin:/bin", "SHELL=/bin/sh", "IKESU_STATE_DIR=" + s.stateDir,
		"IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + filepath.Join(s.stateDir, "guard", "agent-panes", "p_1.json"),
		"IKESU_SOCKET_PATH=" + api.APISocketPath(filepath.Join(s.stateDir, s.name)), "IKESU_SOCKET_PATH_EXPLICIT=1"}
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "AFTER_HANDOFF" {
		t.Fatalf("guard after handoff: %v %s", err, out)
	}
}

// One isolated server is reused across the real-process matrix, so it can
// exceed five seconds on the shared mini.
// Each entry starts a real process for several identity states, so the whole
// matrix can exceed five seconds on a shared mini.
func TestGuardOutsidePaneAllEntryPoints(t *testing.T) {
	t.Parallel()
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	first := "p1"
	s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &first, Direction: "right", Focus: false}, nil)
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: first, Source: "outside-entries", Agent: "claude", State: "idle"}, nil)
	root := s.stateDir
	marker := filepath.Join(root, "guard", "agent-panes", "p_1.json")
	dir, bin := filepath.Join(root, "guard", "shell", "p_1"), filepath.Join(root, "guard", "bin", "p_1")
	entry := filepath.Join(dir, "ikesu")
	tests := []struct {
		name, command, want string
		args                []string
		bash, zsh           bool
	}{
		{"refresh", entry, "generation-", []string{"guard", "refresh"}, false, false},
		{"exec", entry, "SAFE_ENTRY", []string{"guard", "exec", "--", "/usr/bin/printf", "SAFE_ENTRY"}, false, false},
		{"line", entry, "", []string{"guard", "line", "--", "true"}, false, false},
		{"claude-prefix", filepath.Join(dir, "claude-prefix"), "SAFE_ENTRY", []string{"/usr/bin/printf SAFE_ENTRY"}, false, false},
		{"shim", filepath.Join(bin, "git"), "git version", []string{"--version"}, false, false},
		{"bash-env", "/bin/bash", "SAFE_ENTRY", []string{"-c", "printf SAFE_ENTRY"}, true, false},
		{"zdotdir", "/bin/zsh", "SAFE_ENTRY", []string{"-c", "printf SAFE_ENTRY"}, false, true},
	}
	run := func(state string, available bool) {
		for _, tc := range tests {
			t.Run(state+"/"+tc.name, func(t *testing.T) {
				socket := api.APISocketPath(filepath.Join(root, s.name))
				if !available {
					socket = filepath.Join(root, "missing.sock")
				}
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, tc.command, tc.args...)
				cmd.Env = []string{"HOME=" + root, "PATH=/usr/bin:/bin", "SHELL=/bin/bash", "IKESU_STATE_DIR=" + root,
					"IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + marker, "IKESU_GUARD_BIN=" + bin,
					"IKESU_GUARD_SHELL_DIR=" + dir, "IKESU_SOCKET_PATH=" + socket, "IKESU_SOCKET_PATH_EXPLICIT=1"}
				if tc.bash {
					cmd.Env = append(cmd.Env, "BASH_ENV="+filepath.Join(dir, "bash-env"))
				}
				if tc.zsh {
					cmd.Env = append(cmd.Env, "ZDOTDIR="+dir)
				}
				out, err := cmd.CombinedOutput()
				if available && (err != nil || !strings.Contains(string(out), tc.want)) {
					t.Fatalf("safe entry failed: %v %s", err, out)
				}
				if !available && (err == nil || strings.Contains(string(out), "SAFE_ENTRY") || strings.Contains(string(out), "git version")) {
					t.Fatalf("unverified entry ran: %v %s", err, out)
				}
			})
		}
	}
	// The test process is unrelated to the live shell whose marker it inherits.
	run("other-live-pane", true)
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	run("missing-marker-live-pane", true)
	s.rpc(api.MethodPaneClose, api.PaneTarget{PaneID: first}, nil)
	s.waitFor("closed marker", 3*time.Second, func(string) bool { _, err := os.Stat(marker); return os.IsNotExist(err) })
	run("closed-pane", true)
	if output, err := s.cli("server", "stop"); err != nil {
		t.Fatalf("stop isolated server: %v %s", err, output)
	}
	run("unavailable", false)
}

func TestGuardMatchingPaneCorruptMarkerFailsClosed(t *testing.T) {
	t.Parallel()
	runGuardMatchingInvalidMarker(t, false)
}

func TestGuardMatchingPanePolicyMismatchFailsClosed(t *testing.T) {
	t.Parallel()
	runGuardMatchingInvalidMarker(t, true)
}

func runGuardMatchingInvalidMarker(t *testing.T, mismatch bool) {
	t.Helper()
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	root := s.stateDir
	gate, ready, out, status := filepath.Join(root, "corrupt-gate"), filepath.Join(root, "corrupt-ready"), filepath.Join(root, "corrupt-output"), filepath.Join(root, "corrupt-status")
	if err := unix.Mkfifo(gate, 0600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "corrupt-worker.sh")
	body := "#!/bin/sh\nprintf ready > \"$3\"\nread go < \"$2\" || exit 2\n\"$1\" guard exec -- /usr/bin/printf NEVER_RAN > \"$4\" 2>&1\nprintf '%s' \"$?\" > \"$5\"\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }
	entry := filepath.Join(root, "guard", "shell", "p_1", "ikesu")
	s.send("/bin/sh " + quote(script) + " " + quote(entry) + " " + quote(gate) + " " + quote(ready) + " " + quote(out) + " " + quote(status) + "\r")
	s.waitFor("worker started", 5*time.Second, func(string) bool { _, err := os.Stat(ready); return err == nil })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "corrupt-test", Agent: "claude", State: "idle"}, nil)
	markerDir := filepath.Join(root, "guard", "agent-panes")
	if err := os.Chmod(markerDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(markerDir, 0700) })
	marker := filepath.Join(root, "guard", "agent-panes", "p_1.json")
	content := []byte("{")
	want := "目印ファイルが壊れています"
	if mismatch {
		original, err := os.ReadFile(marker)
		if err != nil {
			t.Fatal(err)
		}
		var stored map[string]any
		if err := json.Unmarshal(original, &stored); err != nil {
			t.Fatal(err)
		}
		policy, ok := stored["guard_config"].(map[string]any)
		if !ok {
			t.Fatal("missing stored policy")
		}
		policy["Mode"] = "record"
		content, err = json.Marshal(stored)
		if err != nil {
			t.Fatal(err)
		}
		want = "guard marker policy mismatch"
	}
	if err := os.WriteFile(marker, content, 0600); err != nil {
		t.Fatal(err)
	}
	sent := make(chan error, 1)
	go func() {
		f, err := os.OpenFile(gate, os.O_WRONLY, 0)
		if err == nil {
			_, err = f.WriteString("go\n")
			_ = f.Close()
		}
		sent <- err
	}()
	select {
	case err := <-sent:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not receive signal")
	}
	s.waitFor("corrupt marker rejected", 3*time.Second, func(string) bool { _, err := os.Stat(status); return err == nil })
	code, err := os.ReadFile(status)
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(code)) == "0" || !strings.Contains(string(output), want) || strings.Contains(string(output), "NEVER_RAN") {
		t.Fatalf("corrupt matched marker did not fail closed: status=%s output=%s", code, output)
	}
}

// The detached child and actual pane closure establish the identity transition;
// waiting for both real RPCs can exceed five seconds on a busy mini.
func TestGuardDecisionFailsIfPaneClosesAfterIdentity(t *testing.T) {
	t.Parallel()
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	first := "p1"
	s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &first, Direction: "right", Focus: false}, nil)
	root := s.stateDir
	gate1, gate2 := filepath.Join(root, "verify-gate"), filepath.Join(root, "decision-gate")
	for _, p := range []string{gate1, gate2} {
		if err := unix.Mkfifo(p, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ready, firstResult, secondResult := filepath.Join(root, "identity-ready"), filepath.Join(root, "identity-result"), filepath.Join(root, "decision-result")
	script := filepath.Join(root, "identity-transition.py")
	body := `import json, os, signal, socket, sys
signal.signal(signal.SIGHUP, signal.SIG_IGN)
sock, g1, g2, ready, result1, result2, start_sock = sys.argv[1:]
def wait(p):
    with open(p) as f: return f.readline().strip()
def rpc(method, params):
    s=socket.socket(socket.AF_UNIX); s.connect(sock)
    s.sendall((json.dumps({'id':'test','method':method,'params':params})+'\n').encode())
    data=b''
    while not data.endswith(b'\n'):
        part=s.recv(65536)
        if not part: raise RuntimeError('server closed')
        data+=part
    s.close(); return json.loads(data)
with open(ready,'w') as f: f.write(str(os.getpid()))
if wait(g1) != 'go': sys.exit(1)
st=socket.socket(socket.AF_UNIX); st.connect(start_sock); start=st.makefile().readline().strip(); st.close()
claim={'pid':os.getpid(),'start_time':start}
identity=rpc('guard.identity',dict({'pane_id':'p1'},**claim))
with open(result1+'.tmp','w') as f: json.dump(identity,f)
os.replace(result1+'.tmp',result1)
if wait(g2) != 'go': sys.exit(1)
ctx=dict({'pane_id':'p1','match':True,'policy_digest':identity['result']['policy_digest']},**claim)
params={'kind':'command_guard','title':'isolated transition','options':[{'id':'reject','label':'no','role':'reject'}],'origin':{'pane_id':'p1'},'guard_context':ctx}
result=rpc('decision.create',params)
with open(result2+'.tmp','w') as f: json.dump(result,f)
os.replace(result2+'.tmp',result2)
`
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }
	socket := api.APISocketPath(filepath.Join(root, s.name))
	args := []string{python, script, socket, gate1, gate2, ready, firstResult, secondResult, serveProcessStarts(t, root)}
	line := "nohup"
	for _, arg := range args {
		line += " " + quote(arg)
	}
	line += " > /dev/null 2>&1 < /dev/null &\r"
	s.send(line)
	s.waitFor("child ready", 5*time.Second, func(string) bool { _, err := os.Stat(ready); return err == nil })
	send := func(path, message string) error {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			fd, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK, 0)
			if err == nil {
				_, err = unix.Write(fd, []byte(message+"\n"))
				_ = unix.Close(fd)
				return err
			}
			if err != unix.ENXIO {
				return err
			}
			time.Sleep(time.Millisecond)
		}
		return fmt.Errorf("fifo reader unavailable")
	}
	t.Cleanup(func() {
		for _, path := range []string{gate1, gate2} {
			if fd, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK, 0); err == nil {
				_, _ = unix.Write(fd, []byte("abort\n"))
				_ = unix.Close(fd)
			}
		}
	})
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "identity-transition", Agent: "claude", State: "idle"}, nil)
	if err := send(gate1, "go"); err != nil {
		t.Fatal(err)
	}
	s.waitFor("matching identity", 5*time.Second, func(string) bool { _, err := os.Stat(firstResult); return err == nil })
	data, err := os.ReadFile(firstResult)
	if err != nil {
		t.Fatal(err)
	}
	var identity api.Response
	if err := json.Unmarshal(data, &identity); err != nil || identity.Err != nil {
		t.Fatalf("identity: %v %s", err, data)
	}
	var result api.GuardIdentityResult
	if err := json.Unmarshal(identity.Result, &result); err != nil || !result.Match {
		t.Fatalf("not matched before close: %v %s", err, data)
	}
	s.rpc(api.MethodPaneClose, api.PaneTarget{PaneID: "p1"}, nil)
	if err := send(gate2, "go"); err != nil {
		t.Fatal(err)
	}
	s.waitFor("closed-pane decision rejected", 5*time.Second, func(string) bool { _, err := os.Stat(secondResult); return err == nil })
	data, err = os.ReadFile(secondResult)
	if err != nil {
		t.Fatal(err)
	}
	var response api.Response
	if err := json.Unmarshal(data, &response); err != nil || response.Err == nil || response.Err.Code != "guard_identity_changed" {
		t.Fatalf("decision accepted after pane closed: %v %s", err, data)
	}
	var pending api.DecisionListResult
	s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
	if len(pending.Decisions) != 0 {
		t.Fatalf("stale decision was created: %+v", pending.Decisions)
	}
}

// Each entry runs under the actual pane shell and involves real processes;
// establishing and checking the identity can exceed five seconds on mini.
func TestGuardMatchingPaneAllEntryPoints(t *testing.T) {
	t.Parallel()
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	root := s.stateDir
	dir := filepath.Join(root, "guard", "shell", "p_1")
	bin := filepath.Join(root, "guard", "bin", "p_1")
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }
	python := `import json,os,socket,sys
st=socket.socket(socket.AF_UNIX); st.connect(sys.argv[3]); start=st.makefile().readline().strip(); st.close()
s=socket.socket(socket.AF_UNIX); s.connect(sys.argv[1])
s.sendall((json.dumps({'id':'match','method':'guard.identity','params':{'pane_id':'p1','pid':os.getpid(),'start_time':start}})+'\n').encode())
f=s.makefile(); r=json.loads(f.readline()); assert r['result']['match'] == (sys.argv[2] == 'true'), r
`
	probe := filepath.Join(root, "match-probe.py")
	probeCommand := "python3 " + quote(probe)
	startSock := serveProcessStarts(t, root)
	if err := os.WriteFile(probe, []byte(python), 0600); err != nil {
		t.Fatal(err)
	}
	commands := []struct{ name, command, want string }{
		{"refresh", quote(filepath.Join(dir, "ikesu")) + " guard refresh", "generation-"},
		{"exec", quote(filepath.Join(dir, "ikesu")) + " guard exec -- /usr/bin/printf SAFE_ENTRY", "SAFE_ENTRY"},
		{"line", quote(filepath.Join(dir, "ikesu")) + " guard line -- true", ""},
		{"claude-prefix", quote(filepath.Join(dir, "claude-prefix")) + " 'printf SAFE_ENTRY'", "SAFE_ENTRY"},
		{"shim", quote(filepath.Join(bin, "git")) + " --version", "git version"},
		{"bash-env", "BASH_ENV=" + quote(filepath.Join(dir, "bash-env")) + " /bin/bash -c 'printf SAFE_ENTRY'", "SAFE_ENTRY"},
		{"zdotdir", "ZDOTDIR=" + quote(dir) + " /bin/zsh -c 'printf SAFE_ENTRY'", "SAFE_ENTRY"},
	}
	boot := filepath.Join(root, "match-start-gate")
	if err := unix.Mkfifo(boot, 0600); err != nil {
		t.Fatal(err)
	}
	var script strings.Builder
	script.WriteString("#!/bin/sh\nprintf ready > " + quote(filepath.Join(root, "match-started")) + "\nread go < " + quote(boot) + " || exit 2\n")
	script.WriteString(probeCommand + " " + quote(api.APISocketPath(filepath.Join(root, s.name))) + " true " + quote(startSock) + " > " + quote(filepath.Join(root, "match-probe-output")) + " 2>&1\n")
	script.WriteString("printf '%s' $? > " + quote(filepath.Join(root, "match-probe-status")) + "\n")
	for _, tc := range commands {
		script.WriteString(tc.command + " > " + quote(filepath.Join(root, tc.name+"-output")) + " 2>&1\n")
		script.WriteString("printf '%s' $? > " + quote(filepath.Join(root, tc.name+"-status")) + "\n")
	}
	gate := filepath.Join(root, "missing-marker-gate")
	if err := unix.Mkfifo(gate, 0600); err != nil {
		t.Fatal(err)
	}
	script.WriteString("printf ready > " + quote(filepath.Join(root, "missing-marker-ready")) + "\n")
	script.WriteString("read go < " + quote(gate) + " || exit 2\n")
	script.WriteString(probeCommand + " " + quote(api.APISocketPath(filepath.Join(root, s.name))) + " false " + quote(startSock) + " > " + quote(filepath.Join(root, "missing-probe-output")) + " 2>&1\n")
	script.WriteString("printf '%s' $? > " + quote(filepath.Join(root, "missing-probe-status")) + "\n")
	for _, tc := range commands {
		script.WriteString(tc.command + " > " + quote(filepath.Join(root, "missing-"+tc.name+"-output")) + " 2>&1\n")
		script.WriteString("printf '%s' $? > " + quote(filepath.Join(root, "missing-"+tc.name+"-status")) + "\n")
	}
	script.WriteString("printf done > " + quote(filepath.Join(root, "match-done")) + "\n")
	path := filepath.Join(root, "match-entries.sh")
	if err := os.WriteFile(path, []byte(script.String()), 0700); err != nil {
		t.Fatal(err)
	}
	s.send("/bin/sh " + quote(path) + "\r")
	s.waitFor("matching worker started", 5*time.Second, func(string) bool { _, err := os.Stat(filepath.Join(root, "match-started")); return err == nil })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "matching-entries", Agent: "claude", State: "idle"}, nil)
	bootSent := make(chan error, 1)
	go func() {
		f, err := os.OpenFile(boot, os.O_WRONLY, 0)
		if err == nil {
			_, err = f.WriteString("go\n")
			_ = f.Close()
		}
		bootSent <- err
	}()
	select {
	case err := <-bootSent:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	s.waitFor("matching entries ready", 15*time.Second, func(string) bool { _, err := os.Stat(filepath.Join(root, "missing-marker-ready")); return err == nil })
	markerDir := filepath.Join(root, "guard", "agent-panes")
	if err := os.Remove(filepath.Join(markerDir, "p_1.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(markerDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(markerDir, 0700) })
	gateResult := make(chan error, 1)
	go func() {
		f, err := os.OpenFile(gate, os.O_WRONLY, 0)
		if err == nil {
			_, err = f.WriteString("go\n")
			_ = f.Close()
		}
		gateResult <- err
	}()
	select {
	case err := <-gateResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pane process did not receive gate")
	}
	s.waitFor("matching and missing-marker entries", 15*time.Second, func(string) bool { _, err := os.Stat(filepath.Join(root, "match-done")); return err == nil })
	status, err := os.ReadFile(filepath.Join(root, "match-probe-status"))
	if err != nil || string(status) != "0" {
		output, _ := os.ReadFile(filepath.Join(root, "match-probe-output"))
		t.Fatalf("pane process did not match: status=%s output=%s err=%v", status, output, err)
	}
	missingStatus, err := os.ReadFile(filepath.Join(root, "missing-probe-status"))
	if err != nil || string(missingStatus) != "0" {
		output, _ := os.ReadFile(filepath.Join(root, "missing-probe-output"))
		t.Fatalf("missing marker was not classified outside: status=%s output=%s err=%v", missingStatus, output, err)
	}
	for _, prefix := range []string{"", "missing-"} {
		for _, tc := range commands {
			status, err := os.ReadFile(filepath.Join(root, prefix+tc.name+"-status"))
			if err != nil {
				t.Fatal(err)
			}
			output, err := os.ReadFile(filepath.Join(root, prefix+tc.name+"-output"))
			if err != nil {
				t.Fatal(err)
			}
			if string(status) != "0" || !strings.Contains(string(output), tc.want) {
				t.Errorf("%s%s: status=%s output=%s", prefix, tc.name, status, output)
			}
		}
	}
}

// The request is written by a real process that exits while the isolated server
// is suspended. All seven entry points must reject the server's unknown result.
func TestGuardAllEntryPointsRejectExitedSocketWriter(t *testing.T) {
	t.Parallel()
	runGuardWireFailure(t, true)
}

func runGuardWireFailure(t *testing.T, exitedWriter bool) {
	pidfile := filepath.Join(t.TempDir(), "server-pid")
	wrapper := filepath.Join(t.TempDir(), "server-wrapper")
	if err := os.WriteFile(wrapper, []byte(fmt.Sprintf("#!/bin/sh\necho $$ > %q\nexec %q \"$@\"\n", pidfile, binPath)), 0700); err != nil {
		t.Fatal(err)
	}
	s := startVersionMode(t, nil, false, binPath, wrapper)
	data, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	first := "p1"
	s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &first, Direction: "right", Focus: false}, nil)
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: first, Source: "corrupt-response", Agent: "claude", State: "idle"}, nil)
	s.rpc(api.MethodPaneClose, api.PaneTarget{PaneID: first}, nil)
	root := s.stateDir
	marker := filepath.Join(root, "guard", "agent-panes", "p_1.json")
	s.waitFor("closed marker", 3*time.Second, func(string) bool { _, err := os.Stat(marker); return os.IsNotExist(err) })
	socket := api.APISocketPath(filepath.Join(root, s.name))
	realSocket := socket + ".real"
	if err := os.Rename(socket, realSocket); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		_ = os.Rename(realSocket, socket)
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	var corrupted atomic.Int32
	var suspension sync.Mutex
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer client.Close()
				_ = client.SetDeadline(time.Now().Add(5 * time.Second))
				req, err := api.ReadRequest(bufio.NewReader(client))
				if err != nil {
					return
				}
				server, err := net.DialTimeout("unix", realSocket, time.Second)
				if err != nil {
					return
				}
				defer server.Close()
				_ = server.SetDeadline(time.Now().Add(5 * time.Second))
				if exitedWriter && req.Method == api.MethodGuardIdentity {
					suspension.Lock()
					signalLossProcess(t, pid, syscall.SIGSTOP, binPath)
					fd, err := server.(*net.UnixConn).File()
					if err != nil {
						signalLossProcess(t, pid, syscall.SIGCONT, binPath)
						suspension.Unlock()
						return
					}
					err = writeExitedGuardRequest(fd, *req)
					_ = fd.Close()
					signalLossProcess(t, pid, syscall.SIGCONT, binPath)
					suspension.Unlock()
					if err != nil {
						return
					}
				} else {
					if req.Method == api.MethodGuardIdentity {
						var p api.GuardIdentityParams
						if json.Unmarshal(req.Params, &p) != nil {
							return
						}
						_, p.StartTime, err = guard.ApprovalProcess(os.Getpid())
						if err != nil {
							return
						}
						p.PID = os.Getpid()
						req.Params = api.MarshalParams(p)
					}
					if api.WriteRequest(server, *req) != nil {
						return
					}
				}
				line, err := bufio.NewReader(server).ReadBytes('\n')
				if err != nil {
					return
				}
				if req.Method == api.MethodGuardIdentity {
					var response api.Response
					if json.Unmarshal(line, &response) != nil {
						return
					}
					if exitedWriter {
						if response.Err == nil || response.Err.Code != "guard_identity_unknown" {
							return
						}
					} else {
						if response.Err != nil || len(response.Result) == 0 {
							return
						}
						line = append(line[:len(line)/2], '\n')
					}
					corrupted.Add(1)
				}
				_, _ = client.Write(line)
			}()
		}
	}()
	defer func() {
		_ = listener.Close()
		<-acceptDone
		workers.Wait()
		_ = os.Remove(socket)
		if err := os.Rename(realSocket, socket); err != nil {
			t.Errorf("restore isolated socket: %v", err)
		}
	}()
	dir, bin := filepath.Join(root, "guard", "shell", "p_1"), filepath.Join(root, "guard", "bin", "p_1")
	entry := filepath.Join(dir, "ikesu")
	cases := []struct {
		name, command string
		args          []string
		extra         string
	}{
		{"refresh", entry, []string{"guard", "refresh"}, ""},
		{"exec", entry, []string{"guard", "exec", "--", "/usr/bin/printf", "NEVER_RAN"}, ""},
		{"line", entry, []string{"guard", "line", "--", "true"}, ""},
		{"claude-prefix", filepath.Join(dir, "claude-prefix"), []string{"/usr/bin/printf NEVER_RAN"}, ""},
		{"shim", filepath.Join(bin, "git"), []string{"--version"}, ""},
		{"bash-env", "/bin/bash", []string{"-c", "printf NEVER_RAN"}, "BASH_ENV=" + filepath.Join(dir, "bash-env")},
		{"zdotdir", "/bin/zsh", []string{"-c", "printf NEVER_RAN"}, "ZDOTDIR=" + dir},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, tc.command, tc.args...)
			cmd.Env = []string{"HOME=" + root, "PATH=/usr/bin:/bin", "SHELL=/bin/bash", "IKESU_STATE_DIR=" + root,
				"IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + marker, "IKESU_GUARD_BIN=" + bin, "IKESU_GUARD_SHELL_DIR=" + dir,
				"IKESU_SOCKET_PATH=" + socket, "IKESU_SOCKET_PATH_EXPLICIT=1"}
			if tc.extra != "" {
				cmd.Env = append(cmd.Env, tc.extra)
			}
			out, err := cmd.CombinedOutput()
			if err == nil || strings.Contains(string(out), "NEVER_RAN") || strings.Contains(string(out), "git version") {
				t.Fatalf("corrupt response permitted execution: %v %s", err, out)
			}
		})
	}
	if got := corrupted.Load(); got != int32(len(cases)) {
		t.Errorf("verified %d actual failure responses, want %d", got, len(cases))
	}
}

// Seven processes from the same real pane wait at the socket response while
// that pane closes. The synchronization can exceed five seconds on mini.
func TestGuardAllEntriesStopWhenMatchedPaneCloses(t *testing.T) {
	t.Parallel()
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	first := "p1"
	s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &first, Direction: "right", Focus: false}, nil)
	root := s.stateDir
	startGate, releaseGate := filepath.Join(root, "close-start"), filepath.Join(root, "close-release")
	for _, path := range []string{startGate, releaseGate} {
		if err := unix.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ready, matched, result := filepath.Join(root, "close-ready"), filepath.Join(root, "close-matched"), filepath.Join(root, "close-result")
	script := filepath.Join(root, "close-entries.py")
	body := `import json, os, signal, socket, subprocess, sys, threading
signal.signal(signal.SIGHUP, signal.SIG_IGN)
root, sock, start, release, ready, matched, result, start_sock = sys.argv[1:]
st=socket.socket(socket.AF_UNIX); st.connect(start_sock); self_start=st.makefile().readline().strip(); st.close()
real = sock + '.real'
os.rename(sock, real)
listener = socket.socket(socket.AF_UNIX)
listener.bind(sock)
listener.listen(32)
release_event = threading.Event()
lock = threading.Lock()
responses = []
handlers = []
def receive(conn):
    data = b''
    while not data.endswith(b'\n'):
        part = conn.recv(65536)
        if not part: raise RuntimeError('socket closed')
        data += part
    return data
def handle(client):
    try:
        request = receive(client)
        server = socket.socket(socket.AF_UNIX)
        server.settimeout(20)
        server.connect(real)
        decoded_request = json.loads(request)
        if decoded_request.get('method') == 'guard.identity':
            decoded_request['params'].update({'pid':os.getpid(),'start_time':self_start})
            request = (json.dumps(decoded_request)+'\n').encode()
        server.sendall(request)
        response = receive(server)
        server.close()
        if json.loads(request).get('method') == 'guard.identity':
            decoded = json.loads(response)
            with lock:
                responses.append(decoded.get('result', {}).get('match') is True)
                if len(responses) == 7:
                    with open(matched+'.tmp', 'w') as f: json.dump(responses, f)
                    os.replace(matched+'.tmp', matched)
            release_event.wait(20)
        client.sendall(response)
    finally:
        client.close()
def accept():
    while True:
        try: client, _ = listener.accept()
        except OSError: return
        thread = threading.Thread(target=handle, args=(client,), daemon=True)
        handlers.append(thread)
        thread.start()
threading.Thread(target=accept, daemon=True).start()
with open(ready, 'w') as f: f.write(str(os.getpid()))
def gate(path):
    with open(path) as f: return f.readline().strip()
try:
    if gate(start) != 'go': sys.exit(1)
    directory = os.path.join(root, 'guard', 'shell', 'p_1')
    bindir = os.path.join(root, 'guard', 'bin', 'p_1')
    entry = os.path.join(directory, 'ikesu')
    cases = [
      ('refresh', [entry,'guard','refresh'], None),
      ('exec', [entry,'guard','exec','--','/usr/bin/printf','NEVER_RAN'], None),
      ('line', [entry,'guard','line','--','true'], None),
      ('claude-prefix', [os.path.join(directory,'claude-prefix'),'/usr/bin/printf NEVER_RAN'], None),
      ('shim', [os.path.join(bindir,'git'),'--version'], None),
      ('bash-env', ['/bin/bash','-c','printf NEVER_RAN'], ('BASH_ENV',os.path.join(directory,'bash-env'))),
      ('zdotdir', ['/bin/zsh','-c','printf NEVER_RAN'], ('ZDOTDIR',directory)),
    ]
    processes = []
    for name, args, extra in cases:
        env = dict(os.environ)
        env.update({'HOME':root,'PATH':'/usr/bin:/bin','SHELL':'/bin/zsh','IKESU_STATE_DIR':root,
          'IKESU_PANE_ID':'p1','IKESU_GUARD_MARKER':os.path.join(root,'guard','agent-panes','p_1.json'),
          'IKESU_GUARD_BIN':bindir,'IKESU_GUARD_SHELL_DIR':directory,'IKESU_SOCKET_PATH':sock,'IKESU_SOCKET_PATH_EXPLICIT':'1'})
        if extra: env[extra[0]] = extra[1]
        processes.append((name, subprocess.Popen(args, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)))
    if gate(release) != 'go': sys.exit(1)
    release_event.set()
    results = []
    for name, process in processes:
        output, _ = process.communicate(timeout=10)
        results.append({'name':name,'status':process.returncode,'output':output.decode(errors='replace')})
finally:
    release_event.set()
    listener.close()
    try: os.unlink(sock)
    except FileNotFoundError: pass
    os.rename(real, sock)
with open(result+'.tmp','w') as f: json.dump(results, f)
os.replace(result+'.tmp', result)
`
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }
	socket := api.APISocketPath(filepath.Join(root, s.name))
	line := "nohup"
	for _, arg := range []string{python, script, root, socket, startGate, releaseGate, ready, matched, result, serveProcessStarts(t, root)} {
		line += " " + quote(arg)
	}
	line += " > /dev/null 2>&1 < /dev/null &\r"
	s.send(line)
	s.waitFor("pane relay ready", 5*time.Second, func(string) bool { _, err := os.Stat(ready); return err == nil })
	send := func(path, message string) error {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			fd, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK, 0)
			if err == nil {
				_, err = unix.Write(fd, []byte(message+"\n"))
				_ = unix.Close(fd)
				return err
			}
			if err != unix.ENXIO {
				return err
			}
			time.Sleep(time.Millisecond)
		}
		return fmt.Errorf("fifo reader unavailable")
	}
	t.Cleanup(func() {
		for _, path := range []string{startGate, releaseGate} {
			if fd, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK, 0); err == nil {
				_, _ = unix.Write(fd, []byte("abort\n"))
				_ = unix.Close(fd)
			}
		}
	})
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: first, Source: "close-all-entries", Agent: "claude", State: "idle"}, nil)
	if err := send(startGate, "go"); err != nil {
		t.Fatal(err)
	}
	s.waitFor("all matched identity responses", 15*time.Second, func(string) bool { _, err := os.Stat(matched); return err == nil })
	data, err := os.ReadFile(matched)
	if err != nil {
		t.Fatal(err)
	}
	var matches []bool
	if err := json.Unmarshal(data, &matches); err != nil || len(matches) != 7 {
		t.Fatalf("matching responses: %s %v", data, err)
	}
	for _, match := range matches {
		if !match {
			t.Fatalf("not all processes matched their live pane: %s", data)
		}
	}
	s.rpc(api.MethodPaneClose, api.PaneTarget{PaneID: first}, nil)
	if err := send(releaseGate, "go"); err != nil {
		t.Fatal(err)
	}
	s.waitFor("closed pane entry results", 15*time.Second, func(string) bool { _, err := os.Stat(result); return err == nil })
	data, err = os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	var results []struct {
		Name   string `json:"name"`
		Status int    `json:"status"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal(data, &results); err != nil || len(results) != 7 {
		t.Fatalf("entry results: %s %v", data, err)
	}
	for _, r := range results {
		if r.Status == 0 || strings.Contains(r.Output, "NEVER_RAN") || strings.Contains(r.Output, "git version") {
			t.Errorf("%s ran after pane closed: %+v", r.Name, r)
		}
	}
}
