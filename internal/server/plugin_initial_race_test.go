package server

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/plugin"
)

func TestPluginInitialRegistryChangeDuringManifestRead(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config")
	dir := filepath.Join(root, "plugin")
	for _, p := range []string{config, dir} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	fifo := filepath.Join(dir, "ikesu-plugin.toml")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	item := plugin.Installed{Manifest: plugin.Manifest{ID: "event.external"}, ManifestPath: fifo, Root: dir, Enabled: true}
	data, err := json.Marshal([]plugin.Installed{item})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(config, "plugins.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	manifest := "id=\"event.external\"\nname=\"External\"\nversion=\"0.1.0\"\nmin_ikesu_version=\"0.1.0\"\n[[events]]\non=\"workspace.created\"\ncommand=[\"/usr/bin/true\"]\n"
	store := &plugin.Store{ConfigRoot: config, StateRoot: filepath.Join(root, "state"), Version: "0.1.0"}
	s := &Server{plugins: &plugin.Engine{Store: store}}
	initialized := make(chan struct{})
	go func() { s.initPlugins(); close(initialized) }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	var fd int
	for {
		fd, err = syscall.Open(fifo, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("manifest reader did not start")
		}
	}
	writer := os.NewFile(uintptr(fd), fifo)
	c := exec.Command(os.Args[0], "-test.run=^TestPluginExternalWriterHelper$")
	c.Env = append(os.Environ(), "PLUGIN_TEST_ROOT="+root, "PLUGIN_TEST_OP=empty-registry")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("external edit: %v %s", err, out)
	}
	if _, err = writer.WriteString(manifest); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	for {
		select {
		case <-initialized:
			defer s.stopPluginWatcher()
			s.pluginEventsMu.RLock()
			n := len(s.pluginEvents)
			s.pluginEventsMu.RUnlock()
			if n != 0 {
				t.Fatalf("stale registrations published: %d", n)
			}
			return
		case <-ticker.C:
			next, e := syscall.Open(fifo, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
			if e == nil {
				f := os.NewFile(uintptr(next), fifo)
				_, _ = f.WriteString(manifest)
				_ = f.Close()
			}
		case <-deadline.C:
			t.Fatal("initial read did not complete")
		}
	}
}

func TestPluginInitialManifestChangeDuringRead(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config")
	dir := filepath.Join(root, "plugin")
	for _, p := range []string{config, dir} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	fifo := filepath.Join(dir, "ikesu-plugin.toml")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	item := plugin.Installed{Manifest: plugin.Manifest{ID: "event.external"}, ManifestPath: fifo, Root: dir, Enabled: true}
	data, err := json.Marshal([]plugin.Installed{item})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(config, "plugins.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	manifest := func(value string) string {
		return "id=\"event.external\"\nname=\"External\"\nversion=\"0.1.0\"\nmin_ikesu_version=\"0.1.0\"\n[[events]]\non=\"workspace.created\"\ncommand=[\"/bin/echo\",\"" + value + "\"]\n"
	}
	store := &plugin.Store{ConfigRoot: config, StateRoot: filepath.Join(root, "state"), Version: "0.1.0"}
	s := &Server{plugins: &plugin.Engine{Store: store}}
	initialized := make(chan struct{})
	go func() { s.initPlugins(); close(initialized) }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	replaced := false
	for {
		select {
		case <-initialized:
			defer s.stopPluginWatcher()
			if !replaced {
				t.Fatal("manifest was not replaced during the read")
			}
			s.pluginEventsMu.RLock()
			got := ""
			if len(s.pluginEvents) > 0 {
				got = s.pluginEvents[0].Events[0].Command[1]
			}
			s.pluginEventsMu.RUnlock()
			for got != "new" {
				select {
				case <-ticker.C:
				case <-deadline.C:
					t.Fatalf("manifest snapshot=%q want new", got)
				}
				s.pluginEventsMu.RLock()
				if len(s.pluginEvents) > 0 {
					got = s.pluginEvents[0].Events[0].Command[1]
				}
				s.pluginEventsMu.RUnlock()
			}
			return
		case <-ticker.C:
			if replaced {
				continue
			}
			fd, e := syscall.Open(fifo, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
			if e != nil {
				continue
			}
			f := os.NewFile(uintptr(fd), fifo)
			var stacks bytes.Buffer
			_ = pprof.Lookup("goroutine").WriteTo(&stacks, 2)
			listRead := false
			for _, g := range strings.Split(stacks.String(), "\n\n") {
				if strings.Contains(g, "ReadManifest") && strings.Contains(g, "(*Store).List") {
					listRead = true
				}
			}
			if !listRead && !replaced {
				c := exec.Command(os.Args[0], "-test.run=^TestPluginExternalWriterHelper$")
				c.Env = append(os.Environ(), "PLUGIN_TEST_ROOT="+root, "PLUGIN_TEST_OP=replace-manifest", "PLUGIN_TEST_MANIFEST="+manifest("new"))
				if out, err := c.CombinedOutput(); err != nil {
					_ = f.Close()
					t.Fatalf("external edit: %v %s", err, out)
				}
				replaced = true
			}
			_, _ = f.WriteString(manifest("old"))
			_ = f.Close()
		case <-deadline.C:
			t.Fatal("initial manifest read did not complete")
		}
	}
}
