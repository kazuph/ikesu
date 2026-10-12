package kitty

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestVirtualPlacementAndPlaceholderCoordinates(t *testing.T) {
	var store Store
	response, cols, rows, _ := store.Handle([]byte("Ga=T,U=1,q=2,f=24,s=1,v=1,i=33554474,p=7,c=4,r=2;AP8A"), 9, 5, 0, 0, false)
	if response != "" || cols != 0 || rows != 0 || len(store.Placements) != 1 || !store.Placements[0].Virtual {
		t.Fatalf("virtual placement must remain invisible and not move cursor: %q %d %d %+v", response, cols, rows, store)
	}
	style := uv.Style{Fg: ansi.IndexedColor(42), UnderlineColor: ansi.IndexedColor(7)}
	var previous PlaceholderCell
	for col, text := range []string{"\U0010eeee\u0305\u0305\u030e", "\U0010eeee\u0305\u030d", "\U0010eeee\u0305", "\U0010eeee"} {
		decoded := DecodePlaceholder(uv.Cell{Content: text, Style: style}, previous)
		if !decoded.Valid || decoded.ImageID != 33554474 || decoded.PlacementID != 7 || decoded.Row != 0 || decoded.Col != col {
			t.Fatalf("placeholder col %d: %+v", col, decoded)
		}
		previous = decoded
	}
	trueColor := uv.Style{Fg: PlaceholderColor(0xabcdef), UnderlineColor: PlaceholderColor(0x123456)}
	decoded := DecodePlaceholder(uv.Cell{Content: PlaceholderText(1, 2, 0xfeabcdef), Style: trueColor}, PlaceholderCell{})
	if decoded.ImageID != 0xfeabcdef || decoded.PlacementID != 0x123456 || decoded.Row != 1 || decoded.Col != 2 {
		t.Fatalf("32-bit image ID and true-color placement: %+v", decoded)
	}
	store.Scroll(false, 0, 0, 80, 24, -1)
	if store.Placements[0].Y != 5 || len(store.Visible(true)) != 1 {
		t.Fatal("virtual prototypes have no screen/scroll location", store)
	}
	store.Handle([]byte("Ga=d,d=A;"), 0, 0, 0, 0, false)
	if len(store.Placements) != 1 || len(store.Images) != 1 {
		t.Fatal("visible-placement deletion removed virtual prototype", store)
	}
	store.Handle([]byte("Ga=d,d=I,i=33554474;"), 0, 0, 0, 0, false)
	if len(store.Placements) != 0 || len(store.Images) != 0 {
		t.Fatal("image-ID deletion retained virtual prototype", store)
	}
}
