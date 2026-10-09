package tui

import (
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/runtime"
)

func TestSPECSplitDefaultsAndOverridesSingleAction(t *testing.T) {
	teststate.Lock(t)
	for _, tc := range []struct {
		key string
		dir layout.Direction
	}{{"%", layout.Vertical}, {"v", layout.Vertical}, {"\"", layout.Horizontal}, {"-", layout.Horizontal}} {
		t.Run(tc.key, func(t *testing.T) {
			a := specRuntimeApp(t)
			specFeed(a, "\x02"+tc.key)
			l := a.st.ActiveWorkspace().TabRef().Layout
			if l.PaneCount() != 2 || l.Root().Direction != tc.dir || l.Root().Ratio != .5 {
				t.Fatal("split action", l.Root())
			}
		})
	}
	a := specRuntimeApp(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[keys]\nsplit_vertical=['prefix+o']\nsplit_horizontal=['prefix+d']\ncopy_mode=['prefix+y']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "GTX_CONFIG", path)
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	a.SetKeybinds(c.CopyKeys(), c.SplitVerticalKeys(), c.SplitHorizontalKeys())
	for _, key := range []string{"%", "v", "\"", "-"} {
		specFeed(a, "\x02"+key)
	}
	l := a.st.ActiveWorkspace().TabRef().Layout
	if l.PaneCount() != 1 {
		t.Fatal("default override fired")
	}
	specFeed(a, "\x02o")
	if l.PaneCount() != 2 || l.Root().Direction != layout.Vertical {
		t.Fatal("override split duplicated")
	}
	specFeed(a, "\x02d")
	if l.PaneCount() != 3 {
		t.Fatal("override split duplicated")
	}
	specFeed(a, "\x02]")
	specFeed(a, "\x02[")
	if a.cm != nil {
		t.Fatal("copy default survived override")
	}
	specFeed(a, "\x02y")
	if a.cm == nil {
		t.Fatal("copy override ignored")
	}
}

func TestSPECThreeCopyEntriesAndFullscreenRuntimeResize(t *testing.T) {
	teststate.Lock(t)
	for _, entry := range []string{"[", "]", "button"} {
		t.Run(entry, func(t *testing.T) {
			a := specRuntimeApp(t)
			if err := a.StartInitialPane(); err != nil {
				t.Fatal(err)
			}
			before := a.st.ActiveWorkspace().TabRef().Layout.Fingerprint()
			pid := a.focusedPane()
			if entry == "button" {
				a.copyCycle()
			} else {
				specFeed(a, "\x02"+entry)
			}
			if a.cm == nil || a.cm.pane != pid || a.cm.fullscreen || a.copyLabel() != " PHONE COPY " {
				t.Fatal("entry state")
			}
			a.copyCycle()
			if !a.cm.fullscreen || a.copyLabel() != " EXIT COPY " {
				t.Fatal("fullscreen state")
			}
			area := a.st.PanesArea()
			a.sup.Get(pid).WithEmu(func(e *vt.Emulator) {
				if e.Width() != area.W || e.Height() != area.H {
					t.Fatalf("fullscreen size=%dx%d want=%v", e.Width(), e.Height(), area)
				}
			})
			a.copyCycle()
			if a.cm != nil || a.copyLabel() != " COPY " || a.st.ActiveWorkspace().TabRef().Layout.Fingerprint() != before {
				t.Fatal("copy exit layout changed")
			}
			inner := a.innerRect(pid)
			a.sup.Get(pid).WithEmu(func(e *vt.Emulator) {
				if e.Width() != inner.W || e.Height() != inner.H {
					t.Fatal("exit size")
				}
			})
		})
	}
}

