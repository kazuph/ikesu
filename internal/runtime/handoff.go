package runtime

import (
	"context"
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
	ShellStart                  string
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
	LastMailExternalInput       int64
	IdleActivityAt              time.Time
	IdleActivitySeq             uint64
}

func (p *Pane) startPump() {
	p.startPumpWithTiming(pumpBatchWait, pumpStreamGap)
}

// Timing belongs to this pump: a terminated supervisor's reader may still
// be draining while another pane starts with different test timings.
func (p *Pane) startPumpWithTiming(batchWait, streamGap time.Duration) {
	p.pumpDone = make(chan struct{})
	p.pauseSignal = make(chan struct{})
	go func() {
		defer close(p.pumpDone)
		p.mu.Lock()
		pending := p.clipboard
		p.clipboard = nil
		p.mu.Unlock()
		p.deliverClipboard(pending, true)
		p.pumpPTYToEmu(batchWait, streamGap)
		if p.importedPID != 0 && !p.pausing.Load() && !p.retired.Load() {
			p.importedExited.Store(true)
			p.mu.Lock()
			p.dead = true
			p.mu.Unlock()
			p.endProcessWatch()
			if p.guard != nil {
				_ = p.guard.Close()
			}
			p.notify(EventExited)
			p.closeExited()
		}
	}()
}
func (s *Supervisor) PauseHandoff() ([]PaneTransfer, []*os.File, error) {
	return s.PauseHandoffContext(context.Background())
}

func (s *Supervisor) PauseHandoffContext(ctx context.Context) ([]PaneTransfer, []*os.File, error) {
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
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		p := s.Get(id)
		if !p.pausing.CompareAndSwap(false, true) {
			return fail(fmt.Errorf("pane %d is already pausing", id))
		}
		close(p.pauseSignal)
		if _, err := p.wakeWrite.Write([]byte{1}); err != nil {
			return fail(fmt.Errorf("pause PTY: %w", err))
		}
		select {
		case <-p.pumpDone:
		case <-ctx.Done():
			return fail(ctx.Err())
		}
	}
	// The app loop cannot consume queued clipboard events while it is handing
	// off. Move them ahead of each pane's unsent events before snapshotting.
	queued := make(map[*Pane][]string)
	var other []Event
	for n := len(s.events); n > 0; n-- {
		e := <-s.events
		if e.Kind == EventClipboard && s.Get(e.Pane) == e.Source {
			queued[e.Source] = append(queued[e.Source], e.Data)
		} else {
			other = append(other, e)
		}
	}
	for p, values := range queued {
		p.mu.Lock()
		p.clipboard = append(values, p.clipboard...)
		p.mu.Unlock()
	}
	for _, e := range other {
		select {
		case s.events <- e:
		default:
			if e.Source != nil && (e.Kind == EventChanged || e.Kind == EventExited) {
				e.Source.keepMissedChange(e.Kind)
			}
		}
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		p := s.Get(id)
		p.mu.Lock()
		terminal, err := p.emu.MarshalHandoff()
		if err != nil {
			p.mu.Unlock()
			return fail(err)
		}
		v := PaneTransfer{ID: id, PID: p.ShellPID(), Cwd: p.cwd, Argv: p.spawnArgv, Terminal: terminal, AgentOverride: p.agentOverride, GuardStarted: p.guardStarted, Modes: p.modes, Read: p.read.transfer()}
		v.ShellStart = p.shellStart
		v.Graphics, v.GraphicsEnabled, v.CellWidth, v.CellHeight = p.graphics, p.graphicsEnabled, p.cellWidth, p.cellHeight
		v.TapState, v.TapBody, v.Clipboard = int(p.tap.state), p.tap.body, p.clipboard
		if p.guard != nil {
			g := p.guard.Transfer()
			v.Guard = &g
		}
		p.mu.Unlock()
		v.LastMailExternalInput = p.lastMailExternalInput.Load()
		v.IdleActivityAt, v.IdleActivitySeq = p.IdleActivity()
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
	for _, p := range s.panes {
		if !p.pausing.Load() {
			continue
		}
		done := p.pumpDone
		select {
		case <-done:
			s.resumePaneLocked(p)
		default:
			go func(p *Pane, done <-chan struct{}) {
				<-done
				s.mu.Lock()
				defer s.mu.Unlock()
				if s.panes[p.ID] == p && p.pumpDone == done && p.pausing.Load() {
					s.resumePaneLocked(p)
				}
			}(p, done)
		}
	}
	s.mu.Unlock()
}

func (s *Supervisor) resumePaneLocked(p *Pane) {
	if ptyReadyNow(p.wakeFD) {
		_, _ = p.wakeRead.Read(make([]byte, 1))
	}
	p.pausing.Store(false)
	p.startPump()
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
		p.lastMailExternalInput.Store(v.LastMailExternalInput)
		p.RestoreIdleActivity(v.IdleActivityAt, v.IdleActivitySeq)
		p.importedPID = v.PID
		p.shellStart = v.ShellStart
		if p.shellStart == "" && v.PID > 1 {
			// Older handoff payloads did not include a process generation.
			// Establish it only when the live process owns this transferred PTY.
			p.shellStart, _ = legacyShellStart(p.ptyFD, v.PID)
		}
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
		if q.Dead {
			p.closeExited()
		}
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

// ShellIdentity is recorded when the pane is spawned and retained on handoff.
func (p *Pane) ShellIdentity() (int, string) { return p.ShellPID(), p.shellStart }

func (p *Pane) killProcess() {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = unix.Kill(-p.cmd.Process.Pid, unix.SIGHUP)
		_ = p.cmd.Process.Kill()
	} else if p.importedPID != 0 && !p.importedExited.Load() {
		_ = unix.Kill(-p.importedPID, unix.SIGHUP)
		_ = unix.Kill(p.importedPID, unix.SIGKILL)
	}
}

// MailInputTimes captures admitted keys after the terminal snapshot.
func (s *Supervisor) MailInputTimes() map[int]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	times := make(map[int]int64, len(s.panes))
	for id, pane := range s.panes {
		times[id] = pane.lastMailExternalInput.Load()
	}
	return times
}

// RestoreMailInputTimes conservatively starts the quiet period when a time is missing.
func (s *Supervisor) RestoreMailInputTimes(times map[int]int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixNano()
	for id, pane := range s.panes {
		at := times[id]
		if at == 0 {
			at = now
		}
		pane.lastMailExternalInput.Store(at)
	}
}

// IdleActivityTransfer is the last admitted activity at the handoff barrier.
type IdleActivityTransfer struct {
	At       time.Time
	Sequence uint64
}

func (s *Supervisor) IdleActivities() map[int]IdleActivityTransfer {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[int]IdleActivityTransfer, len(s.panes))
	for id, p := range s.panes {
		at, seq := p.IdleActivity()
		result[id] = IdleActivityTransfer{At: at, Sequence: seq}
	}
	return result
}

func (s *Supervisor) RestoreIdleActivities(values map[int]IdleActivityTransfer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.panes {
		value := values[id]
		p.RestoreIdleActivity(value.At, value.Sequence)
	}
}
