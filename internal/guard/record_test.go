package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordJSONAndMarkerIdentity(t *testing.T) {
	d := t.TempDir()
	a := NewAssets(d, "p_1", "/usr/bin/true", Config{Mode: "record"})
	env, e := a.Prepare([]string{"HOME=" + d, "PATH=/usr/bin:/bin", "GTX_PANE_ID=p1", "GTX_SOCKET_PATH=" + d + "/socket", "GTX_WORKSPACE_ID=s1", "GTX_TAB_ID=s1:t1"}, d)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	i := Input{Command: "git", Args: []string{"push", "--force"}, Cwd: d, Env: EnvMap(env)}
	if _, active, e := ReadMarker(i); e == nil || !active {
		t.Fatal(active, e)
	}
	if e = a.Sync("codex"); e != nil {
		t.Fatal(e)
	}
	m, active, e := ReadMarker(i)
	if e != nil || !active {
		t.Fatal(active, e)
	}
	i.LaunchCwd = m.Cwd
	if e = Log(d, i, m, Evaluate(Config{}, i), "shim"); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(d, "guard", "decisions.jsonl"))
	if e != nil {
		t.Fatal(e)
	}
	var r Record
	if e = json.Unmarshal(b, &r); e != nil {
		t.Fatal(e)
	}
	if r.Epoch == 0 || r.Timestamp == "" || r.Decision != "block" || r.Rule != "git-push-force" || r.CommandLine != "git push --force" || r.Cwd != d || r.LaunchCwd != d || r.Pane != "p1" || r.Workspace != "s1" || r.Tab != "s1:t1" || r.Agent != "codex" || r.Via != "shim" {
		t.Fatal(r)
	}
	i.Env["GTX_PANE_ID"] = "p2"
	if _, active, e = ReadMarker(i); e == nil || !active {
		t.Fatal(active, e)
	}
}

func TestResolveSelfShimAndEmptyPATHEntry(t *testing.T) {
	d := t.TempDir()
	shim := filepath.Join(d, "shim")
	real := filepath.Join(d, "real")
	os.Mkdir(shim, 0700)
	os.Mkdir(real, 0700)
	for _, p := range []string{filepath.Join(shim, "tool"), filepath.Join(real, "tool")} {
		os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0700)
	}
	got, e := Resolve("tool", []string{"PATH=" + shim + ":" + real, "GTX_GUARD_BIN=" + shim})
	if e != nil || got != filepath.Join(real, "tool") {
		t.Fatal(got, e)
	}
	// An unset guard directory must not remove the shell's relative PATH entry.
	exe, e := Resolve("git", []string{"PATH=/usr/bin:/bin"})
	if e != nil || exe == "" {
		t.Fatal(exe, e)
	}
}
