package server

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/teststate"
)

func directGraphicsFixture(t *testing.T) (string, *testConn, string, map[string]any) {
	t.Helper()
	teststate.Lock(t)
	config := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(config, []byte("onboarding=false\n[experimental]\nkitty_graphics=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "IKESU_CONFIG", config)
	teststate.Setenv(t, "XDG_RUNTIME_DIR", t.TempDir())
	dir, done := startTestEndpoint(t, endpoint.GraphicsPeer())
	t.Cleanup(func() { stopServer(t, dir, done) })
	viewer := dialClient(t, dir)
	t.Cleanup(func() { viewer.conn.Close() })
	viewer.send(t, clientMsg{Type: "hello", Peer: endpoint.GraphicsPeer(), GraphicsFileTransport: endpoint.DirectKittyTransport,
		Cols: 80, Rows: 24, Xpixel: 640, Ypixel: 384})
	viewer.readUntil(t, 5*time.Second, func(clientMsg) bool { return viewer.lastRegions != nil })
	pane := firstPaneID(t, dir)
	deadline := time.Now().Add(5 * time.Second)
	for {
		var info map[string]any
		response := apiReq(t, dir, api.MethodPaneGraphicsInfo, map[string]any{"pane_id": pane})
		if response.Err != nil {
			if response.Err.Code != "cell_size_unavailable" {
				t.Fatalf("graphics fixture readiness: %+v", response.Err)
			}
		} else {
			if err := json.Unmarshal(response.Result, &info); err != nil {
				t.Fatal(err)
			}
			if info["file_frame_transport"] == endpoint.DirectKittyTransport {
				if info["cell_width_px"] != float64(8) || info["cell_height_px"] != float64(16) {
					t.Fatal("negotiated cell pixels differ from hello", info)
				}
				return dir, viewer, pane, info
			}
		}
		if time.Now().After(deadline) {
			var panes api.PaneListResult
			apiOK(t, dir, api.MethodPaneList, api.PaneListParams{}, &panes)
			t.Logf("actual panes at advertisement failure: %+v", panes)
			t.Fatal("no direct transport on actual negotiated Unix viewer", info)
		}
		// Keep acknowledging real snapshots while exact cell geometry settles.
		viewer.readUntil(t, 20*time.Millisecond, func(message clientMsg) bool { return message.Type == "output" })
	}
}

func graphicsFileProducer(t *testing.T, dir, pane, path string, sequence, revision uint64) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("unix", api.APISocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := api.WriteRequest(conn, api.Request{ID: "file-stream", Method: api.MethodPaneGraphicsStream,
		Params: api.MarshalParams(map[string]any{"pane_id": pane})}); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	if response, err := api.ReadResponse(reader); err != nil || response.Err != nil {
		t.Fatalf("stream open: %+v %v", response, err)
	}
	frame := api.PaneGraphicsStreamFrame{PaneGraphicsSetParams: api.PaneGraphicsSetParams{
		Format: "rgba", ImageWidth: 2, ImageHeight: 3, Placement: api.PaneGraphicsPlacement{GridCols: 2, GridRows: 3}},
		File: &api.PaneGraphicsFile{Path: path}, Sequence: sequence, Revision: revision}
	if err := json.NewEncoder(conn).Encode(frame); err != nil {
		t.Fatal(err)
	}
	return conn, reader
}

func readGraphicsFile(t *testing.T, viewer *testConn) endpoint.GraphicsFile {
	t.Helper()
	var file *endpoint.GraphicsFile
	viewer.readUntil(t, 5*time.Second, func(message clientMsg) bool {
		if message.Type == endpoint.GraphicsFileMessage {
			file = message.GraphicsFile
			return true
		}
		return false
	})
	if file == nil {
		t.Fatal("no native file message")
	}
	return *file
}

func TestGraphicsFileClientRejectionCopiesBeforeAcknowledgementAndDisablesOnlyDirect(t *testing.T) {
	dir, viewer, pane, info := directGraphicsFixture(t)
	source := filepath.Join(info["file_frame_directory"].(string), "reject.rgba")
	pixels := []byte{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 17, 18, 19, 255, 20, 21, 22, 255, 23, 24, 25, 255}
	if err := os.WriteFile(source, pixels, 0600); err != nil {
		t.Fatal(err)
	}
	conn, reader := graphicsFileProducer(t, dir, pane, source, math.MaxUint64, math.MaxUint64-1)
	file := readGraphicsFile(t, viewer)
	if file.Path != source || file.ExpectedLen != 24 || file.Leading != "" || !strings.HasPrefix(file.Control, "a=t,f=32,s=2,v=3,i=") || !strings.HasSuffix(file.Control, ",q=0") {
		t.Fatalf("file projection changed producer path or controls: %+v", file)
	}
	var asset endpoint.GraphicsSurfaceAsset
	if err := json.Unmarshal(file.SurfaceAsset, &asset); err != nil || asset.Placement.Image.NativeID != file.ImageID || len(asset.Placement.Image.Data) != 0 {
		t.Fatalf("surface asset=%+v error=%v", asset, err)
	}
	identity := endpoint.GraphicsTransmission{TransferID: file.TransferID, ImageID: file.ImageID}
	// These are inputs to the real endpoint protocol, not terminal receipt proof.
	viewer.send(t, clientMsg{Type: endpoint.GraphicsResultMessage,
		GraphicsResult: &endpoint.GraphicsTransmissionResult{GraphicsTransmission: identity, Success: true}})
	conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if response, err := api.ReadResponse(reader); err == nil {
		t.Fatal("success before Started acknowledged the producer", response)
	} else if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatal("stream ended rather than ignoring premature success", err)
	}
	viewer.send(t, clientMsg{Type: endpoint.GraphicsResultMessage,
		GraphicsResult: &endpoint.GraphicsTransmissionResult{GraphicsTransmission: identity, Success: false}})
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	response, err := api.ReadResponse(reader)
	if err != nil || response.Err != nil || response.ID != "file-stream:file:18446744073709551615" {
		t.Fatalf("fallback ack=%+v error=%v", response, err)
	}
	var ack struct {
		Type               string
		Sequence, Revision uint64
	}
	if err := json.Unmarshal(response.Result, &ack); err != nil || ack.Type != "pane_graphics_frame_ack" || ack.Sequence != math.MaxUint64 || ack.Revision != math.MaxUint64-1 {
		t.Fatalf("u64 acknowledgement was changed: %+v %v", ack, err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal("producer file was removed by the server", err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	want := base64.StdEncoding.EncodeToString(pixels)
	messages := viewer.readUntil(t, 5*time.Second, func(message clientMsg) bool { return strings.Contains(outputs([]clientMsg{message}), want) })
	if !strings.Contains(outputs(messages), want) {
		t.Fatal("owned fallback pixels were not rendered after source unlink")
	}
	var after map[string]any
	apiOK(t, dir, api.MethodPaneGraphicsInfo, map[string]any{"pane_id": pane}, &after)
	if after["pixel_mouse"] != true || after["file_frame_damage"] != true {
		t.Fatal("file rejection disabled independent graphics capabilities", after)
	}
	for _, key := range []string{"file_frame_directory", "file_frame_formats", "file_frame_max_bytes", "file_frame_direct_max_bytes", "file_frame_transport"} {
		if _, exists := after[key]; exists {
			t.Fatal("rejected direct connection still advertised", key, after)
		}
	}
}

func TestGraphicsFileReceiptDeadlinesNeverBecomeInlineSuccess(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "delivery", true: "terminal"}[started], func(t *testing.T) {
			dir, viewer, pane, info := directGraphicsFixture(t)
			source := filepath.Join(info["file_frame_directory"].(string), "expired.rgba")
			if err := os.WriteFile(source, make([]byte, 24), 0600); err != nil {
				t.Fatal(err)
			}
			conn, reader := graphicsFileProducer(t, dir, pane, source, 7, 8)
			file := readGraphicsFile(t, viewer)
			identity := endpoint.GraphicsTransmission{TransferID: file.TransferID, ImageID: file.ImageID}
			if started {
				viewer.send(t, clientMsg{Type: endpoint.GraphicsStartedMessage, GraphicsTransmission: &identity})
			}
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			response, err := api.ReadResponse(reader)
			if err != nil || response.Err == nil || response.Err.Code != "server_unavailable" || response.ID != "file-stream:file:7" {
				t.Fatalf("expired transfer acknowledged or lost correlation: %+v %v", response, err)
			}
			retired := false
			viewer.readUntil(t, 5*time.Second, func(message clientMsg) bool {
				retired = message.Type == endpoint.GraphicsRetiredMessage && message.GraphicsTransmission != nil && *message.GraphicsTransmission == identity
				return retired
			})
			if !retired {
				t.Fatal("expired terminal image was not retired")
			}
			if _, err := os.Stat(source); err != nil {
				t.Fatal("expiry unlinked producer file", err)
			}
			var after map[string]any
			apiOK(t, dir, api.MethodPaneGraphicsInfo, map[string]any{"pane_id": pane}, &after)
			if _, exists := after["file_frame_transport"]; exists {
				t.Fatal("expiry retained direct capability", after)
			}
		})
	}
}
