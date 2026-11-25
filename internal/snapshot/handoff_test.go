package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const (
	gitCheck  = "rev-parse --is-inside-work-tree"
	gitBranch = "rev-parse --abbrev-ref HEAD"
	gitStatus = "status --short"
	gitDiff   = "diff --stat"
	gitLog    = "log -n 3 --oneline"
)

func TestBuildHandoffGitAndRedaction(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, status, diff, log string
		inside, timeout         bool
	}{
		{name: "dirty", inside: true, status: " M file.go", diff: " file.go | 1 +", log: "abc commit"},
		{name: "clean", inside: true, log: "abc commit"},
		{name: "non_git"},
		{name: "git_timeout", inside: true, status: "?? file", timeout: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			git := func(ctx context.Context, dir string, args ...string) (string, error) {
				if dir != "/work" {
					t.Errorf("git called in %q", dir)
				}
				switch command := strings.Join(args, " "); command {
				case gitCheck:
					if !tc.inside {
						return "", errors.New("outside git")
					}
					return "true\n", nil
				case gitBranch:
					return "feature\n", nil
				case gitStatus:
					return tc.status + "\n", nil
				case gitDiff:
					if tc.timeout {
						deadline, ok := ctx.Deadline()
						if !ok || deadline.Sub(start) < 2900*time.Millisecond || deadline.Sub(start) > 3100*time.Millisecond {
							t.Errorf("git deadline = %v, want whole collection deadline at 3s", deadline)
						}
						<-ctx.Done()
						return "", ctx.Err()
					}
					return tc.diff + "\n", nil
				case gitLog:
					if !tc.timeout {
						return tc.log + "\n", nil
					}
				}
				t.Errorf("unexpected git call %q", args)
				return "", nil
			}
			var saved string
			got := Build(context.Background(), Input{
				Workspace: "workspace", PaneID: "p1", Agent: "codex", Cwd: "/work", Mode: "handoff", Git: git,
				Lines:   []string{"before", "› review ghp_fakeExampleValue", "AWS_SECRET_KEY=sample-secret", "⠋ Working…"},
				SaveLog: func(text string) (string, error) { saved = text; return "/private/tmp/log", nil },
			})

			var fields map[string]any
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			keys := slices.Sorted(maps.Keys(fields))
			wantKeys := []string{"agent", "boundary_found", "branch", "cwd", "git_diff_stat", "git_log", "git_status", "last_turn", "log_path", "markdown", "mode", "pane_id", "workspace"}
			if !slices.Equal(keys, wantKeys) {
				t.Fatalf("JSON keys = %q, want %q", keys, wantKeys)
			}
			if got.GitStatus != tc.status || got.GitDiffStat != tc.diff || got.GitLog != tc.log {
				t.Fatalf("unexpected Git data: %s", raw)
			}
			wantBranch := ""
			if tc.inside {
				wantBranch = "feature"
			}
			if got.Branch != wantBranch || !got.BoundaryFound || got.Mode != "handoff" || got.LogPath != "/private/tmp/log" {
				t.Fatalf("unexpected metadata: %s", raw)
			}
			want := "# ikesu handoff: workspace / p1\n\n- agent: codex / cwd: /work / branch: " + wantBranch + "\n\n"
			if tc.inside {
				want += "## git status\n" + tc.status + "\n\n## git diff --stat\n" + tc.diff + "\n\n## recent commits\n" + tc.log + "\n\n"
			}
			if tc.timeout {
				want += "git timeout\n\n"
			}
			want += "## last turn\n" + got.LastTurn + "\n\n## raw log\n/private/tmp/log"
			if got.Markdown != want {
				t.Fatalf("Markdown = %q, want %q", got.Markdown, want)
			}
			for _, text := range []string{string(raw), saved} {
				if strings.Contains(text, "ghp_fakeExampleValue") || strings.Contains(text, "sample-secret") || !strings.Contains(text, "AWS_SECRET_KEY=") {
					t.Fatalf("wrong redaction: %q", text)
				}
			}
			if saved != "before\n"+got.LastTurn || strings.Contains(saved, "Working") {
				t.Fatalf("saved log = %q", saved)
			}
		})
	}
}

