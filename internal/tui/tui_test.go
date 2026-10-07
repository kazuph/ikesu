package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/detect"
	"github.com/kazuph/goterrace/internal/model"
)

// innerRect must never return a non-positive size: a too-narrow terminal
// (sidebar taking the whole width) used to pass negative dimensions into
// vt.NewEmulator and panic.
func TestInnerRectClampsTinyPanes(t *testing.T) {
	for _, cols := range []int{30, 10, 4, 1} {
		st := model.New("", cols, 5)
		a := &App{st: st}
		pid := st.Workspaces[0].Tabs[0].Layout.Focused()
		r := a.innerRect(pid)
		if r.W < 1 || r.H < 1 {
			t.Fatalf("cols=%d: innerRect %+v has non-positive size", cols, r)
		}
	}
}

// G3 fail-closed restart warning: with a missing-session warning the
// dialog switches title and lists the pane; without warnings it keeps the
// normal restart wording.
func TestRestartDialogWarnings(t *testing.T) {
	st := model.New("/tmp", 120, 30)
	a := &App{st: st}

	a.warningsHook = func() []api.AgentSessionWarningInfo {
		return []api.AgentSessionWarningInfo{{
			WorkspaceNumber: 1, WorkspaceLabel: "w1", PaneLabel: "p2",
			Agent: "claude", Cwd: "/tmp/x", Reason: "missing session id",
		}}
	}
	a.openRestartDialog()
	if a.dialog == nil {
		t.Fatal("no dialog")
	}
	if a.dialog.d.Title != "Restart with missing agent sessions?" {
		t.Fatalf("title = %q", a.dialog.d.Title)
	}
	if a.dialog.d.Detail != "These AI panes do not have a recorded session id:" {
		t.Fatalf("detail = %q", a.dialog.d.Detail)
	}
	if len(a.dialog.d.Lines) != 1 || !strings.Contains(a.dialog.d.Lines[0], "missing session id") {
		t.Fatalf("lines = %v", a.dialog.d.Lines)
	}
	if a.dialog.d.Width != 76 {
		t.Fatalf("width = %d, want 76", a.dialog.d.Width)
	}

	a2 := &App{st: st}
	a2.warningsHook = func() []api.AgentSessionWarningInfo { return nil }
	a2.openRestartDialog()
	if a2.dialog.d.Title != "Restart gtx?" || len(a2.dialog.d.Lines) != 0 {
		t.Fatalf("unexpected warning form: %+v", a2.dialog.d)
	}
}

// SPEC G3 fossil expiry: an unchanged Claude/Codex fingerprint keeps the
// pane working for 15s then drops it to idle; a changed fingerprint
// restarts the window. Non-Claude/Codex agents and fingerprint-free
// working evidence are never filtered.
func TestFossilExpiry(t *testing.T) {
	st := model.New("/tmp", 120, 30)
	a := &App{st: st}
	working := detect.Detection{State: detect.Working, VisibleWorking: true}
	claudeScreen := "──────────────\n✻ Thinking… (12s · esc to interrupt)\n──────────────\n❯ \n──────────────\n"

	t0 := time.Now()
	if d := a.applyFossilExpiry(1, "claude", claudeScreen, working, t0); d.State != detect.Working {
		t.Fatal("fresh fingerprint must stay working")
	}
	if d := a.applyFossilExpiry(1, "claude", claudeScreen, working, t0.Add(14*time.Second)); d.State != detect.Working {
		t.Fatal("unchanged fingerprint under 15s must stay working")
	}
	if d := a.applyFossilExpiry(1, "claude", claudeScreen, working, t0.Add(16*time.Second)); d.State != detect.Idle {
		t.Fatalf("fossil fingerprint must drop to idle, got %s", d.State)
	}
	if d := a.applyFossilExpiry(1, "claude", claudeScreen, working, t0.Add(16*time.Second)); d.VisibleWorking {
		t.Fatal("fossilized verdict must clear visible_working")
	}
}

