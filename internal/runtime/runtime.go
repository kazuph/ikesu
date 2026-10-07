// Package runtime owns everything with side effects: PTYs, child processes,
// terminal emulators, and the OS probes used by detection. The application
// state lives in model; runtime only materialises it.
package runtime

import (
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"

	"github.com/kazuph/goterrace/internal/detect"
	"github.com/kazuph/goterrace/internal/platform"
)

// ScrollbackSize is the number of lines kept per pane.
const ScrollbackSize = 1000

// EventKind classifies pane lifecycle notifications.
type EventKind int

const (
	// EventExited means the pane's process exited.
	EventExited EventKind = iota
	// EventChanged means the pane's screen content changed.
	EventChanged
	// EventStatus means the pane's agent status changed.
	EventStatus
)

// Event is a runtime→TUI notification.
type Event struct {
	Pane int
	Kind EventKind
}

// CursorState mirrors the pane's terminal cursor for IME anchoring.
type CursorState struct {
	X, Y    int
	Visible bool
	Style   vt.CursorStyle
	Blink   bool
}

// Pane is a running terminal: a process on a PTY plus its emulator state.
type Pane struct {
	ID  int
	cwd string // spawn cwd; OSC 7 or proc cwd may refine it

	cmd     *exec.Cmd
	ptyFile *os.File
	done    chan struct{}

	mu            sync.Mutex
	emu           *vt.Emulator
	dead          bool
	fgPID         int
	procName      string
	agentID       string
	procCwd       string
	oscTitle      string
	oscProgress   string
	altScreen     bool
	mouseTracking bool
	mouseMotion   bool
	appCursorKeys bool
	modes         map[ansi.Mode]bool
	cursor        CursorState

	tap oscTap
	ev  chan<- Event

	// changedPending is set while an EventChanged for this pane is still
	// sitting in the event queue or being consumed by the TUI.
	changedPending atomic.Bool
}

// Snapshot is a consistent read of the pane's observable state.
type Snapshot struct {
	ProcName      string
	AgentID       string
	FgPID         int
	Cwd           string
	OSCTitle      string
	OSCProgress   string
	AltScreen     bool
	MouseTracking bool
	MouseMotion   bool
	AppCursorKeys bool
	BracketedKeys bool
	Cursor        CursorState
	ScrollbackLen int
	Dead          bool
}

// Supervisor runs all panes of the app.
type Supervisor struct {
	mu     sync.Mutex
	panes  map[int]*Pane
	events chan Event
	done   chan struct{}
}

// NewSupervisor creates an empty supervisor.
func NewSupervisor() *Supervisor {
	return &Supervisor{
		panes:  map[int]*Pane{},
		events: make(chan Event, 256),
		done:   make(chan struct{}),
	}
}

// Events returns the notification channel.
func (s *Supervisor) Events() <-chan Event { return s.events }

// Get returns the pane or nil.
func (s *Supervisor) Get(id int) *Pane {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.panes[id]
}

// Spawn starts a login shell in cwd inside a new pane of the given size.
func (s *Supervisor) Spawn(id int, cwd string, cols, rows int) (*Pane, error) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if cwd == "" {
		cwd = os.Getenv("HOME")
	}

	emu := vt.NewEmulator(cols, rows)
	emu.SetScrollbackSize(ScrollbackSize)

	p := &Pane{ID: id, cwd: cwd, emu: emu, ev: s.events, done: make(chan struct{})}
	p.cursor.Visible = true

	emu.SetCallbacks(vt.Callbacks{
		Title:            p.onOSCTitle,
		AltScreen:        func(on bool) { p.altScreen = on },
		CursorPosition:   func(_, n uv.Position) { p.cursor.X, p.cursor.Y = n.X, n.Y },
		CursorVisibility: func(v bool) { p.cursor.Visible = v },
		CursorStyle:      func(st vt.CursorStyle, blink bool) { p.cursor.Style, p.cursor.Blink = st, blink },
		WorkingDirectory: func(d string) {
			if d != "" {
				p.procCwd = d
			}
		},
		EnableMode:  func(m ansi.Mode) { p.onMode(m, true) },
		DisableMode: func(m ansi.Mode) { p.onMode(m, false) },
	})

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	cmd := exec.Command(shell, "-l")
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"GOTERRACE=1",
		"GOTERRACE_PANE_ID="+itoa(id),
	)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		emu.Close()
		return nil, err
	}
	p.cmd = cmd
	p.ptyFile = f

	go p.pumpPTYToEmu()
	go p.pumpEmuToPTY()
	go p.waitExit()

	s.mu.Lock()
	s.panes[id] = p
	s.mu.Unlock()
	return p, nil
}

