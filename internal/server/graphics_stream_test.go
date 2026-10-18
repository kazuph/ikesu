package server

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestGraphicsStreamRealSocketOwnershipAndDisconnect(t *testing.T) {
	teststate.Lock(t)
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[experimental]\nkitty_graphics=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "GTX_CONFIG", cfg)
	dir, done := startTestServer(t)
	viewer := dialClient(t, dir)
	defer viewer.conn.Close()
	defer stopServer(t, dir, done)
	viewer.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24, Xpixel: 640, Ypixel: 384})
	viewer.readUntil(t, time.Second, func(m clientMsg) bool { return m.Type == "output" })
	pane := firstPaneID(t, dir)
	open := func() (net.Conn, *bufio.Reader) {
		conn, err := net.Dial("unix", api.APISocketPath(dir))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if err := api.WriteRequest(conn, api.Request{ID: "stream", Method: "pane.graphics.stream",
			Params: api.MarshalParams(map[string]any{"pane_id": pane, "layer_id": "test"})}); err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(conn)
		response, err := api.ReadResponse(reader)
		if err != nil || response.Err != nil {
			t.Fatalf("stream open: %+v %v", response, err)
		}
		return conn, reader
	}
	frame := func(conn net.Conn) {
		if err := json.NewEncoder(conn).Encode(map[string]any{"format": "rgb", "image_width": 2, "image_height": 1,
			"data_length": 6, "layer_id": "test", "placement": map[string]any{"grid_cols": 4, "grid_rows": 2}}); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write([]byte{255, 0, 0, 0, 255, 0}); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(marker string) {
		messages := viewer.readUntil(t, 5*time.Second, func(m clientMsg) bool {
			return strings.Contains(outputs([]clientMsg{m}), marker)
		})
		if !strings.Contains(outputs(messages), marker) {
			t.Fatalf("stream did not produce %q on the viewer", marker)
		}
	}
	first, _ := open()
	frame(first)
	wait("/wAAAP8A")
	clear := apiReq(t, dir, api.MethodPaneGraphicsClear, map[string]any{"pane_id": pane, "layer_id": "test"})
	if clear.Err == nil || clear.Err.Code != "stream_conflict" {
		t.Fatalf("non-owner clear removed a stream-owned image: %+v", clear)
	}
	second, reader := open()
	frame(second)
	response, err := api.ReadResponse(reader)
	if err != nil || response.Err == nil || response.Err.Code != "stream_conflict" {
		t.Fatalf("simultaneous owner: %+v %v", response, err)
	}
	first.Close()
	wait("a=d,d=I,")
	successor, _ := open()
	frame(successor)
	wait("/wAAAP8A")
}
