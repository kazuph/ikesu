package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

func nativePaintFixture(t *testing.T) (*machineClient, *os.File, <-chan []byte, *endpoint.GraphicsTransmissionResult) {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outputRead, outputWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	terminal := make(chan []byte, 1)
	go func() { data, _ := io.ReadAll(outputRead); terminal <- data }()
	peer := endpoint.GraphicsPeer()
	conn := &endpointConnection{writer: &writer{conn: write, available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- conn.writeQueued(ctx) }()
	t.Cleanup(func() {
		cancel()
		read.Close()
		write.Close()
		outputWrite.Close()
		outputRead.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("native fixture writer remained running")
		}
	})
	in := &render.Input{State: model.New(t.TempDir(), 160, 45)}
	pid := in.State.FocusedPane()
	in.Panes = map[int]*render.PaneSnap{pid: {}}
	m := &machineClient{out: outputWrite, active: "local", order: []string{"local"}, size: tui.Winsize{Cols: 160, Rows: 45}, settings: &localSettings{},
		endpoints: map[string]*managedEndpoint{"local": {snapshot: in, epoch: 7, connection: conn, profile: machine.Profile{Enabled: true}}}}
	identity := endpoint.GraphicsTransmission{TransferID: 9, ImageID: 17}
	asset := endpoint.GraphicsSurfaceAsset{Pane: pid, Placement: kitty.Placement{Image: kitty.Image{ID: 41, NativeID: 17, Width: 2, Height: 3, Format: 32}, Cols: 2, Rows: 3}}
	m.nativeTransfer = &nativeViewerTransfer{identity: identity, asset: asset, epoch: 7, connection: conn}
	if err := m.frames.ReserveNativeImage(17); err != nil {
		t.Fatal(err)
	}
	return m, read, terminal, &endpoint.GraphicsTransmissionResult{GraphicsTransmission: identity, Success: true}
}

func TestNativePlacedResultRejectsChangedFocusBeforeDeferredPaint(t *testing.T) {
	m, wire, terminal, result := nativePaintFixture(t)
	m.inputBatch = true
	if err := m.sendGraphicsFileResult(m.endpoints["local"].connection, result); err != nil {
		t.Fatal(err)
	}
	m.endpoints["local"].epoch++
	m.inputBatch = false
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	wire.SetReadDeadline(time.Now().Add(5 * time.Second))
	var response msg
	if err := json.NewDecoder(wire).Decode(&response); err != nil || response.GraphicsResult == nil || response.GraphicsResult.Success || response.GraphicsResult.GraphicsTransmission != result.GraphicsTransmission {
		t.Fatal("changed focus was acknowledged as placed", response, err)
	}
	m.out.Close()
	if output := <-terminal; bytes.Contains(output, []byte("a=p,i=17,")) {
		t.Fatalf("changed focus still placed the old transfer: %q", output)
	}
}

func TestNativePlacedResultRequiresSuccessfulTerminalWrite(t *testing.T) {
	m, wire, _, result := nativePaintFixture(t)
	m.out.Close()
	if err := m.sendGraphicsFileResult(m.endpoints["local"].connection, result); err == nil {
		t.Fatal("closed terminal was reported as a successful native placement")
	}
	if m.nativeTransfer.sent {
		t.Fatal("failed terminal write marked the transfer sent")
	}
	wire.SetReadDeadline(time.Now())
	var data [1]byte
	if n, err := wire.Read(data[:]); n != 0 || err == nil {
		t.Fatal("failed terminal write emitted a success response", n, err)
	}
}

func TestNativePlacedResultWaitsForMatchingResizeSurface(t *testing.T) {
	m, wire, _, result := nativePaintFixture(t)
	m.out.Close()
	output, err := os.CreateTemp(t.TempDir(), "native-resize")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	m.out = output
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	before, _ := output.Seek(0, 1)
	m.size = tui.Winsize{Cols: 140, Rows: 40}
	if err := m.sendGraphicsFileResult(m.endpoints["local"].connection, result); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output.Name())
	if err != nil || bytes.Contains(data[before:], []byte("\x1b_G")) || m.nativeTransfer.sent {
		t.Fatal("pending resize changed native images or acknowledged an unplaced image", err)
	}
	m.endpoints["local"].snapshot.State.Cols = 140
	m.endpoints["local"].snapshot.State.Rows = 40
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	wire.SetReadDeadline(time.Now().Add(5 * time.Second))
	decoder := json.NewDecoder(wire)
	for {
		var response msg
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		if response.Type == endpoint.GraphicsResultMessage {
			if response.GraphicsResult == nil || !response.GraphicsResult.Success {
				t.Fatal("fresh resize surface was not acknowledged", response)
			}
			break
		}
	}
	data, err = os.ReadFile(output.Name())
	if err != nil || !bytes.Contains(data[before:], []byte("a=p,i=17,")) {
		t.Fatal("success arrived without the native placement", err)
	}
}
