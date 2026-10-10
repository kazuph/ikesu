package session

import (
	"github.com/kazuph/goterrace/internal/config"
	"strings"
	"unicode"
)

// builtinRestoreTemplates are the default per-agent restore command
// templates; each must contain {session_id}. Only agents with a known
// resume verb get a builtin — anything else needs
// [agent_restore.templates] or [agent_start.commands] and otherwise
// fails closed.
var builtinRestoreTemplates = map[string]string{
	"claude": "claude --resume {session_id}",
	"codex":  "codex resume {session_id}",
	"devin":  "devin --resume {session_id}",
	"grok":   "grok --resume {session_id}",
	"qwen":   "qwen --resume {session_id}",
	"agy":    "agy --conversation {session_id}",
	"letta":  "letta --conversation {session_id}",
}

// resumeArgsFor is the per-agent argv suffix appended to a configured
// [agent_start.commands] argv to resume a session.
func ResumeArgsFor(agent string) []string {
	if spec, native := NativeResumeAgents[agent]; native {
		return []string{spec.Flag}
	}
	if agent == "codex" {
		return []string{"resume"}
	}
	return []string{"--resume"}
}

// shellJoin quotes argv for typing into a shell prompt.
func shellJoin(argv []string) string {
	var b strings.Builder
	for i, a := range argv {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(shellQuote(a))
	}
	return b.String()
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("@%_+=:,./-~", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ResumeCommand renders the exact resume command line for a pane.
// Returns ok=false when no safe command can be formed — the pane stays a
// shell rather than resuming the wrong conversation.
func ResumeCommand(cfg config.Config, agent, kind, value string) (string, bool) {
	if _, native := NativeResumeAgents[agent]; native && (kind != "id" || !SafeSessionID(value)) {
		return "", false
	}
	if !SafeSessionValue(kind, value) {
		return "", false
	}
	if argv, ok := cfg.AgentStartCommand(agent); ok {
		for _, arg := range argv {
			if arg == "--last" {
				return "", false
			}
		}
		args := append(argv, ResumeArgsFor(agent)...)
		args = append(args, value)
		return "command " + shellJoin(args), true
	}
	tmpl := cfg.RestoreTemplate(agent)
	if tmpl == "" {
		tmpl = builtinRestoreTemplates[agent]
	}
	if tmpl == "" || !strings.Contains(tmpl, "{session_id}") {
		return "", false
	}
	// SPEC G3 fail-closed: a template carrying a standalone --last token
	// can never produce an exact-session command.
	for _, tok := range strings.Fields(tmpl) {
		if tok == "--last" {
			return "", false
		}
	}
	command := strings.ReplaceAll(tmpl, "{session_id}", value)
	if _, canonical := cfg.AgentRestore.Commands[agent]; canonical {
		return "command " + shellJoin([]string{"sh", "-lc", command}), true
	}
	return command, true
}