func TestSPECActionBarActionsPriorityDirtyAndTitleZoom(t *testing.T) {
	teststate.Lock(t)
	for _, op := range []string{"cycle", "rotate", "equalize"} {
		a := specRuntimeApp(t)
		a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
		a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
		l := a.st.ActiveWorkspace().TabRef().Layout
		l.SetRatioAt(nil, .8)
		changed := 0
		a.SetChangeHook(func() { changed++ })
		out := render.Compose(render.Input{State: a.st})
		a.actionBar = out.ActionBar
		r := a.actionBar.Cycle
		if op == "rotate" {
			r = a.actionBar.Rotate
		}
		if op == "equalize" {
			r = a.actionBar.Equalize
		}
		a.dirty = false
		a.actionBarHit(uv.Mouse{X: r.X, Y: r.Y, Button: uv.MouseLeft})
		if changed != 1 || !a.dirty {
			t.Fatal("action did not dirty once", op, changed)
		}
		if op == "cycle" && l.Root().Direction != layout.Horizontal {
			t.Fatal("cycle")
		}
		if op == "rotate" && !reflect.DeepEqual(l.LeafIDs(), []int{3, 1, 2}) {
			t.Fatal("rotate", l.LeafIDs())
		}
		if op == "equalize" && l.Root().Ratio != 1.0/3 {
			t.Fatal("equalize", l.Root().Ratio)
		}
		// Overlapping hit areas fire copy first and perform no layout command.
		if err := a.StartInitialPane(); err != nil {
			t.Fatal(err)
		}
		a.actionBar.Copy = r
		a.actionBar.Cycle = r
		a.actionBar.Rotate = r
		a.actionBar.Equalize = r
		before := l.Fingerprint()
		changed = 0
		a.actionBarHit(uv.Mouse{X: r.X, Y: r.Y, Button: uv.MouseLeft})
		if a.cm == nil || l.Fingerprint() != before || changed != 0 {
			t.Fatal("copy priority")
		}
		a.exitCopy()
		// Title ownership determines focus and toggles zoom without selection.
		pr := l.Panes(a.st.PanesArea())[0]
		a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: pr.Rect.X + 2, Y: pr.Rect.Y, Button: uv.MouseLeft}})
		if a.focusedPane() != pr.ID || a.st.ActiveWorkspace().TabRef().Zoomed != model.PaneID(pr.ID) || a.sel != nil || a.sep != nil || a.fwd != -1 {
			t.Fatal("title zoom/focus/routing")
		}
		a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: a.st.PanesArea().X + 2, Y: a.st.PanesArea().Y, Button: uv.MouseLeft}})
		if a.st.ActiveWorkspace().TabRef().Zoomed != 0 {
			t.Fatal("title unzoom")
		}
	}
}

func TestSPECMoveAndRotateKeepRealProcessIdentity(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	a.exec(model.CmdSplitFocused{Dir: layout.Horizontal})
	l := a.st.ActiveWorkspace().TabRef().Layout
	ptr := map[int]*runtime.Pane{}
	pid := map[int]int{}
	for _, id := range l.PaneIDs() {
		p := a.sup.Get(id)
		p.PollProcess()
		ptr[id] = p
		pid[id] = p.Snapshot().FgPID
		if pid[id] <= 0 {
			t.Fatal("missing actual process id")
		}
	}
	check := func() {
		for id, p := range ptr {
			now := a.sup.Get(id)
			now.PollProcess()
			if now != p || now.ID != id || now.Snapshot().FgPID != pid[id] {
				t.Fatal("pane/process pairing changed", id, pid[id], now.Snapshot().FgPID)
			}
		}
	}
	for _, op := range []string{"rotate_forward", "rotate_backward"} {
		a.exec(model.CmdArrange{Pane: model.PaneID(l.Focused()), Op: op})
		check()
	}
	moving := l.PaneIDs()[1]
	for _, dest := range []model.MoveDest{{Kind: model.MoveDestNewTab, Workspace: 0}, {Kind: model.MoveDestNewWorkspace, Cwd: a.cwd}, {Kind: model.MoveDestTab, TabW: 0, TabT: 0, Split: layout.Vertical}} {
		a.exec(model.CmdMovePane{Pane: model.PaneID(moving), Dest: dest, Focus: true})
		check()
		if a.focusedPane() != moving {
			t.Fatal("moved focus")
		}
	}
}
