package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
	"time"

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
		if !bytes.Equal(message.snapshot.Panes[1].Graphics[0].Image.Data, pixels) {
			t.Fatal("receipt preceded exact reconstruction")
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
