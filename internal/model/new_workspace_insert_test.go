package model

import "testing"

func TestNewWorkspaceInsertAfterActive(t *testing.T) {
	s := New("/a", 80, 24)
	s.Apply(CmdNewWorkspace{Cwd: "/b", Name: "b"})
	s.Apply(CmdNewWorkspace{Cwd: "/c", Name: "c"})
	// Select first workspace (index 0, "/a").
	s.Apply(CmdSelectWorkspace{Index: 0})

	// InsertAfterActive places the new workspace immediately after index 0 (at index 1) and focuses it.
	s.Apply(CmdNewWorkspace{Cwd: "/after-a", Name: "after-a", Section: SectionPersonal, InsertAfterActive: true})
	if len(s.Workspaces) != 4 {
		t.Fatalf("len=%d, want 4", len(s.Workspaces))
	}
	if s.Active != 1 {
		t.Fatalf("Active=%d, want 1", s.Active)
	}
	if got := []string{s.Workspaces[0].Cwd, s.Workspaces[1].Cwd, s.Workspaces[2].Cwd, s.Workspaces[3].Cwd}; got[1] != "/after-a" || got[2] != "/b" || got[3] != "/c" {
		t.Fatalf("order=%v, want [/a /after-a /b /c]", got)
	}
	if s.Workspaces[1].Section != SectionPersonal {
		t.Fatalf("section=%q, want %q", s.Workspaces[1].Section, SectionPersonal)
	}

	// Default (InsertAfterActive: false) still appends to the end.
	s.Apply(CmdSelectWorkspace{Index: 0})
	s.Apply(CmdNewWorkspace{Cwd: "/end", Name: "end"})
	if s.Active != 4 || s.Workspaces[4].Cwd != "/end" {
		t.Fatalf("default append broken: active=%d last=%q", s.Active, s.Workspaces[4].Cwd)
	}

	// InsertAfterActive with NoFocus keeps focus on the previously active workspace.
	s.Apply(CmdSelectWorkspace{Index: 1}) // "/after-a"
	s.Apply(CmdNewWorkspace{Cwd: "/after-after-a", Name: "after-after-a", NoFocus: true, InsertAfterActive: true})
	if s.Active != 1 || s.Workspaces[1].Cwd != "/after-a" || s.Workspaces[2].Cwd != "/after-after-a" {
		t.Fatalf("NoFocus insert broken: active=%d ws1=%q ws2=%q", s.Active, s.Workspaces[1].Cwd, s.Workspaces[2].Cwd)
	}
}
