package machine

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRemoteCommandsAcrossShells(t *testing.T) {
	for _, name := range []string{"sh", "bash", "zsh", "fish"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			shell := requireShell(t, name)
			home := t.TempDir()
			entry := filepath.Join(home, ".local/bin/ikesu")
			if err := os.MkdirAll(filepath.Dir(entry), 0700); err != nil {
				t.Fatal(err)
			}
			writeExecutable(t, entry, `printf '%s\000' "$@"`)
			override := filepath.Join(home, `a'\$ custom ikesu`)
			if err := os.Symlink(entry, override); err != nil {
				t.Fatal(err)
			}
			login := filepath.Join(home, "login shell")
			writeExecutable(t, login, `printf '%s\000' "$1"; exec "$TEST_SHELL" "$@"`)
			dir := "場 所\\\\'\\$HOME\n続き"
			if err := os.Mkdir(filepath.Join(home, dir), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(entry, filepath.Join(home, dir, "ikesu")); err != nil {
				t.Fatal(err)
			}
			p := Profile{ID: "m1", Target: "host", Session: "日本 語'\\\\$", Enabled: true}
			argv := []string{"", "a'b", `a\\b`, `\'`, `end\`, "$HOME", "a b\tc", "line\nbreak", "日本語"}
			encoded := strings.Join(argv, "\x00") + "\x00"
			execArgs := append([]string{"./ikesu"}, argv...)
			ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
			defer cancel()
			run := func(args []string, input, want string, code int) string {
				t.Helper()
				// OpenSSH joins the arguments following the SSH target with spaces.
				remote := strings.Join(args[slices.Index(args, p.Target)+1:], " ")
				cmd := exec.CommandContext(ctx, shell, "-c", remote)
				cmd.Env = append(shellEnv(home), "SHELL="+login, "TEST_SHELL="+shell)
				cmd.Stdin = strings.NewReader(input)
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				out, err := cmd.Output()
				if cmd.ProcessState.ExitCode() != code || string(out) != want {
					t.Fatalf("%s -c %q: status %v, stdout %q, stderr %q; want status %d and stdout %q", shell, remote, err, out, &stderr, code, want)
				}
				return stderr.String()
			}

			forwarded := "--session\x00" + p.Session + "\x00" + encoded
			run(ForwardArgs(p, "ikesu", argv), "", forwarded, 0)
			run(ForwardArgs(p, override, argv), "", forwarded, 0)
			run(ProbeArgs(p.Target), "", "", 0)
			script := `set -eu; printf '%s\000' "$@"; cat`
			stdin := "画像データ\x00\n"
			run(SSHArgs(p, append([]string{"sh", "-c", script, "clipboard"}, argv...)), stdin, encoded+stdin, 0)
			remote := RunArgs(p, RemoteDirectory("", "", "~/"+dir), execArgs, false)
			run(remote, "", "-lic\x00"+encoded, 0)
			marker := `marker'\$`
			id, observed := ObserveRun([]Profile{p}, append([]string{"ssh"}, remote...), marker)
			if id != p.ID {
				t.Fatal("remote run was not recognized")
			}
			if stderr := run(observed, "", "-lic\x00"+encoded, 0); !strings.HasPrefix(stderr, marker+"\n") {
				t.Fatalf("missing connection marker in %q", stderr)
			}
			if err := os.Remove(entry); err != nil {
				t.Fatal(err)
			}
			if stderr := run(ForwardArgs(p, "ikesu", nil), "", "", 127); stderr != "ikesu missing on host: "+entry+"\n" {
				t.Fatalf("unexpected diagnostic %q", stderr)
			}
		})
	}
}

func TestRemoteSyncAcrossShells(t *testing.T) {
	rsync, err := exec.LookPath("rsync")
	if err != nil {
		t.Skip("rsync is unavailable")
	}
	for _, name := range []string{"sh", "bash", "zsh", "fish"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			shell := requireShell(t, name)
			home, source := t.TempDir(), t.TempDir()
			writeExecutable(t, filepath.Join(home, "ssh"), `
while [ "$#" -gt 0 ] && [ "$1" != -- ]; do shift; done
shift
[ "$1" = user@host ] || exit 97
shift
exec "$TEST_SHELL" -c "$*"`)
			const expected = "同期するデータ\n"
			if err := os.WriteFile(filepath.Join(source, "source.txt"), []byte(expected), 0600); err != nil {
				t.Fatal(err)
			}
			destination := "同 期\\\\'\\$HOME\n続き"
			steps, err := SyncPlan(Profile{Target: "user@host"}, source, RemoteDirectory("", "", "~/"+destination))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
			defer cancel()
			env := append(shellEnv(home), "TEST_SHELL="+shell, "PATH="+home+":"+filepath.Dir(rsync)+":/usr/bin:/bin")
			mkdir := exec.CommandContext(ctx, shell, "-c", steps[0][len(steps[0])-1])
			mkdir.Env = env
			if out, err := mkdir.CombinedOutput(); err != nil {
				t.Fatalf("create remote directory: %v\n%s", err, out)
			}
			transfer := exec.CommandContext(ctx, steps[1][0], steps[1][1:]...)
			transfer.Env = env
			if out, err := transfer.CombinedOutput(); err != nil {
				t.Fatalf("transfer over simulated SSH: %v\n%s", err, out)
			}
			out, err := os.ReadFile(filepath.Join(home, destination, "source.txt"))
			if err != nil || string(out) != expected {
				t.Fatalf("synced file: %q, %v", out, err)
			}
		})
	}
}

func requireShell(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is unavailable", name)
	}
	return path
}

func shellEnv(home string) []string {
	return []string{"HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "PATH=/usr/bin:/bin", "TERM=xterm"}
}

func writeExecutable(t *testing.T, path, script string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
}
