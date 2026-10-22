package guardmarker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPinnedServerPolicyAgainstCallerConfig(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "ikesu")
	if b, e := exec.Command("go", "build", "-o", binary, "../../cmd/ikesu").CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
	for _, mode := range []string{"enabled=false", "mode=\"record\""} {
		for _, missing := range []bool{false, true} {
			for _, sub := range []string{"exec", "line"} {
				a, env, target := newEntry(t, binary)
				if e := a.Sync("codex"); e != nil {
					t.Fatal(e)
				}
				home := filepath.Join(a.Root, "caller-home")
				if e := os.MkdirAll(filepath.Join(home, ".config", "ikesu"), 0700); e != nil {
					t.Fatal(e)
				}
				cfg := filepath.Join(home, "alternate.toml")
				if e := os.WriteFile(cfg, []byte("[guard]\n"+mode+"\n"), 0600); e != nil {
					t.Fatal(e)
				}
				for _, via := range []string{"IKESU_CONFIG", "HOME"} {
					// The HOME route loads its usual per-user config; the explicit route loads cfg.
					if e := os.WriteFile(filepath.Join(home, ".config", "ikesu", "config.toml"), []byte("[guard]\n"+mode+"\n"), 0600); e != nil {
						t.Fatal(e)
					}
					actual := append([]string{}, env...)
					for n, kv := range actual {
						if strings.HasPrefix(kv, "HOME=") {
							actual[n] = "HOME=" + home
						}
						if strings.HasPrefix(kv, "IKESU_CONFIG=") {
							if via == "IKESU_CONFIG" {
								actual[n] = "IKESU_CONFIG=" + cfg
							} else {
								actual[n] = "IKESU_CONFIG="
							}
						}
					}
					if missing {
						if e := os.Remove(a.MarkerPath); e != nil && !os.IsNotExist(e) {
							t.Fatal(e)
						}
					}
					args := []string{"guard", sub, "--", "rm", target}
					if sub == "line" {
						args = []string{"guard", "line", "--", "/bin/rm " + target}
					}
					cmd := exec.Command(binary, args...)
					cmd.Env = actual
					b, e := cmd.CombinedOutput()
					if e == nil || !strings.Contains(string(b), map[bool]string{true: "目印", false: "対象プロジェクト・必要な理由・実施内容を説明する申請文が必要"}[missing]) {
						t.Fatalf("%s %s missing=%v: %v %s", sub, via, missing, e, b)
					}
					if _, e = os.Stat(target); e != nil {
						t.Fatal("test file was deleted", e)
					}
				}
			}
		}
	}
}
