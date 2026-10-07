// Package tui runs the outer terminal: raw mode, input framing, the
// command/effect loop against model.AppState, and frame rendering.
//
// The App itself is transport-agnostic: it consumes raw input bytes and
// resize events from channels and writes encoded frames to an io.Writer.
// Run wires it to the local terminal; the server wires it to the attach
// socket instead, which keeps the session alive while clients come and go.
package tui

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/x/vt"
	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"github.com/kazuph/goterrace/internal/detect"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/platform"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/runtime"
	"github.com/kazuph/goterrace/internal/status"
)

// spinnerFrames rotate for working agents (150ms per frame).
var spinnerFrames = []string{"◐", "◓", "◑", "◒"}

const (
	drawMinInterval = 16 * time.Millisecond
	spinInterval    = 150 * time.Millisecond
	copiedDuration  = 2 * time.Second
	tickInterval    = 50 * time.Millisecond
	drainMs         = 100 * time.Millisecond
)

const (
	// EnterSeq puts the terminal into the app modes.
	EnterSeq = "\x1b[?1049h" + // alternate screen
		"\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1006h" + // SGR mouse + motion
		"\x1b[?1004h" + // focus reporting
		"\x1b[?2004h" // bracketed paste
	// MouseOffSeq disables mouse reporting before LeaveSeq so queued mouse
	// bytes can be flushed before the outer shell sees them (fix #6).
	MouseOffSeq = "\x1b[?1006l\x1b[?1003l\x1b[?1002l\x1b[?1000l"
	// LeaveSeq restores the terminal.
	LeaveSeq = "\x1b[?1004l" + "\x1b[?2004l" + "\x1b[?25h" + "\x1b[?1049l"
)

type menuKind int

const (
	menuPane menuKind = iota
	menuWorkspace
	menuMain
	menuTab
)

// uiMenu is an open popup menu (TUI-owned state).
type uiMenu struct {
	m    render.Menu
	kind menuKind
	pane int // target pane (menuPane)
	ws   int // target workspace (menuWorkspace, menuTab)
	tab  int // target tab (menuTab)
	// justOpened suppresses the release event from the same click that
	// opened the menu (the menu may clamp onto the pointer).
	justOpened bool
}

// uiDialog is an open confirmation dialog; input != nil turns the detail
// row into a single-line text field (run sees the entered value).
type uiDialog struct {
	d     render.Dialog
	run   func(a *App) // confirm action
	input *string      // text-entry mode when non-nil
}

// sepDrag tracks a split-border ratio drag.
type sepDrag struct {
	path []bool
	dir  layout.Direction
	area layout.Rect
	l    *layout.Layout
}

// selState is an in-progress mouse selection inside a pane, expressed in
// combined-buffer coordinates (scrollback + screen lines).
type selState struct {
	pane     int
	ax, ay   int // anchor
	hx, hy   int // head
	moved    bool
	inner    layout.Rect // pane inner rect at drag start
	scrollAt int         // scroll offset at drag start
}

// appJob runs fn on the app loop goroutine and signals completion.
type appJob struct {
	fn   func()
	done chan struct{}
}

// App is the whole TUI: model state, supervisor, input framing, and
// rendering. External inputs (the API server, a client socket) submit work
// through Do/PushInput/Resize so all state changes stay on the loop
// goroutine.
type App struct {
	st  *model.AppState
	sup *runtime.Supervisor
	out io.Writer
	fr  *input.Framer
	enc *render.Encoder
	cwd string

	input    chan []byte
	resizeCh chan winsize
	jobs     chan appJob
	doneCh   chan struct{}

	inputEOF       bool                // input close means quit (local mode)
	detacher       func(reason string) // server mode: detach the client
	sinkOn         func() bool         // nil = always draw
	quitHook       func()              // runs on the loop right before Run returns
	availHook      func(pid int)       // pane status changed; server flushes mail
	changeHook     func()              // persistent state changed; server coalesces saves
	restartHook    func()              // main menu "Restart server…" (server mode only)
	restoreHook    func(dryRun bool)   // main menu "Restore agents…" (server mode only)
	paneClosedHook func(pid int)       // pane permanently removed; server cleans ledger

	prefix        bool
	navKey        rune // prefix+<navKey> jumps to the toast target
	toast         *toastView
	toastRect     layout.Rect
	menu          *uiMenu
	dialog        *uiDialog
	sep           *sepDrag
	sel           *selState
	tabHits       []layout.Rect // last frame's tab strip hit areas
	newTabRect    layout.Rect   // last frame's "+" button
	fwd           int           // pane receiving forwarded mouse events during a drag
	scroll        map[int]int
	trackers      map[int]*status.Tracker
	reported      map[int]reportedPane // pane.report_agent overrides
	sessions      map[int]*AgentRecord // persisted agent-session record per pane (SPEC G3)
	sidebarScroll int
	copied        string
	copiedAt      time.Time
	spin          int
	lastSpin      time.Time
	lastDraw      time.Time
	dirty         bool
	quit          bool
}

