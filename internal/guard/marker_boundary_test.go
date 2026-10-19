package guard

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkerFailuresAlwaysDenyAndRecord(t *testing.T) {
	for _, mode := range []string{"record", "enforce"} {
		for _, failure := range []string{"missing", "unreadable", "malformed", "identity"} {
			t.Run(mode+"/"+failure, func(t *testing.T) {
				root := t.TempDir()
				a := NewAssets(root, "p_1", "/usr/bin/true", Config{Mode: mode})
				env, e := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "GTX_PANE_ID=p1", "GTX_SOCKET_PATH=" + root + "/socket"}, root)
				if e != nil {
					t.Fatal(e)
				}
				if e = a.Sync("codex"); e != nil {
					t.Fatal(e)
				}
				switch failure {
				case "missing":
					e = os.Remove(a.MarkerPath)
				case "unreadable":
					e = os.Remove(a.MarkerPath)
					if e == nil {
						e = os.Mkdir(a.MarkerPath, 0700)
					}
				case "malformed":
					e = os.WriteFile(a.MarkerPath, []byte("{"), 0600)
				case "identity":
					m := a.meta
					m.Pane = "p2"
					b, _ := json.Marshal(m)
					e = os.WriteFile(a.MarkerPath, b, 0600)
				}
				if e != nil {
					t.Fatal(e)
				}
				i := Input{Command: "git", Args: []string{"status"}, Cwd: root, Env: EnvMap(env)}
				for _, line := range []bool{false, true} {
					var out bytes.Buffer
					allowed := false
					if line {
						allowed = AuthorizeLine(a.Config, root, i, "git status", &out, nil)
					} else {
						allowed = Authorize(a.Config, root, i, "shim", &out, nil)
					}
					if allowed || !strings.Contains(out.String(), "停止") {
						t.Fatal(allowed, out.String())
					}
				}
				b, e := os.ReadFile(filepath.Join(root, "guard", "decisions.jsonl"))
				if e != nil {
					t.Fatal(e)
				}
				records := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
				if len(records) != 2 {
					t.Fatal(string(b))
				}
				for _, row := range records {
					var r Record
					if e = json.Unmarshal(row, &r); e != nil || r.Decision != "block" || r.Rule != "guard-marker" || r.Reason == "" {
						t.Fatal(string(row), e)
					}
				}
			})
		}
	}
}

func TestMarkerSocketIndependentOfShellEnvironment(t *testing.T) {
	root := t.TempDir()
	a := NewAssets(root, "p_1", "/usr/bin/true", Config{})
	env, e := a.Prepare([]string{"HOME=" + root, "PATH=/bin", "GTX_PANE_ID=p1", "GTX_SOCKET_PATH=" + root + "/old.sock"}, root)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.Sync("codex"); e != nil {
		t.Fatal(e)
	}
	i := Input{Env: EnvMap(env)}
	for _, socket := range []string{"", "/missing.sock", root + "/other.sock"} {
		i.Env["GTX_SOCKET_PATH"] = socket
		m, active, e := ReadMarker(i)
		if e != nil || !active || m.Socket != root+"/old.sock" {
			t.Fatal(m, active, e)
		}
	}
	imported := Import(a.Transfer())
	if e = imported.RebindSocket(root + "/new.sock"); e != nil {
		t.Fatal(e)
	}
	m, active, e := ReadMarker(i)
	if e != nil || !active || m.Socket != root+"/new.sock" {
		t.Fatal(m, active, e)
	}
	if e = imported.Sync("codex"); e != nil {
		t.Fatal(e)
	}
	m, _, e = ReadMarker(i)
	if e != nil || m.Socket != root+"/new.sock" {
		t.Fatal(m, e)
	}
}

func TestNoMarkerOrdinaryPaneStillAllowed(t *testing.T) {
	var out bytes.Buffer
	if !Authorize(Config{}, t.TempDir(), Input{Command: "rm", Args: []string{"outside"}, Env: map[string]string{}}, "shim", &out, nil) {
		t.Fatal(out.String())
	}
}
