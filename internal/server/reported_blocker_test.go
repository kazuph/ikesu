package server

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/notify"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestReportedQuestionAPIAndSingleNotificationRequest(t *testing.T) {
	teststate.Lock(t)
	s, p, _ := screenTerminal(t, "claude-single", "claude")
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
		p.WithEmu(func(e *vt.Emulator) { e.Write([]byte("\x1b[2J\x1b[HReady")) })
		if resp := s.paneReportAgent("report", api.PaneReportAgentParams{PaneID: "p1", Source: "claude", Agent: "claude", State: "working"}); resp.Err != nil {
			t.Errorf("report: %v", resp.Err)
		}
		s.lastStatus[1] = "working"
		s.app.SetAvailHook(s.onPaneStatus)
	})
	t.Cleanup(func() { s.app.Do(func() { s.app.SetAvailHook(nil); s.client = nil }); local.Close() })
	p.WithEmu(func(e *vt.Emulator) {
		e.Write([]byte("\x1b[2J\x1b[HQuestion?\r\n1. First\r\n2. Second\r\nEnter to select · Tab/Arrow keys to navigate · Esc to cancel"))
	})
	waitPromptCondition(t, func() bool {
		var blocked bool
		s.app.Do(func() { info, _ := s.paneInfo(1); blocked = info.AgentStatus == api.AgentStatus("blocked") })
		return blocked
	})
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
	p.WithEmu(func(e *vt.Emulator) { e.Write([]byte("\x1b[2J\x1b[HReady")) })
	waitPromptCondition(t, func() bool {
		var restored bool
		s.app.Do(func() { restored = s.app.PaneStatusKey(1) == "working" })
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
