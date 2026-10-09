package model

import (
	"encoding/json"
	"testing"

	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/session"
)

func TestDefaultWorkspaceNameSurvivesSaveDuplicateAndRename(t *testing.T) {
	s := newState()
	s.Apply(CmdNewWorkspace{Cwd: "/tmp/cwd", Name: "cwd"})
	data, err := json.Marshal(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var saved session.File
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	r := Restore(&saved)
	w := r.ActiveWorkspace()
	if w.Name != "cwd" || !w.DefaultName || w.CustomName {
		t.Fatal("automatic name became a custom name after restore", w)
	}
	r.Apply(CmdDuplicateWorkspace{Index: r.Active, Focus: true})
	duplicate := r.Workspaces[len(r.Workspaces)-1]
	if duplicate.Name != "cwd" || !duplicate.DefaultName || duplicate.CustomName {
		t.Fatal("duplicate lost automatic name semantics", duplicate)
	}
	r.Apply(CmdRenameWorkspace{Index: r.Active, Name: "chosen"})
	if r.ActiveWorkspace().Name != "chosen" || r.ActiveWorkspace().DefaultName || !r.ActiveWorkspace().CustomName {
		t.Fatal("explicit rename retained automatic name semantics")
	}
	if !w.DefaultName {
		t.Fatal("renaming the duplicate changed its source")
	}
}

// Snapshot → Validate → Restore must reproduce workspaces, tabs, split
// trees, focus, and pane ids exactly (SPEC G3/G5).
func TestSnapshotRestoreRoundTrip(t *testing.T) {
	s := newState()
	s.Apply(CmdSplitFocused{Dir: layout.Vertical})
	s.Apply(CmdNewTab{Workspace: 0})
	s.Apply(CmdNewWorkspace{Cwd: "/w2"})
	s.Apply(CmdRenamePane{Pane: 1, Label: "main"})

	f := s.Snapshot()
	if w := f.Validate(); len(w) != 0 {
		t.Fatalf("snapshot invalid: %v", w)
	}
	r := Restore(f)

	if len(r.Workspaces) != len(s.Workspaces) {
		t.Fatalf("workspaces %d, want %d", len(r.Workspaces), len(s.Workspaces))
	}
	for i, ws := range s.Workspaces {
		rw := r.Workspaces[i]
		if rw.ID != ws.ID || rw.Name != ws.Name || rw.Cwd != ws.Cwd || rw.ActiveTab != ws.ActiveTab {
			t.Fatalf("workspace %d mismatch: %+v vs %+v", i, rw, ws)
		}
		if len(rw.Tabs) != len(ws.Tabs) {
			t.Fatalf("workspace %d tabs %d, want %d", i, len(rw.Tabs), len(ws.Tabs))
		}
		for j, tab := range ws.Tabs {
			rt := rw.Tabs[j]
			if rt.ID != tab.ID || rt.Name != tab.Name {
				t.Fatalf("tab mismatch: %+v vs %+v", rt, tab)
			}
			if rt.Layout.Focused() != tab.Layout.Focused() {
				t.Fatalf("focus %d, want %d", rt.Layout.Focused(), tab.Layout.Focused())
			}
			got, want := rt.Layout.PaneIDs(), tab.Layout.PaneIDs()
			if len(got) != len(want) {
				t.Fatalf("panes %v, want %v", got, want)
			}
			for k := range got {
				if got[k] != want[k] {
					t.Fatalf("pane ids %v, want %v", got, want)
				}
			}
		}
	}
	if r.PaneLabels[1] != "main" {
		t.Fatalf("pane label lost: %v", r.PaneLabels)
	}
	if r.Active != s.Active || r.SidebarVisible != s.SidebarVisible {
		t.Fatal("active/sidebar not preserved")
	}
}

// G5: after restore the allocation counters are beyond every restored id,
// so new panes never collide with restored ones.
func TestRestoreAdvancesCounters(t *testing.T) {
	f := &session.File{
		Active: 0,
		Workspaces: []session.Workspace{{
			ID: 3, Name: "w", Cwd: "/x",
			Tabs: []session.Tab{{
				ID: 7, Focused: 9, Order: []int{9},
				Layout: &session.Node{Leaf: 9},
				Panes:  []session.Pane{{ID: 9, Cwd: "/x"}},
			}},
		}},
	}
	f.Validate()
	r := Restore(f)
	effs := r.Apply(CmdSplitFocused{Dir: layout.Horizontal})
	spawn, ok := findEffect[EffSpawnPane](effs)
	if !ok {
		t.Fatal("no spawn effect")
	}
	if spawn.Pane <= 9 {
		t.Fatalf("new pane id %d collides with restored ids", spawn.Pane)
	}
	effs = r.Apply(CmdNewWorkspace{Cwd: "/y"})
	ws := r.Workspaces[1]
	if ws.ID <= 3 {
		t.Fatalf("workspace id %d collides", ws.ID)
	}
	if ws.Tabs[0].ID <= 7 {
		t.Fatalf("tab id %d collides", ws.Tabs[0].ID)
	}
}
