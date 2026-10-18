package kitty

import (
	"strings"
	"testing"
)

func TestSourceCropPlacementIsAccepted(t *testing.T) {
	var store Store
	reply, _, _, _ := store.Handle([]byte("Ga=T,f=24,s=1,v=1,i=7,x=0,w=1;AQID"), 0, 0, 8, 16, false)
	if !strings.Contains(reply, ";OK") || len(store.Placements) != 1 {
		t.Fatalf("source-crop placement rejected: reply=%q placements=%d", reply, len(store.Placements))
	}
}
