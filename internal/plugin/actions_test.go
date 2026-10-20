package plugin

import (
	"encoding/json"
	"github.com/kazuph/ikesu/internal/api"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func actionEngine(t *testing.T, text string) (*Engine, Installed) {
	t.Helper()
	root := t.TempDir()
	s := &Store{ConfigRoot: filepath.Join(root, "config"), StateRoot: filepath.Join(root, "state"), Version: "0.1.0"}
	p, err := s.Link(writeManifest(t, text), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &Engine{Store: s, SocketPath: filepath.Join(root, "socket"), BinPath: "/test/gtx"}, p
}
func TestActionRealExecutionAndEnvironment(t *testing.T) {
	e, p := actionEngine(t, minimal+`[[actions]]
id="run"
title="Run"
command=["/bin/sh", "-c", "printf '%s\\n' \"$PWD\" \"$GTX_BIN_PATH\" \"$GTX_PLUGIN_ID\" \"$GTX_PLUGIN_ACTION_ID\" \"$GTX_WORKSPACE_ID\" \"$GTX_TAB_ID\" \"$GTX_PANE_ID\" \"$GTX_PLUGIN_CONTEXT_JSON\"; printf 'saved' > \"$GTX_PLUGIN_STATE_DIR/state\"; printf 'config' > \"$GTX_PLUGIN_CONFIG_DIR/config\""]
`)
	context := Context{"workspace_id": "s1", "tab_id": "s1:t1", "focused_pane_id": "p1", "invocation_source": "cli"}
	info, started, err := e.Invoke("", p.ID+".run", context)
	if err != nil {
		t.Fatal(err)
	}
	if info.ActionID != p.ID+".run" || started.Status != "running" {
		t.Fatal(info, started)
	}
	e.Wait()
	logs, err := e.Store.Logs(p.ID, nil)
	if err != nil || len(logs) != 1 {
		t.Fatal(logs, err)
	}
	log := logs[0]
	if log.Status != "succeeded" || log.ExitCode == nil || *log.ExitCode != 0 || log.Stdout == nil {
		t.Fatal(log)
	}
	for _, want := range []string{p.Root, "/test/gtx", p.ID, "run", "s1", "s1:t1", "p1", "\"invocation_source\":\"cli\""} {
		if !strings.Contains(*log.Stdout, want) {
			t.Fatalf("missing %s in %q", want, *log.Stdout)
		}
	}
	for _, path := range []string{filepath.Join(e.Store.StateRoot, "plugins", p.ID, "state"), filepath.Join(e.Store.ConfigRoot, "plugins", "config", p.ID, "config")} {
		if _, err = os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	second := &Store{ConfigRoot: e.Store.ConfigRoot, StateRoot: e.Store.StateRoot, Version: e.Store.Version}
	logs, err = second.Logs(p.ID, nil)
	if err != nil || len(logs) != 1 || logs[0].Status != "succeeded" {
		t.Fatal(logs, err)
	}
}
func TestActionFailureAndDisabled(t *testing.T) {
	e, p := actionEngine(t, minimal+`[[actions]]
id="fail"
title="Fail"
command=["/bin/sh", "-c", "printf failure >&2; exit 7"]
`)
	if _, _, err := e.Invoke(p.ID, "fail", Context{}); err != nil {
		t.Fatal(err)
	}
	e.Wait()
	logs, err := e.Store.Logs(p.ID, nil)
	if err != nil || len(logs) != 1 || logs[0].Status != "failed" || logs[0].ExitCode == nil || *logs[0].ExitCode != 7 || logs[0].Stderr == nil || *logs[0].Stderr != "failure" {
		t.Fatal(logs, err)
	}
	if _, err = e.Store.SetEnabled(p.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err = e.Invoke(p.ID, "fail", Context{}); err == nil || err.(*api.Error).Code != "plugin_disabled" {
		t.Fatal(err)
	}
	logs, _ = e.Store.Logs(p.ID, nil)
	if len(logs) != 1 {
		t.Fatal("disabled action executed", logs)
	}
}
func TestActionInheritanceAndAmbiguity(t *testing.T) {
	e, p := actionEngine(t, minimal+`platforms=["macos","linux"]
[[actions]]
id="run"
title="Run"
command=["/bin/echo","ok"]
[[actions]]
id="no"
title="No"
command=["/bin/echo","forbidden"]
platforms=[]
`)
	actions, err := e.Store.Actions(p.ID)
	if err != nil || len(actions) != 2 || len(actions[0].Platforms) != 2 {
		t.Fatal(actions, err)
	}
	if _, _, err = e.Invoke(p.ID, "no", Context{}); err == nil || err.(*api.Error).Code != "platform_unsupported" {
		t.Fatal(err)
	}
	if _, err = e.Store.Link(writeManifest(t, strings.ReplaceAll(minimal, "example.local", "another")+`[[actions]]
id="run"
title="Run"
command=["/bin/echo","ok"]
`), true, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err = e.Invoke("", "run", Context{}); err == nil || err.(*api.Error).Code != "ambiguous_target" {
		t.Fatal(err)
	}
	if _, _, err = e.Invoke("", p.ID+".run", Context{}); err != nil {
		t.Fatal(err)
	}
	e.Wait()
}
func TestEventHooksAndStartupNeverRuns(t *testing.T) {
	e, p := actionEngine(t, minimal+`[[events]]
on="worktree.created"
command=["/bin/sh","-c","printf '%s\\n' \"$GTX_PLUGIN_EVENT\" \"$GTX_PLUGIN_EVENT_JSON\""]
[[startup]]
command=["/bin/sh","-c","touch startup-ran"]
`)
	data := json.RawMessage(`{"workspace_id":"s2"}`)
	if err := e.Event("pane.created", data, Context{}); err != nil {
		t.Fatal(err)
	}
	logs, err := e.Store.Logs(p.ID, nil)
	if err != nil || len(logs) != 0 {
		t.Fatal(logs, err)
	}
	if err = e.Event("worktree.created", data, Context{}); err != nil {
		t.Fatal(err)
	}
	e.Wait()
	logs, err = e.Store.Logs(p.ID, nil)
	if err != nil || len(logs) != 1 || logs[0].Event != "worktree.created" || logs[0].Stdout == nil || !strings.Contains(*logs[0].Stdout, "s2") {
		t.Fatal(logs, err)
	}
	if _, err = os.Stat(filepath.Join(p.Root, "startup-ran")); !os.IsNotExist(err) {
		t.Fatal("startup hook ran", err)
	}
	if _, err = e.Store.SetEnabled(p.ID, false); err != nil {
		t.Fatal(err)
	}
	if err = e.Event("worktree.created", data, Context{}); err != nil {
		t.Fatal(err)
	}
	e.Wait()
	logs, err = e.Store.Logs(p.ID, nil)
	if err != nil || len(logs) != 1 {
		t.Fatal(logs, err)
	}
}
func TestMissingManifestCannotExecuteCachedAction(t *testing.T) {
	e, p := actionEngine(t, minimal+`[[actions]]
id="run"
title="Run"
command=["/bin/echo","cached"]
`)
	if err := os.Remove(p.ManifestPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.Invoke(p.ID, "run", Context{}); err == nil {
		t.Fatal("executed cached command")
	}
}
