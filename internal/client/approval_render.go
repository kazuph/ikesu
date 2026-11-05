package client

import (
	"fmt"
	"strings"

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
	for _, d := range s.pending {
		var pane int
		if _, err := fmt.Sscanf(d.Origin.PaneID, "p%d", &pane); err == nil {
			in.ApprovalCounts[pane]++
		}
	}
	d := s.current()
	if d == nil {
		return
	}
	var pane int
	if _, err := fmt.Sscanf(d.Origin.PaneID, "p%d", &pane); err != nil {
		return
	}
	width := 1
	for _, p := range in.State.DisplayPaneRects() {
		if p.ID == pane {
			width = max(1, min(p.Rect.W, 72)-2)
			break
		}
	}
	lines := []string{}
	add := func(value string) {
		for _, paragraph := range strings.Split(value, "\n") {
			current, cells := "", 0
			for _, r := range paragraph {
				w := max(1, ansi.StringWidth(string(r)))
				if cells+w > width && current != "" {
					lines = append(lines, current)
					current, cells = "", 0
				}
				current += string(r)
				cells += w
			}
			lines = append(lines, current)
		}
	}
	add("出どころ: " + d.Origin.PaneID + " " + d.Origin.PaneLabel)
	add("コマンド: " + d.Origin.Command)
	if d.Origin.TargetCommand != "" {
		add("対象: " + d.Origin.TargetCommand)
	}
	if d.Body != nil {
		add("申請文: " + *d.Body)
	}
	for i, option := range d.Options {
		mark := " "
		if s.selected == option.ID {
			mark = "●"
		}
		add(fmt.Sprintf("%s %d: %s", mark, i+1, option.Label))
	}
	if s.selected == "reject" && d.AllowText {
		add("却下理由: " + s.reason)
	}
	if s.waiting {
		add("回答を送信中")
	} else {
		add("数字: 選択  Enter: 確定  Esc: 閉じる  ↑↓: スクロール")
	}
	in.Approval = &render.Approval{Pane: pane, Title: d.Title, Lines: lines, Scroll: s.scroll}
}
