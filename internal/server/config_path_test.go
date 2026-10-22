package server

import (
	"bytes"
	"encoding/json"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/cli"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/plugin"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPaneReceivesActualConfigurationPath(t *testing.T) {
	teststate.Lock(t)
	custom := filepath.Join(t.TempDir(), "server-read.toml")
	if err := os.WriteFile(custom, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "IKESU_CONFIG", custom)
	teststate.Setenv(t, "IKESU_CONFIG_PATH", "/outer-server/wrong.toml")
	dir, done := startPluginServer(t)
	defer stopServer(t, dir, done)
	client := dialClient(t, dir)
	defer client.conn.Close()
	client.send(t, clientMsg{Type: "hello", Cols: 100, Rows: 30})
	pid := firstPaneID(t, dir)
	apiOK(t, dir, api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: pid, Text: "printf 'CONFIG_READ_PATH=%s\\n' \"$IKESU_CONFIG_PATH\"", Keys: []string{"Enter"}}, nil)
	waitRead(t, dir, pid, "CONFIG_READ_PATH="+config.Path())
	var reloaded api.ConfigReloadResult
	apiOK(t, dir, api.MethodServerReloadConfig, api.EmptyParams{}, &reloaded)
	if reloaded.ConfigPath != custom || !filepath.IsAbs(reloaded.ConfigPath) {
		t.Fatal(reloaded)
	}
	var out, stderr bytes.Buffer
	code := cli.Run([]string{"server", "reload-config"}, cli.Options{Session: "test", Explicit: true, Stdout: &out, Stderr: &stderr, Getenv: func(key string) string {
		if key == "IKESU_STATE_DIR" {
			return api.StateDir()
		}
		return ""
	}})
	if code != 0 || json.Unmarshal(out.Bytes(), &reloaded) != nil || reloaded.ConfigPath != custom {
		t.Fatal(code, &out, &stderr)
	}
	root := t.TempDir()
	manifest := `id="path.test"
name="Config path"
version="0.1.0"
min_ikesu_version="0.1.0"
[[actions]]
id="read"
title="Read configuration path"
command=["/bin/sh","-c","printf '%s' \"$IKESU_CONFIG_PATH\""]
`
	if err := os.WriteFile(filepath.Join(root, "ikesu-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	apiOK(t, dir, "plugin.link", map[string]any{"path": root}, nil)
	apiOK(t, dir, "plugin.action.invoke", map[string]any{"action_id": "path.test.read"}, nil)
	deadline := time.Now().Add(10 * time.Second)
	for {
		var result struct {
			Logs []plugin.CommandLog `json:"logs"`
		}
		apiOK(t, dir, "plugin.log.list", map[string]any{"plugin_id": "path.test"}, &result)
		if len(result.Logs) == 1 && result.Logs[0].Status == "succeeded" {
			if result.Logs[0].Stdout == nil || *result.Logs[0].Stdout != reloaded.ConfigPath {
				t.Fatal(result)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("plugin action did not finish", result)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
