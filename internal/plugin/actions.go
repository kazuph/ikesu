package plugin

import (
	"encoding/json"
	"fmt"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/childenv"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type ActionInfo struct {
	PluginID    string   `json:"plugin_id"`
	ActionID    string   `json:"action_id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Command     []string `json:"command"`
	Contexts    []string `json:"contexts"`
	Platforms   []string `json:"platforms,omitempty"`
}
type Context map[string]any

type CommandLog struct {
	LogID          string   `json:"log_id"`
	PluginID       string   `json:"plugin_id"`
	ActionID       string   `json:"action_id,omitempty"`
	Event          string   `json:"event,omitempty"`
	Command        []string `json:"command"`
	Status         string   `json:"status"`
	StartedUnixMS  int64    `json:"started_unix_ms"`
	FinishedUnixMS *int64   `json:"finished_unix_ms,omitempty"`
	ExitCode       *int     `json:"exit_code,omitempty"`
	Stdout         *string  `json:"stdout,omitempty"`
	Stderr         *string  `json:"stderr,omitempty"`
	Error          *string  `json:"error,omitempty"`
}

// Engine owns running out-of-process commands; the Store owns shared data.
// Wait allows the server to finish recording logs before shutdown.
type Engine struct {
	Store               *Store
	SocketPath, BinPath string
	wg                  sync.WaitGroup
}

func effectivePlatforms(item, parent []string) []string {
	if item != nil {
		return item
	}
	return parent
}
func supported(platforms []string) bool {
	if platforms == nil {
		return true
	}
	osname := runtime.GOOS
	if osname == "darwin" {
		osname = "macos"
	}
	for _, p := range platforms {
		if p == osname {
			return true
		}
	}
	return false
}
func (s *Store) Actions(id string) ([]ActionInfo, error) {
	plugins, err := s.List(id)
	if err != nil {
		return nil, err
	}
	out := []ActionInfo{}
	for _, p := range plugins {
		for _, a := range p.Actions {
			contexts := a.Contexts
			if contexts == nil {
				contexts = []string{}
			}
			out = append(out, ActionInfo{PluginID: p.ID, ActionID: p.ID + "." + a.ID, Title: a.Title, Description: a.Description, Command: a.Command, Contexts: contexts, Platforms: effectivePlatforms(a.Platforms, p.Platforms)})
		}
	}
	return out, nil
}
func (s *Store) ResolveAction(pluginID, actionID string) (Installed, Action, ActionInfo, error) {
	plugins, err := s.List(pluginID)
	if err != nil {
		return Installed{}, Action{}, ActionInfo{}, err
	}
	var owner Installed
	var action Action
	found := false
	for _, p := range plugins {
		for _, a := range p.Actions {
			if a.ID == actionID || p.ID+"."+a.ID == actionID {
				if found {
					return owner, action, ActionInfo{}, &api.Error{Code: "ambiguous_target", Message: fmt.Sprintf("ambiguous action %q", actionID)}
				}
				owner = p
				action = a
				found = true
			}
		}
	}
	if !found {
		return owner, action, ActionInfo{}, &api.Error{Code: "plugin_action_not_found", Message: actionID}
	}
	if !owner.Enabled {
		return owner, action, ActionInfo{}, &api.Error{Code: "plugin_disabled", Message: owner.ID}
	}
	// Never execute the cached command of a missing or now-invalid manifest.
	m, _, err := ReadManifest(owner.ManifestPath, s.Version)
	if err != nil {
		return owner, action, ActionInfo{}, err
	}
	owner.Manifest = m
	platforms := effectivePlatforms(action.Platforms, owner.Platforms)
	if !supported(platforms) {
		return owner, action, ActionInfo{}, &api.Error{Code: "platform_unsupported", Message: owner.ID}
	}
	contexts := action.Contexts
	if contexts == nil {
		contexts = []string{}
	}
	info := ActionInfo{PluginID: owner.ID, ActionID: owner.ID + "." + action.ID, Title: action.Title, Description: action.Description, Command: action.Command, Contexts: contexts, Platforms: platforms}
	return owner, action, info, nil
}
func (e *Engine) Environment(p Installed, context Context, extra map[string]string) ([]string, error) {
	config, err := e.Store.ConfigDir(p.ID)
	if err != nil {
		return nil, err
	}
	state, err := e.Store.StateDir(p.ID)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(context)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	// A pane's inherited plugin context must not leak into nested invocations.
	for _, kv := range childenv.Scrub(os.Environ()) {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(k, "GTX_PLUGIN_") || k == "GTX_PANE_ID" || k == "GTX_TAB_ID" || k == "GTX_WORKSPACE_ID" {
			continue
		}
		values[k] = v
	}
	for k, v := range extra {
		values[k] = v
	}
	for k, v := range map[string]string{"GTX_SOCKET_PATH": e.SocketPath, "GTX_BIN_PATH": stableBinPath(e.BinPath), "GTX_ENV": "1", "GTX_PLUGIN_ID": p.ID, "GTX_PLUGIN_ROOT": p.Root, "GTX_PLUGIN_CONFIG_DIR": config, "GTX_PLUGIN_STATE_DIR": state, "GTX_PLUGIN_CONTEXT_JSON": string(raw)} {
		values[k] = v
	}
	for field, key := range map[string]string{"workspace_id": "GTX_WORKSPACE_ID", "tab_id": "GTX_TAB_ID", "focused_pane_id": "GTX_PANE_ID", "clicked_url": "GTX_PLUGIN_CLICKED_URL", "link_handler_id": "GTX_PLUGIN_LINK_HANDLER_ID"} {
		if v, ok := context[field].(string); ok && v != "" {
			values[key] = v
		}
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, k := range keys {
		env = append(env, k+"="+values[k])
	}
	return env, nil
}
func (e *Engine) Invoke(pluginID, actionID string, context Context) (ActionInfo, CommandLog, error) {
	owner, a, info, err := e.Store.ResolveAction(pluginID, actionID)
	if err != nil {
		return info, CommandLog{}, err
	}
	log, err := e.start(owner, a.Command, a.ID, "", context, map[string]string{"GTX_PLUGIN_ACTION_ID": a.ID})
	return info, log, err
}
func (e *Engine) start(p Installed, argv []string, action, event string, context Context, extra map[string]string) (CommandLog, error) {
	env, err := e.Environment(p, context, extra)
	if err != nil {
		return CommandLog{}, err
	}
	dir, err := e.Store.StateDir(p.ID)
	if err != nil {
		return CommandLog{}, err
	}
	file, err := os.CreateTemp(dir, "command-*.json")
	if err != nil {
		return CommandLog{}, err
	}
	path := file.Name()
	file.Close()
	now := time.Now().UnixMilli()
	log := CommandLog{LogID: strings.TrimSuffix(filepath.Base(path), ".json"), PluginID: p.ID, ActionID: action, Event: event, Command: argv, Status: "running", StartedUnixMS: now}
	write := func(record CommandLog) error {
		data, err := json.Marshal(record)
		if err != nil {
			return err
		}
		tmp, err := os.CreateTemp(dir, ".log-*")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		if _, err = tmp.Write(data); err == nil {
			err = tmp.Sync()
		}
		ce := tmp.Close()
		if err != nil {
			return err
		}
		if ce != nil {
			return ce
		}
		return os.Rename(tmp.Name(), path)
	}
	if err = write(log); err != nil {
		os.Remove(path)
		return log, err
	}
	cmd := childenv.Command(argv[0], argv[1:]...)
	cmd.Dir = p.Root
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		finished := time.Now().UnixMilli()
		msg := err.Error()
		log.Status = "failed"
		log.FinishedUnixMS = &finished
		log.Error = &msg
		if writeErr := write(log); writeErr != nil {
			return log, writeErr
		}
		return log, err
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		err := cmd.Wait()
		finished := time.Now().UnixMilli()
		code := cmd.ProcessState.ExitCode()
		out, errout := stdout.String(), stderr.String()
		record := log
		record.Status = "succeeded"
		if err != nil {
			record.Status = "failed"
			message := err.Error()
			record.Error = &message
		}
		record.FinishedUnixMS = &finished
		record.ExitCode = &code
		record.Stdout = &out
		record.Stderr = &errout
		if err := write(record); err != nil {
			fmt.Fprintf(os.Stderr, "gtx plugin log %s: %v\n", record.LogID, err)
		}
	}()
	return log, nil
}
func (e *Engine) Wait() { e.wg.Wait() }
func (s *Store) Logs(pluginID string, limit *uint) ([]CommandLog, error) {
	plugins, err := s.List(pluginID)
	if err != nil {
		return nil, err
	}
	logs := []CommandLog{}
	for _, p := range plugins {
		paths, err := filepath.Glob(filepath.Join(s.StateRoot, "plugins", p.ID, "command-*.json"))
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			var log CommandLog
			if err = json.Unmarshal(data, &log); err != nil {
				return nil, err
			}
			logs = append(logs, log)
		}
	}
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].StartedUnixMS == logs[j].StartedUnixMS {
			return logs[i].LogID > logs[j].LogID
		}
		return logs[i].StartedUnixMS > logs[j].StartedUnixMS
	})
	if limit != nil && uint(len(logs)) > *limit {
		logs = logs[:*limit]
	}
	return logs, nil
}
func (e *Engine) Event(name string, data json.RawMessage, context Context) error {
	plugins, err := e.Store.List("")
	if err != nil {
		return err
	}
	for _, p := range plugins {
		if !p.Enabled {
			continue
		}
		m, _, err := ReadManifest(p.ManifestPath, e.Store.Version)
		if err != nil {
			return err
		}
		p.Manifest = m
		for _, hook := range p.Events {
			if hook.On != name || !supported(effectivePlatforms(hook.Platforms, p.Platforms)) {
				continue
			}
			if _, err = e.start(p, hook.Command, "", name, context, map[string]string{"GTX_PLUGIN_EVENT": name, "GTX_PLUGIN_EVENT_JSON": string(data)}); err != nil {
				return err
			}
		}
	}
	return nil
}
