// Package tui runs the outer terminal: raw mode, input framing, the
// command/effect loop against model.AppState, and frame rendering.
package tui

import (
	"fmt"
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
	enterSeq = "\x1b[?1049h" + // alternate screen
		"\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1006h" + // SGR mouse + motion
		"\x1b[?1004h" + // focus reporting
		"\x1b[?2004h" // bracketed paste
	mouseOffSeq = "\x1b[?1006l\x1b[?1003l\x1b[?1002l\x1b[?1000l"
	leaveSeq    = "\x1b[?1004l" + "\x1b[?2004l" + "\x1b[?25h" + "\x1b[?1049l"
)

type menuKind int

const (
	menuPane menuKind = iota
	menuWorkspace
	menuMain
)

// uiMenu is an open popup menu (TUI-owned state).
type uiMenu struct {
	m    render.Menu
	kind menuKind
	pane int // target pane (menuPane)
	ws   int // target workspace (menuWorkspace)
	// justOpened suppresses the release event from the same click that
	// opened the menu (the menu may clamp onto the pointer).
	justOpened bool
}

// uiDialog is an open confirmation dialog.
type uiDialog struct {
	d   render.Dialog
	run func(a *App) // confirm action
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

// App is the whole TUI.
type App struct {
	st  *model.AppState
	sup *runtime.Supervisor
	out *os.File
	fr  *input.Framer
	enc *render.Encoder
	cwd string

	prefix        bool
	menu          *uiMenu
	dialog        *uiDialog
	sep           *sepDrag
	sel           *selState
	fwd           int // pane receiving forwarded mouse events during a drag
	scroll        map[int]int
	trackers      map[int]*status.Tracker
	sidebarScroll int
	copied        string
	copiedAt      time.Time
	spin          int
	lastSpin      time.Time
	lastDraw      time.Time
	dirty         bool
	quit          bool
}

// Run starts the TUI and blocks until quit. Returns an exit code.
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

	ws, err := getWinsize(in)
	if err != nil {
		return 1, err
	}
	if _, err := out.WriteString(enterSeq); err != nil {
		return 1, err
	}
	leave := func() {
		// Fix #6: stop mouse reporting first, flush queued input so the
		// outer shell never sees leftover mouse bytes, then restore modes.
		_, _ = out.WriteString(mouseOffSeq)
		platform.FlushInput(int(in.Fd()))
		drainInput(in, drainMs)
		_, _ = out.WriteString(leaveSeq)
		restore()
	}
	defer leave()

	st := model.New(cwd, ws.Cols, ws.Rows)
	sup := runtime.NewSupervisor()
	a := &App{
		st:       st,
		sup:      sup,
		out:      out,
		fr:       &input.Framer{},
		enc:      &render.Encoder{},
		cwd:      cwd,
		scroll:   map[int]int{},
		trackers: map[int]*status.Tracker{},
		fwd:      -1,
	}

	// Spawn the initial pane sized to its inner rect.
	pid := st.Workspaces[0].Layout.Focused()
	r := a.innerRect(pid)
	if _, err := sup.Spawn(pid, cwd, r.W, r.H); err != nil {
		return 1, fmt.Errorf("spawn pane: %w", err)
	}

	inCh := make(chan []byte, 64)
	go readInput(in, inCh)
	winch := make(chan os.Signal, 4)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)

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
		case b, ok := <-inCh:
			if !ok {
				a.exec(model.CmdQuit{})
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
		case ev := <-sup.Events():
			a.onRuntimeEvent(ev)
		case <-winch:
			if sz, err := getWinsize(in); err == nil && (sz.Cols != st.Cols || sz.Rows != st.Rows) {
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
		for _, pid := range ws.Layout.PaneIDs() {
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
			agentID := detect.Identify(sn.ProcName, sn.ProcArgv0)
			in := detect.Input{OSCTitle: sn.OSCTitle, OSCProgress: sn.OSCProgress}
			if agentID != "" {
				in.Screen = p.ScreenText()
			}
			if tr.Update(detect.Detect(agentID, in), agentID, sn.Dead, now) {
				a.dirty = true
			}
			// The focused pane in the visible workspace is seen.
			if ws == activeWS && pid == ws.Layout.Focused() {
				if !tr.Seen {
					tr.Focus()
					a.dirty = true
				}
			}
		}
	}
	// Drop trackers for panes that no longer exist.
	for pid := range a.trackers {
		if a.sup.Get(pid) == nil {
			delete(a.trackers, pid)
		}
	}
}

