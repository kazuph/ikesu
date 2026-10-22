package server

import (
	"encoding/json"
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/plugin"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) ensurePlugins() *plugin.Engine {
	if s.plugins == nil {
		bin, _ := os.Executable()
		s.plugins = &plugin.Engine{Store: plugin.DefaultStore(s.version), SocketPath: api.APISocketPath(s.dir), BinPath: bin, ConfigPath: s.configPath}
	}
	return s.plugins
}
func (s *Server) refreshPluginHooks() {
	engine := s.ensurePlugins()
	items, err := engine.Store.List("")
	s.pluginEvents = false
	if err != nil {
		fmt.Fprintln(os.Stderr, "ikesu plugin registry:", err)
		return
	}
	for _, item := range items {
		if item.Enabled && len(item.Events) > 0 {
			s.pluginEvents = true
			break
		}
	}
}
func (s *Server) initPlugins() {
	store := plugin.DefaultStore(s.version)
	if _, err := os.Stat(filepath.Join(store.ConfigRoot, "plugins.json")); err == nil {
		s.refreshPluginHooks()
	}
}
func (s *Server) pluginContext(requestID string, supplied plugin.Context) plugin.Context {
	context := plugin.Context{}
	st := s.app.State()
	ws := st.ActiveWorkspace()
	if ws != nil {
		context["workspace_id"] = fmtWorkspaceID(ws.ID)
		context["workspace_label"] = ws.Name
		context["workspace_cwd"] = ws.Cwd
		if ws.Worktree != nil {
			context["worktree"] = ws.Worktree
		}
		if tab := ws.TabRef(); tab != nil {
			context["tab_id"] = fmtTabID(ws.ID, tab.ID)
			context["tab_label"] = tab.Name
		}
		if pane, ok := s.paneInfo(st.FocusedPane()); ok {
			context["focused_pane_id"] = pane.PaneID
			context["focused_pane_status"] = pane.AgentStatus
			if pane.Agent != nil {
				context["focused_pane_agent"] = *pane.Agent
			}
			if pane.Cwd != nil {
				context["focused_pane_cwd"] = *pane.Cwd
			}
		}
	}
	context["correlation_id"] = requestID
	context["invocation_source"] = "api"
	for k, v := range supplied {
		if v != nil {
			context[k] = v
		}
	}
	return context
}
func (s *Server) pluginRequest(req *api.Request) api.Response {
	if strings.HasPrefix(req.Method, "plugin.pane.") {
		return s.pluginPaneRequest(req)
	}
	engine := s.ensurePlugins()
	if resp, ok := engine.Store.RegistryRequest(req); ok {
		if resp.Err == nil && req.Method != "plugin.list" {
			s.refreshPluginHooks()
		}
		return resp
	}
	var p struct {
		PluginID string         `json:"plugin_id"`
		ActionID string         `json:"action_id"`
		Context  plugin.Context `json:"context"`
		Limit    *uint          `json:"limit"`
	}
	if err := req.DecodeParams(&p); err != nil {
		return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
	}
	fail := func(err error) api.Response {
		code := "plugin_error"
		if e, ok := err.(*api.Error); ok {
			code = e.Code
		}
		return api.Failure(req.ID, code, err.Error())
	}
	switch req.Method {
	case "plugin.action.list":
		actions, err := engine.Store.Actions(p.PluginID)
		if err != nil {
			return fail(err)
		}
		return api.Success(req.ID, map[string]any{"type": "plugin_action_list", "actions": actions})
	case "plugin.action.invoke":
		if p.ActionID == "" {
			return api.Failure(req.ID, api.ErrInvalidRequest, "action_id is required")
		}
		context := s.pluginContext(req.ID, p.Context)
		action, log, err := engine.Invoke(p.PluginID, p.ActionID, context)
		if err != nil {
			return fail(err)
		}
		return api.Success(req.ID, map[string]any{"type": "plugin_action_invoked", "action": action, "context": context, "log": log})
	case "plugin.log.list":
		logs, err := engine.Store.Logs(p.PluginID, p.Limit)
		if err != nil {
			return fail(err)
		}
		return api.Success(req.ID, map[string]any{"type": "plugin_log_list", "logs": logs})
	default:
		return api.Failure(req.ID, "unsupported", "unsupported plugin method: "+req.Method)
	}
}
func (s *Server) pluginEvent(kind string, raw json.RawMessage) {
	if !s.pluginEvents {
		return
	}
	// Manifest hooks use dotted event names; socket envelopes use underscores.
	name := strings.ReplaceAll(kind, "_", ".")
	for dotted := range subscriptionKinds {
		if dottedToKind(dotted) == kind {
			name = dotted
			break
		}
	}
	context := s.pluginContext("", plugin.Context{"invocation_source": "event"})
	if err := s.plugins.Event(name, raw, context); err != nil {
		fmt.Fprintln(os.Stderr, "ikesu plugin event:", err)
	}
}
