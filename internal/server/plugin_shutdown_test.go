package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/plugin"
)

func TestPluginQueuedEventsAreRecordedOnStop(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugin")
	if err := os.Mkdir(pluginDir, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := "id=\"event.stop\"\nname=\"Stop\"\nversion=\"0.1.0\"\nmin_ikesu_version=\"0.1.0\"\n[[events]]\non=\"workspace.created\"\ncommand=[\"/usr/bin/true\"]\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "ikesu-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	store := &plugin.Store{ConfigRoot: filepath.Join(root, "config"), StateRoot: filepath.Join(root, "state"), Version: "0.1.0"}
	item, err := store.Link(pluginDir, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{plugins: &plugin.Engine{Store: store}, pluginWorkerPaused: true}
	s.initPlugins()
	s.pluginQueueMu.Lock()
	s.pluginQueue = append(s.pluginQueue, pluginEventWork{Items: []plugin.Installed{item}, Name: "workspace.created"})
	s.pluginQueueMu.Unlock()
	s.stopPluginWatcher()
	logs, err := store.Logs(item.ID, nil)
	if err != nil || len(logs) != 1 || logs[0].Status != "discarded" || logs[0].Error == nil {
		t.Fatalf("logs=%+v error=%v", logs, err)
	}
}
