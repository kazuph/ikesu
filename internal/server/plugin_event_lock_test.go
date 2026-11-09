package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestPluginEventDoesNotWaitForRegistryLock(t *testing.T) {
	teststate.Lock(t)
	dir, done := startPluginServer(t)
	defer stopServer(t, dir, done)
	root := t.TempDir()
	manifest := "id=\"event.lock\"\nname=\"Event Lock\"\nversion=\"0.1.0\"\nmin_ikesu_version=\"0.1.0\"\n[[events]]\non=\"workspace.created\"\ncommand=[\"/usr/bin/true\"]\n"
	if err := os.WriteFile(filepath.Join(root, "ikesu-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	apiOK(t, dir, "plugin.link", map[string]any{"path": root}, nil)
	ready := time.NewTimer(5 * time.Second)
	defer ready.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		apiOK(t, dir, api.MethodWorkspaceCreate, api.WorkspaceCreateParams{}, nil)
		var result struct {
			Logs []json.RawMessage `json:"logs"`
		}
		apiOK(t, dir, "plugin.log.list", map[string]any{"plugin_id": "event.lock"}, &result)
		if len(result.Logs) > 0 {
			break
		}
		select {
		case <-tick.C:
		case <-ready.C:
			t.Fatal("plugin event registration did not become active")
		}
	}
	f, err := os.OpenFile(filepath.Join(filepath.Dir(config.Path()), ".plugins.lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	held := true
	defer func() {
		if held {
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		}
	}()
	conn, err := net.Dial("unix", api.APISocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	err = api.WriteRequest(conn, api.Request{ID: "workspace-create", Method: api.MethodWorkspaceCreate, Params: api.MarshalParams(api.WorkspaceCreateParams{})})
	if err != nil {
		t.Fatal(err)
	}
	_, err = bufio.NewReader(conn).ReadBytes('\n')
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	held = false
	if err != nil {
		t.Fatalf("workspace API blocked on plugin registry lock: %v", err)
	}
}

func TestPluginRegistryLockDoesNotBlockOtherAPI(t *testing.T) {
	teststate.Lock(t)
	dir, done := startPluginServer(t)
	defer stopServer(t, dir, done)
	root := t.TempDir()
	manifest := "id=\"event.registry\"\nname=\"Registry\"\nversion=\"0.1.0\"\nmin_ikesu_version=\"0.1.0\"\n"
	if err := os.WriteFile(filepath.Join(root, "ikesu-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	apiOK(t, dir, "plugin.link", map[string]any{"path": root}, nil)
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(config.Path()), ".plugins.lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	held := true
	defer func() {
		if held {
			_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		}
	}()
	reg, err := net.Dial("unix", api.APISocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	if err = api.WriteRequest(reg, api.Request{ID: "disable", Method: "plugin.disable", Params: api.MarshalParams(map[string]any{"plugin_id": "event.registry"})}); err != nil {
		t.Fatal(err)
	}
	ready := time.NewTimer(5 * time.Second)
	defer ready.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var b bytes.Buffer
		_ = pprof.Lookup("goroutine").WriteTo(&b, 2)
		found := false
		for _, g := range strings.Split(b.String(), "\n\n") {
			if strings.Contains(g, "RegistryRequest") && strings.Contains(g, "Flock") {
				found = true
				break
			}
		}
		if found {
			break
		}
		select {
		case <-tick.C:
		case <-ready.C:
			t.Fatal("registry request did not reach the held lock")
		}
	}
	other, err := net.Dial("unix", api.APISocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	_ = other.SetDeadline(time.Now().Add(2 * time.Second))
	err = api.WriteRequest(other, api.Request{ID: "other", Method: api.MethodWorkspaceList, Params: api.MarshalParams(nil)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = bufio.NewReader(other).ReadBytes('\n')
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	held = false
	if err != nil {
		t.Fatalf("other API blocked behind registry lock: %v", err)
	}
}
