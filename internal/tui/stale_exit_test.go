package tui

import (
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestStaleExitDoesNotCloseReusedPane(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	id := a.focusedPane()
	if _, err := a.sup.SpawnWith(id, a.cwd, 80, 20, nil, []string{"/usr/bin/true"}); err != nil {
		t.Fatal(err)
	}
	var exited runtime.Event
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
wait:
	for {
		select {
		case ev := <-a.sup.Events():
			if ev.Pane == id && ev.Kind == runtime.EventExited {
				exited = ev
				break wait
			}
		case <-timer.C:
			t.Fatal("old process did not exit")
		}
	}
	a.sup.Kill(id)
	current, err := a.sup.Spawn(id, a.cwd, 80, 20)
	if err != nil {
		t.Fatal(err)
	}
	a.onRuntimeEvent(exited)
	if a.sup.Get(id) != current || a.st.Workspaces[0].Tabs[0].Layout.Focused() != id {
		t.Fatal("old exit affected replacement pane")
	}
}

func TestStaleExitDoesNotCloseReusedTemporaryPane(t *testing.T) {
	for _, kind := range []string{"popup", "overlay"} {
		t.Run(kind, func(t *testing.T) {
			teststate.Lock(t)
			a := specRuntimeApp(t)
			id := 2
			if kind == "popup" {
				a.st.Popup = &model.PopupPaneState{WorkspaceID: a.st.ActiveWorkspace().ID, PaneID: id}
				a.st.Popups = map[int]*model.PopupPaneState{a.st.Popup.WorkspaceID: a.st.Popup}
			} else {
				a.st.Overlay = &model.OverlayPaneState{WorkspaceID: a.st.ActiveWorkspace().ID, TabID: a.st.ActiveWorkspace().TabRef().ID, PaneID: id, TargetPaneID: a.focusedPane()}
			}
			if _, err := a.sup.SpawnWith(id, a.cwd, 80, 20, nil, []string{"/usr/bin/true"}); err != nil {
				t.Fatal(err)
			}
			var exited runtime.Event
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
		wait:
			for {
				select {
				case ev := <-a.sup.Events():
					if ev.Pane == id && ev.Kind == runtime.EventExited {
						exited = ev
						break wait
					}
				case <-timer.C:
					t.Fatal("old process did not exit")
				}
			}
			a.sup.Kill(id)
			current, err := a.sup.Spawn(id, a.cwd, 80, 20)
			if err != nil {
				t.Fatal(err)
			}
			defer a.sup.Kill(id)
			a.onRuntimeEvent(exited)
			if a.sup.Get(id) != current || (kind == "popup" && a.st.PopupByPane(id) == nil) || (kind == "overlay" && a.st.Overlay == nil) {
				t.Fatal("old exit affected replacement", kind)
			}
		})
	}
}