func TestFossilResetsOnFingerprintChange(t *testing.T) {
	st := model.New("/tmp", 120, 30)
	a := &App{st: st}
	working := detect.Detection{State: detect.Working}
	s1 := "──────────────\n✻ Thinking… (12s)\n──────────────\n❯ \n──────────────\n"
	s2 := "──────────────\n✢ Thinking… (30s)\n──────────────\n❯ \n──────────────\n"

	t0 := time.Now()
	a.applyFossilExpiry(1, "claude", s1, working, t0)
	// New turn changes the fingerprint → window restarts.
	a.applyFossilExpiry(1, "claude", s2, working, t0.Add(10*time.Second))
	if d := a.applyFossilExpiry(1, "claude", s2, working, t0.Add(20*time.Second)); d.State != detect.Working {
		t.Fatal("changed fingerprint must restart the window, not fossilize")
	}
	if d := a.applyFossilExpiry(1, "claude", s2, working, t0.Add(26*time.Second)); d.State != detect.Idle {
		t.Fatalf("unchanged new fingerprint fossilizes 15s after its change, got %s", d.State)
	}
}

func TestFossilSkipsOtherAgentsAndNoFingerprint(t *testing.T) {
	st := model.New("/tmp", 120, 30)
	a := &App{st: st}
	working := detect.Detection{State: detect.Working}
	t0 := time.Now()
	// gemini: never fossilized.
	if d := a.applyFossilExpiry(1, "gemini", "anything", working, t0.Add(time.Hour)); d.State != detect.Working {
		t.Fatal("non-claude/codex must never be fossilized")
	}
	// claude with fingerprint-free working evidence: no filter.
	if d := a.applyFossilExpiry(1, "claude", "no spinner here", working, t0.Add(time.Hour)); d.State != detect.Working {
		t.Fatal("fingerprint-free working evidence must stay working")
	}
	// codex fossilizes too.
	codexScreen := "old\n• Working (37s • esc to interrupt)\n\n› \n"
	a.applyFossilExpiry(2, "codex", codexScreen, working, t0)
	if d := a.applyFossilExpiry(2, "codex", codexScreen, working, t0.Add(16*time.Second)); d.State != detect.Idle {
		t.Fatalf("codex fossil must drop to idle, got %s", d.State)
	}
}

// SPEC G3 authority clearing: a reported working/blocked that survives
// 15s of explicit visible idle for the same Claude/Codex agent is stale
// and cleared; the recorded session is untouched.
func TestClearStaleReport(t *testing.T) {
	st := model.New("/tmp", 120, 30)
	a := &App{st: st}
	a.reported = map[int]reportedPane{1: {agent: "claude", state: "working"}}
	idle := detect.Detection{State: detect.Idle, VisibleIdle: true}
	t0 := time.Now()

	a.clearStaleReport(1, "claude", idle, t0)
	a.clearStaleReport(1, "claude", idle, t0.Add(10*time.Second))
	if _, ok := a.reported[1]; !ok {
		t.Fatal("authority must survive <15s of visible idle")
	}
	a.clearStaleReport(1, "claude", idle, t0.Add(16*time.Second))
	if _, ok := a.reported[1]; ok {
		t.Fatal("stale reported authority must be cleared after 15s idle")
	}

	// Different agent on screen → never cleared.
	a.reported = map[int]reportedPane{2: {agent: "codex", state: "blocked"}}
	a.clearStaleReport(2, "claude", idle, t0)
	a.clearStaleReport(2, "claude", idle, t0.Add(time.Hour))
	if _, ok := a.reported[2]; !ok {
		t.Fatal("different-agent authority must not be cleared")
	}

	// Non-visible idle → never cleared.
	a.reported = map[int]reportedPane{3: {agent: "claude", state: "working"}}
	notIdle := detect.Detection{State: detect.Idle, VisibleIdle: false}
	a.clearStaleReport(3, "claude", notIdle, t0)
	a.clearStaleReport(3, "claude", notIdle, t0.Add(time.Hour))
	if _, ok := a.reported[3]; !ok {
		t.Fatal("non-visible idle must not clear authority")
	}
}
