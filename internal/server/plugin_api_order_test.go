package server

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestPluginRegistrySuccessPrecedesNextEvent(t *testing.T) {
	teststate.Lock(t)
	dir, done := startPluginServer(t)
	defer stopServer(t, dir, done)
	root := t.TempDir()
	output := filepath.Join(root, "events")
	manifest := fmt.Sprintf("id=\"event.order\"\nname=\"Order\"\nversion=\"0.1.0\"\nmin_ikesu_version=\"0.1.0\"\n[[events]]\non=\"workspace.created\"\ncommand=[\"/bin/sh\",\"-c\",\"printf x >> %s\"]\n", output)
	if err := os.WriteFile(filepath.Join(root, "ikesu-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	event := func() { apiOK(t, dir, api.MethodWorkspaceCreate, api.WorkspaceCreateParams{}, nil) }
	wait := func(want string) {
		t.Helper()
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			b, _ := os.ReadFile(output)
			if string(b) == want {
				return
			}
			if len(b) > len(want) {
				t.Fatalf("events=%q want=%q", b, want)
			}
			select {
			case <-tick.C:
			case <-deadline.C:
				t.Fatalf("events=%q want=%q", b, want)
			}
		}
	}
	apiOK(t, dir, "plugin.link", map[string]any{"path": root}, nil)
	event()
	wait("x")
	apiOK(t, dir, "plugin.disable", map[string]any{"plugin_id": "event.order"}, nil)
	event()
	apiOK(t, dir, "plugin.enable", map[string]any{"plugin_id": "event.order"}, nil)
	event()
	wait("xx")
	apiOK(t, dir, "plugin.unlink", map[string]any{"plugin_id": "event.order"}, nil)
	event()
	apiOK(t, dir, "plugin.link", map[string]any{"path": root}, nil)
	event()
	wait("xxx")
}
