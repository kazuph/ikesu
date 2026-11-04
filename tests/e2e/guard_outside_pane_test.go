package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/guard"
	"golang.org/x/sys/unix"
)

// A real isolated server is briefly suspended so its socket receives a request
// after the writing process has exited; process setup can exceed five seconds.
func TestGuardIdentityExitedWriterFailsClosed(t *testing.T) {
	if os.Getenv("IKESU_EXITED_PEER_HELPER") == "1" {
		file := os.NewFile(3, "inherited-server-connection")
		defer file.Close()
		if err := api.WriteRequest(file, api.Request{ID: "exited-peer", Method: api.MethodGuardIdentity, Params: api.MarshalParams(api.GuardIdentityParams{PaneID: "p1"})}); err != nil {
			t.Fatal(err)
		}
		return
	}
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
	cmd := exec.Command(os.Args[0], "-test.run=^TestGuardIdentityExitedWriterFailsClosed$")
	cmd.ExtraFiles = []*os.File{childFD}
	cmd.Env = append(os.Environ(), "CLIENT_UPGRADE_RAW_HELPER=1", "IKESU_EXITED_PEER_HELPER=1")
	if err := cmd.Start(); err != nil {
		childFD.Close()
		t.Fatal(err)
	}
	childFD.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
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
		file, err := conn.(*net.UnixConn).File()
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		defer conn.Close()
		if err := unix.Sendmsg(3, []byte{1}, unix.UnixRights(int(file.Fd())), nil, 0); err != nil {
			t.Fatal(err)
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
	if err := cmd.Wait(); err != nil {
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
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := api.WriteRequest(conn, api.Request{ID: "dead-peer", Method: api.MethodGuardIdentity, Params: api.MarshalParams(api.GuardIdentityParams{PaneID: "p1"})}); err != nil {
		t.Fatal(err)
	}
	resp, err := api.ReadResponse(bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	// macOS LOCAL_PEERPID identifies the last writer after SCM_RIGHTS transfer;
	// Linux SO_PEERCRED retains the dead connector's identity.
	if runtime.GOOS == "darwin" {
		if resp.Err != nil {
			t.Fatal(resp.Err)
		}
		var result api.GuardIdentityResult
		if err := json.Unmarshal(resp.Result, &result); err != nil || result.Match {
			t.Fatalf("transferred socket misattributed to pane: %+v %v", result, err)
		}
	} else if resp.Err == nil {
		t.Fatal("dead connector was classified instead of failing closed")
	}
}

// Each sample starts a real Bash and git process; the interleaved measurement
// can exceed five seconds on the shared mini.
func TestGuardOutsidePaneDebugLatency(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	first := "p1"
	s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &first, Direction: "right", Focus: false}, nil)
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: first, Source: "outside-latency", Agent: "claude", State: "idle"}, nil)
	s.rpc(api.MethodPaneClose, api.PaneTarget{PaneID: first}, nil)
	root := s.stateDir
	marker := filepath.Join(root, "guard", "agent-panes", "p_1.json")
	s.waitFor("closed marker", 3*time.Second, func(string) bool { _, err := os.Stat(marker); return os.IsNotExist(err) })
	plainEnv := []string{"HOME=" + root, "PATH=/usr/bin:/bin", "SHELL=/bin/bash"}
	guardEnv := append(append([]string{}, plainEnv...), "IKESU_STATE_DIR="+root, "IKESU_PANE_ID=p1",
		"IKESU_GUARD_MARKER="+marker, "IKESU_GUARD_BIN="+filepath.Join(root, "guard", "bin", "p_1"),
		"BASH_ENV="+filepath.Join(root, "guard", "shell", "p_1", "bash-env"),
		"IKESU_SOCKET_PATH="+api.APISocketPath(filepath.Join(root, s.name)), "IKESU_SOCKET_PATH_EXPLICIT=1")
	var plain, guarded []time.Duration
	for sample := 0; sample < 9; sample++ {
		for _, condition := range []struct {
			env    []string
			values *[]time.Duration
		}{{plainEnv, &plain}, {guardEnv, &guarded}} {
			cmd := exec.Command("/bin/bash", "-c", "git --version")
			cmd.Env = condition.env
			start := time.Now()
			out, err := cmd.CombinedOutput()
			elapsed := time.Since(start)
			if err != nil || !strings.Contains(string(out), "git version") {
				t.Fatalf("latency command: %v %s", err, out)
			}
			*condition.values = append(*condition.values, elapsed)
		}
	}
	sort.Slice(plain, func(i, j int) bool { return plain[i] < plain[j] })
	sort.Slice(guarded, func(i, j int) bool { return guarded[i] < guarded[j] })
	t.Logf("interleaved samples=%d bash_git_plain_median=%s bash_git_guarded_median=%s difference=%s", len(plain), plain[len(plain)/2], guarded[len(guarded)/2], guarded[len(guarded)/2]-plain[len(plain)/2])
}

