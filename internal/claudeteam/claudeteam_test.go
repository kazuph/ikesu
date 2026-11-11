package claudeteam

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

type member struct {
	AgentID   string `json:"agentId"`
	AgentType string `json:"agentType,omitempty"`
	IsActive  *bool  `json:"isActive,omitempty"`
}

func flag(v bool) *bool { return &v }

// writeTeam writes <root>/teams/<name>/config.json the way Claude Code
// lays it out: the lead first (agentType team-lead), then teammates.
func writeTeam(t *testing.T, root, name string, pid int, space string, created int64, teammates ...*bool) string {
	t.Helper()
	cfg := map[string]any{
		"name":        name,
		"createdAt":   created,
		"leadAgentId": "team-lead@" + name,
		"leadProcess": map[string]any{"pid": pid, "pidSpace": space},
	}
	members := []member{{AgentID: "team-lead@" + name, AgentType: "team-lead"}}
	for i, active := range teammates {
		members = append(members, member{AgentID: string(rune('a'+i)) + "@" + name, IsActive: active})
	}
	cfg["members"] = members
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "teams", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func host(t *testing.T) string {
	t.Helper()
	h, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// refresh steps the clock past the refresh interval and refreshes.
func refresh(w *Watcher, now *time.Time) []int {
	*now = now.Add(refreshInterval)
	return w.Refresh(*now)
}

func TestIdleOnlyWhenEveryTeammateIsInactive(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		teammates []*bool
		want      bool
	}{
		{"all inactive", []*bool{flag(false), flag(false)}, true},
		{"one active", []*bool{flag(false), flag(true)}, false},
		{"flag missing counts as busy", []*bool{flag(false), nil}, false},
		{"lead only is not a team", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeTeam(t, root, "session-aaaa", 4242, host(t), 1, tc.teammates...)
			w := NewWatcher()
			now := time.Unix(1000, 0)
			w.Watch(7, 4242, root, now)
			refresh(w, &now)
			if got := w.Idle(4242); got != tc.want {
				t.Fatalf("Idle = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRefreshReportsPaneWhenTeammateStartsAndStops(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	h := host(t)
	writeTeam(t, root, "session-aaaa", 4242, h, 1, flag(false))
	w := NewWatcher()
	now := time.Unix(1000, 0)
	w.Watch(7, 4242, root, now)
	if got := refresh(w, &now); !slices.Equal(got, []int{7}) {
		t.Fatalf("first refresh changed = %v, want [7]", got)
	}
	if got := refresh(w, &now); len(got) != 0 {
		t.Fatalf("unchanged refresh changed = %v, want none", got)
	}

	path := writeTeam(t, root, "session-aaaa", 4242, h, 1, flag(true))
	// The rewrite may land in the same mtime tick; force a visible change.
	bump(t, path)
	if got := refresh(w, &now); !slices.Equal(got, []int{7}) || w.Idle(4242) {
		t.Fatalf("teammate started: changed = %v idle = %v, want [7] false", got, w.Idle(4242))
	}

	path = writeTeam(t, root, "session-aaaa", 4242, h, 1, flag(false))
	bump(t, path)
	if got := refresh(w, &now); !slices.Equal(got, []int{7}) || !w.Idle(4242) {
		t.Fatalf("teammate finished: changed = %v idle = %v, want [7] true", got, w.Idle(4242))
	}
}

func bump(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	next := info.ModTime().Add(time.Second)
	if err := os.Chtimes(path, next, next); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshIsThrottled(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTeam(t, root, "session-aaaa", 4242, host(t), 1, flag(false))
	w := NewWatcher()
	now := time.Unix(1000, 0)
	w.Watch(7, 4242, root, now)
	refresh(w, &now)
	path := writeTeam(t, root, "session-aaaa", 4242, host(t), 1, flag(true))
	bump(t, path)
	if got := w.Refresh(now.Add(refreshInterval / 2)); got != nil || !w.Idle(4242) {
		t.Fatalf("refresh inside the interval read files: changed = %v idle = %v", got, w.Idle(4242))
	}
}

func TestOtherLeadsAndHostsDoNotCount(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTeam(t, root, "session-other", 5151, host(t), 1, flag(false))
	writeTeam(t, root, "session-remote", 4242, "elsewhere.invalid", 1, flag(false))
	w := NewWatcher()
	now := time.Unix(1000, 0)
	w.Watch(7, 4242, root, now)
	refresh(w, &now)
	if w.Idle(4242) {
		t.Fatal("a team led by another PID or another host made pid 4242 idle")
	}
}

func TestNewestTeamForReusedPIDWins(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	h := host(t)
	// A stale team left by a dead session whose PID was reused.
	writeTeam(t, root, "session-old", 4242, h, 1, flag(false))
	writeTeam(t, root, "session-new", 4242, h, 2, flag(true))
	w := NewWatcher()
	now := time.Unix(1000, 0)
	w.Watch(7, 4242, root, now)
	refresh(w, &now)
	if w.Idle(4242) {
		t.Fatal("the stale team decided the state instead of the newest one")
	}
}

func TestTeamCreatedAfterFirstScanIsFound(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "teams"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := NewWatcher()
	now := time.Unix(1000, 0)
	w.Watch(7, 4242, root, now)
	refresh(w, &now)
	writeTeam(t, root, "session-aaaa", 4242, host(t), 1, flag(false))
	bump(t, filepath.Join(root, "teams"))
	if got := refresh(w, &now); !slices.Equal(got, []int{7}) || !w.Idle(4242) {
		t.Fatalf("new team: changed = %v idle = %v, want [7] true", got, w.Idle(4242))
	}
}

func TestUnwatchedLeadExpires(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTeam(t, root, "session-aaaa", 4242, host(t), 1, flag(false))
	w := NewWatcher()
	now := time.Unix(1000, 0)
	w.Watch(7, 4242, root, now)
	refresh(w, &now)
	if !w.Idle(4242) {
		t.Fatal("setup: want idle")
	}
	now = now.Add(watchTTL)
	if got := refresh(w, &now); !slices.Equal(got, []int{7}) || w.Idle(4242) {
		t.Fatalf("expired watch: changed = %v idle = %v, want [7] false", got, w.Idle(4242))
	}
}
