package endpoint

import (
	"bytes"
	"testing"

	"github.com/kazuph/goterrace/internal/kitty"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

func TestSnapshotPatchRoundTripMissingBaseAndFullRecovery(t *testing.T) {
	var encoder SnapshotEncoder
	encoder.GraphicsPatches = true
	var decoder SnapshotDecoder
	decoder.GraphicsPatches = true
	pixels := bytes.Repeat([]byte{12, 34, 56, 255}, 1024*256)
	pane := &render.PaneSnap{Graphics: []kitty.Placement{{Image: kitty.Image{ID: 42, Format: 32, Width: 1024, Height: 256, Data: pixels}}}}
	in := render.Input{State: model.New(t.TempDir(), 160, 45), Panes: map[int]*render.PaneSnap{1: pane}}
	transfer := func() (render.Input, int) {
		t.Helper()
		data, err := encoder.Encode(in)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decoder.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		return got, len(data)
	}
	first, _ := transfer()
	if len(first.GraphicsPatches) != 0 || decoder.MissingGraphicsKey != nil {
		t.Fatal("first image was not ordinary full data")
	}
	unchanged, _ := transfer()
	initialImage, unchangedImage := first.Panes[1].Graphics[0].Image, unchanged.Panes[1].Graphics[0].Image
	if initialImage.Format != unchangedImage.Format || !bytes.Equal(initialImage.Data, unchangedImage.Data) {
		t.Fatal("unchanged patch altered the image representation and would cause an upload")
	}
	pixels[0] ^= 255
	second, size := transfer()
	if len(second.GraphicsPatches) != 1 || size >= imageCompressionMinimum || !bytes.Equal(second.Panes[1].Graphics[0].Image.Data, pixels) {
		t.Fatal("small patch did not reconstruct exact pixels")
	}
	// Lose only image bases, retaining this real gob connection's type state.
	decoder.bases = nil
	pixels[4] ^= 255
	missing, _ := transfer()
	if decoder.MissingGraphicsKey == nil || len(missing.GraphicsPatches) != 1 || !sameGraphicsKey(*decoder.MissingGraphicsKey, missing.GraphicsPatches[0].Patch.Key) {
		t.Fatal("missing patch was discarded or no miss key was reported")
	}
	encoder.ResetGraphics()
	recovered, _ := transfer()
	if len(recovered.GraphicsPatches) != 0 || decoder.MissingGraphicsKey != nil {
		t.Fatal("miss did not recover through an ordinary full image")
	}
	canonical, err := canonicalGraphics(recovered.Panes[1].Graphics[0].Image)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical.Data, pixels) {
		t.Fatal("full recovery pixels changed")
	}
	pixels[8] ^= 255
	resumed, _ := transfer()
	if len(resumed.GraphicsPatches) != 1 || !bytes.Equal(resumed.Panes[1].Graphics[0].Image.Data, pixels) {
		t.Fatal("patches did not resume after full recovery")
	}
}