// NewApp creates an App over existing state and supervisor. out is the
// display sink: the local terminal, or the server's attached client.
func NewApp(st *model.AppState, sup *runtime.Supervisor, out io.Writer, cwd string) *App {
	return &App{
		st:       st,
		sup:      sup,
		out:      out,
		fr:       &input.Framer{},
		enc:      &render.Encoder{},
		cwd:      cwd,
		input:    make(chan []byte, 256),
		resizeCh: make(chan winsize, 8),
		jobs:     make(chan appJob),
		doneCh:   make(chan struct{}),
		scroll:   map[int]int{},
		trackers: map[int]*status.Tracker{},
		fwd:      -1,
	}
}

// State returns the application state. Callers must only touch it inside
// Do; it is exposed for the server package.
func (a *App) State() *model.AppState { return a.st }

// Supervisor returns the pane supervisor.
func (a *App) Supervisor() *runtime.Supervisor { return a.sup }

// SetDetacher registers the detach hook for prefix `d`. nil keeps `d`
// unbound (in-process mode).
func (a *App) SetDetacher(f func(reason string)) { a.detacher = f }

// SetSinkGate registers a predicate consulted before composing a frame.
// The server uses it to skip rendering while no client is attached.
func (a *App) SetSinkGate(f func() bool) { a.sinkOn = f }

// SetQuitHook registers a callback invoked on the loop just before Run
// returns (e.g. to send the client a final exit message).
func (a *App) SetQuitHook(f func()) { a.quitHook = f }

// SetAvailHook registers a callback invoked on the app loop whenever a
// pane's agent status changes (detection or report). The server uses it
// for event-driven mail delivery.
func (a *App) SetAvailHook(f func(pid int)) { a.availHook = f }

// Do runs fn on the app loop goroutine and blocks until it completes. It
// returns without running fn if the app has already stopped.
func (a *App) Do(fn func()) {
	done := make(chan struct{})
	select {
	case a.jobs <- appJob{fn: fn, done: done}:
		<-done
	case <-a.doneCh:
	}
}

// PushInput feeds raw terminal input bytes to the framer.
func (a *App) PushInput(b []byte) {
	select {
	case a.input <- b:
	case <-a.doneCh:
	}
}

// CloseInput closes the input channel; in local mode that quits the app.
func (a *App) CloseInput() { close(a.input) }

// Resize records a new outer terminal size.
func (a *App) Resize(cols, rows int) {
	select {
	case a.resizeCh <- winsize{Cols: cols, Rows: rows}:
	case <-a.doneCh:
	}
}

// Stop requests the event loop to exit.
func (a *App) Stop() {
	a.Do(func() { a.exec(model.CmdQuit{}) })
}

// ExecCmd applies a model command and its effects. It must be called on
// the app loop goroutine (from inside Do) or before Run starts.
func (a *App) ExecCmd(cmd model.Command) { a.exec(cmd) }

// StartInitialPane spawns the shell for the workspace created by
// model.New, sized to its inner rect.
func (a *App) StartInitialPane() error {
	if len(a.st.Workspaces) == 0 {
		return nil
	}
	pid := a.st.Workspaces[0].Tabs[0].Layout.Focused()
	r := a.innerRect(pid)
	_, err := a.sup.Spawn(pid, a.cwd, r.W, r.H)
	return err
}

