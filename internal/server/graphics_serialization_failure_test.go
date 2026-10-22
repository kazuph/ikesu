package server

import (
	"encoding/json"
	"image/color"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
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

func TestSnapshotGobFailureReportsErrorAndClosesRealSocket(t *testing.T) {
	for _, textOnly := range []bool{false, true} {
		name := "normal"
		if textOnly {
			name = "text"
		}
		t.Run(name, func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "ikesugob-")
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
			defer conn.Close()
			out, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			state := model.New(".", 80, 24)
			pid := state.FocusedPane()
			app := tui.NewApp(state, runtime.NewSupervisor(), out, ".")
			c := &clientConn{conn: conn, viewer: app.NewViewer(out, 80, 24, func(string) {})}
			s := &Server{app: app, endpointPeer: endpoint.GraphicsPeer(), clients: map[*clientConn]bool{c: true}, client: c}
			s.installGraphicsSnapshotSink(c, endpoint.GraphicsPeer())
			in := render.Input{State: state, Panes: map[int]*render.PaneSnap{pid: {Grid: [][]uv.Cell{{{Content: "a", Width: 1}}}, Graphics: []kitty.Placement{{Image: kitty.Image{ID: 1, Format: 32, Width: 1, Height: 1, Data: []byte{1, 2, 3, 255}}}}}}}
			if _, err := c.graphics.encoder.Encode(in); err != nil {
				t.Fatal(err)
			}
			c.graphics.textSurface = captureGraphicsText(in)
			c.graphics.revision = 5
			if textOnly {
				c.graphics.awaiting = 3
				c.graphics.until = time.Now().Add(graphicsReceiptWait)
			}
			// This real standard-library color implements color.Color but is not
			// registered in the connection's gob protocol. Do not approximate it.
			in.Panes[pid].Grid[0][0].Style.Fg = color.Alpha{A: 255}
			err = s.sendGraphicsSnapshot(c, in)
			if err == nil || !strings.Contains(err.Error(), "type not registered") {
				t.Fatalf("gob failure=%v", err)
			}
			if !c.closed || s.client != nil || s.clients[c] || c.graphics.revision != 5 {
				t.Fatal("failed serialization retained connection or committed revision")
			}
			if err := peer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			var message clientMsg
			decoder := json.NewDecoder(peer)
			if err := decoder.Decode(&message); err != nil {
				t.Fatal(err)
			}
			if message.Type != "error" || !strings.Contains(message.Reason, "endpoint_snapshot_invalid: gob: type not registered") {
				t.Fatalf("error message=%+v", message)
			}
			if err := decoder.Decode(&message); err != io.EOF {
				t.Fatalf("connection not closed after explicit error: %v", err)
			}
		})
	}
}
