package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/guard"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestRestoreShowsShellSpawnFailure(t *testing.T) {
	teststate.Lock(t)
	dir := t.TempDir()
	teststate.Setenv(t, "SHELL", dir+"/missing-shell")
	st := model.New(dir, 120, 35)
	sup := runtime.NewSupervisor()
	t.Cleanup(sup.KillAll)
	a := NewApp(st, sup, io.Discard, dir)
	other := filepath.Join(dir, "saved-cwd")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	saved := st.Snapshot()
	saved.EachPane(func(_ *session.Workspace, _ *session.Tab, p *session.Pane) { p.Cwd = other })
	a.RestoreSession(saved)
	a.snapshotSink = func(in render.Input) error {
		if !strings.Contains(in.RestoreFailures[1], "missing-shell") {
			t.Fatalf("spawn failure not recorded: %q", in.RestoreFailures[1])
		}
		frame := render.Compose(in).Frame
		var screen strings.Builder
		for y := 0; y < frame.H; y++ {
			for x := 0; x < frame.W; x++ {
				if cell := frame.Cell(x, y); cell != nil {
					screen.WriteString(cell.Content)
				}
			}
		}
		if !strings.Contains(screen.String(), "シェルを起動できません") {
			t.Fatalf("spawn failure missing from screen: %s", screen.String())
		}
		return nil
	}
	a.draw()
}

func TestRestoreFailuresStayInTheirPanesWithExistingWarnings(t *testing.T) {
	teststate.Lock(t)
	dir := t.TempDir()
	st := model.New(dir, 500, 42)
	st.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	st.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	sup := runtime.NewSupervisor()
	t.Cleanup(sup.KillAll)
	off := false
	sup.SetGuard(guard.Config{Enabled: &off}, dir)
	long := filepath.Join(dir, strings.Repeat("a", 90))
	if err := os.Mkdir(long, 0700); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(long, "missing-shell")
	blocked := filepath.Join(long, "non-executable-shell")
	if err := os.WriteFile(blocked, []byte("not executable"), 0600); err != nil {
		t.Fatal(err)
	}
	_, e1 := sup.SpawnWith(1, dir, 60, 35, nil, []string{missing})
	_, e2 := sup.SpawnWith(2, dir, 60, 35, nil, []string{blocked})
	if e1 == nil || e2 == nil || e1.Error() == e2.Error() {
		t.Fatalf("distinct real failures: %v %v", e1, e2)
	}
	p, err := sup.SpawnWith(3, dir, 60, 35, nil, []string{"/bin/sleep", "60"})
	if err != nil {
		t.Fatal(err)
	}
	p.SetAgent("claude")
	sup.SetGuard(guard.Config{}, dir)
	a := NewApp(st, sup, io.Discard, dir)
	a.serverUI = true
	a.configWarning, a.configWarningBanner = "config.toml: test warning", true
	var before render.Input
	a.snapshotSink = func(in render.Input) error { before = in; return nil }
	a.draw()
	a.restoreWarnings = map[int]string{1: e1.Error(), 2: e2.Error()}
	a.snapshotSink = func(in render.Input) error {
		summary := "2 個のペインでシェルを起動できません（理由は各ペインに表示）"
		if in.ConfigWarning != before.ConfigWarning+"; "+summary || in.ConfigWarningBanner != before.ConfigWarningBanner {
			t.Errorf("existing warning changed: %q %v; want %q %v", in.ConfigWarning, in.ConfigWarningBanner, before.ConfigWarning, before.ConfigWarningBanner)
		}
		frame := render.Compose(in).Frame
		var warningRow strings.Builder
		for x := 0; x < frame.W; x++ {
			if cell := frame.Cell(x, 0); cell != nil && cell.Width > 0 {
				warningRow.WriteString(cell.Content)
			}
		}
		if !strings.Contains(warningRow.String(), before.ConfigWarning+"; "+summary) {
			t.Errorf("warnings missing or reordered: %s", warningRow.String())
		}
		for _, pr := range st.DisplayPaneRects() {
			if pr.ID != 1 && pr.ID != 2 {
				continue
			}
			rect, _ := model.PaneContentRect(pr.Rect, st.PaneBorders)
			var content strings.Builder
			for y := rect.Y; y < rect.Y+rect.H; y++ {
				var line strings.Builder
				for x := rect.X; x < rect.X+rect.W; x++ {
					if c := frame.Cell(x, y); c != nil && c.Width > 0 {
						line.WriteString(c.Content)
					}
				}
				content.WriteString(strings.TrimRight(line.String(), " "))
			}
			want, other := e1.Error(), e2.Error()
			if pr.ID == 2 {
				want, other = other, want
			}
			if !strings.Contains(content.String(), want) || strings.Contains(content.String(), other) {
				t.Errorf("pane %d failure missing or wrong: %q, want %q", pr.ID, content.String(), want)
			}
		}
		return nil
	}
	a.draw()
}

func TestRestoreFailureCanScrollToEnd(t *testing.T) {
	dir := t.TempDir()
	st := model.New(dir, 80, 10)
	sup := runtime.NewSupervisor()
	a := NewApp(st, sup, io.Discard, dir)
	pid := st.FocusedPane()
	reason := strings.Repeat("long-path/", 100) + "END-OF-REASON"
	a.restoreWarnings = map[int]string{pid: reason}
	pr := st.DisplayPaneRects()[0]
	inner, _ := model.PaneContentRect(pr.Rect, st.PaneBorders)
	for i := 0; i < len(render.RestoreFailureLines(reason, inner.W)); i++ {
		a.wheel(uv.Mouse{X: inner.X + 1, Y: inner.Y, Button: uv.MouseWheelDown})
	}
	a.snapshotSink = func(in render.Input) error {
		frame := render.Compose(in).Frame
		var text strings.Builder
		for y := inner.Y; y < inner.Y+inner.H; y++ {
			for x := inner.X; x < inner.X+inner.W; x++ {
				if c := frame.Cell(x, y); c != nil && c.Width > 0 {
					text.WriteString(c.Content)
				}
			}
		}
		if !strings.Contains(text.String(), "END-OF-REASON") {
			t.Fatalf("end cannot be reached: %q", text.String())
		}
		return nil
	}
	a.draw()
	a.wheel(uv.Mouse{X: inner.X + 1, Y: inner.Y, Button: uv.MouseWheelUp})
	if a.scroll[pid] >= len(render.RestoreFailureLines(reason, inner.W))-inner.H {
		t.Fatal("wheel up did not move toward beginning")
	}
}
