package guardmarker

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewedCommandsStopBeforeExecution(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "gtx")
	if b, e := exec.Command("go", "build", "-o", binary, "../../cmd/gtx").CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
	data, e := os.ReadFile("../../internal/guard/testdata/review_cases.json")
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct{ Line, Want string }
	if e = json.Unmarshal(data, &cases); e != nil {
		t.Fatal(e)
	}
	goExe, e := exec.LookPath("go")
	if e != nil {
		t.Fatal(e)
	}
	for _, shell := range []string{"/bin/bash", "/bin/zsh"} {
		for _, tc := range cases {
			t.Run(filepath.Base(shell)+"/"+tc.Line, func(t *testing.T) {
				a, env, target := newEntry(t, binary)
				if e := a.Sync("codex"); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(filepath.Join(a.Root, "guard"), filepath.Join(a.Root, "guard-link")); e != nil {
					t.Fatal(e)
				}
				for path, text := range map[string]string{"go.mod": "module guard-test-fixture\n\ngo 1.23\n", "fixture.go": "package fixture\n", "Makefile": "all:\n\t@printf 'ordinary make\\n'\n", "tp.py": "print('ordinary script')\n"} {
					if e := os.WriteFile(filepath.Join(a.Root, path), []byte(text), 0600); e != nil {
						t.Fatal(e)
					}
				}
				if b, e := exec.Command("git", "init", "-q", a.Root).CombinedOutput(); e != nil {
					t.Fatal(e, string(b))
				}
				for n, kv := range env {
					if strings.HasPrefix(kv, "PATH=") {
						env[n] = "PATH=" + filepath.Dir(goExe) + ":" + strings.TrimPrefix(kv, "PATH=")
					}
				}
				env = append(env, "A=/bin/echo")
				line := strings.NewReplacer("${ROOT}", a.Root, "${GUARD}", filepath.Join(a.Root, "guard"), "${TARGET}", target).Replace(tc.Line)
				protected := []string{a.MarkerPath, filepath.Join(a.Bin, "rm"), filepath.Join(a.Root, "guard", "shell", "p_1", ".zshenv")}
				before := map[string][]byte{}
				modes := map[string]os.FileMode{}
				for _, path := range protected {
					b, e := os.ReadFile(path)
					if e != nil {
						t.Fatal(e)
					}
					st, e := os.Lstat(path)
					if e != nil {
						t.Fatal(e)
					}
					before[path], modes[path] = b, st.Mode()
				}
				c := exec.Command(shell, "-lc", line)
				c.Env = env
				c.Dir = a.Root
				b, e := c.CombinedOutput()
				if tc.Want == "ask" {
					if e == nil || !strings.Contains(string(b), "日本語の申請文が必要") {
						t.Fatalf("not stopped before execution: %v %s", e, b)
					}
					records, e := os.ReadFile(filepath.Join(a.Root, "guard", "decisions.jsonl"))
					if e != nil || !strings.Contains(string(records), `"verdict":"ask"`) {
						t.Fatal("missing ask log", e, string(records))
					}
				} else if e != nil {
					t.Fatalf("ordinary command stopped: %v %s", e, b)
				}
				if _, e = os.Stat(target); e != nil {
					t.Fatal("test target deleted", e)
				}
				for _, path := range protected {
					b, e := os.ReadFile(path)
					if e != nil {
						t.Fatal(e)
					}
					st, e := os.Lstat(path)
					if e != nil || !bytes.Equal(before[path], b) || modes[path] != st.Mode() {
						t.Fatal("guard asset changed", path, e)
					}
				}
			})
		}
	}
}
