package client

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/api"
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
			if d.Kind == "command_guard" || d.Kind == "tool_permission" {
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
	const reasonIndent = "        "
	add := func(value, role string, prefix int, selected bool) {
		for _, paragraph := range strings.Split(value, "\n") {
			current, cells := "", 0
			for _, r := range safeApprovalText(paragraph) {
				w := max(1, ansi.StringWidth(string(r)))
				if cells+w > width && current != "" {
					lines = append(lines, current)
					kinds = append(kinds, render.ApprovalLineKind{Role: role, Prefix: prefix, Selected: selected})
					current, cells, prefix = "", 0, 0
					if role == "reason" && width >= ansi.StringWidth(reasonIndent)+2 {
						current, cells = reasonIndent, ansi.StringWidth(reasonIndent)
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
	if !d.LateAnswer && d.Kind != "tool_permission" && d.Origin.PaneLabel != "" {
		paneName += " " + d.Origin.PaneLabel
	}
	workspaceName := d.Origin.WorkspaceLabel
	for _, ws := range in.State.Workspaces {
		for _, tab := range ws.Tabs {
			for _, id := range tab.Layout.PaneIDs() {
				if id == pane {
					if d.LateAnswer || d.Kind == "tool_permission" {
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
	if !d.LateAnswer && d.Kind != "tool_permission" && d.Origin.TabLabel != "" {
		workspaceName += "/" + d.Origin.TabLabel
	}
	parts := []string{paneName}
	if workspaceName != "" {
		parts = append(parts, workspaceName)
	}
	if d.Kind == "tool_permission" && !d.LateAnswer {
		parts = append(parts, d.Origin.Tool)
		if d.ExpiresUnixMS != nil {
			parts = append(parts, "expires "+time.UnixMilli(*d.ExpiresUnixMS).Local().Format("15:04"))
		}
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
	} else if d.Kind == "tool_permission" && d.Origin.Tool != "" {
		add(" $ "+d.Origin.Command, "surface", 0, false)
		add(" 画面の表示どおり（折り返しを含む）", "dim", 0, false)
		if d.Origin.Warning != "" {
			add(ansi.Truncate(" warning "+safeApprovalText(d.Origin.Warning), width, "…"), "warning", 0, false)
		}
		blank()
		add(" reason "+d.Origin.Explanation, "reason", 8, false)
	} else if d.Kind == "command_guard" && len(d.Origin.CommandUnits) > 0 {
		addCommandUnits(add, d.Origin, width)
		blank()
		if d.Body != nil {
			add(" reason "+*d.Body, "reason", 8, false)
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
		if d.LateAnswer || d.Kind == "tool_permission" {
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
	} else if s.selectedReject() && d.AllowText {
		blank()
		add(" reject note "+s.reason, "input", 0, false)
	}
	blank()
	if s.waiting {
		add(" sending", "dim", 0, false)
	} else if d.AllowText && (d.LateAnswer && s.textMode || !d.LateAnswer && s.selectedReject()) {
		footer("confirm ↵", "back ctrl+u", "close esc")
	} else {
		parts := []string{fmt.Sprintf("select ↑↓ 1-%d", len(d.Options))}
		if d.LateAnswer && d.MultiSelect {
			parts = append(parts, "check space")
		}
		if d.LateAnswer && d.AllowText {
			parts = append(parts, "text t")
		}
		footer(append(parts, "confirm ↵", "scroll pgup/pgdn", "close esc")...)
	}
	in.Approval = &render.Approval{Pane: pane, Title: title, Lines: lines, Kinds: kinds, Scroll: s.scroll, Follow: s.follow, Late: d.LateAnswer}
}

// addCommandUnits は command_guard の命令を行ごとに出す。確認対象（ask・block）の行は切らずに折り返し、他の行は幅で切る。
func addCommandUnits(add func(value, role string, prefix int, selected bool), o *api.DecisionOrigin, width int) {
	var targets []string
	for n, u := range o.CommandUnits {
		if u.Verdict == "ask" || u.Verdict == "block" {
			targets = append(targets, strconv.Itoa(n+1))
		}
	}
	if len(targets) == 0 {
		rule := o.GuardRule
		if rule == "" {
			rule = "不明"
		}
		add(fmt.Sprintf(" 行を特定できない（規則 %s）", rule), "warning", 0, false)
	} else {
		add(fmt.Sprintf(" %d行のうち %s行目が確認対象", len(o.CommandUnits), strings.Join(targets, "・")), "warning", 0, false)
	}
	for n, u := range o.CommandUnits {
		op := u.Op
		if op == "\n" {
			op = "↵"
		}
		parts := []string{strconv.Itoa(n + 1), u.Label}
		if op != "" {
			parts = append(parts, op)
		}
		parts = append(parts, u.Text)
		line := " " + strings.Join(parts, " ")
		if u.Verdict == "ask" || u.Verdict == "block" {
			add(line, "warning", 0, false)
			if u.Target != "" {
				add("   中で: "+u.Target, "warning", 0, false)
			}
			if u.Reason != "" || u.Rule != "" {
				add("   理由: "+u.Reason+"（規則 "+u.Rule+"）", "warning", 0, false)
			}
			continue
		}
		add(ansi.Truncate(safeApprovalText(line), width, "…"), "dim", 0, false)
	}
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
