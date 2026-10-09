package tui

import (
	"github.com/kazuph/goterrace/internal/teststate"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/detect"
	"github.com/kazuph/goterrace/internal/render"
)

func TestSPECBranchUsesRuntimeCwdBeforeSavedCwd(t *testing.T) {
	teststate.Lock(t)
	a, r := worktreeApp(t)
	other := t.TempDir()
	for _, tc := range []struct{ dir, branch string }{{r, "saved-branch"}, {other, "runtime-branch"}} {
		for _, args := range [][]string{{"init", "-q"}, {"symbolic-ref", "HEAD", "refs/heads/" + tc.branch}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "branch"}} {
			if b, err := exec.Command("git", append([]string{"-C", tc.dir}, args...)...).CombinedOutput(); err != nil {
				t.Fatalf("git: %v %s", err, b)
			}
		}
	}
	if got := gitBranch(other, r); got != "runtime-branch" {
		t.Fatal("runtime priority", got)
	}
	if got := gitBranch("", r); got != "saved-branch" {
		t.Fatal("saved fallback", got)
	}
	if got := gitBranch(t.TempDir(), r); got != "" {
		t.Fatal("available nongit runtime replaced by saved cwd", got)
	}
	pid := a.focusedPane()
	p, err := a.sup.SpawnWith(pid, other, 80, 20, nil, []string{"/bin/sh", "-c", "printf READY; exec sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	p.PollProcess()
	if snap := a.paneSnaps()[pid]; snap.Branch != "runtime-branch" || strings.Contains(snap.Title, "saved-branch") {
		t.Fatal("render snapshot branch", snap)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	if b, err := exec.Command("git", "-C", r, "worktree", "add", "-qb", "linked-branch", linked).CombinedOutput(); err != nil {
		t.Fatalf("worktree: %v %s", err, b)
	}
	if got := gitBranch(linked, r); got != "linked-branch" {
		t.Fatal("worktree HEAD", got)
	}
}

func TestSPECSafeRecordedPendingRestoreLabels(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	for _, tc := range []struct{ agent, record, value, want string }{
		{"codex", "codex", "safe-session", "saved session"},
		{"", "codex", "safe-session", "saved session"},
		{"codex", "codex", "unsafe;session", "no saved session"},
		{"codex", "claude", "safe-session", "no saved session"},
		{"codex", "codex", "", "no saved session"},
		{"", "", "safe-session", ""},
	} {
		a.RecordSession(1, AgentRecord{Agent: tc.record, Kind: "id", Value: tc.value})
		_, label := a.paneRestoreLabel(1, tc.agent)
		if label != tc.want {
			t.Fatal(tc, label)
		}
	}
}

func TestSPECExactCopilotFootersAndAnimatedSummary(t *testing.T) {
	teststate.Lock(t)
	for _, footer := range []string{"● Thinking esc cancel", "◉ Loading: 1 instruction, 5 hooks, 62 skills"} {
		d := detect.Detect("copilot", detect.Input{Screen: footer})
		if d.State != detect.Working || !d.VisibleWorking {
			t.Fatal("Copilot footer", footer, d)
		}
	}
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	pid := a.focusedPane()
	a.detectAll()
	a.SetReported(pid, "copilot", "working")
	seen := map[string]bool{}
	for spin, frame := range spinnerFrames {
		a.spin = spin
		in := render.Input{State: a.st, Panes: a.paneSnaps(), Spin: spin}
		if in.Panes[pid].Status.Rune != frame || in.Panes[pid].Status.Key != "working" {
			t.Fatal("pane spinner")
		}
		out := render.Compose(in)
		mobile := *a.st
		mobile.MobileWidthThreshold = mobile.Cols
		in.State = &mobile
		mobileOut := render.Compose(in)
		var summary strings.Builder
		for x := 0; x < a.st.Cols; x++ {
			summary.WriteString(mobileOut.Frame.Cell(x, a.st.Rows-1).Content)
		}
		if !strings.Contains(summary.String(), frame+" 1") {
			t.Fatal("summary spinner", summary.String())
		}
		var sidebar strings.Builder
		for y := 0; y < a.st.Rows-1; y++ {
			for x := 0; x < 28; x++ {
				sidebar.WriteString(out.Frame.Cell(x, y).Content)
			}
		}
		if !strings.Contains(sidebar.String(), frame) {
			t.Fatal("workspace spinner")
		}
		seen[frame] = true
	}
	if len(seen) != 4 {
		t.Fatal("fixed dot summary")
	}
}
