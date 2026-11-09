package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/plugin"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginStopAccountsForAcceptedSocketEvents(t *testing.T) {
	teststate.Lock(t)
	dir, done := startPluginServer(t)
	root := t.TempDir()
	manifest := "id=\"event.account\"\nname=\"Account\"\nversion=\"0.1.0\"\nmin_ikesu_version=\"0.1.0\"\n[[events]]\non=\"workspace.created\"\ncommand=[\"/usr/bin/true\"]\n"
	if err := os.WriteFile(filepath.Join(root, "ikesu-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	apiOK(t, dir, "plugin.link", map[string]any{"path": root}, nil)
	const accepted = 20
	for n := 0; n < accepted; n++ {
		apiOK(t, dir, api.MethodWorkspaceCreate, api.WorkspaceCreateParams{}, nil)
	}
	stopServer(t, dir, done)
	logs, err := plugin.DefaultStore("0.1.0").Logs("event.account", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != accepted {
		t.Fatalf("accepted=%d logged=%d: %+v", accepted, len(logs), logs)
	}
	for _, log := range logs {
		if log.Status == "running" && log.ServerStop == nil {
			t.Errorf("running event without stop record: %+v", log)
		}
		if log.Status == "discarded" && log.Error == nil {
			t.Errorf("discarded event without reason: %+v", log)
		}
	}
}
