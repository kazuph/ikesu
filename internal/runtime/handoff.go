package runtime

import (
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/guard"
	"github.com/kazuph/ikesu/internal/kitty"
	"golang.org/x/sys/unix"
	"os"
	"sort"
	"time"
)

type PaneTransfer struct {
	Graphics                    *kitty.Store
	GraphicsEnabled             bool
	CellWidth, CellHeight       int
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

func (p *Pane) startPump() {
	p.startPumpWithTiming(pumpBatchWait, pumpStreamGap)
}

// Timing belongs to this pump: a terminated supervisor's reader may still
// be draining while another pane starts with different test timings.
func (p *Pane) startPumpWithTiming(batchWait, streamGap time.Duration) {
	p.pumpDone = make(chan struct{})
	go func() {
		defer close(p.pumpDone)
		p.pumpPTYToEmu(batchWait, streamGap)
		if p.importedPID != 0 && !p.pausing.Load() && !p.retired.Load() {
			p.endProcessWatch()
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
		if _, err := p.wakeWrite.Write([]byte{1}); err != nil {
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
		v.Graphics, v.GraphicsEnabled, v.CellWidth, v.CellHeight = p.graphics, p.graphicsEnabled, p.cellWidth, p.cellHeight
		v.TapState, v.TapBody, v.Clipboard = int(p.tap.state), p.tap.body, p.clipboard
		if p.guard != nil {
			g := p.guard.Transfer()
			v.Guard = &g
		}
		p.mu.Unlock()
		v.Snapshot = p.Snapshot()
		fd, err := unix.FcntlInt(uintptr(p.ptyFD), unix.F_DUPFD_CLOEXEC, 0)
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
			if ptyReadyNow(p.wakeFD) {
				_, _ = p.wakeRead.Read(make([]byte, 1))
			}
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
		p := s.newPane(v.ID, v.Cwd, 1, 1)
		if err := p.installPTY(files[i]); err != nil {
			return err
		}
		if err := p.emu.UnmarshalHandoff(v.Terminal); err != nil {
			return err
		}
		p.importedPID = v.PID
		p.spawnArgv = v.Argv
		p.agentOverride, p.guardStarted, p.modes = v.AgentOverride, v.GuardStarted, v.Modes
		p.graphics, p.graphicsEnabled, p.cellWidth, p.cellHeight = v.Graphics, v.GraphicsEnabled, v.CellWidth, v.CellHeight
		if p.graphics != nil {
			p.emu.SetScrollCallback(p.scrollGraphics)
		}
		p.read = v.Read.restore()
		p.tap = oscTap{state: oscState(v.TapState), body: v.TapBody}
		p.clipboard = v.Clipboard
		if p.read != nil {
			p.emu.Scrollback().OnDrop = p.read.dropHistory
		}
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
		p.beginProcessWatch()
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
		p.closePTY()
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
