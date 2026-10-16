package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/kitty"
	"github.com/kazuph/goterrace/internal/machine"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/tui"
	"golang.org/x/term"
)

func TestGraphicsFileMachinePathUsesActualTerminalReplyAndPreservesKeys(t *testing.T) {
	clearGraphicsFileEnvironment(t)
	if err := os.Setenv("TERM_PROGRAM", "ghostty"); err != nil {
		t.Fatal(err)
	}
	master, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer terminal.Close()
	previous, err := term.MakeRaw(int(terminal.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	defer term.Restore(int(terminal.Fd()), previous)
	wireRead, wireWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer wireRead.Close()
	defer wireWrite.Close()
	peer := endpoint.GraphicsPeer()
	connection := &endpointConnection{writer: &writer{conn: wireWrite, available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- connection.writeQueued(ctx) }()
	events, stopInput, err := readMachineInput(terminal, ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stopInput()
	snapshot := &render.Input{State: model.New(t.TempDir(), 160, 45)}
	pid := snapshot.State.FocusedPane()
	snapshot.Panes = map[int]*render.PaneSnap{pid: {}}
	m := &machineClient{in: terminal, out: terminal, active: "local", size: tui.Winsize{Cols: 160, Rows: 45},
		settings: &localSettings{}, regions: render.Compose(*snapshot),
		order:     []string{"local"},
		endpoints: map[string]*managedEndpoint{"local": {connection: connection, snapshot: snapshot, epoch: 7, profile: machine.Profile{Enabled: true}}}}
	file := graphicsTransferFixture(t)
	file.SurfaceAsset, err = json.Marshal(endpoint.GraphicsSurfaceAsset{Pane: pid, Placement: kitty.Placement{
		Image: kitty.Image{ID: 41, NativeID: file.ImageID, Width: 2, Height: 3, Format: 32}, Cols: 2, Rows: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.graphicsFileEvent("local", msg{Type: endpoint.GraphicsFileMessage, GraphicsFile: &file, FocusEpoch: 7}); err != nil {
		t.Fatal(err)
	}
	master.SetReadDeadline(time.Now().Add(5 * time.Second))
	wantUpload := "\x1b7\x1b_G" + file.Control + ",t=f;" + base64.StdEncoding.EncodeToString([]byte(file.Path)) + "\x1b\\\x1b8"
	upload := make([]byte, len(wantUpload))
	if _, err := io.ReadFull(master, upload); err != nil || !bytes.Equal(upload, []byte(wantUpload)) {
		t.Fatalf("actual terminal upload=%q error=%v", upload, err)
	}
	placed := make(chan struct{}, 1)
	go func() {
		var output bytes.Buffer
		buffer := make([]byte, 4096)
		for {
			n, err := master.Read(buffer)
			output.Write(buffer[:n])
			if bytes.Contains(output.Bytes(), []byte("a=p,i=17,")) {
				select {
				case placed <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()
	wireRead.SetReadDeadline(time.Now().Add(5 * time.Second))
	decoder := json.NewDecoder(wireRead)
	var started msg
	if err := decoder.Decode(&started); err != nil || started.Type != endpoint.GraphicsStartedMessage || started.GraphicsTransmission == nil || started.GraphicsTransmission.TransferID != file.TransferID {
		t.Fatal("missing Started before terminal response", started, err)
	}
	if _, err := master.Write([]byte("\x1b_Gi=17;OK\x1b\\k")); err != nil {
		t.Fatal(err)
	}
	m.inputBatch = true
	for range 2 {
		select {
		case event := <-events:
			if err := m.input(event); err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("actual terminal reply/key did not reach the input framer")
		}
	}
	var key msg
	if err := decoder.Decode(&key); err != nil {
		t.Fatal(err)
	}
	typed, err := base64.StdEncoding.DecodeString(key.Data)
	if err != nil || key.Type != "input" || string(typed) != "k" {
		t.Fatal("terminal reply leaked into pane input or ordinary key was lost", key, string(typed), err)
	}
	if !m.paintQueued || m.nativeTransfer == nil || m.nativeTransfer.sent {
		t.Fatal("terminal OK escaped before the deferred native paint")
	}
	if err := wireRead.SetReadDeadline(time.Now()); err != nil {
		t.Fatal(err)
	}
	var extra [1]byte
	if n, err := wireRead.Read(extra[:]); n != 0 || err == nil {
		t.Fatal("result was written before placement", n, err)
	}
	wireRead.SetReadDeadline(time.Now().Add(5 * time.Second))
	m.inputBatch = false
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-placed:
	case <-time.After(5 * time.Second):
		t.Fatal("native placement was not written to the terminal")
	}
	var result msg
	for {
		if err := decoder.Decode(&result); err != nil {
			t.Fatal(err)
		}
		if result.Type == endpoint.GraphicsResultMessage {
			break
		}
		if result.Type != "rendered" {
			t.Fatal("unexpected message before placed success", result)
		}
	}
	if result.GraphicsResult == nil || !result.GraphicsResult.Success || result.GraphicsResult.TransferID != file.TransferID || result.GraphicsResult.ImageID != file.ImageID {
		t.Fatal("placement did not release the correlated success", result)
	}
	if len(snapshot.Panes[pid].Graphics) != 0 {
		t.Fatal("provisional placement mutated the authoritative server snapshot")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("graphics writer did not close")
	}
}
