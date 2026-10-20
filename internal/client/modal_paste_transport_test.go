package client

import (
	"context"
	"encoding/base64"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestModalClipboardTerminatorsStayTyped(t *testing.T) {
	for _, mode := range []string{"rename", "navigator", "search"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			peer := endpoint.Current()
			conn := &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
			view := &render.Input{State: model.New(t.TempDir(), 160, 45)}
			switch mode {
			case "rename":
				view.Dialog = &render.Dialog{InputEdit: true}
			case "navigator":
				view.Navigator = &render.Navigator{Searching: true}
			case "search":
				view.Copy = &render.CopyMark{Pane: 1}
				view.CopyHint = " COPY  /█  enter search  esc cancel"
			}
			m := &machineClient{ctx: ctx, active: "local", regions: &render.Output{}, events: make(chan endpointEvent), endpoints: map[string]*managedEndpoint{"local": {connection: conn, snapshot: view}}}
			payload := "日本語\x1b[200~alpha\x1b[201~\rX\n\x1bEND"
			path := filepath.Join(t.TempDir(), "test-clipboard")
			if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
				t.Fatal(err)
			}
			m.readClipboard = func(context.Context) (clipboardContent, error) {
				b, err := os.ReadFile(path)
				return clipboardContent{Text: string(b)}, err
			}
			defer func() { cancel(); m.workers.Wait() }()
			if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: 'v', Mod: uv.ModCtrl}}); err != nil {
				t.Fatal(err)
			}
			select {
			case ev := <-m.events:
				ev.formUpdate()
			case <-time.After(5 * time.Second):
				t.Fatal("read timeout")
			}
			if len(conn.queue) != 1 || conn.queue[0].Type != "paste" {
				t.Fatalf("not a typed paste: %+v", conn.queue)
			}
			got, err := base64.StdEncoding.DecodeString(conn.queue[0].Data)
			if err != nil || string(got) != payload {
				t.Fatalf("body=%q err=%v", got, err)
			}
		})
	}
}

func TestModalPasteUnsupportedPeerRejectsWithoutQueueing(t *testing.T) {
	c := &endpointConnection{writer: &writer{available: 1, methods: []string{"input"}}, wake: make(chan struct{}, 1)}
	m := &machineClient{endpoints: map[string]*managedEndpoint{"local": {connection: c}}}
	err := m.sendModalPaste("local", "alpha\x1b[201~\rX")
	if err == nil || err.Error() != "paste unsupported" || len(c.queue) != 0 {
		t.Fatal("unsupported paste sent", err, c.queue)
	}
}