func TestGuardDecisionRejectsWrongPaneAtCreation(t *testing.T) {
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

func TestGuardOutsidePaneMissingMarkerSafeLine(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	first := "p1"
	s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &first, Direction: "right", Focus: false}, nil)
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "outside-test", Agent: "claude", State: "idle"}, nil)
	marker := filepath.Join(s.stateDir, "guard", "agent-panes", "p_1.json")
	s.rpc(api.MethodPaneClose, api.PaneTarget{PaneID: "p1"}, nil)
	s.waitFor("closed marker removed", 3*time.Second, func(string) bool { _, err := os.Stat(marker); return os.IsNotExist(err) })
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	entry := filepath.Join(s.stateDir, "guard", "shell", "p_1", "ikesu")
	cmd := exec.CommandContext(ctx, entry, "guard", "line", "--", "true")
	cmd.Env = []string{"HOME=" + s.stateDir, "PATH=/usr/bin:/bin", "IKESU_STATE_DIR=" + s.stateDir,
		"IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + marker,
		"IKESU_SOCKET_PATH=" + api.APISocketPath(filepath.Join(s.stateDir, s.name)), "IKESU_SOCKET_PATH_EXPLICIT=1"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("safe line with stale marker must be evaluated as outside: %v: %s", err, out)
	}
}

func TestGuardOutsidePaneUnreachableServerStopsSafeCommand(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "outside-test", Agent: "claude", State: "idle"}, nil)
	marker := filepath.Join(s.stateDir, "guard", "agent-panes", "p_1.json")
	entry := filepath.Join(s.stateDir, "guard", "shell", "p_1", "ikesu")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, entry, "guard", "line", "--", "true")
	cmd.Env = []string{"HOME=" + s.stateDir, "PATH=/usr/bin:/bin", "IKESU_STATE_DIR=" + s.stateDir,
		"IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + marker,
		"IKESU_SOCKET_PATH=" + filepath.Join(s.stateDir, "missing.sock"), "IKESU_SOCKET_PATH_EXPLICIT=1"}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("unreachable identity server allowed a safe line: %s", out)
	}
}

