// Package runtime owns everything with side effects: PTYs, child processes,
// terminal emulators, and the OS probes used by detection. The application
// state lives in model; runtime only materialises it.
package runtime

import "github.com/kazuph/ikesu/internal/benchstage"

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"golang.org/x/sync/semaphore"
	"golang.org/x/sys/unix"

	"github.com/kazuph/ikesu/internal/detect"
	"github.com/kazuph/ikesu/internal/guard"
	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/platform"
)

// ScrollbackSize is the number of lines kept per pane.
const ScrollbackSize = 1000

// CursorState mirrors the pane's terminal cursor for IME anchoring.
type CursorState struct {
	X, Y    int
	Visible bool
	Style   vt.CursorStyle
	Blink   bool
}

// Pane is a running terminal: a process on a PTY plus its emulator state.
type Pane struct {
	graphicsLayers         map[string]*graphicsLayer
	graphics               *kitty.Store
	graphicsEnabled        bool
	graphicsVersion        string
	cellWidth, cellHeight  int
	guard                  *guard.Assets
	guardStarted           string
	ID                     int
	cwd                    string // spawn cwd; OSC 7 or proc cwd may refine it
	spawnArgv              []string
	processCache           foregroundCache
	processChanged         atomic.Bool
	processIdentityChanged atomic.Bool
	processRevision        atomic.Uint64
	processSettlingUntil   time.Time
	processOutputUntil     time.Time
	processCwdAt           time.Time
	owner                  *Supervisor

	cmd                 *exec.Cmd
	ptyFile             *os.File
	wakeRead, wakeWrite *os.File
	wakeFD              int
	ptyFD               int // captured before publishing the pane; Close may race with readers
	done                chan struct{}
	pumpDone            chan struct{}
	importedPID         int
	pausing             atomic.Bool
	retired             atomic.Bool
	importedExited      atomic.Bool

	inputMu           sync.Mutex
	lastExternalInput atomic.Int64
	externalInputSeq  atomic.Uint64
	mu                sync.Mutex
	emu               *vt.Emulator
	read              *readBuffer
	dead              bool
	fgPID             int
	procName          string
	foregroundSudo    bool
	agentID           string
	agentOverride     string // set by pane.report_agent; wins over agentID
	procCwd           string
	oscTitle          string
	oscProgress       string
	clipboard         []string
	altScreen         bool
	mouseTracking     bool
	mouseMotion       bool
	appCursorKeys     bool
	modes             map[ansi.Mode]bool
	cursor            CursorState
	scrollbarVisible  bool // measured on PTY output; resize alone preserves visibility

	tap oscTap
	ev  chan<- Event
	// changedPending is set while an EventChanged for this pane is still
	// sitting in the event queue or being consumed by the TUI.
	changedPending atomic.Bool
	// revision counts PTY output batches; bumped per flush.
	revision atomic.Uint64
}

// Snapshot is a consistent read of the pane's observable state.
type Snapshot struct {
	Revision         uint64
	ProcName         string
	AgentID          string
	FgPID            int
	Cwd              string
	OSCTitle         string
	OSCProgress      string
	AltScreen        bool
	MouseTracking    bool
	MouseMotion      bool
	AppCursorKeys    bool
	BracketedKeys    bool
	Cursor           CursorState
	ScrollbackLen    int
	ScrollbarVisible bool
	Dead             bool
}

// Supervisor runs all panes of the app.
type Supervisor struct {
	graphicsEnabled bool
	graphicsVersion string
	guardConfig     *guard.Config
	guardRoot       string
	mu              sync.Mutex
	panes           map[int]*Pane
	events          chan Event
	missedMu        sync.Mutex
	missed          map[*Pane]EventKind
	processMu       sync.Mutex
	processWork     map[*Pane]bool
	processTicks    map[*Pane]bool
	processDue      map[*Pane]time.Time
	processNext     time.Time
	done            chan struct{}
	loginPathMu     sync.Mutex
	loginPathResult *loginPathResult

	// Env, when set, contributes extra KEY=VALUE pairs to a spawned pane's
	// environment (the server uses it for IKESU_PANE_ID/IKESU_SOCKET_PATH).
	Env                     func(paneID int) []string
	defaultShell, shellMode string
	scrollbackBytes         *int
}

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

