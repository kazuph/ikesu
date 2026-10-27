package client

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestConfigurationEditQueuePreservesOrderAndContinuesAfterError(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, config.EnvVar, path)
	if err := os.WriteFile(path, []byte("onboarding=true\n[theme]\nname='tokyo-night'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &machineClient{ctx: ctx, events: make(chan endpointEvent), settings: &localSettings{config: &cfg}, endpoints: map[string]*managedEndpoint{"local": {attempt: 1}}}
	defer func() { cancel(); m.workers.Wait() }()
	edits := []config.Edit{{Kind: "onboarding", Enabled: false}, {Kind: "invalid"}, {Kind: "onboarding", Enabled: true}, {Kind: "sound", Enabled: false}}
	for n, edit := range edits {
		edit := edit
		m.queueConfigEdit("local", msg{ConfigID: uint64(n + 1), ConfigEdit: &edit})
	}
	for range edits {
		select {
		case event := <-m.events:
			if event.formUpdate == nil {
				t.Fatal("configuration did not return to event loop")
			}
			event.formUpdate()
		case <-time.After(5 * time.Second):
			t.Fatal("configuration queue did not finish")
		}
	}
	if len(m.configEdits) != 0 {
		t.Fatal("configuration request remains queued")
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Onboarding == nil || !*saved.Onboarding || saved.ThemeName() != "tokyo-night" {
		t.Fatal("edit order or unrelated configuration changed")
	}
	if m.settings.config.Onboarding == nil || !*m.settings.config.Onboarding {
		t.Fatal("returned state differs from persisted state")
	}
}
