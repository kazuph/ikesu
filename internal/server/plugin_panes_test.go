package server

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginPanePlacementsAndOwnership(t *testing.T) {
	teststate.Lock(t)
	dir, done := startPluginServer(t)
	defer stopServer(t, dir, done)
	root := t.TempDir()
	manifest := `id="pane.test"
name="Panes"
version="0.1.0"
min_gtx_version="0.1.0"
platforms=["macos","linux"]
[[panes]]
id="board"
title="Board"
command=["/bin/cat"]
`
	if err := os.WriteFile(filepath.Join(root, "gtx-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	apiOK(t, dir, "plugin.link", map[string]any{"path": root}, nil)
	for _, placement := range []string{"overlay", "split", "tab", "zoomed"} {
		var opened struct {
			Type       string `json:"type"`
			PluginPane struct {
				PluginID   string       `json:"plugin_id"`
				Entrypoint string       `json:"entrypoint"`
				Pane       api.PaneInfo `json:"pane"`
			} `json:"plugin_pane"`
		}
		apiOK(t, dir, "plugin.pane.open", map[string]any{"plugin_id": "pane.test", "entrypoint": "board", "placement": placement, "focus": true}, &opened)
		if opened.Type != "plugin_pane_opened" || opened.PluginPane.PluginID != "pane.test" || opened.PluginPane.Entrypoint != "board" || opened.PluginPane.Pane.PaneID == "" {
			t.Fatal(opened)
		}
		id := opened.PluginPane.Pane.PaneID
		apiOK(t, dir, "plugin.pane.focus", map[string]any{"pane_id": id}, nil)
		apiOK(t, dir, "pane.send_text", map[string]any{"pane_id": id, "text": "pane-proof\n"}, nil)
		apiOK(t, dir, "plugin.pane.close", map[string]any{"pane_id": id}, nil)
		resp := apiReq(t, dir, "plugin.pane.focus", map[string]any{"pane_id": id})
		if resp.Err == nil || resp.Err.Code != "plugin_pane_not_found" {
			t.Fatal(resp)
		}
	}
	for i := 0; i < 2; i++ {
		if i == 1 {
			apiOK(t, dir, "workspace.create", api.WorkspaceCreateParams{Focus: true}, nil)
		}
		var result api.OkResult
		apiOK(t, dir, "plugin.pane.open", map[string]any{"plugin_id": "pane.test", "entrypoint": "board", "placement": "popup", "width": 40, "height": "50%"}, &result)
		if result.Type != "ok" {
			t.Fatal(result)
		}
	}
	apiOK(t, dir, "popup.close", map[string]any{}, nil)
	apiOK(t, dir, "workspace.focus", map[string]any{"workspace_id": "s1"}, nil)
	apiOK(t, dir, "popup.close", map[string]any{}, nil)
	var moved struct {
		PluginPane struct {
			Pane api.PaneInfo `json:"pane"`
		} `json:"plugin_pane"`
	}
	apiOK(t, dir, "plugin.pane.open", map[string]any{"plugin_id": "pane.test", "entrypoint": "board", "placement": "split", "focus": false}, &moved)
	pid := moved.PluginPane.Pane.PaneID
	apiOK(t, dir, "pane.move", map[string]any{"pane_id": pid, "destination": map[string]any{"type": "new_tab", "workspace_id": "s2"}, "focus": false}, nil)
	var focused struct {
		PluginPane struct {
			PluginID string       `json:"plugin_id"`
			Pane     api.PaneInfo `json:"pane"`
		} `json:"plugin_pane"`
	}
	apiOK(t, dir, "plugin.pane.focus", map[string]any{"pane_id": pid}, &focused)
	if focused.PluginPane.PluginID != "pane.test" || focused.PluginPane.Pane.PaneID != pid || focused.PluginPane.Pane.WorkspaceID != "s2" {
		t.Fatal(focused)
	}
	apiOK(t, dir, "plugin.pane.close", map[string]any{"pane_id": pid}, nil)
	apiOK(t, dir, "plugin.disable", map[string]any{"plugin_id": "pane.test"}, nil)
	rejected := apiReq(t, dir, "plugin.pane.open", map[string]any{"plugin_id": "pane.test", "entrypoint": "board"})
	if rejected.Err == nil || rejected.Err.Code != "plugin_disabled" {
		t.Fatal(rejected)
	}
}
