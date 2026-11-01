package guardmarker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A Bash tool command must not drop the runtime guard of a script it starts by
// clearing or replacing the environment, PATH or BASH_ENV. The script reads its
// target at runtime, so only the per-line check can stop it.
func TestBashToolEnvironmentEscapeKeepsScriptGuard(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "ikesu")
	if b, e := exec.Command("go", "build", "-o", binary, "../../cmd/ikesu").CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
	cases := map[string]string{
		"env -i":                 "env -i HOME=HOMEDIR PATH=/usr/bin:/bin bash SCRIPT",
		"env -i stdin pipe":      "echo x | env -i HOME=HOMEDIR PATH=/usr/bin:/bin bash -x SCRIPT 2>&1 | cat",
		"env -i dynamic PATH":    "P=$(printf /usr/bin:/bin); env -i HOME=HOMEDIR PATH=\"$P\" bash SCRIPT",
		"PATH only":              "PATH=/usr/bin:/bin bash SCRIPT",
		"env PATH only":          "env PATH=/usr/bin:/bin bash SCRIPT",
		"env -u BASH_ENV":        "env -u BASH_ENV bash SCRIPT",
		"empty BASH_ENV":         "BASH_ENV= /bin/bash SCRIPT",
		"unset BASH_ENV":         "unset BASH_ENV; /bin/bash SCRIPT",
		"absolute bash":          "/bin/bash SCRIPT",
		"sh -c env -i":           "sh -c 'env -i PATH=/usr/bin:/bin bash SCRIPT'",
		"bash -c env -i":         "/bin/bash -c 'env -i PATH=/usr/bin:/bin /bin/bash SCRIPT'",
		"export PATH then bash":  "export PATH=/usr/bin:/bin; bash SCRIPT",
		"env -i sh":              "env -i PATH=/usr/bin:/bin /bin/sh SCRIPT",
		"env -i zsh":             "env -i PATH=/usr/bin:/bin /bin/zsh SCRIPT",
		"env -i script directly": "env -i PATH=/usr/bin:/bin SCRIPT",
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			a, env, target := newEntry(t, binary)
			defer a.Close()
			if e := a.Sync("claude"); e != nil {
				t.Fatal(e)
			}
			env = append(env, "SHELL=/bin/zsh")
			home := ""
			for _, kv := range env {
				if strings.HasPrefix(kv, "HOME=") {
					home = strings.TrimPrefix(kv, "HOME=")
				}
			}
			holder := filepath.Join(home, "target-path")
			if e := os.WriteFile(holder, []byte(target), 0600); e != nil {
				t.Fatal(e)
			}
			script := filepath.Join(home, "program.sh")
			if e := os.WriteFile(script, []byte("#!/bin/bash\nT=$(/bin/cat "+shellQuote(holder)+")\n/bin/rm \"$T\"\n"), 0700); e != nil {
				t.Fatal(e)
			}
			cwdFile := "/tmp/claude-" + filepath.Base(a.Root) + "-cwd"
			defer os.Remove(cwdFile)
			command := strings.NewReplacer("SCRIPT", shellQuote(script), "HOMEDIR", shellQuote(home)).Replace(payload)
			line := fmt.Sprintf(`{ shopt -u extglob || setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL; } >/dev/null 2>&1 || true && { \builtin unalias -- 'unsetenv'; \builtin unset -f -- 'unsetenv'; } >/dev/null 2>&1 || true && eval %s < /dev/null && pwd -P >| %s`, shellQuote(command), shellQuote(cwdFile))
			c := exec.Command(filepath.Join(a.Root, "guard", "shell", a.Pane, "claude-prefix"), line)
			c.Env = a.PrepareClaude(env)
			c.Dir = home
			b, _ := c.CombinedOutput()
			t.Logf("%s", firstLines(string(b)))
			if _, e := os.Stat(target); e != nil {
				t.Fatalf("script ran unguarded and removed the target: %s", b)
			}
		})
	}
}

func firstLines(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "判定理由") || strings.Contains(line, "ブロック理由") {
			return line
		}
	}
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

