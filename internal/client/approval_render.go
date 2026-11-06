package client

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/render"
)

func (m *machineClient) approvalCanOpen(e *managedEndpoint) bool {
	if e == nil || e.snapshot == nil || e.snapshot.State == nil || m.blurred || m.pending != nil {
		return false
	}
	view := *e.snapshot
	if m.forms != nil {
		m.forms.Overlay(&view)
	}
	return view.Dialog == nil && view.Menu == nil && view.Help == nil && view.Settings == nil && view.Navigator == nil && view.Copy == nil && (view.ModeHint == "" || view.Prefix)
}

func (s *approvalState) overlay(in *render.Input) {
	in.ApprovalCounts = make(map[int]int)
	in.ApprovalGuardPanes = make(map[int]bool)
	for _, d := range s.pending {
		if pane, ok := approvalPaneID(d.Origin.PaneID); ok {
			in.ApprovalCounts[pane]++
			if d.Kind == "command_guard" {
				in.ApprovalGuardPanes[pane] = true
			}
		}
	}
	d := s.current()
	if d == nil {
		return
	}
	pane, ok := approvalPaneID(d.Origin.PaneID)
	if !ok {
		return
	}
	width := 1
	for _, p := range in.State.DisplayPaneRects() {
		if p.ID == pane {
			width = max(1, min(p.Rect.W, 72)-2)
			break
		}
	}
	var lines []string
	var kinds []render.ApprovalLineKind
	add := func(value, role string, prefix int, selected bool) {
		for _, paragraph := range strings.Split(value, "\n") {
			current, cells := "", 0
			for _, r := range safeApprovalText(paragraph) {
				w := max(1, ansi.StringWidth(string(r)))
				if cells+w > width && current != "" {
					lines = append(lines, current)
					kinds = append(kinds, render.ApprovalLineKind{Role: role, Prefix: prefix, Selected: selected})
					current, cells, prefix = "", 0, 0
					if role == "reason" && width > 8 {
						current, cells = "        ", 8
					}
				}
				current += string(r)
				cells += w
			}
			lines = append(lines, current)
			kinds = append(kinds, render.ApprovalLineKind{Role: role, Prefix: prefix, Selected: selected})
		}
	}
	blank := func() { add("", "", 0, false) }
	footer := func(parts ...string) {
		line := " "
		for _, part := range parts {
			if ansi.StringWidth(" "+part) > width {
				break
			}
			separator := "  ·  "
			if line == " " {
				separator = ""
			}
			if ansi.StringWidth(line+separator+part) > width {
				add(line, "footer", 0, false)
				line, separator = " ", ""
			}
			line += separator + part
		}
		if line != " " {
			add(line, "footer", 0, false)
		}
	}
	paneName := d.Origin.PaneID
	if !d.LateAnswer && d.Origin.PaneLabel != "" {
		paneName += " " + d.Origin.PaneLabel
	}
	workspaceName := d.Origin.WorkspaceLabel
	for _, ws := range in.State.Workspaces {
		for _, tab := range ws.Tabs {
			for _, id := range tab.Layout.PaneIDs() {
				if id == pane {
					if d.LateAnswer {
						if workspaceName == "" {
							workspaceName = render.WorkspaceDisplayName(ws, nil)
						}
					} else {
						workspaceName = render.WorkspaceDisplayName(ws, in.Panes)
					}
				}
			}
		}
	}
	if !d.LateAnswer && d.Origin.TabLabel != "" {
		workspaceName += "/" + d.Origin.TabLabel
	}
	parts := []string{paneName}
	if workspaceName != "" {
		parts = append(parts, workspaceName)
	}
	origin := strings.Join(parts, " · ")
	title := " approve command?"
	if d.LateAnswer {
		title = " answer question"
	} else if _, rule, ok := strings.Cut(d.Title, "（規則: "); ok {
		origin += " · rule " + strings.TrimSuffix(rule, "）")
	}
	add(" "+origin, "subtitle", 0, false)
	blank()
	if d.LateAnswer {
		if d.QuestionLabel != nil {
			add(" "+*d.QuestionLabel, "subtitle", 0, false)
		}
		add(" "+d.Title, "", 0, false)
		if d.Body != nil {
			add(" "+*d.Body, "dim", 0, false)
		}
	} else {
		add(" $ "+d.Origin.Command, "surface", 0, false)
		if d.Origin.TargetCommand != "" {
			add(" target "+d.Origin.TargetCommand, "surface", 0, false)
		}
		blank()
		if d.Body != nil {
			add(" reason "+*d.Body, "reason", 8, false)
		}
	}
	blank()
	for i, option := range d.Options {
		mark := ""
		if d.LateAnswer && d.MultiSelect {
			mark = "  "
			if s.checked[option.ID] {
				mark = "✓ "
			}
		}
		prefix := fmt.Sprintf(" %s%d  ", mark, i+1)
		add(prefix+option.Label, "", ansi.StringWidth(prefix), s.selected == option.ID)
		if d.LateAnswer {
			if option.Description != nil && *option.Description != "" {
				add("    "+*option.Description, "dim", 0, false)
			}
			if option.Preview != nil && *option.Preview != "" {
				add("    preview: "+*option.Preview, "dim", 0, false)
			} else if option.PreviewUnavailable {
				add("    preview: 画面で確認", "dim", 0, false)
			}
		}
	}
	if d.LateAnswer && d.AllowText {
		blank()
		add(" text "+s.reason, "input", 0, false)
	} else if s.selected == "reject" && d.AllowText {
		blank()
		add(" reject note "+s.reason, "input", 0, false)
	}
	blank()
	if s.waiting {
		add(" sending", "dim", 0, false)
	} else if d.AllowText && (d.LateAnswer && s.textMode || !d.LateAnswer && s.selected == "reject") {
		footer("confirm ↵", "back ctrl+u", "close esc")
	} else {
		parts := []string{fmt.Sprintf("select 1-%d", len(d.Options))}
		if d.LateAnswer && d.MultiSelect {
			parts = append(parts, "check space")
		}
		if d.LateAnswer && d.AllowText {
			parts = append(parts, "text t")
		}
		footer(append(parts, "confirm ↵", "scroll ↑↓", "close esc")...)
	}
	in.Approval = &render.Approval{Pane: pane, Title: title, Lines: lines, Kinds: kinds, Scroll: s.scroll, Late: d.LateAnswer}
}

func approvalPaneID(value string) (int, bool) {
	if !strings.HasPrefix(value, "p") {
		return 0, false
	}
	pane, err := strconv.Atoi(strings.TrimPrefix(value, "p"))
	return pane, err == nil && pane > 0 && value == fmt.Sprintf("p%d", pane)
}

func safeApprovalText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, value)
}
