package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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
	case "remove-dir":
		if err := os.RemoveAll(filepath.Dir(manifest)); err != nil {
			t.Fatal(err)
		}
	case "recreate-dir":
		if err := os.MkdirAll(filepath.Dir(manifest), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manifest, []byte(os.Getenv("PLUGIN_TEST_MANIFEST")), 0600); err != nil {
			t.Fatal(err)
		}
	case "hold-lock":
		f, err := os.OpenFile(filepath.Join(store.ConfigRoot, ".plugins.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
			t.Fatal(err)
		}
		fmt.Println("locked")
		_, _ = bufio.NewReader(os.Stdin).ReadByte()
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	case "replace-manifest":
		tmp := manifest + ".next"
		if err := os.WriteFile(tmp, []byte(os.Getenv("PLUGIN_TEST_MANIFEST")), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, manifest); err != nil {
			t.Fatal(err)
		}
	case "empty-registry":
		tmp := filepath.Join(store.ConfigRoot, "plugins.next")
		if err := os.WriteFile(tmp, []byte("[]"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, filepath.Join(store.ConfigRoot, "plugins.json")); err != nil {
			t.Fatal(err)
		}
	case "repair-registry":
		if err := os.WriteFile(filepath.Join(store.ConfigRoot, "plugins.json"), []byte("[]"), 0600); err != nil {
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
	change("remove-dir", "")
	wait("")
	change("recreate-dir", manifest("third"))
	wait("third")
	run("third", 3)
	change("unlink", "")
	wait("")
	run("", 0)
}

func TestPluginWatcherRecoversBrokenRegistry(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	pluginDir := filepath.Join(root, "plugin")
	for _, p := range []string{configDir, pluginDir} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(configDir, "plugins.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := "id=\"event.external\"\nname=\"External\"\nversion=\"0.1.0\"\nmin_ikesu_version=\"0.1.0\"\n[[events]]\non=\"workspace.created\"\ncommand=[\"/bin/echo\",\"fixed\"]\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "ikesu-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	store := &plugin.Store{ConfigRoot: configDir, StateRoot: filepath.Join(root, "state"), Version: "0.1.0"}
	s := &Server{plugins: &plugin.Engine{Store: store}}
	s.initPlugins()
	defer s.stopPluginWatcher()
	for _, op := range []string{"repair-registry", "link"} {
		c := exec.Command(os.Args[0], "-test.run=^TestPluginExternalWriterHelper$")
		c.Env = append(os.Environ(), "PLUGIN_TEST_ROOT="+root, "PLUGIN_TEST_OP="+op)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("external %s: %v %s", op, err, out)
		}
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		s.pluginEventsMu.RLock()
		ready := len(s.pluginEvents) == 1 && s.pluginEvents[0].Events[0].Command[1] == "fixed"
		s.pluginEventsMu.RUnlock()
		if ready {
			return
		}
		select {
		case <-tick.C:
		case <-timer.C:
			t.Fatal("registry correction did not refresh hooks")
		}
	}
}

func TestPluginWatcherStopsWhileExternalRegistryLocked(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	store := &plugin.Store{ConfigRoot: configDir, StateRoot: filepath.Join(root, "state"), Version: "0.1.0"}
	s := &Server{plugins: &plugin.Engine{Store: store}}
	s.initPlugins()
	c := exec.Command(os.Args[0], "-test.run=^TestPluginExternalWriterHelper$")
	c.Env = append(os.Environ(), "PLUGIN_TEST_ROOT="+root, "PLUGIN_TEST_OP=hold-lock")
	in, err := c.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close(); _ = c.Wait() }()
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("lock readiness %q: %v", line, err)
	}
	s.requestPluginRefresh()
	done := make(chan struct{})
	go func() { s.stopPluginWatcher(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not stop while lock held")
	}
	_, _ = in.Write([]byte("x"))
	_ = in.Close()
	select {
	case <-s.pluginRefreshDone:
	default:
		t.Fatal("refresh worker remains after stop")
	}
}
