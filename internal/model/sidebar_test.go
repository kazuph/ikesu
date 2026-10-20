package model

import (
	"github.com/kazuph/ikesu/internal/layout"
	"reflect"
	"testing"
)

func TestSidebarPresetsSnapshot(t *testing.T) {
	s := New("/tmp", 100, 30)
	s.SidebarMin, s.SidebarMax, s.SidebarDefault = 18, 36, 26
	for _, want := range []int{36, 18, 26} {
		s.Apply(CmdSidebarPreset{Preset: s.CyclePreset()})
		if s.ExpandedSidebarWidth() != want {
			t.Fatalf("width %d want %d", s.ExpandedSidebarWidth(), want)
		}
		r := Restore(s.Snapshot())
		if r.ExpandedSidebarWidth() != want {
			t.Fatal("snapshot width")
		}
	}
	s.SidebarMin, s.SidebarMax = 80, 10
	lo, hi, _ := s.SidebarBounds()
	if lo != 18 || hi != 72 {
		t.Fatal(lo, hi)
	}
}
func TestSidebarSectionsOrderCollapse(t *testing.T) {
	s := New("/tmp", 100, 30)
	for i := 0; i < 3; i++ {
		s.Apply(CmdNewWorkspace{})
	}
	for i, sec := range []Section{SectionWork, SectionNone, SectionFavorite, SectionPersonal} {
		s.Apply(CmdSetSection{Index: i, Section: sec})
	}
	if got := s.VisibleWorkspaceOrder(); !reflect.DeepEqual(got, []int{2, 0, 3, 1}) {
		t.Fatal(got)
	}
	s.Apply(CmdToggleSection{Section: SectionPersonal})
	if got := s.VisibleWorkspaceOrder(); !reflect.DeepEqual(got, []int{2, 0, 1}) {
		t.Fatal(got)
	}
	r := Restore(s.Snapshot())
	if !r.CollapsedSections[SectionPersonal] || r.Workspaces[2].Section != SectionFavorite {
		t.Fatal("snapshot")
	}
	s.Apply(CmdSetSection{Index: 0, Section: SectionPersonal})
	if s.CollapsedSections[SectionPersonal] {
		t.Fatal("destination must expand")
	}
}
func TestDuplicateWorkspaceLayoutCwds(t *testing.T) {
	s := New("/tmp", 100, 30)
	s.Apply(CmdSplitFocused{Dir: layout.Vertical})
	s.Apply(CmdNewTab{Workspace: 0})
	s.Apply(CmdRenameTab{Workspace: 0, Tab: 1, Label: "logs"})
	src := s.Workspaces[0]
	cwds := map[int]string{1: "/tmp/one", 2: "/tmp/two", 3: "/tmp/three"}
	effects := s.Apply(CmdDuplicateWorkspace{Index: 0, Focus: true, Cwds: cwds})
	dst := s.Workspaces[1]
	if dst.ID == src.ID || s.Active != 1 || dst.ActiveTab != src.ActiveTab || len(dst.Tabs) != 2 || dst.Tabs[1].Name != "logs" || dst.Tabs[0].Layout.PaneCount() != 2 {
		t.Fatal("structure")
	}
	seen := map[string]bool{}
	for _, e := range effects {
		if e, ok := e.(EffSpawnPane); ok {
			seen[e.Cwd] = true
			if e.Pane <= 3 {
				t.Fatal("id reused")
			}
		}
	}
	for _, c := range cwds {
		if !seen[c] {
			t.Fatal(c)
		}
	}
	before := len(s.Workspaces)
	if e := s.Apply(CmdDuplicateWorkspace{Index: 99}); len(e) != 0 || len(s.Workspaces) != before {
		t.Fatal("invalid index mutated state")
	}
}
