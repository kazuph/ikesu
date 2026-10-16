package runtime

import (
	"reflect"
	"testing"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/kitty"
)

func TestNativeGraphicsPreparationDoesNotPublishAndPreservesOwnership(t *testing.T) {
	p := &Pane{graphicsEnabled: true, cellWidth: 8, cellHeight: 16}
	s := &Supervisor{panes: map[int]*Pane{1: p}}
	frame := api.PaneGraphicsSetParams{Format: "rgba", ImageWidth: 100, ImageHeight: 33,
		Placement: api.PaneGraphicsPlacement{ViewportCol: -2, ViewportRow: 3}}
	placement, replace, failure := s.NativeGraphicsPlacement(1, frame, 17, 1)
	if failure != nil || replace != 0 || placement.Image.ID == 0 || placement.Cols != 13 || placement.Rows != 3 || placement.X != -2 || placement.Y != 3 {
		t.Fatalf("prepared=%+v replace=%d failure=%v", placement, replace, failure)
	}
	if got := p.Graphics(); len(got) != 0 {
		t.Fatal("preparation published an unreceived image", got)
	}
	if failure := s.SetNativeGraphicsLayer(1, frame, placement, 1); failure != nil {
		t.Fatal(failure)
	}
	got := p.Graphics()
	if len(got) != 1 || !reflect.DeepEqual(got[0], placement) || len(got[0].Image.Data) != 0 {
		t.Fatalf("native descriptor changed during commit: %+v", got)
	}
	if _, _, failure := s.NativeGraphicsPlacement(1, frame, 18, 2); failure == nil || failure.Code != "stream_conflict" {
		t.Fatal("foreign preparation bypassed stream ownership", failure)
	}
	if failure := p.ClearGraphicsLayer(nil, 2); failure == nil || failure.Code != "stream_conflict" {
		t.Fatal("foreign clear removed native layer", failure)
	}
	next, replace, failure := s.NativeGraphicsPlacement(1, frame, 18, 1)
	if failure != nil || replace != placement.Image.ID || next.Image.ID != placement.Image.ID {
		t.Fatalf("replacement changed source identity: %+v %d %v", next, replace, failure)
	}
	if failure := s.SetNativeGraphicsLayer(1, frame, next, 1); failure != nil {
		t.Fatal(failure)
	}
	if failure := p.ClearGraphicsLayer(nil, 1); failure != nil || len(p.Graphics()) != 0 {
		t.Fatal("stream cleanup retained native descriptor", failure)
	}
}

func TestNativeGraphicsAdmissionRechecksChangesWhileTerminalReceives(t *testing.T) {
	p := &Pane{graphicsEnabled: true, cellWidth: 8, cellHeight: 16}
	s := &Supervisor{panes: map[int]*Pane{1: p}}
	frame := api.PaneGraphicsSetParams{Format: "rgba", ImageWidth: 1, ImageHeight: 1}
	placement, _, failure := s.NativeGraphicsPlacement(1, frame, 17, 1)
	if failure != nil {
		t.Fatal(failure)
	}
	if failure := s.SetGraphicsLayer(1, frame, []byte{1, 2, 3, 255}, 2); failure != nil {
		t.Fatal(failure)
	}
	if failure := s.SetNativeGraphicsLayer(1, frame, placement, 1); failure == nil || failure.Code != "stream_conflict" {
		t.Fatal("receipt overwrote a newer stream owner", failure)
	}
	if got := p.Graphics(); len(got) != 1 || got[0].Image.NativeID != 0 || got[0].Image.Data[0] != 1 {
		t.Fatal("failed commit changed current image", got)
	}
	p.ClearGraphicsLayer(nil, 2)
	p.graphicsEnabled = false
	if failure := s.SetNativeGraphicsLayer(1, frame, placement, 1); failure == nil || failure.Code != "feature_disabled" {
		t.Fatal("receipt bypassed disabled graphics", failure)
	}
	p.graphicsEnabled = true
	delete(s.panes, 1)
	if failure := s.SetNativeGraphicsLayer(1, frame, placement, 1); failure == nil || failure.Code != api.ErrPaneNotFound {
		t.Fatal("receipt resurrected removed pane", failure)
	}
}