func (p *Pane) onMode(m ansi.Mode, on bool) {
	if p.modes == nil {
		p.modes = map[ansi.Mode]bool{}
	}
	p.modes[m] = on
	p.mouseTracking = p.modes[ansi.ModeMouseX10] || p.modes[ansi.ModeMouseNormal] ||
		p.modes[ansi.ModeMouseHighlight] || p.modes[ansi.ModeMouseButtonEvent] ||
		p.modes[ansi.ModeMouseAnyEvent]
	p.mouseMotion = p.modes[ansi.ModeMouseButtonEvent] || p.modes[ansi.ModeMouseAnyEvent]
	p.appCursorKeys = p.modes[ansi.ModeCursorKeys]
}

func (p *Pane) onOSCTitle(t string) { p.oscTitle = t }

// pumpPTYToEmu copies process output into the emulator and taps OSC state.
func (p *Pane) pumpPTYToEmu() {
	buf := make([]byte, 64*1024)
	for {
		n, err := p.ptyFile.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			p.mu.Lock()
			for _, e := range p.tap.observe(chunk) {
				switch e[0] {
				case "0", "2":
					p.oscTitle = e[1]
				case "9":
					p.oscProgress = e[1]
				}
			}
			_, _ = p.emu.Write(chunk)
			p.mu.Unlock()
			p.notifyChanged()
		}
		if err != nil {
			return
		}
	}
}