// Run executes the event loop until quit. It returns after the model
// signals EffQuit (last pane closed, quit dialog, or API stop).
func (a *App) Run() {
	defer close(a.doneCh)
	tick := time.NewTicker(tickInterval)
	defer tick.Stop()
	var flush <-chan time.Time
	var flushTimer *time.Timer
	defer func() {
		if flushTimer != nil {
			flushTimer.Stop()
		}
	}()

	a.dirty = true
	for !a.quit {
		select {
		case b, ok := <-a.input:
			if !ok {
				a.input = nil
				if a.inputEOF {
					a.exec(model.CmdQuit{})
				}
				continue
			}
			for _, ev := range a.fr.Push(b) {
				a.onInput(ev)
			}
			if a.fr.Pending() {
				if flushTimer == nil {
					flushTimer = time.NewTimer(time.Duration(input.IdleFlushMs) * time.Millisecond)
					flush = flushTimer.C
				} else {
					flushTimer.Reset(time.Duration(input.IdleFlushMs) * time.Millisecond)
				}
			} else if flushTimer != nil {
				flushTimer.Stop()
				flush = nil
			}
		case <-flush:
			for _, ev := range a.fr.FlushTimeout() {
				a.onInput(ev)
			}
			if a.fr.Pending() {
				flushTimer.Reset(time.Duration(input.IdleFlushMs) * time.Millisecond)
			} else {
				flush = nil
			}
		case j := <-a.jobs:
			j.fn()
			close(j.done)
		case ev := <-a.sup.Events():
			a.onRuntimeEvent(ev)
		case sz := <-a.resizeCh:
			if sz.Cols != a.st.Cols || sz.Rows != a.st.Rows {
				a.exec(model.CmdResize{Cols: sz.Cols, Rows: sz.Rows})
				a.enc.Reset()
			}
		case <-tick.C:
			a.onTick()
		}
		if a.quit {
			break
		}
		if a.dirty && time.Since(a.lastDraw) >= drawMinInterval {
			a.draw()
		}
	}
	if a.quitHook != nil {
		a.quitHook()
	}
}

// Run starts the TUI on the local terminal and blocks until quit. Returns
// an exit code.
func Run(in *os.File, out *os.File) (int, error) {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = os.Getenv("HOME")
	}
	old, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return 1, fmt.Errorf("raw mode: %w", err)
	}
	restored := false
	restore := func() {
		if !restored {
			_ = term.Restore(int(in.Fd()), old)
			restored = true
		}
	}
	defer restore()

	ws, err := GetWinsize(in)
	if err != nil {
		return 1, err
	}
	if _, err := out.WriteString(EnterSeq); err != nil {
		return 1, err
	}
	leave := func() {
		// Fix #6: stop mouse reporting first, flush queued input so the
		// outer shell never sees leftover mouse bytes, then restore modes.
		_, _ = out.WriteString(MouseOffSeq)
		platform.FlushInput(int(in.Fd()))
		DrainInput(in, drainMs)
		_, _ = out.WriteString(LeaveSeq)
		restore()
	}
	defer leave()

	st := model.New(cwd, ws.Cols, ws.Rows)
	sup := runtime.NewSupervisor()
	a := NewApp(st, sup, out, cwd)
	a.inputEOF = true

	if err := a.StartInitialPane(); err != nil {
		return 1, fmt.Errorf("spawn pane: %w", err)
	}

	go readInput(in, a.input)
	winch := make(chan os.Signal, 4)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			if sz, err := GetWinsize(in); err == nil {
				a.Resize(sz.Cols, sz.Rows)
			}
		}
	}()

	a.Run()
	sup.KillAll()
	return 0, nil
}

// onInput routes a framed input event. Unknown fragments are dropped —
// never forwarded to a pane (fix #3).
func (a *App) onInput(ev input.Event) {
	switch ev.Kind {
	case input.KindKey:
		a.handleKey(ev.Key, ev.Raw)
	case input.KindPaste:
		if pid := a.focusedPane(); pid >= 0 {
			if p := a.sup.Get(pid); p != nil {
				p.WithEmu(func(e *vt.Emulator) { e.Paste(ev.Text) })
			}
		}
	case input.KindMouseClick, input.KindMouseRelease, input.KindMouseWheel, input.KindMouseMotion:
		a.onMouse(ev)
	case input.KindFocus, input.KindBlur, input.KindUnknown:
		// Dropped on purpose.
	}
}

// onRuntimeEvent handles supervisor notifications.
func (a *App) onRuntimeEvent(ev runtime.Event) {
	switch ev.Kind {
	case runtime.EventChanged:
		if p := a.sup.Get(ev.Pane); p != nil {
			p.MarkChangedRead()
		}
		a.dirty = true
	case runtime.EventExited:
		a.exec(model.CmdPaneExited{Pane: ev.Pane})
	}
}

