package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/color"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

func TestTextSnapshotsRealSocketRetainOffscreenBaseWithoutControlsOrUploads(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "ikesutext-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	listener, err := net.Listen("unix", filepath.Join(dir, "sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	left, err := net.Dial("unix", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer left.Close()
	right, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer right.Close()
	peer := endpoint.GraphicsPeer()
	c := &endpointConnection{stream: left, reader: bufio.NewReader(left), writer: &writer{conn: left, available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	c.snapshots.GraphicsPatches = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.writeQueued(ctx) }()
	defer func() { cancel(); left.Close(); <-done }()
	pixels := bytes.Repeat([]byte{3, 4, 5, 255}, 512*256)
	image := kitty.Image{ID: 1, Format: 32, Width: 512, Height: 256, Data: pixels}
	held := image
	held.ID = 2
	in := render.Input{State: model.New(".", 80, 24), Panes: map[int]*render.PaneSnap{1: {Grid: [][]uv.Cell{{{Content: "a", Width: 1}}}, Graphics: []kitty.Placement{{Image: image, Cols: 2, Rows: 2}, {Image: held, X: 500, Cols: 2, Rows: 2}}}}}
	encoder := endpoint.SnapshotEncoder{GraphicsPatches: true}
	display := render.Encoder{}
	receive := func(text bool) msg {
		t.Helper()
		in.SurfaceRevision++
		var data []byte
		if text {
			data, err = encoder.EncodeText(in)
		} else {
			data, err = encoder.Encode(in)
		}
		if err != nil {
			t.Fatal(err)
		}
		written := make(chan error, 1)
		go func() {
			written <- json.NewEncoder(right).Encode(msg{Type: "snapshot", Data: base64.StdEncoding.EncodeToString(data)})
		}()
		message, err := c.read()
		if err != nil {
			t.Fatal(err)
		}
		if err = <-written; err != nil {
			t.Fatal(err)
		}
		if text {
			encoder.CommitText()
		}
		output := display.Frame(render.Compose(*message.snapshot).Frame, 0, 0, false)
		if text {
			if bytes.Contains(output, []byte("\x1b_G")) {
				t.Fatalf("text retransferred or erased images: %q", output)
			}
			if len(message.snapshot.GraphicsPatches) > 0 || c.snapshots.MissingGraphicsKey != nil {
				t.Fatal("text required ACK/MISS")
			}
			for i := range message.snapshot.Panes[1].Graphics {
				if n, _ := c.snapshots.GraphicsPayloadSize(1, i); n != 0 {
					t.Fatal("text payload", n)
				}
			}
			_ = right.SetReadDeadline(time.Now().Add(render.TerminalStatusWait / 100))
			var value [1]byte
			if n, err := right.Read(value[:]); n != 0 || err == nil {
				t.Fatal("extra control on text", n, err)
			}
			_ = right.SetReadDeadline(time.Time{})
		}
		return message
	}
	receive(false)
	// All three existing grid paths: unchanged/reuse, changed delta, resized grid.
	saved := in.Panes[1].Graphics
	for i, text := range []string{"a", "b", "b"} {
		p := *in.Panes[1]
		p.Graphics = append([]kitty.Placement(nil), saved...)
		for j := range p.Graphics {
			p.Graphics[j].Image.Data = nil
		}
		p.Grid = [][]uv.Cell{{{Content: text, Width: 1}}}
		if i == 2 {
			p.Grid = append(p.Grid, []uv.Cell{{Content: "c", Width: 1}})
		}
		in.Panes = map[int]*render.PaneSnap{1: &p}
		got := receive(true)
		if len(got.snapshot.Panes[1].Graphics[1].Image.Data) == 0 {
			t.Fatal("offscreen image lost")
		}
	}
	p := *in.Panes[1]
	p.Graphics = append([]kitty.Placement(nil), saved...)
	changed := bytes.Clone(pixels)
	changed[0]++
	p.Graphics[1].Image.Data = changed
	in.Panes = map[int]*render.PaneSnap{1: &p}
	got := receive(false)
	if c.snapshots.MissingGraphicsKey != nil {
		t.Fatal("held patch missing base")
	}
	restored := got.snapshot.Panes[1].Graphics[1].Image
	if restored.Format == 100 {
		decoded, err := png.Decode(bytes.NewReader(restored.Data))
		if err != nil {
			t.Fatal(err)
		}
		for y := 0; y < restored.Height; y++ {
			for x := 0; x < restored.Width; x++ {
				c := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
				index := (y*restored.Width + x) * 4
				if !bytes.Equal([]byte{c.R, c.G, c.B, c.A}, changed[index:index+4]) {
					t.Fatal("held patch pixels differ", x, y)
				}
			}
		}
	} else if !bytes.Equal(restored.Data, changed) {
		t.Fatal("held patch pixels differ")
	}
	_ = right.SetReadDeadline(time.Now().Add(time.Second))
	var ack msg
	if err := json.NewDecoder(right).Decode(&ack); err != nil {
		t.Fatal(err)
	}
	if ack.Kind != endpoint.GraphicsPatchAck {
		t.Fatal("expected only next-image ACK", ack)
	}
}
