package server

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestStage6aFixTerminalRealDisplaySocket(t *testing.T) {
	teststate.Lock(t)
	for _, tt := range []struct {
		name     string
		terminal *endpoint.Terminal
		want     string
	}{
		{"ghostty", &endpoint.Terminal{Program: "ghostty"}, "\x1b]9;日本語: 本文\x1b\\"},
		{"iterm", &endpoint.Terminal{Program: "iTerm.app"}, "\x1b]9;日本語: 本文\x1b\\"},
		{"wezterm", &endpoint.Terminal{Program: "WezTerm"}, "\x1b]9;日本語: 本文\x1b\\"},
		{"kitty", &endpoint.Terminal{Kitty: true}, "\x1b]99;i=1:d=0;日本語\x1b\\\x1b]99;i=1:p=body;本文\x1b\\"},
		{"term-ghostty", &endpoint.Terminal{Term: "xterm-ghostty"}, "\x1b]9;日本語: 本文\x1b\\"},
		{"term-kitty", &endpoint.Terminal{Term: "xterm-kitty"}, "\x1b]99;i=1:d=0;日本語\x1b\\\x1b]99;i=1:p=body;本文\x1b\\"},
		{"term-wezterm", &endpoint.Terminal{Term: "wezterm"}, "\x1b]9;日本語: 本文\x1b\\"},
		{"tmux", &endpoint.Terminal{Program: "ghostty", Tmux: true}, "\x1bPtmux;\x1b\x1b]9;日本語: 本文\x1b\x1b\\\x1b\\"},
		{"unknown", &endpoint.Terminal{Term: "xterm-256color"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(cfg, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n[ui.toast]\ndelivery='terminal'\n[ui.sound]\nenabled=false\n"), 0600); err != nil {
				t.Fatal(err)
			}
			teststate.Setenv(t, "GTX_CONFIG", cfg)
			dir, done := startTestServer(t)
			defer stopServer(t, dir, done)
			c := dialClient(t, dir)
			defer c.conn.Close()
			c.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24, Terminal: tt.terminal})
			c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" })
			// Keep draining real display frames while the independent API waits.
			notifications := make(chan string, 1)
			readDone := make(chan struct{})
			notificationCount := 0
			go func() {
				defer close(readDone)
				c.readUntil(t, 5*time.Second, func(m clientMsg) bool {
					b, _ := base64.StdEncoding.DecodeString(m.Data)
					if strings.Contains(string(b), "\x1b]9;") || strings.Contains(string(b), "\x1b]99;") {
						notificationCount++
						select {
						case notifications <- string(b):
						default:
						}
					}
					return false
				})
			}()
			r := apiReq(t, dir, "notification.show", map[string]any{"title": "日本語", "body": "本文", "sound": "none"})
			want := `{"type":"notification_show","shown":true,"reason":"shown"}`
			if r.Err != nil || string(r.Result) != want {
				t.Fatal(r.Err, string(r.Result), want)
			}
			if tt.want != "" {
				select {
				case got := <-notifications:
					if got != tt.want {
						t.Fatalf("notification bytes=%q want=%q", got, tt.want)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("notification bytes not received")
				}
			}
			if tt.want == "" {
				r = apiReq(t, dir, "notification.show", map[string]any{"title": "日本語", "sound": "none"})
				if r.Err != nil || string(r.Result) != `{"type":"notification_show","shown":false,"reason":"rate_limited"}` {
					t.Fatal(r.Err, string(r.Result))
				}
			}
			<-readDone
			c.conn.Close()
			wantCount := 1
			if tt.want == "" {
				wantCount = 0
			}
			if notificationCount != wantCount {
				t.Fatalf("notification count=%d want=%d", notificationCount, wantCount)
			}
		})
	}
}

func TestStage6aFixRateOrderAndFailures(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "explicit-rate")
	set := func(delivery string) { s.cfg, _ = config.Decode([]byte("[ui.toast]\ndelivery='" + delivery + "'\n")) }
	call := func(want string) {
		t.Helper()
		r := s.dispatchOnLoop(&api.Request{ID: "rate", Method: api.MethodNotificationShow, Params: api.MarshalParams(map[string]any{"title": "日本語", "sound": "none"})})
		var v api.NotificationShowResult
		if r.Err != nil {
			t.Fatal(r.Err)
		}
		if err := json.Unmarshal(r.Result, &v); err != nil {
			t.Fatal(err)
		}
		if v.Reason != want {
			t.Fatalf("reason=%s want=%s", v.Reason, want)
		}
	}
	set("terminal")
	call("no_foreground_client")
	if !s.lastAPINotification.IsZero() {
		t.Fatal("failure advanced success time")
	}
	set("gtx")
	call("shown")
	success := s.lastAPINotification
	call("busy")
	s.app.DismissToast()
	call("rate_limited")
	set("off")
	call("disabled")
	if s.lastAPINotification != success {
		t.Fatal("non-success advanced success time")
	}
	set("terminal")
	call("rate_limited")
	<-time.After(apiNotificationInterval)
	call("no_foreground_client")
	set("gtx")
	call("shown")
}

func TestStage6aFixTerminalWriteFailure(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "terminal-write-failure")
	s.cfg, _ = config.Decode([]byte("[ui.toast]\ndelivery='terminal'\n"))
	dir, err := os.MkdirTemp("/tmp", "gtx-notify-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	l, err := net.Listen("unix", filepath.Join(dir, "display.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	peer, err := net.Dial("unix", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	conn, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	s.client = &clientConn{conn: conn, terminal: &endpoint.Terminal{Program: "ghostty"}}
	conn.Close()
	r := s.dispatchOnLoop(&api.Request{ID: "write", Method: api.MethodNotificationShow, Params: api.MarshalParams(map[string]any{"title": "日本語", "sound": "none"})})
	if r.Err != nil || !strings.Contains(string(r.Result), `"reason":"no_foreground_client"`) {
		t.Fatal(r.Err, string(r.Result))
	}
	if !s.lastAPINotification.IsZero() {
		t.Fatal("failed write advanced success time")
	}
}

func TestStage6aFixNotificationText(t *testing.T) {
	for _, tt := range []struct {
		in    string
		limit int
		want  string
	}{
		{"A\nB", 80, "A B"}, {"C\tD", 240, "C D"}, {"  A \r \n B  ", 80, "A B"}, {"\x00\x1b\a", 80, ""},
		{strings.Repeat("日", 81), 80, strings.Repeat("日", 80)},
		{strings.Repeat("語", 241), 240, strings.Repeat("語", 240)},
		{" " + strings.Repeat("日", 80), 80, strings.Repeat("日", 79)},
	} {
		if got := notificationText(tt.in, tt.limit); got != tt.want {
			t.Fatalf("got %q want %q", got, tt.want)
		}
	}
}
