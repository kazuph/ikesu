package distribution

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/plugin"
)

func TestMacStatusSeparatesRegistrationRunningAndUnknownOwner(t *testing.T) {
	node := fixtureNode(t)
	home := t.TempDir()
	store := &plugin.Store{ConfigRoot: filepath.Join(home, "config"), StateRoot: filepath.Join(home, "state"), Version: "0.4.1"}
	i := &Installer{Product: MacDialog, Home: home, Store: store}
	running := filepath.Join(i.Directory(), "0.1.0")
	registered := filepath.Join(i.Directory(), "0.2.0")
	for _, root := range []string{running, registered} {
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte(filepath.Base(root)+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `id="kazuph.mac-dialog-decisions"
name="Mac Decisions"
version="0.2.0"
min_ikesu_version="0.1.0"
platforms=["macos"]
`
	if err := os.WriteFile(filepath.Join(registered, "ikesu-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Link(registered, true, nil); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(running, "daemon.mjs")
	if err := os.WriteFile(script, []byte("setInterval(()=>{}, 1000);\n"), 0600); err != nil {
		t.Fatal(err)
	}
	process := fixtureCommand(t, fixtureEnvironment(home), node, script, "run")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { process.Process.Kill(); process.Wait() })
	directory, err := store.StateDir(MacDialog.PluginID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	actual, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, value any) {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	owner := map[string]any{"pid": process.Process.Pid, "owner": "one", "node": node, "script": script, "root": running, "version": "0.1.0", "stateDir": actual}
	write("daemon.json", owner)
	write("daemon-state.json", map[string]any{"pid": process.Process.Pid, "owner": "one", "root": running, "version": "0.1.0", "all_machines": true})
	status, err := i.Status()
	if err != nil || status.RegisteredVersion != "0.2.0" || status.RunningVersion != "0.1.0" || !status.SwitchPending || status.RunningUnknown {
		t.Fatal(status, err)
	}
	delete(owner, "node")
	write("daemon.json", owner)
	status, err = i.Status()
	if err != nil || !status.RunningUnknown || !status.SwitchPending || status.RunningVersion != "" {
		t.Fatal(status, err)
	}
	owner["node"] = node
	owner["stateDir"] = home
	write("daemon.json", owner)
	status, err = i.Status()
	if err != nil || !status.RunningUnknown || status.RunningVersion != "" {
		t.Fatal("unproven owner reported as running", status, err)
	}
}
