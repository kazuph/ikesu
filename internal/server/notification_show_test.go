package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/teststate"
)

func TestStage6aNotificationShowRealSocketAndViewer(t *testing.T) {
	teststate.Lock(t)
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n[ui.toast]\ndelivery='gtx'\n[ui.sound]\nenabled=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "GTX_CONFIG", cfg)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	c := dialClient(t, dir)
	defer c.conn.Close()
	c.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24})
	c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" })
	show := func(params map[string]any, want string) {
		t.Helper()
		response := apiReq(t, dir, "notification.show", params)
		if response.Err != nil || string(response.Result) != want {
			t.Fatalf("notification.show: error=%v result=%s want=%s", response.Err, response.Result, want)
		}
	}
	show(map[string]any{"title": "日本語", "body": "本文", "sound": "none", "position": "bottom-left"}, `{"type":"notification_show","shown":true,"reason":"shown"}`)
	frames := c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return strings.Contains(outputs([]clientMsg{m}), "日本語") })
	if !strings.Contains(outputs(frames), "日本語") || c.lastRegions.ToastRect.W != 12 || c.lastRegions.ToastRect.H != 4 || c.lastRegions.ToastRect.X != 0 || c.lastRegions.ToastRect.Y != 20 {
		t.Fatal("notification did not reach viewer at requested position", c.lastRegions)
	}
	show(map[string]any{"title": "次の通知", "sound": "none"}, `{"type":"notification_show","shown":false,"reason":"busy"}`)
	response := apiReq(t, dir, "notification.show", map[string]any{"title": " \n "})
	if response.Err == nil || response.Err.Code != "invalid_params" || response.Err.Message != "notification title is empty" {
		t.Fatal("empty title accepted", response)
	}
}
