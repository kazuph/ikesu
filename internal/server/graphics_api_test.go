package server

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestPaneGraphicsSetClearAndLayerBoundary(t *testing.T) {
	teststate.Lock(t)
	config := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(config, []byte("onboarding=false\n[experimental]\nkitty_graphics=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "GTX_CONFIG", config)
	dir, done := startTestServer(t)
	client := dialClient(t, dir)
	defer client.conn.Close()
	defer stopServer(t, dir, done)
	client.send(t, clientMsg{Type: "hello", Cols: 160, Rows: 45, Xpixel: 1280, Ypixel: 720})
	client.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "snapshot" })
	pane := firstPaneID(t, dir)
	var info map[string]any
	apiOK(t, dir, "pane.graphics.info", map[string]any{"pane_id": pane}, &info)
	if info["type"] != "pane_graphics_info" || info["max_layers_per_pane"] != float64(16) {
		t.Fatalf("graphics info=%v", info)
	}
	for i := 0; i <= 16; i++ {
		params := map[string]any{"pane_id": pane, "format": "rgb", "image_width": 2, "image_height": 1,
			"data_base64": "/wAAAP8A", "layer_id": fmt.Sprintf("layer-%d", i),
			"placement": map[string]any{"grid_cols": 4, "grid_rows": 2}}
		response := apiReq(t, dir, "pane.graphics.set", params)
		if i < 16 && response.Err != nil {
			t.Fatalf("layer %d: %+v", i, response.Err)
		}
		if i == 16 && (response.Err == nil || response.Err.Code != "layer_limit") {
			t.Fatalf("17th layer=%+v", response)
		}
	}
	apiOK(t, dir, "pane.graphics.clear", map[string]any{"pane_id": pane, "layer_id": "layer-0"}, nil)
	apiOK(t, dir, "pane.graphics.set", map[string]any{"pane_id": pane, "format": "bgra", "image_width": 2, "image_height": 1,
		"data_base64": "AAD//wD/AP8=", "layer_id": "replacement", "placement": map[string]any{"grid_cols": 4, "grid_rows": 2}}, nil)
	if resp := apiReq(t, dir, "pane.graphics.info", map[string]any{"pane_id": "p999999"}); resp.Err == nil || resp.Err.Code != api.ErrPaneNotFound {
		t.Fatalf("absent pane=%+v", resp)
	}
	if err := os.WriteFile(config, []byte("onboarding=false\n[experimental]\nkitty_graphics=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	apiOK(t, dir, api.MethodServerReloadConfig, api.EmptyParams{}, nil)
	for _, method := range []string{"pane.graphics.info", "pane.graphics.set", "pane.graphics.clear", "pane.graphics.stream"} {
		response := apiReq(t, dir, method, map[string]any{"pane_id": pane})
		if response.Err == nil || response.Err.Code != "feature_disabled" {
			t.Fatalf("disabled %s: %+v", method, response)
		}
	}
}

func TestGraphicsInfoRequiresExactCellsAndPreservesDamageCapability(t *testing.T) {
	teststate.Lock(t)
	config := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(config, []byte("onboarding=false\n[experimental]\nkitty_graphics=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "GTX_CONFIG", config)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	client := dialClient(t, dir)
	defer client.conn.Close()
	pane := firstPaneID(t, dir)
	response := apiReq(t, dir, api.MethodPaneGraphicsInfo, map[string]any{"pane_id": pane})
	if response.Err == nil || response.Err.Code != "cell_size_unavailable" {
		t.Fatalf("unknown real cell geometry was approximated: %+v", response)
	}
	client.send(t, clientMsg{Type: "hello", Cols: 160, Rows: 45, Xpixel: 1280, Ypixel: 720})
	client.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "snapshot" })
	var info map[string]any
	apiOK(t, dir, api.MethodPaneGraphicsInfo, map[string]any{"pane_id": pane}, &info)
	if info["cell_width_px"] != float64(8) || info["cell_height_px"] != float64(16) || info["file_frame_damage"] != true {
		t.Fatalf("info lost known geometry or damage capability: %v", info)
	}
	for _, key := range []string{"file_frame_directory", "file_frame_formats", "file_frame_max_bytes", "file_frame_direct_max_bytes", "file_frame_transport"} {
		if _, exists := info[key]; exists {
			t.Fatalf("advertised %s without a direct-capable viewer: %v", key, info)
		}
	}
}
