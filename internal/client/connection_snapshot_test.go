package client

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

func TestEndpointReaderReconstructsSnapshotsBeforePosting(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	connection := &endpointConnection{reader: bufio.NewReader(reader)}
	state := model.New(t.TempDir(), 80, 24)
	var encoder endpoint.SnapshotEncoder
	for _, name := range []string{"first", "second"} {
		state.ActiveWorkspace().Name = name
		data, err := encoder.Encode(render.Input{State: state})
		if err != nil {
			t.Fatal(err)
		}
		written := make(chan error, 1)
		go func() {
			written <- json.NewEncoder(writer).Encode(msg{Type: "snapshot", Data: base64.StdEncoding.EncodeToString(data)})
		}()
		message, err := connection.read()
		if err != nil {
			t.Fatal(err)
		}
		if err := <-written; err != nil {
			t.Fatal(err)
		}
		if message.snapshot == nil || message.snapshot.State.ActiveWorkspace().Name != name || message.Data != "" {
			t.Fatal("reader left reconstruction to the terminal event loop")
		}
	}
	go func() { _ = json.NewEncoder(writer).Encode(msg{Type: "snapshot", Data: "corrupt"}) }()
	if _, err := connection.read(); err == nil {
		t.Fatal("corrupt wire snapshot accepted")
	}
}