func TestBuildHandoffKeepsHeadingsAt500Runes(t *testing.T) {
	t.Parallel()
	rows := make([]string, 50)
	for i := range rows {
		rows[i] = fmt.Sprintf(" M file-%02d-%s", i, strings.Repeat("長", 21))
	}
	status := strings.Join(rows, "\n")
	turn := "› prompt\n" + strings.Repeat("長", 300) + "END"
	got := Build(context.Background(), Input{
		Workspace: "work", PaneID: "p1", Agent: "codex", Cwd: "/work", Mode: "handoff", MaxChars: 500, Lines: strings.Split(turn, "\n"),
		Git: func(_ context.Context, _ string, args ...string) (string, error) {
			return map[string]string{gitCheck: "true", gitBranch: "feature", gitStatus: status, gitDiff: " change | 1 +", gitLog: "abc commit"}[strings.Join(args, " ")], nil
		},
	})
	if utf8.RuneCountInString(got.Markdown) > 500 || got.GitStatus != status || got.LastTurn != turn {
		t.Fatalf("Markdown exceeded budget or structured data changed: %+v", got)
	}
	for _, heading := range []string{"# ikesu handoff", "## git status", "## git diff --stat", "## recent commits", "## last turn"} {
		if !strings.Contains(got.Markdown, heading) {
			t.Fatalf("%q is missing: %q", heading, got.Markdown)
		}
	}
	_, lastTurn, _ := strings.Cut(got.Markdown, "## last turn\n")
	if !strings.Contains(got.Markdown, "…（ほか ") || !strings.Contains(got.Markdown, " change | 1 +") || !strings.Contains(got.Markdown, "abc commit") || utf8.RuneCountInString(lastTurn) < 500/3 || !strings.HasSuffix(lastTurn, "END") {
		t.Fatalf("wrong Git or turn budget: %q", got.Markdown)
	}
}

func TestCollectGitStatusRetainsFiftyRows(t *testing.T) {
	t.Parallel()
	got := CollectGit(context.Background(), "/work", func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case gitCheck:
			return "true", nil
		case gitStatus:
			return strings.Repeat(" M file\n", 53), nil
		}
		return "", nil
	})
	if got.Status != strings.Repeat(" M file\n", 50)+"ほか 3 件" {
		t.Fatalf("wrong status cap: %q", got.Status)
	}
}

func TestHandoffGitSectionsShare1200RuneBudget(t *testing.T) {
	t.Parallel()
	makeRows := func(label string) string {
		return strings.Repeat(label+strings.Repeat("字", 80)+"\n", 9)
	}
	result := Build(context.Background(), Input{
		Workspace: "W", Cwd: "/work", Agent: "codex", Mode: "handoff", MaxChars: 1200,
		Lines: []string{"› prompt", strings.Repeat("返", 500)},
		Git: func(_ context.Context, _ string, args ...string) (string, error) {
			return map[string]string{gitCheck: "true", gitBranch: "branch", gitStatus: makeRows("status-row"), gitDiff: makeRows("diff-row"), gitLog: makeRows("commit-row")}[strings.Join(args, " ")], nil
		},
	})
	if utf8.RuneCountInString(result.Markdown) > 1200 {
		t.Fatal(result.Markdown)
	}
	var minRows, maxRows = 10, 0
	for _, row := range []string{"status-row", "diff-row", "commit-row"} {
		count := strings.Count(result.Markdown, row)
		minRows, maxRows = min(minRows, count), max(maxRows, count)
	}
	if minRows < 1 || maxRows-minRows > 1 {
		t.Fatalf("end rows were not removed in rotation: %q", result.Markdown)
	}
}
