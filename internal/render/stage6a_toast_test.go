package render

import (
	"github.com/kazuph/ikesu/internal/layout"
	"testing"
)

func TestStage6aJapaneseToastCellDimensions(t *testing.T) {
	for _, body := range []string{"", "本文"} {
		toast := &Toast{Title: "日本語", Body: body}
		r := ToastRect(toast, layout.Rect{W: 160, H: 45}, layout.Rect{})
		height := 3
		if body != "" {
			height = 4
		}
		if r.W != 12 || r.H != height || r.X != 148 || r.Y != 0 {
			t.Fatal(body, r)
		}
		f := NewFrame(160, 45)
		out := &Output{}
		drawToast(f, toast, out)
		if f.Cell(r.X, r.Y+r.H-1).Content != "└" || f.Cell(r.X+r.W-1, r.Y+r.H-1).Content != "┘" {
			t.Fatal("toast body overwrote border")
		}
	}
}
