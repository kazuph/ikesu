package server

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/notify"
)

// Agent notifications (SPEC G4). A background pane transitions to
// blocked → needs_attention; working→idle → finished. The same rate
// limit applies to every delivery route: same pane+kind within 10s is
// dropped, and any needs_attention shields all finished notifications
// for 10s so a question toast is not overwritten by a completion.

const notifyCooldown = 10 * time.Second

// onPaneStatus is the app's availability hook: mail flush plus
// notification evaluation. Runs on the app loop.
func (s *Server) onPaneStatus(pid int) {
	cur := s.app.PaneStatusKey(pid)
	if cur == "done" {
		cur = "idle"
	}
	prev := s.lastStatus[pid]
	s.lastStatus[pid] = cur
	s.flushMailForPane(pid)
	if prev == "" || prev == cur {
		return
	}
	kind := ""
	switch {
	case cur == "blocked":
		kind = "needs_attention"
	case prev == "working" && cur == "idle":
		kind = "finished"
	}
	if kind == "" || s.paneOnActiveTab(pid) || !s.notifyAllowed(pid, kind) {
		return
	}
	s.deliverNotification(pid, kind)
}

// paneOnActiveTab suppresses notifications for the visible tab.
func (s *Server) paneOnActiveTab(pid int) bool {
	st := s.app.State()
	ws := st.ActiveWorkspace()
	if ws == nil {
		return false
	}
	t := ws.TabRef()
	if t == nil {
		return false
	}
	for _, id := range t.Layout.PaneIDs() {
		if id == pid {
			return true
		}
	}
	return false
}

// notifyAllowed applies the shared 10s rate limit and records the event.
func (s *Server) notifyAllowed(pid int, kind string) bool {
	now := time.Now()
	if kind == "finished" && now.Sub(s.lastAttention) < notifyCooldown {
		return false
	}
	if s.notifyLast[pid] == nil {
		s.notifyLast[pid] = map[string]time.Time{}
	}
	if lt, ok := s.notifyLast[pid][kind]; ok && now.Sub(lt) < notifyCooldown {
		return false
	}
	s.notifyLast[pid][kind] = now
	if kind == "needs_attention" {
		s.lastAttention = now
	}
	return true
}

// notifyTitle builds "<ws number> <ws name>-<pane OSC title>".
func (s *Server) notifyTitle(pid int) string {
	st := s.app.State()
	wsIdx, tabIdx := st.FindPane(pid)
	if wsIdx < 0 {
		return "gtx"
	}
	ws := st.Workspaces[wsIdx]
	name := ws.Name
	if name == "" {
		name = fmtWorkspaceID(ws.ID)
	}
	title := fmt.Sprintf("%d %s", wsIdx+1, name)
	if tabIdx >= 0 && tabIdx < len(ws.Tabs) {
		panes := ws.Tabs[tabIdx].Layout.PaneIDs()
		if len(panes) > 0 {
			if p := s.sup.Get(panes[0]); p != nil {
				if t := p.Snapshot().OSCTitle; t != "" {
					title += "-" + t
				}
			}
		}
	}
	return title
}

// deliverNotification fans out to the configured route.
func (s *Server) deliverNotification(pid int, kind string) {
	title := s.notifyTitle(pid)
	body := ""
	if p := s.sup.Get(pid); p != nil {
		lines := append(p.ScrollbackLines(), p.ScreenLines()...)
		body = notify.Body(lines, title)
	}
	switch s.cfg.ToastDelivery() {
	case config.ToastGTX:
		s.app.ShowToast(title, body, kind, pid)
	case config.ToastTerminal:
		s.sendOSC9(title, body)
	case config.ToastSystem:
		s.sendSystemNotification(title, body, pid)
	}
}

// sendOSC9 writes an iTerm2/kitty-style notification escape into the
// attached client's output stream.
func (s *Server) sendOSC9(title, body string) {
	text := title
	if body != "" {
		text += " — " + body
	}
	s.writeClientOutput("\x1b]9;" + text + "\x07")
}

// sendSystemNotification uses terminal-notifier when present (with a
// -execute focus command), else osascript. macOS-only route.
func (s *Server) sendSystemNotification(title, body string, pid int) {
	if tn, err := exec.LookPath("terminal-notifier"); err == nil {
		exe, _ := os.Executable()
		focus := fmt.Sprintf("%s pane focus %s >/dev/null 2>&1", exe, fmtPaneID(pid))
		_ = exec.Command(tn, "-title", title, "-message", body,
			"-execute", focus).Start()
		return
	}
	script := fmt.Sprintf(`display notification %q with title %q`, body, title)
	_ = exec.Command("/usr/bin/osascript", "-e", script).Start()
}
