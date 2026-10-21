package cli

import (
	"bytes"
	"encoding/json"
	"github.com/kazuph/ikesu/internal/api"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginOfflineCLIAndSessionSharing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("GTX_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("GTX_CONFIG", "")
	path := filepath.Join(root, "plugin")
	if e := os.Mkdir(path, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(path, "gtx-plugin.toml"), []byte("id='local.example'\nname='Example'\nversion='0.1.0'\nmin_gtx_version='0.1.0'\n"), 0600); e != nil {
		t.Fatal(e)
	}
	run := func(session string, args ...string) api.Response {
		t.Helper()
		var out, err bytes.Buffer
		if code := Run(args, Options{Session: session, Stdout: &out, Stderr: &err}); code != 0 {
			t.Fatalf("%v exit=%d %s", args, code, err.String())
		}
		var r api.Response
		if e := json.Unmarshal(out.Bytes(), &r); e != nil {
			t.Fatal(out.String(), e)
		}
		return r
	}
	run("one", "plugin", "link", path, "--disabled")
	list := run("two", "plugin", "list", "--json")
	var body struct {
		Plugins []struct {
			Enabled bool `json:"enabled"`
		} `json:"plugins"`
	}
	if e := json.Unmarshal(list.Result, &body); e != nil || len(body.Plugins) != 1 || body.Plugins[0].Enabled {
		t.Fatal(string(list.Result), e)
	}
	run("two", "plugin", "enable", "local.example")
	list = run("one", "plugin", "list", "--json")
	if e := json.Unmarshal(list.Result, &body); e != nil || !body.Plugins[0].Enabled {
		t.Fatal(string(list.Result), e)
	}
	run("one", "plugin", "unlink", "local.example")
	if _, e := os.Stat(path); e != nil {
		t.Fatal(e)
	}
}
