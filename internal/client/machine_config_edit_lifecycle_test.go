package client

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/teststate"
	"github.com/kazuph/ikesu/internal/tui"
	"golang.org/x/sys/unix"
)

type configLifecycleFixture struct {
	m        *machineClient
	path     string
	cancel   context.CancelFunc
	original []byte
}

func configLifecycle(t *testing.T) configLifecycleFixture {
	t.Helper()
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	original := []byte("onboarding=true\n[theme]\nname='tokyo-night'\n")
	teststate.Setenv(t, config.EnvVar, path)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.CreateTemp(t.TempDir(), "terminal")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { out.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	m := &machineClient{ctx: ctx, events: make(chan endpointEvent), settings: &localSettings{config: &cfg}, endpoints: map[string]*managedEndpoint{"local": {profile: machine.Profile{ID: "local", Label: "Local", Enabled: true}}}, active: "local", order: []string{"local"}, session: "config-lifecycle", size: tui.Winsize{Cols: 80, Rows: 24}, out: out}
	t.Cleanup(func() { cancel(); m.workers.Wait() })
	return configLifecycleFixture{m, path, cancel, original}
}

func (f configLifecycleFixture) pausedSave(t *testing.T) func() {
	t.Helper()
	if err := os.Remove(f.path); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(f.path, 0600); err != nil {
		t.Fatal(err)
	}
	edit := config.Edit{Kind: "onboarding", Enabled: false}
	f.m.queueConfigEdit("local", msg{ConfigID: 17, ConfigEdit: &edit})
	deadline := time.Now().Add(5 * time.Second)
	var fd int
	for {
		var err error
		fd, err = unix.Open(f.path, unix.O_WRONLY|unix.O_NONBLOCK, 0600)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.ENXIO) || time.Now().After(deadline) {
			t.Fatal("configuration reader did not open actual FIFO", err)
		}
		time.Sleep(time.Millisecond)
	}
	writer := os.NewFile(uintptr(fd), f.path)
	var once sync.Once
	release := func() {
		once.Do(func() {
			if _, err := writer.Write(f.original); err != nil {
				t.Error(err)
			}
			if err := writer.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(release)
	return release
}

func configForm(t *testing.T, m *machineClient) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-m.events:
			if event.formUpdate != nil {
				event.formUpdate()
				return
			}
			m.event(event)
		case <-deadline:
			t.Fatal("configuration did not complete")
		}
	}
}

func configConnected(t *testing.T, m *machineClient) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-m.events:
			if event.formUpdate != nil {
				event.formUpdate()
				continue
			}
			m.event(event)
			if event.connection != nil {
				return
			}
		case <-deadline:
			t.Fatal("actual endpoint did not connect")
		}
	}
}

func configPeer(t *testing.T, m *machineClient) (*net.UnixConn, *json.Decoder, func()) {
	t.Helper()
	server, client := shutdownSocketPair(t)
	m.start("local", client)
	decoder := json.NewDecoder(server)
	var hello msg
	if err := decoder.Decode(&hello); err != nil {
		t.Fatal(err)
	}
	if hello.Type != "hello" || hello.Config == nil || hello.Config.Onboarding == nil || !*hello.Config.Onboarding {
		t.Fatal("expected original settings in real hello", hello)
	}
	welcome := func() {
		peer := endpoint.Current()
		if err := json.NewEncoder(server).Encode(msg{Type: "welcome", Peer: peer}); err != nil {
			t.Fatal(err)
		}
	}
	return server, decoder, welcome
}

func TestConfigurationEditsDrainAfterInputLoopExit(t *testing.T) {
	f := configLifecycle(t)
	server, decoder, welcome := configPeer(t, f.m)
	welcome()
	configConnected(t, f.m)
	release := f.pausedSave(t)
	for n, edit := range []config.Edit{{Kind: "invalid"}, {Kind: "onboarding", Enabled: true}, {Kind: "sound", Enabled: false}} {
		edit := edit
		f.m.queueConfigEdit("local", msg{ConfigID: uint64(18 + n), ConfigEdit: &edit})
	}
	f.cancel()
	done := make(chan struct{})
	go func() { f.m.workers.Wait(); close(done) }()
	release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("accepted configuration edits did not drain")
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Onboarding == nil || !*saved.Onboarding || saved.SoundEnabled() || saved.ThemeName() != "tokyo-night" {
		t.Fatalf("last accepted values lost: onboarding=%v sound=%v theme=%s", saved.Onboarding, saved.SoundEnabled(), saved.ThemeName())
	}
	server.SetReadDeadline(time.Now().Add(5 * time.Second))
	var late msg
	if err := decoder.Decode(&late); err == nil {
		t.Fatal("reply sent after input loop exit", late)
	}
}

func TestConfigurationEditResultUsesCurrentConnectionGeneration(t *testing.T) {
	for _, order := range []string{"same-generation", "connected-before-save", "connected-after-save"} {
		t.Run(order, func(t *testing.T) {
			f := configLifecycle(t)
			server, decoder, welcome := configPeer(t, f.m)
			welcome()
			configConnected(t, f.m)
			release := f.pausedSave(t)
			if order != "same-generation" {
				server, decoder, welcome = configPeer(t, f.m)
				if order == "connected-before-save" {
					welcome()
					configConnected(t, f.m)
				}
			}
			release()
			configForm(t, f.m)
			if order == "connected-after-save" {
				welcome()
				configConnected(t, f.m)
			}
			server.SetReadDeadline(time.Now().Add(5 * time.Second))
			var received msg
			if err := decoder.Decode(&received); err != nil {
				t.Fatal("latest configuration did not reach actual connection", err)
			}
			want := "view_config"
			if order == "same-generation" {
				want = "config_result"
			}
			if received.Type != want || received.Config == nil || received.Config.Onboarding == nil || *received.Config.Onboarding {
				t.Fatal("wrong configuration delivery", received)
			}
			if order == "same-generation" && received.ConfigID != 17 {
				t.Fatal("original ConfigID was not acknowledged", received)
			}
			if order != "same-generation" && received.ConfigID != 0 {
				t.Fatal("stale ConfigID sent to a new connection", received)
			}
			server.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			var extra msg
			if err := decoder.Decode(&extra); err == nil {
				t.Fatal("unexpected extra reply", extra)
			}
		})
	}
}
