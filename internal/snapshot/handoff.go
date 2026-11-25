package snapshot

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

type Result struct {
	Workspace     string `json:"workspace"`
	PaneID        string `json:"pane_id"`
	Agent         string `json:"agent"`
	Cwd           string `json:"cwd"`
	Branch        string `json:"branch"`
	GitStatus     string `json:"git_status"`
	GitDiffStat   string `json:"git_diff_stat"`
	GitLog        string `json:"git_log"`
	LastTurn      string `json:"last_turn"`
	BoundaryFound bool   `json:"boundary_found"`
	LogPath       string `json:"log_path"`
	Markdown      string `json:"markdown"`
	Mode          string `json:"mode"`
}

type Input struct {
	Workspace, PaneID, Agent, Cwd string
	Lines                         []string
	Mode                          string
	FallbackLines, MaxChars       int
	Git                           Runner
	SaveLog                       func(text string) (string, error)
}

func Build(ctx context.Context, in Input) Result {
	lines := redactLines(Clean(in.Lines[max(0, len(in.Lines)-MaxLines):]))
	turn, found := LastTurn(lines, in.Agent, in.FallbackLines)
	result := Result{
		Workspace: safeText(in.Workspace), PaneID: safeText(in.PaneID),
		Agent: safeText(in.Agent), Cwd: safeText(in.Cwd),
		LastTurn: strings.Join(turn, "\n"), BoundaryFound: found, Mode: in.Mode,
	}
	limit := in.MaxChars
	if limit <= 0 {
		limit = DefaultMaxChars
	}

	if in.Mode == "last_turn" {
		prefix := fmt.Sprintf("# ikesu last turn: %s / %s\n\n", result.Workspace, result.PaneID)
		result.Markdown = Trim(prefix+Trim(result.LastTurn, limit-utf8.RuneCountInString(prefix)), limit)
		return result
	}

	info := CollectGit(ctx, in.Cwd, in.Git)
	if info != nil {
		result.Branch, result.GitStatus, result.GitDiffStat, result.GitLog = info.Branch, info.Status, info.DiffStat, info.Log
	}
	if in.SaveLog != nil {
		if path, err := in.SaveLog(strings.Join(lines, "\n")); err == nil {
			result.LogPath = safeText(path)
		}
	}
	result.Markdown = handoffMarkdown(result, info, limit)
	return result
}

type gitSection struct {
	heading string
	lines   []string
}

func (s gitSection) markdown(keep int) string {
	body := strings.Join(s.lines[:keep], "\n")
	if keep < len(s.lines) {
		if keep > 0 {
			body += "\n"
		}
		body += gitRowsOmitted(len(s.lines) - keep)
	}
	return s.heading + "\n" + body + "\n\n"
}

func handoffMarkdown(result Result, info *GitInfo, limit int) string {
	header := fmt.Sprintf("# ikesu handoff: %s / %s\n\n- agent: %s / cwd: %s / branch: %s\n\n",
		result.Workspace, result.PaneID, result.Agent, result.Cwd, result.Branch)
	lastTurnHeading := "## last turn\n"
	suffix := ""
	if result.LogPath != "" {
		suffix = "\n\n## raw log\n" + result.LogPath
	}
	sections := []gitSection{}
	if info != nil {
		for _, section := range []struct{ heading, body string }{
			{"## git status", info.Status},
			{"## git diff --stat", info.DiffStat},
			{"## recent commits", info.Log},
		} {
			var lines []string
			if section.body != "" {
				lines = strings.Split(section.body, "\n")
			}
			sections = append(sections, gitSection{section.heading, lines})
		}
		if info.Truncated != "" {
			lastTurnHeading = info.Truncated + "\n\n" + lastTurnHeading
		}
	}

	fixed := utf8.RuneCountInString(header) + utf8.RuneCountInString(lastTurnHeading) + utf8.RuneCountInString(suffix)
	reserved := min(utf8.RuneCountInString(result.LastTurn), 1000, limit/3)
	gitBudget := limit - fixed - reserved
	git := fitGitSections(sections, gitBudget)
	turn := Trim(result.LastTurn, limit-fixed-utf8.RuneCountInString(git))
	return Trim(header+git+lastTurnHeading+turn+suffix, limit)
}

func fitGitSections(sections []gitSection, budget int) string {
	keep := make([]int, len(sections))
	total := 0
	for i, section := range sections {
		keep[i] = len(section.lines)
		total += utf8.RuneCountInString(section.markdown(keep[i]))
	}
	// Preserve a row from every nonempty section if it fits. Otherwise
	// continue in the same order until the headings and markers fit.
	for _, floor := range []int{1, 0} {
		for total > budget {
			removedRow := false
			for i, section := range sections {
				if keep[i] <= floor {
					continue
				}
				kept := keep[i]
				removed := len(section.lines) - kept
				delta := utf8.RuneCountInString(gitRowsOmitted(removed + 1))
				if removed > 0 {
					delta -= utf8.RuneCountInString(gitRowsOmitted(removed))
				}
				if kept > 1 || removed > 0 {
					delta--
				}
				total += delta - utf8.RuneCountInString(section.lines[kept-1])
				keep[i]--
				removedRow = true
				if total <= budget {
					break
				}
			}
			if !removedRow {
				break
			}
		}
	}
	var text strings.Builder
	for i, section := range sections {
		text.WriteString(section.markdown(keep[i]))
	}
	return text.String()
}

func gitRowsOmitted(n int) string { return fmt.Sprintf("…（ほか %d 行）", n) }
