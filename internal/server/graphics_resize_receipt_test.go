package server

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestGraphicsReceiptWaitTracksResizeAndDragOnRealSocket(t *testing.T) {
	teststate.Lock(t)
	for _, completion := range []string{"ack", "stale-ack", "miss", "expiry", "reconnect", "other-view"} {
		t.Run(completion, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "graphics.toml")
			if err := os.WriteFile(config, []byte("onboarding=false\n[experimental]\nkitty_graphics=true\n"), 0600); err != nil {
				t.Fatal(err)
			}
			teststate.Setenv(t, "IKESU_CONFIG", config)
			dir, done := startTestEndpoint(t, endpoint.GraphicsPeer())
			defer stopServer(t, dir, done)
			connect := func() *testConn {
				c := dialClient(t, dir)
				t.Cleanup(func() { c.conn.Close() })
				c.snapshots.GraphicsPatches = true
				c.send(t, clientMsg{Peer: endpoint.GraphicsPeer(), Type: "hello", Cols: 106, Rows: 38, Xpixel: 106 * 8, Ypixel: 38 * 16})
				return c
			}
			c := connect()
			read := func(c *testConn, timeout time.Duration, predicate func(render.Input) bool) render.Input {
				t.Helper()
				_ = c.conn.SetReadDeadline(time.Now().Add(timeout))
				for {
					message, err := readClientMsg(c.rd)
					if err != nil {
						t.Fatal("snapshot observation", err)
					}
					if message.Type == "error" {
						t.Fatal(message.Reason)
					}
					if message.Type != "snapshot" {
						continue
					}
					data, err := decode64(message.Data)
					if err != nil {
						t.Fatal(err)
					}
					in, err := c.snapshots.Decode(data)
					if err != nil || c.snapshots.MissingGraphicsKey != nil {
						t.Fatal("actual image reconstruction", err, c.snapshots.MissingGraphicsKey)
					}
					regions := render.Compose(in)
					regions.Frame = nil
					c.send(t, clientMsg{Type: "rendered", Rendered: regions, FocusEpoch: message.FocusEpoch})
					if predicate(in) {
						return in
					}
				}
			}
			read(c, time.Second, func(in render.Input) bool { return len(in.Panes) > 0 })
			pane := firstPaneID(t, dir)
			pixels := bytes.Repeat([]byte{12, 34, 56, 255}, 512*256)
			set := func() {
				apiOK(t, dir, api.MethodPaneGraphicsSet, map[string]any{"pane_id": pane, "format": "rgba", "image_width": 512, "image_height": 256, "data_base64": base64.StdEncoding.EncodeToString(pixels), "placement": map[string]any{"grid_cols": 4, "grid_rows": 2}}, nil)
			}
			imagePixels := func(in render.Input) []byte {
				for _, p := range in.Panes {
					if len(p.Graphics) > 0 {
						return p.Graphics[0].Image.Data
					}
				}
				return nil
			}
			set()
			initial := read(c, time.Second, func(in render.Input) bool { return len(imagePixels(in)) > 0 })
			base := bytes.Clone(pixels)
			if !bytes.Equal(imagePixels(initial), base) {
				t.Fatal("initial image was not reconstructed exactly")
			}
			if len(initial.GraphicsPatches) > 0 {
				c.send(t, clientMsg{Type: "control", Kind: endpoint.GraphicsPatchAck, Data: strconv.FormatUint(initial.SurfaceRevision, 10)})
			}
			pixels[0]++
			set()
			pending := read(c, time.Second, func(in render.Input) bool {
				if len(in.GraphicsPatches) == 0 {
					return false
				}
				if bytes.Equal(imagePixels(in), pixels) {
					return true
				}
				// A redraw queued before set() can carry a valid no-change
				// patch for the previous image. ACK only its exact known bytes;
				// keep the requested update unacknowledged for the resize test.
				if !bytes.Equal(imagePixels(in), base) {
					t.Fatal("unexpected pixels in a queued image generation")
				}
				c.send(t, clientMsg{Type: "control", Kind: endpoint.GraphicsPatchAck, Data: strconv.FormatUint(in.SurfaceRevision, 10)})
				t.Logf("acknowledged queued previous image: revision=%d", in.SurfaceRevision)
				return false
			})
			if !bytes.Equal(imagePixels(pending), pixels) {
				t.Fatal("do not acknowledge a generation without exact pixels")
			}
			held := bytes.Clone(imagePixels(pending))
			started := time.Now()
			pixels[4]++
			set()
			c.send(t, clientMsg{Type: "resize", Cols: 160, Rows: 45, Xpixel: 160 * 8, Ypixel: 45 * 16})
			x, y := pending.State.PanesArea().X, pending.State.PanesArea().Y+5
			// The released coordinate differs from both motion coordinates.
			drag := fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<32;%d;%dM\x1b[<32;%d;%dM\x1b[<0;%d;%dm", x+1, y+1, x+5, y+1, x+7, y+1, x+9, y+1)
			c.send(t, clientMsg{Type: "input", Data: encode64([]byte(drag))})
			tracked := read(c, graphicsReceiptWait/2, func(in render.Input) bool {
				return in.State.Cols == 160 && in.State.Rows == 45 && in.State.SidebarArea().W == x+9
			})
			if len(tracked.GraphicsPatches) != 0 || !bytes.Equal(imagePixels(tracked), held) {
				t.Fatal("resize advanced the unacknowledged image generation")
			}
			for id, p := range tracked.Panes {
				for index := range p.Graphics {
					if size, _ := c.snapshots.GraphicsPayloadSize(id, index); size != 0 {
						t.Fatal("tracking frame transferred unconfirmed pixels", size)
					}
				}
			}
			ack := func(revision uint64) {
				c.send(t, clientMsg{Type: "control", Kind: endpoint.GraphicsPatchAck, Data: strconv.FormatUint(revision, 10)})
			}
			switch completion {
			case "stale-ack":
				ack(pending.SurfaceRevision - 1)
				c.send(t, clientMsg{Type: "resize", Cols: 250, Rows: 70, Xpixel: 250 * 8, Ypixel: 70 * 16})
				stale := read(c, graphicsReceiptWait/2, func(in render.Input) bool { return in.State.Cols == 250 && in.State.Rows == 70 })
				if len(stale.GraphicsPatches) != 0 || !bytes.Equal(imagePixels(stale), held) {
					t.Fatal("stale ACK advanced image pixels")
				}
				ack(pending.SurfaceRevision)
			case "miss":
				// The protocol accepts a negative receipt independently of payload.
				c.send(t, clientMsg{Type: "control", Kind: endpoint.GraphicsPatchMiss, Data: "ignored"})
			case "expiry":
				// Drop the real receipt and observe the existing deadline unchanged.
			case "reconnect":
				c.conn.Close()
				c = connect()
			case "other-view":
				other := connect()
				other.send(t, clientMsg{Type: "input", Data: encode64([]byte("printf 'OTHER_RECEIPT_%s\\n' VIEW\r"))})
				read(other, graphicsReceiptWait/2, func(in render.Input) bool {
					for _, p := range in.Panes {
						var text strings.Builder
						for _, row := range p.Grid {
							for _, cell := range row {
								text.WriteString(cell.Content)
							}
						}
						if strings.Contains(text.String(), "OTHER_RECEIPT_VIEW") {
							return true
						}
					}
					return false
				})
				ack(pending.SurfaceRevision)
			default:
				ack(pending.SurfaceRevision)
			}
			recovered := read(c, 3*graphicsReceiptWait, func(in render.Input) bool { return bytes.Equal(imagePixels(in), pixels) })
			if len(recovered.GraphicsPatches) != 0 {
				t.Fatal("a relayout recovered from a possibly invalid image base")
			}
			if completion == "expiry" {
				t.Logf("real dropped-receipt expiry: %s; configured wait: %s", time.Since(started), graphicsReceiptWait)
			}
		})
	}
}
