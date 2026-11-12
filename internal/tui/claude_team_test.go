package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/claudeteam"
	"github.com/kazuph/ikesu/internal/detect"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/status"
)

// Screens below follow Claude Code 2.1.32x: the agent panel sits under
// the prompt box, and a lead with teammates keeps a half-circle title.
const claudeTeamPanelIdle = `⏺ 返事を書き終えたよ。

──────────────────────────────── View teammates: ` + "`tmux -L claude-swarm-92227 a`" + ` ─
❯
────────────────────────────────────────────────────────────
  ⏵⏵ auto mode on (shift+tab to cycle) · ← 4 agents

  ⏺ main
  ◯ chromatic-short   Build zundamon Chromatic short        1d 5h 28m
  ◯ satella-rpg       Build Satella RPG from the order      1d 5h 20m
`

const claudeTeamLeadWorking = `⏺ Bash(go test ./...)
  ⎿  Running…

✻ Skedaddling… (18s · ↓ 1.1k tokens · thought for 5s)

──────────────────────────────── View teammates: ` + "`tmux -L claude-swarm-92227 a`" + ` ─
❯
────────────────────────────────────────────────────────────
  ⏵⏵ auto mode on (shift+tab to cycle) · ← 4 agents

  ⏺ main
  ◯ chromatic-short   Build zundamon Chromatic short        1d 5h 28m
`

// The lead works while a long agent panel pushes its spinner line out of
// the bottom twelve lines (2026-11-12, space 61 showed idle while working).
const claudeTeamLeadWorkingLongPanel = `⏺ Bash(python3 - <<'EOF')
  ⎿  BUILD_OK
     (5m 1s · timeout 20m)

✻ Effecting… (6m 7s · ↓ 4.8k tokens)

────────────────────────────────────────────────────────────
❯
────────────────────────────────────────────────────────────
  ⚠ compact-handoff: compact-handoff: 引き継ぎ文書なし
  ⚠ cache-warm: ☕ cache 残り54分
  Opus 5.5 |  main +3259/-178 | ~/s/g/g/m/./u/1111
  ¥101171 / $674.48 ctx ▓▓▓░░░ 32%/1M
  ⏵⏵ auto mode on · PR #345 · 1 shell · ← 4 agents

  ⏺ main
  ◯ pr4-fix          Fix stickman PASS failure
  ◯ pr3-fix          Fix PR3 script-to-bubble test
  ◯ pr1-fix          Fix PR1 nav disabled-steps test
  ◯ quality-fix      Investigate quality-branch worker resets
  ◯ review-plan-sn   Review script→name plan read-only
  ↓ 16 more
`

const claudeTeamSubagentRunning = `⏺ 返事を書き終えたよ。

────────────────────────────────────────────────────────────
❯
────────────────────────────────────────────────────────────
  ⏵⏵ auto mode on · 3 shells

  ⏺ main
  ◯ general-purpose  Running t160.yaml scroll flow   26m 49s · ↓ 139.2k tokens
  ◯ chromatic-short   Build zundamon Chromatic short        1d 5h 28m
`

