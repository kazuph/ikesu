package session

import (
	"github.com/kazuph/ikesu/internal/config"
	"testing"
)

func TestCanonicalRestoreCommandsAndCodexArgvPriority(t *testing.T) {
	cfg, err := config.Decode([]byte(`[agent_restore]
enabled=true
[agent_restore.commands]
claude="printf 'CLAUDE:%s' '{session_id}'"
codex="printf 'CODEX:%s' '{session_id}'"
[agent_start.commands]
claude=["claude","--permission-mode","auto"]
codex=["codex","--config","sandbox_workspace_write.network_access=true"]
`))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ResumeCommand(cfg, "claude", "id", "sid-exact")
	if !ok || got != "command claude --permission-mode auto --resume sid-exact" {
		t.Fatal(got, ok)
	}
	got, ok = ResumeCommand(cfg, "codex", "id", "sid-exact")
	if !ok || got != "command codex --config sandbox_workspace_write.network_access=true resume sid-exact" {
		t.Fatal(got, ok)
	}
	if _, ok := ResumeCommand(cfg, "claude", "id", "bad;id"); ok {
		t.Fatal("unsafe session accepted")
	}
	delete(cfg.AgentStart.Commands, "claude")
	got, ok = ResumeCommand(cfg, "claude", "id", "sid-exact")
	if !ok || got != "command "+shellJoin([]string{"sh", "-lc", "printf 'CLAUDE:%s' 'sid-exact'"}) {
		t.Fatal("canonical template must use login shell", got, ok)
	}
}
