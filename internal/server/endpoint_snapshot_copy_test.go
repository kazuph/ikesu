package server

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestHandoffSnapshotCopyNegotiation(t *testing.T) {
	for _, tc := range []struct {
		name, reason               string
		legacyClient, legacyServer bool
	}{
		{name: "supported"},
		{name: "supported hook failure after switching", reason: "pane not found"},
		{name: "legacy client", legacyClient: true},
		{name: "legacy server", legacyServer: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			teststate.Lock(t)
			modern := !tc.legacyClient && !tc.legacyServer
			withoutCopy := func(peer endpoint.Peer) endpoint.Peer {
				peer.Methods = slices.DeleteFunc(peer.Methods, func(method string) bool {
					return method == endpoint.CopyTextMessage
				})
				return peer
			}
			started, release := make(chan struct{}, 1), make(chan struct{}, 1)
			const markdown = "# handoff\n概要"
			encoded := encode64([]byte(markdown))
			osc := "\x1b]52;c;" + encoded + "\a"
			s, _ := startClipboardEndpointImported(t, nil, nil, func(s *Server) {
				if tc.legacyServer {
					s.endpointPeer = withoutCopy(s.endpointPeer)
				}
				s.app.SetSnapshotHook(func(int) (string, error) {
					started <- struct{}{}
					<-release
					if tc.reason != "" {
						return markdown, errors.New(tc.reason)
					}
					return markdown, nil
				})
			})
			defer close(release)
			c := dialClient(t, s.dir)
			defer c.conn.Close()
			peer := endpoint.MachinePeer()
			if tc.legacyClient {
				peer = withoutCopy(peer)
			}
			c.send(t, clientMsg{Type: "hello", Peer: peer, Cols: 120, Rows: 35})
			c.readUntil(t, time.Second, func(clientMsg) bool { return c.lastInput != nil })
			if c.lastInput == nil {
				t.Fatal("missing initial view")
			}

			copySnapshot := func() {
				t.Helper()
				rect := c.lastInput.State.DisplayPaneRects()[0].Rect
				inner, _ := model.PaneContentRect(rect, c.lastInput.State.PaneBorders)
				x, y := inner.X+2, inner.Y+2
				click := fmt.Sprintf("\x1b[<2;%d;%dM\x1b[<2;%d;%dm", x, y, x, y)
				c.send(t, clientMsg{Type: "input", Data: encode64([]byte(click))})
				c.readUntil(t, time.Second, func(clientMsg) bool { return c.lastInput.Menu != nil })
				menu := c.lastInput.Menu
				if menu == nil {
					t.Fatal("missing pane menu")
				}
				var down int
				for i := menu.Sel + 1; i < len(menu.Items); i++ {
					item := menu.Items[i]
					if !item.Separator && !item.Disabled {
						down++
					}
					if item.Label == "Copy handoff snapshot" && !item.Disabled {
						c.send(t, clientMsg{Type: "input", Data: encode64([]byte(strings.Repeat("\x1b[B", down) + "\r"))})
						select {
						case <-started:
						case <-time.After(time.Second):
							t.Fatal("snapshot hook did not start")
						}
						return
					}
				}
				t.Fatal("missing enabled copy action")
			}
			copySnapshot()

			if modern {
				// The workspace snapshot acknowledges the visibility change
				// queued ahead of it on the initiating display socket.
				hidden := false
				c.send(t, clientMsg{Type: "visibility", Visible: &hidden})
				workspace := c.lastInput.State.ActiveWorkspace().ID
				c.send(t, clientMsg{Type: "workspace_select", Workspace: workspace, InputSequence: 1})
				acknowledged := false
				c.readUntil(t, time.Second, func(m clientMsg) bool {
					acknowledged = m.ProcessedInput == 1
					return acknowledged
				})
				if !acknowledged {
					t.Fatal("server did not process visibility barrier")
				}
			}
			release <- struct{}{}
			var result clientMsg
			messages := c.readUntil(t, time.Second, func(m clientMsg) bool {
				if m.Type == endpoint.CopyTextMessage {
					result = m
					return true
				}
				return c.lastInput.Copied != ""
			})
			hasOSC := strings.Contains(outputs(messages), "\x1b]52;c;")
			if modern {
				wantData := encoded
				if tc.reason != "" {
					wantData = ""
				}
				if result.Type != endpoint.CopyTextMessage || result.Data != wantData || result.Reason != tc.reason || hasOSC || c.lastInput.Copied != "" {
					t.Fatalf("modern output: type=%q data=%q reason=%q OSC=%t copied=%q", result.Type, result.Data, result.Reason, hasOSC, c.lastInput.Copied)
				}
				// Draw once more after delivery to detect a server-rendered
				// copy status or a cancellation replay on the next visit.
				visible := true
				c.send(t, clientMsg{Type: "visibility", Visible: &visible})
				c.send(t, clientMsg{Type: "workspace_select", Workspace: c.lastInput.State.ActiveWorkspace().ID, InputSequence: 2})
				acknowledged := false
				messages := c.readUntil(t, time.Second, func(m clientMsg) bool {
					acknowledged = m.ProcessedInput == 2
					return acknowledged
				})
				if !acknowledged || c.lastInput.Copied != "" || c.lastInput.Toast != nil || strings.Contains(outputs(messages), osc) {
					t.Fatal("modern viewer rendered server-side copy feedback")
				}
			} else if result.Type != "" || !strings.Contains(outputs(messages), osc) || c.lastInput.Copied != "Copied 2 lines" {
				t.Fatalf("legacy output: copy type=%q OSC=%t copied=%q", result.Type, hasOSC, c.lastInput.Copied)
			}
		})
	}
}
