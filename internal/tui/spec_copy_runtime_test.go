package tui

import (
	"encoding/base64"
	"github.com/kazuph/ikesu/internal/teststate"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
)

func specRuntimeApp(t *testing.T) *App {
	t.Helper()
	teststate.Setenv(t, "SHELL", "/bin/sh")
	sup := runtime.NewSupervisor()
	a := NewApp(model.New(t.TempDir(), 120, 35), sup, io.Discard, t.TempDir())
	t.Cleanup(func() {
		for _, w := range a.st.Workspaces {
			for _, tab := range w.Tabs {
				for _, id := range tab.Layout.PaneIDs() {
					sup.Kill(id)
				}
			}
		}
	})
	return a
}

func specWaitText(t *testing.T, p *runtime.Pane, text string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var b strings.Builder
		for _, row := range p.BufferLines() {
			for _, c := range row {
				if c.Width > 0 {
					b.WriteString(c.Content)
				}
			}
			b.WriteByte('\n')
		}
		if strings.Contains(b.String(), text) {
			return
		}
		select {
		case <-timer.C:
			t.Fatal("PTY did not produce", text)
		case <-tick.C:
		}
	}
}

func TestCopyObservedFeedbackWriteSuccessFailureAndTwoSecondDeadline(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	pid := a.focusedPane()
	p, err := a.sup.SpawnWith(pid, a.cwd, 80, 20, nil, []string{"/bin/sh", "-c", "printf 'alpha\\r\\nbeta\\r\\ngamma\\r\\n'; exec sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "beta")
	for _, lines := range []int{1, 2, 3} {
		f, err := os.CreateTemp(t.TempDir(), "osc52")
		if err != nil {
			t.Fatal(err)
		}
		a.out = f
		a.cm = &copyMode{pane: pid, anchor: &copyPoint{0, 0}, cx: 4, cy: lines - 1}
		a.copyYank()
		text := "alpha"
		want := "Copied 1 line"
		if lines == 2 {
			text = "alpha\nbeta"
			want = "Copied 2 lines"
		}
		if lines == 3 {
			text = "alpha\nbeta\ngamma"
			want = "Copied 3 lines"
		}
		b, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != "\x1b]52;c;"+base64.StdEncoding.EncodeToString([]byte(text))+"\a" || a.copied != want {
			t.Fatalf("copy bytes=%q flash=%q", b, a.copied)
		}
		at := a.copiedAt
		if a.copiedFlashAt(at.Add(2*time.Second-time.Nanosecond)) != want || a.copiedFlashAt(at.Add(2*time.Second)) != "" {
			t.Fatal("two-second boundary")
		}
		f.Close()
		a.copied = ""
		a.copiedAt = time.Time{}
		a.copyYank()
		if a.copied != "" || !a.copiedAt.IsZero() {
			t.Fatal("failed clipboard write displayed success")
		}
	}
}
