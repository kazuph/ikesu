package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

// ClaudeHarnessCommand reports whether line is exactly a hook or status line
// command from Claude Code's settings. Claude Code passes these strings to the
// shell prefix unchanged; the Bash tool always uses its bootstrap instead, and
// any other line keeps the ordinary guarded path.
func ClaudeHarnessCommand(i Input, line string) bool {
	if line == "" || ClaudePrefixLine(i, line) != line {
		return false
	}
	for _, command := range claudeHarnessCommands(i) {
		if command == line {
			return true
		}
	}
	return false
}

func claudeHarnessCommands(i Input) []string {
	config := i.Env["CLAUDE_CONFIG_DIR"]
	if config == "" {
		if i.Env["HOME"] == "" {
			return nil
		}
		config = filepath.Join(i.Env["HOME"], ".claude")
	}
	if !filepath.IsAbs(config) {
		return nil
	}
	files := []string{filepath.Join(config, "settings.json"), filepath.Join(config, "settings.local.json")}
	if filepath.IsAbs(i.Cwd) {
		files = append(files, filepath.Join(i.Cwd, ".claude", "settings.json"), filepath.Join(i.Cwd, ".claude", "settings.local.json"))
	}
	switch runtime.GOOS {
	case "darwin":
		files = append(files, "/Library/Application Support/ClaudeCode/managed-settings.json")
	case "linux":
		files = append(files, "/etc/claude-code/managed-settings.json")
	}
	var installed struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if data, err := os.ReadFile(filepath.Join(config, "plugins", "installed_plugins.json")); err == nil && json.Unmarshal(data, &installed) == nil {
		for _, entries := range installed.Plugins {
			for _, entry := range entries {
				if filepath.IsAbs(entry.InstallPath) {
					files = append(files, filepath.Join(entry.InstallPath, "hooks", "hooks.json"))
				}
			}
		}
	}
	var commands []string
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var settings struct {
			Hooks      map[string][]claudeHookGroup `json:"hooks"`
			StatusLine *claudeHookCommand           `json:"statusLine"`
		}
		if json.Unmarshal(data, &settings) != nil {
			continue
		}
		if settings.StatusLine != nil && settings.StatusLine.Type == "command" {
			commands = append(commands, settings.StatusLine.Command)
		}
		for _, groups := range settings.Hooks {
			for _, group := range groups {
				for _, hook := range group.Hooks {
					if hook.Type == "command" {
						commands = append(commands, hook.Command)
					}
				}
			}
		}
	}
	return commands
}

type claudeHookGroup struct {
	Hooks []claudeHookCommand `json:"hooks"`
}

type claudeHookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// PrepareClaudeHarness marks a configured Claude Code hook or status line for
// an unguarded launch and leaves an audit record. Anything else is unchanged.
func PrepareClaudeHarness(root string, i Input, line string) (Input, bool) {
	m, active, err := ReadMarker(i)
	if err != nil || !active || !ClaudeHarnessCommand(i, line) {
		return i, false
	}
	i.Env = EnvMap(environment(i.Env))
	i.Command, i.Args = line, nil
	i.claudeHarness = true
	_ = Log(root, i, m, Verdict{Decision: "allow", Rule: "claude-harness", Reason: "Claude Code の設定にあるフック・ステータスライン"}, "claude-prefix")
	return i, true
}
