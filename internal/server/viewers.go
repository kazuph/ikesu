package server

import "github.com/kazuph/goterrace/internal/render"

func (s *Server) deliverPaneClipboard(pid int, data string) {
	var target *clientConn
	for _, c := range s.displayClients() {
		if c.viewer != nil && c.viewer.DisplaysPane(s.app, pid) && (target == nil || c.lastUse > target.lastUse) {
			target = c
		}
	}
	if target != nil {
		if err := target.send(clientMsg{Type: "output", Data: encode64([]byte(data))}); err != nil {
			s.dropConn(target)
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