// detectAll runs the detection/status pass over every live pane.
func (a *App) detectAll() {
	now := time.Now()
	activeWS := a.st.ActiveWorkspace()
	for _, ws := range a.st.Workspaces {
		focusedID := -1
		if l := ws.ActiveLayout(); l != nil {
			focusedID = l.Focused()
		}
		for _, t := range ws.Tabs {
			for _, pid := range t.Layout.PaneIDs() {
				p := a.sup.Get(pid)
				if p == nil {
					continue
				}
				tr := a.trackers[pid]
				if tr == nil {
					tr = status.NewTracker()
					a.trackers[pid] = tr
				}
				sn := p.Snapshot()
				agentID := sn.AgentID
				in := detect.Input{OSCTitle: sn.OSCTitle, OSCProgress: sn.OSCProgress}
				if agentID != "" {
					in.Screen = p.ScreenText()
				}
				if tr.Update(detect.Detect(agentID, in), agentID, sn.Dead, now) {
					a.dirty = true
					if a.availHook != nil {
						a.availHook(pid)
					}
				}
				// The focused pane in the visible workspace is seen.
				if ws == activeWS && pid == focusedID {
					if !tr.Seen {
						tr.Focus()
						a.dirty = true
					}
				}
			}
		}
	}
	// Drop trackers for panes that no longer exist.
	for pid := range a.trackers {
		if a.sup.Get(pid) == nil {
			delete(a.trackers, pid)
			delete(a.reported, pid)
		}
	}
}

// onTick handles periodic work: spinner animation, copied-flash expiry,
// foreground-process polling, and detection.
func (a *App) onTick() {
	a.expireToast()
	if a.copied != "" && time.Since(a.copiedAt) > copiedDuration {
		a.copied = ""
		a.dirty = true
	}
	if time.Since(a.lastSpin) >= spinInterval {
		a.spin++
		a.lastSpin = time.Now()
		if a.anyWorking() {
			a.dirty = true
		}
	}
	// Foreground process poll: every ~1s per pane.
	for _, ws := range a.st.Workspaces {
		for _, t := range ws.Tabs {
			for _, pid := range t.Layout.PaneIDs() {
				if p := a.sup.Get(pid); p != nil {
					p.PollProcess()
				}
			}
		}
	}
	a.detectAll()
}

func (a *App) anyWorking() bool {
	for _, ws := range a.st.Workspaces {
		for _, t := range ws.Tabs {
			for _, pid := range t.Layout.PaneIDs() {
				if g := a.glyph(pid); g.Key == "working" {
					return true
				}
			}
		}
	}
	return false
}

// exec applies a command and performs its effects.
func (a *App) exec(cmd model.Command) {
	for _, e := range a.st.Apply(cmd) {
		a.effect(e)
	}
	a.dirty = true
	// Keystrokes and resizes never change persisted state; everything else
	// (splits, closes, focus, renames, tabs, workspaces) schedules a save.
	switch cmd.(type) {
	case model.CmdInput, model.CmdResize:
	default:
		a.changed()
	}
}

func (a *App) effect(e model.Effect) {
	switch e := e.(type) {
	case model.EffSpawnPane:
		r := a.rectForPane(e.Pane)
		var env []string
		for k, v := range e.Env {
			env = append(env, k+"="+v)
		}
		_, _ = a.sup.SpawnWith(e.Pane, e.Cwd, r.W, r.H, env)
	case model.EffKillPane:
		a.sup.Kill(e.Pane)
		delete(a.scroll, e.Pane)
		delete(a.reported, e.Pane)
		delete(a.sessions, e.Pane)
		if a.paneClosedHook != nil {
			a.paneClosedHook(e.Pane)
		}
	case model.EffSyncLayout:
		a.syncLayout()
	case model.EffInput:
		a.sup.Input(e.Pane, e.Bytes)
	case model.EffQuit:
		a.quit = true
	}
}

// syncLayout resizes every pane of the active workspace to its inner rect.
func (a *App) syncLayout() {
	ws := a.st.ActiveWorkspace()
	if ws == nil {
		return
	}
	area := a.st.PanesArea()
	if l := ws.ActiveLayout(); l != nil {
		for _, pr := range l.Panes(area) {
			a.sup.Resize(pr.ID, max(pr.Rect.W-2, 1), max(pr.Rect.H-2, 1))
		}
	}
}

