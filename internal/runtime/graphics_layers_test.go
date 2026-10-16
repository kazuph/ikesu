package runtime

import (
	"bytes"
	"testing"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/kitty"
)

func TestGraphicsLayersPreservePixelsAndPTYNamespace(t *testing.T) {
	p := &Pane{graphicsEnabled: true}
	sup := &Supervisor{panes: map[int]*Pane{1: p}}
	frame := func(layer string) api.PaneGraphicsSetParams {
		return api.PaneGraphicsSetParams{Format: "rgb", ImageWidth: 2, ImageHeight: 1, LayerID: &layer,
			Placement: api.PaneGraphicsPlacement{GridCols: 4, GridRows: 2}}
	}
	data := []byte{255, 0, 0, 0, 255, 0}
	if failure := sup.SetGraphicsLayer(1, frame("z"), data, 0); failure != nil {
		t.Fatal(failure)
	}
	first := p.Graphics()[0].Image.ID
	if failure := sup.SetGraphicsLayer(1, frame("a"), data, 0); failure != nil {
		t.Fatal(failure)
	}
	images := p.Graphics()
	if len(images) != 2 || images[1].Image.ID != first || images[0].Image.ID == first {
		t.Fatalf("adding a layer changed an existing image ID: %+v", images)
	}
	p.graphics = &kitty.Store{}
	p.graphics.Handle([]byte("Ga=T,f=24,s=2,v=1,i=4294967295,c=4,r=2,C=1;AP8AAP8A"), 2, 2, 8, 16, false)
	images = p.Graphics()
	ids := map[uint32]bool{}
	for _, placement := range images {
		if ids[placement.Image.ID] {
			t.Fatalf("producer layer collided with PTY image ID: %+v", images)
		}
		ids[placement.Image.ID] = true
	}
	if len(images) != 3 || !bytes.Equal(images[0].Image.Data, []byte{0, 255, 0, 0, 255, 0}) {
		t.Fatalf("producer image replaced PTY pixels: %+v", images)
	}
	bad := frame("z")
	bad.Format, bad.ImageWidth, bad.ImageHeight = "rgba", 1<<31, 1<<31
	if failure := sup.SetGraphicsLayer(1, bad, nil, 0); failure == nil {
		t.Fatal("overflowed pixel dimensions accepted")
	}
	bgra := frame("a")
	bgra.Format = "bgra"
	if failure := sup.SetGraphicsLayer(1, bgra, []byte{0, 0, 255, 255, 0, 255, 0, 255}, 0); failure != nil {
		t.Fatal(failure)
	}
	if got := p.Graphics()[1].Image.Data; !bytes.Equal(got, []byte{255, 0, 0, 255, 0, 255, 0, 255}) {
		t.Fatalf("BGRA channel conversion=%v", got)
	}
}

func TestGraphicsLayerOwnershipPreventsStaleDisconnect(t *testing.T) {
	p := &Pane{graphicsEnabled: true}
	sup := &Supervisor{panes: map[int]*Pane{1: p}}
	layer := "stream"
	frame := api.PaneGraphicsSetParams{Format: "rgb", ImageWidth: 1, ImageHeight: 1, LayerID: &layer}
	if failure := sup.SetGraphicsLayer(1, frame, []byte{0, 255, 0}, 1); failure != nil {
		t.Fatal(failure)
	}
	if failure := sup.SetGraphicsLayer(1, frame, []byte{255, 0, 0}, 2); failure == nil || failure.Code != "stream_conflict" {
		t.Fatalf("concurrent stream replaced its peer: %+v", failure)
	}
	p.ClearGraphicsLayer(&layer, 1)
	if failure := sup.SetGraphicsLayer(1, frame, []byte{255, 0, 0}, 2); failure != nil {
		t.Fatal(failure)
	}
	p.ClearGraphicsLayer(&layer, 1)
	if len(p.Graphics()) != 1 || !bytes.Equal(p.Graphics()[0].Image.Data, []byte{255, 0, 0}) {
		t.Fatal("old stream's disconnect deleted its successor's image")
	}
}
