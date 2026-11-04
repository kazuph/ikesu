package tui

import (
	"fmt"
	"io"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/teststate"
)

// 縦長（高さ px > 幅 px、ピクセルが無ければ 行数×2 > 列数）の画面では右サイドバーを出さない。

type portraitAreas struct{ left, right, panes, cols int }

func viewerAreas(a *App, v *Viewer) (got portraitAreas) {
	v.with(a, func(ui *App) {
		got = portraitAreas{ui.st.SidebarArea().W, ui.st.RightSidebarArea().W, ui.st.PanesArea().W, ui.st.Cols}
	})
	return got
}

func checkPortrait(t *testing.T, a *App, v *Viewer, portrait bool) portraitAreas {
	t.Helper()
	got := viewerAreas(a, v)
	if portrait && got.right != 0 {
		t.Fatalf("portrait screen shows the right sidebar: %+v", got)
	}
	if !portrait && got.right == 0 {
		t.Fatalf("landscape screen lost the right sidebar: %+v", got)
	}
	if got.panes != got.cols-got.left-got.right {
		t.Fatalf("panes do not fill the freed width: %+v", got)
	}
	return got
}

func portraitApp(t *testing.T) *App {
	t.Helper()
	return NewApp(model.New(t.TempDir(), 100, 30), runtime.NewSupervisor(), io.Discard, "")
}

func TestPortraitJudgmentBoundaries(t *testing.T) {
	for _, tc := range []struct {
		cols, rows, xp, yp int
		portrait           bool
	}{
		{100, 30, 1000, 1000, false}, // 正方形のピクセルは横長
		{100, 30, 1000, 1001, true},  // 高さ px が 1 だけ大きい
		{100, 30, 1001, 1000, false},
		{100, 60, 2000, 1000, false}, // ピクセルが取れれば行数より優先
		{100, 50, 0, 0, false},       // 行数×2 == 列数
		{100, 51, 0, 0, true},
		{100, 60, 1000, 0, true}, // 片方だけでは行数で判定
		{100, 30, 0, 1001, false},
		{107, 87, 1070, 1827, true}, // 店長の今の画面
	} {
		t.Run(fmt.Sprintf("%dx%d_%dx%dpx", tc.cols, tc.rows, tc.xp, tc.yp), func(t *testing.T) {
			a := portraitApp(t)
			v := a.NewViewer(io.Discard, tc.cols, tc.rows, nil)
			v.Begin(a, tc.xp, tc.yp)
			got := checkPortrait(t, a, v, tc.portrait)
			if got.left != 26 {
				t.Fatalf("left sidebar changed: %+v", got)
			}
		})
	}
}

func TestPortraitFollowsPixelOnlyAndLateReports(t *testing.T) {
	a := portraitApp(t)
	v := a.NewViewer(io.Discard, 100, 30, nil)
	v.Begin(a, 1000, 1000)
	checkPortrait(t, a, v, false)
	v.Resize(a, 100, 30, 1000, 1001) // 行と列はそのまま、ピクセルだけ縦長へ
	checkPortrait(t, a, v, true)
	v.Resize(a, 100, 30, 1000, 1000)
	checkPortrait(t, a, v, false)
	v.Resize(a, 100, 60, 2000, 1000)
	checkPortrait(t, a, v, false)
	v.Resize(a, 100, 60, 0, 0) // ピクセルが取れなくなったら古い値を使わない
	checkPortrait(t, a, v, true)
	v.Input(a, []byte("\x1b[4;1000;2000t")) // 遅れて届く端末のピクセル応答（高さ;幅）
	checkPortrait(t, a, v, false)
	v.Input(a, []byte("\x1b[4;2000;1000t"))
	checkPortrait(t, a, v, true)

	late := a.NewViewer(io.Discard, 100, 30, nil)
	late.Begin(a, 0, 0)
	checkPortrait(t, a, late, false)
	late.Input(a, []byte("\x1b[4;1001;1000t"))
	checkPortrait(t, a, late, true)
}

func TestPortraitStandaloneHostTerminal(t *testing.T) {
	a := portraitApp(t)
	a.BeginHostMouse(1000, 1001)
	if a.st.RightSidebarArea().W != 0 {
		t.Fatal("standalone portrait start shows the right sidebar")
	}
	a.onInput(hostReport(t, "\x1b[4;1000;2000t"))
	if a.st.RightSidebarArea().W == 0 {
		t.Fatal("standalone pixel report did not return to landscape")
	}
}

