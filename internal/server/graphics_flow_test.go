package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
)

func graphicsWriterStack() string {
	stack := make([]byte, 64*1024)
	for {
		n := runtime.Stack(stack, true)
		if n < len(stack) {
			return string(stack[:n])
		}
		stack = make([]byte, len(stack)*2)
	}
}

func graphicsFrameWriter(stack, id string, blocked bool) bool {
	for _, goroutine := range strings.Split(stack, "\n\n") {
		if strings.HasPrefix(goroutine, "goroutine "+id+" ") &&
			strings.Contains(goroutine, "clientFrameWriter.write") &&
			(!blocked || strings.Contains(goroutine, "[IO wait]")) {
			return true
		}
	}
	return false
}

func TestGraphicsReceiptAndMissRecoveryOnRealSocket(t *testing.T) {
	if runIsolatedParallel(t) {
		return
	}
	teststate.Lock(t)
	config := filepath.Join(t.TempDir(), "graphics.toml")
	if err := os.WriteFile(config, []byte("onboarding=false\n[experimental]\nkitty_graphics=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "IKESU_CONFIG", config)
	clock := &controlledReceiptClock{now: time.Now()}
	dir, done := startTestEndpointWithClock(t, endpoint.GraphicsPeer(), clock)
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
	ptyReady := make(chan struct{})
	otherDone := make(chan struct{})
	go func() {
		defer close(otherDone)
		seen := false
		seenReady := false
		observe := func(message clientMsg) bool {
			if !seen && bytes.Contains([]byte(outputs([]clientMsg{message})), []byte("TB_OTHER_VIEWER")) {
				seen = true
				close(otherInputSeen)
			}
			if !seenReady && bytes.Contains([]byte(outputs([]clientMsg{message})), []byte("TB_PTY_READY")) {
				seenReady = true
				close(ptyReady)
			}
			return false
		}
		for !other.readClosed {
			if _, err := other.readUntilResult(5*time.Second, observe); err != nil {
				t.Errorf("other viewer read: %v", err)
				return
			}
		}
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
	held := first
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
					if old := held.Panes[id]; old != nil && len(old.Graphics) > i && !bytes.Equal(p.Graphics[i].Image.Data, old.Graphics[i].Image.Data) {
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
	held = read(func(in render.Input) bool {
		return len(in.GraphicsPatches) > 0 && len(in.GraphicsPatches[0].Patch.Rects) > 0
	})
	pixels[16] ^= 255
	set()
	started := time.Now()
	clock.Advance(graphicsReceiptWait - time.Nanosecond)
	assertNoImage()
	clock.Advance(time.Nanosecond)
	released := read(func(in render.Input) bool {
		return len(in.GraphicsPatches) > 0 && len(in.GraphicsPatches[0].Patch.Rects) > 0
	})
	t.Logf("unacknowledged image released after advancing receipt clock by %s (socket observation %s)", graphicsReceiptWait, time.Since(started))
	// Keep the real PTY slave unread so the test can observe delivered bytes
	// without waiting for the shell to render its response.
	ttyPath := filepath.Join(t.TempDir(), "tty")
	command := "tty > '" + ttyPath + "'; stty raw -echo; printf TB_PTY_%s READY; exec sleep 60\r"
	other.send(t, clientMsg{Type: "input", Data: base64.StdEncoding.EncodeToString([]byte(command))})
	select {
	case <-ptyReady:
	case <-otherDone:
		t.Fatal("other viewer closed before PTY was ready")
	case <-t.Context().Done():
		t.Fatal("test ended before PTY was ready")
	}
	ttyName, err := os.ReadFile(ttyPath)
	if err != nil {
		t.Fatal(err)
	}
	tty, err := os.OpenFile(strings.TrimSpace(string(ttyName)), os.O_RDONLY|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	// Drain frames produced while preparing the PTY and retire the image patch.
	client.send(t, clientMsg{Type: "control", Kind: endpoint.GraphicsPatchMiss, Data: "ignored"})
	read(func(in render.Input) bool {
		return in.SurfaceRevision > released.SurfaceRevision && hasImage(in) && len(in.GraphicsPatches) == 0
	})
	// Consume part of the large frame so an earlier small frame cannot be
	// mistaken for the write being tested; leave the rest unread.
	rand.New(rand.NewSource(7)).Read(pixels)
	set()
	// Force a fresh frame even if an earlier patch is still unacknowledged.
	client.send(t, clientMsg{Type: "control", Kind: endpoint.GraphicsPatchMiss, Data: "ignored"})
	if err := client.conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if n, err := io.ReadFull(client.rd, make([]byte, len(pixels)/8)); err != nil {
		t.Fatalf("large frame stopped after %d bytes: %v", n, err)
	}
	var writerID string
	writeObservedAt := time.Now()
	waitPromptCondition(t, func() bool {
		var ids []string
		for _, goroutine := range strings.Split(graphicsWriterStack(), "\n\n") {
			if strings.Contains(goroutine, "clientFrameWriter.write") && strings.Contains(goroutine, "[IO wait]") {
				fields := strings.Fields(goroutine)
				if len(fields) >= 2 && fields[0] == "goroutine" {
					ids = append(ids, fields[1])
				}
			}
		}
		if len(ids) != 1 {
			return false
		}
		writerID = ids[0]
		return true
	})
	t.Logf("blocked image writer %s observed after %s", writerID, time.Since(writeObservedAt))
	writeObservedAt = time.Now()
	writerFinished := make(chan struct{})
	stopMonitor := make(chan struct{})
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopMonitor:
				return
			case <-ticker.C:
				if !graphicsFrameWriter(graphicsWriterStack(), writerID, false) {
					close(writerFinished)
					return
				}
			}
		}
	}()
	defer func() { close(stopMonitor); <-monitorDone }()
	stalledReaderInputSeen := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		_, err := tty.Read(buf)
		if err == nil && buf[0] != 'x' {
			err = fmt.Errorf("PTY received %q instead of x", buf[0])
		}
		stalledReaderInputSeen <- err
	}()
	other.send(t, clientMsg{Type: "input", Data: base64.StdEncoding.EncodeToString([]byte("x"))})
	select {
	case err := <-stalledReaderInputSeen:
		t.Logf("PTY input observed after %s", time.Since(writeObservedAt))
		if err != nil {
			t.Fatal(err)
		}
		if !graphicsFrameWriter(graphicsWriterStack(), writerID, false) {
			t.Fatal("image write finished before the other viewer's input reached the pane")
		}
	case <-writerFinished:
		t.Logf("image writer finished after %s", time.Since(writeObservedAt))
		t.Fatal("image write finished before the other viewer's input reached the pane")
	case <-otherDone:
		t.Fatal("other viewer closed before its input reached the pane")
	case <-t.Context().Done():
		t.Fatal("test ended before the other viewer's input reached the pane")
	}
}
