package server

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/teststate"
	"reflect"
	"testing"
)

func TestAgentViewAPIIsDisplayOnlyAndSourceOwned(t *testing.T) {
	teststate.Lock(t)
	dir, done := startPluginServer(t)
	defer stopServer(t, dir, done)
	apiOK(t, dir, "pane.report_agent", map[string]any{"pane_id": "p1", "source": "custom:view-test", "agent": "codex", "state": "blocked"}, nil)
	var before api.AgentListResult
	apiOK(t, dir, "agent.list", map[string]any{}, &before)
	var view struct {
		Type   string  `json:"type"`
		Active bool    `json:"active"`
		Source *string `json:"source"`
		Label  *string `json:"label"`
	}
	apiOK(t, dir, "agent.view.set", map[string]any{"source": "plugin.test", "label": "Blocked", "filter": map[string]any{"op": "eq", "field": "status", "value": "idle"}}, &view)
	if view.Type != "agent_view" || !view.Active || view.Source == nil || *view.Source != "plugin.test" {
		t.Fatal(view)
	}
	var after api.AgentListResult
	apiOK(t, dir, "agent.list", map[string]any{}, &after)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("view changed agent API", before, after)
	}
	apiOK(t, dir, "agent.view.clear", map[string]any{"source": "other"}, &view)
	if !view.Active {
		t.Fatal("foreign source cleared view")
	}
	apiOK(t, dir, "agent.view.clear", map[string]any{"source": "plugin.test"}, &view)
	if view.Active {
		t.Fatal(view)
	}
	rejected := apiReq(t, dir, "agent.view.set", map[string]any{"source": "plugin.test", "filter": map[string]any{"op": "eq", "field": "cwd", "value": "/tmp"}})
	if rejected.Err == nil || rejected.Err.Code != api.ErrInvalidRequest {
		t.Fatal(rejected)
	}
}
