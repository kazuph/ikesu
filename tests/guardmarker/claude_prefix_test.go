package guardmarker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudePrefixBeforeSnapshotEval(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "gtx")
	if b, e := exec.Command("go", "build", "-o", binary, "../../cmd/gtx").CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
	for _, payload := range []string{"/bin/rm TARGET", "env -i /bin/rm TARGET", "false || /bin/rm TARGET", "command rm TARGET", "rm TARGET", "ls", "git status", "cd /tmp; pwd", "printf '%s' ok | cat", "echo ordinary"} {
		t.Run(payload, func(t *testing.T) {
			a, env, target := newEntry(t, binary)
			env = append(env, "SHELL=/bin/zsh")
			defer a.Close()
			if e := a.Sync("claude"); e != nil {
				t.Fatal(e)
			}
			home := filepath.Dir(filepath.Dir(target)) // newEntry spells the victim as home/../victim.
			for _, kv := range env {
				if strings.HasPrefix(kv, "HOME=") {
					home = strings.TrimPrefix(kv, "HOME=")
				}
			}
			if b, e := exec.Command("git", "init", "-q", home).CombinedOutput(); e != nil {
				t.Fatal(e, string(b))
			}
			snapshot := filepath.Join(home, ".claude", "shell-snapshots", "snapshot-zsh-1-entry.sh")
			if e := os.MkdirAll(filepath.Dir(snapshot), 0700); e != nil {
				t.Fatal(e)
			}
			// Capture an actual interactive shell's exports and functions, as Claude does.
			snapshotShell := exec.Command("/bin/zsh", "-lic", `export -p > "$1"; functions >> "$1"`, "capture", snapshot)
			snapshotShell.Env = env
			if b, e := snapshotShell.CombinedOutput(); e != nil {
				t.Fatal(e, string(b))
			}
			cwdFile := "/tmp/claude-" + filepath.Base(a.Root) + "-cwd"
			defer os.Remove(cwdFile)
			command := strings.ReplaceAll(payload, "TARGET", shellQuote(target))
			line := fmt.Sprintf(`source %s 2>/dev/null || true && { shopt -u extglob || setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL; } >/dev/null 2>&1 || true && { \builtin unalias -- 'unsetenv'; \builtin unset -f -- 'unsetenv'; } >/dev/null 2>&1 || true && eval %s < /dev/null && pwd -P >| %s`, shellQuote(snapshot), shellQuote(command), shellQuote(cwdFile))
			for _, withSnapshot := range []bool{false, true} {
				actualLine := line
				if !withSnapshot {
					actualLine = strings.TrimPrefix(line, "source "+shellQuote(snapshot)+" 2>/dev/null || true && ")
				}
				for _, chained := range []bool{false, true} {
					launchEnv := env
					if chained {
						prefix := filepath.Join(t.TempDir(), "existing prefix")
						script := "#!/bin/sh\ntest \"$CLAUDE_CODE_SHELL_PREFIX\" = \"$0\" || exit 98\nexec /bin/zsh -c \"$1\"\n"
						if e := os.WriteFile(prefix, []byte(script), 0700); e != nil {
							t.Fatal(e)
						}
						launchEnv = append(append([]string(nil), env...), "CLAUDE_CODE_SHELL_PREFIX="+prefix)
					}
					c := exec.Command(filepath.Join(a.Root, "guard", "shell", a.Pane, "claude-prefix"), actualLine)
					c.Env = a.PrepareClaude(launchEnv)
					c.Dir = home
					b, e := c.CombinedOutput()
					deny := strings.Contains(payload, "rm TARGET")
					if deny {
						if e == nil || !strings.Contains(string(b), "日本語の申請文が必要") {
							t.Fatal(chained, e, string(b))
						}
						if _, e := os.Stat(target); e != nil {
							t.Fatal("victim changed", e)
						}
						if _, e := os.Stat(cwdFile); !os.IsNotExist(e) {
							t.Fatal("bootstrap executed before denial")
						}
						log, e := os.ReadFile(filepath.Join(a.Root, "guard", "decisions.jsonl"))
						if e != nil || !strings.Contains(string(log), `"verdict":"ask"`) {
							t.Fatal(e, string(log))
						}
					} else {
						if e != nil {
							t.Fatal(chained, e, string(b))
						}
						if _, e := os.Stat(cwdFile); e != nil {
							t.Fatal("unaltered bootstrap did not finish", e)
						}
					}
				}
			}
		})
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
