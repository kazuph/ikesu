package client

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

func TestRemoteClipboardConsumesCommandVAndKeepsOtherEndpointResponsive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	makeConnection := func() *endpointConnection {
		peer := endpoint.Current()
		return &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	}
	remote, local := makeConnection(), makeConnection()
	st := model.New(t.TempDir(), 160, 45)
	m := &machineClient{ctx: ctx, active: "remote", events: make(chan endpointEvent), regions: &render.Output{}, endpoints: map[string]*managedEndpoint{
		"remote": {connection: remote, snapshot: &render.Input{State: st}}, "local": {connection: local},
	}}
	// PLAN explicitly requires a fake clipboard rather than the owner's pasteboard.
	path := filepath.Join(t.TempDir(), "clipboard.txt")
	want := "日本語\n'quoted'\tend"
	if err := os.WriteFile(path, []byte(want), 0600); err != nil { t.Fatal(err) }
	ready := make(chan struct{})
	m.readClipboard = func(ctx context.Context) (clipboardContent, error) {
		select { case <-ready: case <-ctx.Done(): return clipboardContent{}, ctx.Err() }
		data, err := os.ReadFile(path)
		return clipboardContent{Text: string(data)}, err
	}
	defer func() { cancel(); m.workers.Wait() }()
	var framer input.Framer
	for _, event := range framer.Push([]byte("\x1b[118;9u\x1b[118;9:3u")) {
		if err := m.input(event); err != nil { t.Fatal(err) }
	}
	if m.endpoints["remote"].paste == nil { t.Fatal("Command-V did not start clipboard read") }
	if err := m.sendInput("remote", []byte("after-paste")); err != nil { t.Fatal(err) }
	if err := m.sendInput("local", []byte("local-still-works")); err != nil { t.Fatal(err) }
	if len(local.queue) != 1 || len(remote.queue) != 0 { t.Fatal("clipboard read blocked Local or leaked Command-V") }
	close(ready)
	select {
	case event := <-m.events: event.formUpdate()
	case <-time.After(5*time.Second): t.Fatal("clipboard read did not finish")
	}
	if len(remote.queue) != 2 { t.Fatal("paste or following input missing", remote.queue) }
	for i, expected := range []string{"\x1b[200~"+want+"\x1b[201~", "after-paste"} {
		data, err := base64.StdEncoding.DecodeString(remote.queue[i].Data)
		if err != nil || string(data) != expected { t.Fatalf("message %d = %q, %v", i, data, err) }
	}
}

func TestClipboardReadFailureDoesNotLeakCommandVOrDiscardFollowingInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background()); defer cancel()
	peer := endpoint.Current()
	c := &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	m := &machineClient{ctx: ctx, active: "remote", events: make(chan endpointEvent), endpoints: map[string]*managedEndpoint{"remote": {connection: c, snapshot: &render.Input{State: model.New(t.TempDir(),160,45)}}}}
	m.readClipboard = func(context.Context) (clipboardContent,error) { _,err := os.ReadFile(filepath.Join(t.TempDir(),"absent")); return clipboardContent{},err }
	defer func(){cancel();m.workers.Wait()}()
	if err := m.pasteClipboard(); err != nil { t.Fatal(err) }
	if err := m.sendInput("remote",[]byte("kept"));err != nil {t.Fatal(err)}
	select {case event:= <-m.events: event.formUpdate(); case <-time.After(5*time.Second):t.Fatal("read failure not delivered")}
	if m.notice=="" || len(c.queue)!=1 {t.Fatal("missing explicit error or input",m.notice,c.queue)}
	if data,_:=base64.StdEncoding.DecodeString(c.queue[0].Data);string(data)!="kept" {t.Fatal("following input changed")}
	var f input.Framer
	release := f.Push([]byte("\x1b[118;9:3u"))
	if len(release)!=1 || release[0].Kind!=input.KindUnknown || release[0].Key.Mod!=uv.ModSuper {t.Fatal("release not decoded",release)}
}
