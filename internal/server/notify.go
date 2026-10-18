package server

import (
	"fmt"
	"os"
	"time"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
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
	clients := s.displayClients()
	if len(clients) > 0 {
		for _, c := range clients {
			if c.viewer != nil && c.viewer.PaneOnSelectedTab(s.app, pid) {
				return true
			}
		}
		return false
	}
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
		lines, _ := p.ReadLines("recent_unwrapped", true)
		body = notify.Body(lines, title)
	}
	switch s.cfg.ToastDelivery() {
	case config.ToastGTX:
		s.app.ShowToast(title, body, kind, pid)
		for _, c := range s.displayClients() {
			if c.viewer != nil {
				c.viewer.ShowToast(s.app, title, body, kind, pid)
			}
		}
	case config.ToastTerminal:
		s.sendOSC9(title, body)
	case config.ToastSystem:
		// OS delivery belongs to the attached host.
	}
	if kind == "finished" || kind == "needs_attention" || s.cfg.ToastDelivery() == config.ToastSystem {
		agent := ""
		if p := s.sup.Get(pid); p != nil {
			agent = p.Snapshot().AgentID
		}
		n := endpoint.Notification{Title: title, Body: body, Kind: kind, Agent: agent, TargetPaneID: fmtPaneID(pid), SystemToast: s.cfg.ToastDelivery() == config.ToastSystem}
		s.mu.Lock()
		c := s.client
		s.mu.Unlock()
		if c != nil {
			if c.available < endpoint.NotificationGeneration {
				fmt.Fprintln(os.Stderr, "gtx: endpoint_action_unsupported: desktop notification requires endpoint generation 4")
				return
			}
			if err := c.send(clientMsg{Type: "notify", Notification: &n}); err != nil {
				s.dropConn(c)
			}
		}
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
