package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestGraphicsReceiptAndMissRecoveryOnRealSocket(t *testing.T) {
	teststate.Lock(t)
	config := filepath.Join(t.TempDir(), "graphics.toml")
	if err := os.WriteFile(config, []byte("onboarding=false\n[experimental]\nkitty_graphics=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "GTX_CONFIG", config)
	dir, done := startTestEndpoint(t, endpoint.GraphicsPeer())
	client := dialClient(t, dir)
	defer client.conn.Close()
	defer stopServer(t, dir, done)
	client.snapshots.GraphicsPatches = true
	client.send(t, clientMsg{Peer: endpoint.GraphicsPeer(), Type: "hello", Cols: 80, Rows: 24, Xpixel: 640, Ypixel: 384})
	ack := func(revision uint64) {
		client.send(t, clientMsg{Type: "control", Kind: endpoint.GraphicsPatchAck, Data: strconv.FormatUint(revision, 10)})
	}
	read := func(predicate func(render.Input) bool) render.Input {
		t.Helper()
		if err := client.conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		for {
			line, err := client.rd.ReadBytes('\n')
			if err != nil {
				t.Fatal(err)
			}
			var message clientMsg
			if err := json.Unmarshal(line, &message); err != nil {
				t.Fatal(err)
			}
			if message.Type == "error" {
				t.Fatal(message.Reason)
			}
			if message.Type != "snapshot" {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(message.Data)
			if err != nil {
				t.Fatal(err)
			}
			in, err := client.snapshots.Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			regions := render.Compose(in)
			regions.Frame = nil
			client.send(t, clientMsg{Type: "rendered", Rendered: regions, FocusEpoch: message.FocusEpoch})
			if predicate(in) {
				return in
			}
			if len(in.GraphicsPatches) > 0 {
				ack(in.SurfaceRevision)
			}
		}
	}
	read(func(in render.Input) bool { return len(in.Panes) > 0 })
	pane := firstPaneID(t, dir)
	const width = 1024
	const height = api.MaxGraphicsSetBytes / 4 / width
	pixels := bytes.Repeat([]byte{12, 34, 56, 255}, width*height)
	set := func() {
		t.Helper()
		apiOK(t, dir, "pane.graphics.set", map[string]any{"pane_id": pane, "format": "rgba", "image_width": width, "image_height": height, "data_base64": base64.StdEncoding.EncodeToString(pixels), "placement": map[string]any{"grid_cols": 4, "grid_rows": 2}}, nil)
	}
	hasImage := func(in render.Input) bool {
		for _, p := range in.Panes {
			if len(p.Graphics) > 0 {
				return true
			}
		}
		return false
	}
	set()
	read(hasImage)
	other := dialClient(t, dir)
	defer other.conn.Close()
	other.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24})
	other.readUntil(t, time.Second, func(message clientMsg) bool { return other.lastRegions != nil })
	otherInputSeen := make(chan struct{})
	stalledReaderInputSeen := make(chan struct{})
	otherDone := make(chan struct{})
	go func() {
		defer close(otherDone)
		seen := false
		seenStalled := false
		other.readUntil(t, 5*time.Second, func(message clientMsg) bool {
			if !seen && bytes.Contains([]byte(outputs([]clientMsg{message})), []byte("TB_OTHER_VIEWER")) {
				seen = true
				close(otherInputSeen)
			}
			if !seenStalled && bytes.Contains([]byte(outputs([]clientMsg{message})), []byte("TB_STALLED_READER")) {
				seenStalled = true
				close(stalledReaderInputSeen)
			}
			return false
		})
	}()
	defer func() { other.conn.Close(); <-otherDone }()
	pixels[0] ^= 255
	set()
	first := read(func(in render.Input) bool {
		return len(in.GraphicsPatches) > 0 && len(in.GraphicsPatches[0].Patch.Rects) > 0
	})
	pixels[4] ^= 255
	set()
	other.send(t, clientMsg{Type: "input", Data: base64.StdEncoding.EncodeToString([]byte("printf TB_OTHER_VIEWER\\n\r"))})
	select {
	case <-otherInputSeen:
	case <-time.After(time.Second):
		t.Fatal("another viewer's input was blocked by image receipt")
	}
	// Text frames can pass, but neither payload nor patch may advance.
	assertNoImage := func() {
		t.Helper()
		deadline := time.Now().Add(graphicsReceiptWait / 4)
		_ = client.conn.SetReadDeadline(deadline)
		for {
			line, err := client.rd.ReadBytes('\n')
			if err != nil {
				if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
					return
				}
				t.Fatal(err)
			}
			var message clientMsg
			if err = json.Unmarshal(line, &message); err != nil {
				t.Fatal(err)
			}
			if message.Type != "snapshot" {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(message.Data)
			if err != nil {
				t.Fatal(err)
			}
			in, err := client.snapshots.Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			if len(in.GraphicsPatches) != 0 {
				t.Fatal("unacknowledged image advanced", in.SurfaceRevision)
			}
			for id, p := range in.Panes {
				for i := range p.Graphics {
					if n, _ := client.snapshots.GraphicsPayloadSize(id, i); n != 0 {
						t.Fatal("text frame carried pixels", n)
					}
					if old := first.Panes[id]; old != nil && len(old.Graphics) > i && !bytes.Equal(p.Graphics[i].Image.Data, old.Graphics[i].Image.Data) {
						t.Fatal("text changed pixels")
					}
				}
			}
		}
	}
	assertNoImage()
	ack(first.SurfaceRevision - 1)
	assertNoImage()
	ack(first.SurfaceRevision)
	second := read(func(in render.Input) bool {
		return len(in.GraphicsPatches) > 0 && len(in.GraphicsPatches[0].Patch.Rects) > 0
	})
	if second.SurfaceRevision <= first.SurfaceRevision {
		t.Fatal("surface revision did not advance")
	}
	// Payload is deliberately not JSON: the contract ignores it and clears
	// this viewer's wait and all image bases before scheduling an ordinary frame.
	client.send(t, clientMsg{Type: "control", Kind: endpoint.GraphicsPatchMiss, Data: "ignored"})
	recovered := read(func(in render.Input) bool { return hasImage(in) && len(in.GraphicsPatches) == 0 })
	if client.snapshots.MissingGraphicsKey != nil {
		t.Fatal("ordinary full recovery did not restore the base")
	}
	if recovered.SurfaceRevision <= second.SurfaceRevision {
		t.Fatal("miss did not schedule a new surface")
	}
	pixels[8] ^= 255
	set()
	resumed := read(func(in render.Input) bool {
		return len(in.GraphicsPatches) > 0 && len(in.GraphicsPatches[0].Patch.Rects) > 0
	})
	ack(resumed.SurfaceRevision)
	pixels[12] ^= 255
	set()
	read(func(in render.Input) bool {
		return len(in.GraphicsPatches) > 0 && len(in.GraphicsPatches[0].Patch.Rects) > 0
	})
	pixels[16] ^= 255
	set()
	started := time.Now()
	read(func(in render.Input) bool {
		return len(in.GraphicsPatches) > 0 && len(in.GraphicsPatches[0].Patch.Rects) > 0
	})
	t.Logf("unacknowledged image released after %s (configured wait %s)", time.Since(started), graphicsReceiptWait)
	// A real large socket write must also stay off the session loop. Consume
	// just its first byte and leave the rest unread while the other viewer types.
	rand.New(rand.NewSource(7)).Read(pixels)
	set()
	if err := client.conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.rd.ReadByte(); err != nil {
		t.Fatal(err)
	}
	other.send(t, clientMsg{Type: "input", Data: base64.StdEncoding.EncodeToString([]byte("printf TB_STALLED_READER\\n\r"))})
	select {
	case <-stalledReaderInputSeen:
	case <-time.After(time.Second):
		t.Fatal("an unread image socket blocked another viewer's input")
	}
}
