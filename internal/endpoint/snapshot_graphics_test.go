package endpoint

import (
	"bytes"
	"image/color"
	"image/png"
	"math/rand"
	"testing"

	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

func TestSnapshotCompressesLargeRawGraphicsWithoutChangingPixels(t *testing.T) {
	for _, format := range []int{24, 32} {
		width, height := 1024, 256
		channels := format / 8
		pixels := make([]byte, width*height*channels)
		for i := 0; i < len(pixels); i += channels {
			pixels[i], pixels[i+1], pixels[i+2] = byte(i/width), byte(i/height), 31
			if channels == 4 {
				pixels[i+3] = byte(i / 4)
			}
		}
		state := model.New(t.TempDir(), 160, 45)
		pane := &render.PaneSnap{Graphics: []kitty.Placement{{Image: kitty.Image{ID: 42, Format: format, Width: width, Height: height, Data: pixels}, Cols: 80, Rows: 20}}}
		in := render.Input{State: state, Panes: map[int]*render.PaneSnap{1: pane}}
		var encoder SnapshotEncoder
		var decoder SnapshotDecoder
		for update := 0; update < 2; update++ {
			data, err := encoder.Encode(in)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decoder.Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			image := decoded.Panes[1].Graphics[0].Image
			if image.Format != 100 || len(image.Data)*2 >= len(pixels) {
				t.Fatalf("large compressible image was not reduced: format=%d bytes=%d", image.Format, len(image.Data))
			}
			actual, err := png.Decode(bytes.NewReader(image.Data))
			if err != nil {
				t.Fatal(err)
			}
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					i := (y*width + x) * channels
					want := color.NRGBA{R: pixels[i], G: pixels[i+1], B: pixels[i+2], A: 255}
					if channels == 4 {
						want.A = pixels[i+3]
					}
					if got := color.NRGBAModel.Convert(actual.At(x, y)).(color.NRGBA); got != want {
						t.Fatalf("pixel %d,%d = %+v want %+v", x, y, got, want)
					}
				}
			}
			if pane.Graphics[0].Image.Format != format || !bytes.Equal(pane.Graphics[0].Image.Data, pixels) {
				t.Fatal("wire compression changed producer data")
			}
			// Reusing a producer buffer must not reuse a stale compressed image.
			pixels[0] ^= 255
		}
	}
}

func TestSnapshotKeepsIncompressibleAndSmallImagesRaw(t *testing.T) {
	for _, size := range [][2]int{{1024, 256}, {4, 2}} {
		pixels := make([]byte, size[0]*size[1]*4)
		rand.New(rand.NewSource(7)).Read(pixels)
		in := render.Input{State: model.New(t.TempDir(), 160, 45), Panes: map[int]*render.PaneSnap{1: {Graphics: []kitty.Placement{{Image: kitty.Image{ID: 42, Format: 32, Width: size[0], Height: size[1], Data: pixels}}}}}}
		var encoder SnapshotEncoder
		var decoder SnapshotDecoder
		data, err := encoder.Encode(in)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decoder.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		image := got.Panes[1].Graphics[0].Image
		if image.Format != 32 || !bytes.Equal(image.Data, pixels) {
			t.Fatal("image without the required PNG savings changed")
		}
	}
}
