package server

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/teststate"
	"github.com/kazuph/ikesu/internal/tui"
)

func TestConfigNotificationDeliveryChangesRealOutput(t *testing.T) {
	teststate.Lock(t)
	for _, delivery := range []string{"off", "herdr", "terminal", "system"} {
		t.Run(delivery, func(t *testing.T) {
			s := newTestServer(t, "notify-config")
			c, err := config.Decode([]byte("[ui.toast]\ndelivery='" + delivery + "'\ndelay_seconds=0\n[ui.toast.herdr]\nposition='bottom-right'\n"))
			if err != nil {
				t.Fatal(err)
			}
			s.cfg = c
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			captured := make(chan []byte, 1)
			go func() { b, _ := io.ReadAll(r); r.Close(); captured <- b }()
			s.app = tui.NewApp(s.app.State(), s.sup, w, s.app.State().Workspaces[0].Cwd)
			s.app.SetServerUI()
			s.app.SetSidebarConfig(c, false)
			// Terminal delivery goes through the production client writer and a real
			// local Unix socket, independently of the app's render output.
			dir, err := os.MkdirTemp("/tmp", "gtx-notify-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			listener, err := net.Listen("unix", filepath.Join(dir, "display.sock"))
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			remote, err := net.Dial("unix", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer remote.Close()
			local, err := listener.Accept()
			if err != nil {
				t.Fatal(err)
			}
			defer local.Close()
			s.client = &clientConn{conn: local, available: endpoint.Generation}
			defer s.client.close()
			messages := make(chan clientMsg, 1)
			go func() {
				var m clientMsg
				if json.NewDecoder(remote).Decode(&m) == nil {
					messages <- m
				}
			}()
			done := make(chan struct{})
			go func() { s.app.Run(); close(done) }()
			s.app.Do(func() { s.deliverNotification(1, "info") })
			if delivery == "terminal" || delivery == "system" {
				select {
				case m := <-messages:
					if delivery == "system" {
						if m.Type != "notify" || m.Notification == nil || !m.Notification.SystemToast || m.Notification.TargetPaneID != "p1" {
							t.Fatal(m)
						}
						break
					}
					b, err := base64.StdEncoding.DecodeString(m.Data)
					if err != nil || !strings.HasPrefix(string(b), "\x1b]9;") {
						t.Fatal(m, err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("terminal mode did not write OSC 9")
				}
			}
			// Synchronize the app loop and stop only after the pending frame was drawn.
			s.app.Do(func() {})
			s.app.Stop()
			<-done
			s.sup.KillAll()
			w.Close()
			data := <-captured
			emu := vt.NewEmulator(80, 24)
			go func() { _, _ = io.Copy(io.Discard, emu) }()
			_, _ = emu.Write(data)
			bottom := ""
			for y := 12; y < 24; y++ {
				for x := 40; x < 80; x++ {
					bottom += emu.CellAt(x, y).Content
				}
			}
			emu.Close()
			// The notification title is assembled by the real server from workspace 1.
			if strings.Contains(bottom, "1 workspace 1") != (delivery == "herdr") {
				t.Fatal(delivery, bottom)
			}
		})
	}
}
