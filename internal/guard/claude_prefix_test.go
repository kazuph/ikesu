package guard

import (
	"path/filepath"
	"strings"
	"testing"
)

func bootstrapForTest(snapshot, payload, cwd string) string {
	s := strings.Replace(claudeBootstrap, "GTX_SNAPSHOT", quote(snapshot), 1)
	s = strings.Replace(s, "GTX_PAYLOAD", quote(payload), 1)
	return strings.Replace(s, "GTX_CWD", quote(cwd), 1)
}

func TestClaudePrefixRecognizesOnlyCanonicalBootstrap(t *testing.T) {
	i := Input{Env: map[string]string{"HOME": "/tmp/test-user"}}
	snapshot := "/tmp/test-user/.claude/shell-snapshots/snapshot-zsh-123-x.sh"
	for _, payload := range []string{"ls", "git status", "cd /tmp; pwd", "printf '%s' ok | cat", `echo "$HOME"`, "false || /bin/rm '/tmp/victim'", "env -i /bin/rm /tmp/victim"} {
		line := bootstrapForTest(snapshot, payload, "/tmp/claude-123-cwd")
		for _, candidate := range []string{line, strings.TrimPrefix(line, "source "+quote(snapshot)+" 2>/dev/null || true && ")} {
			if got := ClaudePrefixLine(i, candidate); got != payload {
				t.Fatalf("payload=%q got=%q", payload, got)
			}
		}
	}
	good := bootstrapForTest(snapshot, "ls", "/tmp/claude-123-cwd")
	for name, line := range map[string]string{
		"extra command":         good + "; /bin/rm /tmp/victim",
		"glob snapshot":         strings.Replace(good, quote(snapshot), strings.Replace(snapshot, "snapshot-zsh-123-x.sh", "snapshot-zsh-*.sh", 1), 1),
		"different snapshot":    strings.Replace(good, snapshot, "/tmp/untrusted.sh", 1),
		"different destination": strings.Replace(good, "/tmp/claude-123-cwd", "/tmp/victim", 1),
		"changed settings":      strings.Replace(good, "shopt -u extglob", "shopt -u extdebug", 1),
		"expanded eval":         strings.Replace(good, "eval 'ls'", `eval "$PAYLOAD"`, 1),
		"negated":               "! " + good,
		"extra eval":            strings.Replace(good, "eval 'ls'", "eval 'ls' && eval '/bin/rm /tmp/victim'", 1),
		"plain eval":            "eval ls",
		"hook":                  "node ~/.claude/hooks/stop-require-japanese.js",
		"syntax error":          "eval '",
	} {
		t.Run(name, func(t *testing.T) {
			if got := ClaudePrefixLine(i, line); got != line {
				t.Fatalf("unexpected normalization %q", got)
			}
		})
	}
	i.Env["CLAUDE_CONFIG_DIR"] = "/tmp/custom-claude"
	custom := bootstrapForTest("/tmp/custom-claude/shell-snapshots/snapshot-zsh-1-a.sh", "ls", "/tmp/claude-1-cwd")
	if got := ClaudePrefixLine(i, custom); got != "ls" {
		t.Fatal(got)
	}
}

func TestClaudePrefixEnvironmentIsScopedAndRestored(t *testing.T) {
	for _, old := range []string{"", "custom-prefix --unchanged"} {
		for _, present := range []bool{false, true} {
			a := NewAssets(t.TempDir(), "p_1", "/usr/bin/true", Config{})
			input := []string{"HOME=" + t.TempDir(), "PATH=/usr/bin:/bin", "GTX_PANE_ID=p1"}
			if present {
				input = append(input, "CLAUDE_CODE_SHELL_PREFIX="+old)
			}
			env, e := a.Prepare(input, "/tmp")
			if e != nil {
				t.Fatal(e)
			}
			defer a.Close()
			if present && EnvMap(env)["CLAUDE_CODE_SHELL_PREFIX"] != old {
				t.Fatal("ordinary pane prefix changed")
			}
			prepared := a.PrepareClaude(env)
			prefix := EnvMap(prepared)["CLAUDE_CODE_SHELL_PREFIX"]
			want := filepath.Join(a.Root, "guard", "shell", a.Pane, "claude-prefix")
			if prefix != want {
				t.Fatal(prefix, want)
			}
			got, exists := EnvMap(RestoreInherited(prepared))["CLAUDE_CODE_SHELL_PREFIX"]
			if exists != present || present && got != old {
				t.Fatal(got, exists, old, present)
			}
		}
	}
}
