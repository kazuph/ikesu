package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/teststate"
)

func TestPaneMenuCopiesHandoffSnapshot(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	const markdown = "# ikesu handoff\n## last turn"
	osc := []byte("\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(markdown)) + "\a")
	copied := make(chan struct{}, 1)
	pid := a.focusedPane()
	disabled := a.NewViewer(&snapshotClipboardSink{want: osc, copied: copied}, a.st.Cols, a.st.Rows, nil)
	disabled.with(a, func(ui *App) {
		ui.openPaneMenu(5, 5, pid)
		index := menuIndex(ui, "Copy handoff snapshot")
		if index < 0 || !ui.menu.m.Items[index].Disabled || ui.menu.m.Items[index+1].Label != "Rename pane" {
			t.Fatal("disabled snapshot item should appear immediately before Rename pane")
		}
		ui.activateMenu(index)
	})
	called := make(chan int, 1)
	a.SetSnapshotHook(func(target int) (string, error) {
		// The real server hook also enters the app loop to capture the pane.
		a.Do(func() { called <- target })
		return markdown, nil
	})
	viewer := a.NewViewer(&snapshotClipboardSink{want: osc, copied: copied}, a.st.Cols, a.st.Rows, nil)
	rect := a.innerRect(pid)
	go a.Run()
	defer func() { a.Stop(); <-a.Done(); a.sup.KillAll() }()

	a.Do(func() {
		x, y := rect.X+2, rect.Y+2
		viewer.Input(a, []byte(fmt.Sprintf("\x1b[<2;%d;%dM\x1b[<2;%d;%dm", x, y, x, y)))
		viewer.with(a, func(ui *App) {
			if ui.menu == nil {
				t.Error("right-click did not open the pane menu")
				return
			}
			index := menuIndex(ui, "Copy handoff snapshot")
			if index < 0 || ui.menu.m.Items[index].Disabled {
				t.Error("snapshot item should be enabled with the server hook")
				return
			}
			ui.activateMenu(index)
		})
	})
	select {
	case <-copied:
	case <-time.After(3 * time.Second):
		t.Fatal("viewer did not emit the snapshot OSC 52 copy")
	}
	var notice string
	a.Do(func() { notice = viewer.app.copied })
	if got := <-called; got != pid || notice != "Copied 2 lines" {
		t.Fatalf("target=%d, notice=%q", got, notice)
	}
}

type snapshotClipboardSink struct {
	want   []byte
	copied chan<- struct{}
}

func (s *snapshotClipboardSink) Write(p []byte) (int, error) {
	if bytes.Contains(p, s.want) {
		select {
		case s.copied <- struct{}{}:
		default:
		}
	}
	return len(p), nil
}

func TestPaneMenuCancelsSnapshotCopyForHiddenViewer(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	const markdown = "remote snapshot"
	osc := []byte("\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(markdown)) + "\a")
	copied := make(chan struct{}, 1)
	started, finish := make(chan struct{}), make(chan struct{})
	a.SetSnapshotHook(func(int) (string, error) {
		close(started)
		<-finish
		return markdown, nil
	})
	viewer := a.NewViewer(&snapshotClipboardSink{want: osc, copied: copied}, a.st.Cols, a.st.Rows, nil)
	go a.Run()
	defer func() { a.Stop(); <-a.Done(); a.sup.KillAll() }()

	a.Do(func() {
		viewer.with(a, func(ui *App) {
			ui.openPaneMenu(5, 5, ui.focusedPane())
			ui.activateMenu(menuIndex(ui, "Copy handoff snapshot"))
		})
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("copy hook did not start")
	}
	a.Do(func() { viewer.SetVisible(a, false) })
	close(finish)
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		var canceled bool
		a.Do(func() { canceled = viewer.snapshotCopyCanceled })
		if canceled {
			break
		}
		select {
		case <-copied:
			t.Fatal("copy escaped to the hidden machine")
		case <-timer.C:
			t.Fatal("hidden copy was not canceled")
		case <-tick.C:
		}
	}
	a.Do(func() {
		if viewer.app.toast != nil {
			t.Error("cancellation notification appeared while hidden")
		}
		viewer.SetVisible(a, true)
		if viewer.app.toast == nil || viewer.app.toast.body != "画面が切り替わったためコピーを取り消しました。もう一度選んでください" {
			t.Errorf("cancellation notification missing on return: %+v", viewer.app.toast)
		}
		if viewer.app.copied != "" || viewer.snapshotCopyCanceled {
			t.Error("hidden copy retained success or a pending notice")
		}
	})
	select {
	case <-copied:
		t.Fatal("canceled copy was replayed")
	default:
	}
}
