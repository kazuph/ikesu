package tui

import (
	"fmt"
	"io"
	"testing"
	"time"

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
	// サーバーと同じく、描画中（この画面の状態が当たっている間）に encode する。
	var enc endpoint.SnapshotEncoder
	var dec endpoint.SnapshotDecoder
	var wire []byte
	v.SetSnapshotSink(func(in render.Input) error {
		data, err := enc.Encode(in)
		wire = append([]byte(nil), data...)
		return err
	})
	v.Begin(a, 1000, 1001)
	decode := func() render.Input {
		t.Helper()
		wire = nil
		v.Draw(a, a.SessionPaneSnaps())
		in, err := dec.Decode(wire)
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
	var oldEnc endpoint.SnapshotEncoder
	var oldDec endpoint.SnapshotDecoder
	data, err := oldEnc.Encode(old)
	if err != nil {
		t.Fatal(err)
	}
	got, err := oldDec.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.State.RightSidebarArea().W == 0 {
		t.Fatal("old server snapshot lost the right sidebar")
	}
}

// 単独起動のイベントループ（App.Run の resizeCh）で、行と列はそのままピクセルだけ変わる。
// 右の幅・ペインの PTY の大きさ・描画の更新・コピー表示の再配置を確かめる。
func TestPortraitStandaloneRunLoopPixelOnlyResizeAndCopy(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.st.Cols, a.st.Rows = 160, 45
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { a.Run(); close(done) }()
	defer func() { a.Stop(); <-done; a.sup.KillAll() }()
	a.Do(func() { a.BeginHostMouse(1600, 900) })
	state := func() (right, inner int, pty [2]int, drawn time.Time) {
		a.Do(func() {
			right = a.st.RightSidebarArea().W
			inner = a.innerRect(1).W
			pty = viewerTerminalSizes(a)[1]
			drawn = a.lastDraw
		})
		return
	}
	waitDrawAfter := func(after time.Time) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			if _, _, _, drawn := state(); drawn.After(after) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("orientation change was not drawn")
			}
			a.Do(func() { a.dirty = true })
		}
	}
	right, wideInner, pty, drawn := state()
	if right == 0 || pty[0] != wideInner {
		t.Fatalf("landscape right=%d inner=%d pty=%v", right, wideInner, pty)
	}
	landscapeInner := wideInner

	a.Resize(160, 45, 1600, 1801)
	right, wideInner, pty, _ = state()
	if right != 0 || wideInner <= landscapeInner || pty[0] != wideInner {
		t.Fatalf("pixel-only portrait right=%d inner=%d (landscape %d) pty=%v", right, wideInner, landscapeInner, pty)
	}
	waitDrawAfter(drawn)
	p := a.sup.Get(1)
	if _, err := p.WriteToPTY([]byte("set -- $(stty size); printf '\\nRUNLOOP_%s_%s\\n' \"$1\" \"$2\"\r")); err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, fmt.Sprintf("RUNLOOP_%d_%d", pty[1], wideInner))

	// 縦長（広い）でコピー表示を開き、カーソルと選択の端を右端へ置く。
	a.Do(func() {
		a.enterCopy(false)
		a.cm.cx = a.copyInnerW() - 1
		a.cm.anchor = &copyPoint{x: a.copyInnerW() - 1, y: a.cm.cy}
	})
	_, _, _, drawn = state()
	a.Resize(160, 45, 1600, 900) // ピクセルだけ横長へ戻す：ペインが狭くなる
	right, inner, pty, _ := state()
	if right == 0 || inner != landscapeInner || pty[0] != landscapeInner {
		t.Fatalf("pixel-only landscape right=%d inner=%d pty=%v want %d", right, inner, pty, landscapeInner)
	}
	waitDrawAfter(drawn)
	var cx, ax, ay, cy, top, h int
	var open bool
	a.Do(func() {
		open = a.cm != nil
		if open {
			cx, cy, top, h = a.cm.cx, a.cm.cy, a.cm.top, a.copyInnerH()
			ax, ay = a.cm.anchor.x, a.cm.anchor.y
		}
	})
	if !open {
		t.Fatal("copy view closed by the orientation change")
	}
	if cx != landscapeInner-1 || ax != landscapeInner-1 || cy < top || cy >= top+h || ay < top || ay >= top+h {
		t.Fatalf("copy cursor/selection outside the narrowed pane: cx=%d anchor=(%d,%d) cy=%d top=%d h=%d inner=%d", cx, ax, ay, cy, top, h, landscapeInner)
	}
	a.PushInput([]byte("h")) // 操作を続けられる
	deadline := time.Now().Add(3 * time.Second)
	for {
		a.Do(func() { cx = a.cm.cx })
		if cx == landscapeInner-2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("copy cursor did not move after the orientation change: cx=%d", cx)
		}
		a.Do(func() {})
	}
}

// 2つのタブに、縦長と横長の別々の大きさの持ち主を残す。もう一方の画面の描画・入力・
// 切断の後も、各タブの PTY の列数がそれぞれの持ち主の判定のまま保たれる。
func TestPortraitInactiveTabOwnersKeepTheirOrientation(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	wide := a.NewViewer(io.Discard, 160, 45, nil)
	wide.Begin(a, 1600, 900)
	tall := a.NewViewer(io.Discard, 160, 45, nil)
	tall.Begin(a, 1600, 1801)
	tall.with(a, func(ui *App) { ui.exec(model.CmdNewTab{}) })
	tallTab := a.st.Workspaces[0].Tabs[1]
	wideTab := a.st.Workspaces[0].Tabs[0]
	if tall.tabs[a.st.Workspaces[0].ID] != tallTab.ID || wide.tabs[a.st.Workspaces[0].ID] != wideTab.ID {
		t.Fatal("viewers are not on separate tabs")
	}
	widths := func(tab *model.Tab, portrait bool) map[int]int {
		state := *a.st
		state.Cols, state.Rows = 160, 45
		if portrait {
			state.RightSidebarMode = "hidden"
		} else {
			state.RightSidebarMode = ""
		}
		want := map[int]int{}
		for _, pr := range state.PaneRectsForDisplay(tab, state.PanesArea()) {
			w, _ := model.PaneTermSize(pr.Rect, state.PaneBorders)
			want[pr.ID] = w
		}
		return want
	}
	attached := true
	check := func(label string) {
		t.Helper()
		checkPortrait(t, a, wide, false)
		if attached {
			checkPortrait(t, a, tall, true)
		}
		sizes := viewerTerminalSizes(a)
		for tab, portrait := range map[*model.Tab]bool{wideTab: false, tallTab: true} {
			for id, w := range widths(tab, portrait) {
				if sizes[id][0] != w {
					t.Fatalf("%s: pane %d (portrait owner %t) pty cols=%d want %d", label, id, portrait, sizes[id][0], w)
				}
			}
		}
	}
	tall.Input(a, []byte("x"))
	wide.Input(a, []byte("x"))
	check("both typed")
	wide.Draw(a, a.SessionPaneSnaps())
	tall.Draw(a, a.SessionPaneSnaps())
	wide.Resize(a, 160, 45, 1600, 900)
	check("after draws and a landscape resize")
	tall.Resize(a, 160, 45, 1600, 1801)
	wide.Input(a, []byte("y"))
	check("after a portrait resize")
	tall.Detach(a)
	attached = false
	wide.Input(a, []byte("z"))
	wide.Draw(a, a.SessionPaneSnaps())
	check("after the portrait viewer detached")
}
