package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/render"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/teststate"
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

func TestStage6aNotificationShowHeadlessDisabledAndValidation(t *testing.T) {
	teststate.Lock(t)
	for _, delivery := range []string{"off", "herdr", "terminal", "system"} {
		t.Run(delivery, func(t *testing.T) {
			s := newTestServer(t, "explicit-notification")
			s.cfg, _ = config.Decode([]byte("[ui.toast]\ndelivery='" + delivery + "'\n"))
			call := func(params any) api.Response {
				return s.dispatchOnLoop(&api.Request{ID: "test", Method: api.MethodNotificationShow, Params: api.MarshalParams(params)})
			}
			for _, params := range []map[string]any{{}, {"title": nil}, {"title": "日本語", "position": "bad"}, {"title": "日本語", "sound": "bad"}, {"title": "日本語", "sound": nil}, {"title": "日本語", "sound": ""}} {
				if response := call(params); response.Err == nil || response.Err.Code != api.ErrInvalidRequest {
					t.Fatal("invalid params accepted", params, response)
				}
			}
			response := call(map[string]any{"title": "  日本語  ", "body": nil, "sound": "none"})
			want := `{"type":"notification_show","shown":true,"reason":"shown"}`
			if delivery == "off" {
				want = `{"type":"notification_show","shown":false,"reason":"disabled"}`
			}
			if delivery == "system" || delivery == "terminal" {
				want = `{"type":"notification_show","shown":false,"reason":"no_foreground_client"}`
			}
			if response.Err != nil || string(response.Result) != want {
				t.Fatal(response.Err, string(response.Result), want)
			}
		})
	}
}

func TestStage6aBackgroundToastReachesConnectedViewer(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "background-toast")
	s.cfg, _ = config.Decode([]byte("[ui.toast]\ndelivery='herdr'\ndelay_seconds=0\n"))
	s.app.SetServerUI()
	s.app.SetSidebarConfig(s.cfg, false)
	v := s.app.NewViewer(io.Discard, 80, 24, nil)
	var toast *render.Toast
	v.SetSnapshotSink(func(in render.Input) error { toast = in.Toast; return nil })
	s.deliverNotification(1, "finished")
	v.Tick(s.app)
	v.Draw(s.app, nil)
	if toast == nil || toast.Title != "1 workspace 1" || toast.Body != "workspace 1 · 1" || toast.Kind != "finished" {
		t.Fatalf("background notification missing from viewer: %+v", toast)
	}
}
