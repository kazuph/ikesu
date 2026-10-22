package guardmarker

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/guard"
)

// Exercise the actual CLI and generated shell entries, with no replacement
// authorization server. These commands require a reason before any RPC.
func TestPinnedGuardEntryPoints(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "ikesu")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/ikesu")
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
	cases := []struct{ name, code string }{
		{"plain", `rm "$1"`},
		{"marker-empty", `IKESU_GUARD_MARKER= rm "$1"`},
		{"marker-other", `IKESU_GUARD_MARKER="$HOME/other.json" rm "$1"`},
		{"marker-missing", `IKESU_GUARD_MARKER=/missing-marker rm "$1"`},
		{"socket-empty", `IKESU_SOCKET_PATH= rm "$1"`},
		{"socket-other", `IKESU_SOCKET_PATH=/missing.sock rm "$1"`},
		{"pane-other", `IKESU_PANE_ID=p2 rm "$1"`},
		{"unset-marker", `/usr/bin/env -u IKESU_GUARD_MARKER rm "$1"`},
		{"unset-pane", `/usr/bin/env -u IKESU_PANE_ID rm "$1"`},
		{"unset-socket", `/usr/bin/env -u IKESU_SOCKET_PATH rm "$1"`},
		{"export-empty", `export IKESU_GUARD_MARKER=; rm "$1"`},
		{"sh", `/bin/sh -c 'rm "$1"' nested "$1"`},
		{"bash", `/bin/bash -c 'rm "$1"' nested "$1"`},
		{"zsh", `/bin/zsh -c 'rm "$1"' nested "$1"`},
		{"command", `command rm "$1"`},
	}
	for _, shell := range []string{"/bin/bash", "/bin/zsh"} {
		for _, tc := range cases {
			t.Run(filepath.Base(shell)+"/"+tc.name, func(t *testing.T) {
				a, env, target := newEntry(t, binary)
				if e := a.Sync("codex"); e != nil {
					t.Fatal(e)
				}
				c := exec.Command(shell, "-c", tc.code, "test", target)
				c.Env = env
				b, e := c.CombinedOutput()
				if e == nil || !strings.Contains(string(b), "対象プロジェクト・必要な理由・実施内容を説明する申請文が必要") {
					t.Fatal(e, string(b))
				}
				if _, e = os.Stat(target); e != nil {
					t.Fatal("target removed", e)
				}
				records, e := os.ReadFile(filepath.Join(a.Root, "guard", "decisions.jsonl"))
				if e != nil || !strings.Contains(string(records), `"rule":"rm-paths"`) {
					t.Fatal(e, string(records))
				}
			})
		}
		for _, state := range []string{"ordinary", "missing", "malformed", "mismatch"} {
			t.Run(filepath.Base(shell)+"/"+state, func(t *testing.T) {
				a, env, target := newEntry(t, binary)
				if state != "ordinary" {
					if e := a.Sync("codex"); e != nil {
						t.Fatal(e)
					}
				}
				var err error
				switch state {
				case "missing":
					err = os.Remove(a.MarkerPath)
				case "malformed":
					err = os.WriteFile(a.MarkerPath, []byte("{"), 0600)
				case "mismatch":
					b, e := os.ReadFile(a.MarkerPath)
					if e != nil {
						t.Fatal(e)
					}
					var m guard.Marker
					if e = json.Unmarshal(b, &m); e != nil {
						t.Fatal(e)
					}
					m.Pane = "p2"
					b, e = json.Marshal(m)
					if e != nil {
						t.Fatal(e)
					}
					err = os.WriteFile(a.MarkerPath, b, 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
				c := exec.Command(shell, "-c", `IKESU_GUARD_MARKER= rm "$1"`, "test", target)
				c.Env = env
				b, e := c.CombinedOutput()
				_, fileErr := os.Stat(target)
				if state == "ordinary" {
					if e != nil || !os.IsNotExist(fileErr) {
						t.Fatal(e, fileErr, string(b))
					}
					return
				}
				if e == nil || fileErr != nil || !strings.Contains(string(b), "停止") {
					t.Fatal(e, fileErr, string(b))
				}
				records, e := os.ReadFile(filepath.Join(a.Root, "guard", "decisions.jsonl"))
				if e != nil || !strings.Contains(string(records), `"rule":"guard-marker"`) {
					t.Fatal(e, string(records))
				}
			})
		}
	}
}

func newEntry(t *testing.T, binary string) (*guard.Assets, []string, string) {
	t.Helper()
	root, err := os.MkdirTemp("", "ikesu-entry-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	home := filepath.Join(root, "home")
	if e := os.Mkdir(home, 0700); e != nil {
		t.Fatal(e)
	}
	// A '..' spelling selects the existing ask rule while all effects stay in TempDir.
	target := home + "/../victim"
	if e := os.WriteFile(target, []byte("test-only"), 0600); e != nil {
		t.Fatal(e)
	}
	a := guard.NewAssets(root, "p_1", binary, guard.Config{})
	env, e := a.Prepare([]string{"HOME=" + home, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + root + "/socket", "IKESU_CONFIG=" + root + "/missing.toml", "IKESU_STATE_DIR=" + root}, home)
	if e != nil {
		t.Fatal(e)
	}
	return a, env, target
}

func TestNoninteractiveGuardBeforeExecution(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "ikesu")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/ikesu")
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
	cases := []string{
		`/bin/rm TARGET`,
		`/usr/bin/env -i /bin/rm TARGET`,
		`/usr/bin/env -u IKESU_GUARD_MARKER /bin/rm TARGET`,
		`command /bin/rm TARGET`,
		`builtin command /bin/rm TARGET`,
		`exec /bin/rm TARGET`,
		`nohup /bin/rm TARGET`,
		`nice -n 2 /bin/rm TARGET`,
		`false || /bin/rm TARGET`,
		`true && /bin/rm TARGET`,
		`printf ok; /bin/rm TARGET`,
		`printf ok | /bin/rm TARGET`,
		`if true; then /bin/rm TARGET; fi`,
		`while true; do /bin/rm TARGET; done`,
		`for x in 1; do /bin/rm TARGET; done`,
		`case x in x) /bin/rm TARGET;; esac`,
		`( /bin/rm TARGET )`,
		`printf '%s' $(/bin/rm TARGET)`,
		"printf '%s' `/bin/rm TARGET`",
		`f() { /bin/rm TARGET; }; f`,
		`/usr/bin/env -i /bin/zsh -c '"/bin/rm" TARGET'`,
	}
	for _, shell := range []string{"/bin/bash", "/bin/zsh"} {
		for _, code := range cases {
			t.Run(filepath.Base(shell)+"/"+code, func(t *testing.T) {
				a, env, target := newEntry(t, binary)
				if e := a.Sync("codex"); e != nil {
					t.Fatal(e)
				}
				// TempDir contains no quote characters; preserve its explicit '..' spelling.
				code = strings.ReplaceAll(code, "TARGET", target)
				command := exec.Command(shell, "-lc", code)
				command.Env = env
				b, e := command.CombinedOutput()
				if e == nil || !strings.Contains(string(b), "対象プロジェクト・必要な理由・実施内容を説明する申請文が必要") {
					t.Fatal("pre-execution guard did not reject", e, string(b))
				}
				if _, e = os.Stat(target); e != nil {
					t.Fatal("target deleted", e)
				}
			})
		}
		for _, code := range []string{"ls", "cd /tmp", "printf ok | grep ok", "git status"} {
			t.Run(filepath.Base(shell)+"/allow/"+code, func(t *testing.T) {
				a, env, _ := newEntry(t, binary)
				if e := a.Sync("codex"); e != nil {
					t.Fatal(e)
				}
				init := exec.Command("git", "init", "-q", a.Root)
				if b, e := init.CombinedOutput(); e != nil {
					t.Fatal(e, string(b))
				}
				command := exec.Command(shell, "-lc", code)
				command.Env = env
				command.Dir = a.Root
				if b, e := command.CombinedOutput(); e != nil {
					t.Fatal("allow command failed", e, string(b))
				}
			})
		}
	}
}