// rectForPane returns the inner rect of a pane in whichever workspace owns
// it; panes of a hidden workspace spawn at a default size and are resized
// when their workspace becomes active.
func (a *App) rectForPane(pid int) layout.Rect {
	for _, ws := range a.st.Workspaces {
		for _, t := range ws.Tabs {
			for _, pr := range t.Layout.Panes(a.st.PanesArea()) {
				if pr.ID == pid {
					return layout.Rect{X: pr.Rect.X + 1, Y: pr.Rect.Y + 1, W: max(pr.Rect.W-2, 1), H: max(pr.Rect.H-2, 1)}
				}
			}
		}
	}
	return layout.Rect{W: 80, H: 24}
}

// innerRect returns the content rect (inside the border) of a pane of the
// active workspace.
func (a *App) innerRect(pid int) layout.Rect {
	ws := a.st.ActiveWorkspace()
	if ws == nil {
		return layout.Rect{W: 80, H: 24}
	}
	l := ws.ActiveLayout()
	if l == nil {
		return layout.Rect{W: 80, H: 24}
	}
	for _, pr := range l.Panes(a.st.PanesArea()) {
		if pr.ID == pid {
			return layout.Rect{X: pr.Rect.X + 1, Y: pr.Rect.Y + 1, W: max(pr.Rect.W-2, 1), H: max(pr.Rect.H-2, 1)}
		}
	}
	return layout.Rect{W: 80, H: 24}
}

// paneRect returns the outer (bordered) rect of a pane of the active ws.
func (a *App) paneRect(pid int) (layout.Rect, bool) {
	ws := a.st.ActiveWorkspace()
	if ws == nil {
		return layout.Rect{}, false
	}
	l := ws.ActiveLayout()
	if l == nil {
		return layout.Rect{}, false
	}
	for _, pr := range l.Panes(a.st.PanesArea()) {
		if pr.ID == pid {
			return pr.Rect, true
		}
	}
	return layout.Rect{}, false
}

// focusedPane returns the focused pane id of the active workspace.
func (a *App) focusedPane() int {
	return a.st.FocusedPane()
}

// glyph returns the pane's display status glyph.
func (a *App) glyph(pid int) render.StatusGlyph {
	tr := a.trackers[pid]
	if tr == nil {
		return render.StatusGlyph{Key: "unknown", Rune: "·", Color: render.Dim}
	}
	key := tr.Key()
	if r, ok := a.reported[pid]; ok && r.state != "" {
		key = r.state
	}
	switch key {
	case "working":
		return render.StatusGlyph{Key: "working", Rune: "◐", Color: render.Yellow}
	case "blocked":
		return render.StatusGlyph{Key: "blocked", Rune: "▲", Color: render.Peach}
	case "done":
		return render.StatusGlyph{Key: "done", Rune: "●", Color: render.Green}
	case "idle":
		return render.StatusGlyph{Key: "idle", Rune: "○", Color: render.Dim}
	}
	return render.StatusGlyph{Key: "unknown", Rune: "·", Color: render.Dim}
}

// draw composes and writes one frame.
func (a *App) draw() {
	if a.sinkOn != nil && !a.sinkOn() {
		// No display attached: drop the frame work but clear the flag so a
		// later attach gets a full repaint via the resize/reset path.
		a.dirty = false
		a.lastDraw = time.Now()
		return
	}
	in := render.Input{
		State:         a.st,
		Panes:         a.paneSnaps(),
		Scroll:        a.scroll,
		Spin:          a.spin,
		Copied:        a.copied,
		Prefix:        a.prefix,
		SidebarScroll: a.sidebarScroll,
	}
	if a.menu != nil {
		in.Menu = &a.menu.m
	}
	if a.dialog != nil {
		in.Dialog = &a.dialog.d
	}
	if a.sel != nil && a.sel.moved {
		in.Sel = a.selRender()
	}
	if a.toast != nil {
		in.Toast = &render.Toast{Title: a.toast.title, Body: a.toast.body, Kind: a.toast.kind}
	}
	out := render.Compose(in)
	a.tabHits = out.TabHits
	a.newTabRect = out.NewTabRect
	a.toastRect = out.ToastRect
	a.lastDraw = time.Now()
	a.dirty = false
	_, _ = a.out.Write(a.enc.Frame(out.Frame, out.AnchorX, out.AnchorY, out.AnchorVisible))
}

