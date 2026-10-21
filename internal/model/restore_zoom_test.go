package model

import (
	"encoding/json"
	"testing"

	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/session"
)

func TestZoomSnapshotAndRestoreKeepsExactPane(t *testing.T) {
	s := New(t.TempDir(), 120, 40)
	s.Apply(CmdSplitFocused{Dir: layout.Vertical})
	tab := s.Workspaces[0].Tabs[0]
	pid := tab.Layout.Focused()
	s.Apply(CmdZoomPane{Pane: pid, Mode: "on"})
	f := s.Snapshot()
	if f.Workspaces[0].Tabs[0].Zoomed != pid {
		t.Fatal("zoom not saved")
	}
	if warnings := f.Validate(); len(warnings) > 0 {
		t.Fatal(warnings)
	}
	restored := Restore(f)
	if restored.Workspaces[0].Tabs[0].Zoomed != pid {
		t.Fatal("zoom not restored")
	}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var saved session.File
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if Restore(&saved).Workspaces[0].Tabs[0].Zoomed != pid {
		t.Fatal("JSON lost zoom")
	}
}
func TestOldSessionWithoutZoomRestoresUnzoomed(t *testing.T) {
	f := New(t.TempDir(), 120, 40).Snapshot()
	b, _ := json.Marshal(f)
	var old session.File
	if err := json.Unmarshal(b, &old); err != nil {
		t.Fatal(err)
	}
	if old.Workspaces[0].Tabs[0].Zoomed != 0 || Restore(&old).Workspaces[0].Tabs[0].Zoomed != 0 {
		t.Fatal("old snapshot has zoom")
	}
}
