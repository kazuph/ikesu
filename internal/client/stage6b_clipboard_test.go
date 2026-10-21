package client

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

func TestStage6bLocalModalClipboard(t *testing.T) {
	for _, mode := range []string{"name", "navigator", "copy", "empty", "cancelled"} {
		for _, mod := range []uv.KeyMod{uv.ModCtrl, uv.ModSuper} {
			t.Run(fmt.Sprintf("%s-%d", mode, mod), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				peer := endpoint.Current()
				connection := &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
				view := &render.Input{State: model.New(t.TempDir(), 160, 45), Dialog: &render.Dialog{InputEdit: true}}
				if mode == "navigator" {
					view.Dialog = nil
					view.Navigator = &render.Navigator{Searching: true}
				}
				if mode == "copy" {
					view.Dialog = nil
					view.Copy = &render.CopyMark{Pane: 1}
					view.CopyHint = " COPY  /█  enter search  esc cancel"
				}
				m := &machineClient{ctx: ctx, active: "local", regions: &render.Output{}, events: make(chan endpointEvent), endpoints: map[string]*managedEndpoint{"local": {connection: connection, snapshot: view}}}
				path := filepath.Join(t.TempDir(), "clipboard.txt")
				text := "日本語\talpha\n"
				if mode == "empty" {
					text = ""
				}
				if err := os.WriteFile(path, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
				m.readClipboard = func(context.Context) (clipboardContent, error) {
					data, err := os.ReadFile(path)
					return clipboardContent{Text: string(data)}, err
				}
				defer func() { cancel(); m.workers.Wait() }()
				if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: 'v', Mod: mod}}); err != nil {
					t.Fatal(err)
				}
				if err := m.input(input.Event{Kind: input.KindKeyRelease, Key: uv.Key{Code: 'v', Mod: mod}}); err != nil {
					t.Fatal(err)
				}
				if len(connection.queue) != 0 {
					t.Fatal("modal release leaked to pane")
				}
				if m.endpoints["local"].paste == nil {
					t.Fatal("modal key did not start read")
				}
				if mode == "cancelled" {
					view.Dialog = nil
				}
				select {
				case event := <-m.events:
					event.formUpdate()
				case <-time.After(5 * time.Second):
					t.Fatal("clipboard read did not finish")
				}
				if mode == "cancelled" || mode == "empty" {
					if len(connection.queue) != 0 {
						t.Fatal("closed dialog leaked text to pane")
					}
					return
				}
				if len(connection.queue) != 1 {
					t.Fatal("missing paste")
				}
				data, err := base64.StdEncoding.DecodeString(connection.queue[0].Data)
				want := text
				if connection.queue[0].Type != "paste" {
					t.Fatal("modal clipboard was reframed as keys")
				}
				if err != nil || string(data) != want {
					t.Fatalf("got %q want %q: %v", data, want, err)
				}
			})
		}
	}
}
