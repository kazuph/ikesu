package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestStage3AgentToastDismissalIsLocalToEachRealDisplay(t *testing.T) {
	teststate.Lock(t)
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n[ui.toast]\ndelivery='gtx'\ndelay_seconds=0\n[ui.sound]\nenabled=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "GTX_CONFIG", cfg)
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := config.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	ready.send(t, clientMsg{Type: "hello", Cols: 160, Rows: 45, Config: &settings})
	ready.readUntil(t, 5*time.Second, func(m clientMsg) bool { return ready.lastRegions != nil })
	ready.conn.Close()
	target := firstPaneID(t, dir)
	var foreground api.TabCreatedResult
	apiOK(t, dir, api.MethodTabCreate, api.TabCreateParams{Focus: true}, &foreground)
	clients := []*testConn{dialClient(t, dir), dialClient(t, dir)}
	for _, c := range clients {
		defer c.conn.Close()
		c.send(t, clientMsg{Type: "hello", Cols: 160, Rows: 45, Config: &settings})
		c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return c.lastRegions != nil })
	}
	waitToast := func(c *testConn, visible bool) {
		t.Helper()
		found := false
		frames := c.readUntil(t, 10*time.Second, func(m clientMsg) bool {
			found = m.Type == "output" && c.lastRegions != nil && (c.lastRegions.ToastRect.W > 0) == visible
			return found
		})
		if !found {
			t.Fatalf("display toast visible=%v not observed, regions=%+v output=%q", visible, c.lastRegions, outputs(frames))
		}
	}
	for _, state := range []string{"working", "idle"} {
		apiOK(t, dir, api.MethodPaneReportAgent, map[string]any{"pane_id": target, "agent": "codex", "source": "codex", "session_id": t.Name(), "state": state}, nil)
	}
	t.Logf("target: %s", apiReq(t, dir, api.MethodPaneGet, api.PaneTarget{PaneID: target}).Result)
	t.Logf("tabs: %s", apiReq(t, dir, api.MethodTabList, api.TabListParams{}).Result)
	for i, c := range clients {
		t.Logf("waiting for initial notification on display %d", i)
		waitToast(c, true)
	}
	c := clients[0]
	r := c.lastRegions.ToastRect
	mouse := func(code int, suffix string) {
		c.send(t, clientMsg{Type: "input", Data: encode64([]byte(fmt.Sprintf("\x1b[<%d;%d;%d%s", code, r.X+2, r.Y+2, suffix)))})
	}
	mouse(2, "M")
	mouse(2, "m")
	menuHasRename := func() bool {
		if c.lastFrame == nil || c.lastRegions == nil {
			return false
		}
		r := c.lastRegions.MenuRect
		for y := r.Y; y < r.Y+r.H; y++ {
			var row strings.Builder
			for x := r.X; x < r.X+r.W; x++ {
				if cell := c.lastFrame.Cell(x, y); cell != nil {
					row.WriteString(cell.Content)
				}
			}
			if strings.Contains(row.String(), "Rename pane") {
				return true
			}
		}
		return false
	}
	frames := c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" && menuHasRename() })
	if !menuHasRename() || c.lastRegions.ToastRect.W == 0 {
		t.Fatalf("right click did not retain toast and open menu: toast=%+v menu=%+v output=%q", c.lastRegions.ToastRect, c.lastRegions.MenuRect, outputs(frames))
	}
	c.send(t, clientMsg{Type: "input", Data: encode64([]byte{27})})
	mouse(0, "M")
	mouse(0, "m")
	waitToast(clients[0], false)
	waitToast(clients[1], true)
	response := apiReq(t, dir, api.MethodNotificationShow, map[string]any{"title": "after click", "sound": "none"})
	if response.Err != nil || !strings.Contains(string(response.Result), `"reason":"busy"`) {
		t.Fatal("viewer dismissal cleared the server's pending toast", response)
	}
	// The remaining display expires on the observed eight-second agent clock.
	waitToast(clients[1], false)
	response = apiReq(t, dir, api.MethodNotificationShow, map[string]any{"title": "after expiry", "sound": "none"})
	if response.Err != nil || !strings.Contains(string(response.Result), `"reason":"shown"`) {
		t.Fatal("expired server toast still busy", response)
	}
	for _, display := range clients {
		waitToast(display, true)
	}
	for _, display := range clients {
		waitToast(display, false)
	}
}
