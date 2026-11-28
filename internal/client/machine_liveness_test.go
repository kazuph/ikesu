package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/machine"
)

func TestRemoteProbeSupervisesIdleDisplayConnection(t *testing.T) {
	t.Setenv("IKESU_STATE_DIR", t.TempDir())
	for _, tc := range []struct {
		name       string
		responds   bool
		advertises bool
	}{
		{name: "stalled remote", advertises: true},
		{name: "responsive idle remote", advertises: true, responds: true},
		{name: "legacy remote"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				client, server := net.Pipe()
				remote := &managedEndpoint{profile: machine.Profile{ID: "remote", Label: "Mini"}}
				m := &machineClient{ctx: ctx, active: "local", settings: &localSettings{}, events: make(chan endpointEvent, 8), endpoints: map[string]*managedEndpoint{
					"local": {}, "remote": remote,
				}}
				defer func() {
					cancel()
					server.Close()
					m.workers.Wait()
				}()
				peer := endpoint.Current()
				if !tc.advertises {
					peer.Methods = []string{"visibility", "workspace_select"}
				}
				dec, enc := json.NewDecoder(server), json.NewEncoder(server)
				read := func() msg {
					t.Helper()
					server.SetReadDeadline(time.Now().Add(time.Second))
					var request msg
					if err := dec.Decode(&request); err != nil {
						t.Fatal(err)
					}
					return request
				}
				applyEvent := func() {
					t.Helper()
					synctest.Wait()
					select {
					case event := <-m.events:
						m.event(event)
					default:
						t.Fatal("missing endpoint event")
					}
				}
				m.start("remote", client)
				if hello := read(); hello.Type != "hello" {
					t.Fatalf("unexpected handshake: %+v", hello)
				}
				if err := enc.Encode(msg{Type: "welcome", Peer: peer}); err != nil {
					t.Fatal(err)
				}
				applyEvent()
				if remote.status != "online" || remote.connection == nil {
					t.Fatalf("welcome did not establish a connection: %s %s", remote.status, remote.reason)
				}
				if !tc.advertises {
					if !m.nextDeadline().IsZero() {
						t.Fatal("legacy peer was scheduled for an unsupported request")
					}
					return
				}
				for range 3 {
					if next := m.nextDeadline(); !next.Equal(time.Now().Add(remoteProbeInterval)) {
						t.Fatalf("next probe = %v", next)
					}
					time.Sleep(remoteProbeInterval)
					m.advance(time.Now())
					if request := read(); request.Type != "oyagoi_check" || request.Workspace != 0 || request.WorkspaceGeneration != "" {
						t.Fatalf("unexpected probe: %+v", request)
					}
					if next := m.nextDeadline(); !next.Equal(time.Now().Add(remoteProbeTimeout)) {
						t.Fatalf("reply deadline = %v", next)
					}
					time.Sleep(remoteProbeTimeout - time.Nanosecond)
					m.advance(time.Now())
					if remote.status != "online" || remote.connection == nil {
						t.Fatal("remote disconnected before its reply deadline")
					}
					if !tc.responds {
						attempt := remote.attempt
						time.Sleep(time.Nanosecond)
						m.advance(time.Now())
						m.workers.Wait()
						m.event(endpointEvent{id: "remote", attempt: attempt, err: net.ErrClosed})
						want := fmt.Sprintf("remote ikesu not responding for %s", remoteProbeTimeout)
						if remote.status != "reconnecting" || remote.connection != nil || remote.reason != want {
							t.Fatalf("stalled remote did not disconnect with its timeout reason: %s %s", remote.status, remote.reason)
						}
						if remote.ctx.Err() == nil || !remote.retry.next.Equal(time.Now().Add(initialRetryDelay)) || !m.nextDeadline().Equal(remote.retry.next) {
							t.Fatalf("reconnect was not scheduled by retryPolicy: %+v", remote.retry)
						}
						return
					}
					exists := false
					if err := enc.Encode(msg{Type: "oyagoi_check_result", OyagoiExists: &exists}); err != nil {
						t.Fatal(err)
					}
					applyEvent()
				}
				if remote.status != "online" || remote.connection == nil || !remote.retry.next.IsZero() {
					t.Fatal("idle remote was treated as disconnected despite responding")
				}
			})
		})
	}
}
