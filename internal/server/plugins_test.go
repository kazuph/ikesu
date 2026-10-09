package server

import (
	"encoding/json"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/plugin"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func startPluginServer(t *testing.T) (string, chan int) {
	t.Helper()
	pinTestConfig(t)
	base, err := os.MkdirTemp("/tmp", "gtxp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	teststate.Setenv(t, "GTX_STATE_DIR", filepath.Join(base, "state"))
	teststate.Setenv(t, "XDG_CONFIG_HOME", filepath.Join(base, "config"))
	teststate.Setenv(t, "SHELL", "/bin/sh")
	// Config path is already isolated by pinTestConfig. The registry follows it.
	dir := api.SessionDir("test")
	done := make(chan int, 1)
	go func() {
		code, err := Run("test", "0.1.0")
		if err != nil {
			t.Errorf("Run: %v", err)
		}
		done <- code
	}()
	c := dialClient(t, dir)
	c.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24})
	c.readUntil(t, 10*time.Second, func(m clientMsg) bool { return m.Type == "output" })
	t.Cleanup(func() { c.conn.Close() })
	return dir, done
}
func TestPluginSocketActionAndEvent(t *testing.T) {
	teststate.Lock(t)
	dir, done := startPluginServer(t)
	defer stopServer(t, dir, done)
	root := t.TempDir()
	manifest := `id="socket.test"
name="Socket Test"
version="0.1.0"
min_gtx_version="0.1.0"
platforms=["macos","linux"]
[[actions]]
id="context"
title="Context"
command=["/bin/sh","-c","printf '%s' \"$GTX_PLUGIN_CONTEXT_JSON\""]
[[events]]
on="workspace.created"
command=["/bin/sh","-c","printf '%s' \"$GTX_PLUGIN_EVENT\""]
`
	if err := os.WriteFile(filepath.Join(root, "gtx-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	apiOK(t, dir, "plugin.link", map[string]any{"path": root}, nil)
	var invoked struct {
		Type    string            `json:"type"`
		Action  plugin.ActionInfo `json:"action"`
		Context plugin.Context    `json:"context"`
		Log     plugin.CommandLog `json:"log"`
	}
	apiOK(t, dir, "plugin.action.invoke", map[string]any{"action_id": "socket.test.context"}, &invoked)
	if invoked.Type != "plugin_action_invoked" || invoked.Action.ActionID != "socket.test.context" || invoked.Context["focused_pane_id"] != "p1" || invoked.Context["workspace_id"] != "s1" || invoked.Context["tab_id"] != "s1:t1" {
		t.Fatal(invoked)
	}
	apiOK(t, dir, api.MethodWorkspaceCreate, api.WorkspaceCreateParams{}, nil)
	deadline := time.Now().Add(10 * time.Second)
	for {
		var logs struct {
			Logs []plugin.CommandLog `json:"logs"`
		}
		apiOK(t, dir, "plugin.log.list", map[string]any{"plugin_id": "socket.test"}, &logs)
		complete := 0
		for _, l := range logs.Logs {
			if l.Status == "succeeded" {
				complete++
				if l.Event == "workspace.created" && (l.Stdout == nil || *l.Stdout != "workspace.created") {
					t.Fatal(l)
				}
				if l.ActionID == "context" {
					var ctx plugin.Context
					if l.Stdout == nil || json.Unmarshal([]byte(*l.Stdout), &ctx) != nil || ctx["focused_pane_id"] != "p1" {
						t.Fatal(l)
					}
				}
			}
		}
		if complete == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("logs not completed", logs)
		}
		time.Sleep(10 * time.Millisecond)
	}
	apiOK(t, dir, "plugin.disable", map[string]any{"plugin_id": "socket.test"}, nil)
	rejected := apiReq(t, dir, "plugin.action.invoke", map[string]any{"action_id": "socket.test.context"})
	if rejected.Err == nil || rejected.Err.Code != "plugin_disabled" {
		t.Fatal(rejected)
	}
}

func TestOverlaySocketCommandAndPaneAddressing(t *testing.T) {
	teststate.Lock(t)
	dir, done := startPluginServer(t)
	defer stopServer(t, dir, done)
	var before api.PaneListResult
	apiOK(t, dir, "pane.list", map[string]any{}, &before)
	var opened api.CommandExecutedResult
	apiOK(t, dir, "pane.command.execute", map[string]any{"pane_id": "p1", "action": "overlay", "command": "/bin/cat"}, &opened)
	if opened.Pane == nil || opened.Pane.PaneID == "p1" || opened.Pane.WorkspaceID != "s1" || opened.Pane.TabID != "s1:t1" || !opened.Pane.Focused {
		t.Fatal(opened)
	}
	refused := apiReq(t, dir, "pane.zoom", map[string]any{"pane_id": opened.Pane.PaneID, "mode": "on"})
	if refused.Err == nil || refused.Err.Code != "unsupported" {
		t.Fatal(refused)
	}
	var during api.PaneListResult
	apiOK(t, dir, "pane.list", map[string]any{}, &during)
	if len(during.Panes) != len(before.Panes)+1 {
		t.Fatal(before, during)
	}
	apiOK(t, dir, "pane.send_text", map[string]any{"pane_id": opened.Pane.PaneID, "text": "socket-overlay-proof\n"}, nil)
	apiOK(t, dir, "pane.close", map[string]any{"pane_id": opened.Pane.PaneID}, nil)
	var after api.PaneListResult
	apiOK(t, dir, "pane.list", map[string]any{}, &after)
	if len(after.Panes) != len(before.Panes) || after.Panes[0].PaneID != "p1" || !after.Panes[0].Focused {
		t.Fatal(after)
	}
}
