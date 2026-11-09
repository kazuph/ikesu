package server

import "github.com/kazuph/ikesu/internal/render"

type clipboardTransfer struct {
	Pane int
	Data string
}

func (s *Server) deliverPaneClipboard(pid int, data string, transferred bool) {
	for _, pending := range s.pendingClipboard {
		if pending.Pane == pid {
			s.pendingClipboard = append(s.pendingClipboard, clipboardTransfer{pid, data})
			s.flushPaneClipboard()
			return
		}
	}
	if !s.sendPaneClipboard(pid, data) && transferred {
		s.pendingClipboard = append(s.pendingClipboard, clipboardTransfer{pid, data})
	}
}

func (s *Server) sendPaneClipboard(pid int, data string) bool {
	var target *clientConn
	for _, c := range s.displayClients() {
		if c.viewer != nil && c.viewer.DisplaysPane(s.app, pid) && (target == nil || c.lastUse > target.lastUse) {
			target = c
		}
	}
	if target == nil {
		return false
	}
	if err := target.send(clientMsg{Type: "output", Data: encode64([]byte(data))}); err != nil {
		s.dropConn(target)
		return false
	}
	return true
}

func (s *Server) flushPaneClipboard() {
	pending := s.pendingClipboard
	s.pendingClipboard = nil
	blocked := make(map[int]bool)
	for _, entry := range pending {
		if blocked[entry.Pane] || !s.sendPaneClipboard(entry.Pane, entry.Data) {
			blocked[entry.Pane] = true
			s.pendingClipboard = append(s.pendingClipboard, entry)
		}
	}
}

func (s *Server) displayClients() []*clientConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*clientConn, 0, len(s.clients))
	for c := range s.clients {
		out = append(out, c)
	}
	return out
}

func (s *Server) visiblePane(pid int) bool {
	for _, c := range s.displayClients() {
		if c.viewer != nil && c.viewer.VisiblePane(s.app, pid) {
			return true
		}
	}
	return false
}

func (s *Server) tickViewers() {
	s.flushPaneClipboard()
	s.tickNativeGraphics()
	for _, c := range s.displayClients() {
		if c.viewer != nil {
			c.viewer.Tick(s.app)
		}
	}
}

func (s *Server) drawViewers() {
	clients := s.displayClients()
	for _, c := range clients {
		if c.viewer != nil {
			for _, ws := range s.app.State().Workspaces {
				for _, tab := range ws.Tabs {
					for _, pid := range tab.Layout.PaneIDs() {
						if c.viewer.VisiblePane(s.app, pid) {
							s.app.MarkViewerSeen(pid)
						}
					}
				}
			}
		}
	}
	var panes map[int]*render.PaneSnap
	if len(clients) > 0 {
		panes = s.app.SessionPaneSnaps()
	}
	for _, c := range clients {
		if c.viewer != nil {
			c.viewer.Draw(s.app, panes)
		}
	}
}

type viewerSink struct {
	s *Server
	c *clientConn
}

func (w *viewerSink) Write(bytes []byte) (int, error) {
	if err := w.c.send(clientMsg{Type: "output", Data: encode64(bytes)}); err != nil {
		w.s.dropConn(w.c)
		return 0, err
	}
	return len(bytes), nil
}