// pumpStreamGap is how recent the previous flush must be for the pump to
// treat output as still streaming: only then may a read pause briefly to
// gather more. The first chunk after an idle gap — a key echo, say — is
// written to the emulator without waiting.
const pumpStreamGap = 5 * time.Millisecond

// pumpBatchWait bounds how long the writer side keeps gathering queued
// output before one emu.Write plus notification, applied only while output
// is streaming. The cap bounds added latency to about this much.
const pumpBatchWait = time.Millisecond

// pumpBatchCap bounds how much a single gathered batch may hold; beyond it
// the writer flushes early and the queue back-pressures the reader.
const pumpBatchCap = 256 * 1024

// pumpQueueBytes bounds how much output may sit unprocessed between the
// reader and the writer. It caps how far the emulator can trail the child
// process — the screen lag after a flood ends and the output replayed
// after an interrupt — and, with pumpQueueLen, the queued memory.
const pumpQueueBytes = 256 << 10

// pumpQueueLen bounds outstanding chunks so many small reads cannot hold
// more buffers than the byte budget implies. pumpPoolLen bounds recycled
// buffers kept for reuse; both trade a little allocation for bounded RSS.
const (
	pumpQueueLen = 32
	pumpPoolLen  = 16
)

// ptyReadyNow reports whether fd has data (or an event worth a read attempt)
// available without waiting.
func ptyReadyNow(fd int) bool {
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, 0)
	return err == nil && n > 0
}

// readPTY drains the PTY into chunks as fast as the kernel delivers them and
// forwards each chunk on out, closing it at EOF or read error. While the
// writer is busy it also merges whatever is already queued into the same
// buffer, which both batches the read side and keeps read syscalls low. The
// semaphore bounds queued bytes; acquiring it is what back-pressures the
// child when the writer falls behind.
func (p *Pane) readPTY(out chan<- []byte, pool chan []byte, sem *semaphore.Weighted) {
	defer close(out)
	statsFile := pumpStatsPath
	var reads, chunksSent, bytes int64
	writeStats := func() {
		if statsFile != "" && reads > 0 {
			_ = os.WriteFile(statsFile,
				[]byte(fmt.Sprintf("reads=%d chunks=%d bytes=%d avg=%.0f\n", reads, chunksSent, bytes, float64(bytes)/float64(reads))), 0o644)
		}
	}
	defer writeStats()
	fd := p.ptyFD
	for {
		var buf []byte
		select {
		case buf = <-pool:
			buf = buf[:cap(buf)]
		default:
			buf = make([]byte, 64*1024)
		}
		n, err := p.readPTYReady(buf)
		reads++
		for err == nil && n < len(buf) && ptyReadyNow(fd) {
			m, rerr := p.ptyFile.Read(buf[n:])
			reads++
			n += m
			if rerr != nil {
				err = rerr
				break
			}
			if m == 0 {
				break
			}
		}
		if n > 0 {
			chunksSent++
			bytes += int64(n)
			_ = sem.Acquire(context.Background(), int64(n))
			out <- buf[:n]
			if chunksSent%32 == 0 {
				writeStats()
			}
		}
		if err != nil {
			return
		}
	}
}

// pumpPTYToEmu copies process output into the emulator and taps OSC state.
// The reader goroutine streams PTY output into a chunk channel so reads
// never wait on emulator work; this loop flushes each chunk immediately
// after an idle gap, and while output streams it gathers for up to
// pumpBatchWait before one emu.Write plus change notification per burst.
func (p *Pane) pumpPTYToEmu(batchWait, streamGap time.Duration) {
	chunks := make(chan []byte, pumpQueueLen)
	pool := make(chan []byte, pumpPoolLen)
	sem := semaphore.NewWeighted(pumpQueueBytes)
	go p.readPTY(chunks, pool, sem)

	recycle := func(c []byte) {
		sem.Release(int64(len(c)))
		select {
		case pool <- c[:cap(c)]:
		default:
		}
	}
	var batch []byte
	var lastFlush time.Time
	for {
		c, ok := <-chunks
		if !ok {
			return
		}
		batch = append(batch[:0], c...)
		recycle(c)

		if !lastFlush.IsZero() && time.Since(lastFlush) < streamGap {
			// Output is still streaming: gather whatever else arrives within
			// the window, then flush once.
			timer := time.NewTimer(batchWait)
		gather:
			for len(batch) < pumpBatchCap {
				select {
				case c, ok := <-chunks:
					if !ok {
						timer.Stop()
						p.flushPTYBatch(batch)
						return
					}
					batch = append(batch, c...)
					recycle(c)
				case <-timer.C:
					break gather
				}
			}
			timer.Stop()
			// The window ended while chunks were still queued: take them too
			// so a close cannot be delayed behind the timer.
			for len(batch) < pumpBatchCap {
				select {
				case c, ok := <-chunks:
					if !ok {
						p.flushPTYBatch(batch)
						return
					}
					batch = append(batch, c...)
					recycle(c)
				default:
					goto flush
				}
			}
		}
	flush:
		p.flushPTYBatch(batch)
		lastFlush = time.Now()
	}
}

