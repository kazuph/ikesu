package render

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/kitty"
)

func TestNativeGraphicsKeepsReceivedPixelsAcrossHiddenPane(t *testing.T) {
	pixels := []byte{255, 0, 0, 255, 0, 255, 0, 255}
	path := filepath.Join(t.TempDir(), "source.rgba")
	if err := os.WriteFile(path, pixels, 0600); err != nil {
		t.Fatal(err)
	}
	var images kitty.Store
	outer := vt.NewEmulator(40, 20)
	defer outer.Close()
	outer.RegisterApcHandler(func(data []byte) bool {
		cursor := outer.CursorPosition()
		_, _, _, handled := images.Handle(data, cursor.X, cursor.Y, 8, 16, false)
		return handled
	})
	upload := fmt.Sprintf("\x1b_Ga=t,t=f,f=32,s=2,v=1,i=1,q=0;%s\x1b\\", base64.StdEncoding.EncodeToString([]byte(path)))
	outer.Write([]byte(upload))
	if !bytes.Equal(images.Images[1].Data, pixels) {
		t.Fatal("terminal did not receive the actual source file")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	var encoder Encoder
	if err := encoder.ReserveNativeImage(1); err != nil {
		t.Fatal(err)
	}
	image := kitty.Image{ID: 41, NativeID: 1, Format: 32, Width: 2, Height: 1}
	placement := kitty.Placement{Image: image, X: 2, Y: 3, Cols: 4, Rows: 2}
	panes := map[int]*PaneSnap{7: {Graphics: []kitty.Placement{placement}}}
	paint := func(visible bool) []byte {
		t.Helper()
		retired, err := encoder.ObserveNativeImages(panes)
		if err != nil {
			t.Fatal(err)
		}
		frame := NewFrame(40, 20)
		if visible {
			frame.Graphics = []Graphic{{Pane: 7, Placement: placement}}
		}
		data := append(encoder.Frame(frame, 0, 0, false), retired...)
		outer.Write(data)
		return data
	}
	first := paint(true)
	if bytes.Contains(first, []byte("a=t,")) || len(images.Placements) != 1 || images.Placements[0].Image.ID != 1 || images.Placements[0].X != 2 || images.Placements[0].Y != 3 {
		t.Fatalf("native source was reuploaded or misplaced: %q %+v", first, images.Placements)
	}
	hidden := paint(false)
	if bytes.Contains(hidden, []byte("d=I,")) || len(images.Placements) != 0 || !bytes.Equal(images.Images[1].Data, pixels) {
		t.Fatal("hidden pane lost its received source", string(hidden))
	}
	restored := paint(true)
	if bytes.Contains(restored, []byte("a=t,")) || len(images.Placements) != 1 || !bytes.Equal(images.Placements[0].Image.Data, pixels) {
		t.Fatal("restoring needed the deleted producer file", string(restored))
	}
	panes = nil
	paint(false)
	if len(images.Images) != 0 {
		t.Fatal("stream removal retained native source pixels")
	}
}

func TestNativeGraphicsIDsDoNotCollideWithInlineAndRejectUnknownAssets(t *testing.T) {
	var encoder Encoder
	if err := encoder.ReserveNativeImage(1); err != nil {
		t.Fatal(err)
	}
	if err := encoder.ReserveNativeImage(1); err == nil {
		t.Fatal("duplicate pending image accepted")
	}
	frame := NewFrame(20, 10)
	frame.Graphics = []Graphic{{Pane: 2, Placement: kitty.Placement{Image: kitty.Image{ID: 1, Format: 24, Width: 1, Height: 1, Data: []byte{255, 0, 0}}, Cols: 1, Rows: 1}}}
	data := encoder.Frame(frame, 0, 0, false)
	if !bytes.Contains(data, []byte("i=2,q=2,m=0;")) {
		t.Fatalf("inline upload reused reserved native ID: %q", data)
	}
	if err := encoder.ReserveNativeImage(2); err == nil {
		t.Fatal("native upload could overwrite an existing inline image")
	}
	if _, err := encoder.ObserveNativeImages(map[int]*PaneSnap{1: {Graphics: []kitty.Placement{{Image: kitty.Image{NativeID: 99}}}}}); err == nil {
		t.Fatal("unreceived native image accepted in a snapshot")
	}
	cleared := encoder.RestoreCursor()
	if !bytes.Contains(cleared, []byte("d=I,i=1,")) || !bytes.Contains(cleared, []byte("d=I,i=2,")) {
		t.Fatalf("client exit left native or inline sources behind: %q", cleared)
	}
}
