package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/color"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/kitty"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

func TestEndpointReaderAcknowledgesReconstructedGraphicsRevision(t *testing.T) {
	inReader, inWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inReader.Close()
	defer inWriter.Close()
	ackReader, ackWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer ackReader.Close()
	defer ackWriter.Close()
	peer := endpoint.GraphicsPeer()
	c := &endpointConnection{reader: bufio.NewReader(inReader), writer: &writer{conn: ackWriter, available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	c.snapshots.GraphicsPatches = true
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.writeQueued(ctx) }()
	defer func() { cancel(); <-done }()
	pixels := bytes.Repeat([]byte{12, 34, 56, 255}, 1024*256)
	pane := &render.PaneSnap{Graphics: []kitty.Placement{{Image: kitty.Image{ID: 42, Format: 32, Width: 1024, Height: 256, Data: pixels}}}}
	in := render.Input{State: model.New(t.TempDir(), 80, 24), Panes: map[int]*render.PaneSnap{1: pane}}
	var encoder endpoint.SnapshotEncoder
	encoder.GraphicsPatches = true
	var display render.Encoder
	var received kitty.Store
	outer := vt.NewEmulator(80, 24)
	defer outer.Close()
	outer.RegisterApcHandler(func(data []byte) bool {
		position := outer.CursorPosition()
		_, _, _, handled := received.Handle(data, position.X, position.Y, 8, 16, false)
		return handled
	})
	for revision := uint64(1); revision <= 2; revision++ {
		in.SurfaceRevision = revision
		data, err := encoder.Encode(in)
		if err != nil {
			t.Fatal(err)
		}
		written := make(chan error, 1)
		go func() {
			written <- json.NewEncoder(inWriter).Encode(msg{Type: "snapshot", Data: base64.StdEncoding.EncodeToString(data)})
		}()
		message, err := c.read()
		if err != nil {
			t.Fatal(err)
		}
		if err := <-written; err != nil {
			t.Fatal(err)
		}
		placement := message.snapshot.Panes[1].Graphics[0]
		if placement.Image.Format != 100 || len(placement.Image.Data)*2 >= len(pixels) {
			t.Fatal("reconstructed display image expanded back to raw pixels")
		}
		placement.Cols, placement.Rows = 60, 20
		frame := render.NewFrame(80, 24)
		frame.Graphics = []render.Graphic{{Pane: 1, Placement: placement}}
		output := display.Frame(frame, 0, 0, false)
		if !bytes.Contains(output, []byte("f=100,")) || len(output) >= len(pixels) {
			t.Fatal("outer terminal upload did not retain PNG compression")
		}
		if _, err := outer.Write(output); err != nil {
			t.Fatal(err)
		}
		if len(received.Images) != 1 {
			t.Fatalf("outer terminal images=%d", len(received.Images))
		}
		for _, image := range received.Images {
			decoded, err := png.Decode(bytes.NewReader(image.Data))
			if err != nil {
				t.Fatal(err)
			}
			for y := 0; y < image.Height; y++ {
				for x := 0; x < image.Width; x++ {
					got := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
					index := (y*image.Width + x) * 4
					if !bytes.Equal([]byte{got.R, got.G, got.B, got.A}, pixels[index:index+4]) {
						t.Fatal("outer terminal received changed pixels")
					}
				}
			}
		}
		pixels[0] ^= 255
	}
	if err := ackReader.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var ack msg
	if err := json.NewDecoder(ackReader).Decode(&ack); err != nil {
		t.Fatal(err)
	}
	if ack.Type != "control" || ack.Kind != endpoint.GraphicsPatchAck || ack.Data != "2" {
		t.Fatalf("graphics receipt=%+v", ack)
	}
}
