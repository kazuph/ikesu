package tui

import (
	"io"
	"reflect"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/teststate"
	"path/filepath"
)

func TestResizeDrawWaitFollowsCapturedGestureAndSizeOwner(t *testing.T) {
	now := time.Unix(0, 0)
	a := &App{lastDraw: now}
	check := func(want time.Duration) {
		t.Helper()
		if got := a.redrawDelay(now); got != want {
			t.Fatalf("redraw delay=%s want=%s", got, want)
		}
	}
	check(drawMinInterval)
	a.sep = &sepDrag{}
	check(drawMinInterval)
	a.sep = nil
	a.sidebarGesture = &sidebarGesture{divider: true}
	check(drawMinInterval)
	a.sidebarGesture = &sidebarGesture{}
	check(drawMinInterval)
	a.sidebarGesture = nil
	first := &Viewer{app: &App{sep: &sepDrag{}}}
	second := &Viewer{app: &App{}}
	a.sizeViewer = first
	check(drawMinInterval)
	a.sizeViewer = second
	check(drawMinInterval)
	a.sizeViewer = first
	first.app.sep = nil
	check(drawMinInterval)
	a.lastDraw = now.Add(-drawMinInterval)
	check(0)
}

func TestResizeInputClaimsViewerAndAppliesFinalGeometryTogetherRealPTY(t *testing.T) {
	teststate.Lock(t)
	a := sizeOwnerRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	small := a.NewViewer(io.Discard, 106, 38, nil)
	small.Begin(a, 106*8, 38*16)
	large := a.NewViewer(io.Discard, 160, 45, nil)
	large.Begin(a, 160*8, 45*16)
	large.AcquireSize(a)
	before := viewerTerminalSizes(a)
	small.with(a, func(ui *App) {
		area := ui.st.PanesArea()
		border := ui.st.ActiveWorkspace().ActiveLayout().Splits(area)[0].Pos
		ui.onInput(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: border, Y: area.Y + area.H/2, Button: uv.MouseLeft}})
		ev := input.Event{Kind: input.KindMouseMotion, Mouse: uv.Mouse{X: border + 4, Y: area.Y + area.H/2, Button: uv.MouseLeft}}
		ui.resizeInput(ev, func() {
			small.applyInput(ui, ev)
			if got := viewerTerminalSizes(a); !reflect.DeepEqual(got, before) {
				t.Fatal("old ratio was applied before final drag geometry", before, got)
			}
		})
		if got := viewerTerminalSizes(a); !reflect.DeepEqual(got, before) {
			t.Fatal("motion resized PTYs before frame", before, got)
		}
		ui.flushResize()
		for _, pr := range ui.ptyPaneRects(ui.currentSizeTab(), ui.st.PanesArea()) {
			w, h := model.PaneTermSize(pr.Rect, ui.st.PaneBorders)
			reviewPTYSize(t, a, pr.ID, w, h, 8, 16)
		}
	})
	if a.sizeViewer != small {
		t.Fatal("last-input viewer did not retain size ownership")
	}
}

func TestResizeInputCoalescesOnlyConsecutiveActiveMotions(t *testing.T) {
	a := NewApp(model.New(t.TempDir(), 160, 45), runtime.NewSupervisor(), io.Discard, "")
	a.st.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	area := a.st.PanesArea()
	border := a.st.ActiveWorkspace().ActiveLayout().Splits(area)[0].Pos
	event := func(kind input.Kind, dx int) input.Event {
		return input.Event{Kind: kind, Mouse: uv.Mouse{X: border + dx, Y: area.Y + area.H/2, Button: uv.MouseLeft}}
	}
	events := []input.Event{event(input.KindMouseClick, 0), event(input.KindMouseMotion, 4), event(input.KindMouseMotion, 8), event(input.KindMouseWheel, 8), event(input.KindMouseMotion, 9), {Kind: input.KindKey, Key: uv.Key{Text: "x", Code: 'x'}}, event(input.KindMouseMotion, 10), event(input.KindMouseRelease, 11)}
	// Modifiers do not end a captured resize. Their motion must still collapse
	// into the last position before the next real input boundary.
	events[2].Mouse.Mod = uv.ModShift
	events[2].Mouse.Button = uv.MouseNone
	var delivered []input.Kind
	a.dispatchInputEvents(events, func(ev input.Event) {
		delivered = append(delivered, ev.Kind)
		a.onInput(ev)
	})
	want := []input.Kind{input.KindMouseClick, input.KindMouseMotion, input.KindMouseWheel, input.KindMouseMotion, input.KindKey, input.KindMouseMotion, input.KindMouseRelease}
	if !reflect.DeepEqual(delivered, want) || a.sep != nil {
		t.Fatal("input boundary changed", delivered, a.sep)
	}
	if got := a.st.ActiveWorkspace().ActiveLayout().Splits(area)[0].Pos; got != border+11 {
		t.Fatal("release coordinate was lost", got, border+11)
	}
	var inactive int
	a.dispatchInputEvents(events[1:3], func(ev input.Event) { inactive++; a.onInput(ev) })
	if inactive != 2 {
		t.Fatal("non-resize motion was coalesced")
	}
}

func TestSidebarResizeRoundTripStaysDragAndSavesReleasedWidth(t *testing.T) {
	for _, right := range []bool{false, true} {
		a := NewApp(model.New(t.TempDir(), 160, 45), runtime.NewSupervisor(), io.Discard, "")
		path := filepath.Join(t.TempDir(), "preferences.json")
		a.preferenceSave = func(p endpoint.ViewPreferences) error { return endpoint.SaveViewPreferences(path, p) }
		x := a.st.SidebarArea().W - 1
		if right {
			x = a.st.RightSidebarArea().X
		}
		mouse := uv.Mouse{X: x, Y: 5, Button: uv.MouseLeft}
		a.beginSidebarDivider(mouse, right, time.Now())
		moved := mouse
		moved.X += 4
		a.dispatchInputEvents([]input.Event{{Kind: input.KindMouseMotion, Mouse: moved}, {Kind: input.KindMouseMotion, Mouse: mouse}}, a.onInput)
		if a.sidebarGesture == nil || !a.sidebarGesture.moved {
			t.Fatal("roundtrip was converted into a click")
		}
		released := mouse
		released.X += 8
		a.onInput(input.Event{Kind: input.KindMouseRelease, Mouse: released})
		preferences, err := endpoint.LoadViewPreferences(path)
		if err != nil || preferences == nil || a.sidebarGesture != nil || a.dividerClicks[0] != nil || a.dividerClicks[1] != nil {
			t.Fatal("drag completion or saving failed", preferences, err)
		}
		if right {
			if preferences.RightSidebarWidth != a.st.Cols-released.X {
				t.Fatal("right release coordinate was not saved", preferences)
			}
		} else if preferences.SidebarWidth != released.X+1 {
			t.Fatal("left release coordinate was not saved", preferences)
		}
	}
}
