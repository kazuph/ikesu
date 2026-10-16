package tui

import (
	"testing"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
)

// Terminal bytes exercise the actual detector; live Claude acceptance is separate.
func TestReportedWorkingVisibleQuestionAndRecovery(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	p := a.sup.Get(1)
	p.SetAgent("claude")
	a.SetReported(1, "claude", "working")
	var transitions []string
	a.SetAvailHook(func(pid int) { transitions = append(transitions, a.PaneStatusKey(pid)) })
	question := "Question?\r\n1. First\r\n2. Second\r\nEnter to select · Tab/Arrow keys to navigate · Esc to cancel"
	p.WithEmu(func(e *vt.Emulator) { e.Write([]byte("\x1b[2J\x1b[H" + question)) })
	a.detectAll()
	if got := a.PaneStatusKey(1); got != "blocked" {
		t.Fatalf("reported working hid visible question: %s", got)
	}
	assertReportedBlockerRender(t, a, "blocked")
	v := a.NewViewer(nil, 120, 35, nil)
	v.with(a, func(ui *App) { assertReportedBlockerRender(t, ui, "blocked") })
	a.detectAll()
	if len(transitions) != 1 || transitions[0] != "blocked" {
		t.Fatalf("duplicate/missing transition: %v", transitions)
	}
	p.WithEmu(func(e *vt.Emulator) { e.Write([]byte("\x1b[2J\x1b[HReady")) })
	a.detectAll()
	if got := a.PaneStatusKey(1); got != "working" {
		t.Fatalf("question disappeared, got %s", got)
	}
	assertReportedBlockerRender(t, a, "working")
	if len(transitions) != 2 || transitions[1] != "working" {
		t.Fatalf("recovery transition: %v", transitions)
	}
	if _, state, _ := a.Reported(1); state != "working" {
		t.Fatal("overlay replaced report", state)
	}
}

func assertReportedBlockerRender(t *testing.T, a *App, key string) {
	t.Helper()
	snaps := a.paneSnaps()
	if snaps[1].Status.Key != key {
		t.Fatalf("pane snapshot: %s", snaps[1].Status.Key)
	}
	out := render.Compose(render.Input{State: a.st, Panes: snaps})
	r := out.TabHits[0]
	want := a.glyph(1)
	if out.Frame.Cell(r.X+1, r.Y).Style.Fg != out.Frame.ThemeColor(want.Color) {
		t.Fatal("tab status color differs")
	}
}
