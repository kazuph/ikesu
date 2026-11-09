package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/plugin"
)

func TestPluginExternalWriterHelper(t *testing.T) {
	root := os.Getenv("PLUGIN_TEST_ROOT")
	if root == "" {
		return
	}
	store := &plugin.Store{ConfigRoot: filepath.Join(root, "config"), StateRoot: filepath.Join(root, "state"), Version: "0.1.0"}
	manifest := filepath.Join(root, "plugin", "ikesu-plugin.toml")
	switch os.Getenv("PLUGIN_TEST_OP") {
	case "link":
		if _, err := store.Link(filepath.Dir(manifest), true, nil); err != nil {
			t.Fatal(err)
		}
	case "edit":
		if err := os.WriteFile(manifest, []byte(os.Getenv("PLUGIN_TEST_MANIFEST")), 0600); err != nil {
			t.Fatal(err)
		}
	case "unlink":
		if _, err := store.Unlink("event.external"); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("missing operation")
	}
}

func TestPluginExternalChangesUpdateEventSnapshot(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugin")
	if err := os.MkdirAll(pluginDir, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := func(command string) string {
		return "id=\"event.external\"\nname=\"External\"\nversion=\"0.1.0\"\nmin_ikesu_version=\"0.1.0\"\n[[events]]\non=\"workspace.created\"\ncommand=[\"/bin/echo\",\"" + command + "\"]\n"
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "ikesu-plugin.toml"), []byte(manifest("first")), 0600); err != nil {
		t.Fatal(err)
	}
	store := &plugin.Store{ConfigRoot: filepath.Join(root, "config"), StateRoot: filepath.Join(root, "state"), Version: "0.1.0"}
	s := &Server{plugins: &plugin.Engine{Store: store}}
	s.initPlugins()
	defer s.stopPluginWatcher()
	change := func(op, text string) {
		t.Helper()
		c := exec.Command(os.Args[0], "-test.run=^TestPluginExternalWriterHelper$")
		c.Env = append(os.Environ(), "PLUGIN_TEST_ROOT="+root, "PLUGIN_TEST_OP="+op, "PLUGIN_TEST_MANIFEST="+text)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("external %s: %v %s", op, err, out)
		}
	}
	wait := func(want string) {
		t.Helper()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			s.pluginEventsMu.RLock()
			got := ""
			if len(s.pluginEvents) > 0 {
				got = s.pluginEvents[0].Events[0].Command[1]
			}
			s.pluginEventsMu.RUnlock()
			if got == want {
				return
			}
			select {
			case <-ticker.C:
			case <-timer.C:
				t.Fatalf("snapshot = %q, want %q", got, want)
			}
		}
	}
	run := func(want string, count int) {
		t.Helper()
		s.pluginEventsMu.RLock()
		items := s.pluginEvents
		s.pluginEventsMu.RUnlock()
		if err := s.plugins.EventInstalled(items, "workspace.created", json.RawMessage(`{}`), plugin.Context{}); err != nil {
			t.Fatal(err)
		}
		s.plugins.Wait()
		logs, err := store.Logs("event.external", nil)
		if err != nil {
			t.Fatal(err)
		}
		if want != "" {
			found := false
			for _, log := range logs {
				if log.Stdout != nil && strings.TrimSpace(*log.Stdout) == want {
					found = true
				}
			}
			if len(logs) != count || !found {
				t.Fatalf("logs=%+v, want %q count %d", logs, want, count)
			}
		}
	}
	change("link", "")
	wait("first")
	run("first", 1)
	change("edit", manifest("second"))
	wait("second")
	run("second", 2)
	change("unlink", "")
	wait("")
	run("", 0)
}