func hostReport(t *testing.T, raw string) input.Event {
	t.Helper()
	events := (&input.Framer{}).Push([]byte(raw))
	if len(events) != 1 {
		t.Fatalf("events=%d", len(events))
	}
	return events[0]
}

func TestPortraitTwoViewersStayIndependentWithPTYOwner(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	wide := a.NewViewer(io.Discard, 160, 45, nil)
	wide.Begin(a, 1600, 900)
	tall := a.NewViewer(io.Discard, 160, 45, nil)
	tall.Begin(a, 1600, 1801)
	expectPTY := func(owner *Viewer, portrait bool) {
		t.Helper()
		state := *a.st
		owner.display.apply(&state)
		state.Cols, state.Rows = owner.cols, owner.rows
		if portrait {
			state.RightSidebarMode = "hidden"
		}
		want := map[int][2]int{}
		for _, pr := range state.PaneRectsForDisplay(state.ActiveWorkspace().TabRef(), state.PanesArea()) {
			w, h := model.PaneTermSize(pr.Rect, state.PaneBorders)
			want[pr.ID] = [2]int{w, h}
		}
		for id, size := range viewerTerminalSizes(a) {
			if w, ok := want[id]; ok && size != w {
				t.Fatalf("pane %d pty=%v want=%v (owner portrait=%t)", id, size, w, portrait)
			}
		}
	}
	for round := 0; round < 2; round++ {
		tall.Input(a, []byte("x"))
		checkPortrait(t, a, tall, true)
		checkPortrait(t, a, wide, false)
		expectPTY(tall, true)
		wide.Input(a, []byte("x"))
		checkPortrait(t, a, wide, false)
		checkPortrait(t, a, tall, true)
		expectPTY(wide, false)
		tall.Resize(a, 160, 45, 1600, 1801)
		checkPortrait(t, a, wide, false)
		expectPTY(tall, true)
	}
	tall.Detach(a)
	checkPortrait(t, a, wide, false)
	expectPTY(wide, false)
}

func TestPortraitToggleKeyDoesNothingAndLandscapeRestores(t *testing.T) {
	for _, mode := range []string{"", "compact", "hidden"} {
		t.Run("mode="+mode, func(t *testing.T) {
			a := NewApp(model.New(t.TempDir(), 160, 45), runtime.NewSupervisor(), io.Discard, "")
			v := a.NewViewer(io.Discard, 160, 45, nil)
			saves := 0
			var saved endpoint.ViewPreferences
			v.Configure(a, &config.Config{}, &endpoint.ViewPreferences{SidebarWidth: 26, RightSidebarMode: mode, RightSidebarWidth: 30}, func(p endpoint.ViewPreferences) error { saves++; saved = p; return nil })
			v.Begin(a, 1600, 900)
			before := viewerAreas(a, v)
			v.Resize(a, 160, 45, 1600, 1801)
			if got := viewerAreas(a, v); got.right != 0 {
				t.Fatalf("portrait right=%+v", got)
			}
			for i := 0; i < 3; i++ {
				v.with(a, func(ui *App) { ui.configAction("toggle_right_sidebar", uv.Key{}) })
			}
			v.with(a, func(ui *App) {
				if ui.st.RightSidebarMode != mode || ui.st.RightSidebarSize != 30 {
					t.Fatalf("portrait toggle changed preference: mode=%q size=%d", ui.st.RightSidebarMode, ui.st.RightSidebarSize)
				}
				if ui.st.RightSidebarArea().W != 0 {
					t.Fatal("toggle revealed the right sidebar")
				}
			})
			if saves != 0 && (saved.RightSidebarMode != mode || saved.RightSidebarWidth != 30) {
				t.Fatalf("saved changed preference: %+v", saved)
			}
			v.Resize(a, 160, 45, 1600, 900)
			if got := viewerAreas(a, v); got != before {
				t.Fatalf("landscape restore=%+v want %+v", got, before)
			}
		})
	}
}

