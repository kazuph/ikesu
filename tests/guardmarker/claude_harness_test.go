package guardmarker

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Claude Code passes configured hook and status line commands to the shell
// prefix verbatim. Those run as the user's own scripts; any other line,
// including a changed copy of the same command, keeps the guarded path.
func TestClaudeHarnessCommandsRunOutsideGuard(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "ikesu")
	if b, e := exec.Command("go", "build", "-o", binary, "../../cmd/ikesu").CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
	a, env, _ := newEntry(t, binary)
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
	config := filepath.Join(home, ".claude")
	plugin := filepath.Join(config, "plugins", "cache", "market", "demo", "1.0.0")
	if e := os.MkdirAll(filepath.Join(plugin, "hooks"), 0700); e != nil {
		t.Fatal(e)
	}
	// The '..' spelling selects the existing rm-paths ask rule, as in newEntry.
	lock := home + "/../model-limits.lock"
	statusline := filepath.Join(config, "statusline.sh")
	hook := filepath.Join(config, "hooks", "prompt.sh")
	pluginHook := filepath.Join(plugin, "hooks", "check.sh")
	scripts := map[string]string{
		statusline: "#!/bin/bash\ninput=$(cat)\nLOCK=" + shellQuote(lock) + "\nif [[ -d \"$LOCK\" ]]; then\n  rmdir \"$LOCK\" 2>/dev/null\nfi\nprintf 'status %s guard=%s\\n' \"$input\" \"${IKESU_GUARD_MARKER:-none}\"\n",
		hook:       "#!/bin/bash\nINPUT=$(cat)\nprintf '%s' \"$INPUT\"\n",
		pluginHook: "#!/bin/bash\nprintf 'plugin root=%s\\n' \"${CLAUDE_PLUGIN_ROOT:-unset}\"\n",
	}
	for path, body := range scripts {
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, []byte(body), 0700); e != nil {
			t.Fatal(e)
		}
	}
	writeJSON := func(path string, v any) {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	statusLine := "~/.claude/statusline.sh"
	promptHook := "~/.claude/hooks/prompt.sh"
	pluginLine := "${CLAUDE_PLUGIN_ROOT}/hooks/check.sh"
	writeJSON(filepath.Join(config, "settings.json"), map[string]any{
		"statusLine": map[string]any{"type": "command", "command": statusLine},
		"hooks": map[string]any{"UserPromptSubmit": []any{map[string]any{"hooks": []any{
			map[string]any{"type": "command", "command": promptHook},
		}}}},
	})
	writeJSON(filepath.Join(plugin, "hooks", "hooks.json"), map[string]any{"hooks": map[string]any{"Stop": []any{map[string]any{"hooks": []any{
		map[string]any{"type": "command", "command": pluginLine},
	}}}}})
	writeJSON(filepath.Join(config, "plugins", "installed_plugins.json"), map[string]any{"version": 2, "plugins": map[string]any{"demo@market": []any{map[string]any{"installPath": plugin}}}})

	logPath := filepath.Join(a.Root, "guard", "decisions.jsonl")
	runIn := func(dir, line, stdin string, extra ...string) (string, error) {
		c := exec.Command(filepath.Join(a.Root, "guard", "shell", a.Pane, "claude-prefix"), line)
		c.Env = append(a.PrepareClaude(env), extra...)
		c.Dir = dir
		c.Stdin = strings.NewReader(stdin)
		b, e := c.CombinedOutput()
		return string(b), e
	}
	run := func(line, stdin string, extra ...string) (string, error) { return runIn(home, line, stdin, extra...) }
	ask := func() int {
		b, _ := os.ReadFile(logPath)
		return strings.Count(string(b), `"verdict":"ask"`)
	}

	t.Run("status line removes stale lock and prints", func(t *testing.T) {
		if e := os.Mkdir(lock, 0700); e != nil {
			t.Fatal(e)
		}
		out, e := run(statusLine, "Opus")
		if e != nil || !strings.Contains(out, "status Opus guard=none") {
			t.Fatal(e, out)
		}
		if _, e := os.Stat(lock); !os.IsNotExist(e) {
			t.Fatal("stale lock remains", e)
		}
	})
	t.Run("prompt hook passes danger words through printf", func(t *testing.T) {
		out, e := run(promptHook, `{"prompt":"github-actions-deploy"}`)
		if e != nil || out != `{"prompt":"github-actions-deploy"}` {
			t.Fatal(e, out)
		}
	})
	t.Run("plugin hook", func(t *testing.T) {
		out, e := run(pluginLine, "", "CLAUDE_PLUGIN_ROOT="+plugin)
		if e != nil || !strings.Contains(out, "plugin root="+plugin) {
			t.Fatal(e, out)
		}
	})
	t.Run("audit record", func(t *testing.T) {
		b, e := os.ReadFile(logPath)
		if e != nil || strings.Count(string(b), `"rule":"claude-harness"`) != 3 || ask() != 0 {
			t.Fatal(e, string(b))
		}
	})
	// A project's .claude/settings*.json is in the tree the agent edits. Its
	// hooks keep the ordinary guard even when they match the line exactly.
	t.Run("guarded project settings hook", func(t *testing.T) {
		project := filepath.Join(home, "project")
		if e := os.MkdirAll(filepath.Join(project, ".claude"), 0700); e != nil {
			t.Fatal(e)
		}
		projectHook := "./project-hook.sh"
		if e := os.WriteFile(filepath.Join(project, "project-hook.sh"), []byte("#!/bin/bash\nINPUT=$(cat)\nprintf '%s' \"$INPUT\"\n"), 0700); e != nil {
			t.Fatal(e)
		}
		for _, name := range []string{"settings.json", "settings.local.json"} {
			writeJSON(filepath.Join(project, ".claude", name), map[string]any{
				"statusLine": map[string]any{"type": "command", "command": projectHook},
				"hooks": map[string]any{"UserPromptSubmit": []any{map[string]any{"hooks": []any{
					map[string]any{"type": "command", "command": projectHook},
				}}}},
			})
		}
		before := ask()
		out, e := runIn(project, projectHook, `{"prompt":"github-actions-deploy"}`)
		if e == nil || !strings.Contains(out, "Command cancelled") || ask() <= before {
			t.Fatal("project settings hook ran outside the guard", e, out)
		}
		b, _ := os.ReadFile(logPath)
		if strings.Contains(string(b), `"cmd_line":"./project-hook.sh"`) && strings.Contains(string(b), `"rule":"claude-harness","reason":"Claude Code の設定にあるフック・ステータスライン","cmd":["./project-hook.sh"]`) {
			t.Fatal("project hook was recorded as a harness command", string(b))
		}
	})
	// The same scripts reached through any other line stay guarded line by line.
	for name, c := range map[string]struct{ line, stdin string }{
		"changed status line": {statusLine + " ", "Opus"},
		"status line by path": {statusline, "Opus"},
		"changed prompt hook": {promptHook + " && true", `{"prompt":"github-actions-deploy"}`},
		"bash tool rmdir":     {"rmdir " + shellQuote(lock), ""},
	} {
		t.Run("guarded "+name, func(t *testing.T) {
			if e := os.MkdirAll(lock, 0700); e != nil {
				t.Fatal(e)
			}
			before := ask()
			out, e := run(c.line, c.stdin)
			if e == nil || strings.Contains(out, "status Opus") || strings.Contains(out, "github-actions-deploy\"}") && !strings.Contains(out, "Command cancelled") {
				t.Fatal(e, out)
			}
			if _, e := os.Stat(lock); e != nil {
				t.Fatal("guarded line removed the lock", e)
			}
			if ask() <= before {
				t.Fatal("no ask recorded", out)
			}
		})
	}
}
