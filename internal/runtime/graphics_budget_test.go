package runtime

import (
	"fmt"
	"testing"

	"github.com/kazuph/goterrace/internal/api"
)

func TestGraphicsSessionLayerLimitAndReplacement(t *testing.T) {
	s := NewSupervisor()
	for pid := 1; pid <= 5; pid++ {
		s.panes[pid] = &Pane{graphicsEnabled: true}
	}
	frame := api.PaneGraphicsSetParams{Format: "rgba", ImageWidth: 1, ImageHeight: 1}
	for pid := 1; pid <= 4; pid++ {
		for index := 0; index < 16; index++ {
			id := fmt.Sprint(index)
			frame.LayerID = &id
			if failure := s.SetGraphicsLayer(pid, frame, []byte{byte(pid), byte(index), 0, 255}, 0); failure != nil {
				t.Fatal("64 valid layers were not retained", pid, index, failure)
			}
		}
	}
	frame.LayerID = nil
	if failure := s.SetGraphicsLayer(5, frame, []byte{1, 2, 3, 255}, 0); failure == nil || failure.Code != "layer_limit" {
		t.Fatal("65th session layer", failure)
	}
	id := "0"
	frame.LayerID = &id
	if failure := s.SetGraphicsLayer(1, frame, []byte{7, 8, 9, 255}, 0); failure != nil {
		t.Fatal("replacement consumed an extra layer", failure)
	}
	if got := s.panes[1].Graphics()[0].Image.Data; len(got) != 4 || got[0] != 7 {
		t.Fatal("replacement pixels were not installed", got)
	}
	s.panes[1].ClearGraphicsLayer(&id, 0)
	frame.LayerID = nil
	if failure := s.SetGraphicsLayer(5, frame, []byte{1, 2, 3, 255}, 0); failure != nil {
		t.Fatal("clearing did not release layer capacity", failure)
	}
}

func TestGraphicsSessionInlineBudgetExcludesReplacement(t *testing.T) {
	s := NewSupervisor()
	for pid := 1; pid <= 5; pid++ {
		s.panes[pid] = &Pane{graphicsEnabled: true}
	}
	frame := api.PaneGraphicsSetParams{Format: "rgba", ImageWidth: 2048, ImageHeight: 2048}
	for pid := 1; pid <= 4; pid++ {
		pixels := make([]byte, 2048*2048*4)
		pixels[0] = byte(pid)
		if failure := s.SetGraphicsLayer(pid, frame, pixels, 0); failure != nil {
			t.Fatal("64 MiB boundary rejected", pid, failure)
		}
	}
	small := api.PaneGraphicsSetParams{Format: "rgba", ImageWidth: 1, ImageHeight: 1}
	if failure := s.SetGraphicsLayer(5, small, []byte{1, 2, 3, 255}, 0); failure == nil || failure.Code != "graphics_budget_exceeded" {
		t.Fatal("64 MiB plus one pixel accepted", failure)
	}
	replacement := make([]byte, 2048*2048*4)
	replacement[0] = 9
	if failure := s.SetGraphicsLayer(1, frame, replacement, 0); failure != nil {
		t.Fatal("old frame bytes were counted during replacement", failure)
	}
	if got := s.panes[1].Graphics()[0].Image.Data; len(got) != len(replacement) || got[0] != 9 {
		t.Fatal("budget replacement did not retain new pixels")
	}
	s.panes[1].ClearGraphicsLayer(nil, 0)
	if failure := s.SetGraphicsLayer(5, frame, replacement, 0); failure != nil {
		t.Fatal("clear did not release inline budget", failure)
	}
}

func TestGraphicsSessionConcurrentLayerAdmissions(t *testing.T) {
	s := NewSupervisor()
	for pid := 1; pid <= 5; pid++ {
		s.panes[pid] = &Pane{graphicsEnabled: true}
	}
	results := make(chan *api.Error, 65)
	for index := 0; index < 65; index++ {
		go func(index int) {
			id := fmt.Sprint(index)
			frame := api.PaneGraphicsSetParams{Format: "rgba", ImageWidth: 1, ImageHeight: 1, LayerID: &id}
			results <- s.SetGraphicsLayer(index%5+1, frame, []byte{byte(index), 0, 0, 255}, 0)
		}(index)
	}
	accepted, rejected := 0, 0
	for range 65 {
		failure := <-results
		if failure == nil {
			accepted++
		} else if failure.Code == "layer_limit" {
			rejected++
		} else {
			t.Fatal(failure)
		}
	}
	retained := 0
	for _, pane := range s.panes {
		retained += len(pane.Graphics())
	}
	if accepted != 64 || rejected != 1 || retained != 64 {
		t.Fatalf("simultaneous admissions: accepted=%d rejected=%d retained=%d", accepted, rejected, retained)
	}
}
