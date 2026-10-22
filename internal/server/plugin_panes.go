package server

import (
	"encoding/json"
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/model"
	"strings"
)

type pluginPaneOwner struct {
	PluginID   string
	Entrypoint string
}

func (s *Server) pluginPaneRequest(req *api.Request) api.Response {
	fail := func(err error) api.Response {
		code := "plugin_error"
		if e, ok := err.(*api.Error); ok {
			code = e.Code
		}
		if err.Error() == "ui_busy" {
			code = "ui_busy"
		}
		return api.Failure(req.ID, code, err.Error())
	}
	if req.Method != "plugin.pane.open" {
		var p struct {
			PaneID string `json:"pane_id"`
		}
		if err := req.DecodeParams(&p); err != nil {
			return fail(err)
		}
		pid, _, _, ok := s.findPane(p.PaneID)
		owner, owned := s.pluginPanes[pid]
		if !ok || !owned {
			return api.Failure(req.ID, "plugin_pane_not_found", p.PaneID)
		}
		switch req.Method {
		case "plugin.pane.focus":
			wi, ti := s.app.State().FindPane(pid)
			s.app.ExecCmd(model.CmdSelectTab{Workspace: wi, Tab: ti})
			s.app.ExecCmd(model.CmdFocusPane{Pane: pid})
			pane, _ := s.paneInfo(pid)
			return api.Success(req.ID, map[string]any{"type": "plugin_pane_focused", "plugin_pane": map[string]any{"plugin_id": owner.PluginID, "entrypoint": owner.Entrypoint, "pane": pane}})
		case "plugin.pane.close":
			response := s.paneClose(req.ID, p.PaneID)
			if response.Err != nil {
				return response
			}
			delete(s.pluginPanes, pid)
			return api.Success(req.ID, map[string]any{"type": "plugin_pane_closed", "pane_id": p.PaneID})
		default:
			return api.Failure(req.ID, api.ErrUnknownMethod, req.Method)
		}
	}
	var p struct {
		PluginID     string            `json:"plugin_id"`
		Entrypoint   string            `json:"entrypoint"`
		Placement    string            `json:"placement"`
		Direction    string            `json:"direction"`
		Cwd          string            `json:"cwd"`
		WorkspaceID  string            `json:"workspace_id"`
		TargetPaneID string            `json:"target_pane_id"`
		Focus        bool              `json:"focus"`
		Env          map[string]string `json:"env"`
		Width        *api.PopupSize    `json:"width"`
		Height       *api.PopupSize    `json:"height"`
	}
	if err := req.DecodeParams(&p); err != nil {
		return fail(err)
	}
	if p.PluginID == "" || p.Entrypoint == "" {
		return api.Failure(req.ID, api.ErrInvalidRequest, "plugin_id and entrypoint are required")
	}
	engine := s.ensurePlugins()
	owner, entry, err := engine.Store.ResolvePane(p.PluginID, p.Entrypoint)
	if err != nil {
		return fail(err)
	}
	if p.Placement == "" {
		p.Placement = entry.Placement
	}
	st := s.app.State()
	wi := st.Active
	target := st.FocusedPane()
	if p.WorkspaceID != "" {
		wi = s.findWorkspace(p.WorkspaceID)
		if wi < 0 {
			return s.wsNotFound(req.ID, p.WorkspaceID)
		}
		target = st.Workspaces[wi].ActiveLayout().Focused()
	}
	if p.TargetPaneID != "" {
		pid, w, _, ok := s.findPane(p.TargetPaneID)
		if !ok {
			return s.paneNotFound(req.ID, p.TargetPaneID)
		}
		if p.WorkspaceID != "" && wi != w {
			return api.Failure(req.ID, api.ErrInvalidRequest, "target pane is outside requested workspace")
		}
		wi, target = w, pid
	}
	if p.Cwd == "" {
		p.Cwd = owner.Root
	}
	ctx := s.pluginContext(req.ID, nil)
	rawEnv, err := engine.Environment(owner, ctx, p.Env)
	if err != nil {
		return fail(err)
	}
	env := map[string]string{}
	for _, kv := range rawEnv {
		k, v, _ := strings.Cut(kv, "=")
		if k == "IKESU_PANE_ID" || p.Placement != "popup" && (k == "IKESU_WORKSPACE_ID" || k == "IKESU_TAB_ID") {
			continue
		}
		env[k] = v
	}
	env["IKESU_PLUGIN_ENTRYPOINT_ID"] = entry.ID
	if p.Placement == "popup" {
		for _, size := range []struct {
			source any
			target **api.PopupSize
		}{{entry.Width, &p.Width}, {entry.Height, &p.Height}} {
			if *size.target == nil && size.source != nil {
				raw, _ := json.Marshal(size.source)
				var parsed api.PopupSize
				if err := json.Unmarshal(raw, &parsed); err != nil {
					return fail(err)
				}
				*size.target = &parsed
			}
		}
		if wi != st.Active {
			return api.Failure(req.ID, api.ErrInvalidRequest, "popup workspace must be active")
		}
		if _, err := s.app.OpenPluginPopup(p.Cwd, entry.Command, env, p.Width, p.Height); err != nil {
			return fail(err)
		}
		return api.Success(req.ID, api.OkResult{Type: "ok"})
	}
	pid, err := s.app.OpenPluginPane(wi, target, p.Placement, p.Direction, p.Cwd, entry.Command, env, p.Focus)
	if err != nil {
		return fail(err)
	}
	if s.pluginPanes == nil {
		s.pluginPanes = map[int]pluginPaneOwner{}
	}
	s.pluginPanes[pid] = pluginPaneOwner{PluginID: owner.ID, Entrypoint: entry.ID}
	pane, ok := s.paneInfo(pid)
	if !ok {
		return fail(fmt.Errorf("created pane is unavailable"))
	}
	return api.Success(req.ID, map[string]any{"type": "plugin_pane_opened", "plugin_pane": map[string]any{"plugin_id": owner.ID, "entrypoint": entry.ID, "pane": pane}})
}