// flushPTYBatch taps OSC state out of the chunk and writes it to the
// emulator, then notifies the TUI once.
func (p *Pane) flushPTYBatch(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	p.mu.Lock()
	for _, e := range p.tap.observe(chunk) {
		switch e[0] {
		case "0", "2":
			p.oscTitle = e[1]
		case "9":
			p.oscProgress = e[1]
		}
	}
	p.writeRecorded(chunk)
	benchstage.Output(p.ID, chunk)
	clipboard := p.clipboard
	p.clipboard = nil
	p.scrollbarVisible = p.emu.ScrollbackLen() > 0
	p.mu.Unlock()
	p.revision.Add(1)
	p.markProcessOutput()
	p.notifyChanged()
	for _, data := range clipboard {
		select {
		case p.ev <- Event{Pane: p.ID, Kind: EventClipboard, Data: data}:
		case <-p.done:
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
	defer p.endProcessWatch()
	defer close(p.done)
	_ = p.cmd.Wait()
	if p.retired.Load() {
		return
	}
	p.mu.Lock()
	p.dead = true
	p.mu.Unlock()
	if p.guard != nil {
		_ = p.guard.Close()
	}
	p.notify(EventExited)
}

func (p *Pane) notify(k EventKind) {
	select {
	case p.ev <- Event{Pane: p.ID, Kind: k}:
	default:
		if k == EventChanged || k == EventExited {
			p.keepMissedChange(k)
		}
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
		p.keepMissedChange(EventChanged)
		p.changedPending.Store(false)
	}
}

// MarkChangedRead clears the pending EventChanged flag. The TUI calls it
// when it consumes the event, allowing the next screen change to notify
// again.
func (p *Pane) MarkChangedRead() {
	p.clearMissedChange()
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
	if p.guard != nil {
		_ = p.guard.Close()
	}

	if p.ptyFile != nil {
		p.closePTY() // SIGHUP to foreground group
	}
	p.killProcess()
	_ = p.emu.Close()
}

// Input writes raw key/paste bytes to the pane's PTY.
func (s *Supervisor) Input(id int, b []byte) {
	if p := s.Get(id); p != nil {
		p.RecordExternalInput()
		_, _ = p.WriteToPTY(b)
	}
}

// WriteToPTY writes all of b to the pane's PTY, looping over short
// writes. It returns the number of bytes written; a short count means
// delivery did not complete (callers keep the message queued).
func (p *Pane) WriteToPTY(b []byte) (int, error) {
	defer p.markProcessChanged()
	p.mu.Lock()
	f := p.ptyFile
	p.mu.Unlock()
	if f == nil {
		return 0, os.ErrClosed
	}
	total := 0
	for total < len(b) {
		n, err := f.Write(b[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// BracketedPasteMode reports whether the pane's app negotiated bracketed
// paste (input must be wrapped in paste markers).
func (p *Pane) BracketedPasteMode() bool {
	on := false
	p.WithEmu(func(e *vt.Emulator) {
		on = e.ModeSet(ansi.ModeBracketedPaste)
	})
	return on
}

// SendMouse encodes a mouse event in the mode the app negotiated and feeds it
// to the PTY through the emulator's response channel.
func (p *Pane) SendMouse(m uv.MouseEvent) {
	p.RecordExternalInput()
	p.mu.Lock()
	p.emu.SendMouse(m)
	p.mu.Unlock()
}

// MousePixelMode reports whether the app enabled SGR pixel mouse mode
// (DEC 1016): callers must then deliver pixel coordinates (SPEC G6).
func (p *Pane) MousePixelMode() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.emu.ModeSet(ansi.ModeMouseExtSgrPixel)
}

// KbdExplicit reports whether the app pushed an explicit kitty keyboard
// protocol request; the FixTerm Shift+Enter exception then does not apply
// (SPEC G6).
func (p *Pane) KbdExplicit() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.emu.KbdExplicit()
}

// Resize updates the emulator and PTY window size (SIGWINCH to the child).
func (s *Supervisor) Resize(id, cols, rows int) {
	s.ResizeWithPixels(id, cols, rows, 0, 0)
}

// ResizeWithPixels supplies the pane's character geometry through its PTY.
func (s *Supervisor) ResizeWithPixels(id, cols, rows, cw, ch int) {
	defer benchstage.Span("runtime.resize")()
	p := s.Get(id)
	if p == nil || cols <= 0 || rows <= 0 {
		return
	}
	p.mu.Lock()
	charactersChanged := p.emu.Width() != cols || p.emu.Height() != rows
	if !charactersChanged && p.cellWidth == cw && p.cellHeight == ch {
		p.mu.Unlock()
		return
	}
	p.cellWidth, p.cellHeight = cw, ch
	if charactersChanged {
		p.resizeTerminal(cols, rows)
		if p.read != nil {
			p.read.resize(cols, rows)
		}
	} else {
		// Pixel reports still change without resetting terminal tab stops
		// and scroll margins at unchanged character dimensions.
		p.resizeTerminal(cols, rows)
	}
	p.mu.Unlock()
	_ = unix.IoctlSetWinsize(p.ptyFD, unix.TIOCSWINSZ, &unix.Winsize{Row: uint16(rows), Col: uint16(cols), Xpixel: uint16(cols * cw), Ypixel: uint16(rows * ch)})
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
		Revision:         p.revision.Load(),
		ProcName:         p.procName,
		AgentID:          p.effectiveAgent(),
		FgPID:            p.fgPID,
		Cwd:              p.procCwdOrDefault(),
		OSCTitle:         p.oscTitle,
		OSCProgress:      p.oscProgress,
		AltScreen:        p.altScreen,
		MouseTracking:    p.mouseTracking,
		MouseMotion:      p.mouseMotion,
		AppCursorKeys:    p.appCursorKeys,
		Cursor:           p.cursor,
		ScrollbackLen:    p.emu.ScrollbackLen(),
		ScrollbarVisible: p.scrollbarVisible,
		Dead:             p.dead,
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
// BufferLines returns the retained buffer (scrollback rows first, then
// the screen) as cell rows for the copy-motion/search contract.
func (p *Pane) BufferLines() [][]uv.Cell {
	p.mu.Lock()
	defer p.mu.Unlock()
	sb := p.emu.ScrollbackLen()
	h := p.emu.Height()
	w := p.emu.Width()
	out := make([][]uv.Cell, sb+h)
	for y := 0; y < sb+h; y++ {
		row := make([]uv.Cell, w)
		for x := 0; x < w; x++ {
			var c *uv.Cell
			if y < sb {
				c = p.emu.ScrollbackCellAt(x, y)
			} else {
				c = p.emu.CellAt(x, y-sb)
			}
			if c != nil {
				row[x] = *c
			} else {
				row[x] = uv.Cell{Content: " ", Width: 1}
			}
		}
		out[y] = row
	}
	return out
}

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
	defer benchstage.Span("runtime.pollprocess")()
	if p.ptyFile == nil {
		return
	}
	p.processChanged.Store(false)
	if p.processIdentityChanged.Swap(false) {
		p.processCache.mu.Lock()
		p.processCache.readAt = time.Time{}
		p.processCache.mu.Unlock()
	}
	job := p.cachedForegroundJob()
	p.setForegroundSudo(job.Sudo)
	pids := make([]int, 0, len(job.Processes))
	for _, pr := range job.Processes {
		pids = append(pids, pr.PID)
	}
	p.watchProcessChanges(job.ProcessGroupID, pids...)
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
	agentChanged := p.fgPID != job.ProcessGroupID || p.procName != name || p.agentID != agentID
	cwdChanged := cwd != "" && cwd != p.procCwd
	p.processCwdAt = time.Now()
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
	effective := p.effectiveAgent()
	if effective == "" && p.guardStarted != "" {
		effective = p.guardStarted
	}
	dead := p.dead
	if agentChanged || cwdChanged {
		p.processRevision.Add(1)
	}
	p.mu.Unlock()
	if p.guard != nil && !dead && (agentChanged || effective != "") {
		_ = p.guard.Sync(effective)
	}
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
		if p.guard != nil {
			_ = p.guard.Close()
		}
		if p.ptyFile != nil {
			p.closePTY()
		}
		p.killProcess()
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

// SpawnArgv returns the command argv the pane was spawned with, or nil
// for the default login shell.
func (p *Pane) SpawnArgv() []string { return p.spawnArgv }

// ForegroundJob returns the pane's foreground process group, best effort.
func (p *Pane) ForegroundJob() platform.ForegroundJob {
	if p.ptyFile == nil {
		return platform.ForegroundJob{}
	}
	return p.cachedForegroundJob()
}

// SetShell applies to subsequent interactive shells only.
func (s *Supervisor) SetShell(shell, mode string) {
	s.mu.Lock()
	s.defaultShell, s.shellMode = shell, mode
	s.mu.Unlock()
}

// PollProcessChanged observes native process notifications and PTY activity.
// Explicit API reads still use PollProcess for immediate freshness.
func (p *Pane) PollProcessChanged() {
	defer benchstage.Span("runtime.process.check")()
	p.mu.Lock()
	guarded := p.guard != nil && (p.effectiveAgent() != "" || p.guardStarted != "")
	effective, dead := p.effectiveAgent(), p.dead
	if effective == "" {
		effective = p.guardStarted
	}
	now := time.Now()
	settling := now.Before(p.processSettlingUntil)
	settled := !p.processSettlingUntil.IsZero() && !settling
	if settled {
		p.processSettlingUntil = time.Time{}
		p.processIdentityChanged.Store(true)
	}
	output := !p.processOutputUntil.IsZero()
	if output && !now.Before(p.processOutputUntil) {
		p.processOutputUntil = time.Time{}
	}
	p.mu.Unlock()
	if !processEventQueue() || settling || settled || p.processChanged.Load() {
		p.PollProcess()
		return
	}
	if guarded && !dead {
		_ = p.guard.Sync(effective)
	}
	// kqueue has no chdir/tcsetpgrp event. Silent programs keep the existing
	// cwd-observation cadence. Output keeps the same per-tick observation
	// window, but only an observed change needs the full PID/argv scan.
	p.mu.Lock()
	due := output || now.Sub(p.processCwdAt) >= foregroundJobCacheLifetime
	if due {
		p.processCwdAt = now
	}
	p.mu.Unlock()
	if due {
		group := platform.ForegroundPID(p.ptyFD)
		cwd := platform.ProcCwd(group)
		p.mu.Lock()
		changed := group != p.fgPID || cwd != "" && cwd != p.procCwd
		p.mu.Unlock()
		if changed {
			p.PollProcess()
			p.notifyChanged()
		}
	}
}

func (p *Pane) markProcessOutput() {
	p.mu.Lock()
	p.processOutputUntil = time.Now().Add(foregroundJobCacheLifetime)
	p.mu.Unlock()
	p.queueProcessChange()
}

// fork/exec notification precedes tcsetpgrp and shell builtins can finish after
// the input write. Observe that transition over the existing cache lifetime.
func (p *Pane) markProcessChanged() {
	p.mu.Lock()
	p.processSettlingUntil = time.Now().Add(foregroundJobCacheLifetime)
	p.mu.Unlock()
	p.processChanged.Store(true)
	p.queueProcessChange()
}

// ForegroundSudo reads the result of the existing foreground job scan.
func (p *Pane) ForegroundSudo() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.foregroundSudo && !p.dead
}

func (p *Pane) setForegroundSudo(active bool) {
	p.mu.Lock()
	changed := p.foregroundSudo != active
	p.foregroundSudo = active
	if changed {
		p.processRevision.Add(1)
	}
	p.mu.Unlock()
	if changed {
		p.notify(EventChanged)
	}
}
