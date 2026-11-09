package server

import (
	"encoding/json"
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/filewatch"
	"github.com/kazuph/ikesu/internal/plugin"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Server) ensurePlugins() *plugin.Engine {
	if s.plugins == nil {
		bin, _ := os.Executable()
		s.plugins = &plugin.Engine{Store: plugin.DefaultStore(s.version), SocketPath: api.APISocketPath(s.dir), BinPath: bin, ConfigPath: s.configPath}
	}
	return s.plugins
}

// reloadPluginHooks reads atomically published registry snapshots outside the app loop.
func (s *Server) reloadPluginHooks(engine *plugin.Engine) error {
	s.pluginRegistryMu.Lock()
	defer s.pluginRegistryMu.Unlock()
	return s.reloadPluginHooksLocked(engine)
}
func existingPluginWatchPaths(path string) []string {
	// Preserve the nearest existing directory and its parent through replacement.
	dir := path
	for {
		st, err := os.Stat(dir)
		if err == nil && st.IsDir() {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return []string{dir, filepath.Dir(dir), path}
}
func pluginManifestPaths(items []plugin.Installed) map[string]bool {
	paths := make(map[string]bool, len(items))
	for _, item := range items {
		paths[item.ManifestPath] = true
	}
	return paths
}
func samePluginPaths(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for path := range a {
		if !b[path] {
			return false
		}
	}
	return true
}
func samePluginFile(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime() == b.ModTime() && a.Mode() == b.Mode()
}
func (s *Server) reloadPluginHooksLocked(engine *plugin.Engine) error {
	for {
		select {
		case <-s.pluginStop:
			return fmt.Errorf("plugin registry: server stopped")
		default:
		}
		paths := existingPluginWatchPaths(engine.Store.ConfigRoot)
		paths = append(paths, filepath.Join(engine.Store.ConfigRoot, "plugins.json"))
		retained := append(append([]string(nil), paths...), s.pluginWatchPaths...)
		if !s.pluginWatcher.Update(retained) {
			return fmt.Errorf("plugin registry: filewatch unavailable")
		}
		first, err := engine.Store.Snapshot()
		if err != nil {
			return fmt.Errorf("plugin registry: %w", err)
		}
		for _, item := range first {
			paths = append(paths, existingPluginWatchPaths(filepath.Dir(item.ManifestPath))...)
			paths = append(paths, item.ManifestPath)
		}
		if !s.pluginWatcher.Update(paths) {
			return fmt.Errorf("plugin registry: filewatch unavailable")
		}
		s.pluginWatchPaths = paths
		rev := s.pluginWatcher.Revision()
		registryPath := filepath.Join(engine.Store.ConfigRoot, "plugins.json")
		registryBefore, _ := os.Stat(registryPath)
		items, err := engine.Store.Snapshot()
		if err != nil {
			return fmt.Errorf("plugin registry: %w", err)
		}
		if !samePluginPaths(pluginManifestPaths(first), pluginManifestPaths(items)) {
			continue
		}
		var hooks []plugin.Installed
		changed := false
		for _, item := range items {
			if !item.Enabled {
				continue
			}
			before, _ := os.Stat(item.ManifestPath)
			manifest, _, err := plugin.ReadManifest(item.ManifestPath, engine.Store.Version)
			after, _ := os.Stat(item.ManifestPath)
			if !samePluginFile(before, after) {
				changed = true
				break
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "ikesu plugin event:", err)
				continue
			}
			item.Manifest = manifest
			if len(item.Events) > 0 {
				hooks = append(hooks, item)
			}
		}
		registryAfter, _ := os.Stat(registryPath)
		if changed || !samePluginFile(registryBefore, registryAfter) || s.pluginWatcher.Revision() != rev {
			continue
		}
		index := make(map[string][]plugin.Installed)
		for _, item := range hooks {
			for _, hook := range item.Events {
				copy := item
				copy.Events = []plugin.Event{hook}
				index[hook.On] = append(index[hook.On], copy)
			}
		}
		s.pluginEventsMu.Lock()
		s.pluginEvents, s.pluginIndex = hooks, index
		s.pluginEventsMu.Unlock()
		// A queued notification also causes a later reread if delivery raced publication.
		return nil
	}
}
func (s *Server) requestPluginRefresh() {
	if s.pluginRefresh == nil {
		return
	}
	select {
	case s.pluginRefresh <- struct{}{}:
	default:
	}
}
func (s *Server) initPlugins() {
	engine := s.ensurePlugins()
	s.pluginRefresh = make(chan struct{}, 1)
	s.pluginStop = make(chan struct{})
	s.pluginRefreshDone = make(chan struct{})
	s.pluginWake = make(chan struct{}, 1)
	s.pluginWorkerState = make(chan struct{}, 1)
	s.pluginWorkerDone = make(chan struct{})
	go s.runPluginEvents(engine)
	s.pluginWatcher = filewatch.New(s.requestPluginRefresh)
	if err := s.reloadPluginHooks(engine); err != nil {
		fmt.Fprintln(os.Stderr, "ikesu", err)
	}
	go func() {
		defer close(s.pluginRefreshDone)
		for {
			select {
			case <-s.pluginRefresh:
				if err := s.reloadPluginHooks(engine); err != nil {
					fmt.Fprintln(os.Stderr, "ikesu", err)
				}
			case <-s.pluginStop:
				return
			}
		}
	}()
}
func (s *Server) stopPluginWatcher() {
	if s.pluginWatcher == nil {
		return
	}
	s.pluginWatcher.Close()
	close(s.pluginStop)
	<-s.pluginRefreshDone
	s.pluginQueueMu.Lock()
	pending := s.pluginQueue
	s.pluginQueue = nil
	s.pluginWorkerPaused = false
	s.pluginWorkerClosing = true
	s.pluginQueueMu.Unlock()
	s.wakePluginWorker()
	<-s.pluginWorkerDone
	reason := "server_stopped"
	if s.handedOff {
		reason = "server_handoff"
	}
	if err := s.plugins.MarkRunning(reason); err != nil {
		fmt.Fprintln(os.Stderr, "ikesu plugin log:", err)
	}
	if !s.handedOff {
		for _, work := range pending {
			if err := s.plugins.RecordDiscarded(work.Items, work.Name); err != nil {
				fmt.Fprintln(os.Stderr, "ikesu plugin log:", err)
			}
		}
	}
}
func (s *Server) pausePluginWorker() error {
	timer := time.NewTimer(handoffWait)
	defer timer.Stop()
	s.pluginQueueMu.Lock()
	s.pluginWorkerPaused = true
	for s.pluginWorkerBusy {
		s.pluginQueueMu.Unlock()
		select {
		case <-s.pluginWorkerState:
		case <-timer.C:
			s.resumePluginWorker()
			return fmt.Errorf("timed out waiting for plugin dispatch")
		}
		s.pluginQueueMu.Lock()
	}
	s.pluginQueueMu.Unlock()
	return nil
}
func (s *Server) resumePluginWorker() {
	s.pluginQueueMu.Lock()
	s.pluginWorkerPaused = false
	s.pluginQueueMu.Unlock()
	s.wakePluginWorker()
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
func (s *Server) pluginRegistryRequest(req *api.Request) api.Response {
	s.pluginOperationsMu.Lock()
	defer s.pluginOperationsMu.Unlock()
	resp, _ := s.plugins.Store.RegistryRequest(req)
	if resp.Err == nil && req.Method != "plugin.list" {
		if err := s.reloadPluginHooks(s.plugins); err != nil {
			return api.Failure(req.ID, "plugin_error", err.Error())
		}
	}
	return resp
}
func (s *Server) pluginRequest(req *api.Request) api.Response {
	if strings.HasPrefix(req.Method, "plugin.pane.") {
		return s.pluginPaneRequest(req)
	}
	engine := s.ensurePlugins()

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

type pluginEventWork struct {
	Items   []plugin.Installed
	Name    string
	Raw     json.RawMessage
	Context json.RawMessage
}

func (s *Server) wakePluginWorker() {
	select {
	case s.pluginWake <- struct{}{}:
	default:
	}
}
func (s *Server) pluginEvent(kind string, raw json.RawMessage) {
	name := strings.ReplaceAll(kind, "_", ".")
	for dotted := range subscriptionKinds {
		if dottedToKind(dotted) == kind {
			name = dotted
			break
		}
	}
	s.pluginEventsMu.RLock()
	items := s.pluginIndex[name]
	s.pluginEventsMu.RUnlock()
	if len(items) == 0 {
		return
	}
	context, err := json.Marshal(s.pluginContext("", plugin.Context{"invocation_source": "event"}))
	if err != nil {
		fmt.Fprintln(os.Stderr, "ikesu plugin context:", err)
		return
	}
	s.pluginQueueMu.Lock()
	s.pluginQueue = append(s.pluginQueue, pluginEventWork{Items: items, Name: name, Raw: raw, Context: context})
	s.pluginQueueMu.Unlock()
	s.wakePluginWorker()
}
func (s *Server) runPluginEvents(engine *plugin.Engine) {
	defer close(s.pluginWorkerDone)
	for {
		s.pluginQueueMu.Lock()
		if s.pluginWorkerClosing && len(s.pluginQueue) == 0 {
			s.pluginQueueMu.Unlock()
			return
		}
		if s.pluginWorkerPaused || len(s.pluginQueue) == 0 {
			s.pluginQueueMu.Unlock()
			<-s.pluginWake
			continue
		}
		work := s.pluginQueue[0]
		s.pluginQueue[0] = pluginEventWork{}
		s.pluginQueue = s.pluginQueue[1:]
		s.pluginWorkerBusy = true
		s.pluginQueueMu.Unlock()
		var context plugin.Context
		if err := json.Unmarshal(work.Context, &context); err != nil {
			fmt.Fprintln(os.Stderr, "ikesu plugin context:", err)
		} else if err := engine.EventInstalled(work.Items, work.Name, work.Raw, context); err != nil {
			fmt.Fprintln(os.Stderr, "ikesu plugin event:", err)
		}
		s.pluginQueueMu.Lock()
		s.pluginWorkerBusy = false
		s.pluginQueueMu.Unlock()
		select {
		case s.pluginWorkerState <- struct{}{}:
		default:
		}
	}
}