func TestPortraitDuringRightDividerDragKeepsPreference(t *testing.T) {
	a := NewApp(model.New(t.TempDir(), 160, 45), runtime.NewSupervisor(), io.Discard, "")
	v := a.NewViewer(io.Discard, 160, 45, nil)
	var saved *endpoint.ViewPreferences
	v.Configure(a, &config.Config{}, nil, func(p endpoint.ViewPreferences) error { saved = &p; return nil })
	v.Begin(a, 1600, 900)
	var x int
	v.with(a, func(ui *App) { x = ui.st.RightSidebarArea().X })
	v.Input(a, []byte(fmt.Sprintf("\x1b[<0;%d;10M", x+1)))
	v.Input(a, []byte(fmt.Sprintf("\x1b[<32;%d;10M", x-4+1)))
	var dragged int
	v.with(a, func(ui *App) { dragged = ui.st.RightSidebarSize })
	if dragged != 160-(x-4) {
		t.Fatalf("landscape drag did not resize: %d", dragged)
	}
	v.Resize(a, 160, 45, 1600, 1801)
	v.Input(a, []byte(fmt.Sprintf("\x1b[<32;%d;10M", 150)))
	v.Input(a, []byte(fmt.Sprintf("\x1b[<0;%d;10m", 150)))
	v.with(a, func(ui *App) {
		if ui.st.RightSidebarSize != dragged || ui.st.RightSidebarMode != "" {
			t.Fatalf("portrait drag changed preference: size=%d mode=%q", ui.st.RightSidebarSize, ui.st.RightSidebarMode)
		}
	})
	if saved != nil && (saved.RightSidebarWidth != dragged || saved.RightSidebarMode != "") {
		t.Fatalf("saved=%+v", *saved)
	}
	v.Resize(a, 160, 45, 1600, 900)
	if got := viewerAreas(a, v); got.right != dragged {
		t.Fatalf("landscape restore right=%d want %d", got.right, dragged)
	}
}

func TestPortraitMobileThresholdAndPreset(t *testing.T) {
	for _, tc := range []struct {
		cols         int
		preset       bool
		left, mobile int
	}{
		{64, false, 0, 64},
		{70, false, 26, 64},
		{160, true, 26, 64},
	} {
		t.Run(fmt.Sprintf("cols=%d/preset=%t", tc.cols, tc.preset), func(t *testing.T) {
			st := model.New(t.TempDir(), tc.cols, 45)
			st.MobileWidthThreshold = tc.mobile
			st.SidebarPresetActive = tc.preset
			a := NewApp(st, runtime.NewSupervisor(), io.Discard, "")
			v := a.NewViewer(io.Discard, tc.cols, 45, nil)
			v.Begin(a, tc.cols*10, 450)
			landscape := viewerAreas(a, v)
			v.Resize(a, tc.cols, 45, tc.cols*10, 2000)
			got := viewerAreas(a, v)
			if got.right != 0 || got.left != landscape.left || got.panes != tc.cols-got.left {
				t.Fatalf("portrait=%+v landscape=%+v", got, landscape)
			}
			if tc.cols <= tc.mobile && (landscape.left != 0 || landscape.right != 0) {
				t.Fatalf("mobile changed: %+v", landscape)
			}
		})
	}
}

func TestPortraitRemoteSnapshotCarriesServerJudgment(t *testing.T) {
	a := portraitApp(t)
	v := a.NewViewer(io.Discard, 100, 30, nil)
	var last render.Input
	v.SetSnapshotSink(func(in render.Input) error { last = in; return nil })
	v.Begin(a, 1000, 1001)
	decode := func() render.Input {
		t.Helper()
		v.Draw(a, a.SessionPaneSnaps())
		var enc endpoint.SnapshotEncoder
		var dec endpoint.SnapshotDecoder
		data, err := enc.Encode(last)
		if err != nil {
			t.Fatal(err)
		}
		in, err := dec.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		return in
	}
	in := decode()
	if in.State.RightSidebarArea().W != 0 || in.State.PanesArea().W != 100-26 {
		t.Fatalf("remote portrait right=%+v panes=%+v", in.State.RightSidebarArea(), in.State.PanesArea())
	}
	out := render.Compose(in)
	if out.Frame.W != 100 {
		t.Fatalf("frame width=%d", out.Frame.W)
	}
	v.Resize(a, 100, 30, 1000, 1000)
	if in := decode(); in.State.RightSidebarArea().W == 0 {
		t.Fatal("remote landscape lost the right sidebar")
	}

	// 古い版のサーバーは判定を送らない。クライアントは自分で縦長と決めず右を出す。
	old := render.Input{State: model.New(t.TempDir(), 100, 60)}
	var enc endpoint.SnapshotEncoder
	var dec endpoint.SnapshotDecoder
	data, err := enc.Encode(old)
	if err != nil {
		t.Fatal(err)
	}
	got, err := dec.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.State.RightSidebarArea().W == 0 {
		t.Fatal("old server snapshot lost the right sidebar")
	}
}
