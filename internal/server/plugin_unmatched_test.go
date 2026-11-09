package server

import (
	"bytes"
	"encoding/json"
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

func TestUnmatchedPluginEventsDoNotCreateWaitingGoroutines(t *testing.T) {
	teststate.Lock(t)
	dir, done := startPluginServer(t)
	defer stopServer(t, dir, done)
	apiOK(t, dir, api.MethodWorkspaceCreate, api.WorkspaceCreateParams{}, nil)
	root := t.TempDir()
	manifest := "id=\"event.unmatched\"\nname=\"Unmatched\"\nversion=\"0.1.0\"\nmin_ikesu_version=\"0.1.0\"\n[[events]]\non=\"workspace.created\"\ncommand=[\"/usr/bin/true\"]\n"
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
		apiOK(t, dir, "plugin.log.list", map[string]any{"plugin_id": "event.unmatched"}, &result)
		if len(result.Logs) > 0 {
			break
		}
		select {
		case <-tick.C:
		case <-ready.C:
			t.Fatal("hook not active")
		}
	}
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
	maxWaiting := 0
	for n := 0; n < 20; n++ {
		workspace := "s1"
		if n%2 == 1 {
			workspace = "s2"
		}
		apiOK(t, dir, api.MethodWorkspaceFocus, api.WorkspaceTarget{WorkspaceID: workspace}, nil)
		var b bytes.Buffer
		_ = pprof.Lookup("goroutine").WriteTo(&b, 2)
		count := strings.Count(b.String(), "github.com/kazuph/ikesu/internal/server.(*Server).pluginEvent.func1")
		if count > maxWaiting {
			maxWaiting = count
		}
	}
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	held = false
	if maxWaiting != 0 {
		t.Fatalf("unmatched events created %d waiting dispatch goroutines", maxWaiting)
	}
}
