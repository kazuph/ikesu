package tui

import (
	"io"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/guard"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestUncoveredAgentAppearsInRenderedWarning(t *testing.T) {
	teststate.Lock(t)
	d := t.TempDir()
	teststate.Setenv(t, "HOME", d)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	st := model.New(d, 240, 35)
	sup := runtime.NewSupervisor()
	t.Cleanup(sup.KillAll)
	off := false
	sup.SetGuard(guard.Config{Enabled: &off}, d)
	pid := st.FocusedPane()
	p, e := sup.SpawnWith(pid, d, 80, 24, nil, []string{"/bin/sleep", "60"})
	if e != nil {
		t.Fatal(e)
	}
	a := NewApp(st, sup, io.Discard, d)
	a.serverUI = true
	p.SetAgent("claude")
	sup.SetGuard(guard.Config{}, d)
	var screen string
	a.snapshotSink = func(in render.Input) error {
		if !in.ConfigWarningBanner {
			t.Fatal("warning not visible")
		}
		out := render.Compose(in)
		var b strings.Builder
		for y := 0; y < 35; y++ {
			for x := 0; x < 240; x++ {
				if c := out.Frame.Cell(x, y); c != nil {
					b.WriteString(c.Content)
				}
			}
			b.WriteByte('\n')
		}
		screen = b.String()
		return nil
	}
	a.draw()
	if !strings.Contains(screen, "見張り未適用") || !strings.Contains(screen, "起動時") {
		t.Fatal(screen)
	}
	if !strings.Contains(sup.GuardWarnings(), "新しい pane") {
		t.Fatal(sup.GuardWarnings())
	}
	t.Log(screen)
	p.SetAgent("")
	if sup.GuardWarnings() != "" {
		t.Fatal("human pane still warned")
	}
}
