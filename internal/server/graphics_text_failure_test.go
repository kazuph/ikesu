package server

import (
	"errors"
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
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/tui"
)

func TestTextSendFailureDoesNotCommitRevisionOrBaseline(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "gtxfail-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	ln, err := net.Listen("unix", filepath.Join(dir, "sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	peer, err := net.Dial("unix", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	conn, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	state := model.New(".", 80, 24)
	pid := state.FocusedPane()
	app := tui.NewApp(state, runtime.NewSupervisor(), out, ".")
	c := &clientConn{conn: conn, viewer: app.NewViewer(out, 80, 24, func(string) {})}
	s := &Server{app: app, endpointPeer: endpoint.GraphicsPeer()}
	s.installGraphicsSnapshotSink(c, endpoint.GraphicsPeer())
	in := render.Input{State: state, Panes: map[int]*render.PaneSnap{pid: {Grid: [][]uv.Cell{{{Content: "a", Width: 1}}}, Graphics: []kitty.Placement{{Image: kitty.Image{ID: 1, Format: 32, Width: 1, Height: 1, Data: []byte{1, 2, 3, 255}}}}}}}
	if _, err := c.graphics.encoder.Encode(in); err != nil {
		t.Fatal(err)
	}
	old := captureGraphicsText(in)
	c.graphics.textSurface = old
	c.graphics.revision = 5
	c.graphics.awaiting = 3
	c.graphics.until = time.Now().Add(graphicsReceiptWait)
	until := c.graphics.until
	c.close()
	in.Panes[pid].Grid = [][]uv.Cell{{{Content: "b", Width: 1}}}
	if err := s.sendGraphicsSnapshot(c, in); !errors.Is(err, net.ErrClosed) {
		t.Fatal("expected real closed-connection failure", err)
	}
	if c.graphics.revision != 5 || c.graphics.awaiting != 3 || c.graphics.until != until || c.graphics.textSurface != old {
		t.Fatal("failed text send committed flow")
	}
	// A second text encoding must still compare with a, not the failed b.
	in.Panes[pid].Grid = [][]uv.Cell{{{Content: "a", Width: 1}}}
	data, err := c.graphics.encoder.EncodeText(in)
	if err != nil {
		t.Fatal(err)
	}
	_ = data
	// The endpoint's own test additionally decodes this baseline transition.
	if c.graphics.encoder.SentGraphicsPatch() {
		t.Fatal("failure advanced images")
	}
}