// onTick handles periodic work: spinner animation, copied-flash expiry,
// foreground-process polling, and detection (stub until M6/M7).
func (a *App) onTick() {
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
		for _, pid := range ws.Layout.PaneIDs() {
			if p := a.sup.Get(pid); p != nil {
				p.PollProcess()
			}
		}
	}
	a.detectAll()
}

func (a *App) anyWorking() bool {
	for _, ws := range a.st.Workspaces {
		for _, pid := range ws.Layout.PaneIDs() {
			if g := a.glyph(pid); g.Key == "working" {
				return true
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
}

func (a *App) effect(e model.Effect) {
	switch e := e.(type) {
	case model.EffSpawnPane:
		r := a.innerRect(e.Pane)
		_, _ = a.sup.Spawn(e.Pane, e.Cwd, r.W, r.H)
	case model.EffKillPane:
		a.sup.Kill(e.Pane)
		delete(a.scroll, e.Pane)
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
	for _, pr := range ws.Layout.Panes(area) {
		a.sup.Resize(pr.ID, max(pr.Rect.W-2, 1), max(pr.Rect.H-2, 1))
	}
}

// innerRect returns the content rect (inside the border) of a pane of the
// active workspace.
func (a *App) innerRect(pid int) layout.Rect {
	ws := a.st.ActiveWorkspace()
	if ws == nil {
		return layout.Rect{W: 80, H: 24}
	}
	for _, pr := range ws.Layout.Panes(a.st.PanesArea()) {
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
	for _, pr := range ws.Layout.Panes(a.st.PanesArea()) {
		if pr.ID == pid {
			return pr.Rect, true
		}
	}
	return layout.Rect{}, false
}

// focusedPane returns the focused pane id of the active workspace.
func (a *App) focusedPane() int {
	if ws := a.st.ActiveWorkspace(); ws != nil {
		return ws.Layout.Focused()
	}
	return -1
}

// glyph returns the pane's display status glyph.
func (a *App) glyph(pid int) render.StatusGlyph {
	tr := a.trackers[pid]
	if tr == nil {
		return render.StatusGlyph{Key: "unknown", Rune: "·", Color: render.Dim}
	}
	switch tr.Key() {
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
	out := render.Compose(in)
	a.lastDraw = time.Now()
	a.dirty = false
	_, _ = a.out.Write(a.enc.Frame(out.Frame, out.AnchorX, out.AnchorY, out.AnchorVisible))
}

// paneSnaps builds render snapshots for every live pane.
func (a *App) paneSnaps() map[int]*render.PaneSnap {
	out := map[int]*render.PaneSnap{}
	active := map[int]bool{}
	if ws := a.st.ActiveWorkspace(); ws != nil {
		for _, id := range ws.Layout.PaneIDs() {
			active[id] = true
		}
	}
	for _, ws := range a.st.Workspaces {
		for _, pid := range ws.Layout.PaneIDs() {
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
	return detect.Identify(sn.ProcName, sn.ProcArgv0)
}

// --- helpers ---

type winsize struct{ Cols, Rows int }

func getWinsize(f *os.File) (winsize, error) {
	w, h, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return winsize{120, 40}, err
	}
	return winsize{w, h}, nil
}

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

// drainInput reads and discards leftover input (e.g. mouse reports already
// in flight) for up to d, so the outer shell never sees them.
func drainInput(f *os.File, d time.Duration) {
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