// pumpEmuToPTY drains emulator responses (DA reports, mouse/key encodings,
// in-band resize answers) into the PTY so applications receive them.
func (p *Pane) pumpEmuToPTY() {
	buf := make([]byte, 8192)
	for {
		n, err := p.emu.Read(buf)
		if n > 0 {
			if _, werr := p.ptyFile.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (p *Pane) waitExit() {
	_ = p.cmd.Wait()
	p.mu.Lock()
	p.dead = true
	p.mu.Unlock()
	p.notify(EventExited)
}

func (p *Pane) notify(k EventKind) {
	select {
	case p.ev <- Event{Pane: p.ID, Kind: k}:
	default:
	}
}

// notifyChanged coalesces EventChanged notifications: while one is still
// pending — sent but not yet consumed via MarkChangedRead — later screen
// changes skip the send, because the queued event already prompts a redraw
// of the latest screen state.
func (p *Pane) notifyChanged() {
	if !p.changedPending.CompareAndSwap(false, true) {
		return
	}
	select {
	case p.ev <- Event{Pane: p.ID, Kind: EventChanged}:
	default:
		p.changedPending.Store(false)
	}
}

// MarkChangedRead clears the pending EventChanged flag. The TUI calls it
// when it consumes the event, allowing the next screen change to notify
// again.
func (p *Pane) MarkChangedRead() {
	p.changedPending.Store(false)
}

// Kill terminates the pane's process tree and releases the PTY.
func (s *Supervisor) Kill(id int) {
	p := s.Get(id)
	if p == nil {
		return
	}
	s.mu.Lock()
	delete(s.panes, id)
	s.mu.Unlock()

	p.mu.Lock()
	p.dead = true
	p.mu.Unlock()

	if p.ptyFile != nil {
		_ = p.ptyFile.Close() // SIGHUP to foreground group
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = unix.Kill(-p.cmd.Process.Pid, unix.SIGHUP)
		_ = p.cmd.Process.Kill()
	}
	_ = p.emu.Close()
}

// Input writes raw key/paste bytes to the pane's PTY.
func (s *Supervisor) Input(id int, b []byte) {
	if p := s.Get(id); p != nil {
		_, _ = p.ptyFile.Write(b)
	}
}

// SendMouse encodes a mouse event in the mode the app negotiated and feeds it
// to the PTY through the emulator's response channel.
func (p *Pane) SendMouse(m uv.MouseEvent) {
	p.mu.Lock()
	p.emu.SendMouse(m)
	p.mu.Unlock()
}

// Resize updates the emulator and PTY window size (SIGWINCH to the child).
func (s *Supervisor) Resize(id, cols, rows int) {
	p := s.Get(id)
	if p == nil || cols <= 0 || rows <= 0 {
		return
	}
	p.mu.Lock()
	p.emu.Resize(cols, rows)
	p.mu.Unlock()
	_ = pty.Setsize(p.ptyFile, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

// WithEmu runs fn with exclusive access to the pane's emulator.
func (p *Pane) WithEmu(fn func(e *vt.Emulator)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn(p.emu)
}

// ScreenText returns the visible screen rows as plain text (right-trimmed,
// joined with \n) — the input for agent detection. Scrollback is excluded.
func (p *Pane) ScreenText() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	w, h := p.emu.Width(), p.emu.Height()
	var b strings.Builder
	for y := 0; y < h; y++ {
		var sb strings.Builder
		for x := 0; x < w; x++ {
			if c := p.emu.CellAt(x, y); c != nil && c.Width > 0 {
				sb.WriteString(c.Content)
			}
		}
		line := strings.TrimRight(sb.String(), " ")
		if y > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return b.String()
}

// Snapshot reads the pane's observable state under the lock.
func (p *Pane) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snapshotLocked()
}

func (p *Pane) snapshotLocked() Snapshot {
	return Snapshot{
		ProcName:      p.procName,
		AgentID:       p.agentID,
		FgPID:         p.fgPID,
		Cwd:           p.procCwdOrDefault(),
		OSCTitle:      p.oscTitle,
		OSCProgress:   p.oscProgress,
		AltScreen:     p.altScreen,
		MouseTracking: p.mouseTracking,
		MouseMotion:   p.mouseMotion,
		AppCursorKeys: p.appCursorKeys,
		Cursor:        p.cursor,
		ScrollbackLen: p.emu.ScrollbackLen(),
		Dead:          p.dead,
	}
}

func (p *Pane) procCwdOrDefault() string {
	if p.procCwd != "" {
		return p.procCwd
	}
	return p.cwd
}

// ScreenLines returns the visible screen text (one trimmed string per row).
func (p *Pane) ScreenLines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return screenLines(p.emu)
}

func screenLines(emu *vt.Emulator) []string {
	h := emu.Height()
	w := emu.Width()
	lines := make([]string, h)
	for y := 0; y < h; y++ {
		var sb strings.Builder
		for x := 0; x < w; x++ {
			c := emu.CellAt(x, y)
			if c == nil || c.Width == 0 {
				continue
			}
			sb.WriteString(c.Content)
		}
		lines[y] = strings.TrimRight(sb.String(), " ")
	}
	return lines
}

// ScrollbackLines returns scrollback history (index 0 = oldest).
func (p *Pane) ScrollbackLines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.emu.ScrollbackLen()
	out := make([]string, 0, n)
	w := p.emu.Width()
	for y := 0; y < n; y++ {
		var sb strings.Builder
		for x := 0; x < w; x++ {
			c := p.emu.ScrollbackCellAt(x, y)
			if c == nil || c.Width == 0 {
				continue
			}
			sb.WriteString(c.Content)
		}
		out = append(out, strings.TrimRight(sb.String(), " "))
	}
	return out
}

// PollProcess refreshes the foreground process info. Called periodically.
func (p *Pane) PollProcess() {
	if p.ptyFile == nil {
		return
	}
	job := platform.ForegroundJobOf(int(p.ptyFile.Fd()))
	if len(job.Processes) == 0 {
		return
	}
	dj := detect.Job{ProcessGroupID: job.ProcessGroupID}
	for _, pr := range job.Processes {
		dj.Processes = append(dj.Processes, detect.JobProcess{
			PID:     pr.PID,
			Name:    pr.Name,
			Argv:    pr.Argv,
			Cmdline: pr.Cmdline,
		})
	}
	agentID, name := detect.IdentifyJob(dj)
	cwd := platform.ProcCwd(job.ProcessGroupID)
	p.mu.Lock()
	agentChanged := p.fgPID != job.ProcessGroupID || p.procName != name
	p.fgPID = job.ProcessGroupID
	p.procName = name
	p.agentID = agentID
	if cwd != "" {
		p.procCwd = cwd
	}
	if agentChanged {
		// A new foreground process must not inherit OSC evidence.
		p.oscTitle = ""
		p.oscProgress = ""
	}
	p.mu.Unlock()
	if agentChanged {
		p.notify(EventChanged)
	}
}

// ClearOSC drops retained OSC title/progress (on agent change handled in
// PollProcess; exposed for tests).
func (p *Pane) ClearOSC() {
	p.mu.Lock()
	p.oscTitle, p.oscProgress = "", ""
	p.mu.Unlock()
}

// KillAll terminates every pane.
func (s *Supervisor) KillAll() {
	s.mu.Lock()
	panes := make([]*Pane, 0, len(s.panes))
	for _, p := range s.panes {
		panes = append(panes, p)
	}
	s.panes = map[int]*Pane{}
	s.mu.Unlock()
	for _, p := range panes {
		p.mu.Lock()
		p.dead = true
		p.mu.Unlock()
		if p.ptyFile != nil {
			_ = p.ptyFile.Close()
		}
		if p.cmd != nil && p.cmd.Process != nil {
			_ = unix.Kill(-p.cmd.Process.Pid, unix.SIGHUP)
			_ = p.cmd.Process.Kill()
		}
		_ = p.emu.Close()
	}
	close(s.done)
}

// Done closes when KillAll ran.
func (s *Supervisor) Done() <-chan struct{} { return s.done }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [16]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}

// SpawnCwd returns the best-known cwd of a pane (for new workspaces).
func (p *Pane) SpawnCwd() string { return p.Snapshot().Cwd }
