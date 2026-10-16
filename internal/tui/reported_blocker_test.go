package tui

import (
	"bytes"
	"encoding/gob"
	"github.com/kazuph/goterrace/internal/detect"
	"testing"
	"time"

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
	a.SetServerUI()
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
	out := render.Compose(render.Input{State: a.st, Panes: snaps, ServerUI: a.serverUI})
	statusCells := 0
	for y := 0; y < a.st.Rows; y++ {
		for x := 0; x < a.st.ExpandedSidebarWidth(); x++ {
			cell := out.Frame.Cell(x, y)
			if cell.Content == a.glyph(1).Rune && cell.Style.Fg == out.Frame.ThemeColor(a.glyph(1).Color) {
				statusCells++
			}
		}
	}
	if statusCells == 0 {
		t.Fatal("sidebar has no matching status-colored cell")
	}
	r := out.TabHits[0]
	want := a.glyph(1)
	if out.Frame.Cell(r.X+1, r.Y).Style.Fg != out.Frame.ThemeColor(want.Color) {
		t.Fatal("tab status color differs")
	}
}

func TestReportedBlockerPriorityAndObservationOrder(t *testing.T) {
	a := specRuntimeApp(t)
	at := time.Now()
	visible := detect.Detection{State: detect.Blocked, VisibleBlocker: true}
	cases := []struct {
		name, source, agent, detected, state string
		visible                              bool
		age                                  time.Duration
		want                                 string
	}{
		{"fresh", "claude", "claude", "claude", "working", true, time.Second, "blocked"},
		{"idle", "claude", "claude", "claude", "idle", true, time.Second, "blocked"},
		{"old", "claude", "claude", "claude", "working", true, -time.Second, "working"},
		{"same time", "claude", "claude", "claude", "working", true, 0, "working"},
		{"source label mismatch", "gtx:pi", "claude", "claude", "working", true, time.Second, "blocked"},
		{"other agent", "claude", "claude", "codex", "working", true, time.Second, "working"},
		{"not visible", "claude", "claude", "claude", "working", false, time.Second, "working"},
		{"reported blocked", "claude", "claude", "claude", "blocked", false, time.Second, "blocked"},
	}
	for _, agent := range []string{"pi", "omp", "mastracode", "hermes", "opencode", "kilo", "kimi"} {
		cases = append(cases, struct {
			name, source, agent, detected, state string
			visible                              bool
			age                                  time.Duration
			want                                 string
		}{"official " + agent, "gtx:" + agent, agent, agent, "working", true, time.Second, "working"})
		cases = append(cases, struct {
			name, source, agent, detected, state string
			visible                              bool
			age                                  time.Duration
			want                                 string
		}{"custom " + agent, "custom:" + agent, agent, agent, "working", true, time.Second, "blocked"})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a.SetReportedSource(1, tc.agent, tc.state, tc.source)
			r := a.reported[1]
			r.at = at
			a.reported[1] = r
			d := visible
			d.VisibleBlocker = tc.visible
			a.observeBlocker(1, tc.detected, d, at.Add(tc.age))
			if got := a.PaneStatusKey(1); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
			a.SetDecisionAttention(map[int]bool{1: true})
			if a.PaneStatusKey(1) != "blocked" {
				t.Fatal("decision lost priority")
			}
			a.SetDecisionAttention(nil)
			if a.PaneStatusKey(1) != tc.want {
				t.Fatal("decision did not restore state")
			}
		})
	}
	a.SetReportedSource(1, "claude", "working", "claude")
	a.observeBlocker(1, "claude", visible, time.Now().Add(time.Second))
	a.SetReportedSource(1, "claude", "idle", "claude")
	// A new report must not use the earlier observation.
	r := a.reported[1]
	r.at = time.Now().Add(2 * time.Second)
	a.reported[1] = r
	if a.PaneStatusKey(1) != "idle" {
		t.Fatal("new report reused old question")
	}
}

func TestReportedBlockerHandoffPreservesAuthorityAndObservation(t *testing.T) {
	a := specRuntimeApp(t)
	a.SetReportedSource(1, "claude", "working", "claude")
	a.observeBlocker(1, "claude", detect.Detection{State: detect.Blocked, VisibleBlocker: true}, time.Now().Add(time.Second))
	b := specRuntimeApp(t)
	var wire bytes.Buffer
	if err := gob.NewEncoder(&wire).Encode(a.HandoffState()); err != nil {
		t.Fatal(err)
	}
	var handoff HandoffState
	if err := gob.NewDecoder(&wire).Decode(&handoff); err != nil {
		t.Fatal(err)
	}
	b.RestoreHandoff(handoff)
	if b.PaneStatusKey(1) != "blocked" {
		t.Fatal("handoff lost question")
	}
	b.observeBlocker(1, "claude", detect.Detection{}, time.Now().Add(2*time.Second))
	if b.PaneStatusKey(1) != "working" {
		t.Fatal("handoff lost report")
	}
}