func TestGuardOutsidePaneDoesNotUseInheritedOrigin(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "outside-test", Agent: "claude", State: "idle"}, nil)
	marker := filepath.Join(s.stateDir, "guard", "agent-panes", "p_1.json")
	entry := filepath.Join(s.stateDir, "guard", "shell", "p_1", "ikesu")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, entry, "guard", "exec", "--", "dd", "if=/dev/null", "of=/dev/null")
	cmd.Env = []string{"HOME=" + s.stateDir, "PATH=/usr/bin:/bin", "IKESU_STATE_DIR=" + s.stateDir,
		"IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + marker,
		"IKESU_SOCKET_PATH=" + api.APISocketPath(filepath.Join(s.stateDir, s.name)), "IKESU_SOCKET_PATH_EXPLICIT=1",
		"AI_GUARD_REQUEST_REASON=隔離した試験で判断待ちの出所を確認します。"}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer cancel()
	var pending api.DecisionListResult
	s.waitFor("outside guard decision", 3*time.Second, func(string) bool {
		s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
		return len(pending.Decisions) == 1
	})
	d := pending.Decisions[0]
	if d.Origin == nil || !d.Origin.OutsidePane || d.Origin.PaneID != "" || d.Origin.PaneLabel != "" || d.Origin.WorkspaceLabel != "" || d.Origin.TabLabel != "" || d.Origin.Agent != "" {
		t.Fatalf("outside process inherited pane or agent origin: %+v", d.Origin)
	}
	var pane api.PaneInfoResult
	s.rpc(api.MethodPaneGet, api.PaneTarget{PaneID: "p1"}, &pane)
	if pane.Pane.AgentStatus == api.AgentBlocked {
		t.Fatal("outside decision blocked the unrelated pane")
	}
	if _, err := s.cli("decision", "answer", d.DecisionID, "reject"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("rejected command succeeded")
	}
	data, err := os.ReadFile(filepath.Join(s.stateDir, "guard", "decisions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var record guard.Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record.OutsidePane {
			found = true
			if record.Pane != "" || record.Workspace != "" || record.Tab != "" || record.Agent != "" {
				t.Fatalf("audit retained inherited origin: %+v", record)
			}
		}
	}
	if !found {
		t.Fatal("outside guard decision was not identified in audit")
	}
}

func TestGuardOutsidePaneShellRetainsProtectionAfterCleanup(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	first := "p1"
	s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &first, Direction: "right", Focus: false}, nil)
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "outside-test", Agent: "claude", State: "idle"}, nil)
	marker := filepath.Join(s.stateDir, "guard", "agent-panes", "p_1.json")
	s.rpc(api.MethodPaneClose, api.PaneTarget{PaneID: "p1"}, nil)
	s.waitFor("closed marker removed", 3*time.Second, func(string) bool { _, err := os.Stat(marker); return os.IsNotExist(err) })
	bin := filepath.Join(s.stateDir, "guard", "bin", "p_1")
	target := filepath.Join(t.TempDir(), "must-wait")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/bash", "-c", `dd if=/dev/null of="$1"`, "outside-test", target)
	cmd.Env = []string{"HOME=" + s.stateDir, "PATH=/usr/bin:/bin", "SHELL=/bin/bash", "IKESU_STATE_DIR=" + s.stateDir,
		"BASH_ENV=" + filepath.Join(s.stateDir, "guard", "shell", "p_1", "bash-env"),
		"IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + marker, "IKESU_GUARD_BIN=" + bin,
		"IKESU_SOCKET_PATH=" + api.APISocketPath(filepath.Join(s.stateDir, s.name)), "IKESU_SOCKET_PATH_EXPLICIT=1",
		"AI_GUARD_REQUEST_REASON=隔離した試験で承認前に実行しないことを確認します。"}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer cancel()
	var pending api.DecisionListResult
	s.waitFor("outside shell decision", 3*time.Second, func(string) bool {
		if _, err := os.Stat(target); err == nil {
			t.Fatal("command executed before approval")
		}
		s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
		return len(pending.Decisions) == 1
	})
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("command ran before approval: %v", err)
	}
	if _, err := s.cli("decision", "answer", pending.Decisions[0].DecisionID, "reject"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("rejected command succeeded")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("rejected command wrote target: %v", err)
	}
}

func TestGuardUninjectedOutsideDoesNotRequireServer(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command(binPath, "guard", "exec", "--", "/usr/bin/true")
	cmd.Env = []string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_STATE_DIR=" + root, "IKESU_CONFIG=" + filepath.Join(root, "none")}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ordinary outside process was guarded: %v %s", err, out)
	}
}

