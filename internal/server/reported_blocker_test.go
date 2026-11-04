package server

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/notify"
	"github.com/kazuph/ikesu/internal/teststate"
	"golang.org/x/sys/unix"
)

// reportedQuestionTerminal writes fixture updates through the real PTY so the
// supervisor delivers the same screen-change events as a running agent.
func reportedQuestionTerminal(t *testing.T) (*Server, *os.File) {
	t.Helper()
	s := newTestServer(t, "reported-question")
	teststate.Setenv(t, "SHELL", "/bin/sh")
	dir := t.TempDir()
	fifo := filepath.Join(dir, "output")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(fifo, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.sup.SpawnWith(1, dir, 160, 60, nil, []string{"/bin/sh", "-c", `stty raw -echo; exec cat "$1"`, "screen", fifo})
	if err != nil {
		output.Close()
		t.Fatal(err)
	}
	p.SetAgent("claude")
	s.initDecisionHub()
	s.app.SetQuitHook(s.stopDecisions)
	done := make(chan struct{})
	go func() { s.app.Run(); close(done) }()
	t.Cleanup(func() {
		s.app.Stop()
		<-done
		output.Close()
		s.sup.KillAll()
		if s.store != nil {
			s.store.Close()
		}
	})
	return s, output
}

func TestReportedQuestionAPIAndSingleNotificationRequest(t *testing.T) {
	if runIsolatedParallel(t) {
		return
	}
	teststate.Lock(t)
	s, output := reportedQuestionTerminal(t)
	p := s.sup.Get(1)
	paint := func(text string) {
		t.Helper()
		if _, err := output.Write([]byte("\x1b[2J\x1b[H" + text)); err != nil {
			t.Fatal(err)
		}
	}
	paint("Ready")
	waitPromptCondition(t, func() bool { return strings.Contains(p.ScreenText(), "Ready") })
	dir, err := os.MkdirTemp("/tmp", "ikesu-question-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	listener, err := net.Listen("unix", filepath.Join(dir, "host.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	remote, err := net.Dial("unix", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	local, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	var mu sync.Mutex
	var messages []clientMsg
	go func() {
		dec := json.NewDecoder(remote)
		for {
			var m clientMsg
			if dec.Decode(&m) != nil {
				return
			}
			mu.Lock()
			messages = append(messages, m)
			mu.Unlock()
		}
	}()
	s.app.Do(func() {
		s.cfg, _ = config.Decode([]byte("[ui.toast]\ndelivery='system'\n"))
		s.client = &clientConn{conn: local, available: endpoint.Generation}
		s.lastStatus = map[int]string{}
		s.notifyLast = map[int]map[string]time.Time{}
		// The target is genuinely in a background workspace, as required by G4.
		s.app.State().Apply(model.CmdNewWorkspace{Cwd: dir, Name: "foreground"})
		if resp := s.paneReportAgent("report", api.PaneReportAgentParams{PaneID: "p1", Source: "claude", Agent: "claude", State: "working"}); resp.Err != nil {
			t.Errorf("report: %v", resp.Err)
		}
		s.lastStatus[1] = "working"
		s.app.SetAvailHook(s.onPaneStatus)
	})
	t.Cleanup(func() { s.app.Do(func() { s.app.SetAvailHook(nil); s.client = nil }); local.Close() })
	paint("Question?\r\n1. First\r\n2. Second\r\nEnter to select · Tab/Arrow keys to navigate · Esc to cancel")
	t.Logf("question painted at %s revision=%d", time.Now().Format(time.RFC3339Nano), p.Snapshot().Revision)
	waitPromptCondition(t, func() bool {
		var blocked bool
		s.app.Do(func() { info, _ := s.paneInfo(1); blocked = info.AgentStatus == api.AgentStatus("blocked") })
		return blocked
	})
	t.Logf("blocked observed at %s revision=%d", time.Now().Format(time.RFC3339Nano), p.Snapshot().Revision)
	waitPromptCondition(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(messages) > 0 })
	s.app.Do(func() {
		var result api.PaneInfoResult
		if err := json.Unmarshal(s.paneGet("get", "p1").Result, &result); err != nil || result.Pane.AgentStatus != "blocked" {
			t.Errorf("pane.get: %+v %v", result, err)
		}
		if got := s.agentInfoAt(1, 0, 0).AgentStatus; got != "blocked" {
			t.Errorf("agent API: %s", got)
		}
		for i := 0; i < 3; i++ {
			s.onPaneStatus(1)
		}
	})
	paint("Ready")
	t.Logf("ready painted at %s revision=%d", time.Now().Format(time.RFC3339Nano), p.Snapshot().Revision)
	var last string
	waitPromptCondition(t, func() bool {
		var restored bool
		s.app.Do(func() {
			status := s.app.PaneStatusKey(1)
			restored = status == "working"
			if status != last {
				t.Logf("status=%s at %s revision=%d", status, time.Now().Format(time.RFC3339Nano), p.Snapshot().Revision)
				last = status
			}
		})
		return restored
	})
	mu.Lock()
	defer mu.Unlock()
	if len(messages) != 1 || messages[0].Type != "notify" || messages[0].Notification == nil {
		t.Fatalf("notifications: %+v", messages)
	}
	n := messages[0].Notification
	if n.Kind != "needs_attention" || n.Agent != "claude" || n.TargetPaneID != "p1" || !n.SystemToast {
		t.Fatal(n)
	}
	if _, enabled := notify.SoundPath(s.cfg, n.Agent, n.Kind); !enabled {
		t.Fatal("attention notification did not request enabled sound")
	}
	t.Logf("one host notification + sound request: %+v; API blocked -> working", n)
}
