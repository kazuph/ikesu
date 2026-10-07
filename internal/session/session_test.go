package session

import (
	"os"
	"path/filepath"
	"testing"
)

func twoPaneFile() *File {
	return &File{
		Active: 0, SidebarVisible: true,
		NextPaneID: 2, NextWorkspaceID: 1, NextTabID: 1,
		Workspaces: []Workspace{{
			ID: 1, Name: "workspace 1", Cwd: "/work",
			Tabs: []Tab{{
				ID: 1, Focused: 1, Order: []int{1, 2},
				Layout: &Node{
					Leaf: -1, Direction: "v", Ratio: 0.5,
					First:  &Node{Leaf: 1},
					Second: &Node{Leaf: 2},
				},
				Panes: []Pane{
					{ID: 1, Cwd: "/work", Agent: "claude", SessionID: "abc-123", SessionKind: "id", Title: "t"},
					{ID: 2, Cwd: "/work/sub", Label: "shell"},
				},
			}},
		}},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SessionFileName)
	f := twoPaneFile()
	if err := Save(path, f); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.NextPaneID != 2 || len(got.Workspaces) != 1 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	p := got.Workspaces[0].Tabs[0].Panes[0]
	if p.Agent != "claude" || p.SessionID != "abc-123" || p.Title != "t" {
		t.Fatalf("agent record lost: %+v", p)
	}
	if got.SavedAt == "" {
		t.Fatal("saved_at not set")
	}
}

func TestLoadMissing(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || f != nil {
		t.Fatalf("missing file = (%v, %v), want nil,nil", f, err)
	}
}

// A corrupt file must fail closed: an error, never a partial guess.
func TestLoadCorruptFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), SessionFileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("corrupt file must error")
	}
}

// Atomic writes never leave a truncated file behind.
func TestSaveAtomicNoTmpLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SessionFileName)
	if err := Save(path, twoPaneFile()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
}

// G5: valid ids are preserved unchanged.
func TestValidateKeepsValidIDs(t *testing.T) {
	f := twoPaneFile()
	if w := f.Validate(); len(w) != 0 {
		t.Fatalf("unexpected warnings: %v", w)
	}
	got := map[int]bool{}
	f.EachPane(func(_ *Workspace, _ *Tab, p *Pane) { got[p.ID] = true })
	if !got[1] || !got[2] {
		t.Fatalf("ids changed: %v", got)
	}
}

// G5: a duplicated pane id discards the whole saved numbering and
// renumbers every pane.
func TestValidateRenumbersDuplicateIDs(t *testing.T) {
	f := twoPaneFile()
	f.Workspaces[0].Tabs[0].Panes[1].ID = 1 // duplicate
	f.Workspaces[0].Tabs[0].Layout.Second.Leaf = 1
	w := f.Validate()
	if len(w) == 0 {
		t.Fatal("expected renumber warning")
	}
	seen := map[int]bool{}
	f.EachPane(func(_ *Workspace, _ *Tab, p *Pane) {
		if p.ID <= 0 || seen[p.ID] {
			t.Fatalf("id %d still invalid", p.ID)
		}
		seen[p.ID] = true
	})
	if len(seen) != 2 {
		t.Fatalf("panes lost: %v", seen)
	}
}

// G5: a missing pane record for a layout leaf is an inconsistency.
func TestValidateRenumbersOnMissingRecord(t *testing.T) {
	f := twoPaneFile()
	f.Workspaces[0].Tabs[0].Panes = f.Workspaces[0].Tabs[0].Panes[:1]
	if w := f.Validate(); len(w) == 0 {
		t.Fatal("expected warning for missing record")
	}
}

// A tab with a broken layout is dropped, not guessed.
func TestValidateDropsBrokenLayout(t *testing.T) {
	f := twoPaneFile()
	f.Workspaces[0].Tabs[0].Layout = &Node{Leaf: -1, Direction: "v", Ratio: 0.5, First: &Node{Leaf: 1}}
	w := f.Validate()
	if len(w) == 0 {
		t.Fatal("expected drop warning")
	}
	if len(f.Workspaces) != 0 {
		t.Fatalf("workspace with no usable tabs must be dropped: %+v", f.Workspaces)
	}
}

func TestSafeSessionID(t *testing.T) {
	ok := []string{"abc-123", "a.b_c", "018f-4e2a"}
	for _, v := range ok {
		if !SafeSessionID(v) {
			t.Fatalf("%q should be safe", v)
		}
	}
	bad := []string{"", "-x", "last", "--last", "a b", "a/b", "a:b", string(make([]byte, 200))}
	for _, v := range bad {
		if SafeSessionID(v) {
			t.Fatalf("%q should be unsafe", v)
		}
	}
}
