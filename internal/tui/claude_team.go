package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kazuph/ikesu/internal/detect"
	"github.com/kazuph/ikesu/internal/runtime"
)

// claudeTeamRoot remembers the Claude config directory resolved for the
// lead process currently in a pane, so detection does not copy the pane
// environment on every title frame.
type claudeTeamRoot struct {
	pid  int
	root string
}

// claudeSubagentRunning matches a row of Claude's agent panel for a
// subagent still at work: "◯ name  description  26m 44s · ↓ 104.6k tokens".
// Idle teammates show only their age ("◯ name  description  1d 5h 30m"),
// and finished subagents leave the panel.
var claudeSubagentRunning = regexp.MustCompile(`(?m)^\s*◯\s+\S.*·\s+[↓↑]\s+[\d.,]+[kKmM]?\s+tokens\s*$`)

// claudeTeamIdle lets a Claude lead whose teammates have all gone idle
// read as idle. Claude Code keeps the busy half-circle title while any
// teammate exists, even after every teammate finished, so the title rule
// alone keeps such a lead "working" forever. The override applies only
// when the team config says every teammate is idle, no subagent row is
// still counting tokens, and the screen without the title is the plain
// input prompt; anything else keeps the title's verdict.
func (a *App) claudeTeamIdle(pid int, p *runtime.Pane, sn runtime.Snapshot, in detect.Input, d detect.Detection) detect.Detection {
	if sn.AgentID != "claude" || d.RuleID != "osc_title_working" || sn.FgPID <= 0 || a.claudeTeams == nil {
		return d
	}
	if a.claudeTeamRoots == nil {
		a.claudeTeamRoots = map[int]claudeTeamRoot{}
	}
	r, ok := a.claudeTeamRoots[pid]
	if !ok || r.pid != sn.FgPID {
		if ok {
			a.claudeTeams.Unwatch(r.pid)
		}
		env := os.Environ()
		if p != nil {
			env = p.SessionEnvironment()
		}
		r = claudeTeamRoot{pid: sn.FgPID, root: claudeConfigRoot(env)}
		a.claudeTeamRoots[pid] = r
	}
	a.claudeTeams.Watch(pid, sn.FgPID, r.root)
	// The lead's own spinner line ("✻ Effecting… (6m 7s · ↓ 4.8k tokens)")
	// sits right above the prompt box. A long agent panel below can push it
	// out of the manifest's bottom-12-lines window, so look for it here.
	if !a.claudeTeams.Idle(sn.FgPID) || claudeSubagentRunning.MatchString(in.Screen) || detect.ActivityFingerprint(sn.AgentID, in.Screen) != "" {
		return d
	}
	bare := detect.Detect(sn.AgentID, detect.Input{Screen: in.Screen, OSCProgress: in.OSCProgress})
	if bare.State != detect.Idle || bare.RuleID != "live_prompt_box" {
		return d
	}
	// Plain (not visible) idle: the tracker then holds Working for its usual
	// confirmations, so a lead between two spinner frames does not flash
	// idle and ring the done notification.
	return detect.Detection{State: detect.Idle, RuleID: "claude_team_idle"}
}

// forgetClaudeTeam drops the team tracking of a pane that went away.
func (a *App) forgetClaudeTeam(pid int) {
	if r, ok := a.claudeTeamRoots[pid]; ok {
		a.claudeTeams.Unwatch(r.pid)
		delete(a.claudeTeamRoots, pid)
	}
}

// claudeConfigRoot resolves Claude Code's config directory the way Claude
// does: CLAUDE_CONFIG_DIR, else $HOME/.claude.
func claudeConfigRoot(env []string) string {
	home := ""
	for _, item := range env {
		key, value, found := strings.Cut(item, "=")
		if !found {
			continue
		}
		switch key {
		case "CLAUDE_CONFIG_DIR":
			if value != "" {
				return value
			}
		case "HOME":
			home = value
		}
	}
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".claude")
}