// A Claude started without the pane's claude function has only the command
// shims. The env shim must not start a child whose environment drops them.
func TestShimOnlyEnvEscapeAsks(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "ikesu")
	if b, e := exec.Command("go", "build", "-o", binary, "../../cmd/ikesu").CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
	type want int
	const (
		guarded want = iota // the target survives and an ask is recorded
		runs                // the ordinary command runs
		open                // known gap without the claude function: no hook sees it
	)
	cases := []struct {
		name, payload string
		want          want
	}{
		{"env -i", "env -i HOME=HOMEDIR PATH=/usr/bin:/bin bash SCRIPT", guarded},
		{"env -i stdin pipe", "echo x | env -i HOME=HOMEDIR PATH=/usr/bin:/bin bash -x SCRIPT 2>&1 | cat", guarded},
		{"env PATH only", "env PATH=/usr/bin:/bin bash SCRIPT", guarded},
		{"env -u PATH", "env -u PATH /bin/bash SCRIPT", guarded},
		{"env -i script directly", "env -i PATH=/usr/bin:/bin SCRIPT", guarded},
		{"env -i sh", "env -i PATH=/usr/bin:/bin /bin/sh SCRIPT", guarded},
		{"sh -c env -i", "sh -c 'env -i PATH=/usr/bin:/bin bash SCRIPT'", guarded},
		{"env -S", "env -S 'PATH=/usr/bin:/bin bash' SCRIPT", guarded},
		{"plain bash", "bash SCRIPT", guarded},
		{"env PATH prepend keeps shims", "env PATH=/usr/bin:/bin:\"$PATH\" bash SCRIPT", guarded},
		{"env PATH reorder keeps shims", "env PATH=\"/usr/bin:/bin:${PATH%%:*}\" bash SCRIPT", guarded},
		{"env same PATH", "env PATH=\"$PATH\" bash SAFE", runs},
		{"env PATH appended", "env PATH=\"$PATH:/opt/extra\" bash SAFE", runs},
		{"env keeps shims", "env EXTRA=1 bash SAFE", runs},
		{"env -u unrelated", "env -u EXTRA bash SAFE", runs},
		{"PATH assignment", "PATH=/usr/bin:/bin bash SCRIPT", open},
		{"absolute env -i", "/usr/bin/env -i PATH=/usr/bin:/bin bash SCRIPT", open},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, env, target := newEntry(t, binary)
			defer a.Close()
			if e := a.Sync("claude"); e != nil {
				t.Fatal(e)
			}
			home := ""
			var shimOnly []string
			for _, kv := range env {
				key, value, _ := strings.Cut(kv, "=")
				switch {
				case key == "HOME":
					home = value
				case key == "ZDOTDIR" || key == "BASH_ENV":
					continue // no generated startup chain, as for a Claude started without the function
				}
				shimOnly = append(shimOnly, kv)
			}
			holder := filepath.Join(home, "target-path")
			if e := os.WriteFile(holder, []byte(target), 0600); e != nil {
				t.Fatal(e)
			}
			script := filepath.Join(home, "program.sh")
			if e := os.WriteFile(script, []byte("#!/bin/bash\nT=$(cat "+shellQuote(holder)+")\nrm \"$T\"\n"), 0700); e != nil {
				t.Fatal(e)
			}
			safe := filepath.Join(home, "safe.sh")
			if e := os.WriteFile(safe, []byte("#!/bin/bash\necho safe-ran\n"), 0700); e != nil {
				t.Fatal(e)
			}
			command := strings.NewReplacer("SCRIPT", shellQuote(script), "SAFE", shellQuote(safe), "HOMEDIR", shellQuote(home)).Replace(c.payload)
			cmd := exec.Command("/bin/zsh", "-f", "-c", command)
			cmd.Env = shimOnly
			cmd.Dir = home
			b, _ := cmd.CombinedOutput()
			_, statErr := os.Stat(target)
			log, _ := os.ReadFile(filepath.Join(a.Root, "guard", "decisions.jsonl"))
			switch c.want {
			case guarded:
				if statErr != nil || !strings.Contains(string(log), `"verdict":"ask"`) {
					t.Fatalf("target removed=%v, output: %s\nlog: %s", statErr != nil, b, log)
				}
			case runs:
				if statErr != nil || !strings.Contains(string(b), "safe-ran") {
					t.Fatalf("ordinary env use did not run: %s", b)
				}
			case open:
				t.Logf("known gap without the claude function: target removed=%v", statErr != nil)
			}
		})
	}
}
