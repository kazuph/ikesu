package guardmarker

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
