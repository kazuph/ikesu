package server

import (
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
	set("ikesu")
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
	set("ikesu")
	call("shown")
}

func TestStage6aFixTerminalWriteFailure(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "terminal-write-failure")
	s.cfg, _ = config.Decode([]byte("[ui.toast]\ndelivery='terminal'\n"))
	dir, err := os.MkdirTemp("/tmp", "ikesu-notify-")
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
