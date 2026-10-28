package guardmarker

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/guard"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These use the real executable and generated startup chain, never a replacement
// policy, approval function, or command implementation.
func TestScriptFileRealEntries(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "ikesu")
	if b, e := exec.Command("go", "build", "-o", binary, "../../cmd/ikesu").CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
	for _, agent := range []string{"claude", "claude-snapshot", "codex"} {
		for _, scenario := range []string{"safe", "danger", "unresolved", "blocked"} {
			t.Run(agent+"/"+scenario, func(t *testing.T) {
				root := t.TempDir()
				home := filepath.Join(root, "home")
				if e := os.Mkdir(home, 0700); e != nil {
					t.Fatal(e)
				}
				target := home + "/../victim"
				if e := os.WriteFile(target, []byte("test-only"), 0600); e != nil {
					t.Fatal(e)
				}
				a := guard.NewAssets(root, "p_1", binary, guard.Config{})
				loader := filepath.Join(root, "original-loader")
				originalSeen := filepath.Join(root, "original-seen")
				if e := os.WriteFile(loader, []byte("printf original >> "+shellQuote(originalSeen)+"\n"), 0600); e != nil {
					t.Fatal(e)
				}
				env, e := a.Prepare([]string{"BASH_ENV=" + loader, "HOME=" + home, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + root + "/socket", "IKESU_CONFIG=" + root + "/missing.toml", "IKESU_STATE_DIR=" + root}, home)
				if e != nil {
					t.Fatal(e)
				}
				defer a.Close()
				active := agent
				if active == "claude-snapshot" {
					active = "claude"
				}
				if e := a.Sync(active); e != nil {
					t.Fatal(e)
				}
				script := filepath.Join(a.Root, "program.sh")
				before := filepath.Join(a.Root, "before")
				after := filepath.Join(a.Root, "after")
				body := "printf before > " + shellQuote(before) + "\n"
				switch scenario {
				case "danger":
					body += "/bin/rm " + shellQuote(target) + "\n"
				case "unresolved":
					body += "TARGET=" + shellQuote(target) + "\n/bin/rm \"$TARGET\"\n"
				case "blocked":
					body += "git reset --hard\n"
				}
				body += "printf after > " + shellQuote(after) + "\n"
				if e := os.WriteFile(script, []byte(body), 0600); e != nil {
					t.Fatal(e)
				}
				line := "/bin/bash " + shellQuote(script)
				var c *exec.Cmd
				if strings.HasPrefix(agent, "claude") {
					env = append(env, "SHELL=/bin/zsh", "BASH_ENV=/unrelated/original-loader")
					if agent == "claude-snapshot" {
						snapshot := filepath.Join(home, ".claude", "shell-snapshots", "snapshot-zsh-1-script.sh")
						os.MkdirAll(filepath.Dir(snapshot), 0700)
						os.WriteFile(snapshot, []byte("export BASH_ENV="+shellQuote(loader)+"\n"), 0600)
						cwdFile := "/tmp/claude-script-" + filepath.Base(root) + "-cwd"
						defer os.Remove(cwdFile)
						line = fmt.Sprintf(`source %s 2>/dev/null || true && { shopt -u extglob || setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL; } >/dev/null 2>&1 || true && { \builtin unalias -- 'unsetenv'; \builtin unset -f -- 'unsetenv'; } >/dev/null 2>&1 || true && eval %s < /dev/null && pwd -P >| %s`, shellQuote(snapshot), shellQuote(line), shellQuote(cwdFile))
					}
					c = exec.Command(binary, "guard", "claude-prefix", "--", line)
					env = a.PrepareClaude(env)
				} else {
					c = exec.Command("/bin/bash", "-c", line)
				}
				c.Env = env
				c.Dir = a.Root
				out, e := c.CombinedOutput()
				if scenario == "safe" {
					if e != nil {
						t.Fatalf("safe: %v %s", e, out)
					}
					if _, e = os.Stat(after); e != nil {
						t.Fatal(e)
					}
					if _, e = os.Stat(originalSeen); e != nil {
						t.Fatal("original BASH_ENV was not sourced", e)
					}
				} else {
					if e == nil {
						t.Fatalf("guard did not stop: %s", out)
					}
					if _, e = os.Stat(target); e != nil {
						t.Fatalf("target changed: %v", e)
					}
					if _, e = os.Stat(after); !os.IsNotExist(e) {
						t.Fatal("after line ran")
					}
					_, e = os.Stat(before)
					if scenario == "unresolved" && e != nil {
						t.Fatalf("before line missing: %v %s", e, out)
					}
					if scenario != "unresolved" && !os.IsNotExist(e) {
						t.Fatalf("preflight ran body: %s", out)
					}
					if scenario == "danger" && !strings.Contains(string(out), "の2行目:") {
						t.Fatalf("source missing: %s", out)
					}
					if scenario == "blocked" && !strings.Contains(string(out), "git reset") {
						t.Fatalf("block missing: %s", out)
					}
				}
				t.Logf("%s/%s: %s", agent, scenario, out)
			})
		}
	}
}
