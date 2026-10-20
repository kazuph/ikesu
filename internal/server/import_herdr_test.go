package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/herdrimport"
	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/teststate"
)

func importSource(t *testing.T) string {
	t.Helper()
	d := teststate.TempDir(t)
	text := `{"version":5,"active":0,"selected":0,"sidebar_width":27,"workspaces":[{"id":"s5","custom_name":"imported","section":"work","identity_cwd":CWD,"active_tab":0,"public_tab_numbers":[1],"next_public_tab_number":2,"tabs":[{"custom_name":"saved tab","layout":{"Pane":1},"pane_order":[1],"focused":1,"root_pane":1,"panes":{"1":{"global_pane_number":1,"cwd":CWD,"agent_session":{"agent":"claude","kind":"id","value":"exact-import-id"}}}}]}]}`
	cwd, _ := json.Marshal(d)
	text = strings.ReplaceAll(text, "CWD", string(cwd))
	if err := os.WriteFile(filepath.Join(d, "session.json"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return d
}
func TestImportHerdrLiveSessionRetainsPTYAndUsesExactArgv(t *testing.T) {
	teststate.Lock(t)
	source := importSource(t)
	fake := filepath.Join(source, "claude")
	capture := filepath.Join(source, "argv")
	script := "#!/bin/sh\nexport PATH=/usr/bin:/bin\nprintf '%s\\n' \"$@\" > " + capture + "\nprintf 'IMPORTED_ARGV_READY\\n'\n"
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(source, "config.toml")
	cfgText := "onboarding=false\n[session]\nresume_agents_on_restore=false\n[agent_start.commands]\nclaude=[" + strconv.Quote(fake) + "]\n"
	os.WriteFile(cfgPath, []byte(cfgText), 0600)
	teststate.Setenv(t, config.EnvVar, cfgPath)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	ready.conn.Close()
	old := firstPaneID(t, dir)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	p, err := herdrimport.Load(source, cfg)
	if err != nil {
		t.Fatal(err)
	}
	apiOK(t, dir, api.MethodSessionImportHerdr, api.SessionImportHerdrParams{Source: source, Digest: p.Digest}, nil)
	ids := paneIDs(t, dir)
	if len(ids) != 2 || ids[0] != old {
		t.Fatalf("existing pane lost: %v", ids)
	}
	waitRead(t, dir, ids[1], "IMPORTED_ARGV_READY")
	b, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "--resume\nexact-import-id\n" {
		t.Fatalf("wrong argv: %q", b)
	}
	apiOK(t, dir, api.MethodPaneSendText, api.PaneSendTextParams{PaneID: old, Text: "echo EXISTING_PTY_ALIVE\n"}, nil)
	waitRead(t, dir, old, "EXISTING_PTY_ALIVE")
	f := waitForSessionFile(t, dir)
	if len(f.Workspaces) != 2 || f.Active != 1 || f.Workspaces[1].Tabs[0].Name != "saved tab" || f.SidebarWidth != 27 {
		t.Fatalf("bad saved import: %+v", f)
	}
}
func TestImportHerdrRejectChangedSourceAndSpawnFailureWithoutStateChange(t *testing.T) {
	teststate.Lock(t)
	source := importSource(t)
	s := newTestServer(t, "import-failure")
	defer s.sup.KillAll()
	p, err := herdrimport.Load(source, s.cfg)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(s.app.State().Snapshot())
	req := &api.Request{ID: "test", Params: api.MarshalParams(api.SessionImportHerdrParams{Source: source, Digest: "changed"})}
	if resp := s.importHerdr(req); resp.Err == nil {
		t.Fatal("changed source accepted")
	}
	s.sup.SetShell("/missing-import-shell", "interactive")
	req.Params = api.MarshalParams(api.SessionImportHerdrParams{Source: source, Digest: p.Digest})
	if resp := s.importHerdr(req); resp.Err == nil {
		t.Fatal("spawn failure accepted")
	}
	after, _ := json.Marshal(s.app.State().Snapshot())
	if string(before) != string(after) {
		t.Fatal("failure changed session")
	}
	if _, err := os.Stat(session.Path(s.dir)); !os.IsNotExist(err) {
		t.Fatal("failure wrote session")
	}
}

func TestGtxZoomSurvivesSavedSessionRestart(t *testing.T) {
	teststate.Lock(t)
	base := teststate.TempDir(t)
	teststate.Setenv(t, "GTX_STATE_DIR", base)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	dir, done := startServerAt(t, "zoom-persist")
	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{Direction: "right", Focus: true}, nil)
	ids := paneIDs(t, dir)
	pid := ids[1]
	apiOK(t, dir, api.MethodPaneZoom, api.PaneZoomParams{PaneID: &pid, Mode: "on"}, nil)
	stopServer(t, dir, done)
	f, err := session.Load(session.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	want, ok := api.ParsePaneRef(pid)
	if !ok {
		t.Fatal(pid)
	}
	if f.Workspaces[0].Tabs[0].Zoomed != want {
		t.Fatal("shutdown lost zoom")
	}
	dir, done = startServerAt(t, "zoom-persist")
	defer stopServer(t, dir, done)
	var got api.PaneZoomEnvelope
	apiOK(t, dir, api.MethodPaneZoom, api.PaneZoomParams{PaneID: &pid, Mode: "on"}, &got)
	if got.Zoom.Changed || !got.Zoom.Zoomed {
		t.Fatalf("restart did not keep zoom: %+v", got)
	}
}
