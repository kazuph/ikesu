package tui

import (
	"encoding/json"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/plugin"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginConfiguredKeyAndControlHyperlinkActualPTY(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	defer a.sup.KillAll()
	root := t.TempDir()
	manifest := `id="ui.test"
name="UI"
version="0.1.0"
min_gtx_version="0.1.0"
[[actions]]
id="capture"
title="Capture"
command=["/bin/sh","-c","printf '%s' \"$GTX_PLUGIN_CONTEXT_JSON\""]
[[link_handlers]]
id="link"
title="Link"
pattern="^https://example[.]test/"
action="capture"
`
	if err := os.WriteFile(filepath.Join(root, "gtx-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	store := &plugin.Store{ConfigRoot: t.TempDir(), StateRoot: t.TempDir(), Version: "0.1.0"}
	if _, err := store.Link(root, true, nil); err != nil {
		t.Fatal(err)
	}
	engine := &plugin.Engine{Store: store, BinPath: "/gtx", SocketPath: "/socket"}
	a.SetPluginHost(func() *plugin.Engine { return engine }, nil)
	var cfg config.Config
	cfg.Keys.Command = []config.PluginKeyBinding{{Key: "prefix+l", Type: "plugin_action", Command: "ui.test.capture"}}
	a.SetSidebarConfig(cfg, false)
	pid := a.st.FocusedPane()
	rect := a.rectForPane(pid)
	if _, err := a.sup.SpawnWith(pid, a.cwd, rect.W, rect.H, nil, []string{"/bin/cat"}); err != nil {
		t.Fatal(err)
	}
	a.prefixKey(uv.Key{Code: 'l'})
	engine.Wait()
	logs, err := store.Logs("ui.test", nil)
	if err != nil || len(logs) != 1 || logs[0].Stdout == nil {
		t.Fatal(logs, err)
	}
	var ctx plugin.Context
	if err := json.Unmarshal([]byte(*logs[0].Stdout), &ctx); err != nil {
		t.Fatal(err)
	}
	if ctx["workspace_id"] != "s1" || ctx["focused_pane_id"] != "p1" || ctx["invocation_source"] != "keybind" {
		t.Fatal(ctx)
	}
	a.sup.Kill(pid)
	p, err := a.sup.SpawnWith(pid, a.cwd, rect.W, rect.H, nil, []string{"/bin/sh", "-c", "printf '\033]8;;https://example.test/hidden\033\\Click\033]8;;\033\\'; exec /bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "Click")
	r := a.innerRect(pid)
	mouse := uv.Mouse{X: r.X + 1, Y: r.Y, Button: uv.MouseLeft}
	if a.pluginLinkClick(pid, mouse) {
		t.Fatal("plain click invoked handler")
	}
	mouse.Mod = uv.ModCtrl
	if !a.pluginLinkClick(pid, mouse) {
		t.Fatal("control hyperlink not handled")
	}
	engine.Wait()
	logs, err = store.Logs("ui.test", nil)
	if err != nil || len(logs) != 2 {
		t.Fatal(logs, err)
	}
	if err := json.Unmarshal([]byte(*logs[0].Stdout), &ctx); err != nil {
		t.Fatal(err)
	}
	if ctx["clicked_url"] != "https://example.test/hidden" || ctx["link_handler_id"] != "link" || ctx["invocation_source"] != "link_click" {
		t.Fatal(ctx)
	}
}
