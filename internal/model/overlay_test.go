package model

import (
	"encoding/json"
	"github.com/kazuph/goterrace/internal/layout"
	"reflect"
	"testing"
)

func TestOverlayProjectionAndSnapshotInvariant(t *testing.T) {
	s := New("/tmp", 160, 45)
	s.Apply(CmdSplitFocused{Dir: layout.Vertical})
	tab := s.ActiveWorkspace().TabRef()
	target := tab.Layout.Focused()
	before, _ := json.Marshal(s.Snapshot())
	normal := tab.PaneRects(s.PanesArea())
	order := append([]int{}, tab.Layout.PaneIDs()...)
	fingerprint := tab.Layout.Fingerprint()
	effects := s.Apply(CmdOpenOverlay{TargetPaneID: target, Cwd: "/tmp", Argv: []string{"/bin/cat"}})
	if len(effects) != 2 || !s.OverlayVisible() {
		t.Fatal(effects, s.Overlay)
	}
	projected := s.DisplayPaneRects()
	if len(projected) != len(normal) {
		t.Fatal(projected)
	}
	for i, p := range projected {
		want := normal[i]
		if want.ID == target {
			want.ID = s.Overlay.PaneID
		}
		if p.Rect != want.Rect || p.ID != want.ID {
			t.Fatalf("rect/order changed: %v -> %v", normal, projected)
		}
	}
	for _, cmd := range []Command{CmdFocusDir{Dir: layout.NavLeft}, CmdCyclePane{Next: true}, CmdFocusPane{Pane: order[0]}} {
		s.Apply(cmd)
	}
	during, _ := json.Marshal(s.Snapshot())
	if string(before) != string(during) || tab.Layout.Fingerprint() != fingerprint || !reflect.DeepEqual(order, tab.Layout.PaneIDs()) {
		t.Fatalf("snapshot/layout changed: %s -> %s", before, during)
	}
	overlayID := s.Overlay.PaneID
	for _, cmd := range []Command{CmdRenamePane{Pane: overlayID, Label: "transient"}, CmdSplitPane{Pane: overlayID, Dir: layout.Vertical, Ratio: 0.5}, CmdZoomPane{Pane: overlayID, Mode: "on"}, CmdMovePane{Pane: overlayID}} {
		s.Apply(cmd)
	}
	refused, _ := json.Marshal(s.Snapshot())
	if string(before) != string(refused) {
		t.Fatal("overlay layout operation changed snapshot")
	}
	s.Apply(CmdCloseOverlay{})
	after, _ := json.Marshal(s.Snapshot())
	if string(before) != string(after) || s.OverlayVisible() {
		t.Fatalf("snapshot changed after close: %s -> %s", before, after)
	}
	s.Apply(CmdSplitFocused{Dir: layout.Horizontal})
	if s.FocusedPane() <= overlayID {
		t.Fatal("runtime pane ID reused", s.FocusedPane(), overlayID)
	}
}
func TestOverlayRequiresActiveTabAndTarget(t *testing.T) {
	s := New("/tmp", 120, 40)
	target := s.FocusedPane()
	s.Apply(CmdNewTab{Workspace: 0})
	before, _ := json.Marshal(s.Snapshot())
	if effects := s.Apply(CmdOpenOverlay{TargetPaneID: target}); len(effects) != 0 || s.Overlay != nil {
		t.Fatal("inactive target accepted")
	}
	s.Apply(CmdOpenOverlay{TargetPaneID: 999})
	after, _ := json.Marshal(s.Snapshot())
	if string(before) != string(after) {
		t.Fatal("missing target mutated snapshot")
	}
}
func TestOverlayOwnerClosureKillsTransient(t *testing.T) {
	s := New("/tmp", 120, 40)
	s.Apply(CmdSplitFocused{Dir: layout.Vertical})
	target := s.FocusedPane()
	s.Apply(CmdOpenOverlay{TargetPaneID: target})
	id := s.Overlay.PaneID
	effects := s.Apply(CmdClosePane{Pane: target})
	killed := false
	for _, e := range effects {
		if e, ok := e.(EffKillPane); ok && e.Pane == id {
			killed = true
		}
	}
	if !killed || s.Overlay != nil {
		t.Fatal("orphaned overlay", effects, s.Overlay)
	}
}
