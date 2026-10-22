package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
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

func TestReceiptPopupHistoryTextAndScrollOnRealSocket(t *testing.T) {
	teststate.Lock(t)
	config := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(config, []byte("onboarding=false\n[experimental]\nkitty_graphics=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "IKESU_CONFIG", config)
	dir, done := startTestEndpoint(t, endpoint.GraphicsPeer())
	defer stopServer(t, dir, done)
	c := dialClient(t, dir)
	defer c.conn.Close()
	c.snapshots.GraphicsPatches = true
	c.send(t, clientMsg{Peer: endpoint.GraphicsPeer(), Type: "hello", Cols: 100, Rows: 40, Xpixel: 800, Ypixel: 640})
	ack := func(revision uint64) {
		c.send(t, clientMsg{Type: "control", Kind: endpoint.GraphicsPatchAck, Data: strconv.FormatUint(revision, 10)})
	}
	decode := func() (render.Input, error) {
		for {
			line, err := c.rd.ReadBytes('\n')
			if err != nil {
				return render.Input{}, err
			}
			var message clientMsg
			if err = json.Unmarshal(line, &message); err != nil {
				return render.Input{}, err
			}
			if message.Type != "snapshot" {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(message.Data)
			if err != nil {
				return render.Input{}, err
			}
			in, err := c.snapshots.Decode(data)
			if err != nil {
				return in, err
			}
			regions := render.Compose(in)
			regions.Frame = nil
			c.send(t, clientMsg{Type: "rendered", Rendered: regions, FocusEpoch: message.FocusEpoch})
			return in, nil
		}
	}
	read := func(timeout time.Duration, pred func(render.Input) bool) render.Input {
		t.Helper()
		_ = c.conn.SetReadDeadline(time.Now().Add(timeout))
		for {
			in, err := decode()
			if err != nil {
				t.Fatal(err)
			}
			if pred(in) {
				return in
			}
			if len(in.GraphicsPatches) > 0 {
				ack(in.SurfaceRevision)
			}
		}
	}
	read(5*time.Second, func(in render.Input) bool { return len(in.Panes) > 0 })
	tile := firstPaneID(t, dir)
	var opened api.CommandExecutedResult
	apiOK(t, dir, "pane.command.execute", map[string]any{"pane_id": tile, "action": "popup", "width": 50, "height": 12, "command": "i=0; while [ $i -lt 60 ]; do echo HISTORY_$i; i=$((i+1)); done; exec /bin/sh"}, &opened)
	popupText := func(in render.Input, word string) bool {
		if !in.State.PopupVisible() {
			return false
		}
		p := in.Panes[in.State.Popup.PaneID]
		if p == nil {
			return false
		}
		var text strings.Builder
		for _, row := range p.Grid {
			for _, cell := range row {
				text.WriteString(cell.Content)
			}
			text.WriteByte('\n')
		}
		return strings.Contains(text.String(), word)
	}
	read(5*time.Second, func(in render.Input) bool { return popupText(in, "HISTORY_59") })
	var info api.PopupTerminalResult
	apiOK(t, dir, "popup.get", api.PopupTarget{TerminalID: *opened.PopupTerminalID}, &info)
	if info.Scroll.MaxOffsetFromBottom == 0 {
		t.Fatal("popup has no real history", info)
	}
	pixels := bytes.Repeat([]byte{12, 34, 56, 255}, 512*256)
	set := func() {
		apiOK(t, dir, api.MethodPaneGraphicsSet, map[string]any{"pane_id": tile, "format": "rgba", "image_width": 512, "image_height": 256, "data_base64": base64.StdEncoding.EncodeToString(pixels), "placement": map[string]any{"grid_cols": 4, "grid_rows": 2}}, nil)
	}
	set()
	read(time.Second, func(in render.Input) bool {
		for _, p := range in.Panes {
			if len(p.Graphics) > 0 {
				return true
			}
		}
		return false
	})
	pixels[0]++
	set()
	first := read(time.Second, func(in render.Input) bool { return len(in.GraphicsPatches) > 0 })
	pixels[4]++
	set()
	c.send(t, clientMsg{Type: "input", Data: encode64([]byte("printf 'POPUP_TEXT_OK\\n'\r"))})
	text := read(graphicsReceiptWait/2, func(in render.Input) bool {
		return in.SurfaceRevision > first.SurfaceRevision && popupText(in, "POPUP_TEXT_OK")
	})
	if len(text.GraphicsPatches) != 0 {
		t.Fatal("popup text advanced image")
	}
	for id, p := range text.Panes {
		for i := range p.Graphics {
			if n, _ := c.snapshots.GraphicsPayloadSize(id, i); n != 0 {
				t.Fatal("popup text carried pixels")
			}
		}
	}
	// Scroll the viewer through its real UI input; popup.scroll targets the
	// session view, while this socket owns an independent viewer scroll position.
	popupRect := text.State.PopupRect()
	wheel := fmt.Sprintf("\x1b[<64;%d;%dM", popupRect.X+3, popupRect.Y+3)
	c.send(t, clientMsg{Type: "input", Data: encode64([]byte(wheel))})
	blocked := func() {
		t.Helper()
		_ = c.conn.SetReadDeadline(time.Now().Add(graphicsReceiptWait / 8))
		for {
			in, err := decode()
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					return
				}
				t.Fatal(err)
			}
			if in.Panes[in.State.Popup.PaneID].ScrollOff != 0 || len(in.GraphicsPatches) > 0 {
				t.Fatal("scroll or image escaped receipt wait")
			}
		}
	}
	blocked()
	ack(first.SurfaceRevision - 1)
	blocked()
	ack(first.SurfaceRevision)
	latest := read(time.Second, func(in render.Input) bool {
		return in.State.PopupVisible() && in.Panes[in.State.Popup.PaneID].ScrollOff == 3 && len(in.GraphicsPatches) > 0
	})
	if latest.SurfaceRevision <= text.SurfaceRevision || c.snapshots.MissingGraphicsKey != nil {
		t.Fatal("latest popup/image not delivered")
	}
	ack(latest.SurfaceRevision)
	t.Logf("popup history=%d viewport=%d text_revision=%d first_image=%d latest=%d", info.Scroll.MaxOffsetFromBottom, info.Scroll.ViewportRows, text.SurfaceRevision, first.SurfaceRevision, latest.SurfaceRevision)
}
