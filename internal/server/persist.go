package server

// Session persistence and restart (SPEC G3/G5): the server writes
// session.json under the session directory after every persistent state
// change (coalesced), again on shutdown, and restores it on startup.
// Restart saves, stops, respawns a detached `ikesu server`, and the
// attached client reconnects to it.

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/childenv"
	"os"
	"syscall"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/retained"
	"github.com/kazuph/ikesu/internal/session"
)

// sessionSaveDelay coalesces bursts of state changes into one write.
const sessionSaveDelay = 250 * time.Millisecond

// markSessionDirty schedules a coalesced session.json write. It runs on
// the app loop (the app's change hook).
func (s *Server) markSessionDirty() {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if s.saveRunning {
		s.savePending = true
		return
	}
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	s.saveTimer = time.AfterFunc(sessionSaveDelay, s.saveScheduledSession)
}

type sessionSnapshot struct {
	sequence uint64
	file     *session.File
	history  *session.History
}

// Capture on the model owner, then let the timer goroutine perform disk I/O.
func (s *Server) saveScheduledSession() {
	s.saveMu.Lock()
	if s.saveRunning {
		s.savePending = true
		s.saveMu.Unlock()
		return
	}
	s.saveRunning = true
	s.saveMu.Unlock()
	defer func() {
		s.saveMu.Lock()
		s.saveRunning = false
		if s.savePending {
			s.savePending = false
			if s.closing.Load() == 0 {
				s.saveTimer = time.AfterFunc(sessionSaveDelay, s.saveScheduledSession)
			}
		}
		s.saveMu.Unlock()
	}()
	var observations []sessionObservation
	s.app.Do(func() { observations = s.prepareSessionObservations() })
	for i := range observations {
		observations[i].observe()
	}
	var snapshot *sessionSnapshot
	s.app.Do(func() {
		for _, observation := range observations {
			s.applySessionObservation(observation)
		}
		snapshot = s.captureSessionWithoutObservation()
	})
	s.writeSession(snapshot)
}

// saveSession writes session.json atomically. It must run on the app loop
// because it reads model state and session records.
func (s *Server) saveSession() bool {
	return s.writeSession(s.captureSession())
}

func (s *Server) captureSession() *sessionSnapshot {
	if s.app == nil {
		return nil
	}
	s.observeAgentSessions()
	return s.captureSessionWithoutObservation()
}

func (s *Server) captureSessionWithoutObservation() *sessionSnapshot {
	if s.app == nil {
		return nil
	}
	f := s.app.State().Snapshot()
	f.NextPaneID = max(f.NextPaneID, s.app.State().HandoffCounters().RuntimePane)
	if len(f.Workspaces) == 0 {
		return nil
	}
	var ledger map[int]dispatch.PaneSession
	ledgerLoaded := false
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
			p.SessionStartSource = rec.SessionStartSource
		}
		// A safe same-agent ledger id keeps the exact session on the
		// snapshot even when the terminal-side record was lost.
		if !session.SafeSessionValue(p.SessionKind, p.SessionID) {
			if !ledgerLoaded {
				ledger = s.ledgerEntries()
				ledgerLoaded = true
			}
			if led, ok := ledger[p.ID]; ok && led.SessionID != "" &&
				(p.Agent == "" || led.Agent == p.Agent) {
				if p.Agent == "" {
					p.Agent = led.Agent
				}
				p.SessionID = led.SessionID
				p.SessionKind = led.SessionKind
				p.SessionStartSource = led.SessionStartSource
				if p.Title == "" {
					p.Title = led.Title
				}
			}
		}
	})
	s.saveSequence++
	return &sessionSnapshot{sequence: s.saveSequence, file: f, history: s.capturePaneHistory(f)}
}

func (s *Server) writeSession(snapshot *sessionSnapshot) bool {
	if snapshot == nil {
		return false
	}
	s.saveWriteMu.Lock()
	defer s.saveWriteMu.Unlock()
	// Shutdown/restart saves synchronously. A delayed older timer must never
	// overwrite that final state, or another newer captured generation.
	if s.saveClosed || snapshot.sequence <= s.savedSequence {
		return false
	}
	if err := session.Save(session.Path(s.dir), snapshot.file); err != nil {
		fmt.Fprintf(os.Stderr, "ikesu: save session: %v\n", err)
		return false
	}
	if snapshot.history != nil {
		if err := session.SaveHistory(s.dir, *snapshot.history); err != nil {
			fmt.Fprintf(os.Stderr, "ikesu: save pane history: %v\n", err)
		}
	}
	s.savedSequence = snapshot.sequence
	return true
}

func (s *Server) closeSessionWrites() {
	s.saveWriteMu.Lock()
	defer s.saveWriteMu.Unlock()
	s.saveClosed = true
}

// loadSession reads and validates session.json. It returns nil when there
// is nothing to restore; a corrupt file fails closed (fresh state), never
// a guess.
func (s *Server) loadSession() *session.File {
	f, err := session.Load(session.Path(s.dir))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ikesu: ignoring saved session: %v\n", err)
		return nil
	}
	if f == nil {
		return nil
	}
	for _, w := range f.Validate() {
		fmt.Fprintf(os.Stderr, "ikesu: session restore: %s\n", w)
	}
	if len(f.Workspaces) == 0 {
		return nil
	}
	return f
}

// requestRestart saves, then quits the app loop; cleanup respawns a
// detached `ikesu server` which restores session.json. Must run on the app
// loop.
func (s *Server) requestRestart() {
	s.restarting = true
	s.saveSession()
	s.app.ExecCmd(model.CmdQuit{})
}

// notifyRestart tells the attached client the server is restarting so it
// can reconnect, then drops the connection.
func (s *Server) notifyRestart() { s.closeClients("restart") }

// spawnSuccessor launches a detached `ikesu server` for this session. It is
// called only after cleanup released the session lock, so the child
// acquires it cleanly. Mirrors spawnServer in internal/client.
func (s *Server) spawnSuccessor() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ikesu: restart: %v\n", err)
		return
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ikesu: restart: %v\n", err)
		return
	}
	defer devNull.Close()
	logf, err := os.OpenFile(api.LogPath(s.dir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ikesu: restart: %v\n", err)
		return
	}
	defer logf.Close()
	if original := s.originalExecutable; original != "" {
		if _, statErr := os.Stat(original); statErr == nil {
			exe = original
		}
	}
	fmt.Fprintf(logf, "ikesu: restart executable: %s\n", exe)

	cmd := childenv.Command(exe, "--session", s.session, "server")
	if original := s.originalExecutable; original != "" {
		cmd.Env = append(cmd.Env, retained.OriginalPathEnv+"="+original)
	}
	cmd.Args[0] = os.Args[0]
	cmd.Dir = s.cwd
	cmd.Stdin = devNull
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "ikesu: restart: %v\n", err)
		return
	}
	_ = cmd.Process.Release()
}
