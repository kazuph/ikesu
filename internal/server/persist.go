package server

// Session persistence and restart (SPEC G3/G5): the server writes
// session.json under the session directory after every persistent state
// change (coalesced), again on shutdown, and restores it on startup.
// Restart saves, stops, respawns a detached `gtx server`, and the
// attached client reconnects to it.

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/session"
)

// sessionSaveDelay coalesces bursts of state changes into one write.
const sessionSaveDelay = 250 * time.Millisecond

// markSessionDirty schedules a coalesced session.json write. It runs on
// the app loop (the app's change hook).
func (s *Server) markSessionDirty() {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	s.saveTimer = time.AfterFunc(sessionSaveDelay, func() {
		s.app.Do(s.saveSession)
	})
}

// saveSession writes session.json atomically. It must run on the app loop
// because it reads model state and session records.
func (s *Server) saveSession() {
	if s.app == nil {
		return
	}
	s.observeAgentSessions()
	f := s.app.State().Snapshot()
	if len(f.Workspaces) == 0 {
		return
	}
	f.EachPane(func(w *session.Workspace, _ *session.Tab, p *session.Pane) {
		if rp := s.sup.Get(p.ID); rp != nil {
			if cwd := rp.Snapshot().Cwd; cwd != "" {
				p.Cwd = cwd
			}
		}
		if p.Cwd == "" {
			p.Cwd = w.Cwd
		}
		if rec, ok := s.app.SessionRecord(p.ID); ok {
			p.Agent = rec.Agent
			p.SessionID = rec.Value
			p.SessionKind = rec.Kind
			p.Title = rec.Title
		}
		// A safe same-agent ledger id keeps the exact session on the
		// snapshot even when the terminal-side record was lost.
		if !session.SafeSessionValue(p.SessionKind, p.SessionID) {
			if led, ok := s.ledgerEntry(p.ID); ok && led.SessionID != "" &&
				(p.Agent == "" || led.Agent == p.Agent) {
				if p.Agent == "" {
					p.Agent = led.Agent
				}
				p.SessionID = led.SessionID
				p.SessionKind = led.SessionKind
				if p.Title == "" {
					p.Title = led.Title
				}
			}
		}
	})
	if err := session.Save(session.Path(s.dir), f); err != nil {
		fmt.Fprintf(os.Stderr, "gtx: save session: %v\n", err)
	}
}

// loadSession reads and validates session.json. It returns nil when there
// is nothing to restore; a corrupt file fails closed (fresh state), never
// a guess.
func (s *Server) loadSession() *session.File {
	f, err := session.Load(session.Path(s.dir))
	if err != nil {
		fmt.Fprintf(os.Stderr, "gtx: ignoring saved session: %v\n", err)
		return nil
	}
	if f == nil {
		return nil
	}
	for _, w := range f.Validate() {
		fmt.Fprintf(os.Stderr, "gtx: session restore: %s\n", w)
	}
	if len(f.Workspaces) == 0 {
		return nil
	}
	return f
}

// requestRestart saves, then quits the app loop; cleanup respawns a
// detached `gtx server` which restores session.json. Must run on the app
// loop.
func (s *Server) requestRestart() {
	s.restarting = true
	s.saveSession()
	s.app.ExecCmd(model.CmdQuit{})
}

// notifyRestart tells the attached client the server is restarting so it
// can reconnect, then drops the connection.
func (s *Server) notifyRestart() {
	s.mu.Lock()
	c := s.client
	s.client = nil
	s.mu.Unlock()
	if c != nil {
		_ = c.send(clientMsg{Type: "restart"})
		c.close()
	}
}

// spawnSuccessor launches a detached `gtx server` for this session. It is
// called only after cleanup released the session lock, so the child
// acquires it cleanly. Mirrors spawnServer in internal/client.
func (s *Server) spawnSuccessor() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gtx: restart: %v\n", err)
		return
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gtx: restart: %v\n", err)
		return
	}
	defer devNull.Close()
	logf, err := os.OpenFile(api.LogPath(s.dir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gtx: restart: %v\n", err)
		return
	}
	defer logf.Close()

	cmd := exec.Command(exe, "--session", s.session, "server")
	cmd.Args[0] = os.Args[0]
	cmd.Dir = s.cwd
	cmd.Stdin = devNull
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "gtx: restart: %v\n", err)
		return
	}
	_ = cmd.Process.Release()
}