func TestGuardOutsideBlockedLineHasNoSideEffect(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	first := "p1"
	s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &first, Direction: "right", Focus: false}, nil)
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: first, Source: "outside-test", Agent: "claude", State: "idle"}, nil)
	s.rpc(api.MethodPaneClose, api.PaneTarget{PaneID: first}, nil)
	marker := filepath.Join(s.stateDir, "guard", "agent-panes", "p_1.json")
	s.waitFor("closed marker", 3*time.Second, func(string) bool { _, err := os.Stat(marker); return os.IsNotExist(err) })
	target := filepath.Join(t.TempDir(), "blocked-output")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/bash", "-c", `printf ran > "$1"; killall no-such-ikesu-test-process`, "outside-test", target)
	cmd.Env = []string{"HOME=" + s.stateDir, "PATH=/usr/bin:/bin", "SHELL=/bin/bash", "IKESU_STATE_DIR=" + s.stateDir,
		"BASH_ENV=" + filepath.Join(s.stateDir, "guard", "shell", "p_1", "bash-env"), "IKESU_PANE_ID=p1",
		"IKESU_GUARD_MARKER=" + marker, "IKESU_GUARD_BIN=" + filepath.Join(s.stateDir, "guard", "bin", "p_1"),
		"IKESU_SOCKET_PATH=" + api.APISocketPath(filepath.Join(s.stateDir, s.name)), "IKESU_SOCKET_PATH_EXPLICIT=1"}
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "Command cancelled") {
		t.Fatalf("blocked line not rejected: %v %s", err, out)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("part of blocked line executed: %v", err)
	}
}

// One isolated server is reused by fourteen real process invocations, so this
// matrix can exceed five seconds on the shared mini.
func TestGuardOutsidePaneAllEntryPoints(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	first := "p1"
	s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &first, Direction: "right", Focus: false}, nil)
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: first, Source: "outside-entries", Agent: "claude", State: "idle"}, nil)
	s.rpc(api.MethodPaneClose, api.PaneTarget{PaneID: first}, nil)
	root := s.stateDir
	marker := filepath.Join(root, "guard", "agent-panes", "p_1.json")
	s.waitFor("closed marker", 3*time.Second, func(string) bool { _, err := os.Stat(marker); return os.IsNotExist(err) })
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
	for _, tc := range tests {
		for _, available := range []bool{true, false} {
			name := tc.name + "/server"
			if !available {
				name = tc.name + "/unavailable"
			}
			t.Run(name, func(t *testing.T) {
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
}

func TestGuardMatchingPaneCorruptMarkerFailsClosed(t *testing.T) {
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
	lock, err := os.OpenFile(filepath.Join(root, "guard", "shell", "p_1", "generation.lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	marker := filepath.Join(root, "guard", "agent-panes", "p_1.json")
	if err := os.WriteFile(marker, []byte("{"), 0600); err != nil {
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
	if strings.TrimSpace(string(code)) == "0" || !strings.Contains(string(output), "目印ファイルが壊れています") || strings.Contains(string(output), "NEVER_RAN") {
		t.Fatalf("corrupt matched marker did not fail closed: status=%s output=%s", code, output)
	}
}

// The detached child and actual pane closure establish the identity transition;
// waiting for both real RPCs can exceed five seconds on a busy mini.
func TestGuardDecisionFailsIfPaneClosesAfterIdentity(t *testing.T) {
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
sock, g1, g2, ready, result1, result2 = sys.argv[1:]
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
identity=rpc('guard.identity',{'pane_id':'p1'})
with open(result1+'.tmp','w') as f: json.dump(identity,f)
os.replace(result1+'.tmp',result1)
if wait(g2) != 'go': sys.exit(1)
ctx={'pane_id':'p1','match':True,'policy_digest':identity['result']['policy_digest']}
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
	args := []string{python, script, socket, gate1, gate2, ready, firstResult, secondResult}
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
