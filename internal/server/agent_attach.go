package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kazuph/goterrace/internal/api"
)

type agentAttachTarget struct {
	Target   string `json:"target"`
	Takeover bool   `json:"takeover"`
}

// This private client-socket stream is independent of the full-UI endpoint
// protocol. Its controller lifetime is the connection, with no persisted state.
func (s *Server) attachAgent(c *clientConn, rd *bufio.Reader, hello clientMsg) {
	defer c.close()
	var request agentAttachTarget
	if json.Unmarshal([]byte(hello.Data), &request) != nil || hello.Cols <= 0 || hello.Rows <= 0 {
		_ = c.send(clientMsg{Type: "error", Reason: "invalid agent attach request"})
		return
	}
	var pid int
	var failure *api.Error
	s.app.Do(func() {
		t, err := s.resolveTarget(request.Target, true)
		if err != nil {
			failure = err
		} else {
			pid = t.pid
		}
	})
	if failure != nil {
		_ = c.send(clientMsg{Type: "error", Reason: failure.Message})
		return
	}
	pane := s.sup.Get(pid)
	if pane == nil {
		_ = c.send(clientMsg{Type: "error", Reason: "agent terminal not found"})
		return
	}
	s.mu.Lock()
	if s.agentControllers == nil {
		s.agentControllers = map[int]*clientConn{}
	}
	previous := s.agentControllers[pid]
	if previous != nil && !request.Takeover {
		s.mu.Unlock()
		_ = c.send(clientMsg{Type: "error", Reason: fmt.Sprintf("server shut down: terminal attach failed: terminal %s already has an attached client; retry with --takeover", fmtPaneID(pid))})
		return
	}
	if s.closing.Load() != 0 {
		s.mu.Unlock()
		return
	}
	s.agentAttachWG.Add(1)
	defer s.agentAttachWG.Done()
	s.agentControllers[pid] = c
	s.mu.Unlock()
	if previous != nil {
		_ = previous.send(clientMsg{Type: "error", Reason: "server shut down: terminal attach taken over"})
		previous.close()
	}
	defer func() {
		s.mu.Lock()
		if s.agentControllers[pid] == c {
			delete(s.agentControllers, pid)
		}
		s.mu.Unlock()
	}()
	if c.send(clientMsg{Type: "agent_attached"}) != nil {
		return
	}
	cols, rows := hello.Cols, hello.Rows
	s.sup.Resize(pid, cols, rows)
	input := make(chan clientMsg)
	gone := make(chan struct{})
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		defer close(gone)
		for {
			m, err := readClientMsg(rd)
			if err != nil {
				return
			}
			select {
			case input <- m:
			case <-stop:
				return
			}
		}
	}()
	ticker := time.NewTicker(eventWaitPoll)
	defer ticker.Stop()
	var revision uint64
	offset := 0
	dirty := true
	for {
		sn := pane.Snapshot()
		if dirty || sn.Revision != revision {
			frame, actual := pane.AgentAttachFrame(offset, cols, rows)
			offset = actual
			if c.send(clientMsg{Type: "output", Data: encode64([]byte(frame))}) != nil {
				return
			}
			revision = sn.Revision
			dirty = false
		}
		if sn.Dead || s.closing.Load() != 0 {
			_ = c.send(clientMsg{Type: "exit"})
			return
		}
		select {
		case <-gone:
			return
		case <-ticker.C:
			if s.sup.Get(pid) != pane {
				_ = c.send(clientMsg{Type: "exit"})
				return
			}
		case m := <-input:
			s.mu.Lock()
			if s.agentControllers[pid] != c {
				s.mu.Unlock()
				return
			}
			switch m.Type {
			case "input", "paste":
				data, err := decode64(m.Data)
				if err != nil {
					s.mu.Unlock()
					return
				}
				if m.Type == "paste" && pane.BracketedPasteMode() {
					data = append(append([]byte("\x1b[200~"), data...), []byte("\x1b[201~")...)
				}
				offset = 0
				dirty = true
				if _, err = pane.WriteToPTY(data); err != nil {
					s.mu.Unlock()
					return
				}
			case "resize":
				if m.Cols > 0 && m.Rows > 0 {
					cols, rows = m.Cols, m.Rows
					s.sup.Resize(pid, cols, rows)
					dirty = true
				}
			case "scroll":
				offset += m.Rows
				dirty = true
			case "release":
				s.mu.Unlock()
				return
			default:
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()
		}
	}
}

func (s *Server) closeAgentAttachments() {
	s.mu.Lock()
	clients := make([]*clientConn, 0, len(s.agentControllers))
	for _, c := range s.agentControllers {
		clients = append(clients, c)
	}
	s.mu.Unlock()
	for _, c := range clients {
		c.conn.Close()
	}
	s.agentAttachWG.Wait()
}