func writeClaudeTeam(t *testing.T, root string, pid int, active bool) {
	t.Helper()
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "teams", "session-test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"session-test","createdAt":1,"leadAgentId":"team-lead@session-test",` +
		`"leadProcess":{"pid":` + strconv.Itoa(pid) + `,"pidSpace":"` + host + `"},` +
		`"members":[{"agentId":"team-lead@session-test","agentType":"team-lead"},` +
		`{"agentId":"chromatic-short@session-test","isActive":` + strconv.FormatBool(active) + `}]}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// judgeClaudeTeam runs the override twice around a refresh, the way the
// UI loop asks first and the process goroutine reads the files later.
func judgeClaudeTeam(t *testing.T, screen string, title string, active bool) detect.Detection {
	t.Helper()
	root := t.TempDir()
	const leadPID = 4242
	writeClaudeTeam(t, root, leadPID, active)
	a := &App{
		claudeTeams:     claudeteam.NewWatcher(),
		claudeTeamRoots: map[int]claudeTeamRoot{9: {pid: leadPID, root: root}},
	}
	sn := runtime.Snapshot{AgentID: "claude", FgPID: leadPID}
	in := detect.Input{Screen: screen, OSCTitle: title}
	d := detect.Detect("claude", in)
	a.claudeTeamIdle(9, nil, sn, in, d)
	a.claudeTeams.Refresh(time.Unix(1001, 0))
	return a.claudeTeamIdle(9, nil, sn, in, d)
}

func TestClaudeLeadWithIdleTeammatesIsIdle(t *testing.T) {
	t.Parallel()
	d := judgeClaudeTeam(t, claudeTeamPanelIdle, "◐ X投稿取得の無料APIスキル", false)
	if d.State != detect.Idle || d.RuleID != "claude_team_idle" || d.VisibleIdle {
		t.Fatalf("got %v rule=%q visibleIdle=%v, want plain idle from claude_team_idle", d.State, d.RuleID, d.VisibleIdle)
	}
}

// The team idle is plain idle, so a lead shown as working is held for the
// tracker's usual confirmations before it turns idle (and rings done).
func TestClaudeTeamIdleWaitsForTrackerConfirmation(t *testing.T) {
	t.Parallel()
	d := judgeClaudeTeam(t, claudeTeamPanelIdle, "◐ task", false)
	tr := status.NewTracker()
	now := time.Unix(2000, 0)
	tr.Update(detect.Detection{State: detect.Working}, "claude", false, now)
	now = now.Add(status.StartupGrace)
	tr.Update(detect.Detection{State: detect.Working}, "claude", false, now)
	if tr.State != detect.Working {
		t.Fatalf("setup: state %v", tr.State)
	}
	if tr.Update(d, "claude", false, now) || tr.State != detect.Working {
		t.Fatalf("first team-idle detection switched at once: %v", tr.State)
	}
	for i := 0; i < status.PendingIdleConfirmations && tr.State == detect.Working; i++ {
		now = now.Add(tickInterval)
		tr.Update(d, "claude", false, now)
	}
	if tr.State != detect.Idle {
		t.Fatalf("team idle never confirmed: %v", tr.State)
	}
}

func TestClaudeLeadStaysWorking(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		screen string
		title  string
		active bool
	}{
		{"teammate still active", claudeTeamPanelIdle, "◑ task", true},
		{"lead itself is working", claudeTeamLeadWorking, "◐ task", false},
		{"subagent still counting tokens", claudeTeamSubagentRunning, "◐ task", false},
		{"lead working above a long agent panel", claudeTeamLeadWorkingLongPanel, "◑ task", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := judgeClaudeTeam(t, tc.screen, tc.title, tc.active)
			if d.State != detect.Working || d.RuleID != "osc_title_working" {
				t.Fatalf("got %v rule=%q, want working from osc_title_working", d.State, d.RuleID)
			}
		})
	}
}

func TestClaudeTeamOverrideLeavesOtherVerdictsAlone(t *testing.T) {
	t.Parallel()
	// Without the busy title the manifest already judges the screen; the
	// override must not touch it (and must not watch the lead).
	root := t.TempDir()
	writeClaudeTeam(t, root, 4242, false)
	a := &App{claudeTeams: claudeteam.NewWatcher(), claudeTeamRoots: map[int]claudeTeamRoot{9: {pid: 4242, root: root}}}
	in := detect.Input{Screen: claudeTeamLeadWorking, OSCTitle: "✳ task"}
	d := detect.Detect("claude", in)
	got := a.claudeTeamIdle(9, nil, runtime.Snapshot{AgentID: "claude", FgPID: 4242}, in, d)
	if got != d {
		t.Fatalf("override changed a non-title verdict: %+v → %+v", d, got)
	}
	if changed := a.claudeTeams.Refresh(time.Unix(1001, 0)); len(changed) != 0 || a.claudeTeams.Idle(4242) {
		t.Fatalf("lead was watched without the busy title: changed=%v", changed)
	}
}

func TestClaudeConfigRoot(t *testing.T) {
	t.Parallel()
	if got := claudeConfigRoot([]string{"HOME=/h", "CLAUDE_CONFIG_DIR=/c"}); got != "/c" {
		t.Fatalf("CLAUDE_CONFIG_DIR: got %q", got)
	}
	if got := claudeConfigRoot([]string{"CLAUDE_CONFIG_DIR=", "HOME=/h"}); got != "/h/.claude" {
		t.Fatalf("HOME: got %q", got)
	}
}

func TestForgetClaudeTeamStopsWatching(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeClaudeTeam(t, root, 4242, false)
	a := &App{claudeTeams: claudeteam.NewWatcher(), claudeTeamRoots: map[int]claudeTeamRoot{9: {pid: 4242, root: root}}}
	in := detect.Input{Screen: claudeTeamPanelIdle, OSCTitle: "◐ task"}
	a.claudeTeamIdle(9, nil, runtime.Snapshot{AgentID: "claude", FgPID: 4242}, in, detect.Detect("claude", in))
	a.claudeTeams.Refresh(time.Unix(1001, 0))
	if !a.claudeTeams.Idle(4242) {
		t.Fatal("setup: want idle")
	}
	a.forgetClaudeTeam(9)
	if _, ok := a.claudeTeamRoots[9]; ok || a.claudeTeams.Idle(4242) {
		t.Fatal("closed pane still tracked")
	}
}
