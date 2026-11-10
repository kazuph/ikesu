package guardmarker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWideScanFailsWithoutAsk(t *testing.T) {
	t.Parallel()
	for _, shell := range []string{"/bin/bash", "/bin/zsh"} {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			a, env, _ := newEntry(t, fixtureBinary)
			if e := a.Sync("codex"); e != nil {
				t.Fatal(e)
			}
			// 起動した場所は a.Root/home。ルートを丸ごと探す find は確認待ちにならず失敗する。
			command := exec.Command(shell, "-lc", "find / -maxdepth 0 -name x")
			command.Env = env
			command.Dir = a.Root
			b, e := command.CombinedOutput()
			out := string(b)
			if e == nil || !strings.Contains(out, "【警告】大規模なスキャンを検知: /") || strings.Contains(out, "判断待ち") {
				t.Fatalf("find / was not denied cleanly: %v %s", e, out)
			}
			ok := exec.Command(shell, "-lc", "find . -maxdepth 1 -name nothing")
			ok.Env = env
			ok.Dir = filepath.Join(a.Root, "home")
			if b, e := ok.CombinedOutput(); e != nil {
				t.Fatalf("find . inside launch dir failed: %v %s", e, b)
			}
		})
	}
}

func guardLogLines(t *testing.T, root string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "guard", "decisions.jsonl"))
	if err != nil {
		return 0
	}
	return strings.Count(string(b), "\n")
}

// 探索コマンドはシム（PATH の差し替え）の対象外なので、スクリプトの中の grep・ls は guard を通らない。
func TestSearchCommandsAreNotShimmed(t *testing.T) {
	t.Parallel()
	for _, shell := range []string{"/bin/bash", "/bin/zsh"} {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			a, env, _ := newEntry(t, fixtureBinary)
			if e := a.Sync("codex"); e != nil {
				t.Fatal(e)
			}
			work := filepath.Join(a.Root, "home")
			if e := os.WriteFile(filepath.Join(work, "f"), []byte("y\n"), 0o600); e != nil {
				t.Fatal(e)
			}
			// 起動の時に1回走る分を数え終えてから、ループの前後で件数を比べる。
			warm := exec.Command(shell, "-lc", "true")
			warm.Env, warm.Dir = env, work
			_, _ = warm.CombinedOutput()
			before := guardLogLines(t, a.Root)
			start := time.Now()
			loop := exec.Command(shell, "-lc", "for i in $(seq 30); do grep -q x f; ls >/dev/null; done; true")
			loop.Env, loop.Dir = env, work
			if b, e := loop.CombinedOutput(); e != nil {
				t.Fatalf("%v %s", e, b)
			}
			t.Logf("30回の grep+ls: %v", time.Since(start))
			if after := guardLogLines(t, a.Root); after-before > 3 {
				t.Fatalf("guard ran %d times for grep/ls in a loop", after-before)
			}
			for _, name := range []string{"grep", "ls", "find", "rg", "du", "mdfind"} {
				if _, err := os.Lstat(filepath.Join(a.Root, "guard", "bin", name)); err == nil {
					t.Fatalf("shim exists for %s", name)
				}
			}
		})
	}
}