func TestNativeGraphicsDirectBoundaryDoesNotConsumeInlineBudget(t *testing.T) {
	s := &Supervisor{panes: map[int]*Pane{}}
	inline := api.PaneGraphicsSetParams{Format: "rgba", ImageWidth: 2048, ImageHeight: 2048}
	for pid := 1; pid <= 5; pid++ {
		s.panes[pid] = &Pane{graphicsEnabled: true, cellWidth: 8, cellHeight: 16}
		if pid <= 4 {
			if failure := s.SetGraphicsLayer(pid, inline, make([]byte, api.MaxGraphicsFileInlineBytes), 0); failure != nil {
				t.Fatal(failure)
			}
		}
	}
	frame := api.PaneGraphicsSetParams{Format: "rgba", ImageWidth: 10240, ImageHeight: 10240}
	placement, _, failure := s.NativeGraphicsPlacement(5, frame, 17, 1)
	if failure != nil {
		t.Fatal("400 MiB native descriptor was charged to 64 MiB inline budget", failure)
	}
	if failure := s.SetNativeGraphicsLayer(5, frame, placement, 1); failure != nil {
		t.Fatal(failure)
	}
	if got := s.panes[5].Graphics(); len(got) != 1 || len(got[0].Image.Data) != 0 || got[0].Image.NativeID != 17 {
		t.Fatal("direct frame allocated or lost its descriptor", got)
	}
	frame.ImageWidth++
	if _, _, failure := s.NativeGraphicsPlacement(5, frame, 18, 1); failure == nil || failure.Code != "invalid_image" {
		t.Fatal("400 MiB boundary overflow accepted", failure)
	}
	frame.ImageWidth = 1
	frame.ImageHeight = 1
	if failure := s.SetGraphicsLayer(5, frame, []byte{1, 2, 3, 255}, 1); failure == nil || failure.Code != "graphics_budget_exceeded" {
		t.Fatal("native replacement bypassed full inline budget", failure)
	}
}

func TestNativeGraphicsRejectsUnsupportedDescriptorsAndFullPane(t *testing.T) {
	p := &Pane{graphicsEnabled: true, cellWidth: 8, cellHeight: 16}
	s := &Supervisor{panes: map[int]*Pane{1: p}}
	frame := api.PaneGraphicsSetParams{Format: "rgba", ImageWidth: 1, ImageHeight: 1}
	for _, change := range []func(*api.PaneGraphicsSetParams){
		func(f *api.PaneGraphicsSetParams) { f.Format = "bgra" },
		func(f *api.PaneGraphicsSetParams) { f.ImageHeight = 0 },
		func(f *api.PaneGraphicsSetParams) { id := "secondary"; f.LayerID = &id },
	} {
		bad := frame
		change(&bad)
		if _, _, failure := s.NativeGraphicsPlacement(1, bad, 17, 1); failure == nil || failure.Code != "invalid_image" {
			t.Fatal("unsupported native descriptor accepted", failure)
		}
	}
	if _, _, failure := s.NativeGraphicsPlacement(1, frame, 0, 1); failure == nil {
		t.Fatal("zero native ID accepted")
	}
	p.cellWidth = 0
	if _, _, failure := s.NativeGraphicsPlacement(1, frame, 17, 1); failure == nil || failure.Code != "cell_size_unavailable" {
		t.Fatal("unknown cell geometry accepted", failure)
	}
	p.cellWidth = 8
	p.graphicsLayers = map[string]*graphicsLayer{}
	for i := range api.MaxGraphicsLayers {
		id := string(rune('a' + i))
		p.graphicsLayers[id] = &graphicsLayer{placement: kitty.Placement{Image: kitty.Image{ID: uint32(i + 1)}}}
	}
	if _, _, failure := s.NativeGraphicsPlacement(1, frame, 17, 1); failure == nil || failure.Code != "layer_limit" {
		t.Fatal("full pane admitted native upload", failure)
	}
}
