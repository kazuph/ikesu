package client

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

func TestFirstVisibleFrameRegistersRegionsBeforeConcurrentInput(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	server.SetDeadline(time.Now().Add(5 * time.Second))
	client.SetDeadline(time.Now().Add(5 * time.Second))
	reader, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer output.Close()
	reader.SetReadDeadline(time.Now().Add(5 * time.Second))
	peer := endpoint.Current()
	w := &writer{conn: client, available: peer.Generation, methods: peer.Methods}
	painted := make(chan error, 1)
	go func() {
		painted <- w.firstSnapshot(output, bytes.Repeat([]byte("x"), 1<<20), &windowTitleState{}, render.Input{State: model.New("/tmp", 80, 24)}, &render.Output{}, 0)
	}()
	// A byte on the real output pipe proves painting has entered the locked
	// section. Concurrent input must wait even while the remaining paint blocks.
	if _, err := io.ReadFull(reader, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	input := make(chan error, 1)
	go func() { input <- w.send(msg{Type: "input", Data: "Y2xpY2s="}) }()
	drained := make(chan struct{})
	go func() { io.Copy(io.Discard, reader); close(drained) }()
	decoder := json.NewDecoder(server)
	for _, want := range []string{"rendered", "input"} {
		var got msg
		if err := decoder.Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.Type != want {
			t.Fatalf("first paint message order: got %s, want %s", got.Type, want)
		}
	}
	if err := <-painted; err != nil {
		t.Fatal(err)
	}
	if err := <-input; err != nil {
		t.Fatal(err)
	}
	output.Close()
	<-drained
}
