package tui

import (
	"errors"
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/dispatch"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/platform"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/runtime"
	"github.com/kazuph/goterrace/internal/status"
	"golang.org/x/term"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// spinnerFrames rotate for working agents (150ms per frame).
var spinnerFrames = []string{"◐", "◓", "◑", "◒"}

const (
	drawMinInterval = 16 * time.Millisecond
	spinInterval    = 150 * time.Millisecond
	fossilExpiry    = 15 * time.Second
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
	MouseOffSeq = "\x1b[?1016l\x1b[?1006l\x1b[?1003l\x1b[?1002l\x1b[?1000l"
	// LeaveSeq restores the terminal.
	LeaveSeq = "\x1b[?1004l" + "\x1b[?2004l" + "\x1b[?25h" + "\x1b[?1049l"
)

// KbdEnterSeq enables modifyOtherKeys mode 1 when the host terminal is
// Ghostty so modified keys arrive in the legacy CSI 27;mod;key~ form
// (SPEC G6). Unknown hosts get no protocol enable.
func KbdEnterSeq() string {
	if strings.EqualFold(os.Getenv("TERM_PROGRAM"), "ghostty") {
		return "\x1b[>4;1m"
	}
	return ""
}

// KbdLeaveSeq undoes KbdEnterSeq for Ghostty hosts.
func KbdLeaveSeq() string {
	if strings.EqualFold(os.Getenv("TERM_PROGRAM"), "ghostty") {
		return "\x1b[>4;0m"
	}
	return ""
}

type menuKind int

const (
	menuPane menuKind = iota
	menuWorkspace
	menuMain
	menuTab
	menuWorktree
)

// uiMenu is an open popup menu (TUI-owned state).
type uiMenu struct {
	worktreePaths []string
	m             render.Menu
	kind          menuKind
	pane          int // target pane (menuPane)
	ws            int // target workspace (menuWorkspace, menuTab)
	tab           int // target tab (menuTab)
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

	inputEOF          bool                // input close means quit (local mode)
	detacher          func(reason string) // server mode: detach the client
	sinkOn            func() bool         // nil = always draw
	quitHook          func()              // runs on the loop right before Run returns
	availHook         func(pid int)       // pane status changed; server flushes mail
	changeHook        func()              // persistent state changed; server coalesces saves
	restartHook       func()              // main menu "Restart server…" (server mode only)
	standaloneRestart bool
	popupSel          *selState
	popupCopy         *copyMode
	popupAutoScroll   int
	restoreHook       func(dryRun bool)                              // main menu "Restore agents…" (server mode only)
	paneClosedHook    func(pid int)                                  // pane permanently removed; server cleans ledger
	postExecHook      func(cmd model.Command, prev *model.StateSnap) // after every state-changing command
	exitedHook        func(pid int)                                  // pane process exited; server emits pane.exited
	outHook           func(pid int)                                  // pane output changed; server emits pane.output_changed
	worktreeHook      func(string, WorktreeOutcome, bool)
	warningsHook      func() []api.AgentSessionWarningInfo // restart confirm lists missing ids

	prefix               bool
	navKey               rune // prefix+<navKey> jumps to the toast target
	cm                   *copyMode
	copyKeys             map[rune]bool // [keys] copy_mode bindings
	splitVKeys           map[rune]bool // [keys] split_vertical bindings
	splitHKeys           map[rune]bool // [keys] split_horizontal bindings
	renameKeys           map[rune]bool
	editKeys             map[rune]bool
	historyBackKeys      map[rune]bool
	historyForwardKeys   map[rune]bool
	focusHistory         []int
	focusHistoryAt       int
	traversingHistory    bool
	toast                *toastView
	toastRect            layout.Rect
	menu                 *uiMenu
	dialog               *uiDialog
	sep                  *sepDrag
	sel                  *selState
	tabHits              []layout.Rect // last frame's tab strip hit areas
	newTabRect           layout.Rect   // last frame's "+" button
	sb                   *sbDrag       // scrollbar-gutter drag in progress
	fwd                  int           // pane receiving forwarded mouse events during a drag
	scroll               map[int]int
	trackers             map[int]*status.Tracker
	reported             map[int]reportedPane                     // pane.report_agent overrides
	sessions             map[int]*AgentRecord                     // persisted agent-session record per pane (SPEC G3)
	fossils              map[int]*fossilWatch                     // last live-activity fingerprint per pane (15s expiry)
	visibleIdle          map[int]time.Time                        // pane → when explicit visible-idle detection started
	jobSource            func() ([]dispatch.Job, map[string]bool) // jobs + live-runner ids; server only
	jobDots              map[int][]render.JobDot                  // workspace index → dots (SPEC G2)
	workspaceTokenSource func(int) map[string]string
	workspaceGit         map[int]render.GitStatus
	workspaceNames       map[int]string
	gitAt                time.Time
	gitPending           bool
	jobsAt               time.Time             // last job snapshot
	actionBar            render.ActionBarRects // last frame's action bar hit areas
	uiShowTab            bool                  // [ui] show_tab_bar
	uiHideSingle         bool                  // [ui] hide_tab_bar_when_single_tab
	uiBorders            bool                  // [ui] pane_borders
	sidebarConfig        config.Config
	sidebarGesture       *sidebarGesture
	tabGesture           *tabGesture
	dividerAt            time.Time
	sidebarScroll        int
	agentScroll          int
	sidebarBar           *sidebarScrollDrag
	cellW                int // host cell width in pixels (0 = unknown)
	cellH                int // host cell height in pixels (0 = unknown)
	hostPixelSupported   bool
	hostPixelMouse       bool
	mousePixel           *uv.Mouse // original coordinates for the current host event
	help                 *helpView
	copied               string
	copiedAt             time.Time
	lastClick            clickMark // last left press inside a normal pane body
	dblMenu              bool      // second same-cell down/up pending menu open
	spin                 int
	lastSpin             time.Time
	lastDraw             time.Time
	lastSpawnError       error
	dirty                bool
	quit                 bool
}

// SetCellPixels records the host terminal's pixel geometry so SGR 1016
// panes can be sent cell-center pixel coordinates (SPEC G6).
func (a *App) SetCellPixels(xp, yp int) {
	if a.st.Cols > 0 && xp > 0 {
		if width := xp / a.st.Cols; width > 0 {
			a.cellW = width
		}
	}
	if a.st.Rows > 0 && yp > 0 {
		if height := yp / a.st.Rows; height > 0 {
			a.cellH = height
		}
	}
}

// NewApp creates an App over existing state and supervisor. out is the
// display sink: the local terminal, or the server's attached client.
func NewApp(st *model.AppState, sup *runtime.Supervisor, out io.Writer, cwd string) *App {
	a := &App{
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
		// Default bindings; SetKeybinds overlays the user's [keys] config.
		copyKeys:   config.Config{}.CopyKeys(),
		splitVKeys: config.Config{}.SplitVerticalKeys(),
		splitHKeys: config.Config{}.SplitHorizontalKeys(),
	}
	a.SetPaneKeybinds(config.Config{})
	return a
}

// SetKeybinds applies the [keys] config bindings (copy_mode, split_*).
func (a *App) SetKeybinds(copyKeys, splitV, splitH map[rune]bool) {
	a.copyKeys, a.splitVKeys, a.splitHKeys = copyKeys, splitV, splitH
}

// SetUIFlags applies the [ui] config flags and keeps them so a later
// session restore (which replaces the whole state) re-applies them too.
func (a *App) SetUIFlags(showTabBar, hideSingle, borders bool) {
	a.uiShowTab, a.uiHideSingle, a.uiBorders = showTabBar, hideSingle, borders
	a.applyUIFlags()
}

func (a *App) applyUIFlags() {
	a.st.ShowTabBar = a.uiShowTab
	a.st.HideTabBarWhenSingleTab = a.uiHideSingle
	a.st.PaneBorders = a.uiBorders
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

// SetPostExecHook registers a callback fired after every command with the
// pre-command state snapshot (domain events are derived from the diff).
func (a *App) SetPostExecHook(f func(cmd model.Command, prev *model.StateSnap)) {
	a.postExecHook = f
}

// SetExitedHook registers a callback fired when a pane process exits.
func (a *App) SetExitedHook(f func(pid int)) { a.exitedHook = f }

// SetOutputHook registers a callback fired when a pane's output changes.
func (a *App) SetOutputHook(f func(pid int)) { a.outHook = f }

// SetScrollOffset pins a pane's display to offset_from_bottom lines into
// its scrollback (pane.scroll; 0 follows the live bottom). App loop only.
func (a *App) SetScrollOffset(pid, off int) {
	if p := a.sup.Get(pid); p != nil {
		if mx := p.Snapshot().ScrollbackLen; off > mx {
			off = mx
		}
	}
	if off <= 0 {
		delete(a.scroll, pid)
	} else {
		a.scroll[pid] = off
	}
	a.dirty = true
}

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

// Resize records a new outer terminal size; xp/yp carry the host's
// pixel geometry for SGR 1016 cell-center conversion (0 when unknown).
func (a *App) Resize(cols, rows, xp, yp int) {
	select {
	case a.resizeCh <- winsize{Cols: cols, Rows: rows, Xpixel: xp, Ypixel: yp}:
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
	if err == nil {
		a.sup.ResizeWithPixels(pid, r.W, r.H, a.cellW, a.cellH)
	}
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
	var drawTimer *time.Timer
	var drawReady <-chan time.Time
	defer func() {
		if drawTimer != nil {
			drawTimer.Stop()
		}
	}()
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
			beforeW, beforeH := a.cellW, a.cellH
			a.SetCellPixels(sz.Xpixel, sz.Ypixel)
			if a.cellW != beforeW || a.cellH != beforeH {
				a.syncLayout()
			}
		case <-drawReady:
			drawReady = nil
		case <-tick.C:
			a.onTick()
		}
		if a.quit {
			break
		}
		if a.dirty {
			remaining := time.Until(a.lastDraw.Add(drawMinInterval))
			if remaining <= 0 {
				if drawTimer != nil {
					drawTimer.Stop()
				}
				drawReady = nil
				a.draw()
			} else {
				if drawTimer == nil {
					drawTimer = time.NewTimer(remaining)
				} else {
					drawTimer.Reset(remaining)
				}
				drawReady = drawTimer.C
			}
		}
	}
	if a.quitHook != nil {
		a.quitHook()
	}
}

// Run starts the TUI on the local terminal and blocks until quit. Returns
// an exit code.
var ErrStandaloneRestart = errors.New("standalone restart requested")

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
	if _, err := out.WriteString(EnterSeq + KbdEnterSeq()); err != nil {
		return 1, err
	}
	leave := func() {
		// Fix #6: stop mouse reporting first, flush queued input so the
		// outer shell never sees leftover mouse bytes, then restore modes.
		_, _ = out.WriteString(MouseOffSeq)
		platform.FlushInput(int(in.Fd()))
		DrainInput(in, drainMs)
		_, _ = out.WriteString(KbdLeaveSeq() + LeaveSeq)
		restore()
	}
	defer leave()

	st := model.New(cwd, ws.Cols, ws.Rows)
	sup := runtime.NewSupervisor()
	a := NewApp(st, sup, out, cwd)
	a.BeginHostMouse(ws.Xpixel, ws.Ypixel)
	a.inputEOF = true
	a.SetRestartHook(func() { a.standaloneRestart = true; a.exec(model.CmdQuit{}) })
	if cfg, err := config.Load(); err == nil {
		a.SetUIFlags(cfg.ShowTabBar(), cfg.HideTabBarWhenSingleTab(), cfg.PaneBorders())
		a.SetSidebarConfig(cfg, true)
		a.SetKeybinds(cfg.CopyKeys(), cfg.SplitVerticalKeys(), cfg.SplitHorizontalKeys())
		a.SetPaneKeybinds(cfg)
	}

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
				a.Resize(sz.Cols, sz.Rows, sz.Xpixel, sz.Ypixel)
			}
		}
	}()

	a.Run()
	sup.KillAll()
	if a.standaloneRestart {
		return 0, ErrStandaloneRestart
	}
	return 0, nil
}

// onInput routes a framed input event. Unknown fragments are dropped —
// never forwarded to a pane (fix #3).
