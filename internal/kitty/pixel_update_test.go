package kitty

import (
	"bytes"
	"testing"
)

func TestTransmitPixelsRetainsExistingPlacements(t *testing.T) {
	var store Store
	store.Handle([]byte("Ga=T,f=24,s=2,v=1,i=42,p=7,c=4,r=2,C=1;/wAAAP8A"), 6, 4, 8, 16, false)
	store.Handle([]byte("Ga=p,i=42,p=8,c=2,r=1,C=1;"), 12, 8, 8, 16, true)
	store.Handle([]byte("Ga=p,i=42,p=9,U=1,c=4,r=2;"), 0, 0, 8, 16, false)
	before := append([]Placement(nil), store.Placements...)
	response, cols, rows, handled := store.Handle([]byte("Ga=t,f=24,s=2,v=1,i=42;AAD/AP8A"), 30, 20, 8, 16, false)
	if !handled || response != "\x1b_Gi=42;OK\x1b\\" || cols != 0 || rows != 0 {
		t.Fatalf("pixel update moved the cursor or failed: %q %d %d %t", response, cols, rows, handled)
	}
	if len(store.Placements) != len(before) {
		t.Fatalf("pixel update removed placements: got %d, want %d", len(store.Placements), len(before))
	}
	for i, placement := range store.Placements {
		if placement.ID != before[i].ID || placement.X != before[i].X || placement.Y != before[i].Y || placement.Cols != before[i].Cols || placement.Rows != before[i].Rows || placement.Alternate != before[i].Alternate || placement.Virtual != before[i].Virtual {
			t.Fatalf("placement geometry changed: before=%+v after=%+v", before[i], placement)
		}
		if !bytes.Equal(placement.Image.Data, []byte{0, 0, 255, 0, 255, 0}) {
			t.Fatalf("placement retained stale pixels: %+v", placement.Image)
		}
	}
	store.Handle([]byte("Ga=t,f=24,s=2,v=1,i=42;AP8A"), 0, 0, 8, 16, false)
	if !bytes.Equal(store.Images[42].Data, []byte{0, 0, 255, 0, 255, 0}) || len(store.Placements) != len(before) {
		t.Fatal("invalid pixel data mutated the displayed image")
	}
}