// ResetEncoder drops the diff state so the next frame repaints fully. The
// server calls it when a fresh client attaches.
func (a *App) ResetEncoder() { a.enc.Reset() }

// ScrollOffset returns the client's scrollback offset for a pane.
// Call on the app loop.
func (a *App) ScrollOffset(pid int) int { return a.scroll[pid] }

// PaneStatusKey returns the pane's agent status key
// (working|blocked|done|idle|unknown). Call on the app loop.
func (a *App) PaneStatusKey(pid int) string { return a.glyph(pid).Key }

// SetReported applies a pane.report_agent override. Call on the app loop.
func (a *App) SetReported(pid int, agent, state string) {
	if a.reported == nil {
		a.reported = map[int]reportedPane{}
	}
	prev := a.reported[pid]
	if agent == "" && state == "" {
		delete(a.reported, pid)
	} else {
		a.reported[pid] = reportedPane{agent: agent, state: state}
	}
	if a.availHook != nil {
		cur := a.reported[pid]
		if prev != cur {
			a.availHook(pid)
		}
	}
	a.changed()
}

// Reported returns the API-reported agent override, if any. Call on the
// app loop.
func (a *App) Reported(pid int) (agent, state string, ok bool) {
	r, ok := a.reported[pid]
	return r.agent, r.state, ok
}

// reportedPane holds pane.report_agent overrides.
type reportedPane struct {
	agent string
	state string
}

// paneSnaps builds render snapshots for every live pane.
func (a *App) paneSnaps() map[int]*render.PaneSnap {
	out := map[int]*render.PaneSnap{}
	active := map[int]bool{}
	if ws := a.st.ActiveWorkspace(); ws != nil {
		if l := ws.ActiveLayout(); l != nil {
			for _, id := range l.PaneIDs() {
				active[id] = true
			}
		}
	}
	for _, ws := range a.st.Workspaces {
		for _, t := range ws.Tabs {
			for _, pid := range t.Layout.PaneIDs() {
				p := a.sup.Get(pid)
				if p == nil {
					continue
				}
				sn := p.Snapshot()
				snap := &render.PaneSnap{
					Label:  labelFor(sn, p),
					Agent:  agentFor(sn),
					Status: a.glyph(pid),
					Dead:   sn.Dead,
					Cursor: render.CursorInfo{X: sn.Cursor.X, Y: sn.Cursor.Y, Visible: sn.Cursor.Visible},
				}
				if active[pid] {
					r := a.innerRect(pid)
					snap.Grid = a.paneGrid(p, a.scroll[pid], r.H, r.W)
					snap.ScrollOff = a.scroll[pid]
					snap.ScrollN = sn.ScrollbackLen
				}
				out[pid] = snap
			}
		}
	}
	return out
}

func labelFor(sn runtime.Snapshot, p *runtime.Pane) string {
	if sn.ProcName != "" {
		return sn.ProcName
	}
	return "shell"
}

// agentFor reports the detected agent id for a snapshot.
func agentFor(sn runtime.Snapshot) string {
	return sn.AgentID
}

// --- helpers ---

type winsize struct{ Cols, Rows int }

// GetWinsize reads the terminal size of f.
func GetWinsize(f *os.File) (winsize, error) {
	w, h, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return winsize{120, 40}, err
	}
	return winsize{w, h}, nil
}

// Winsize is the terminal size exported for the client package.
type Winsize = winsize

func readInput(f *os.File, ch chan<- []byte) {
	buf := make([]byte, 64*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			b := make([]byte, n)
			copy(b, buf[:n])
			ch <- b
		}
		if err != nil {
			close(ch)
			return
		}
	}
}

// DrainInput reads and discards leftover input (e.g. mouse reports already
// in flight) for up to d, so the outer shell never sees them.
func DrainInput(f *os.File, d time.Duration) {
	deadline := time.Now().Add(d)
	fd := int(f.Fd())
	pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	buf := make([]byte, 64*1024)
	for {
		remain := time.Until(deadline)
		if remain <= 0 {
			return
		}
		ms := int(remain / time.Millisecond)
		if ms < 1 {
			ms = 1
		}
		n, err := unix.Poll(pfd, ms)
		if err != nil || n == 0 {
			return
		}
		if _, err := f.Read(buf); err != nil {
			return
		}
	}
}

// pointIn reports whether (x,y) is inside r.
func pointIn(x, y int, r layout.Rect) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}
