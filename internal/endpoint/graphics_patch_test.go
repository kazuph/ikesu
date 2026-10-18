package endpoint

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/kazuph/goterrace/internal/kitty"
	"github.com/kazuph/goterrace/internal/render"
)

func TestGraphicsPatchExactPixelsAndFullImageBoundary(t *testing.T) {
	for _, format := range []int{24, 32} {
		base := kitty.Image{ID: 42, Format: format, Width: 1024, Height: 256, Data: make([]byte, 1024*256*(format/8))}
		for i := range base.Data {
			base.Data[i] = byte(i)
		}
		next := base
		next.Data = bytes.Clone(base.Data)
		for y := 10; y < 14; y++ {
			for x := 10; x < 14; x++ {
				next.Data[(y*next.Width+x)*(format/8)+1] ^= 255
			}
		}
		patch, err := makeGraphicsPatch(1, base, next)
		if err != nil || patch == nil || len(patch.Rects) != 1 {
			t.Fatalf("small update: patch=%v err=%v", patch, err)
		}
		r := patch.Rects[0]
		if r.X != 10 || r.Y != 10 || r.Width != 4 || r.Height != 4 {
			t.Fatalf("rectangle=%+v", r)
		}
		wire, err := json.Marshal(patch)
		if err != nil {
			t.Fatal(err)
		}
		var received render.GraphicsImagePatch
		if err := json.Unmarshal(wire, &received); err != nil {
			t.Fatal(err)
		}
		got, err := applyGraphicsPatch(1, base, received)
		if err != nil {
			t.Fatal(err)
		}
		want, err := canonicalGraphics(next)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Data, want.Data) {
			t.Fatal("patch changed pixels")
		}
		if bytes.Equal(base.Data, next.Data) {
			t.Fatal("patch application changed the producer base")
		}
		if _, err := applyGraphicsPatch(2, base, received); err == nil {
			t.Fatal("another pane's base accepted")
		}
		if _, err := applyGraphicsPatch(1, next, received); err == nil {
			t.Fatal("incorrect base revision accepted")
		}
		received.Rects[0].X = next.Width
		if _, err := applyGraphicsPatch(1, base, received); err == nil {
			t.Fatal("out of bounds rectangle accepted")
		}
		next.Data = bytes.Repeat([]byte{17}, len(base.Data))
		if patch, err := makeGraphicsPatch(1, base, next); err != nil || patch != nil {
			t.Fatalf("wide change must send ordinary full image: %v %v", patch, err)
		}
	}
}

func TestGraphicsPatchSixtyPercentAndUnchangedImage(t *testing.T) {
	base := kitty.Image{ID: 42, Format: 32, Width: 1024, Height: 256, Data: make([]byte, 1024*256*4)}
	patch, err := makeGraphicsPatch(1, base, base)
	if err != nil || patch == nil || len(patch.Rects) != 0 || !sameGraphicsKey(patch.Key, patch.Base) {
		t.Fatal("unchanged image did not retain its base", err)
	}
	// Exactly 60% at integer pixel precision remains a patch; the next pixel
	// in the bounding rectangle crosses the documented full-image threshold.
	base.Width, base.Height = 1000, 200
	base.Data = make([]byte, base.Width*base.Height*4)
	for _, width := range []int{600, 601} {
		next := base
		next.Data = bytes.Clone(base.Data)
		next.Data[0] = 1
		next.Data[((base.Height-1)*base.Width+width-1)*4] = 1
		patch, err := makeGraphicsPatch(1, base, next)
		if err != nil || (patch != nil) != (width == 600) {
			t.Fatalf("width=%d patch=%v err=%v", width, patch, err)
		}
	}
}
