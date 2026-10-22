package guard

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestServerPolicyCannotBeDowngraded(t *testing.T) {
	disabled := false
	for _, missing := range []bool{false, true} {
		for _, caller := range []Config{{Enabled: &disabled}, {Mode: "record"}} {
			root := t.TempDir()
			a := NewAssets(root, "p_1", "/usr/bin/true", Config{})
			env, e := a.Prepare([]string{"HOME=" + root, "PATH=/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + root + "/socket"}, root)
			if e != nil {
				t.Fatal(e)
			}
			if e = a.Sync("codex"); e != nil {
				t.Fatal(e)
			}
			if missing {
				if e = os.Remove(a.MarkerPath); e != nil {
					t.Fatal(e)
				}
			}
			i := Input{Command: "rm", Args: []string{root + "/../victim"}, Cwd: root, Env: EnvMap(env)}
			for _, line := range []bool{false, true} {
				var out bytes.Buffer
				var allowed bool
				if line {
					allowed = AuthorizeLine(caller, root, i, i.Line(), &out, nil)
				} else {
					allowed = Authorize(caller, root, i, "shim", &out, nil)
				}
				if allowed {
					t.Fatalf("caller=%+v missing=%v line=%v: %s", caller, missing, line, out.String())
				}
			}
		}
	}
}

func TestServerPolicyReloadPublishesNewRules(t *testing.T) {
	root := t.TempDir()
	a := NewAssets(root, "p_1", "/usr/bin/true", Config{})
	env, e := a.Prepare([]string{"HOME=" + root, "PATH=/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + root + "/socket"}, root)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.Sync("codex"); e != nil {
		t.Fatal(e)
	}
	i := Input{Cwd: root, Env: EnvMap(env)}
	var out bytes.Buffer
	if !AuthorizeLine(Config{}, root, i, "printf newword", &out, nil) {
		t.Fatal(out.String())
	}
	if e = a.Reconfigure(Config{DangerWords: []string{"newword"}}); e != nil {
		t.Fatal(e)
	}
	if AuthorizeLine(Config{Mode: "record"}, root, i, "printf newword", &out, nil) {
		t.Fatal("server rule reload ignored")
	}
	// Reloading record is reported as uncovered by the existing mode contract;
	// a fresh record pane still records without requiring an approval.
	record := NewAssets(t.TempDir(), "p_2", "/usr/bin/true", Config{Mode: "record"})
	env, e = record.Prepare([]string{"HOME=" + root, "PATH=/bin", "IKESU_PANE_ID=p2", "IKESU_SOCKET_PATH=" + root + "/socket"}, root)
	if e != nil {
		t.Fatal(e)
	}
	if e = record.Sync("codex"); e != nil {
		t.Fatal(e)
	}
	i.Command, i.Args, i.Env = "rm", []string{root + "/../victim"}, EnvMap(env)
	if !Authorize(Config{}, record.Root, i, "shim", &out, nil) {
		t.Fatal(out.String())
	}
}

func TestStoredPolicyNotExposedInMarkerOutput(t *testing.T) {
	policy := Config{Mode: "enforce"}
	b, e := json.Marshal(Marker{Pane: "p1", Policy: &policy})
	if e != nil || strings.Contains(string(b), "guard_config") {
		t.Fatal(string(b), e)
	}
}

func TestChildEnvCannotDropAuthorizationIdentity(t *testing.T) {
	root := t.TempDir()
	a := NewAssets(root, "p_1", "/usr/bin/true", Config{})
	env, e := a.Prepare([]string{"HOME=" + root, "PATH=/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + root + "/socket"}, root)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.Sync("codex"); e != nil {
		t.Fatal(e)
	}
	for _, prefix := range []string{"env -i ", "env -u IKESU_GUARD_MARKER ", "env -u IKESU_PANE_ID "} {
		var out bytes.Buffer
		if AuthorizeLine(Config{}, root, Input{Cwd: root, Env: EnvMap(env)}, prefix+"/bin/rm "+root+"/../victim", &out, nil) {
			t.Fatal(prefix, out.String())
		}
	}
}
