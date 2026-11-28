package client

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/machine"
)

func TestMachineAutoDisconnectDeadlineAndPersistence(t *testing.T) {
	for _, tc := range []struct {
		name          string
		after         time.Duration
		connectsFirst bool
	}{
		{name: "off"},
		{name: "initial attempt", after: 10 * time.Minute},
		{name: "lost connection", after: 10 * time.Minute, connectsFirst: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("IKESU_STATE_DIR", t.TempDir())
			configPath := filepath.Join(t.TempDir(), "invalid.toml")
			if err := os.WriteFile(configPath, []byte("["), 0600); err != nil {
				t.Fatal(err)
			}
			// A retry can start its worker without running a real SSH command.
			t.Setenv(config.EnvVar, configPath)
			synctest.Test(t, func(t *testing.T) {
				cfg := &config.Config{}
				cfg.Remote.AutoDisconnectAfter = tc.after
				ctx, cancel := context.WithCancel(context.Background())
				viewer, peer := net.Pipe()
				m := &machineClient{ctx: ctx, session: "auto-disconnect", active: "local", settings: &localSettings{config: cfg},
					events: make(chan endpointEvent, 1), endpoints: map[string]*managedEndpoint{"local": {}}}
				defer func() { cancel(); peer.Close(); m.workers.Wait() }()
				catalog := machine.Catalog{Path: m.catalogPath()}
				profile, err := catalog.AddVerified("host", "Remote", "default")
				if err != nil {
					t.Fatal(err)
				}
				remote := &managedEndpoint{profile: profile}
				m.endpoints[profile.ID] = remote
				m.start(profile.ID, viewer)
				attempt := remote.attempt
				since := time.Now()
				if tc.connectsFirst {
					time.Sleep(tc.after / 2)
					m.event(endpointEvent{id: profile.ID, attempt: attempt, connection: &endpointConnection{}})
					if remote.status != "online" || !m.nextDeadline().IsZero() {
						t.Fatal("an online machine retained its initial auto-disconnect deadline")
					}
					time.Sleep(tc.after)
					since = time.Now()
				} else {
					time.Sleep(time.Minute)
				}
				m.event(endpointEvent{id: profile.ID, attempt: attempt, err: errors.New("first failure")})
				time.Sleep(time.Minute)
				retryViewer, retryPeer := net.Pipe()
				defer retryPeer.Close()
				m.start(profile.ID, retryViewer)
				attempt = remote.attempt
				lastReason := "SSH connection refused"
				m.failAt(profile.ID, errors.New(lastReason), time.Now())
				// Model a retry later than the auto-disconnect deadline.
				retryAt := since.Add(11 * time.Minute)
				remote.retry.next = retryAt
				if tc.after == 0 {
					time.Sleep(10*time.Minute - time.Since(since))
					m.advance(time.Now())
					if !catalog.Load()[0].Enabled || remote.status != "reconnecting" || m.failureToast != nil || !m.nextDeadline().Equal(retryAt) {
						t.Fatalf("auto-disconnect with zero changed the machine or retry: %+v", remote)
					}
					time.Sleep(time.Until(retryAt))
					m.advance(time.Now())
					if remote.attempt != attempt+1 {
						t.Fatal("retry did not start with auto-disconnect disabled")
					}
					return
				}
				deadline := since.Add(tc.after)
				if got := m.nextDeadline(); !got.Equal(deadline) {
					t.Fatalf("deadline = %v, want %v", got, deadline)
				}
				time.Sleep(time.Until(deadline) - time.Nanosecond)
				m.advance(time.Now())
				if !catalog.Load()[0].Enabled || remote.status != "reconnecting" {
					t.Fatal("machine disconnected before its deadline")
				}
				time.Sleep(time.Nanosecond)
				m.advance(time.Now())
				saved := catalog.Load()
				if len(saved) != 1 || saved[0].Enabled || remote.profile != saved[0] || remote.status != "disconnected" || remote.retry != (retryPolicy{}) || remote.attempt != attempt+1 {
					t.Fatalf("deadline did not save the disconnect and stop retrying: profile=%+v state=%+v", saved, remote)
				}
				if m.failureToast == nil || !strings.Contains(m.failureToast.Title, "Remote: automatically disconnected") || !strings.Contains(m.failureToast.Body, lastReason) {
					t.Fatalf("disconnect notice did not include the reason: %+v", m.failureToast)
				}
				if !m.nextDeadline().Equal(m.noticeUntil) {
					t.Fatal("a disconnected machine still has a connection deadline")
				}
			})
		})
	}
}
