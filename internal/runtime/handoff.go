package runtime

import (
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/guard"
	"golang.org/x/sys/unix"
	"os"
	"sort"
	"time"
)

type PaneTransfer struct {
	ID, PID                     int
	Cwd                         string
	Argv                        []string
	Terminal                    []byte
	Snapshot                    Snapshot
	AgentOverride, GuardStarted string
	Modes                       map[ansi.Mode]bool
	Guard                       *guard.Transfer
	Read                        *readTransfer
	TapState                    int
	TapBody                     []byte
	Clipboard                   []string
}

// Rewrap a nonblocking descriptor so Go's poller can interrupt only our read.
func pollablePTY(original *os.File) (*os.File, int, error) {
	fd, err := unix.Dup(int(original.Fd()))
	if err != nil {
		return nil, 0, err
	}
	if err = unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return nil, 0, err
	}
	original.Close()
	return os.NewFile(uintptr(fd), "gtx-pty"), fd, nil
}
func (p *Pane) startPump() {
	p.pumpDone = make(chan struct{})
	go func() {
		defer close(p.pumpDone)
		p.pumpPTYToEmu()
		if p.importedPID != 0 && !p.pausing.Load() && !p.retired.Load() {
			p.importedExited.Store(true)
			p.mu.Lock()
			p.dead = true
			p.mu.Unlock()
			if p.guard != nil {
				_ = p.guard.Close()
			}
			p.notify(EventExited)
		}
	}()
}
func (s *Supervisor) PauseHandoff() ([]PaneTransfer, []*os.File, error) {
	s.mu.Lock()
	ids := make([]int, 0, len(s.panes))
	for id := range s.panes {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	sort.Ints(ids)
	var states []PaneTransfer
	var files []*os.File
	fail := func(err error) ([]PaneTransfer, []*os.File, error) {
		for _, f := range files {
			f.Close()
		}
		s.ResumeHandoff()
		return nil, nil, err
	}
	for _, id := range ids {
		p := s.Get(id)
		p.pausing.Store(true)
		if err := p.ptyFile.SetReadDeadline(time.Now()); err != nil {
			return fail(fmt.Errorf("pause PTY: %w", err))
		}
		<-p.pumpDone
		p.mu.Lock()
		terminal, err := p.emu.MarshalHandoff()
		if err != nil {
			p.mu.Unlock()
			return fail(err)
		}
		v := PaneTransfer{ID: id, PID: p.ShellPID(), Cwd: p.cwd, Argv: p.spawnArgv, Terminal: terminal, AgentOverride: p.agentOverride, GuardStarted: p.guardStarted, Modes: p.modes, Read: p.read.transfer()}
		v.TapState, v.TapBody, v.Clipboard = int(p.tap.state), p.tap.body, p.clipboard
		if p.guard != nil {
			g := p.guard.Transfer()
			v.Guard = &g
		}
		p.mu.Unlock()
		v.Snapshot = p.Snapshot()
		fd, err := unix.Dup(p.ptyFD)
		if err != nil {
			return fail(err)
		}
		files = append(files, os.NewFile(uintptr(fd), "handoff-pty"))
		states = append(states, v)
	}
	return states, files, nil
}
func (s *Supervisor) ResumeHandoff() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.panes {
		select {
		case <-p.pumpDone:
			_ = unix.SetNonblock(p.ptyFD, true)
			_ = p.ptyFile.SetReadDeadline(time.Time{})
			p.pausing.Store(false)
			p.startPump()
		default:
		}
	}
}
func (s *Supervisor) ImportHandoff(states []PaneTransfer, files []*os.File) error {
	if len(states) != len(files) {
		return fmt.Errorf("handoff PTY count mismatch")
	}
	for i, v := range states {
		file, fd, err := pollablePTY(files[i])
		if err != nil {
			return err
		}
		p := s.newPane(v.ID, v.Cwd, 1, 1)
		if err := p.emu.UnmarshalHandoff(v.Terminal); err != nil {
			return err
		}
		p.ptyFile, p.ptyFD = file, fd
		p.importedPID = v.PID
		p.spawnArgv = v.Argv
		p.agentOverride, p.guardStarted, p.modes = v.AgentOverride, v.GuardStarted, v.Modes
		p.read = v.Read.restore()
		p.tap = oscTap{state: oscState(v.TapState), body: v.TapBody}
		p.clipboard = v.Clipboard
		p.emu.Scrollback().OnDrop = p.read.dropHistory
		q := v.Snapshot
		p.fgPID, p.procName, p.agentID, p.procCwd = q.FgPID, q.ProcName, q.AgentID, q.Cwd
		p.oscTitle, p.oscProgress, p.altScreen = q.OSCTitle, q.OSCProgress, q.AltScreen
		p.mouseTracking, p.mouseMotion, p.appCursorKeys = q.MouseTracking, q.MouseMotion, q.AppCursorKeys
		p.cursor, p.scrollbarVisible, p.dead = q.Cursor, q.ScrollbarVisible, q.Dead
		p.importedExited.Store(q.Dead)
		p.revision.Store(q.Revision)
		if v.Guard != nil {
			p.guard = guard.Import(*v.Guard)
		}
		s.panes[v.ID] = p
	}
	return nil
}
func (s *Supervisor) StartImported() {
	for _, p := range s.panes {
		p.startPump()
		go p.pumpEmuToPTY()
	}
}

// ReleaseHandoff drops our copies without signalling processes or deleting markers.
func (s *Supervisor) ReleaseHandoff() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.panes {
		p.retired.Store(true)
		p.ptyFile.Close()
		p.emu.Close()
	}
	s.panes = map[int]*Pane{}
}

// ShellPID returns the pid of the pane's shell process, or 0.
func (p *Pane) ShellPID() int {
	if p.importedPID != 0 {
		return p.importedPID
	}
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *Pane) killProcess() {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = unix.Kill(-p.cmd.Process.Pid, unix.SIGHUP)
		_ = p.cmd.Process.Kill()
	} else if p.importedPID != 0 && !p.importedExited.Load() {
		_ = unix.Kill(-p.importedPID, unix.SIGHUP)
		_ = unix.Kill(p.importedPID, unix.SIGKILL)
	}
}
