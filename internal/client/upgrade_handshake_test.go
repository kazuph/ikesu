package client

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/tui"
)

// The real first-attach deadline is ten seconds; the peer accepts hello but never sends welcome.
func TestUpgradeWelcomeTimeoutRequestsRollback(t *testing.T) {
	server, client := shutdownSocketPair(t)
	// Override the pair helper's short deadline: the production handshake must impose its own.
	if err := client.SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := server.SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	out, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cfg := config.Config{}
	m := &machineClient{ctx: ctx, session: "upgrade", settings: &localSettings{config: &cfg, upgradeState: &upgradeState{}}, active: "local", order: []string{"local"}, size: tui.Winsize{Cols: 80, Rows: 24}, out: out, events: make(chan endpointEvent), endpoints: map[string]*managedEndpoint{"local": {profile: machine.Profile{ID: "local", Enabled: true}}}}
	defer func() { cancel(); m.workers.Wait() }()
	m.start("local", client)
	var hello msg
	if err := json.NewDecoder(server).Decode(&hello); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-m.events:
		if event.err == nil {
			t.Fatal("welcome timeout did not report an error")
		}
		result, done := m.event(event)
		if !done || result != pumpRollback {
			t.Fatalf("timeout result: %v %v", result, done)
		}
	case <-time.After(attachTimeout + 2*time.Second):
		t.Fatal("missing welcome did not trigger rollback")
	}
}
